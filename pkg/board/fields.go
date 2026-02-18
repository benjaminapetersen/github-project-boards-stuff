package board

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/benjaminapetersen/github-project-boards-stuff/pkg/ghgql"
)

// FieldDef describes a GitHub Projects V2 field and its options.
type FieldDef struct {
	ID      string
	Name    string
	Type    string // "SINGLE_SELECT", "TEXT", etc.
	Options []FieldOption
}

// FieldOption is a single-select option within a field.
type FieldOption struct {
	ID   string
	Name string
}

// FieldMap maps field names to their definitions (including option IDs).
type FieldMap map[string]FieldDef

// FieldValue holds the value to set on a project item field.
type FieldValue struct {
	SingleSelectOptionID string
	Text                 string
}

// ProjectWithFields holds a project's info along with its field definitions.
type ProjectWithFields struct {
	Info
	Public bool
	Fields FieldMap
}

// ---------- Find Project by Number ----------

// FindProjectByNumber queries a specific project by org + number.
func FindProjectByNumber(gql *ghgql.Client, org string, number int) (*ProjectWithFields, error) {
	query := `query($org: String!, $number: Int!) {
		organization(login: $org) {
			projectV2(number: $number) {
				id title number url public
				fields(first: 50) {
					nodes {
						... on ProjectV2SingleSelectField {
							id name
							options { id name }
						}
						... on ProjectV2FieldCommon {
							id name dataType
						}
					}
				}
			}
		}
	}`

	var result struct {
		Organization struct {
			ProjectV2 *struct {
				ID     string `json:"id"`
				Title  string `json:"title"`
				Number int    `json:"number"`
				URL    string `json:"url"`
				Public bool   `json:"public"`
				Fields struct {
					Nodes []projectFieldNode `json:"nodes"`
				} `json:"fields"`
			} `json:"projectV2"`
		} `json:"organization"`
	}

	err := gql.Do(ghgql.Request{
		Query:     query,
		Variables: map[string]any{"org": org, "number": number},
	}, &result)
	if err != nil {
		return nil, err
	}

	p := result.Organization.ProjectV2
	if p == nil {
		return nil, fmt.Errorf("project #%d not found in org %s", number, org)
	}

	fields := parseFieldNodes(p.Fields.Nodes)

	return &ProjectWithFields{
		Info: Info{
			ID:     p.ID,
			Number: p.Number,
			Title:  p.Title,
			URL:    p.URL,
		},
		Public: p.Public,
		Fields: fields,
	}, nil
}

// FindUserProjectByNumber queries a specific user-owned project by number.
func FindUserProjectByNumber(gql *ghgql.Client, user string, number int) (*ProjectWithFields, error) {
	query := `query($user: String!, $number: Int!) {
		user(login: $user) {
			projectV2(number: $number) {
				id title number url public
				fields(first: 50) {
					nodes {
						... on ProjectV2SingleSelectField {
							id name
							options { id name }
						}
						... on ProjectV2FieldCommon {
							id name dataType
						}
					}
				}
			}
		}
	}`

	var result struct {
		User struct {
			ProjectV2 *struct {
				ID     string `json:"id"`
				Title  string `json:"title"`
				Number int    `json:"number"`
				URL    string `json:"url"`
				Public bool   `json:"public"`
				Fields struct {
					Nodes []projectFieldNode `json:"nodes"`
				} `json:"fields"`
			} `json:"projectV2"`
		} `json:"user"`
	}

	err := gql.Do(ghgql.Request{
		Query:     query,
		Variables: map[string]any{"user": user, "number": number},
	}, &result)
	if err != nil {
		return nil, err
	}

	p := result.User.ProjectV2
	if p == nil {
		return nil, fmt.Errorf("project #%d not found for user %s", number, user)
	}

	fields := parseFieldNodes(p.Fields.Nodes)

	return &ProjectWithFields{
		Info: Info{
			ID:     p.ID,
			Number: p.Number,
			Title:  p.Title,
			URL:    p.URL,
		},
		Public: p.Public,
		Fields: fields,
	}, nil
}

type projectFieldNode struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	DataType string `json:"dataType,omitempty"`
	Options  []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"options,omitempty"`
}

func parseFieldNodes(nodes []projectFieldNode) FieldMap {
	fields := make(FieldMap)
	for _, n := range nodes {
		if n.Name == "" {
			continue
		}
		def := FieldDef{
			ID:   n.ID,
			Name: n.Name,
			Type: n.DataType,
		}
		if len(n.Options) > 0 {
			def.Type = "SINGLE_SELECT"
			for _, opt := range n.Options {
				def.Options = append(def.Options, FieldOption{ID: opt.ID, Name: opt.Name})
			}
		}
		fields[n.Name] = def
	}
	return fields
}

// ---------- Get Project Fields ----------

// GetProjectFields returns all field definitions for a project.
func GetProjectFields(gql *ghgql.Client, projectID string) (FieldMap, error) {
	query := `query($projectId: ID!) {
		node(id: $projectId) {
			... on ProjectV2 {
				fields(first: 50) {
					nodes {
						... on ProjectV2SingleSelectField {
							id name
							options { id name }
						}
						... on ProjectV2FieldCommon {
							id name dataType
						}
					}
				}
			}
		}
	}`

	var result struct {
		Node struct {
			Fields struct {
				Nodes []projectFieldNode `json:"nodes"`
			} `json:"fields"`
		} `json:"node"`
	}

	err := gql.Do(ghgql.Request{
		Query:     query,
		Variables: map[string]any{"projectId": projectID},
	}, &result)
	if err != nil {
		return nil, err
	}

	return parseFieldNodes(result.Node.Fields.Nodes), nil
}

// ---------- Ensure Private ----------

// EnsurePrivate sets a project to not-public.
func EnsurePrivate(gql *ghgql.Client, projectID string) error {
	mutation := `mutation($projectId: ID!) {
		updateProjectV2(input: {projectId: $projectId, public: false}) {
			projectV2 { id public }
		}
	}`

	var result json.RawMessage
	return gql.Do(ghgql.Request{
		Query:     mutation,
		Variables: map[string]any{"projectId": projectID},
	}, &result)
}

// ---------- Update Item Field ----------

// UpdateItemField sets a field value on a project item.
func UpdateItemField(gql *ghgql.Client, projectID, itemID, fieldID string, value FieldValue) error {
	var valueMap map[string]any
	if value.SingleSelectOptionID != "" {
		valueMap = map[string]any{"singleSelectOptionId": value.SingleSelectOptionID}
	} else if value.Text != "" {
		valueMap = map[string]any{"text": value.Text}
	} else {
		return nil // nothing to set
	}

	mutation := `mutation($projectId: ID!, $itemId: ID!, $fieldId: ID!, $value: ProjectV2FieldValue!) {
		updateProjectV2ItemFieldValue(input: {
			projectId: $projectId
			itemId: $itemId
			fieldId: $fieldId
			value: $value
		}) {
			projectV2Item { id }
		}
	}`

	var result json.RawMessage
	return gql.Do(ghgql.Request{
		Query: mutation,
		Variables: map[string]any{
			"projectId": projectID,
			"itemId":    itemID,
			"fieldId":   fieldID,
			"value":     valueMap,
		},
	}, &result)
}

// ---------- Add Item and Return Item ID ----------

// AddItem adds a content item to a project and returns the project item ID.
// Returns ("", nil) if the item is already on the board.
func AddItem(gql *ghgql.Client, projectID, contentID string) (string, error) {
	mutation := `mutation($projectId: ID!, $contentId: ID!) {
		addProjectV2ItemById(input: {projectId: $projectId, contentId: $contentId}) {
			item { id }
		}
	}`

	var result struct {
		AddProjectV2ItemById struct {
			Item struct {
				ID string `json:"id"`
			} `json:"item"`
		} `json:"addProjectV2ItemById"`
	}

	err := gql.Do(ghgql.Request{
		Query:     mutation,
		Variables: map[string]any{"projectId": projectID, "contentId": contentID},
	}, &result)
	if err != nil {
		return "", err
	}

	return result.AddProjectV2ItemById.Item.ID, nil
}

// ---------- Fetch Project Items with Fields ----------

// ProjectItemWithFields represents an item on a board with its custom field values.
type ProjectItemWithFields struct {
	ItemID    string            // project-level item ID (for mutations)
	ContentID string            // underlying issue/PR node ID
	Number    int
	Title     string
	Fields    map[string]string // field name → value
}

// FetchProjectItems returns all items on a project with their custom field values.
func FetchProjectItems(gql *ghgql.Client, projectID string) ([]ProjectItemWithFields, error) {
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
							... on Issue {
								id number title
							}
							... on PullRequest {
								id number title
							}
						}
					}
					pageInfo { hasNextPage endCursor }
				}
			}
		}
	}`

	var items []ProjectItemWithFields
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
							Nodes []fieldValNode `json:"nodes"`
						} `json:"fieldValues"`
						Content struct {
							ID     string `json:"id"`
							Number int    `json:"number"`
							Title  string `json:"title"`
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
				fieldName := fv.Field.Name
				if fieldName == "" {
					continue
				}
				switch {
				case fv.Name != "":
					fields[fieldName] = fv.Name
				case fv.Text != "":
					fields[fieldName] = fv.Text
				case fv.Date != "":
					fields[fieldName] = fv.Date
				case fv.Number != 0:
					fields[fieldName] = fmt.Sprintf("%.0f", fv.Number)
				case fv.Title != "":
					fields[fieldName] = fv.Title
				}
			}
			items = append(items, ProjectItemWithFields{
				ItemID:    n.ID,
				ContentID: n.Content.ID,
				Number:    n.Content.Number,
				Title:     n.Content.Title,
				Fields:    fields,
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

type fieldValNode struct {
	Name   string  `json:"name,omitempty"`
	Text   string  `json:"text,omitempty"`
	Date   string  `json:"date,omitempty"`
	Number float64 `json:"number,omitempty"`
	Title  string  `json:"title,omitempty"`
	Field  struct {
		Name string `json:"name"`
	} `json:"field"`
}

// ---------- Resolve Option ID ----------

// ResolveOptionID finds a single-select option ID by name within a field.
// Returns ("", false) if not found.
func ResolveOptionID(field FieldDef, optionName string) (string, bool) {
	lower := strings.ToLower(optionName)
	for _, opt := range field.Options {
		if strings.ToLower(opt.Name) == lower {
			return opt.ID, true
		}
	}
	return "", false
}

// ---------- Set Item Fields ----------

// SetItemFields sets multiple field values on a project item.
// fieldValues maps field names to desired string values.
// destFields provides the field IDs and option IDs for the destination board.
// Logs warnings for unresolvable fields/options.
func SetItemFields(gql *ghgql.Client, projectID, itemID string, fieldValues map[string]string, destFields FieldMap) {
	for fieldName, desiredValue := range fieldValues {
		if desiredValue == "" {
			continue
		}

		destField, ok := destFields[fieldName]
		if !ok {
			log.Printf("    Field %q not found on destination board, skipping", fieldName)
			continue
		}

		var fv FieldValue
		if destField.Type == "SINGLE_SELECT" {
			optID, found := ResolveOptionID(destField, desiredValue)
			if !found {
				log.Printf("    Option %q not found for field %q, skipping", desiredValue, fieldName)
				continue
			}
			fv.SingleSelectOptionID = optID
		} else {
			fv.Text = desiredValue
		}

		if err := UpdateItemField(gql, projectID, itemID, destField.ID, fv); err != nil {
			log.Printf("    Error setting %s=%s: %v", fieldName, desiredValue, err)
		}
	}
}

// ---------- Create Custom Fields ----------

// FieldSpec describes a custom field to create on a project board.
type FieldSpec struct {
	Name    string   // Field display name
	Type    string   // "TEXT", "SINGLE_SELECT", "NUMBER", "DATE"
	Options []string // Option names for SINGLE_SELECT fields
}

// CreateTextField creates a text custom field on a project.
func CreateTextField(gql *ghgql.Client, projectID, name string) (*FieldDef, error) {
	return createField(gql, projectID, name, "TEXT", nil)
}

// CreateSingleSelectField creates a single-select custom field with the given options.
func CreateSingleSelectField(gql *ghgql.Client, projectID, name string, options []string) (*FieldDef, error) {
	return createField(gql, projectID, name, "SINGLE_SELECT", options)
}

func createField(gql *ghgql.Client, projectID, name, dataType string, options []string) (*FieldDef, error) {
	mutation := `mutation($input: CreateProjectV2FieldInput!) {
		createProjectV2Field(input: $input) {
			projectV2Field {
				... on ProjectV2Field {
					id name dataType
				}
				... on ProjectV2SingleSelectField {
					id name
					options { id name }
				}
			}
		}
	}`

	input := map[string]any{
		"projectId": projectID,
		"dataType":  dataType,
		"name":      name,
	}

	if dataType == "SINGLE_SELECT" && len(options) > 0 {
		colors := []string{"GRAY", "BLUE", "GREEN", "YELLOW", "ORANGE", "RED", "PINK", "PURPLE"}
		var opts []map[string]string
		for i, opt := range options {
			opts = append(opts, map[string]string{
				"name":        opt,
				"color":       colors[i%len(colors)],
				"description": "",
			})
		}
		input["singleSelectOptions"] = opts
	}

	var result struct {
		CreateProjectV2Field struct {
			ProjectV2Field struct {
				ID       string `json:"id"`
				Name     string `json:"name"`
				DataType string `json:"dataType,omitempty"`
				Options  []struct {
					ID   string `json:"id"`
					Name string `json:"name"`
				} `json:"options,omitempty"`
			} `json:"projectV2Field"`
		} `json:"createProjectV2Field"`
	}

	err := gql.Do(ghgql.Request{
		Query:     mutation,
		Variables: map[string]any{"input": input},
	}, &result)
	if err != nil {
		return nil, err
	}

	f := result.CreateProjectV2Field.ProjectV2Field
	def := &FieldDef{
		ID:   f.ID,
		Name: f.Name,
		Type: f.DataType,
	}
	if len(f.Options) > 0 {
		def.Type = "SINGLE_SELECT"
		for _, opt := range f.Options {
			def.Options = append(def.Options, FieldOption{ID: opt.ID, Name: opt.Name})
		}
	}
	return def, nil
}

// EnsureFields ensures the destination board has all the specified fields.
// For SINGLE_SELECT fields, options are copied from the source field definitions.
// Returns the updated FieldMap for the destination board.
func EnsureFields(gql *ghgql.Client, projectID string, needed []FieldSpec, existing FieldMap) FieldMap {
	for _, spec := range needed {
		if existingField, ok := existing[spec.Name]; ok {
			if spec.Type == "SINGLE_SELECT" && len(spec.Options) > 0 {
				missing := countMissingOptions(existingField, spec.Options)
				if missing > 0 {
					log.Printf("  Field %q exists but is missing %d of %d option(s) — delete field on board and re-run to fix",
						spec.Name, missing, len(spec.Options))
				} else {
					log.Printf("  Field %q already exists (%d option(s))", spec.Name, len(existingField.Options))
				}
			} else {
				log.Printf("  Field %q already exists", spec.Name)
			}
			continue
		}

		var newField *FieldDef
		var err error

		switch spec.Type {
		case "SINGLE_SELECT":
			log.Printf("  Creating single-select field %q with %d options...", spec.Name, len(spec.Options))
			newField, err = CreateSingleSelectField(gql, projectID, spec.Name, spec.Options)
		default:
			log.Printf("  Creating text field %q...", spec.Name)
			newField, err = CreateTextField(gql, projectID, spec.Name)
		}

		if err != nil {
			log.Printf("  Warning: could not create field %q: %v", spec.Name, err)
			log.Printf("  Please create this field manually on your destination board.")
			continue
		}
		log.Printf("  Created field %q (ID: %s)", newField.Name, newField.ID)
		existing[spec.Name] = *newField
	}

	return existing
}

func countMissingOptions(field FieldDef, needed []string) int {
	have := make(map[string]bool)
	for _, opt := range field.Options {
		have[strings.ToLower(opt.Name)] = true
	}
	missing := 0
	for _, name := range needed {
		if !have[strings.ToLower(name)] {
			missing++
		}
	}
	return missing
}
