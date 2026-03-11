// Temporary debug tool to inspect board views, fields, and sample data.
package main

import (
	"fmt"
	"log"
	"os"

	"github.com/benjaminapetersen/github-project-boards-stuff/pkg/board"
	"github.com/benjaminapetersen/github-project-boards-stuff/pkg/ghgql"
)

func main() {
	gql := ghgql.NewClient(os.Getenv("GITHUB_TOKEN"))
	project, err := board.FindProjectByNumber(gql, "Azure", 940)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Project: %s (ID: %s)\n\n", project.Title, project.ID)

	// List views
	views, err := board.ListViews(gql, project.ID)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("=== Views (%d) ===\n", len(views))
	for _, v := range views {
		fmt.Printf("  Name: %-40s Filter: %q\n", v.Name, v.Filter)
	}

	// List fields
	fmt.Printf("\n=== Fields (%d) ===\n", len(project.Fields))
	for name, f := range project.Fields {
		fmt.Printf("  %-30s type=%-15s id=%s", name, f.Type, f.ID)
		if len(f.Options) > 0 {
			fmt.Printf("  (%d options)", len(f.Options))
		}
		fmt.Println()
	}

	// Sample items — lightweight query for first 50 items, show Status + Last Updated
	fmt.Printf("\n=== Sample Items with 'Last Updated' (first 50) ===\n")
	sampleQuery := `query($projectId: ID!) {
		node(id: $projectId) {
			... on ProjectV2 {
				items(first: 50) {
					nodes {
						fieldValues(first: 20) {
							nodes {
								... on ProjectV2ItemFieldSingleSelectValue {
									name
									field { ... on ProjectV2FieldCommon { name } }
								}
								... on ProjectV2ItemFieldDateValue {
									date
									field { ... on ProjectV2FieldCommon { name } }
								}
								... on ProjectV2ItemFieldTextValue {
									text
									field { ... on ProjectV2FieldCommon { name } }
								}
							}
						}
						content {
							... on Issue { number title }
							... on PullRequest { number title }
						}
					}
				}
			}
		}
	}`
	var sampleResult struct {
		Node struct {
			Items struct {
				Nodes []struct {
					FieldValues struct {
						Nodes []struct {
							Name  string `json:"name,omitempty"`
							Date  string `json:"date,omitempty"`
							Text  string `json:"text,omitempty"`
							Field struct {
								Name string `json:"name"`
							} `json:"field"`
						} `json:"nodes"`
					} `json:"fieldValues"`
					Content struct {
						Number int    `json:"number"`
						Title  string `json:"title"`
					} `json:"content"`
				} `json:"nodes"`
			} `json:"items"`
		} `json:"node"`
	}
	err = gql.Do(ghgql.Request{
		Query:     sampleQuery,
		Variables: map[string]any{"projectId": project.ID},
	}, &sampleResult)
	if err != nil {
		log.Fatal(err)
	}
	doneCount, hasDateCount, emptyDateCount := 0, 0, 0
	for _, item := range sampleResult.Node.Items.Nodes {
		status, lastUpdated := "", ""
		for _, fv := range item.FieldValues.Nodes {
			fn := fv.Field.Name
			switch fn {
			case "Status":
				if fv.Name != "" {
					status = fv.Name
				}
			case "Last Updated":
				if fv.Date != "" {
					lastUpdated = fv.Date
				} else if fv.Text != "" {
					lastUpdated = "(TEXT:" + fv.Text + ")"
				}
			}
		}
		isDone := status == "✅ Done" || status == "Done"
		if isDone {
			doneCount++
		}
		if lastUpdated != "" {
			hasDateCount++
		} else {
			emptyDateCount++
		}
		fmt.Printf("  #%-6d %-12s Last Updated=%-15s  %s\n",
			item.Content.Number, status, lastUpdated, truncate(item.Content.Title, 50))
	}
	fmt.Printf("\n  Summary of 50 items: %d done, %d have Last Updated, %d empty\n", doneCount, hasDateCount, emptyDateCount)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-3] + "..."
}
