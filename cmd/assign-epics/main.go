// Command assign-epics is a one-off tool to populate the "Epic" field on
// items in an Azure org project board.  It fetches all items, filters to
// those with an empty Epic field, then heuristically assigns an Epic based
// on the item's repository and title.
//
// Usage:
//   source .env/sig-auth-search.azure.env
//   go run ./cmd/assign-epics --dry-run          # preview assignments
//   go run ./cmd/assign-epics                     # apply assignments
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/benjaminapetersen/github-project-boards-stuff/pkg/board"
	"github.com/benjaminapetersen/github-project-boards-stuff/pkg/ghgql"
)

// ---------------------------------------------------------------------------
// Item fetched from the board (project-level ID, content info, field values)
// ---------------------------------------------------------------------------

type boardItem struct {
	ItemID string // project item ID — needed for mutations
	Number int
	Title  string
	Repo   string            // "owner/name"
	State  string            // OPEN, CLOSED, MERGED
	Fields map[string]string // field name → value
}

// fetchAllItems fetches every item on the project, including the repository
// nameWithOwner so we can use it for epic matching.
func fetchAllItems(gql *ghgql.Client, projectID string) ([]boardItem, error) {
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
							}
						}
						content {
							... on Issue {
								number title state
								repository { nameWithOwner }
							}
							... on PullRequest {
								number title state
								repository { nameWithOwner }
							}
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
						ID          string `json:"id"`
						FieldValues struct {
							Nodes []struct {
								Name  string `json:"name,omitempty"`
								Text  string `json:"text,omitempty"`
								Date  string `json:"date,omitempty"`
								Field struct {
									Name string `json:"name"`
								} `json:"field"`
							} `json:"nodes"`
						} `json:"fieldValues"`
						Content struct {
							Number     int    `json:"number"`
							Title      string `json:"title"`
							State      string `json:"state"`
							Repository struct {
								NameWithOwner string `json:"nameWithOwner"`
							} `json:"repository"`
						} `json:"content"`
					} `json:"nodes"`
					PageInfo struct {
						HasNextPage bool   `json:"hasNextPage"`
						EndCursor   string `json:"endCursor"`
					} `json:"pageInfo"`
				} `json:"items"`
			} `json:"node"`
		}

		err := gql.Do(ghgql.Request{Query: query, Variables: vars}, &result)
		if err != nil {
			return nil, err
		}

		for _, n := range result.Node.Items.Nodes {
			fields := make(map[string]string)
			for _, fv := range n.FieldValues.Nodes {
				fn := fv.Field.Name
				if fn == "" {
					continue
				}
				switch {
				case fv.Name != "":
					fields[fn] = fv.Name
				case fv.Text != "":
					fields[fn] = fv.Text
				case fv.Date != "":
					fields[fn] = fv.Date
				}
			}
			items = append(items, boardItem{
				ItemID: n.ID,
				Number: n.Content.Number,
				Title:  n.Content.Title,
				Repo:   n.Content.Repository.NameWithOwner,
				State:  n.Content.State,
				Fields: fields,
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

// ---------------------------------------------------------------------------
// Epic matching rules
// ---------------------------------------------------------------------------

// rule maps a condition (repo substring or title keyword) to an epic name.
type rule struct {
	repoContains  string // match against repo (case-insensitive)
	titleContains string // match against title (case-insensitive)
	epic          string
}

// Rules are evaluated in order; first match wins.
// More specific rules should come first.
var rules = []rule{
	// ---- Repo-based rules (most specific first) ----

	// AKS Azure RBAC++
	{repoContains: "azure-rbac", epic: "AKS Azure RBAC++"},
	{repoContains: "guard", epic: "AKS Azure RBAC++"},

	// AKS Identity Bindings
	{repoContains: "workload-identity", epic: "AKS Identity Bindings"},
	{repoContains: "azure-workload-identity", epic: "AKS Identity Bindings"},
	{repoContains: "aad-pod-identity", epic: "AKS Identity Bindings"},
	{repoContains: "azure-service-operator", epic: "AKS Identity Bindings"},

	// AKS Identity Binding Image Pulls
	{repoContains: "acr-credential-provider", epic: "AKS Identity Binding Image Pulls"},

	// AKS External IDP
	{repoContains: "external-idp", epic: "AKS External IDP"},

	// AKS Image Pulls
	{repoContains: "credential-provider", epic: "AKS Image Pulls"},
	{repoContains: "cloud-provider-azure", epic: "AKS Image Pulls"},

	// SS CSI Driver
	{repoContains: "secrets-store-csi-driver", epic: "SS CSI Driver"},

	// SS Controller
	{repoContains: "secrets-store", epic: "SS Controller"}, // catch-all for secrets-store repos not already matched

	// SVM
	{repoContains: "azurefile-csi-driver", epic: "SVM"},
	{repoContains: "azuredisk-csi-driver", epic: "SVM"},

	// AI
	{repoContains: "kaito", epic: "AI"},
	{repoContains: "gpu-provisioner", epic: "AI"},

	// DRA
	{repoContains: "dra", epic: "DRA"},

	// .kuberc
	{repoContains: "kuberc", epic: ".kuberc"},

	// ARO-HCP
	{repoContains: "aro-hcp", epic: "ARO"},

	// KMS
	{repoContains: "kubernetes-kms", epic: "KMS"},

	// Dalec
	{repoContains: "dalec-build-defs", epic: "Dalec"},

	// Influence & MSFT Brand
	{repoContains: "signalhound", epic: "Influence & MSFT Brand"},
	{repoContains: "aks-engine", epic: "Influence & MSFT Brand"},
	{repoContains: "testgrid", epic: "Influence & MSFT Brand"},
	{repoContains: "cve-feed-osv", epic: "-"},

	// AI
	{repoContains: "wg-ai-conformance", epic: "AI"},

	// ---- Title-based rules (fallback — applied to all repos) ----

	// CSI driver mentions in titles
	{titleContains: "csi driver", epic: "SS CSI Driver"},
	{titleContains: "csi-driver", epic: "SS CSI Driver"},
	{titleContains: "secrets store csi", epic: "SS CSI Driver"},
	{titleContains: "secrets-store-csi", epic: "SS CSI Driver"},

	// Secrets store controller mentions
	{titleContains: "secrets store", epic: "SS Controller"},
	{titleContains: "secrets-store", epic: "SS Controller"},
	{titleContains: "secret store", epic: "SS Controller"},

	// Service account tokens
	{titleContains: "service account token", epic: "External Signing of SA Tokens"},
	{titleContains: "serviceaccount token", epic: "External Signing of SA Tokens"},
	{titleContains: "sa token", epic: "External Signing of SA Tokens"},
	{titleContains: "bound token", epic: "External Signing of SA Tokens"},
	{titleContains: "projected token", epic: "External Signing of SA Tokens"},
	{titleContains: "token signing", epic: "External Signing of SA Tokens"},
	{titleContains: "external jwt", epic: "External Signing of SA Tokens"},

	// Certificates
	{titleContains: "certificate", epic: "Certs"},
	{titleContains: "cert manager", epic: "Certs"},
	{titleContains: "cert-manager", epic: "Certs"},
	{titleContains: "x509", epic: "Certs"},
	{titleContains: "tls ", epic: "Certs"},
	{titleContains: " tls", epic: "Certs"},
	{titleContains: "kube-apiserver cert", epic: "Certs for Kube Services"},
	{titleContains: "kubelet cert", epic: "Certs for Kube Services"},

	// Webhooks
	{titleContains: "webhook", epic: "Auth to Webhooks"},
	{titleContains: "admission hook", epic: "Auth to Webhooks"},
	{titleContains: "validating admission", epic: "Manifest Based Admission"},
	{titleContains: "mutating admission", epic: "Manifest Based Admission"},
	{titleContains: "admission control", epic: "Manifest Based Admission"},
	{titleContains: "admission policy", epic: "Manifest Based Admission"},

	// Constrained impersonation
	{titleContains: "impersonat", epic: "Constrained Impersonation"},

	// Image pulls
	{titleContains: "image pull", epic: "Kube Image Pulls"},
	{titleContains: "imagepull", epic: "Kube Image Pulls"},
	{titleContains: "credential provider", epic: "Kube Image Pulls"},
	{titleContains: "kubelet credential", epic: "Kube Image Pulls"},

	// RBAC
	{titleContains: "rbac", epic: "AKS Azure RBAC++"},

	// Auth / security (broad — should come late)
	{titleContains: "authorization", epic: "Kube Security"},
	{titleContains: "authentication", epic: "Kube Security"},
	{titleContains: "clusterrole", epic: "Kube Security"},
	{titleContains: "rolebinding", epic: "Kube Security"},
	{titleContains: "security", epic: "Kube Security"},
	{titleContains: "seccomp", epic: "Kube Security"},
	{titleContains: "pod security", epic: "Kube Security"},
	{titleContains: "audit log", epic: "Kube Security"},
	{titleContains: "audit policy", epic: "Kube Security"},

	// Conditional auth
	{titleContains: "conditional auth", epic: "Conditional Auth"},

	// kuberc
	{titleContains: "kuberc", epic: ".kuberc"},
	{titleContains: ".kuberc", epic: ".kuberc"},

	// DRA
	{titleContains: "dynamic resource allocation", epic: "DRA"},
	{titleContains: "resource claim", epic: "DRA"},

	// AI
	{titleContains: "kaito", epic: "AI"},
	{titleContains: "gpu provision", epic: "AI"},

	// SVM
	{titleContains: "structured volume metadata", epic: "SVM"},
	{titleContains: "volume metadata", epic: "SVM"},

	// Influence
	{titleContains: "conformance", epic: "Influence & MSFT Brand"},

	// Workload identity (broad title match)
	{titleContains: "workload identity", epic: "AKS Identity Bindings"},
	{titleContains: "federated credential", epic: "AKS Identity Bindings"},
	{titleContains: "pod identity", epic: "AKS Identity Bindings"},

	// KMS
	{titleContains: "kms", epic: "KMS"},
	{titleContains: "key management", epic: "KMS"},
	{titleContains: "key rotation", epic: "KMS"},
}

// ensureEpicOption adds a single-select option to the Epic field if it doesn't
// already exist. Returns the updated FieldDef.
func ensureEpicOption(gql *ghgql.Client, fieldID string, epicField board.FieldDef, optionName string) (board.FieldDef, error) {
	// Already exists?
	if _, found := board.ResolveOptionID(epicField, optionName); found {
		return epicField, nil
	}

	log.Printf("Adding missing Epic option %q to the board...", optionName)

	// Build the full options list (existing + new)
	colors := []string{"GRAY", "BLUE", "GREEN", "YELLOW", "ORANGE", "RED", "PINK", "PURPLE"}
	var opts []map[string]any
	for _, existing := range epicField.Options {
		opts = append(opts, map[string]any{
			"id":   existing.ID,
			"name": existing.Name,
		})
	}
	opts = append(opts, map[string]any{
		"name":  optionName,
		"color": colors[len(epicField.Options)%len(colors)],
	})

	mutation := `mutation($fieldId: ID!, $opts: [ProjectV2SingleSelectFieldOptionInput!]!) {
		updateProjectV2Field(input: {
			fieldId: $fieldId
			singleSelectOptions: $opts
		}) {
			projectV2Field {
				... on ProjectV2SingleSelectField {
					id name
					options { id name }
				}
			}
		}
	}`

	var result struct {
		UpdateProjectV2Field struct {
			ProjectV2Field struct {
				ID      string `json:"id"`
				Name    string `json:"name"`
				Options []struct {
					ID   string `json:"id"`
					Name string `json:"name"`
				} `json:"options"`
			} `json:"projectV2Field"`
		} `json:"updateProjectV2Field"`
	}

	err := gql.Do(ghgql.Request{
		Query:     mutation,
		Variables: map[string]any{"fieldId": fieldID, "opts": opts},
	}, &result)
	if err != nil {
		return epicField, fmt.Errorf("failed to add option %q: %w", optionName, err)
	}

	// Rebuild FieldDef from response
	updated := board.FieldDef{
		ID:   epicField.ID,
		Name: epicField.Name,
		Type: "SINGLE_SELECT",
	}
	for _, opt := range result.UpdateProjectV2Field.ProjectV2Field.Options {
		updated.Options = append(updated.Options, board.FieldOption{ID: opt.ID, Name: opt.Name})
	}
	log.Printf("  Added option %q — field now has %d options", optionName, len(updated.Options))
	return updated, nil
}

// matchEpic returns the best-guess epic for an item, or "-" if no rule matches.
// The "-" value represents a catchall "no epic" bucket.
func matchEpic(repo, title string) string {
	lowerRepo := strings.ToLower(repo)
	lowerTitle := strings.ToLower(title)

	for _, r := range rules {
		if r.repoContains != "" && strings.Contains(lowerRepo, r.repoContains) {
			return r.epic
		}
		if r.titleContains != "" && strings.Contains(lowerTitle, r.titleContains) {
			return r.epic
		}
	}
	return "-"
}

// ---------------------------------------------------------------------------
// main
// ---------------------------------------------------------------------------

func main() {
	dryRun := flag.Bool("dry-run", false, "Preview assignments without writing to the board")
	flag.Parse()

	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		log.Fatal("GITHUB_TOKEN is required — source your .env file first")
	}

	org := "Azure"
	projectNum := 940

	gql := ghgql.NewClient(token)

	// 1. Find the project and get field definitions (including Epic option IDs).
	log.Printf("Finding project %s/projects/%d ...", org, projectNum)
	project, err := board.FindProjectByNumber(gql, org, projectNum)
	if err != nil {
		log.Fatalf("Could not find project: %v", err)
	}
	log.Printf("Found: %s (ID: %s)", project.Title, project.ID)

	epicField, ok := project.Fields["Epic"]
	if !ok {
		log.Fatal("\"Epic\" field not found on the board")
	}
	log.Printf("Epic field has %d options", len(epicField.Options))
	for _, opt := range epicField.Options {
		log.Printf("  %s  (ID: %s)", opt.Name, opt.ID)
	}

	// 1b. Ensure any epics referenced by rules actually exist on the board.
	// Collect unique epic names from rules.
	epicNames := make(map[string]bool)
	for _, r := range rules {
		epicNames[r.epic] = true
	}
	for name := range epicNames {
		if _, found := board.ResolveOptionID(epicField, name); !found {
			epicField, err = ensureEpicOption(gql, epicField.ID, epicField, name)
			if err != nil {
				log.Fatalf("Could not create Epic option %q: %v", name, err)
			}
		}
	}

	// 2. Fetch all items with their field values and repo info.
	log.Println("Fetching all board items (this may take several pages)...")
	items, err := fetchAllItems(gql, project.ID)
	if err != nil {
		log.Fatalf("Error fetching items: %v", err)
	}
	log.Printf("Fetched %d total items", len(items))

	// 3. Filter to items with empty Epic, excluding done/closed/merged/stale.
	oneYearAgo := time.Now().AddDate(-1, 0, 0).Format("2006-01-02")
	var needsEpic []boardItem
	skippedDone, skippedState, skippedStale := 0, 0, 0
	for _, item := range items {
		if item.Fields["Epic"] != "" {
			continue // already has an epic
		}

		// Skip items with Status = Done (matches "✅ Done", "Done", etc.)
		status := item.Fields["Status"]
		if strings.Contains(strings.ToLower(status), "done") {
			skippedDone++
			continue
		}

		// Skip closed/merged issues and PRs
		state := strings.ToUpper(item.State)
		if state == "CLOSED" || state == "MERGED" {
			skippedState++
			continue
		}

		// Skip items with Last Updated > 1 year ago
		lastUpdated := item.Fields["Last Updated"]
		if lastUpdated != "" && lastUpdated < oneYearAgo {
			skippedStale++
			continue
		}

		needsEpic = append(needsEpic, item)
	}
	log.Printf("%d items need Epic (after filtering)", len(needsEpic))
	log.Printf("  Skipped: %d done, %d closed/merged, %d stale (>1yr)", skippedDone, skippedState, skippedStale)

	// 4. Match and (optionally) apply.
	matched, unmatched := 0, 0
	updated, errors := 0, 0

	// Track unmatched items for review
	type unmatchedItem struct {
		Number int
		Title  string
		Repo   string
	}
	var unmatchedItems []unmatchedItem

	// Track matched items by epic for summary
	epicCounts := make(map[string]int)

	for i, item := range needsEpic {
		epic := matchEpic(item.Repo, item.Title)
		if epic == "" {
			unmatched++
			unmatchedItems = append(unmatchedItems, unmatchedItem{
				Number: item.Number,
				Title:  item.Title,
				Repo:   item.Repo,
			})
			continue
		}

		matched++
		epicCounts[epic]++

		optID, found := board.ResolveOptionID(epicField, epic)
		if !found {
			log.Printf("  WARNING: Epic %q is not a valid option on the board — skipping #%d", epic, item.Number)
			errors++
			continue
		}

		if *dryRun {
			log.Printf("  [DRY-RUN] #%-5d %-60s repo=%-40s → %s", item.Number, truncate(item.Title, 60), item.Repo, epic)
		} else {
			err := board.UpdateItemField(gql, project.ID, item.ItemID, epicField.ID, board.FieldValue{
				SingleSelectOptionID: optID,
			})
			if err != nil {
				log.Printf("  ERROR updating #%d: %v", item.Number, err)
				errors++
				continue
			}
			updated++
			if updated%50 == 0 {
				log.Printf("  ... updated %d/%d", updated, matched)
			}
		}

		// Progress
		if (i+1)%100 == 0 {
			log.Printf("  Processed %d/%d items needing epic...", i+1, len(needsEpic))
		}
	}

	// 5. Summary
	fmt.Println()
	fmt.Println("=== Epic Assignment Summary ===")
	fmt.Printf("  Total items on board:     %d\n", len(items))
	fmt.Printf("  Items needing epic:       %d\n", len(needsEpic))
	fmt.Printf("  Matched by rules:         %d\n", matched)
	fmt.Printf("  Unmatched (no rule):      %d\n", unmatched)
	if !*dryRun {
		fmt.Printf("  Successfully updated:     %d\n", updated)
		fmt.Printf("  Errors:                   %d\n", errors)
	}

	fmt.Println()
	fmt.Println("  Matches by epic:")
	for _, opt := range epicField.Options {
		if count, ok := epicCounts[opt.Name]; ok {
			fmt.Printf("    %-40s %d\n", opt.Name, count)
		}
	}

	if len(unmatchedItems) > 0 {
		fmt.Println()
		fmt.Printf("  Unmatched items (%d) — add rules or assign manually:\n", len(unmatchedItems))
		for _, u := range unmatchedItems {
			fmt.Printf("    #%-5d %-55s  repo=%s\n", u.Number, truncate(u.Title, 55), u.Repo)
		}
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-3] + "..."
}
