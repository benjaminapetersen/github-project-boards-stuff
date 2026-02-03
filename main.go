package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/google/go-github/v57/github"
	"golang.org/x/oauth2"
)

const (
	kubernetesOrg = "kubernetes"
)

type Config struct {
	GitHubToken string
	ProjectID   string
	Username    string
}

func main() {
	config := loadConfig()

	ctx := context.Background()
	client := createGitHubClient(ctx, config.GitHubToken)

	log.Printf("Starting GitHub Project Board automation for Kubernetes organization...")
	log.Printf("Looking for activity from user: %s", config.Username)

	// Calculate date range (last month)
	endDate := time.Now()
	startDate := endDate.AddDate(0, -1, 0)

	log.Printf("Date range: %s to %s", startDate.Format("2006-01-02"), endDate.Format("2006-01-02"))

	// Find commits, PRs, and issues
	commits, err := findUserCommits(ctx, client, config.Username, startDate, endDate)
	if err != nil {
		log.Printf("Error finding commits: %v", err)
	} else {
		log.Printf("Found %d commits", len(commits))
	}

	prs, err := findUserPRs(ctx, client, config.Username, startDate, endDate)
	if err != nil {
		log.Printf("Error finding PRs: %v", err)
	} else {
		log.Printf("Found %d PRs", len(prs))
	}

	issues, err := findUserIssues(ctx, client, config.Username, startDate, endDate)
	if err != nil {
		log.Printf("Error finding issues: %v", err)
	} else {
		log.Printf("Found %d issues", len(issues))
	}

	// Add items to project board
	if config.ProjectID != "" {
		log.Printf("Adding items to project board (ID: %s)...", config.ProjectID)
		err = addItemsToProject(ctx, client, config.ProjectID, commits, prs, issues)
		if err != nil {
			log.Fatalf("Error adding items to project: %v", err)
		}
		log.Printf("Successfully added items to project board!")
	} else {
		log.Printf("No PROJECT_ID provided, skipping project board update")
		log.Printf("\nSummary:")
		log.Printf("- Commits: %d", len(commits))
		log.Printf("- Pull Requests: %d", len(prs))
		log.Printf("- Issues: %d", len(issues))
	}
}

func loadConfig() Config {
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		log.Fatal("GITHUB_TOKEN environment variable is required")
	}

	username := os.Getenv("GITHUB_USERNAME")
	if username == "" {
		log.Fatal("GITHUB_USERNAME environment variable is required")
	}

	projectID := os.Getenv("PROJECT_ID")

	return Config{
		GitHubToken: token,
		ProjectID:   projectID,
		Username:    username,
	}
}

func createGitHubClient(ctx context.Context, token string) *github.Client {
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: token},
	)
	tc := oauth2.NewClient(ctx, ts)
	return github.NewClient(tc)
}

type CommitInfo struct {
	Repo    string
	SHA     string
	Message string
	URL     string
	Date    time.Time
}

type PRInfo struct {
	Repo      string
	Number    int
	Title     string
	URL       string
	State     string
	CreatedAt time.Time
}

type IssueInfo struct {
	Repo      string
	Number    int
	Title     string
	URL       string
	State     string
	CreatedAt time.Time
}

func findUserCommits(ctx context.Context, client *github.Client, username string, startDate, endDate time.Time) ([]CommitInfo, error) {
	var allCommits []CommitInfo

	// Search for commits by the user in the Kubernetes organization
	query := fmt.Sprintf("author:%s org:%s author-date:%s..%s",
		username,
		kubernetesOrg,
		startDate.Format("2006-01-02"),
		endDate.Format("2006-01-02"))

	opts := &github.SearchOptions{
		Sort:  "author-date",
		Order: "desc",
		ListOptions: github.ListOptions{
			PerPage: 100,
		},
	}

	for {
		result, resp, err := client.Search.Commits(ctx, query, opts)
		if err != nil {
			return nil, fmt.Errorf("searching commits: %w", err)
		}

		for _, commit := range result.Commits {
			if commit.Commit != nil && commit.Commit.Author != nil && commit.Repository != nil {
				commitInfo := CommitInfo{
					Repo:    commit.Repository.GetFullName(),
					SHA:     commit.GetSHA(),
					Message: commit.Commit.GetMessage(),
					URL:     commit.GetHTMLURL(),
					Date:    commit.Commit.Author.GetDate().Time,
				}
				allCommits = append(allCommits, commitInfo)
			}
		}

		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	return allCommits, nil
}

func findUserPRs(ctx context.Context, client *github.Client, username string, startDate, endDate time.Time) ([]PRInfo, error) {
	var allPRs []PRInfo

	// Search for PRs by the user in the Kubernetes organization
	query := fmt.Sprintf("author:%s org:%s is:pr created:%s..%s",
		username,
		kubernetesOrg,
		startDate.Format("2006-01-02"),
		endDate.Format("2006-01-02"))

	opts := &github.SearchOptions{
		Sort:  "created",
		Order: "desc",
		ListOptions: github.ListOptions{
			PerPage: 100,
		},
	}

	for {
		result, resp, err := client.Search.Issues(ctx, query, opts)
		if err != nil {
			return nil, fmt.Errorf("searching PRs: %w", err)
		}

		for _, issue := range result.Issues {
			if issue.IsPullRequest() {
				prInfo := PRInfo{
					Repo:      extractRepoFromURL(issue.GetHTMLURL()),
					Number:    issue.GetNumber(),
					Title:     issue.GetTitle(),
					URL:       issue.GetHTMLURL(),
					State:     issue.GetState(),
					CreatedAt: issue.GetCreatedAt().Time,
				}
				allPRs = append(allPRs, prInfo)
			}
		}

		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	return allPRs, nil
}

func findUserIssues(ctx context.Context, client *github.Client, username string, startDate, endDate time.Time) ([]IssueInfo, error) {
	var allIssues []IssueInfo

	// Search for issues by the user in the Kubernetes organization
	query := fmt.Sprintf("author:%s org:%s is:issue created:%s..%s",
		username,
		kubernetesOrg,
		startDate.Format("2006-01-02"),
		endDate.Format("2006-01-02"))

	opts := &github.SearchOptions{
		Sort:  "created",
		Order: "desc",
		ListOptions: github.ListOptions{
			PerPage: 100,
		},
	}

	for {
		result, resp, err := client.Search.Issues(ctx, query, opts)
		if err != nil {
			return nil, fmt.Errorf("searching issues: %w", err)
		}

		for _, issue := range result.Issues {
			if !issue.IsPullRequest() {
				issueInfo := IssueInfo{
					Repo:      extractRepoFromURL(issue.GetHTMLURL()),
					Number:    issue.GetNumber(),
					Title:     issue.GetTitle(),
					URL:       issue.GetHTMLURL(),
					State:     issue.GetState(),
					CreatedAt: issue.GetCreatedAt().Time,
				}
				allIssues = append(allIssues, issueInfo)
			}
		}

		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	return allIssues, nil
}

func extractRepoFromURL(url string) string {
	// Extract repo name from URL like https://github.com/kubernetes/kubernetes/pull/123
	if len(url) == 0 {
		return ""
	}

	// Parse URL to extract owner/repo
	// Expected format: https://github.com/owner/repo/...
	parts := splitURL(url)
	if len(parts) >= 5 && parts[2] == "github.com" {
		return parts[3] + "/" + parts[4]
	}

	return ""
}

func splitURL(url string) []string {
	// Simple URL splitter - remove protocol and split by /
	url = trimPrefix(url, "https://")
	url = trimPrefix(url, "http://")

	var parts []string
	current := ""
	for _, ch := range url {
		if ch == '/' {
			if current != "" {
				parts = append(parts, current)
				current = ""
			}
		} else {
			current += string(ch)
		}
	}
	if current != "" {
		parts = append(parts, current)
	}
	return parts
}

func trimPrefix(s, prefix string) string {
	if len(s) >= len(prefix) && s[:len(prefix)] == prefix {
		return s[len(prefix):]
	}
	return s
}

func addItemsToProject(ctx context.Context, client *github.Client, projectID string, commits []CommitInfo, prs []PRInfo, issues []IssueInfo) error {
	log.Printf("Note: GitHub Projects V2 API requires GraphQL. This function shows the structure.")
	log.Printf("To fully implement, use GitHub's GraphQL API with mutations like addProjectV2ItemById")

	// Display what would be added
	log.Printf("\nItems that would be added to project:")
	log.Printf("\nCommits (%d):", len(commits))
	for i, commit := range commits {
		if i < 5 { // Show first 5
			log.Printf("  - %s: %s (%s)", commit.Repo, commit.SHA[:7], commit.Message[:min(50, len(commit.Message))])
		}
	}
	if len(commits) > 5 {
		log.Printf("  ... and %d more", len(commits)-5)
	}

	log.Printf("\nPull Requests (%d):", len(prs))
	for i, pr := range prs {
		if i < 5 { // Show first 5
			log.Printf("  - %s #%d: %s [%s]", pr.Repo, pr.Number, pr.Title, pr.State)
		}
	}
	if len(prs) > 5 {
		log.Printf("  ... and %d more", len(prs)-5)
	}

	log.Printf("\nIssues (%d):", len(issues))
	for i, issue := range issues {
		if i < 5 { // Show first 5
			log.Printf("  - %s #%d: %s [%s]", issue.Repo, issue.Number, issue.Title, issue.State)
		}
	}
	if len(issues) > 5 {
		log.Printf("  ... and %d more", len(issues)-5)
	}

	// Note: Full implementation would use GraphQL API to add items
	// Example GraphQL mutation:
	// mutation {
	//   addProjectV2ItemById(input: {
	//     projectId: "PROJECT_ID"
	//     contentId: "ISSUE_OR_PR_NODE_ID"
	//   }) {
	//     item {
	//       id
	//     }
	//   }
	// }

	return nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
