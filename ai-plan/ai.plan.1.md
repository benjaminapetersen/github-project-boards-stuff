# Plan: `cmd/sig-auth-team-board` — Private SIG-Auth Team Board Builder

## 1. Problem Statement

During each Kubernetes release cycle, our SIG-Auth sub-team needs to track:

1. **KEPs our team is directly driving** — both under `sig/auth` and in other SIGs
   (e.g., `sig/api-machinery`, `sig/node`, `sig/scheduling`)
2. **Custom field state** from the official Kubernetes Enhancements Tracking Board
   (project #241) — Status, Stage, PRR Status, Enhancement Type, etc.
3. **Issues and PRs in `kubernetes/kubernetes`** that our team members are working on
4. Anything else our broader SIG (including leads from other companies) is driving

We want all of this on a **private** project board under our own GitHub user so that:
- We get a unified dashboard of our team's release work
- Adding items to our board does **not** create noise for the open-source community
  (no timeline events on upstream issues)
- We can track items across multiple SIGs without being limited to the `sig/auth` label

### Scope Boundaries

The tool is **team-focused, not label-focused**. The existing `sig-auth-tools` triage
tool casts a wide net (every `sig/auth`-labeled item in all `kubernetes` and
`kubernetes-sigs` repos). This tool is narrower — it follows specific people:

| Group | Members | Purpose |
|---|---|---|
| `CORE_TEAM` | benjaminapetersen, enj, aramase, stlaz, pmengelbert, ritazh | Red Hat sub-team within SIG-Auth |
| `OTHERS_OF_INTEREST` | liggitt, deads2k, ahmredtd, luxas | SIG-Auth leads/regulars from other companies |

Combined = `GITHUB_USERNAMES` — our working set.

---

## 2. Reference Systems

### 2.1 Kubernetes Enhancements Tracking Board (Project #241)

- **URL:** https://github.com/orgs/kubernetes/projects/241
- **Owner:** `kubernetes` org
- **Purpose:** Official release tracking for all enhancements across all SIGs
- **Scope:** All KEPs targeted for the current milestone (e.g., v1.36)

**Custom fields on this board** (these are what we want to capture):

| Field | Type | Example Values |
|---|---|---|
| Status | SingleSelect | `At risk for enhancements freeze`, `Tracked`, `Deferred`, `Removed from Milestone` |
| Stage | SingleSelect | `Alpha`, `Beta`, `Stable`, `Deprecation/Removal` |
| PRR Status | SingleSelect | `Approved`, `In process`, `No need` |
| Enhancement Type | SingleSelect | `Net New`, `Major Change`, `Graduating`, `Deprecation`, `Docs` |
| SIG | SingleSelect | `sig-auth`, `sig-api-machinery`, `sig-node`, etc. |
| Enhancements Contact | Text | `@jmickey`, `@lasomethingsomething`, `@whtssub` |
| Assignees | Users | (GitHub assignees) |
| Exception Request | Text | (free text) |

**Key observation:** Items on this board are issues from `kubernetes/enhancements`.
Each item has both its native issue metadata (labels, milestone, assignees) AND
board-level custom field values set by the enhancements team.

### 2.2 SIG-Auth Triage Board (Project #116)

- **URL:** https://github.com/orgs/kubernetes/projects/116
- **Owner:** `kubernetes` org
- **Maintained by:** `kubernetes-sigs/sig-auth-tools` triage automation
- **Approach:** Broad label-based sweep — finds ALL `sig/auth`-labeled issues/PRs
  across every repo in the `kubernetes` org, plus all items in `kubernetes-sigs` repos
  tagged with the `k8s-sig-auth` topic

### 2.3 `kubernetes-sigs/sig-auth-tools` (Reference Implementation)

- **Source:** https://github.com/kubernetes-sigs/sig-auth-tools/blob/main/main.go
- **Go:** 1.19, `go-github/v48`, `shurcooL/githubv4`
- **Runs on:** GitHub Actions cron (daily at 14:00 UTC)
- **Algorithm:**
  1. Resolve project #116 → get Status field ID + option IDs ("Needs Triage", "Subprojects - Needs Triage")
  2. List ALL repos in `kubernetes` org
  3. For each repo: find issues/PRs labeled `sig/auth`, add to board, set Status if unset
  4. Search `kubernetes-sigs` repos by topic `k8s-sig-auth`, add items with "Subprojects" status
- **Key difference from our tool:** They scan by label across ALL repos. We scan by
  username across specific repos and enrich with board field data.
- **Useful patterns to borrow:** `addProjectV2ItemById` mutation, `updateProjectItemField` mutation for setting Status on newly-added items

---

## 3. Existing Codebase Assets

We have a well-structured Go project with reusable shared packages:

### 3.1 Shared Packages (`pkg/`)

| Package | Provides | Relevant For This Tool? |
|---|---|---|
| `pkg/ghgql` | OAuth2 GraphQL client, `RateLimitError` | **Yes** — all GraphQL queries |
| `pkg/board` | Find/create project, add items, link repos, sync/remove stale items | **Yes** — destination board management |
| `pkg/cache` | JSON file-based caching with timestamps, generic `ReadLatest[T]` | **Yes** — cache API responses |
| `pkg/ratelimit` | Pre-flight REST+GraphQL rate limit checks | **Yes** — budget awareness |

### 3.2 Existing Binaries with Overlapping Functionality

| Binary | What It Does | Reusable Pieces |
|---|---|---|
| `cmd/sig-auth-search` | GraphQL Search API across org | Search query builder, `ProjectItem` type, env var pattern |
| `cmd/sig-auth-projects` | Crawl org ProjectsV2, read custom field values | **Project field reading** — `fetchProjectItems()` with `fieldValues` fragments |
| `cmd/kube-enhancements` | REST API issue listing from `kubernetes/enhancements` | Milestone resolution, per-user filtering |

**Critical insight:** `cmd/sig-auth-projects` already has the GraphQL query for reading
ProjectV2 field values (Status, Stage, PRR, etc.). We need to adapt this to target
a specific project (#241) rather than scanning all org projects.

### 3.3 Gaps in Existing Code

1. **No "write custom fields to destination board" capability** — `pkg/board` can
   add items and link repos, but cannot set custom field values on the destination board
2. **No targeted project query** — existing code lists ALL projects in an org;
   we need to query a specific project by number
3. **No user-scoped search across repos** — existing search is label-based;
   we need `author:USER` / `assignee:USER` / `involves:USER` queries
4. **No private board creation** — `pkg/board.CreateProject` creates projects but
   doesn't set visibility; we need to set `public: false` on creation
5. **No cross-reference enrichment** — reading fields from board A to enrich items
   before adding to board B

---

## 4. Architecture

### 4.1 Binary Design: Subcommands

A single binary `cmd/sig-auth-team-board` with subcommands:

```
sig-auth-team-board enhancements   # Phase 1: KEPs from kubernetes/enhancements
sig-auth-team-board issues         # Phase 2: Issues/PRs from kubernetes/kubernetes
sig-auth-team-board sync           # Phase 3: Run both phases end-to-end
```

**Why subcommands?** Each phase can be run independently for debugging, and the combined
`sync` command runs the full pipeline. This matches the user's suggestion: "If it is
easiest to create one binary with several subcommands, that is great."

### 4.2 Data Flow

```
┌──────────────────────────────────────────────────────────────────┐
│                    sig-auth-team-board sync                       │
│                                                                  │
│  ┌─────────────────────────┐    ┌──────────────────────────────┐ │
│  │  enhancements subcommand│    │    issues subcommand         │ │
│  │                         │    │                              │ │
│  │  1. Search KEPs by:     │    │  1. For each GITHUB_USERNAME:│ │
│  │     - sig/auth label    │    │     - search k/k for author: │ │
│  │       + milestone       │    │       assignee: involves:    │ │
│  │     - GITHUB_USERNAMES  │    │     - filter by milestone    │ │
│  │       as author/assignee│    │     - filter by SIG labels   │ │
│  │                         │    │                              │ │
│  │  2. Read fields from    │    │  2. Deduplicate with phase 1 │ │
│  │     board #241 for each │    │                              │ │
│  │     matched KEP         │    │                              │ │
│  │                         │    │                              │ │
│  │  3. Output enriched     │    │  3. Output items             │ │
│  │     items               │    │                              │ │
│  └───────────┬─────────────┘    └──────────┬───────────────────┘ │
│              │                              │                     │
│              └──────────┬───────────────────┘                     │
│                         ▼                                         │
│              ┌──────────────────────────┐                         │
│              │  Merge & Deduplicate     │                         │
│              │  (by content node ID)    │                         │
│              └──────────┬───────────────┘                         │
│                         ▼                                         │
│              ┌──────────────────────────┐                         │
│              │  Write to PRIVATE board  │                         │
│              │  (find/create, add items,│                         │
│              │  set custom fields,      │                         │
│              │  link repos, sync stale) │                         │
│              └──────────────────────────┘                         │
└──────────────────────────────────────────────────────────────────┘
```

### 4.3 File Layout

```
cmd/sig-auth-team-board/
├── main.go              # CLI entry, subcommand dispatch, env loading
├── config.go            # Config struct, env parsing, validation
├── enhancements.go      # Phase 1: KEP discovery + board field enrichment
├── issues.go            # Phase 2: k/k issues/PRs by team members
├── board_writer.go      # Destination board: create private, add items, set fields
├── cost.go              # Estimate API costs before execution
└── types.go             # Shared types: EnrichedItem, FieldMap, etc.

.env/
├── sig-auth-team-board.env.example  # New env template
```

---

## 5. Detailed Design

### 5.1 Configuration (Environment Variables)

```bash
# --- Authentication ---
export GITHUB_TOKEN=ghp_...             # Classic PAT (read:org, read:project, repo, project)

# --- Team Scoping ---
export CORE_TEAM=benjaminapetersen,enj,aramase,stlaz,pmengelbert,ritazh
export OTHERS_OF_INTEREST=liggitt,deads2k,ahmredtd,luxas
export GITHUB_USERNAMES="${CORE_TEAM},${OTHERS_OF_INTEREST}"

# --- Source: Enhancements ---
export GITHUB_ORG=kubernetes
export ENHANCEMENTS_REPO=kubernetes/enhancements
export GITHUB_MILESTONE=v1.36
export GITHUB_LABELS=sig/auth,sig/security,sig/node,sig/api-machinery,sig/architecture,sig/scheduling
export ENHANCEMENTS_BOARD_NUMBER=241     # Official enhancements tracking board

# --- Exclusions (applied to BOTH phases) ---
# States to exclude from search results (server-side, default: closed)
export GITHUB_EXCLUDE_STATES=closed
# Labels to exclude (server-side, applied as -label: qualifiers)
export GITHUB_EXCLUDE_LABELS=lifecycle/stale,lifecycle/rotten
# Board Status field values to exclude when reading from board #241
# Items with these statuses are dropped after enrichment
export GITHUB_EXCLUDE_STATUSES=Deferred,"Removed from Milestone"

# --- Source: Issues/PRs ---
export ISSUES_REPO=kubernetes/kubernetes
# Reuses GITHUB_MILESTONE, GITHUB_USERNAMES, GITHUB_EXCLUDE_*
# SIG labels to include for the k/k issues search:
export ISSUES_SIG_LABELS=sig/auth,sig/security,sig/node,sig/api-machinery,sig/architecture,sig/scheduling,sig/cli

# --- Destination Board ---
export GITHUB_DEST_BOARD_OWNER=benjaminapetersen    # Personal user (private board)
export GITHUB_DEST_BOARD_NAME=SIG Auth Team v1.36
export GITHUB_LINK_REPOS=kubernetes/enhancements,kubernetes/kubernetes

# --- Behavior ---
# --output=cli|board (default: cli for dry-run)
# --use-cache=true|false (default: no flag = dry-run)
# --sync (remove stale items from dest board)
```

### 5.2 Phase 1: `enhancements` Subcommand

**Goal:** Find all KEPs relevant to our team for the current milestone, enriched
with tracking data from the official enhancements board (#241).

**Step 1 — Discover KEPs (GraphQL Search API)**

Run multiple search queries to capture KEPs by both label and author:

```
# Query A: sig/auth KEPs for the milestone (regardless of author)
# Server-side exclusions: state, -label: for lifecycle/stale, lifecycle/rotten
repo:kubernetes/enhancements is:issue state:open milestone:v1.36 label:sig/auth -label:lifecycle/stale -label:lifecycle/rotten

# Query B: Each team member's KEPs in the milestone (regardless of sig label)
repo:kubernetes/enhancements is:issue state:open milestone:v1.36 author:enj -label:lifecycle/stale -label:lifecycle/rotten
repo:kubernetes/enhancements is:issue state:open milestone:v1.36 author:aramase -label:lifecycle/stale -label:lifecycle/rotten
repo:kubernetes/enhancements is:issue state:open milestone:v1.36 author:stlaz -label:lifecycle/stale -label:lifecycle/rotten
... (for each GITHUB_USERNAME)

# Query C: Each team member's assigned KEPs (catches cases where they're
# assignee but not author, and where assignee: is more accurate than involves:)
repo:kubernetes/enhancements is:issue state:open milestone:v1.36 assignee:enj -label:lifecycle/stale -label:lifecycle/rotten
repo:kubernetes/enhancements is:issue state:open milestone:v1.36 assignee:aramase -label:lifecycle/stale -label:lifecycle/rotten
...
```

All queries include server-side narrowing from `GITHUB_EXCLUDE_STATES` (via `state:` qualifier)
and `GITHUB_EXCLUDE_LABELS` (via `-label:` qualifiers). These are built dynamically from
the env vars, not hardcoded.

Deduplicate results by node ID. This gives us a set of KEP issues.

**Why both label-based and author-based queries?**
- `sig/auth` label catches all auth-related KEPs even if driven by someone outside our team
- Author/assignee queries catch team members' KEPs under OTHER SIGs:
  - enj + stlaz on "Storage Version Migrator" (#4192) tagged `sig/api-machinery`
  - aramase working on scheduling-related items tagged `sig/scheduling`
  - luxas on "Conditional Authorization" (#5681) tagged `sig/auth`

**Step 2 — Enrich from Enhancements Board #241 (GraphQL Projects API)**

For each discovered KEP, look it up on the enhancements tracking board to read
its custom field values:

```graphql
query($projectId: ID!, $cursor: String) {
  node(id: $projectId) {
    ... on ProjectV2 {
      items(first: 100, after: $cursor) {
        pageInfo { hasNextPage endCursor }
        nodes {
          id
          fieldValues(first: 20) {
            nodes {
              ... on ProjectV2ItemFieldSingleSelectValue {
                field { ... on ProjectV2SingleSelectField { name } }
                name  # the selected option name
              }
              ... on ProjectV2ItemFieldTextValue {
                field { ... on ProjectV2FieldCommon { name } }
                text
              }
              # ... other field types as needed
            }
          }
          content {
            ... on Issue {
              id
              number
              title
              url
              state
              milestone { title }
              labels(first: 20) { nodes { name } }
              assignees(first: 20) { nodes { login } }
            }
          }
        }
      }
    }
  }
}
```

**Approach options for enrichment:**

- **Option A: Full board scan** — Fetch all items from board #241 into a map keyed
  by issue number, then look up each discovered KEP. The board typically has
  ~100-200 items per milestone, so this is 1-3 paginated queries. **Preferred** —
  simple, fast, and we already have this query pattern in `cmd/sig-auth-projects`.

- **Option B: Per-item lookup** — For each discovered KEP, query the board for
  that specific item. This would require a different query structure and is
  N+1 calls. **Not preferred.**

**Step 3 — Output EnrichedItems**

Each item carries both its native issue metadata and the board field values:

```go
type EnrichedItem struct {
    // Issue metadata
    NodeID     string
    Number     int
    Title      string
    URL        string
    State      string
    Repo       string
    Author     string
    Milestone  string
    Labels     []string
    Assignees  []string

    // Board #241 enrichment fields
    TrackingStatus   string // "At risk for enhancements freeze", "Tracked", etc.
    Stage            string // "Alpha", "Beta", "Stable", etc.
    PRRStatus        string // "Approved", "In process", "No need"
    EnhancementType  string // "Net New", "Major Change", "Graduating", etc.
    SIG              string // "sig-auth", "sig-api-machinery", etc.
    EnhancementsContact string
    ExceptionRequest string
}
```

### 5.3 Phase 2: `issues` Subcommand

**Goal:** Find issues and PRs in `kubernetes/kubernetes` that our team members are
actively working on for this milestone.

**Step 1 — Search by Team Members**

```
# For each GITHUB_USERNAME, search k/k:
# Server-side narrowing: state, milestone, -label: exclusions
repo:kubernetes/kubernetes state:open milestone:v1.36 involves:enj -label:lifecycle/stale -label:lifecycle/rotten
repo:kubernetes/kubernetes state:open milestone:v1.36 involves:aramase -label:lifecycle/stale -label:lifecycle/rotten
...
```

**Narrowing strategy for prolific contributors** (e.g., liggitt, deads2k):

The `involves:` qualifier is broad — it matches author, assignee, commenter, and
mentioned. For someone like liggitt who comments on hundreds of issues, this can
return a very large result set. We apply multiple layers of narrowing:

1. **Server-side (in the search query):**
   - `state:open` — drop all closed/merged items (from `GITHUB_EXCLUDE_STATES`)
   - `milestone:v1.36` — only current release cycle
   - `-label:lifecycle/stale` `-label:lifecycle/rotten` (from `GITHUB_EXCLUDE_LABELS`)
   - Each SIG label as a separate query: `label:sig/auth involves:liggitt` rather
     than just `involves:liggitt` — this is the most effective narrowing since it
     limits results to work in our SIGs of interest

2. **Client-side (post-fetch):**
   - SIG label filter: item must match at least one `ISSUES_SIG_LABELS`
   - Deduplicate with Phase 1 results (by node ID)
   - Exclude board statuses in `GITHUB_EXCLUDE_STATUSES` (if item was enriched)

**Note on `involves:` vs `author:` + `assignee:`** — We offer a config option:
- `ISSUES_SEARCH_QUALIFIER=involves` (default, broader but noisier)
- `ISSUES_SEARCH_QUALIFIER=author,assignee` (narrower, 2x queries but fewer false positives)

For contributors like liggitt, `author,assignee` may be more practical since
they comment on issues they don't own. The tool can also run both strategies
selectively — `involves` for CORE_TEAM members, `author,assignee` for
OTHERS_OF_INTEREST — but that adds complexity.

**Recommended query structure for maximum narrowing** (label-scoped per-user):

```
# Instead of one broad query per user, scope by SIG label:
repo:kubernetes/kubernetes state:open milestone:v1.36 label:sig/auth involves:liggitt -label:lifecycle/stale -label:lifecycle/rotten
repo:kubernetes/kubernetes state:open milestone:v1.36 label:sig/node involves:liggitt -label:lifecycle/stale -label:lifecycle/rotten
... (for each ISSUES_SIG_LABELS × GITHUB_USERNAMES)
```

This multiplies queries (users × labels) but each query returns a tight,
relevant set. With 10 users and 7 SIG labels = 70 queries at 1 point each = 70
points — still well within budget. Results are deduplicated by node ID.

**Step 2 — Filter client-side**

- Filter by SIG labels if `ISSUES_SIG_LABELS` is set (any item matching at least
  one listed label passes) — this is a safety net; the label-scoped queries above
  already handle this server-side
- Exclude items already captured in Phase 1 (deduplicate by node ID)
- Exclude items whose board #241 status is in `GITHUB_EXCLUDE_STATUSES`
  (e.g., "Deferred", "Removed from Milestone")

**Step 3 — Output as items** (same structure, but without board enrichment fields
since these are k/k issues, not enhancements board items)

### 5.4 Phase 3: Destination Board Writer (`board_writer.go`)

**Private Board Creation**

The GraphQL mutation `createProjectV2` supports a `repositoryId` parameter but
for org-level projects we use `ownerId`. To make the board private, we can either:

1. **Create as private by default** — User-owned projects (under personal account)
   are private by default. Org-owned projects depend on org settings.
2. **Update visibility after creation** — Use `updateProjectV2` mutation:
   ```graphql
   mutation($projectId: ID!) {
     updateProjectV2(input: { projectId: $projectId, public: false }) {
       projectV2 { id title public }
     }
   }
   ```

**Recommendation:** Create the board under `GITHUB_DEST_BOARD_OWNER` (personal user account).
Personal project boards are private by default. After creation, verify and explicitly
set `public: false` to be safe.

**Adding Items**

Use existing `pkg/board.UpdateBoard()` flow:
1. Find or create the destination board
2. Fetch existing items (dedup)
3. Add new items via `addProjectV2ItemById`
4. Link repos
5. Optionally remove stale items (`--sync`)

**Setting Custom Fields on Destination Board**

This is the main new capability needed. After adding an item, we want to set
fields on our private board that mirror the enrichment data from board #241.

We need to:
1. **Create custom fields** on the destination board (or ensure they exist):
   - Status (SingleSelect with options matching board #241)
   - Stage (SingleSelect: Alpha, Beta, Stable, Deprecation/Removal)
   - PRR Status (SingleSelect: Approved, In process, No need)
   - Enhancement Type (SingleSelect: Net New, Major Change, Graduating, Deprecation, Docs)
   - SIG (SingleSelect with relevant SIG options)
   - Source (SingleSelect: Enhancements, k/k Issues — to distinguish phases)

2. **Write field values** after adding each item:
   ```graphql
   mutation($projectId: ID!, $itemId: ID!, $fieldId: ID!, $singleSelectOptionId: String!) {
     updateProjectV2ItemFieldValue(input: {
       projectId: $projectId
       itemId: $itemId
       fieldId: $fieldId
       value: { singleSelectOptionId: $singleSelectOptionId }
     }) {
       projectV2Item { id }
     }
   }
   ```

**Open question:** Should we create the custom fields programmatically, or ask the
user to set them up manually on the private board? The GraphQL API for creating
ProjectV2 fields is:
```graphql
mutation {
  createProjectV2Field(input: {
    projectId: $projectId
    dataType: SINGLE_SELECT
    name: "Stage"
    singleSelectOptions: [
      { name: "Alpha", color: BLUE }
      { name: "Beta", color: YELLOW }
      { name: "Stable", color: GREEN }
      { name: "Deprecation/Removal", color: RED }
    ]
  }) { ... }
}
```

**Recommendation:** Create fields programmatically on first run if they don't exist.
This makes the tool self-contained. Cache field IDs after creation for subsequent runs.

---

## 6. New Shared Code (`pkg/` Enhancements)

### 6.1 `pkg/board` Additions

```go
// New functions needed in pkg/board:

// FindProjectByNumber queries a specific project by org + number
// (existing code only finds by name via listing all projects)
func FindProjectByNumber(gql *ghgql.Client, org string, number int) (*Info, error)

// EnsurePrivate sets a project to private
func EnsurePrivate(gql *ghgql.Client, projectID string) error

// GetProjectFields returns all field definitions for a project
// (field ID, name, type, and for SingleSelect: option IDs + names)
func GetProjectFields(gql *ghgql.Client, projectID string) ([]FieldDef, error)

// CreateField creates a new field on a project (SingleSelect, Text, etc.)
func CreateField(gql *ghgql.Client, projectID string, def FieldDef) (string, error)

// EnsureFields creates fields if they don't exist, returns a FieldMap
// mapping field names to their IDs and option IDs
func EnsureFields(gql *ghgql.Client, projectID string, desired []FieldDef) (FieldMap, error)

// UpdateItemField sets a field value (SingleSelect, Text, etc.) on a project item
func UpdateItemField(gql *ghgql.Client, projectID, itemID, fieldID string, value FieldValue) error

// New types:
type FieldDef struct {
    ID      string
    Name    string
    Type    string // SINGLE_SELECT, TEXT, etc.
    Options []FieldOption
}

type FieldOption struct {
    ID   string
    Name string
}

type FieldMap map[string]FieldDef  // keyed by field name

type FieldValue struct {
    SingleSelectOptionID string
    Text                 string
    // ... other types as needed
}
```

### 6.2 `pkg/board` — Get Project by Number Query

```graphql
query($org: String!, $number: Int!) {
  organization(login: $org) {
    projectV2(number: $number) {
      id
      title
      number
      url
      public
      fields(first: 50) {
        nodes {
          ... on ProjectV2SingleSelectField {
            id
            name
            options { id name }
          }
          ... on ProjectV2FieldCommon {
            id
            name
          }
        }
      }
    }
  }
}
```

---

## 7. Implementation Plan

### Phase 0: Prep (before writing new cmd)

- [x] Fix `.env.example` files to use `export` prefix
- [ ] Add `FindProjectByNumber()` to `pkg/board`
- [ ] Add `UpdateItemField()` to `pkg/board`
- [ ] Add `GetProjectFields()` to `pkg/board`
- [ ] Add `EnsurePrivate()` to `pkg/board`
- [ ] Test these with existing `cmd/check-rate-limits` or a small script

### Phase 1: Core Scaffolding

- [ ] Create `cmd/sig-auth-team-board/main.go` with subcommand dispatch
- [ ] Create `config.go` with env parsing
- [ ] Create `types.go` with `EnrichedItem` type
- [ ] Create `cost.go` with API cost estimation
- [ ] Create `.env/sig-auth-team-board.env.example`
- [ ] Add to Makefile

### Phase 2: `enhancements` Subcommand

- [ ] Implement KEP discovery (GraphQL Search: label-based + user-based queries)
- [ ] Implement board #241 enrichment (fetch all items, build lookup map)
- [ ] Merge and deduplicate
- [ ] CLI output mode (print enriched items with board fields)
- [ ] Board output mode (write to destination board with custom fields)

### Phase 3: `issues` Subcommand

- [ ] Implement user-scoped search across `kubernetes/kubernetes`
- [ ] Client-side SIG label filtering
- [ ] Deduplication with Phase 2 results
- [ ] CLI and board output modes

### Phase 4: `sync` Subcommand

- [ ] Orchestrate enhancements → issues → merge → write
- [ ] Stale item removal on destination board
- [ ] Full end-to-end testing

### Future Iteration (TODO — not this phase)

- [ ] **Fan-out from KEP descriptions** — Parse enhancement issue body to find
  linked PRs and implementation issues (follow `k/k` PR links mentioned in the
  KEP tracking issue)
- [ ] **Periodic refresh** — Script or cron to re-run sync, updating board field
  values as the enhancements team updates board #241
- [ ] **Diff reporting** — Show what changed since last run (new items, status changes)
- [ ] **Additional repos** — Extend to `kubernetes-sigs/*` repos for SIG-Auth subprojects

---

## 8. API Cost Estimates

### Phase 1 (Enhancements)

| Operation | Queries | Points Each | Total |
|---|---|---|---|
| Rate limit check | 1 REST + 1 GQL | 0 + 1 | 1 |
| Search: sig/auth KEPs | 1 | 1 | 1 |
| Search: per-user author queries | ~10 users | 1 | 10 |
| Search: per-user assignee queries | ~10 users | 1 | 10 |
| Fetch board #241 items | 1-3 pages | 1 | 3 |
| **Subtotal (read)** | | | **~25** |
| Add items to dest board | ~20 items | 1 | 20 |
| Set fields per item | ~20 × 5 fields | 1 | 100 |
| **Subtotal (write)** | | | **~120** |
| **Phase 1 total** | | | **~145** |

### Phase 2 (Issues)

| Operation | Queries | Points Each | Total |
|---|---|---|---|
| Search: label-scoped per-user queries | ~10 users × ~7 labels | 1 | 70 |
| Add items to dest board | ~30 items | 1 | 30 |
| Set fields per item | ~30 × 2 fields | 1 | 60 |
| **Phase 2 total** | | | **~160** |

### Total per run: ~305 points

GraphQL rate limit: 5,000 points/hour. This tool uses ~6% of the budget per run.
Comfortable margin for development iteration. The label-scoped query strategy
(users × labels) costs more points but returns far tighter results — especially
important for prolific contributors like liggitt who would otherwise flood the
results with every issue they've ever commented on.

---

## 9. Example CLI Usage

```bash
# Load environment
source .env/sig-auth-team-board.env

# Dry-run: see what KEPs would be found (no API calls, uses cache)
go run ./cmd/sig-auth-team-board enhancements

# Live fetch, print to CLI
go run ./cmd/sig-auth-team-board enhancements --use-cache=false --output=cli

# Live fetch, write to private board
go run ./cmd/sig-auth-team-board enhancements --use-cache=false --output=board

# Full sync: enhancements + issues → private board
go run ./cmd/sig-auth-team-board sync --use-cache=false --output=board --sync

# Check rate limits first
go run ./cmd/check-rate-limits
```

---

## 10. Key Design Decisions

### 10.1 Private Board — User-Owned vs Org-Owned

**Decision:** User-owned (under `GITHUB_DEST_BOARD_OWNER` personal account).
- Personal boards are private by default
- No org admin permissions needed
- The `addProjectV2ItemById` mutation adds a reference to the issue/PR on our
  board WITHOUT creating a timeline event on the source issue (confirmed: project
  board additions from external projects do not notify the issue)

### 10.2 GraphQL Library — Raw HTTP vs `shurcooL/githubv4`

**Decision:** Continue using raw HTTP via `pkg/ghgql` (our existing pattern).
- We already have a working GraphQL client
- Adding `shurcooL/githubv4` would be a second GraphQL approach
- Raw queries are easier to debug and adjust
- The sig-auth-tools reference uses `shurcooL/githubv4` but we don't need to match

### 10.3 Field Sync Strategy

**Decision:** "Ensure and set" — on each run:
1. Read field definitions from destination board
2. Create any missing fields (idempotent)
3. For each added item, set field values (overwrite)

This means re-running the tool updates field values from board #241,
keeping our private board in sync with the official tracking state.

### 10.4 Subcommand Framework

**Decision:** Simple `os.Args` dispatch (no third-party CLI framework).
- Matches existing binaries' patterns
- `flag` package for `--output`, `--use-cache`, `--sync`
- First non-flag arg is the subcommand

---

## 11. Open Questions

1. **Should we also capture draft PRs?** — Draft PRs in `kubernetes/kubernetes`
   often represent in-progress implementation. They wouldn't be on the enhancements
   board but might be valuable for the k/k issues phase.

2. **How to handle items that exist on our board but are no longer in any query
   results?** — The `--sync` flag removes stale items. But should we keep items
   that were "Deferred" or "Removed from Milestone" on board #241? Probably yes,
   with their status updated. Decision: sync removes items that are CLOSED or
   no longer returned by any query, but keeps items with updated statuses.

3. **Rate limit budget per subcommand vs global?** — Each subcommand checks rate
   limits independently. The `sync` command should do a single check at the start.

4. **Token scopes for private board creation** — Classic PAT needs `project` scope
   (full read/write to Projects V2), not just `read:project`. Verify the user's
   token has this scope. The fine-grained PAT does NOT work for Projects V2 API
   (confirmed known limitation).

5. **Multiple milestones?** — Should we support tracking across v1.35 + v1.36
   simultaneously? For now, single milestone. Can extend later.

---

## 12. .env Template Draft

```bash
# sig-auth-team-board environment configuration
# Usage: source .env/sig-auth-team-board.env

# --- Authentication ---
# Classic PAT with scopes: repo, read:org, project
# NOTE: Fine-grained PATs do NOT work with Projects V2 API
export GITHUB_TOKEN=ghp_your_token_here

# --- Organization ---
export GITHUB_ORG=kubernetes

# --- Team Members ---
# Core team (Red Hat SIG-Auth sub-team)
export CORE_TEAM=benjaminapetersen,enj,aramase,stlaz,pmengelbert,ritazh
# Others of interest (SIG-Auth leads/regulars from other companies)
export OTHERS_OF_INTEREST=liggitt,deads2k,ahmredtd,luxas
# Combined (used for queries)
export GITHUB_USERNAMES="${CORE_TEAM},${OTHERS_OF_INTEREST}"

# --- Enhancements Phase ---
export ENHANCEMENTS_REPO=kubernetes/enhancements
export GITHUB_MILESTONE=v1.36
# Labels for the label-based KEP search (finds sig/auth KEPs by anyone)
export ENHANCEMENTS_LABELS=sig/auth
# Official enhancements tracking board number (for field enrichment)
export ENHANCEMENTS_BOARD_ORG=kubernetes
export ENHANCEMENTS_BOARD_NUMBER=241

# --- Exclusions (applied to both phases) ---
# States to exclude from search (server-side via state: qualifier, default: closed)
export GITHUB_EXCLUDE_STATES=closed
# Labels to exclude (server-side, each becomes a -label: qualifier)
export GITHUB_EXCLUDE_LABELS=lifecycle/stale,lifecycle/rotten
# Board #241 status values to drop after enrichment (client-side)
export GITHUB_EXCLUDE_STATUSES=Deferred,"Removed from Milestone"

# --- Issues Phase ---
export ISSUES_REPO=kubernetes/kubernetes
# SIG labels of interest for k/k issues (any match passes)
# Also used for label-scoped per-user queries to narrow results for prolific contributors
export ISSUES_SIG_LABELS=sig/auth,sig/security,sig/node,sig/api-machinery,sig/architecture,sig/scheduling,sig/cli
# Search qualifier for user matching: involves, author, assignee
# For prolific contributors, author,assignee is much narrower than involves
export ISSUES_SEARCH_QUALIFIER=involves

# --- Destination Board ---
export GITHUB_DEST_BOARD_OWNER=benjaminapetersen
export GITHUB_DEST_BOARD_NAME=SIG Auth Team v1.36
export GITHUB_LINK_REPOS=kubernetes/enhancements,kubernetes/kubernetes
```
