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
//	GITHUB_TOKEN          - Classic PAT (ghp_...) with project + read:org scopes
//	GITHUB_ORG            - GitHub organization, e.g. "kubernetes"
//	GITHUB_REPOS          - Comma-separated "owner/repo" pairs (required for repo search)
//	GITHUB_LABELS         - Comma-separated labels to require (server-side), e.g. "sig/auth,sig/security"
//	GITHUB_MILESTONE      - Milestone to filter by (server-side), e.g. "v1.36"
//	GITHUB_STATES         - Comma-separated states to include (server-side): open, closed, merged (default: open)
//	GITHUB_EXCLUDE_LABELS - Comma-separated labels to exclude (server-side), e.g. "lifecycle/rotten,lifecycle/stale"
//	GITHUB_INVOLVED       - Comma-separated usernames; post-fetch filter for author/assignee
//	GITHUB_ITEM_TYPES     - Comma-separated: issue, pr (default: all)
//	DEST_BOARD_OWNER      - User/org owning the destination board
//	DEST_BOARD_NAME       - Name for the destination project board
//	DEST_LINK_REPOS       - Comma-separated repos to link to the board
//
// CLI flags:
//
//	--use-cache=true|false  Omit for dry-run. true=cache, false=live fetch.
//	--output cli|board      Output mode (default: "cli")
//	--sync                  Remove stale items from destination board
type Config struct {
	// GitHub auth
	GitHubToken string

	// Search parameters (server-side — reduces API cost)
	Org           string   // GitHub org (used with org: qualifier when no repos specified)
	Repos         []string // "owner/repo" pairs
	Labels        []string // Labels to require (positive filter)
	Milestone     string   // Milestone qualifier
	States        []string // "open", "closed", "merged" (default: ["open"])
	ExcludeLabels []string // Labels to exclude, e.g. "lifecycle/rotten"

	// Post-fetch filters (client-side)
	Involved  []string // GitHub usernames; author or assignee
	ItemTypes []string // "issue", "pr"; empty = all

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
		fmt.Println("")
		fmt.Println("Search queries that would be executed:")
		for _, q := range buildSearchQueries(config) {
			fmt.Printf("  %s\n", q)
		}
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
		log.Println("Fetching items from GitHub GraphQL search API...")
		var err error
		items, err = queryItems(config)
		if err != nil {
			log.Fatalf("Error fetching items: %v", err)
		}
		writeCache(config, items)
	}

	log.Printf("Items before post-fetch filtering: %d", len(items))

	// Filter by involved users (assigned to or authored by)
	if len(config.Involved) > 0 {
		items = filterByInvolved(items, config.Involved)
		log.Printf("  after involved-users: %d", len(items))
	}

	// Filter by item types if specified
	if len(config.ItemTypes) > 0 {
		items = filterByItemTypes(items, config.ItemTypes)
		log.Printf("  after item-types:     %d", len(items))
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

	repos := parseRepos(os.Getenv("GITHUB_REPOS"), org)
	labels := splitAndTrim(os.Getenv("GITHUB_LABELS"))
	milestone := os.Getenv("GITHUB_MILESTONE")
	states := splitAndTrim(os.Getenv("GITHUB_STATES"))
	excludeLabels := splitAndTrim(os.Getenv("GITHUB_EXCLUDE_LABELS"))
	involved := splitAndTrim(os.Getenv("GITHUB_INVOLVED"))
	itemTypes := splitAndTrim(os.Getenv("GITHUB_ITEM_TYPES"))

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
		GitHubToken:   token,
		Org:           org,
		Repos:         repos,
		Labels:        labels,
		Milestone:     milestone,
		States:        states,
		ExcludeLabels: excludeLabels,
		Involved:      involved,
		ItemTypes:     itemTypes,
		OutputMode:    *outputMode,
		BoardOwner:    boardOwner,
		BoardName:     boardName,
		LinkRepos:     linkRepos,
		Sync:          *sync,
		DryRun:        dryRun,
		UseCache:      *useCache,
	}
}

func logConfig(config Config) {
	log.Printf("Search parameters (server-side):")
	log.Printf("  Org:            %s", config.Org)
	if len(config.Repos) > 0 {
		log.Printf("  Repos:          %s", strings.Join(config.Repos, ", "))
	} else {
		log.Printf("  Repos:          (org-wide)")
	}
	if len(config.Labels) > 0 {
		log.Printf("  Labels:         %s", strings.Join(config.Labels, ", "))
	}
	if config.Milestone != "" {
		log.Printf("  Milestone:      %s", config.Milestone)
	}
	if len(config.States) > 0 {
		log.Printf("  States:         %s", strings.Join(config.States, ", "))
	} else {
		log.Printf("  States:         open (default)")
	}
	if len(config.ExcludeLabels) > 0 {
		log.Printf("  Excl labels:    %s", strings.Join(config.ExcludeLabels, ", "))
	}

	log.Printf("Post-fetch filters (client-side):")
	if len(config.Involved) > 0 {
		log.Printf("  Involved:       %s", strings.Join(config.Involved, ", "))
	}
	if len(config.ItemTypes) > 0 {
		log.Printf("  Item types:     %s", strings.Join(config.ItemTypes, ", "))
	} else {
		log.Printf("  Item types:     all")
	}

	log.Printf("Output:")
	log.Printf("  Mode:           %s", config.OutputMode)
	if config.OutputMode == "board" {
		log.Printf("  Board:          %s (owner: %s)", config.BoardName, config.BoardOwner)
	}
	if len(config.LinkRepos) > 0 {
		log.Printf("  Link repos:     %s", strings.Join(config.LinkRepos, ", "))
	}
	if config.DryRun {
		log.Printf("  Execution:      dry-run (pass --use-cache=true or --use-cache=false to execute)")
	} else {
		log.Printf("  Use cache:      %s", config.UseCache)
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

// filterByInvolved keeps items where the author or at least one assignee
// matches the given usernames.
func filterByInvolved(items []ProjectItem, users []string) []ProjectItem {
	userSet := make(map[string]bool, len(users))
	for _, u := range users {
		userSet[strings.ToLower(u)] = true
	}

	var filtered []ProjectItem
	for _, item := range items {
		if item.Author != "" && userSet[strings.ToLower(item.Author)] {
			filtered = append(filtered, item)
			continue
		}
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

	fmt.Printf("\n=== %s Items (repo search) ===\n", config.Org)
	if config.Milestone != "" {
		fmt.Printf("Milestone: %s\n", config.Milestone)
	}
	fmt.Printf("Found %d item(s)\n\n", len(items))

	for _, item := range items {
		prefix := "Issue"
		if item.Type == "PullRequest" {
			prefix = "PR"
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
		if len(item.Labels) > 0 {
			fmt.Printf("         Labels: %s\n", strings.Join(item.Labels, ", "))
		}
		if item.Milestone != "" {
			fmt.Printf("         Milestone: %s\n", item.Milestone)
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
