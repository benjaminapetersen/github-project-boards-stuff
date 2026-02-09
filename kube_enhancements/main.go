package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"

	"github.com/google/go-github/v57/github"
	"golang.org/x/oauth2"
)

// checkRateLimit inspects a github.Response for 429 status and returns a
// descriptive RateLimitError if throttled.
func checkRateLimit(resp *github.Response, err error) error {
	if resp != nil && resp.StatusCode == 429 {
		retryAfter := resp.Header.Get("Retry-After")
		return &RateLimitError{
			StatusCode: 429,
			RetryAfter: retryAfter,
		}
	}
	return err
}

const (
	defaultOwner = "kubernetes"
	defaultRepo  = "enhancements"
)

// Config holds all the configuration for the program.
// Environment variables provide credentials and query parameters.
// CLI flags control the output mode.
//
// Required env vars:
//
//	GITHUB_TOKEN       - A GitHub personal access token (classic or fine-grained)
//	LABELS             - Comma-separated list of labels, e.g. "sig/auth"
//
// Optional env vars:
//
//	MILESTONES         - Comma-separated list of milestone titles, e.g. "v1.36,v1.35"
//	USERS              - Comma-separated list of GitHub usernames to filter by assignee
//	REPO_OWNER         - GitHub org/owner (default: "kubernetes")
//	REPO_NAME          - GitHub repo  (default: "enhancements")
//	STATE              - Issue state: "open", "closed", or "all" (default: "open")
//	BOARD_OWNER        - GitHub user/org that owns the project board (for --output=board)
//	BOARD_NAME         - Override the auto-generated board name
//	LINK_REPOS         - Comma-separated list of repos to link to the board (e.g. "owner/repo1,repo2")
//
// CLI flags:
//
//	--use-cache          Omit for dry-run. 'true' to use cache, 'false' to fetch live.
//	--output cli|board   Output mode (default: "cli")
//	--sync               When using board output, remove stale items not matching current query
type Config struct {
	GitHubToken string
	RepoOwner   string
	RepoName    string
	Labels      []string
	Milestones  []string
	Users       []string
	State       string

	// Board-related config
	OutputMode string // "cli" or "board"
	BoardOwner string // GitHub user/org owning the project board
	BoardName  string   // Project board title (auto-generated if empty)
	LinkRepos  []string // Repos to link to the project board (e.g. ["owner/repo"])
	Sync       bool     // Remove stale items from the board

	// Execution mode
	DryRun   bool   // true when --use-cache is not provided
	UseCache string // "true", "false", or "" (unset = dry-run)
}

func main() {
	config := loadConfig()

	ctx := context.Background()
	client := createGitHubClient(ctx, config.GitHubToken)

	// Always check rate limits first (GET /rate_limit does NOT count against limits)
	checkRateLimitOrAbort(ctx, client, config)

	log.Printf("Query parameters:")
	log.Printf("  Repo:       %s/%s", config.RepoOwner, config.RepoName)
	log.Printf("  Labels:     %s", strings.Join(config.Labels, ", "))
	if len(config.Milestones) > 0 {
		log.Printf("  Milestones: %s", strings.Join(config.Milestones, ", "))
	}
	if len(config.Users) > 0 {
		log.Printf("  Users:      %s", strings.Join(config.Users, ", "))
	}
	log.Printf("  State:      %s", config.State)
	log.Printf("  Output:     %s", config.OutputMode)
	if len(config.LinkRepos) > 0 {
		log.Printf("  Link repos: %s", strings.Join(config.LinkRepos, ", "))
	}
	if config.DryRun {
		log.Printf("  Mode:       dry-run (pass --use-cache=true or --use-cache=false to execute)")
	} else {
		log.Printf("  Use cache:  %s", config.UseCache)
	}

	// Show estimated cost
	estimateQueryCost(config)

	// If dry-run, stop here
	if config.DryRun {
		fmt.Println("=== Dry Run ===")
		fmt.Println("No API queries executed. Pass --use-cache to run.")
		fmt.Println("")
		fmt.Println("Examples:")
		fmt.Println("  go run . --use-cache=false                       # Fetch live, cache results, print to CLI")
		fmt.Println("  go run . --use-cache=true                        # Use cached data, print to CLI")
		fmt.Println("  go run . --use-cache=false --output board         # Fetch live and update board")
		fmt.Println("  go run . --use-cache=true  --output board         # Update board from cache")
		fmt.Println("  go run . --use-cache=true  --output board --sync  # Update board + remove stale items")
		return
	}

	var issues []*github.Issue
	if config.UseCache == "true" {
		issues = readCacheLatest(config, "issues")
		if issues == nil {
			log.Fatal("No cached data found. Run with --use-cache=false first to fetch and cache.")
		}
	} else {
		log.Printf("Fetching issues from GitHub API...")
		var err error
		issues, err = fetchIssues(ctx, client, config)
		if err != nil {
			log.Fatalf("Error fetching issues: %v", err)
		}
		// Always cache the raw response
		writeCache(cacheKey(config, "issues"), issues)
	}

	if len(config.Users) > 0 {
		issues = filterByAssignees(issues, config.Users)
	}

	switch config.OutputMode {
	case "board":
		updateBoard(ctx, config, issues)
	default:
		printIssues(issues, config)
	}
}

// loadConfig reads CLI flags and environment variables.
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

	labelsStr := os.Getenv("LABELS")
	if labelsStr == "" {
		log.Fatal("LABELS environment variable is required (e.g. \"sig/auth\")")
	}
	labels := splitAndTrim(labelsStr)

	milestones := splitAndTrim(os.Getenv("MILESTONES"))
	users := splitAndTrim(os.Getenv("USERS"))

	owner := os.Getenv("REPO_OWNER")
	if owner == "" {
		owner = defaultOwner
	}
	repo := os.Getenv("REPO_NAME")
	if repo == "" {
		repo = defaultRepo
	}

	state := os.Getenv("STATE")
	if state == "" {
		state = "open"
	}

	boardOwner := os.Getenv("BOARD_OWNER")
	boardName := os.Getenv("BOARD_NAME")
	linkRepos := parseLinkRepos(os.Getenv("LINK_REPOS"), boardOwner)

	if *outputMode == "board" && boardOwner == "" {
		log.Fatal("BOARD_OWNER environment variable is required when using --output=board")
	}

	return Config{
		GitHubToken: token,
		RepoOwner:   owner,
		RepoName:    repo,
		Labels:      labels,
		Milestones:  milestones,
		Users:       users,
		State:       state,
		OutputMode:  *outputMode,
		BoardOwner:  boardOwner,
		BoardName:   boardName,
		LinkRepos:   linkRepos,
		Sync:        *sync,
		DryRun:      dryRun,
		UseCache:    *useCache,
	}
}

// parseLinkRepos parses LINK_REPOS into fully-qualified "owner/repo" entries.
// Entries without a "/" are prefixed with the boardOwner.
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

// splitAndTrim splits a comma-separated string and trims whitespace from each element.
// Returns nil if the input is empty.
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

func createGitHubClient(ctx context.Context, token string) *github.Client {
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: token},
	)
	tc := oauth2.NewClient(ctx, ts)
	return github.NewClient(tc)
}

// fetchIssues retrieves issues from GitHub matching the configured labels and milestones.
// If milestones are specified, it resolves their titles to milestone numbers first.
func fetchIssues(ctx context.Context, client *github.Client, config Config) ([]*github.Issue, error) {
	// If milestones are specified, we need to resolve their names to numbers
	// and query per-milestone, since the GitHub REST API only filters by one milestone number at a time.
	if len(config.Milestones) > 0 {
		milestoneNumbers, err := resolveMilestones(ctx, client, config)
		if err != nil {
			return nil, fmt.Errorf("resolving milestones: %w", err)
		}

		var allIssues []*github.Issue
		seen := make(map[int64]bool)

		for _, msNum := range milestoneNumbers {
			msStr := fmt.Sprintf("%d", msNum)
			issues, err := listIssues(ctx, client, config, msStr)
			if err != nil {
				return nil, err
			}
			for _, issue := range issues {
				if !seen[issue.GetID()] {
					seen[issue.GetID()] = true
					allIssues = append(allIssues, issue)
				}
			}
		}
		return allIssues, nil
	}

	// No milestone filter — just query with labels
	return listIssues(ctx, client, config, "")
}

// listIssues pages through all issues matching labels, state, and optionally a milestone number.
func listIssues(ctx context.Context, client *github.Client, config Config, milestone string) ([]*github.Issue, error) {
	var allIssues []*github.Issue

	opts := &github.IssueListByRepoOptions{
		Labels: config.Labels,
		State:  config.State,
		ListOptions: github.ListOptions{
			PerPage: 100,
		},
	}
	if milestone != "" {
		opts.Milestone = milestone
	}

	for {
		issues, resp, err := client.Issues.ListByRepo(ctx, config.RepoOwner, config.RepoName, opts)
		if err != nil {
			if rlErr := checkRateLimit(resp, err); rlErr != nil {
				return nil, rlErr
			}
			return nil, fmt.Errorf("listing issues: %w", err)
		}

		// The Issues API also returns pull requests; filter them out
		for _, issue := range issues {
			if issue.PullRequestLinks == nil {
				allIssues = append(allIssues, issue)
			}
		}

		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	return allIssues, nil
}

// resolveMilestones maps milestone title strings (e.g. "v1.36") to their numeric IDs.
func resolveMilestones(ctx context.Context, client *github.Client, config Config) ([]int, error) {
	titleSet := make(map[string]bool, len(config.Milestones))
	for _, m := range config.Milestones {
		titleSet[m] = true
	}

	var numbers []int
	opts := &github.MilestoneListOptions{
		State: "all",
		ListOptions: github.ListOptions{
			PerPage: 100,
		},
	}

	for {
		milestones, resp, err := client.Issues.ListMilestones(ctx, config.RepoOwner, config.RepoName, opts)
		if err != nil {
			if rlErr := checkRateLimit(resp, err); rlErr != nil {
				return nil, rlErr
			}
			return nil, fmt.Errorf("listing milestones: %w", err)
		}

		for _, ms := range milestones {
			if titleSet[ms.GetTitle()] {
				numbers = append(numbers, ms.GetNumber())
			}
		}

		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	if len(numbers) == 0 {
		return nil, fmt.Errorf("no milestones found matching: %s", strings.Join(config.Milestones, ", "))
	}

	return numbers, nil
}

// filterByAssignees keeps only issues where at least one assignee matches the given usernames.
func filterByAssignees(issues []*github.Issue, users []string) []*github.Issue {
	userSet := make(map[string]bool, len(users))
	for _, u := range users {
		userSet[strings.ToLower(u)] = true
	}

	var filtered []*github.Issue
	for _, issue := range issues {
		for _, assignee := range issue.Assignees {
			if userSet[strings.ToLower(assignee.GetLogin())] {
				filtered = append(filtered, issue)
				break
			}
		}
	}
	return filtered
}

// updateBoard creates or updates a GitHub Projects V2 board with the fetched issues.
func updateBoard(ctx context.Context, config Config, issues []*github.Issue) {
	gql := newGraphQLClient(ctx, config.GitHubToken)

	boardName := config.BoardName
	if boardName == "" {
		boardName = generateBoardName(config)
	}

	log.Printf("Board name: %q", boardName)
	log.Printf("Board owner: %s", config.BoardOwner)

	// Find or create the project
	project, err := findProject(ctx, gql, config.BoardOwner, boardName)
	if err != nil {
		log.Fatalf("Error searching for project: %v", err)
	}

	if project == nil {
		log.Printf("Project %q not found, creating...", boardName)
		project, err = createProject(ctx, gql, config.BoardOwner, boardName)
		if err != nil {
			log.Fatalf("Error creating project: %v", err)
		}
		log.Printf("Created project: %s", project.URL)
	} else {
		log.Printf("Found existing project: %s", project.URL)
	}

	// Add issues to the board
	log.Printf("Adding %d issue(s) to project board...", len(issues))
	added, skipped, err := addIssuesToProject(ctx, gql, project.ID, issues)
	if err != nil {
		log.Fatalf("Error adding issues: %v", err)
	}
	log.Printf("Done: %d added, %d skipped (already present or error)", added, skipped)

	// Link project to repositories if configured
	if len(config.LinkRepos) > 0 {
		log.Printf("Linking project to %d repository(ies)...", len(config.LinkRepos))
		linked, linkSkipped, err := linkProjectToRepositories(ctx, gql, project.ID, config.LinkRepos)
		if err != nil {
			log.Printf("Warning: error linking repositories: %v", err)
		} else {
			log.Printf("Done: %d linked, %d skipped (already linked or error)", linked, linkSkipped)
		}
	}

	// Optionally remove stale items
	if config.Sync {
		log.Printf("Syncing: removing stale items not in current query...")
		removed, err := removeStaleItems(ctx, gql, project.ID, issues)
		if err != nil {
			log.Printf("Warning: error removing stale items: %v", err)
		} else {
			log.Printf("Removed %d stale item(s)", removed)
		}
	}

	fmt.Printf("\nProject board: %s\n", project.URL)
}

// printIssues renders the fetched issues to stdout in a readable format.
func printIssues(issues []*github.Issue, config Config) {
	if len(issues) == 0 {
		fmt.Println("\nNo issues found matching the criteria.")
		return
	}

	// Sort by issue number descending (newest first)
	sort.Slice(issues, func(i, j int) bool {
		return issues[i].GetNumber() > issues[j].GetNumber()
	})

	fmt.Printf("\n=== %s/%s Enhancement Issues ===\n", config.RepoOwner, config.RepoName)
	fmt.Printf("Found %d issue(s)\n\n", len(issues))

	for _, issue := range issues {
		milestone := "none"
		if issue.Milestone != nil {
			milestone = issue.GetMilestone().GetTitle()
		}

		var assignees []string
		for _, a := range issue.Assignees {
			assignees = append(assignees, a.GetLogin())
		}
		assigneeStr := "unassigned"
		if len(assignees) > 0 {
			assigneeStr = strings.Join(assignees, ", ")
		}

		var labelNames []string
		for _, l := range issue.Labels {
			labelNames = append(labelNames, l.GetName())
		}

		stageLabel := "unknown"
		for _, l := range labelNames {
			if strings.HasPrefix(l, "stage/") {
				stageLabel = l
				break
			}
		}

		fmt.Printf("#%-5d %s\n", issue.GetNumber(), issue.GetTitle())
		fmt.Printf("       Stage: %-20s  Milestone: %-10s  Assignees: %s\n", stageLabel, milestone, assigneeStr)
		fmt.Printf("       URL: %s\n", issue.GetHTMLURL())
		fmt.Printf("       Labels: %s\n", strings.Join(labelNames, ", "))
		fmt.Println()
	}
}
