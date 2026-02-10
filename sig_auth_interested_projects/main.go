package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
)

// Config holds all configuration for the program.
//
// Environment variables:
//
//	GITHUB_TOKEN             - Classic PAT (ghp_...) with project + read:org scopes
//	GITHUB_ORG               - GitHub organization, e.g. "kubernetes"
//	GITHUB_MILESTONE         - Milestone to filter by, e.g. "v1.36"
//	GITHUB_INVOLVED          - Comma-separated usernames; matches items assigned to OR authored by these users
//	GITHUB_REPOS             - Comma-separated "owner/repo" pairs; omit for org-wide
//	GITHUB_ITEM_TYPES        - Comma-separated: issue, pr, draft (default: all)
//	GITHUB_EXCLUDE_STATES    - Comma-separated states to exclude (default: CLOSED,MERGED)
//	GITHUB_EXCLUDE_STATUSES  - Comma-separated board statuses to exclude (default: Done)
//	GITHUB_EXCLUDE_LABELS    - Comma-separated labels to exclude (e.g. lifecycle/rotten,lifecycle/stale)
//	GITHUB_SIG_LABELS        - Comma-separated sig/ labels to require (e.g. sig/auth,sig/security); empty = no filter
//	DEST_BOARD_OWNER         - User/org owning the destination board
//	DEST_BOARD_NAME          - Name for the destination project board
//	DEST_LINK_REPOS          - Comma-separated repos to link to the board
//
// CLI flags:
//
//	--use-cache=true|false  Omit for dry-run. true=cache, false=live fetch.
//	--output cli|board      Output mode (default: "cli")
//	--sync                  Remove stale items from destination board
type Config struct {
	// GitHub auth
	GitHubToken string

	// Query parameters
	Org       string   // GitHub org to search
	Milestone string   // Milestone filter, e.g. "v1.36"
	Involved  []string // GitHub usernames; matches assigned to or authored by
	Repos     []string // "owner/repo" pairs; empty = org-wide
	ItemTypes []string // "issue", "pr", "draft"; empty = all

	// Post-fetch filters
	ExcludeStates   []string // Item states to exclude, e.g. ["CLOSED", "MERGED"]
	ExcludeStatuses []string // Board Status field values to exclude, e.g. ["Done"]
	ExcludeLabels   []string // Labels to exclude, e.g. ["lifecycle/rotten"]
	SigLabels       []string // sig/ labels to require; empty = no filter

	// Destination board
	OutputMode string   // "cli" or "board"
	BoardOwner string   // User/org owning the destination board
	BoardName  string   // Destination project board name
	LinkRepos  []string // Repos to link to the destination board
	Sync       bool     // Remove stale items from destination board

	// Execution mode
	DryRun   bool
	UseCache string // "true", "false", or "" (unset = dry-run)
}

func main() {
	config := loadConfig()

	// Always check rate limits first (free call)
	checkRateLimitOrAbort(config)

	logConfig(config)
	estimateQueryCost(config)

	if config.DryRun {
		fmt.Println("=== Dry Run ===")
		fmt.Println("No API queries executed. Pass --use-cache=true or --use-cache=false to run.")
		fmt.Println("")
		fmt.Println("Examples:")
		fmt.Println("  go run . --use-cache=false                        # Fetch live, cache results, print to CLI")
		fmt.Println("  go run . --use-cache=true                         # Use cached data, print to CLI")
		fmt.Println("  go run . --use-cache=false --output board          # Fetch live and update board")
		fmt.Println("  go run . --use-cache=true  --output board          # Update board from cache")
		fmt.Println("  go run . --use-cache=true  --output board --sync   # Update board + remove stale items")
		return
	}

	// Fetch or load cached items
	var items []ProjectItem
	if config.UseCache == "true" {
		items = readCacheLatest(config)
		if items == nil {
			log.Fatal("No cached data found. Run with --use-cache=false first to fetch and cache.")
		}
	} else {
		log.Println("Fetching items from GitHub GraphQL API...")
		var err error
		items, err = queryItems(config)
		if err != nil {
			log.Fatalf("Error fetching items: %v", err)
		}
		writeCache(config, items)
	}

	log.Printf("Items before filtering: %d", len(items))

	// Exclude by state (CLOSED, MERGED)
	if len(config.ExcludeStates) > 0 {
		items = filterExcludeStates(items, config.ExcludeStates)
		log.Printf("  after exclude-states:   %d", len(items))
	}

	// Exclude by board Status field (Done, etc.)
	if len(config.ExcludeStatuses) > 0 {
		items = filterExcludeStatuses(items, config.ExcludeStatuses)
		log.Printf("  after exclude-statuses: %d", len(items))
	}

	// Exclude by labels (lifecycle/rotten, etc.)
	if len(config.ExcludeLabels) > 0 {
		items = filterExcludeLabels(items, config.ExcludeLabels)
		log.Printf("  after exclude-labels:   %d", len(items))
	}

	// Require at least one sig/ label from the allowlist
	if len(config.SigLabels) > 0 {
		items = filterBySigLabels(items, config.SigLabels)
		log.Printf("  after sig-labels:       %d", len(items))
	}

	// Filter by involved users (assigned to or authored by)
	if len(config.Involved) > 0 {
		items = filterByInvolved(items, config.Involved)
		log.Printf("  after involved-users:   %d", len(items))
	}

	// Filter by item types if specified
	if len(config.ItemTypes) > 0 {
		items = filterByItemTypes(items, config.ItemTypes)
		log.Printf("  after item-types:       %d", len(items))
	}

	log.Printf("Total items after filtering: %d", len(items))

	switch config.OutputMode {
	case "board":
		if err := updateBoard(config, items); err != nil {
			log.Fatalf("Error updating board: %v", err)
		}
	default:
		printItems(items, config)
	}
}

func loadConfig() Config {
	outputMode := flag.String("output", "cli", "Output mode: 'cli' (print to terminal) or 'board' (create/update GitHub project board)")
	sync := flag.Bool("sync", false, "When using --output=board, remove items from the board that no longer match the query")
	useCache := flag.String("use-cache", "", "'true' to use cached data, 'false' to fetch live. Omit for dry-run.")
	flag.Parse()

	dryRun := *useCache == ""
	if !dryRun && *useCache != "true" && *useCache != "false" {
		log.Fatal("--use-cache must be 'true' or 'false'")
	}

	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		log.Fatal("GITHUB_TOKEN environment variable is required")
	}

	org := os.Getenv("GITHUB_ORG")
	if org == "" {
		log.Fatal("GITHUB_ORG environment variable is required (e.g. \"kubernetes\")")
	}

	milestone := os.Getenv("GITHUB_MILESTONE")
	involved := splitAndTrim(os.Getenv("GITHUB_INVOLVED"))
	repos := parseRepos(os.Getenv("GITHUB_REPOS"), org)
	itemTypes := splitAndTrim(os.Getenv("GITHUB_ITEM_TYPES"))

	// Post-fetch filters with sensible defaults
	excludeStates := splitAndTrim(os.Getenv("GITHUB_EXCLUDE_STATES"))
	if len(excludeStates) == 0 && os.Getenv("GITHUB_EXCLUDE_STATES") == "" {
		excludeStates = []string{"CLOSED", "MERGED"}
	}
	excludeStatuses := splitAndTrim(os.Getenv("GITHUB_EXCLUDE_STATUSES"))
	if len(excludeStatuses) == 0 && os.Getenv("GITHUB_EXCLUDE_STATUSES") == "" {
		excludeStatuses = []string{"Done"}
	}
	excludeLabels := splitAndTrim(os.Getenv("GITHUB_EXCLUDE_LABELS"))
	sigLabels := splitAndTrim(os.Getenv("GITHUB_SIG_LABELS"))

	boardOwner := os.Getenv("DEST_BOARD_OWNER")
	boardName := os.Getenv("DEST_BOARD_NAME")
	linkRepos := parseLinkRepos(os.Getenv("DEST_LINK_REPOS"), boardOwner)

	if *outputMode == "board" && boardOwner == "" {
		log.Fatal("DEST_BOARD_OWNER is required when using --output=board")
	}
	if *outputMode == "board" && boardName == "" {
		log.Fatal("DEST_BOARD_NAME is required when using --output=board")
	}

	return Config{
		GitHubToken:     token,
		Org:             org,
		Milestone:       milestone,
		Involved:        involved,
		Repos:           repos,
		ItemTypes:       itemTypes,
		ExcludeStates:   excludeStates,
		ExcludeStatuses: excludeStatuses,
		ExcludeLabels:   excludeLabels,
		SigLabels:       sigLabels,
		OutputMode:      *outputMode,
		BoardOwner:      boardOwner,
		BoardName:       boardName,
		LinkRepos:       linkRepos,
		Sync:            *sync,
		DryRun:          dryRun,
		UseCache:        *useCache,
	}
}

func logConfig(config Config) {
	log.Printf("Query parameters:")
	log.Printf("  Org:        %s", config.Org)
	if config.Milestone != "" {
		log.Printf("  Milestone:  %s", config.Milestone)
	}
	if len(config.Involved) > 0 {
		log.Printf("  Involved:   %s", strings.Join(config.Involved, ", "))
	}
	if len(config.Repos) > 0 {
		log.Printf("  Repos:      %s", strings.Join(config.Repos, ", "))
	} else {
		log.Printf("  Repos:      (org-wide)")
	}
	if len(config.ItemTypes) > 0 {
		log.Printf("  Item types: %s", strings.Join(config.ItemTypes, ", "))
	} else {
		log.Printf("  Item types: all")
	}
	if len(config.ExcludeStates) > 0 {
		log.Printf("  Excl states:    %s", strings.Join(config.ExcludeStates, ", "))
	}
	if len(config.ExcludeStatuses) > 0 {
		log.Printf("  Excl statuses:  %s", strings.Join(config.ExcludeStatuses, ", "))
	}
	if len(config.ExcludeLabels) > 0 {
		log.Printf("  Excl labels:    %s", strings.Join(config.ExcludeLabels, ", "))
	}
	if len(config.SigLabels) > 0 {
		log.Printf("  SIG labels:     %s", strings.Join(config.SigLabels, ", "))
	}
	log.Printf("  Output:     %s", config.OutputMode)
	if config.OutputMode == "board" {
		log.Printf("  Board:      %s (owner: %s)", config.BoardName, config.BoardOwner)
	}
	if len(config.LinkRepos) > 0 {
		log.Printf("  Link repos: %s", strings.Join(config.LinkRepos, ", "))
	}
	if config.DryRun {
		log.Printf("  Mode:       dry-run (pass --use-cache=true or --use-cache=false to execute)")
	} else {
		log.Printf("  Use cache:  %s", config.UseCache)
	}
}

// parseRepos parses "owner/repo" or "repo" entries from GITHUB_REPOS.
// Bare repo names are prefixed with the org.
func parseRepos(raw, org string) []string {
	parts := splitAndTrim(raw)
	var repos []string
	for _, p := range parts {
		if !strings.Contains(p, "/") {
			p = org + "/" + p
		}
		repos = append(repos, p)
	}
	return repos
}

// parseLinkRepos parses DEST_LINK_REPOS into "owner/repo" entries.
func parseLinkRepos(raw, boardOwner string) []string {
	parts := splitAndTrim(raw)
	var repos []string
	for _, p := range parts {
		if !strings.Contains(p, "/") && boardOwner != "" {
			p = boardOwner + "/" + p
		}
		repos = append(repos, p)
	}
	return repos
}

// splitAndTrim splits a comma-separated string and trims whitespace.
func splitAndTrim(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	var result []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

// filterExcludeStates removes items whose State matches any of the given states.
// States are compared case-insensitively (e.g. "CLOSED", "MERGED").
func filterExcludeStates(items []ProjectItem, states []string) []ProjectItem {
	exclude := make(map[string]bool, len(states))
	for _, s := range states {
		exclude[strings.ToUpper(s)] = true
	}
	var filtered []ProjectItem
	for _, item := range items {
		if !exclude[strings.ToUpper(item.State)] {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

// filterExcludeStatuses removes items whose board Status field matches any of
// the given values. Comparison is case-insensitive. Items with no Status field
// are kept.
func filterExcludeStatuses(items []ProjectItem, statuses []string) []ProjectItem {
	exclude := make(map[string]bool, len(statuses))
	for _, s := range statuses {
		exclude[strings.ToLower(s)] = true
	}
	var filtered []ProjectItem
	for _, item := range items {
		status := strings.ToLower(item.Fields["Status"])
		if status == "" || !exclude[status] {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

// filterExcludeLabels removes items that carry any of the given labels.
// Comparison is case-insensitive.
func filterExcludeLabels(items []ProjectItem, labels []string) []ProjectItem {
	exclude := make(map[string]bool, len(labels))
	for _, l := range labels {
		exclude[strings.ToLower(l)] = true
	}
	var filtered []ProjectItem
	for _, item := range items {
		has := false
		for _, l := range item.Labels {
			if exclude[strings.ToLower(l)] {
				has = true
				break
			}
		}
		if !has {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

// filterBySigLabels keeps items that have at least one label matching the
// given sig/ label allowlist. Items with no labels are dropped.
// Comparison is case-insensitive.
func filterBySigLabels(items []ProjectItem, sigLabels []string) []ProjectItem {
	allow := make(map[string]bool, len(sigLabels))
	for _, s := range sigLabels {
		allow[strings.ToLower(s)] = true
	}
	var filtered []ProjectItem
	for _, item := range items {
		for _, l := range item.Labels {
			if allow[strings.ToLower(l)] {
				filtered = append(filtered, item)
				break
			}
		}
	}
	return filtered
}

// filterByInvolved keeps items where the author or at least one assignee
// matches the given usernames.
func filterByInvolved(items []ProjectItem, users []string) []ProjectItem {
	userSet := make(map[string]bool, len(users))
	for _, u := range users {
		userSet[strings.ToLower(u)] = true
	}

	var filtered []ProjectItem
	for _, item := range items {
		// Check author
		if item.Author != "" && userSet[strings.ToLower(item.Author)] {
			filtered = append(filtered, item)
			continue
		}
		// Check assignees
		for _, a := range item.Assignees {
			if userSet[strings.ToLower(a)] {
				filtered = append(filtered, item)
				break
			}
		}
	}
	return filtered
}

// filterByItemTypes keeps items matching the requested types.
func filterByItemTypes(items []ProjectItem, types []string) []ProjectItem {
	typeSet := make(map[string]bool, len(types))
	for _, t := range types {
		switch strings.ToLower(t) {
		case "issues", "issue":
			typeSet["Issue"] = true
		case "pr", "pullrequest", "pull_request":
			typeSet["PullRequest"] = true
		case "draft", "draftissue", "draft_issue":
			typeSet["DraftIssue"] = true
		}
	}

	var filtered []ProjectItem
	for _, item := range items {
		if typeSet[item.Type] {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

// printItems renders items to stdout.
func printItems(items []ProjectItem, config Config) {
	if len(items) == 0 {
		fmt.Println("\nNo items found matching the criteria.")
		return
	}

	fmt.Printf("\n=== %s Items ===\n", config.Org)
	if config.Milestone != "" {
		fmt.Printf("Milestone: %s\n", config.Milestone)
	}
	fmt.Printf("Found %d item(s)\n\n", len(items))

	for _, item := range items {
		prefix := ""
		switch item.Type {
		case "Issue":
			prefix = "Issue"
		case "PullRequest":
			prefix = "PR"
		case "DraftIssue":
			prefix = "Draft"
		}

		assigneeStr := "unassigned"
		if len(item.Assignees) > 0 {
			assigneeStr = strings.Join(item.Assignees, ", ")
		}

		fmt.Printf("[%s] #%-5d %s\n", prefix, item.Number, item.Title)
		if item.Author != "" {
			fmt.Printf("         Author: %s\n", item.Author)
		}
		fmt.Printf("         Assignees: %s\n", assigneeStr)
		if item.URL != "" {
			fmt.Printf("         URL: %s\n", item.URL)
		}
		if item.Repo != "" {
			fmt.Printf("         Repo: %s\n", item.Repo)
		}
		if item.ProjectTitle != "" {
			fmt.Printf("         Project: %s\n", item.ProjectTitle)
		}
		if len(item.Labels) > 0 {
			fmt.Printf("         Labels: %s\n", strings.Join(item.Labels, ", "))
		}
		if item.Milestone != "" {
			fmt.Printf("         Milestone: %s\n", item.Milestone)
		}

		// Print custom field values from the source project
		if len(item.Fields) > 0 {
			var fieldParts []string
			for k, v := range item.Fields {
				fieldParts = append(fieldParts, fmt.Sprintf("%s: %s", k, v))
			}
			fmt.Printf("         Fields: %s\n", strings.Join(fieldParts, "  |  "))
		}
		fmt.Println()
	}
}

// --- Helpers used across files ---

// parseInt is a safe int parser that returns 0 on error.
func parseInt(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}
