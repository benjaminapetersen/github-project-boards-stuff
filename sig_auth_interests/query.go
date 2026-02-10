package main

import (
	"fmt"
	"log"
	"strings"
)

// ProjectItem is the unified representation of an issue or PR found via
// repository search. This tool does NOT query project boards, so there are
// no custom field values (Status, Stage, PRR). Use the sibling tool
// sig_auth_interested_projects for board-level field data.
type ProjectItem struct {
	NodeID    string            `json:"node_id"`
	Type      string            `json:"type"` // "Issue" or "PullRequest"
	Number    int               `json:"number"`
	Title     string            `json:"title"`
	URL       string            `json:"url"`
	State     string            `json:"state"` // "OPEN", "CLOSED", "MERGED"
	Repo      string            `json:"repo"`  // "owner/name"
	Author    string            `json:"author,omitempty"`
	Milestone string            `json:"milestone,omitempty"`
	Labels    []string          `json:"labels,omitempty"`
	Assignees []string          `json:"assignees,omitempty"`
	Fields    map[string]string `json:"fields,omitempty"` // empty for repo-search; kept for struct compat
}

// queryItems searches GitHub for issues and PRs matching the configured
// repos, labels, milestone, and state filters — all server-side.
// This is dramatically cheaper than the project-board approach because
// GitHub's search API handles filtering before returning results.
func queryItems(config Config) ([]ProjectItem, error) {
	gql := newGraphQLClient(config.GitHubToken)

	// Build search queries. We issue one search per repo × label combination
	// to keep results focused and avoid GitHub's 1000-result search limit.
	queries := buildSearchQueries(config)

	log.Printf("Will execute %d search query(ies)", len(queries))

	var allItems []ProjectItem
	seen := make(map[string]bool) // dedup by node ID

	for i, q := range queries {
		log.Printf("  Search [%d/%d]: %s", i+1, len(queries), q)

		items, err := executeSearch(gql, q)
		if err != nil {
			log.Printf("  Warning: search error: %v", err)
			continue
		}

		for _, item := range items {
			if !seen[item.NodeID] {
				seen[item.NodeID] = true
				allItems = append(allItems, item)
			}
		}

		log.Printf("    Got %d results (total unique so far: %d)", len(items), len(allItems))
	}

	return allItems, nil
}

// buildSearchQueries constructs GitHub search query strings from config.
// Each query includes repo, label, state, and milestone qualifiers.
//
// Example output:
//
//	"repo:kubernetes/kubernetes label:sig/auth is:open milestone:v1.36"
func buildSearchQueries(config Config) []string {
	// Base qualifiers common to all queries
	var base []string

	// State: default to "is:open" unless overridden
	if len(config.States) > 0 {
		for _, s := range config.States {
			base = append(base, fmt.Sprintf("is:%s", strings.ToLower(s)))
		}
	} else {
		base = append(base, "is:open")
	}

	// Milestone
	if config.Milestone != "" {
		base = append(base, fmt.Sprintf("milestone:%q", config.Milestone))
	}

	// Exclude labels (server-side)
	for _, l := range config.ExcludeLabels {
		base = append(base, fmt.Sprintf("-label:%q", l))
	}

	baseStr := strings.Join(base, " ")

	// Build one query per (repo, label) pair for best coverage.
	// If no labels specified, query each repo without label filter.
	// If no repos specified, query each label org-wide.
	var queries []string

	repos := config.Repos
	if len(repos) == 0 {
		// Use org qualifier when no specific repos
		repos = []string{""} // sentinel: will use "org:" instead of "repo:"
	}

	labels := config.Labels
	if len(labels) == 0 {
		labels = []string{""} // sentinel: no label filter
	}

	for _, repo := range repos {
		for _, label := range labels {
			var parts []string

			if repo != "" {
				parts = append(parts, fmt.Sprintf("repo:%s", repo))
			} else {
				parts = append(parts, fmt.Sprintf("org:%s", config.Org))
			}

			if label != "" {
				parts = append(parts, fmt.Sprintf("label:%q", label))
			}

			parts = append(parts, baseStr)
			queries = append(queries, strings.Join(parts, " "))
		}
	}

	return queries
}

// executeSearch runs a single GitHub search query via GraphQL and pages
// through all results.
func executeSearch(gql *graphqlClient, searchQuery string) ([]ProjectItem, error) {
	query := `query($q: String!, $cursor: String) {
		search(query: $q, type: ISSUE, first: 100, after: $cursor) {
			issueCount
			nodes {
				... on Issue {
					id
					number
					title
					url
					state
					author { login }
					milestone { title }
					labels(first: 20) { nodes { name } }
					assignees(first: 20) { nodes { login } }
					repository { nameWithOwner }
				}
				... on PullRequest {
					id
					number
					title
					url
					state
					author { login }
					milestone { title }
					labels(first: 20) { nodes { name } }
					assignees(first: 20) { nodes { login } }
					repository { nameWithOwner }
				}
			}
			pageInfo { hasNextPage endCursor }
		}
	}`

	var items []ProjectItem
	var cursor *string
	pageNum := 0

	for {
		pageNum++
		vars := map[string]any{"q": searchQuery}
		if cursor != nil {
			vars["cursor"] = *cursor
		}

		var result struct {
			Search struct {
				IssueCount int `json:"issueCount"`
				Nodes      []searchNode
				PageInfo   struct {
					HasNextPage bool   `json:"hasNextPage"`
					EndCursor   string `json:"endCursor"`
				} `json:"pageInfo"`
			} `json:"search"`
		}

		err := gql.do(graphqlRequest{Query: query, Variables: vars}, &result)
		if err != nil {
			return nil, fmt.Errorf("search page %d: %w", pageNum, err)
		}

		if pageNum == 1 {
			log.Printf("    Total matches reported by GitHub: %d", result.Search.IssueCount)
		}

		for _, node := range result.Search.Nodes {
			item := buildItemFromSearch(node)
			if item.NodeID != "" {
				items = append(items, item)
			}
		}

		if !result.Search.PageInfo.HasNextPage {
			break
		}

		// GitHub search caps at 1000 results. Warn if we're approaching that.
		if len(items) >= 900 {
			log.Printf("    Warning: approaching GitHub's 1000-result search limit (%d so far)", len(items))
		}

		c := result.Search.PageInfo.EndCursor
		cursor = &c
	}

	return items, nil
}

// searchNode represents the union of Issue and PullRequest fields returned
// by the search query. GraphQL inline fragments means both types populate
// the same fields.
type searchNode struct {
	ID     string `json:"id"`
	Number int    `json:"number"`
	Title  string `json:"title"`
	URL    string `json:"url"`
	State  string `json:"state"` // "OPEN", "CLOSED", "MERGED"
	Author *struct {
		Login string `json:"login"`
	} `json:"author"`
	Milestone *struct {
		Title string `json:"title"`
	} `json:"milestone"`
	Labels struct {
		Nodes []struct {
			Name string `json:"name"`
		} `json:"nodes"`
	} `json:"labels"`
	Assignees struct {
		Nodes []struct {
			Login string `json:"login"`
		} `json:"nodes"`
	} `json:"assignees"`
	Repository *struct {
		NameWithOwner string `json:"nameWithOwner"`
	} `json:"repository"`
}

func buildItemFromSearch(node searchNode) ProjectItem {
	item := ProjectItem{
		NodeID: node.ID,
		Number: node.Number,
		Title:  node.Title,
		URL:    node.URL,
		State:  node.State,
		Fields: make(map[string]string), // empty — no board fields in repo search
	}

	// Determine type from state (MERGED only applies to PRs)
	if node.State == "MERGED" {
		item.Type = "PullRequest"
	} else if strings.Contains(node.URL, "/pull/") {
		item.Type = "PullRequest"
	} else {
		item.Type = "Issue"
	}

	if node.Author != nil {
		item.Author = node.Author.Login
	}
	if node.Milestone != nil {
		item.Milestone = node.Milestone.Title
	}
	if node.Repository != nil {
		item.Repo = node.Repository.NameWithOwner
	}

	for _, l := range node.Labels.Nodes {
		item.Labels = append(item.Labels, l.Name)
	}
	for _, a := range node.Assignees.Nodes {
		item.Assignees = append(item.Assignees, a.Login)
	}

	return item
}
