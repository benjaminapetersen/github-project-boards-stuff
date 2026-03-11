package board

import (
	"fmt"
	"log"
	"strings"

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
	FieldNames []string // Field names that should be visible as columns (empty = no change)
}

// ---------- List Views (GraphQL — reliable for reads) ----------

// ListViews returns all views on a project via the GraphQL API.
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

// ---------- REST API Types ----------

// restView is the JSON shape returned by the GitHub REST API for project views.
type restView struct {
	ID     int    `json:"id"`
	NodeID string `json:"node_id"`
	Name   string `json:"name"`
	Number int    `json:"number"`
	Layout string `json:"layout"`
}

// restField is the JSON shape returned by the REST API for project fields.
type restField struct {
	ID       int    `json:"id"`
	NodeID   string `json:"node_id"`
	Name     string `json:"name"`
	DataType string `json:"data_type"`
}

// ---------- REST View Operations ----------

// ownerTypeFromURL determines "users" or "orgs" from a project board URL.
// e.g., "https://github.com/users/alice/projects/5" → "users"
//
//	"https://github.com/orgs/MyOrg/projects/42"  → "orgs"
func ownerTypeFromURL(url string) string {
	if strings.Contains(url, "/users/") {
		return "users"
	}
	return "orgs"
}

// listFieldsREST lists project fields via the REST API.
// Returns fields with integer IDs needed for visible_fields on views.
func listFieldsREST(gql *ghgql.Client, ownerType, owner string, projectNum int) ([]restField, error) {
	path := fmt.Sprintf("/%s/%s/projectsV2/%d/fields?per_page=100", ownerType, owner, projectNum)
	var fields []restField
	err := gql.DoREST("GET", path, nil, &fields)
	return fields, err
}

// createViewREST creates a new table view via the REST API.
// The REST API for project views only supports POST (create). There are no
// GET (list) or PATCH (update) endpoints — those return 404.
// visible_fields must be set at creation time as an array of integer field IDs.
func createViewREST(gql *ghgql.Client, ownerType, owner string, projectNum int, name string, fieldIntIDs []int) (*restView, error) {
	path := fmt.Sprintf("/%s/%s/projectsV2/%d/views", ownerType, owner, projectNum)
	body := map[string]any{
		"name":   name,
		"layout": "table",
	}
	if len(fieldIntIDs) > 0 {
		body["visible_fields"] = fieldIntIDs
	}
	var view restView
	err := gql.DoREST("POST", path, body, &view)
	if err != nil {
		return nil, err
	}
	return &view, nil
}

// ---------- Ensure Views ----------

// EnsureViews creates any missing views and sets visible columns on each.
//
// Listing uses GraphQL (reliable for reads). Creating uses the REST API,
// which is the only API that supports view creation — GraphQL has no mutation
// for views. Note: The REST views API only has a POST (create) endpoint;
// there are no GET (list) or PATCH (update) endpoints.
// visible_fields are set at view creation time in the POST body.
// For views that already exist, columns cannot be updated via API.
func EnsureViews(gql *ghgql.Client, owner string, project *Info, desired []ViewConfig) {
	if len(desired) == 0 {
		return
	}

	ownerType := ownerTypeFromURL(project.URL)

	// Always list views via GraphQL — the REST API has no list endpoint.
	gqlViews, err := ListViews(gql, project.ID)
	if err != nil {
		log.Printf("Warning: could not list project views via GraphQL: %v", err)
		return
	}

	// Index existing views by name
	type viewInfo struct {
		NodeID string
		Name   string
		Number int
	}
	viewsByName := make(map[string]viewInfo, len(gqlViews))
	for _, v := range gqlViews {
		viewsByName[v.Name] = viewInfo{NodeID: v.ID, Name: v.Name, Number: v.Number}
	}

	// Collect views that need manual creation (when REST create fails)
	var manualViews []ViewConfig
	restCreateWorks := true

	// Lazily populated: maps field name → REST integer ID for visible_fields.
	var restFieldsByName map[string]int

	for _, want := range desired {
		if _, exists := viewsByName[want.Name]; exists {
			log.Printf("  View %q already exists", want.Name)
			continue
		}

		if !restCreateWorks {
			manualViews = append(manualViews, want)
			continue
		}

		// Resolve field integer IDs for visible_fields (lazy — fetched once)
		var fieldIDs []int
		if len(want.FieldNames) > 0 {
			if restFieldsByName == nil {
				rfList, rfErr := listFieldsREST(gql, ownerType, owner, project.Number)
				if rfErr != nil {
					log.Printf("    Warning: could not list fields via REST for visible_fields: %v", rfErr)
				} else {
					restFieldsByName = make(map[string]int, len(rfList))
					for _, rf := range rfList {
						restFieldsByName[rf.Name] = rf.ID
					}
				}
			}
			if restFieldsByName != nil {
				fieldIDs = resolveFieldIntIDs(want.FieldNames, restFieldsByName)
			}
		}

		log.Printf("  Creating view %q via REST API...", want.Name)
		created, createErr := createViewREST(gql, ownerType, owner, project.Number, want.Name, fieldIDs)
		if createErr != nil {
			log.Printf("  REST create failed for %q: %v", want.Name, createErr)
			restCreateWorks = false
			manualViews = append(manualViews, want)
			continue
		}
		log.Printf("  Created view %q (number: %d)", want.Name, created.Number)
		if len(fieldIDs) > 0 {
			log.Printf("    Set %d visible column(s): %v", len(fieldIDs), want.FieldNames)
		}
	}

	// Print manual-creation summary if REST failed
	if len(manualViews) > 0 {
		log.Println()
		log.Printf("╔══════════════════════════════════════════════════════════════════╗")
		log.Printf("║  MANUAL ACTION REQUIRED: %d view(s) could not be created        ║", len(manualViews))
		log.Printf("╟──────────────────────────────────────────────────────────────────╢")
		log.Printf("║  The REST API returned an error for this org, and GitHub's      ║")
		log.Printf("║  GraphQL API has no mutation for creating project views.         ║")
		log.Printf("║                                                                  ║")
		log.Printf("║  Please create these views manually in the board UI:             ║")
		log.Printf("║  %s", project.URL)
		log.Printf("║                                                                  ║")
		for i, v := range manualViews {
			log.Printf("║  %2d. %s", i+1, v.Name)
			if len(v.FieldNames) > 0 {
				log.Printf("║      columns: %s", strings.Join(v.FieldNames, ", "))
			}
		}
		log.Printf("║                                                                  ║")
		log.Printf("║  Once created, re-run to verify they are detected.               ║")
		log.Printf("╚══════════════════════════════════════════════════════════════════╝")
		log.Println()
	}
}

// ---------- Update View Filter ----------

// UpdateViewFilter sets the filter string on an existing project view.
// Uses the GraphQL updateProjectV2View mutation.
func UpdateViewFilter(gql *ghgql.Client, viewID, filter string) error {
	mutation := `mutation($viewId: ID!, $filter: String) {
		updateProjectV2View(input: {viewId: $viewId, filter: $filter}) {
			projectV2View { id filter }
		}
	}`

	var result struct {
		UpdateProjectV2View struct {
			ProjectV2View struct {
				ID     string `json:"id"`
				Filter string `json:"filter"`
			} `json:"projectV2View"`
		} `json:"updateProjectV2View"`
	}

	err := gql.Do(ghgql.Request{
		Query: mutation,
		Variables: map[string]any{
			"viewId": viewID,
			"filter": filter,
		},
	}, &result)
	if err != nil {
		return fmt.Errorf("failed to update view filter: %w", err)
	}
	return nil
}

// resolveFieldIntIDs maps field names to REST integer field IDs.
func resolveFieldIntIDs(names []string, fieldsByName map[string]int) []int {
	var ids []int
	for _, name := range names {
		if id, ok := fieldsByName[name]; ok {
			ids = append(ids, id)
		} else {
			log.Printf("    Field %q not found on board, skipping column", name)
		}
	}
	return ids
}
