package main

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
)

// --- Project Board Info ---

type ProjectInfo struct {
	ID     string
	Number int
	Title  string
	URL    string
}

// updateBoard creates or updates a GitHub Projects V2 board with the given items.
func updateBoard(config Config, items []ProjectItem) error {
	gql := newGraphQLClient(config.GitHubToken)

	log.Printf("Board name: %q", config.BoardName)
	log.Printf("Board owner: %s", config.BoardOwner)

	// Find or create the project
	project, err := findProject(gql, config.BoardOwner, config.BoardName)
	if err != nil {
		return fmt.Errorf("searching for project: %w", err)
	}

	if project == nil {
		log.Printf("Project %q not found, creating...", config.BoardName)
		project, err = createProject(gql, config.BoardOwner, config.BoardName)
		if err != nil {
			return fmt.Errorf("creating project: %w", err)
		}
		log.Printf("Created project: %s", project.URL)
	} else {
		log.Printf("Found existing project: %s", project.URL)
	}

	// Add items to the board
	log.Printf("Adding %d item(s) to project board...", len(items))
	added, skipped, err := addItemsToProject(gql, project.ID, items)
	if err != nil {
		return fmt.Errorf("adding items: %w", err)
	}
	log.Printf("Done: %d added, %d skipped (already present or error)", added, skipped)

	// Link repos if configured
	if len(config.LinkRepos) > 0 {
		log.Printf("Linking project to %d repository(ies)...", len(config.LinkRepos))
		linked, linkSkipped, err := linkProjectToRepositories(gql, project.ID, config.LinkRepos)
		if err != nil {
			log.Printf("Warning: error linking repositories: %v", err)
		} else {
			log.Printf("Done: %d linked, %d skipped (already linked or error)", linked, linkSkipped)
		}
	}

	// Optionally remove stale items
	if config.Sync {
		log.Printf("Syncing: removing stale items not in current query...")
		removed, err := removeStaleItems(gql, project.ID, items)
		if err != nil {
			log.Printf("Warning: error removing stale items: %v", err)
		} else {
			log.Printf("Removed %d stale item(s)", removed)
		}
	}

	fmt.Printf("\nProject board: %s\n", project.URL)
	return nil
}

// --- Find Project ---

func findProject(gql *graphqlClient, boardOwner, title string) (*ProjectInfo, error) {
	proj, err := findUserProject(gql, boardOwner, title)
	if err == nil && proj != nil {
		return proj, nil
	}

	proj, err = findOrgProject(gql, boardOwner, title)
	if err == nil && proj != nil {
		return proj, nil
	}

	return nil, nil
}

func findUserProject(gql *graphqlClient, owner, title string) (*ProjectInfo, error) {
	query := `query($owner: String!, $cursor: String) {
		user(login: $owner) {
			projectsV2(first: 100, after: $cursor) {
				nodes { id number title url }
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

		err := gql.do(graphqlRequest{Query: query, Variables: vars}, &result)
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

func findOrgProject(gql *graphqlClient, owner, title string) (*ProjectInfo, error) {
	query := `query($owner: String!, $cursor: String) {
		organization(login: $owner) {
			projectsV2(first: 100, after: $cursor) {
				nodes { id number title url }
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

		err := gql.do(graphqlRequest{Query: query, Variables: vars}, &result)
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

// --- Create Project ---

func createProject(gql *graphqlClient, boardOwner, title string) (*ProjectInfo, error) {
	ownerID, err := resolveOwnerNodeID(gql, boardOwner)
	if err != nil {
		return nil, fmt.Errorf("resolving owner node ID: %w", err)
	}

	mutation := `mutation($ownerId: ID!, $title: String!) {
		createProjectV2(input: {ownerId: $ownerId, title: $title}) {
			projectV2 { id number title url }
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

	err = gql.do(graphqlRequest{
		Query:     mutation,
		Variables: map[string]any{"ownerId": ownerID, "title": title},
	}, &result)
	if err != nil {
		return nil, err
	}

	p := result.CreateProjectV2.ProjectV2
	return &ProjectInfo{ID: p.ID, Number: p.Number, Title: p.Title, URL: p.URL}, nil
}

func resolveOwnerNodeID(gql *graphqlClient, login string) (string, error) {
	query := `query($login: String!) { user(login: $login) { id } }`
	var userResult struct {
		User struct {
			ID string `json:"id"`
		} `json:"user"`
	}
	err := gql.do(graphqlRequest{Query: query, Variables: map[string]any{"login": login}}, &userResult)
	if err == nil && userResult.User.ID != "" {
		return userResult.User.ID, nil
	}

	query = `query($login: String!) { organization(login: $login) { id } }`
	var orgResult struct {
		Organization struct {
			ID string `json:"id"`
		} `json:"organization"`
	}
	err = gql.do(graphqlRequest{Query: query, Variables: map[string]any{"login": login}}, &orgResult)
	if err == nil && orgResult.Organization.ID != "" {
		return orgResult.Organization.ID, nil
	}

	return "", fmt.Errorf("could not resolve node ID for %q", login)
}

// --- Add Items ---

func addItemsToProject(gql *graphqlClient, projectID string, items []ProjectItem) (added, skipped int, err error) {
	existingIDs, err := getProjectItemContentIDs(gql, projectID)
	if err != nil {
		log.Printf("Warning: could not check existing items: %v", err)
		existingIDs = make(map[string]bool)
	}

	mutation := `mutation($projectId: ID!, $contentId: ID!) {
		addProjectV2ItemById(input: {projectId: $projectId, contentId: $contentId}) {
			item { id }
		}
	}`

	for _, item := range items {
		if item.NodeID == "" {
			log.Printf("  Skipping %q (no node ID)", item.Title)
			skipped++
			continue
		}

		if existingIDs[item.NodeID] {
			log.Printf("  #%d already on board, skipping", item.Number)
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

		err := gql.do(graphqlRequest{
			Query:     mutation,
			Variables: map[string]any{"projectId": projectID, "contentId": item.NodeID},
		}, &result)
		if err != nil {
			log.Printf("  Error adding #%d: %v", item.Number, err)
			skipped++
			continue
		}

		log.Printf("  Added #%d: %s", item.Number, item.Title)
		added++
	}

	return added, skipped, nil
}

func getProjectItemContentIDs(gql *graphqlClient, projectID string) (map[string]bool, error) {
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

		err := gql.do(graphqlRequest{Query: query, Variables: vars}, &result)
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

// --- Remove Stale Items ---

func removeStaleItems(gql *graphqlClient, projectID string, currentItems []ProjectItem) (int, error) {
	currentIDs := make(map[string]bool, len(currentItems))
	for _, item := range currentItems {
		if item.NodeID != "" {
			currentIDs[item.NodeID] = true
		}
	}

	items, err := getProjectItems(gql, projectID)
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
		if item.contentID != "" && !currentIDs[item.contentID] {
			var result json.RawMessage
			err := gql.do(graphqlRequest{
				Query:     mutation,
				Variables: map[string]any{"projectId": projectID, "itemId": item.itemID},
			}, &result)
			if err != nil {
				log.Printf("  Error removing stale item %s: %v", item.itemID, err)
				continue
			}
			log.Printf("  Removed stale item: %s", item.title)
			removed++
		}
	}

	return removed, nil
}

type boardItem struct {
	itemID    string
	contentID string
	title     string
}

func getProjectItems(gql *graphqlClient, projectID string) ([]boardItem, error) {
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

	var items []boardItem
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

		err := gql.do(graphqlRequest{Query: query, Variables: vars}, &result)
		if err != nil {
			return nil, err
		}

		for _, n := range result.Node.Items.Nodes {
			items = append(items, boardItem{
				itemID:    n.ID,
				contentID: n.Content.ID,
				title:     n.Content.Title,
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

// --- Link Repos ---

func linkProjectToRepositories(gql *graphqlClient, projectID string, repos []string) (linked, skipped int, err error) {
	for _, repo := range repos {
		parts := strings.SplitN(repo, "/", 2)
		if len(parts) != 2 {
			log.Printf("  Skipping invalid repo %q (expected owner/name)", repo)
			skipped++
			continue
		}
		owner, name := parts[0], parts[1]

		repoID, err := resolveRepoNodeID(gql, owner, name)
		if err != nil {
			log.Printf("  Error resolving repo %s: %v", repo, err)
			skipped++
			continue
		}

		mutation := `mutation($projectId: ID!, $repositoryId: ID!) {
			linkProjectV2ToRepository(input: {projectId: $projectId, repositoryId: $repositoryId}) {
				repository { id }
			}
		}`

		var result json.RawMessage
		linkErr := gql.do(graphqlRequest{
			Query:     mutation,
			Variables: map[string]any{"projectId": projectID, "repositoryId": repoID},
		}, &result)
		if linkErr != nil {
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

func resolveRepoNodeID(gql *graphqlClient, owner, name string) (string, error) {
	query := `query($owner: String!, $name: String!) {
		repository(owner: $owner, name: $name) { id }
	}`

	var result struct {
		Repository struct {
			ID string `json:"id"`
		} `json:"repository"`
	}

	err := gql.do(graphqlRequest{
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
