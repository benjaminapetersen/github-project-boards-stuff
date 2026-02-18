package board

import (
	"encoding/json"
	"fmt"
	"log"

	"github.com/benjaminapetersen/github-project-boards-stuff/pkg/ghgql"
)

// ViewDef describes a GitHub Projects V2 view (tab).
type ViewDef struct {
	ID     string
	Name   string
	Number int
	Layout string // TABLE, BOARD, ROADMAP
	Filter string
}

// ViewConfig describes a desired view on the destination board.
type ViewConfig struct {
	Name       string   // View/tab name
	FieldNames []string // Field names that should be visible as columns
}

// ---------- List Views ----------

// ListViews returns all views on a project.
func ListViews(gql *ghgql.Client, projectID string) ([]ViewDef, error) {
	query := `query($projectId: ID!, $cursor: String) {
		node(id: $projectId) {
			... on ProjectV2 {
				views(first: 50, after: $cursor) {
					nodes {
						id
						name
						number
						layout
						filter
					}
					pageInfo { hasNextPage endCursor }
				}
			}
		}
	}`

	var views []ViewDef
	var cursor *string

	for {
		vars := map[string]any{"projectId": projectID}
		if cursor != nil {
			vars["cursor"] = *cursor
		}

		var result struct {
			Node struct {
				Views struct {
					Nodes []struct {
						ID     string `json:"id"`
						Name   string `json:"name"`
						Number int    `json:"number"`
						Layout string `json:"layout"`
						Filter string `json:"filter"`
					} `json:"nodes"`
					PageInfo struct {
						HasNextPage bool   `json:"hasNextPage"`
						EndCursor   string `json:"endCursor"`
					} `json:"pageInfo"`
				} `json:"views"`
			} `json:"node"`
		}

		err := gql.Do(ghgql.Request{Query: query, Variables: vars}, &result)
		if err != nil {
			return nil, err
		}

		for _, v := range result.Node.Views.Nodes {
			views = append(views, ViewDef{
				ID:     v.ID,
				Name:   v.Name,
				Number: v.Number,
				Layout: v.Layout,
				Filter: v.Filter,
			})
		}

		if !result.Node.Views.PageInfo.HasNextPage {
			break
		}
		c := result.Node.Views.PageInfo.EndCursor
		cursor = &c
	}

	return views, nil
}

// ---------- Create View ----------

// CreateView creates a new TABLE view on a project and returns its definition.
func CreateView(gql *ghgql.Client, projectID, name string) (*ViewDef, error) {
	// Step 1: Create the view (API doesn't accept name at creation time)
	mutation := `mutation($projectId: ID!) {
		createProjectV2View(input: {projectId: $projectId, layout: TABLE}) {
			projectV2View {
				id name number layout
			}
		}
	}`

	var createResult struct {
		CreateProjectV2View struct {
			ProjectV2View struct {
				ID     string `json:"id"`
				Name   string `json:"name"`
				Number int    `json:"number"`
				Layout string `json:"layout"`
			} `json:"projectV2View"`
		} `json:"createProjectV2View"`
	}

	err := gql.Do(ghgql.Request{
		Query:     mutation,
		Variables: map[string]any{"projectId": projectID},
	}, &createResult)
	if err != nil {
		return nil, fmt.Errorf("creating view: %w", err)
	}

	view := createResult.CreateProjectV2View.ProjectV2View

	// Step 2: Rename the view
	renameMutation := `mutation($projectId: ID!, $viewId: ID!, $name: String!) {
		updateProjectV2View(input: {projectId: $projectId, viewId: $viewId, name: $name}) {
			projectV2View {
				id name number layout
			}
		}
	}`

	var renameResult json.RawMessage
	err = gql.Do(ghgql.Request{
		Query:     renameMutation,
		Variables: map[string]any{"projectId": projectID, "viewId": view.ID, "name": name},
	}, &renameResult)
	if err != nil {
		log.Printf("Warning: created view but could not rename to %q: %v", name, err)
	}

	return &ViewDef{
		ID:     view.ID,
		Name:   name,
		Number: view.Number,
		Layout: view.Layout,
	}, nil
}

// ---------- Set View Visible Fields ----------

// SetViewVisibleFields sets which fields are visible as columns on a view.
// fieldIDs should be the project field node IDs to display.
func SetViewVisibleFields(gql *ghgql.Client, projectID, viewID string, fieldIDs []string) error {
	mutation := `mutation($projectId: ID!, $viewId: ID!, $fields: [ID!]!) {
		updateProjectV2View(input: {projectId: $projectId, viewId: $viewId, fields: $fields}) {
			projectV2View {
				id name
			}
		}
	}`

	var result json.RawMessage
	return gql.Do(ghgql.Request{
		Query: mutation,
		Variables: map[string]any{
			"projectId": projectID,
			"viewId":    viewID,
			"fields":    fieldIDs,
		},
	}, &result)
}

// ---------- Ensure Views ----------

// EnsureViews creates any missing views and sets visible columns on each.
// destFields provides the field name → ID mapping for resolving column visibility.
// If destFields is nil, views are created but columns are not configured.
func EnsureViews(gql *ghgql.Client, projectID string, desired []ViewConfig, destFields ...FieldMap) {
	existing, err := ListViews(gql, projectID)
	if err != nil {
		log.Printf("Warning: could not list project views: %v", err)
		return
	}

	// Resolve the optional FieldMap
	var fields FieldMap
	if len(destFields) > 0 && destFields[0] != nil {
		fields = destFields[0]
	}

	// Index existing views by name
	viewsByName := make(map[string]ViewDef, len(existing))
	for _, v := range existing {
		viewsByName[v.Name] = v
	}

	for _, want := range desired {
		view, exists := viewsByName[want.Name]
		if exists {
			log.Printf("  ✓ View %q already exists", want.Name)
		} else {
			log.Printf("  Creating view %q...", want.Name)
			created, err := CreateView(gql, projectID, want.Name)
			if err != nil {
				log.Printf("  ✗ Could not create view %q: %v", want.Name, err)
				continue
			}
			view = *created
			log.Printf("  ✓ Created view %q", want.Name)
		}

		// Set visible fields/columns
		if len(want.FieldNames) > 0 && fields != nil {
			fieldIDs := resolveFieldIDs(want.FieldNames, fields)
			if len(fieldIDs) > 0 {
				err := SetViewVisibleFields(gql, projectID, view.ID, fieldIDs)
				if err != nil {
					log.Printf("    Warning: could not set visible columns on %q: %v", want.Name, err)
					log.Printf("    You may need to manually add these columns: %v", want.FieldNames)
				} else {
					log.Printf("    Set %d visible columns on %q", len(fieldIDs), want.Name)
				}
			} else {
				log.Printf("    Warning: none of the requested fields %v were found on the board", want.FieldNames)
			}
		}
	}
}

// resolveFieldIDs maps field names to their project field IDs.
func resolveFieldIDs(names []string, fields FieldMap) []string {
	var ids []string
	for _, name := range names {
		if fd, ok := fields[name]; ok {
			ids = append(ids, fd.ID)
		} else {
			log.Printf("    Field %q not found on board, skipping column", name)
		}
	}
	return ids
}
