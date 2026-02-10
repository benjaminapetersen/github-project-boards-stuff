# Plan: Mirror Source Project Board Data

## Goal

Read a GitHub Projects V2 board you don't own (e.g. kubernetes project 241 —
the 1.36 Enhancements Tracking board), extract its custom field values (Status,
Stage, PRR, etc.), and use that data to populate fields on your own project board.

## Background

The GitHub GraphQL API allows read access to any **public** project board with a
classic PAT that has `read:project` scope. Custom fields (single-select, text,
date, iteration) and their values are all queryable per-item. This means we can
treat project 241 as a **data source** and mirror its field values onto a
separate board that we own and control.

### What's on project 241

The [1.36 Enhancements Tracking](https://github.com/orgs/kubernetes/projects/241/views/1) board has items with fields like:

- **Status** — e.g. "Tracked", "At risk for Enhancements Freeze", "Deferred", "Removed from Milestone"
- **Stage** — e.g. "alpha", "beta", "stable"
- **PRR status** — approval/review status from the Production Readiness Review
- Plus standard issue metadata (labels, milestone, assignees)

## Implementation Plan

### 1. New CLI flag: `--source-project`

Add a flag that identifies the source project board to read from.

```
--source-project kubernetes/241
```

Format: `<org-or-user>/<project-number>`. When provided, the tool will query
this board first and use its data to enrich the output.

### 2. New config fields

```go
type Config struct {
    // ... existing fields ...

    // Source project board to read field values from
    SourceProjectOwner  string // e.g. "kubernetes"
    SourceProjectNumber int    // e.g. 241
}
```

### 3. Query the source project's fields

GraphQL query to discover all fields and their options:

```graphql
query($owner: String!, $number: Int!) {
  organization(login: $owner) {
    projectV2(number: $number) {
      id
      title
      fields(first: 50) {
        nodes {
          ... on ProjectV2SingleSelectField {
            id
            name
            options { id name }
          }
          ... on ProjectV2Field {
            id
            name
          }
          ... on ProjectV2IterationField {
            id
            name
            configuration {
              iterations { id startDate }
            }
          }
        }
      }
    }
  }
}
```

This returns the field definitions — names like "Status", "Stage", "PRR" and
their option values ("Tracked", "At risk for Enhancements Freeze", etc.).

### 4. Query the source project's items with field values

Page through all items, capturing their content (issue number, URL, node ID)
and all field values:

```graphql
query($projectId: ID!, $cursor: String) {
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
              id
              number
              title
              url
              labels(first: 10) { nodes { name } }
              assignees(first: 10) { nodes { login } }
              milestone { title }
            }
          }
        }
        pageInfo { hasNextPage endCursor }
      }
    }
  }
}
```

### 5. Build a lookup map

From the source board data, build a map of issue node ID → field values:

```go
// SourceItemData holds the field values read from the source project board.
type SourceItemData struct {
    IssueNodeID string
    IssueNumber int
    Fields      map[string]string // field name → value (e.g. "Status" → "Tracked")
}

// map[issueNodeID] → SourceItemData
sourceData map[string]SourceItemData
```

### 6. Create matching fields on the destination board

When `--output board` is used with `--source-project`, before adding items:

1. Query the destination board's existing fields
2. For each single-select field on the source board (Status, Stage, PRR, etc.),
   check if a matching field exists on the destination
3. If not, create it via `createProjectV2Field` mutation with the same options
4. Build a mapping of source field option names → destination option IDs

**Note:** The GraphQL API supports creating single-select fields:

```graphql
mutation {
  createProjectV2Field(input: {
    projectId: "DEST_PROJECT_ID"
    dataType: SINGLE_SELECT
    name: "Status"
    singleSelectOptions: [
      {name: "Tracked", color: GREEN}
      {name: "At risk for Enhancements Freeze", color: YELLOW}
    ]
  }) {
    projectV2Field { id }
  }
}
```

### 7. Set field values on destination board items

After adding each issue to the destination board, set its field values by
looking up the source data:

```graphql
mutation {
  updateProjectV2ItemFieldValue(input: {
    projectId: "DEST_PROJECT_ID"
    itemId: "ITEM_ID"
    fieldId: "DEST_FIELD_ID"
    value: { singleSelectOptionId: "DEST_OPTION_ID" }
  }) {
    projectV2Item { id }
  }
}
```

### 8. CLI output enhancement

In `--output cli` mode with `--source-project`, enrich the printed output:

```
#4955  KEP-1234: Structured Authentication Config
       Stage: beta        Status: Tracked     PRR: Approved
       Milestone: v1.36   Assignees: enj, aramase
       URL: https://github.com/kubernetes/enhancements/issues/4955
       Labels: sig/auth, stage/beta, ...
```

### 9. Caching

Cache the source project data alongside issue data:

```
.cache/source_kubernetes_241_2026-02-09T15-08-17.json
```

When `--use-cache=true`, load the cached source data instead of re-querying.

## New Files / Changes

| File | Changes |
|------|---------|
| `main.go` | Add `--source-project` flag, parse into config, pass source data to output |
| `board.go` | Add `readSourceProject()`, `ensureFieldsExist()`, `setItemFieldValues()` |
| `cache.go` | Add cache read/write for source project data |
| `ratelimit.go` | Update cost estimation to account for source project queries |

## Cost Estimate

Reading the source project:

| Operation | Cost |
|-----------|------|
| Query source project fields | 1 GraphQL point |
| Query source project items (per page of 100) | 1 GraphQL point per page |
| Create fields on destination board (one-time) | ~1 point per field |
| Set field value per item | 1 point per item |

For ~30 sig/auth items on project 241, total additional cost would be ~35-40
GraphQL points on top of the existing board operations.

## Caveats

1. **Custom fields are per-project.** Source field option IDs won't work on the
   destination — we must create matching fields and map option names to new IDs.
2. **REDACTED items.** Items the authenticated user can't view are returned as
   `REDACTED`. For public issues on a public project, this shouldn't apply.
3. **Field creation API.** The `createProjectV2Field` mutation may require the
   `project` scope (not just `read:project`). The existing token config already
   requires `project` scope for board mode.
4. **Sync semantics.** With `--sync`, stale items are removed. Field values on
   remaining items should be re-synced from the source on each run.
5. **Source board schema changes.** If project 241 adds/renames fields or
   options, subsequent runs will detect and handle mismatches gracefully.
