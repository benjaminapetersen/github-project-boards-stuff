// Command assign-bets is a one-off / periodic tool to populate a "Bet"
// single-select field on items in an Azure org project board.
//
// It reads a YAML config that maps Bet categories (e.g. "Top Bets",
// "Tough Cuts") to lists of Epic names.  For every item on the board that
// has an Epic value matching one of the configured epics, the tool sets
// the Bet field to the corresponding category.
//
// Usage:
//
//	source .env/sig-auth-search.azure.env
//	go run ./cmd/assign-bets --config cmd/assign-bets/bets.yaml --dry-run
//	go run ./cmd/assign-bets --config cmd/assign-bets/bets.yaml
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/benjaminapetersen/github-project-boards-stuff/pkg/board"
	"github.com/benjaminapetersen/github-project-boards-stuff/pkg/ghgql"
)

// ---------------------------------------------------------------------------
// Config file schema
// ---------------------------------------------------------------------------

// BetsConfig is the YAML structure for the bets config file.
type BetsConfig struct {
	// FieldName is the single-select field to set (default: "Bet").
	FieldName string `yaml:"fieldName"`
	// Categories maps each Bet value to a list of Epic names.
	Categories map[string][]string `yaml:"categories"`
}

// loadConfig reads and parses the YAML config file.
func loadConfig(path string) (*BetsConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	var cfg BetsConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	if cfg.FieldName == "" {
		cfg.FieldName = "Bet"
	}
	return &cfg, nil
}

// buildEpicToBet builds a case-insensitive lookup from epic name → bet category.
func buildEpicToBet(cfg *BetsConfig) map[string]string {
	m := make(map[string]string)
	for bet, epics := range cfg.Categories {
		for _, epic := range epics {
			m[strings.ToLower(epic)] = bet
		}
	}
	return m
}

// ---------------------------------------------------------------------------
// Item fetched from the board
// ---------------------------------------------------------------------------

type boardItem struct {
	ItemID string
	Number int
	Title  string
	Repo   string
	Fields map[string]string
}

// fetchAllItems fetches every item on the project with field values.
func fetchAllItems(gql *ghgql.Client, projectID string) ([]boardItem, error) {
	query := `query($projectId: ID!, $cursor: String) {
		node(id: $projectId) {
			... on ProjectV2 {
				items(first: 100, after: $cursor) {
					nodes {
						id
						fieldValues(first: 50) {
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
								number title
								repository { nameWithOwner }
							}
							... on PullRequest {
								number title
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
// main
// ---------------------------------------------------------------------------

func main() {
	dryRun := flag.Bool("dry-run", false, "Preview assignments without writing to the board")
	configPath := flag.String("config", "cmd/assign-bets/bets.yaml", "Path to the bets YAML config file")
	flag.Parse()

	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		log.Fatal("GITHUB_TOKEN is required — source your .env file first")
	}

	org := os.Getenv("GITHUB_DEST_BOARD_OWNER")
	if org == "" {
		org = "Azure"
	}
	projectNumStr := os.Getenv("GITHUB_DEST_BOARD_NUMBER")
	projectNum := 940
	if projectNumStr != "" {
		fmt.Sscanf(projectNumStr, "%d", &projectNum)
	}

	// 1. Load config.
	cfg, err := loadConfig(*configPath)
	if err != nil {
		log.Fatalf("Error loading config: %v", err)
	}
	epicToBet := buildEpicToBet(cfg)

	log.Printf("Loaded %d categories from %s:", len(cfg.Categories), *configPath)
	for bet, epics := range cfg.Categories {
		log.Printf("  %s (%d epics)", bet, len(epics))
		for _, e := range epics {
			log.Printf("    - %s", e)
		}
	}

	// 2. Connect and find the project.
	gql := ghgql.NewClient(token)

	log.Printf("Finding project %s/projects/%d ...", org, projectNum)
	project, err := board.FindProjectByNumber(gql, org, projectNum)
	if err != nil {
		log.Fatalf("Could not find project: %v", err)
	}
	log.Printf("Found: %s (ID: %s)", project.Title, project.ID)

	// 3. Locate the Bet field.
	betField, ok := project.Fields[cfg.FieldName]
	if !ok {
		log.Fatalf("%q field not found on the board", cfg.FieldName)
	}
	log.Printf("%s field has %d options:", cfg.FieldName, len(betField.Options))
	for _, opt := range betField.Options {
		log.Printf("  %s  (ID: %s)", opt.Name, opt.ID)
	}

	// 4. Ensure all bet categories exist as options on the field.
	for bet := range cfg.Categories {
		betField, err = board.EnsureOption(gql, betField, bet)
		if err != nil {
			log.Fatalf("Could not ensure %s option %q: %v", cfg.FieldName, bet, err)
		}
	}

	// 5. Locate the Epic field (to read current values).
	epicField, hasEpic := project.Fields["Epic"]
	if !hasEpic {
		log.Fatal("\"Epic\" field not found on the board — cannot map epics to bets")
	}

	// 6. Fetch all items.
	log.Println("Fetching all board items (this may take several pages)...")
	items, err := fetchAllItems(gql, project.ID)
	if err != nil {
		log.Fatalf("Error fetching items: %v", err)
	}
	log.Printf("Fetched %d total items", len(items))

	// 7. Process: for each item, read Epic → look up Bet → set if changed.
	var (
		setCount     int
		skipSame     int
		skipNoEpic   int
		skipNoMatch  int
		errorCount   int
	)

	// Track counts per bet for summary
	betCounts := make(map[string]int)
	// Track epics that aren't in our config (for review)
	unmappedEpics := make(map[string]int)

	for i, item := range items {
		epic := item.Fields["Epic"]
		if epic == "" {
			skipNoEpic++
			continue
		}

		bet, found := epicToBet[strings.ToLower(epic)]
		if !found {
			skipNoMatch++
			unmappedEpics[epic]++
			continue
		}

		// Already set to the right value?
		current := item.Fields[cfg.FieldName]
		if strings.EqualFold(current, bet) {
			skipSame++
			continue
		}

		betCounts[bet]++

		optID, resolved := board.ResolveOptionID(betField, bet)
		if !resolved {
			log.Printf("  WARNING: Bet %q not a valid option — skipping #%d", bet, item.Number)
			errorCount++
			continue
		}

		_ = epicField // used only for field existence check above

		if *dryRun {
			action := "SET"
			if current != "" {
				action = fmt.Sprintf("CHANGE %s →", current)
			}
			log.Printf("  [DRY-RUN] #%-5d %-50s  Epic=%-35s  %s %s",
				item.Number, truncate(item.Title, 50), epic, action, bet)
		} else {
			err := board.UpdateItemField(gql, project.ID, item.ItemID, betField.ID, board.FieldValue{
				SingleSelectOptionID: optID,
			})
			if err != nil {
				log.Printf("  ERROR updating #%d: %v", item.Number, err)
				errorCount++
				continue
			}
			setCount++
			if setCount%50 == 0 {
				log.Printf("  ... updated %d items so far", setCount)
			}
		}

		if (i+1)%500 == 0 {
			log.Printf("  Processed %d/%d items...", i+1, len(items))
		}
	}

	// 8. Summary
	fmt.Println()
	fmt.Println("=== Bet Assignment Summary ===")
	fmt.Printf("  Total items on board:     %d\n", len(items))
	fmt.Printf("  No Epic set (skipped):    %d\n", skipNoEpic)
	fmt.Printf("  Epic not in config:       %d\n", skipNoMatch)
	fmt.Printf("  Already correct (skip):   %d\n", skipSame)
	if *dryRun {
		total := 0
		for _, c := range betCounts {
			total += c
		}
		fmt.Printf("  Would update:             %d\n", total)
	} else {
		fmt.Printf("  Successfully updated:     %d\n", setCount)
		fmt.Printf("  Errors:                   %d\n", errorCount)
	}

	if len(betCounts) > 0 {
		fmt.Println()
		fmt.Println("  Assignments by bet:")
		for bet, count := range betCounts {
			fmt.Printf("    %-30s %d\n", bet, count)
		}
	}

	if len(unmappedEpics) > 0 {
		fmt.Println()
		fmt.Printf("  Unmapped epics (%d unique) — add to config or ignore:\n", len(unmappedEpics))
		for epic, count := range unmappedEpics {
			fmt.Printf("    %-40s %d item(s)\n", epic, count)
		}
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-3] + "..."
}
