package main

import (
	"fmt"
	"log"
	"strings"
)

// ProjectItem is the unified representation of an issue, PR, or draft issue
// found on one or more GitHub project boards in the org.
type ProjectItem struct {
	NodeID       string            `json:"node_id"`
	Type         string            `json:"type"` // "Issue", "PullRequest", "DraftIssue"
	Number       int               `json:"number"`
	Title        string            `json:"title"`
	URL          string            `json:"url"`
	State        string            `json:"state"` // "OPEN", "CLOSED", "MERGED"
	Repo         string            `json:"repo"`  // "owner/name"
	Author       string            `json:"author,omitempty"`
	Milestone    string            `json:"milestone,omitempty"`
	Labels       []string          `json:"labels,omitempty"`
	Assignees    []string          `json:"assignees,omitempty"`
	ProjectTitle string            `json:"project_title,omitempty"`
	Fields       map[string]string `json:"fields,omitempty"` // custom field values from the source project
}

// queryItems fetches items from GitHub projects in the configured organization.
// If GITHUB_REPOS is set, only items from those repos are included.
// Otherwise, all projects in the org are discovered and queried.
func queryItems(config Config) ([]ProjectItem, error) {
	gql := newGraphQLClient(config.GitHubToken)

	// Step 1: discover projects
	var projects []projectRef
	var err error
	if len(config.Repos) > 0 {
		// GITHUB_REPOS filters which repos items come from.
		// We still query org-level projects and filter items by repo.
		projects, err = listOrgProjects(gql, config.Org)
	} else {
		projects, err = listOrgProjects(gql, config.Org)
	}
	if err != nil {
		return nil, fmt.Errorf("listing org projects: %w", err)
	}

	log.Printf("Found %d project(s) in org %s", len(projects), config.Org)

	// Step 2: for each project, fetch items with field values
	var allItems []ProjectItem
	seen := make(map[string]bool) // dedup by nodeID

	for _, proj := range projects {
		log.Printf("  Querying project #%d: %s", proj.Number, proj.Title)

		items, err := fetchProjectItems(gql, proj)
		if err != nil {
			log.Printf("  Warning: error querying project #%d: %v", proj.Number, err)
			continue
		}

		for _, item := range items {
			// Filter by repo if GITHUB_REPOS is set
			if len(config.Repos) > 0 && !matchesRepo(item.Repo, config.Repos) {
				continue
			}

			// Filter by milestone if set
			if config.Milestone != "" && item.Milestone != "" && item.Milestone != config.Milestone {
				continue
			}

			if !seen[item.NodeID] {
				seen[item.NodeID] = true
				allItems = append(allItems, item)
			}
		}

		log.Printf("    Got %d items (total unique so far: %d)", len(items), len(allItems))
	}

	return allItems, nil
}

type projectRef struct {
	ID     string
	Number int
	Title  string
}

// listOrgProjects pages through all projects in an organization.
func listOrgProjects(gql *graphqlClient, org string) ([]projectRef, error) {
	query := `query($org: String!, $cursor: String) {
		organization(login: $org) {
			projectsV2(first: 100, after: $cursor) {
				nodes {
					id
					number
					title
				}
				pageInfo { hasNextPage endCursor }
			}
		}
	}`

	var projects []projectRef
	var cursor *string

	for {
		vars := map[string]any{"org": org}
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
			projects = append(projects, projectRef{
				ID:     p.ID,
				Number: p.Number,
				Title:  p.Title,
			})
		}

		if !result.Organization.ProjectsV2.PageInfo.HasNextPage {
			break
		}
		c := result.Organization.ProjectsV2.PageInfo.EndCursor
		cursor = &c
	}

	return projects, nil
}

// fetchProjectItems pages through all items in a project, extracting content
// metadata and custom field values.
func fetchProjectItems(gql *graphqlClient, proj projectRef) ([]ProjectItem, error) {
	query := `query($projectId: ID!, $cursor: String) {
		node(id: $projectId) {
			... on ProjectV2 {
				items(first: 100, after: $cursor) {
					nodes {
						id
						fieldValues(first: 20) {
							nodes {
								... on ProjectV2ItemFieldSingleSelectValue {
									name
									field { ... on ProjectV2FieldCommon { name } }
								}
								... on ProjectV2ItemFieldTextValue {
									text
									field { ... on ProjectV2FieldCommon { name } }
								}
								... on ProjectV2ItemFieldDateValue {
									date
									field { ... on ProjectV2FieldCommon { name } }
								}
								... on ProjectV2ItemFieldNumberValue {
									number
									field { ... on ProjectV2FieldCommon { name } }
								}
								... on ProjectV2ItemFieldIterationValue {
									title
									field { ... on ProjectV2FieldCommon { name } }
								}
							}
						}
						content {
							__typename
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
							... on DraftIssue {
								id
								title
								assignees(first: 20) { nodes { login } }
							}
						}
					}
					pageInfo { hasNextPage endCursor }
				}
			}
		}
	}`

	var items []ProjectItem
	var cursor *string

	for {
		vars := map[string]any{"projectId": proj.ID}
		if cursor != nil {
			vars["cursor"] = *cursor
		}

		var result struct {
			Node struct {
				Items struct {
					Nodes []struct {
						ID          string `json:"id"`
						FieldValues struct {
							Nodes []fieldValueNode `json:"nodes"`
						} `json:"fieldValues"`
						Content contentNode `json:"content"`
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

		for _, node := range result.Node.Items.Nodes {
			item := buildProjectItem(node.Content, node.FieldValues.Nodes, proj.Title)
			if item.Title != "" { // skip REDACTED or empty items
				items = append(items, item)
			}
		}

		if !result.Node.Items.PageInfo.HasNextPage {
			break
		}
		c := result.Node.Items.PageInfo.EndCursor
		cursor = &c
	}

	return items, nil
}

// --- GraphQL response types ---

type fieldValueNode struct {
	// Single select
	Name string `json:"name,omitempty"`
	// Text
	Text string `json:"text,omitempty"`
	// Date
	Date string `json:"date,omitempty"`
	// Number
	Number float64 `json:"number,omitempty"`
	// Iteration
	Title string `json:"title,omitempty"`
	// Common field reference
	Field struct {
		Name string `json:"name"`
	} `json:"field"`
}

type contentNode struct {
	TypeName string `json:"__typename"`

	// Shared fields across Issue / PullRequest
	ID     string `json:"id"`
	Number int    `json:"number"`
	Title  string `json:"title"`
	URL    string `json:"url"`
	State  string `json:"state"`

	Author *struct {
		Login string `json:"login"`
	} `json:"author"`

	Milestone *struct {
		Title string `json:"title"`
	} `json:"milestone"`

	Labels *struct {
		Nodes []struct {
			Name string `json:"name"`
		} `json:"nodes"`
	} `json:"labels"`

	Assignees *struct {
		Nodes []struct {
			Login string `json:"login"`
		} `json:"nodes"`
	} `json:"assignees"`

	Repository *struct {
		NameWithOwner string `json:"nameWithOwner"`
	} `json:"repository"`
}

func buildProjectItem(content contentNode, fieldValues []fieldValueNode, projectTitle string) ProjectItem {
	item := ProjectItem{
		NodeID:       content.ID,
		Type:         content.TypeName,
		Number:       content.Number,
		Title:        content.Title,
		URL:          content.URL,
		State:        content.State,
		ProjectTitle: projectTitle,
		Fields:       make(map[string]string),
	}

	if content.Repository != nil {
		item.Repo = content.Repository.NameWithOwner
	}
	if content.Author != nil {
		item.Author = content.Author.Login
	}
	if content.Milestone != nil {
		item.Milestone = content.Milestone.Title
	}
	if content.Labels != nil {
		for _, l := range content.Labels.Nodes {
			item.Labels = append(item.Labels, l.Name)
		}
	}
	if content.Assignees != nil {
		for _, a := range content.Assignees.Nodes {
			item.Assignees = append(item.Assignees, a.Login)
		}
	}

	// Extract custom field values
	for _, fv := range fieldValues {
		fieldName := fv.Field.Name
		if fieldName == "" {
			continue
		}
		// Pick the non-empty value
		switch {
		case fv.Name != "":
			item.Fields[fieldName] = fv.Name
		case fv.Text != "":
			item.Fields[fieldName] = fv.Text
		case fv.Date != "":
			item.Fields[fieldName] = fv.Date
		case fv.Number != 0:
			item.Fields[fieldName] = fmt.Sprintf("%.0f", fv.Number)
		case fv.Title != "":
			item.Fields[fieldName] = fv.Title
		}
	}

	return item
}

// matchesRepo checks if an item's repo matches one of the configured project repos.
func matchesRepo(repo string, projects []string) bool {
	if repo == "" {
		return true // draft issues have no repo, include them
	}
	for _, p := range projects {
		if strings.EqualFold(repo, p) {
			return true
		}
	}
	return false
}
