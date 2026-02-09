package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/google/go-github/v57/github"
	"golang.org/x/oauth2"
)

const graphqlEndpoint = "https://api.github.com/graphql"

// graphqlClient wraps an authenticated HTTP client for GitHub GraphQL calls.
type graphqlClient struct {
	httpClient *http.Client
	token      string
}

func newGraphQLClient(ctx context.Context, token string) *graphqlClient {
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	tc := oauth2.NewClient(ctx, ts)
	return &graphqlClient{httpClient: tc, token: token}
}

type graphqlRequest struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables,omitempty"`
}

type graphqlResponse struct {
	Data   json.RawMessage `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors,omitempty"`
}

func (c *graphqlClient) do(ctx context.Context, req graphqlRequest, result any) error {
	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal graphql request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", graphqlEndpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("graphql request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode == http.StatusTooManyRequests {
		retryAfter := resp.Header.Get("Retry-After")
		return &RateLimitError{
			StatusCode: resp.StatusCode,
			RetryAfter: retryAfter,
			Body:       string(respBody),
		}
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("graphql HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var gqlResp graphqlResponse
	if err := json.Unmarshal(respBody, &gqlResp); err != nil {
		return fmt.Errorf("unmarshal response: %w", err)
	}

	if len(gqlResp.Errors) > 0 {
		msgs := make([]string, len(gqlResp.Errors))
		for i, e := range gqlResp.Errors {
			msgs[i] = e.Message
		}
		return fmt.Errorf("graphql errors: %s", strings.Join(msgs, "; "))
	}

	if result != nil {
		if err := json.Unmarshal(gqlResp.Data, result); err != nil {
			return fmt.Errorf("unmarshal data: %w", err)
		}
	}

	return nil
}

// ---------- Project Board Operations ----------

// ProjectInfo holds the basic info for a GitHub Projects V2 project.
type ProjectInfo struct {
	ID     string
	Number int
	Title  string
	URL    string
}

// findProject searches the user's or org's projects for one matching the given title.
func findProject(ctx context.Context, gql *graphqlClient, boardOwner, title string) (*ProjectInfo, error) {
	// Try as a user first, then as an org
	proj, err := findUserProject(ctx, gql, boardOwner, title)
	if err == nil && proj != nil {
		return proj, nil
	}

	proj, err = findOrgProject(ctx, gql, boardOwner, title)
	if err == nil && proj != nil {
		return proj, nil
	}

	return nil, nil // not found
}

func findUserProject(ctx context.Context, gql *graphqlClient, owner, title string) (*ProjectInfo, error) {
	query := `query($owner: String!, $cursor: String) {
		user(login: $owner) {
			projectsV2(first: 100, after: $cursor) {
				nodes {
					id
					number
					title
					url
				}
				pageInfo { hasNextPage endCursor }
			}
		}
	}`

	var cursor *string
	for {
		vars := map[string]any{"owner": owner}
		if cursor != nil {
			vars["cursor"] = *cursor
		}

		var result struct {
			User struct {
				ProjectsV2 struct {
					Nodes []struct {
						ID     string `json:"id"`
						Number int    `json:"number"`
						Title  string `json:"title"`
						URL    string `json:"url"`
					} `json:"nodes"`
					PageInfo struct {
						HasNextPage bool   `json:"hasNextPage"`
						EndCursor   string `json:"endCursor"`
					} `json:"pageInfo"`
				} `json:"projectsV2"`
			} `json:"user"`
		}

		err := gql.do(ctx, graphqlRequest{Query: query, Variables: vars}, &result)
		if err != nil {
			return nil, err
		}

		for _, p := range result.User.ProjectsV2.Nodes {
			if p.Title == title {
				return &ProjectInfo{ID: p.ID, Number: p.Number, Title: p.Title, URL: p.URL}, nil
			}
		}

		if !result.User.ProjectsV2.PageInfo.HasNextPage {
			break
		}
		c := result.User.ProjectsV2.PageInfo.EndCursor
		cursor = &c
	}

	return nil, nil
}

func findOrgProject(ctx context.Context, gql *graphqlClient, owner, title string) (*ProjectInfo, error) {
	query := `query($owner: String!, $cursor: String) {
		organization(login: $owner) {
			projectsV2(first: 100, after: $cursor) {
				nodes {
					id
					number
					title
					url
				}
				pageInfo { hasNextPage endCursor }
			}
		}
	}`

	var cursor *string
	for {
		vars := map[string]any{"owner": owner}
		if cursor != nil {
			vars["cursor"] = *cursor
		}

		var result struct {
			Organization struct {
				ProjectsV2 struct {
					Nodes []struct {
						ID     string `json:"id"`
						Number int    `json:"number"`
						Title  string `json:"title"`
						URL    string `json:"url"`
					} `json:"nodes"`
					PageInfo struct {
						HasNextPage bool   `json:"hasNextPage"`
						EndCursor   string `json:"endCursor"`
					} `json:"pageInfo"`
				} `json:"projectsV2"`
			} `json:"organization"`
		}

		err := gql.do(ctx, graphqlRequest{Query: query, Variables: vars}, &result)
		if err != nil {
			return nil, err
		}

		for _, p := range result.Organization.ProjectsV2.Nodes {
			if p.Title == title {
				return &ProjectInfo{ID: p.ID, Number: p.Number, Title: p.Title, URL: p.URL}, nil
			}
		}

		if !result.Organization.ProjectsV2.PageInfo.HasNextPage {
			break
		}
		c := result.Organization.ProjectsV2.PageInfo.EndCursor
		cursor = &c
	}

	return nil, nil
}

// createProject creates a new GitHub Projects V2 project owned by the given user or org.
func createProject(ctx context.Context, gql *graphqlClient, boardOwner, title string) (*ProjectInfo, error) {
	// First resolve the owner's node ID
	ownerID, err := resolveOwnerNodeID(ctx, gql, boardOwner)
	if err != nil {
		return nil, fmt.Errorf("resolving owner node ID: %w", err)
	}

	mutation := `mutation($ownerId: ID!, $title: String!) {
		createProjectV2(input: {ownerId: $ownerId, title: $title}) {
			projectV2 {
				id
				number
				title
				url
			}
		}
	}`

	var result struct {
		CreateProjectV2 struct {
			ProjectV2 struct {
				ID     string `json:"id"`
				Number int    `json:"number"`
				Title  string `json:"title"`
				URL    string `json:"url"`
			} `json:"projectV2"`
		} `json:"createProjectV2"`
	}

	err = gql.do(ctx, graphqlRequest{
		Query:     mutation,
		Variables: map[string]any{"ownerId": ownerID, "title": title},
	}, &result)
	if err != nil {
		return nil, fmt.Errorf("creating project: %w", err)
	}

	p := result.CreateProjectV2.ProjectV2
	return &ProjectInfo{ID: p.ID, Number: p.Number, Title: p.Title, URL: p.URL}, nil
}

func resolveOwnerNodeID(ctx context.Context, gql *graphqlClient, login string) (string, error) {
	// Try user first
	query := `query($login: String!) {
		user(login: $login) { id }
	}`
	var userResult struct {
		User struct {
			ID string `json:"id"`
		} `json:"user"`
	}
	err := gql.do(ctx, graphqlRequest{Query: query, Variables: map[string]any{"login": login}}, &userResult)
	if err == nil && userResult.User.ID != "" {
		return userResult.User.ID, nil
	}

	// Try org
	query = `query($login: String!) {
		organization(login: $login) { id }
	}`
	var orgResult struct {
		Organization struct {
			ID string `json:"id"`
		} `json:"organization"`
	}
	err = gql.do(ctx, graphqlRequest{Query: query, Variables: map[string]any{"login": login}}, &orgResult)
	if err == nil && orgResult.Organization.ID != "" {
		return orgResult.Organization.ID, nil
	}

	return "", fmt.Errorf("could not resolve node ID for %q", login)
}

// addIssuesToProject adds issues (by their node IDs) to a project board.
// It skips issues that are already on the board.
func addIssuesToProject(ctx context.Context, gql *graphqlClient, projectID string, issues []*github.Issue) (added, skipped int, err error) {
	// Get existing item content IDs to avoid duplicates
	existingIDs, err := getProjectItemContentIDs(ctx, gql, projectID)
	if err != nil {
		log.Printf("Warning: could not check existing items (will attempt to add all): %v", err)
		existingIDs = make(map[string]bool)
	}

	mutation := `mutation($projectId: ID!, $contentId: ID!) {
		addProjectV2ItemById(input: {projectId: $projectId, contentId: $contentId}) {
			item { id }
		}
	}`

	for _, issue := range issues {
		nodeID := issue.GetNodeID()
		if nodeID == "" {
			log.Printf("  Skipping #%d (no node ID available)", issue.GetNumber())
			skipped++
			continue
		}

		if existingIDs[nodeID] {
			log.Printf("  #%d already on board, skipping", issue.GetNumber())
			skipped++
			continue
		}

		var result struct {
			AddProjectV2ItemById struct {
				Item struct {
					ID string `json:"id"`
				} `json:"item"`
			} `json:"addProjectV2ItemById"`
		}

		err := gql.do(ctx, graphqlRequest{
			Query:     mutation,
			Variables: map[string]any{"projectId": projectID, "contentId": nodeID},
		}, &result)
		if err != nil {
			log.Printf("  Error adding #%d: %v", issue.GetNumber(), err)
			skipped++
			continue
		}

		log.Printf("  Added #%d: %s", issue.GetNumber(), issue.GetTitle())
		added++
	}

	return added, skipped, nil
}

// getProjectItemContentIDs returns the set of content node IDs already in the project.
func getProjectItemContentIDs(ctx context.Context, gql *graphqlClient, projectID string) (map[string]bool, error) {
	query := `query($projectId: ID!, $cursor: String) {
		node(id: $projectId) {
			... on ProjectV2 {
				items(first: 100, after: $cursor) {
					nodes {
						content {
							... on Issue { id }
							... on PullRequest { id }
						}
					}
					pageInfo { hasNextPage endCursor }
				}
			}
		}
	}`

	ids := make(map[string]bool)
	var cursor *string

	for {
		vars := map[string]any{"projectId": projectID}
		if cursor != nil {
			vars["cursor"] = *cursor
		}

		var result struct {
			Node struct {
				Items struct {
					Nodes []struct {
						Content struct {
							ID string `json:"id"`
						} `json:"content"`
					} `json:"nodes"`
					PageInfo struct {
						HasNextPage bool   `json:"hasNextPage"`
						EndCursor   string `json:"endCursor"`
					} `json:"pageInfo"`
				} `json:"items"`
			} `json:"node"`
		}

		err := gql.do(ctx, graphqlRequest{Query: query, Variables: vars}, &result)
		if err != nil {
			return nil, err
		}

		for _, item := range result.Node.Items.Nodes {
			if item.Content.ID != "" {
				ids[item.Content.ID] = true
			}
		}

		if !result.Node.Items.PageInfo.HasNextPage {
			break
		}
		c := result.Node.Items.PageInfo.EndCursor
		cursor = &c
	}

	return ids, nil
}

// removeStaleItems removes items from the project board that are NOT in the current issue set.
func removeStaleItems(ctx context.Context, gql *graphqlClient, projectID string, currentIssues []*github.Issue) (int, error) {
	// Build set of current issue node IDs
	currentIDs := make(map[string]bool, len(currentIssues))
	for _, issue := range currentIssues {
		if nid := issue.GetNodeID(); nid != "" {
			currentIDs[nid] = true
		}
	}

	// Get all items on the board with their content IDs
	items, err := getProjectItems(ctx, gql, projectID)
	if err != nil {
		return 0, fmt.Errorf("listing project items: %w", err)
	}

	mutation := `mutation($projectId: ID!, $itemId: ID!) {
		deleteProjectV2Item(input: {projectId: $projectId, itemId: $itemId}) {
			deletedItemId
		}
	}`

	removed := 0
	for _, item := range items {
		if item.ContentID != "" && !currentIDs[item.ContentID] {
			var result json.RawMessage
			err := gql.do(ctx, graphqlRequest{
				Query:     mutation,
				Variables: map[string]any{"projectId": projectID, "itemId": item.ItemID},
			}, &result)
			if err != nil {
				log.Printf("  Error removing stale item %s: %v", item.ItemID, err)
				continue
			}
			log.Printf("  Removed stale item: %s", item.Title)
			removed++
		}
	}

	return removed, nil
}

type projectItem struct {
	ItemID    string
	ContentID string
	Title     string
}

func getProjectItems(ctx context.Context, gql *graphqlClient, projectID string) ([]projectItem, error) {
	query := `query($projectId: ID!, $cursor: String) {
		node(id: $projectId) {
			... on ProjectV2 {
				items(first: 100, after: $cursor) {
					nodes {
						id
						content {
							... on Issue { id title }
							... on PullRequest { id title }
							... on DraftIssue { id title }
						}
					}
					pageInfo { hasNextPage endCursor }
				}
			}
		}
	}`

	var items []projectItem
	var cursor *string

	for {
		vars := map[string]any{"projectId": projectID}
		if cursor != nil {
			vars["cursor"] = *cursor
		}

		var result struct {
			Node struct {
				Items struct {
					Nodes []struct {
						ID      string `json:"id"`
						Content struct {
							ID    string `json:"id"`
							Title string `json:"title"`
						} `json:"content"`
					} `json:"nodes"`
					PageInfo struct {
						HasNextPage bool   `json:"hasNextPage"`
						EndCursor   string `json:"endCursor"`
					} `json:"pageInfo"`
				} `json:"items"`
			} `json:"node"`
		}

		err := gql.do(ctx, graphqlRequest{Query: query, Variables: vars}, &result)
		if err != nil {
			return nil, err
		}

		for _, n := range result.Node.Items.Nodes {
			items = append(items, projectItem{
				ItemID:    n.ID,
				ContentID: n.Content.ID,
				Title:     n.Content.Title,
			})
		}

		if !result.Node.Items.PageInfo.HasNextPage {
			break
		}
		c := result.Node.Items.PageInfo.EndCursor
		cursor = &c
	}

	return items, nil
}

// generateBoardName creates a concise board title from the query parameters.
// Example: "k8s enhancements sig/auth v1.36"
func generateBoardName(config Config) string {
	parts := []string{"k8s", config.RepoName}
	parts = append(parts, config.Labels...)
	parts = append(parts, config.Milestones...)
	return strings.Join(parts, " ")
}

// linkProjectToRepositories links a project board to one or more repositories.
// Each repo should be in "owner/name" format.  Already-linked repos are silently skipped
// (GitHub returns an error that we detect and ignore).
func linkProjectToRepositories(ctx context.Context, gql *graphqlClient, projectID string, repos []string) (linked, skipped int, err error) {
	for _, repo := range repos {
		parts := strings.SplitN(repo, "/", 2)
		if len(parts) != 2 {
			log.Printf("  Skipping invalid repo %q (expected owner/name)", repo)
			skipped++
			continue
		}
		owner, name := parts[0], parts[1]

		// Resolve the repository node ID
		repoID, err := resolveRepoNodeID(ctx, gql, owner, name)
		if err != nil {
			log.Printf("  Error resolving repo %s: %v", repo, err)
			skipped++
			continue
		}

		// Link the project to the repository
		mutation := `mutation($projectId: ID!, $repositoryId: ID!) {
			linkProjectV2ToRepository(input: {projectId: $projectId, repositoryId: $repositoryId}) {
				repository { id }
			}
		}`

		var result json.RawMessage
		linkErr := gql.do(ctx, graphqlRequest{
			Query:     mutation,
			Variables: map[string]any{"projectId": projectID, "repositoryId": repoID},
		}, &result)
		if linkErr != nil {
			// GitHub returns an error if already linked; treat gracefully
			if strings.Contains(linkErr.Error(), "already linked") || strings.Contains(linkErr.Error(), "already exists") {
				log.Printf("  %s already linked, skipping", repo)
				skipped++
				continue
			}
			log.Printf("  Error linking %s: %v", repo, linkErr)
			skipped++
			continue
		}

		log.Printf("  Linked project to %s", repo)
		linked++
	}

	return linked, skipped, nil
}

// resolveRepoNodeID fetches the GraphQL node ID for a repository.
func resolveRepoNodeID(ctx context.Context, gql *graphqlClient, owner, name string) (string, error) {
	query := `query($owner: String!, $name: String!) {
		repository(owner: $owner, name: $name) { id }
	}`

	var result struct {
		Repository struct {
			ID string `json:"id"`
		} `json:"repository"`
	}

	err := gql.do(ctx, graphqlRequest{
		Query:     query,
		Variables: map[string]any{"owner": owner, "name": name},
	}, &result)
	if err != nil {
		return "", err
	}
	if result.Repository.ID == "" {
		return "", fmt.Errorf("repository %s/%s not found", owner, name)
	}
	return result.Repository.ID, nil
}
