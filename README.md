# kube-board

A CLI tool for building a **private GitHub Projects V2 board** that
consolidates a Kubernetes team's work for a release cycle.

It discovers KEPs from `kubernetes/enhancements`, enriches them with tracking
fields from the official enhancements board (#241), finds issues/PRs from
`kubernetes/kubernetes` by team members, and writes everything to a single
private board with custom fields preserved.

## Why?

The public enhancements board (#241) tracks all SIGs.  A SIG lead needs a
filtered, team-scoped view showing only their team's work — plus k/k issues
that never appear on the enhancements board at all.  This tool builds that
view automatically and keeps it in sync.

## Project Layout

```
├── cmd/
│   └── kube-board/          Single CLI binary with subcommands
├── pkg/
│   ├── ghgql/               Shared GraphQL HTTP client
│   ├── board/               Shared Projects V2 CRUD
│   ├── cache/               Generic JSON file caching
│   └── ratelimit/           Rate limit checking & display
├── deploy/                  Kubernetes CronJob manifests
│   └── chart/kube-board/    Helm chart (optional)
├── .env/                    Environment config examples
├── bin/                     Build output (gitignored)
├── _output/                 Markdown reports (gitignored)
├── Dockerfile               Multi-stage container build
├── Makefile
├── go.mod                   Single module
└── README.md
```

## Architecture

```
                             ┌─────────────────────────────────┐
                             │   kubernetes/enhancements repo  │
                             │   (KEPs as issues, milestone)   │
                             └────────────┬────────────────────┘
                                          │ GitHub Search API
                                          ▼
┌──────────────────────┐    ┌──────────────────────────────────┐
│  Enhancements Board  │───▶│  1. Discover KEPs                │
│  #241 (public)       │    │  2. Enrich from board #241       │
│  Status, Stage,      │    │  3. Filter excluded statuses     │
│  PRR, SIG, ...       │    └────────────┬─────────────────────┘
└──────────────────────┘                 │
                                         │  merged +
                                         │  deduplicated
┌──────────────────────┐                 ▼
│  kubernetes/kubernetes│    ┌──────────────────────────────────┐
│  (issues & PRs)       │──▶│  4. Fetch issues/PRs by team     │
└──────────────────────┘    │  5. SIG-label scoped narrowing   │
                            └────────────┬─────────────────────┘
                                         │
                                         ▼
                            ┌──────────────────────────────────┐
                            │  Private Destination Board       │
                            │  (--output=board)                │
                            │  or CLI table (--output=cli)     │
                            └──────────────────────────────────┘
```

## Subcommands

| Subcommand                | Description |
|---------------------------|-------------|
| `check-budget`            | Check current GitHub API rate-limit / budget status |
| `cache-clean`             | Remove old cache files (keeps N newest per prefix, default 5) |
| `sync-enhancement-board`  | Discover KEPs, enrich from source board (#241), sync configured fields to destination board |
| `sync-org-items`          | Search across orgs for issues/PRs by team members, output to CLI or board |
| `sync`                    | Run both phases, merge/deduplicate, and write the combined set |

Legacy aliases `enhancements`, `issues`, and `sync-orgs` still work but print a deprecation warning.

## Flags

| Flag              | Default            | Description |
|-------------------|--------------------|-------------|
| `--output`        | `cli`              | Output mode: `cli` (print to terminal), `board` (write to GitHub Project), or `markdown` (write markdown file) |
| `--markdown-file` | `_output/board.md` | Path for markdown output file (used with `--output=markdown`) |
| `--use-cache`     | _(omit)_           | `true` = use cached JSON, `false` = fetch live. Omit for dry-run |
| `--sync`          | `false`            | Remove stale items from the destination board |
| `--cache-limit`   | `5`                | Keep only N newest cache files per prefix (always enforced, raise for more) |

## Quick Start

Run the binary locally, with a `.env` file for configuration.

```bash
# 1. Copy and fill in the env file
cp .env/kube-board.env.example .env/kube-board.env
# edit .env/kube-board.env with your token and team members

# 2. Source the env
# The .env file contains a number of variables relevant to the following commands.
source .env/kube-board.env
```

Build and run the binary:

```bash
# 3. Build
make build

# 4. Dry-run (no API calls — just shows config + estimated cost)
./bin/kube-board sync-enhancement-board

# 5. Sync enhancements: fetch from source board, write configured fields to dest board
./bin/kube-board sync-enhancement-board --use-cache=false --output=board

# 6. Use cached data (faster iteration)
./bin/kube-board sync-enhancement-board --use-cache=true

# 7. Org search only (issues/PRs by team members across orgs)
./bin/kube-board sync-org-items --use-cache=false

# 8. Org search, write to board
./bin/kube-board sync-org-items --use-cache=false --output=board

# 9. Full sync: fetch both phases + write to private board
./bin/kube-board sync --use-cache=false --output=board --sync

# 10. Write markdown report (uses cached data)
./bin/kube-board sync --use-cache=true --output=markdown

# 11. Markdown report to custom file
./bin/kube-board sync --use-cache=true --output=markdown --markdown-file=report.md

## 12. Log file for stdout & stderr, convenience for review
./bin/kube-board sync --log-file=logs.txt
```

## Expected CLI Output

When run with `--output=cli`, the tool prints enriched items:

```
=== Enhancements (KEPs) ===
Found 12 item(s)

[Issue] #4193  KEP-4193: Bound service account token improvements
         Author:    enj
         Assignees: enj, liggitt
         URL:       https://github.com/kubernetes/enhancements/issues/4193
         Repo:      kubernetes/enhancements
         Labels:    sig/auth, stage/stable
         Milestone: v1.36
         Source:    enhancements
         Status:    Tracked
         Stage:     Stable
         PRR:       Approved

[Issue] #3299  KEP-3299: KMS v2
         Author:    aramase
         URL:       https://github.com/kubernetes/enhancements/issues/3299
         Repo:      kubernetes/enhancements
         Labels:    sig/auth, stage/beta
         Milestone: v1.36
         Source:    enhancements
         Status:    At Risk
         Stage:     Beta
         PRR:       Provisional
         SIG:       sig-auth

...
```

## Expected Board Layout

When using `--output=board`, a private GitHub Projects V2 board is created (or
updated) with the following structure:

```
┌────────────────────────────────────────────────────────────────────────────────────────────────────────────────────┐
│  📋  SIG Auth 1.36 Team Board  (PRIVATE)                                                      [Table View]       │
├─────┬────────────────────────────────────────┬─────────┬────────┬─────────┬──────┬──────┬───────┬────────┬────────┤
│  #  │ Title                                  │ Status  │ Stage  │ PRR     │ Type │ SIG  │I.Auth │Updated │Source  │
├─────┼────────────────────────────────────────┼─────────┼────────┼─────────┼──────┼──────┼───────┼────────┼────────┤
│4193 │ KEP: Bound SA token improvements       │ Tracked │ Stable │Approved │ feat │ auth │ enj   │2026-02 │ enh    │
│3299 │ KEP: KMS v2                            │ At Risk │ Beta   │Provis.  │ feat │ auth │aramase│2026-01 │ enh    │
│4837 │ KEP: Structured auth config            │ Tracked │ GA     │Approved │ feat │ auth │ enj   │2026-02 │ enh    │
│4214 │ KEP: Coordinated leader election       │ Tracked │ Alpha  │Approved │ feat │ node │ stlaz │2025-12 │ enh    │
├─────┼────────────────────────────────────────┼─────────┼────────┼─────────┼──────┼──────┼───────┼────────┼────────┤
│ 123 │ Fix RBAC escalation check              │         │        │         │      │ auth │liggitt│2026-02 │issues  │
│ 456 │ Pod security admission: warn on mount  │         │        │         │      │ auth │ enj   │2026-01 │issues  │
│ 789 │ SA token cleanup controller flake      │         │        │         │      │ auth │aramase│2026-02 │issues  │
│1234 │ [PR] Structured authz: add cel metrics │         │        │         │      │ auth │ stlaz │2026-02 │issues  │
└─────┴────────────────────────────────────────┴─────────┴────────┴─────────┴──────┴──────┴───────┴────────┴────────┘

Automatic Fields (always present, no configuration needed):
  • Item Author   — GitHub username of the issue/PR creator ("Author" is reserved by GitHub)
  • Last Updated  — DATE field: last updated date (YYYY-MM-DD), enables native date filtering
  • Source        — "enhancements" or "issues"

Sync Fields (from source board, configurable via GITHUB_KUBERNETES_RELEASE_SYNC_BOARD_FIELDS):
  • Tracking Status — from board #241: Tracked, At Risk, Deferred, ...
  • Stage           — Alpha, Beta, Stable/GA
  • PRR Status      — Provisional, Implementable, Approved
  • Enhancement Type— feature, deprecation, ...
  • SIG             — sig-auth, sig-node, ...
  • Milestone       — v1.36, v1.37, ...
  • (any passthrough field name from the source board)

Custom Fields (user-defined via GITHUB_DEST_BOARD_CUSTOM_FIELDS):
  • Stream, Priority, Notes, Epic, Bet, Status, etc.
```

KEPs discovered via the enhancements phase carry enriched fields from board
#241.  Issues/PRs from k/k will have Source = "issues" but no board-enrichment
fields (those columns remain blank).

## Search Strategy

### sync-enhancement-board Phase
Three query families, all scoped to `repo:kubernetes/enhancements state:open milestone:"v1.36"`:

1. **Label-based** — `label:"sig/auth"` catches all SIG-labeled KEPs
2. **Per-user author** — catches cross-SIG KEPs authored by team members
3. **Per-user assignee** — catches KEPs assigned to team members

Results are deduplicated by node ID, then enriched by scanning board #241's
items and mapping field values (Status, Stage, PRR, etc.) onto each KEP.

### sync-org-items Phase
Searches across configured orgs (`GITHUB_ADDITIONAL_ORGS`) for issues/PRs by
team members.  For the primary org, label-scoped per-user queries are used.
For additional orgs, per-user queries with an optional time window
(`GITHUB_SEARCH_SINCE`) are used.

### sync (Combined) Phase
Runs both sync-enhancement-board and sync-org-items phases, deduplicates
(enhancements take priority for enrichment), and writes the merged set to the
private board.  With `--sync`, items on the board that are no longer in the
query results are removed.

## Cost Estimates

The tool prints estimated GraphQL API point usage before each run.
GitHub allows 5,000 points/hour.

| Scenario | Approx. Cost |
|----------|-------------|
| `sync-enhancement-board` (8 users, 1 label) | ~40 pts read |
| `sync-org-items` (8 users × 1 SIG label) | ~35 pts read |
| `sync --output=board` (80 items) | ~75 pts read + ~400 pts write |

## Caching

All fetched data is cached as JSON in `.cache/team-board/`:
- `enhancements_2025-01-15T10-30-00.json`
- `issues_2025-01-15T10-30-05.json`

Use `--use-cache=true` to skip API calls and re-process cached data.
Useful for iterating on output formatting or testing board writes with
previously fetched data.

### Cache Cleanup

A maximum of 5 cache files per prefix is maintained automatically after every
sync operation. Use `--cache-limit` to override:

```bash
# Keep more cache history
kube-board sync --use-cache=false --output=board --cache-limit=20

# Or clean manually
kube-board cache-clean
kube-board cache-clean --cache-limit=3
```

## Environment Variables

See [.env/kube-board.env.example](.env/kube-board.env.example)
for the full list with comments. Key variables:

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `GITHUB_TOKEN` | yes | — | Classic PAT with `read:org`, `read:project`, `repo`, `project` |
| `GITHUB_USERNAMES` | yes | — | Comma-separated team member GitHub handles |
| `GITHUB_ADDITIONAL_ORGS` | no | — | Additional GitHub orgs to search (comma-separated). `kubernetes` is always included. |
| `GITHUB_KUBERNETES_MILESTONE` | no | — | e.g., `v1.36` |
| `ENHANCEMENTS_REPO` | no | `kubernetes/enhancements` | Repo containing KEPs |
| `ENHANCEMENTS_LABELS` | no | `sig/auth` | Labels to discover KEPs |
| `GITHUB_KUBERNETES_RELEASE_SYNC_BOARD` | no | `kubernetes/projects/241` | Source board to sync fields from (format: `org/projects/num`) |
| `GITHUB_KUBERNETES_RELEASE_SYNC_BOARD_FIELDS` | no | all well-known | Comma-separated field keys to sync. Well-known: `TrackingStatus`, `Stage`, `PRRStatus`, `EnhancementType`, `SIG`, `Milestone`. Any other key is a **passthrough** (same name on source and dest, type auto-detected). |
| `ISSUES_REPO` | no | `kubernetes/kubernetes` | Repo to search for issues/PRs |
| `GITHUB_KUBERNETES_ISSUES_SIG_LABELS` | no | — | SIG labels for narrowing kubernetes org searches (legacy: `ISSUES_SIG_LABELS`) |
| `ISSUES_SEARCH_QUALIFIER` | no | `involves` | Search qualifier: `involves`, `author`, or `assignee` |
| `GITHUB_SEARCH_SINCE` | no | — | Time window for additional org searches (`Nd`, `Nw`, `Nm`, `Ny`; e.g., `6m` = 6 months, `1y` = 1 year). Omit for no limit. |
| `GITHUB_EXCLUDE_STATES` | no | `closed` | States to exclude server-side |
| `GITHUB_EXCLUDE_LABELS` | no | — | Labels to exclude server-side |
| `GITHUB_EXCLUDE_STATUSES` | no | — | Board status values to exclude client-side |
| `GITHUB_DEST_BOARD_OWNER` | board mode | — | User or org owning the destination board |
| `GITHUB_DEST_BOARD_NAME` | board mode | — | Title of the destination board |
| `GITHUB_DEST_BOARD_PRIVACY` | no | `private` | Board visibility: `private` or `public` |
| `GITHUB_DEST_BOARD_AUTHOR_FIELD_NAME` | no | `Item Author` | Name for the automatic Author field ("Author" is reserved by GitHub Projects) |
| `GITHUB_DEST_BOARD_CUSTOM_FIELDS` | no | — | Custom fields: `Name:Opt1\|Opt2,Name2` (colon = single-select, bare = text). See [Custom Fields](#custom-fields). |
| `GITHUB_DEST_BOARD_ADDITIONAL_VIEWS` | no | — | Views to auto-create: `ViewName=Field1,Field2` (one per line). See [Views](#views). |
| `GITHUB_AUTO_CUSTOM_FIELD_TO_REPO` | no | — | Auto-assign field values by repo: `Field:Value=glob,glob` (one per line). See [Auto-Assign Rules](#auto-assign-rules). |
| `GITHUB_LINK_REPOS` | no | — | Repos to link to the destination board (comma-separated) |

### Automatic Fields

Three fields are **always** created on the destination board — no configuration needed:

| Field | Type | Source | Description |
|-------|------|--------|-------------|
| **Source** | Single-select | Internal | `"enhancements"` or `"issues"` — which phase discovered the item |
| **Item Author** | Text | GitHub API | GitHub username of the issue/PR author ("Author" is a reserved name) |
| **Last Updated** | Date | GitHub API | Date the issue/PR was last updated (`YYYY-MM-DD`). Native DATE type enables date-based filtering and sorting in board views. |

### Passthrough Sync Fields

Any key in `GITHUB_KUBERNETES_RELEASE_SYNC_BOARD_FIELDS` that is not one of the
six well-known keys (`TrackingStatus`, `Stage`, `PRRStatus`, `EnhancementType`,
`SIG`, `Milestone`) is treated as a **passthrough field**:

- The config key is used as both the source and destination column name
- The field type is auto-detected from the source board (single-select, text, etc.)
- Single-select options are copied from the source board automatically

This allows syncing any field from the enhancements board without code changes.
For example, adding `Blog Status,PRR Assignee` to the sync fields list will
create those columns on the destination board with the same name and type as
the source.

### Custom Fields

Define additional fields on the destination board using `GITHUB_DEST_BOARD_CUSTOM_FIELDS`:

```bash
# Format: Name:Opt1|Opt2|Opt3, Name2
#   - Comma separates fields
#   - Colon separates name from single-select options
#   - Pipe separates option values
#   - No colon = TEXT field
export GITHUB_DEST_BOARD_CUSTOM_FIELDS="
  Status:🆕 New|📋 Backlog|🔖 Ready|🛑 Blocked|🏗 In progress|✅ Done,
  Stream:Upstream|Downstream|-,
  Priority:P0|P1|P2|P3,
  Notes,
  Epic:Feature A|Feature B|Feature C
"
```

### Views

Auto-create named views with specific visible columns using
`GITHUB_DEST_BOARD_ADDITIONAL_VIEWS`:

```bash
# Format: ViewName=Field1,Field2,Field3 (one per line)
#   - "=" separates view name from visible field names
#   - No "=" creates the view with default columns
# Views are created via the REST API for reliability.
# Existing view filters are preserved (not overwritten).
export GITHUB_DEST_BOARD_ADDITIONAL_VIEWS="
KEPs=Source,Status,Stage,PRR Status,SIG,Milestone
Current Issues=Stream,Priority,Notes
Done Last Month=Last Updated,Item Author,Notes
PRR=PRR Status,PRR Assignee
"
```

Shell variable composition works for shared column groups:

```bash
TRIMESTER="Epic,Bet,Stream"
export GITHUB_DEST_BOARD_ADDITIONAL_VIEWS="
KEPs=${TRIMESTER},Tracking Status,Stage
Issues=${TRIMESTER},Notes
"
```

### Auto-Assign Rules

Automatically set field values based on which repo an item belongs to:

```bash
# Format: FieldName:Value=repo_glob,repo_glob (one per line)
# Glob patterns match against "owner/name" (supports * and ?).
# First matching rule wins per field.
export GITHUB_AUTO_CUSTOM_FIELD_TO_REPO="
Stream:Upstream=kubernetes/*,kubernetes-sigs/*
Stream:Downstream=Azure/*
Priority:P1=kubernetes/kubernetes
"
```

## Shared Packages

- **pkg/ghgql** — Lightweight GitHub GraphQL client with OAuth2 auth, 429 handling
- **pkg/board** — GitHub Projects V2 operations: find/create projects, add/remove items, link repos
- **pkg/cache** — Generic JSON file caching with Go generics
- **pkg/ratelimit** — REST and GraphQL API rate limit checking, display, and warnings

## Authentication

All tools require a **Classic Personal Access Token** (PAT) with scopes:
- `read:org` — Read org membership
- `read:project` — Read project boards
- `repo` — Repository access
- `project` — Full project control (for board write operations)

> **Note:** Fine-grained PATs do NOT work with the Projects V2 API.

Set via environment variable:
```bash
export GITHUB_TOKEN=ghp_...
```

## Build

```bash
# Build the binary
make build

# Or directly
make kube-board

# Build the container image
make image
```

## Deploying on Kubernetes

The `deploy/` directory contains manifests to run kube-board as a CronJob.
The default schedule syncs **4 times per day**: midnight, 6 AM, noon, and 3 PM.

These instructions assume a local [Kind](https://kind.sigs.k8s.io/) cluster,
but the manifests work on any Kubernetes cluster.

### 1. Create a Kind cluster (if needed)

```bash
kind create cluster --name kube-board
```

### 2. Build and load the image

```bash
# Build the container image and load it into Kind
make kind-load
```

For a remote cluster, push to your registry instead:

```bash
make image
docker tag kube-board:latest ghcr.io/YOUR_USER/kube-board:latest
docker push ghcr.io/YOUR_USER/kube-board:latest
```

Then update `deploy/cronjob.yaml` to reference the pushed image.

### 3. Configure

Edit `deploy/configmap.yaml` with your team members, milestone, board
settings, etc.  At minimum set:

- `GITHUB_USERNAMES` — comma-separated GitHub handles
- `GITHUB_DEST_BOARD_OWNER` — owner of the private board
- `GITHUB_DEST_BOARD_NAME` — title of the private board

### 4. Create the secret

```bash
kubectl create namespace kube-board

kubectl -n kube-board create secret generic kube-board-token \
  --from-literal=GITHUB_TOKEN=ghp_YOUR_TOKEN_HERE
```

### 5. Deploy

```bash
make deploy

# Or manually:
kubectl apply -f deploy/
```

### 6. Verify

```bash
# Check the CronJob
kubectl -n kube-board get cronjobs

# Trigger a manual run
kubectl -n kube-board create job --from=cronjob/kube-board-sync kube-board-manual

# Watch the job
kubectl -n kube-board get pods -w

# Check logs
kubectl -n kube-board logs job/kube-board-manual
```

### Schedule

The default cron schedule is `0 0,6,12,15 * * *`:

| Run       | Time    |
|-----------|---------|
| Midnight  | 12:00 AM |
| Morning   | 6:00 AM  |
| Noon      | 12:00 PM |
| Afternoon | 3:00 PM  |

Edit the `schedule` field in `deploy/cronjob.yaml` (or `values.yaml` for Helm) to adjust.

### Helm Chart (optional)

A Helm chart is available at `deploy/chart/kube-board/` as an alternative to
the plain manifests. All configuration lives in a single `values.yaml`.

```bash
# Lint the chart
make helm-lint

# Preview the rendered templates
make helm-template

# Install (creates the namespace automatically)
make helm-install

# Or with custom values:
helm install kube-board deploy/chart/kube-board \
  -n kube-board --create-namespace \
  --set githubToken=ghp_YOUR_TOKEN \
  --set config.GITHUB_USERNAMES="user1,user2" \
  --set config.GITHUB_DEST_BOARD_OWNER=myorg \
  --set config.GITHUB_DEST_BOARD_NAME="Team Board" \
  --set config.GITHUB_KUBERNETES_MILESTONE=v1.36

# Upgrade after changing values
make helm-upgrade

# Uninstall
make helm-uninstall
```

To use a pre-existing secret instead of letting the chart create one:

```bash
kubectl -n kube-board create secret generic my-token \
  --from-literal=GITHUB_TOKEN=ghp_YOUR_TOKEN

helm install kube-board deploy/chart/kube-board \
  -n kube-board --create-namespace \
  --set existingSecret=my-token
```
