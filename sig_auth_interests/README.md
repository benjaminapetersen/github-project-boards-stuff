# SIG-Auth Interests (Repo Search)

A Go tool that searches GitHub repositories directly via the GraphQL **search**
API, collecting issues and PRs matching label, milestone, and state filters.
Results can be printed to the CLI or pushed to a destination project board.

This is the **repo-centric** sibling of `sig_auth_interested_projects/`, which
discovers items by walking org-level project boards. This tool is faster and
cheaper because GitHub's search API handles most filtering server-side.

## Key Differences from sig_auth_interested_projects/

| | This tool (repo search) | sig_auth_interested_projects (board scan) |
|---|---|---|
| **Data source** | GitHub `search()` API | Project board `items()` API |
| **Server-side filters** | repo, label, milestone, state, exclude-labels | Milestone only |
| **Custom board fields** | Not available | Status, Stage, PRR, etc. |
| **API cost** | ~2-10 GraphQL points | ~50-100+ points |
| **Duplicates** | Deduplicated inherently (search returns each item once) | Same item can appear on multiple boards |

## Setup

### 1. GitHub Token

A **classic** personal access token is required.

1. Go to https://github.com/settings/tokens → **Generate new token (classic)**
2. Select scopes:
   - `read:project` — read project board data
   - `project` — create/update your own project boards
   - `public_repo` — search issues in public repos
   - `read:org` — list org projects (for board mode)
3. Copy the token (starts with `ghp_`)

### 2. Environment Variables

| Variable | Required | Description |
|----------|----------|-------------|
| `GITHUB_TOKEN` | Yes | Classic PAT with scopes listed above |
| `GITHUB_ORG` | Yes | GitHub organization, e.g. `kubernetes` |
| `GITHUB_REPOS` | Recommended | Comma-separated repos to search (e.g. `kubernetes/kubernetes,kubernetes/enhancements`). Omit for org-wide search. |
| `GITHUB_LABELS` | Recommended | Labels to require (**server-side**), e.g. `sig/auth,sig/security` |
| `GITHUB_MILESTONE` | No | Milestone filter (**server-side**), e.g. `v1.36` |
| `GITHUB_STATES` | No | States to include (**server-side**): `open`, `closed`, `merged` (default: `open`) |
| `GITHUB_EXCLUDE_LABELS` | No | Labels to exclude (**server-side**), e.g. `lifecycle/rotten,lifecycle/stale` |
| `GITHUB_INVOLVED` | No | Comma-separated usernames (post-fetch: matches author or assignee) |
| `GITHUB_ITEM_TYPES` | No | Comma-separated: `issue`, `pr` (default: all) |
| `DEST_BOARD_OWNER` | For board mode | User/org that will own the destination board |
| `DEST_BOARD_NAME` | For board mode | Name for the destination project board |
| `DEST_LINK_REPOS` | No | Comma-separated repos to link to the destination board |

Source the file before running:

```bash
source .env
```

## Usage

### CLI Flags

| Flag | Description |
|------|-------------|
| `--use-cache=true\|false` | `false` = fetch live. `true` = use cache. **Omit for dry-run.** |
| `--output cli\|board` | Output mode (default: `cli`) |
| `--sync` | With `--output board`, remove board items not in current query |

### Examples

```bash
# Dry run — shows search queries that would be executed
source .env && go run .

# Fetch live from GitHub, print to terminal
source .env && go run . --use-cache=false

# Print from cache
source .env && go run . --use-cache=true

# Fetch live and create/update destination board
source .env && go run . --use-cache=false --output board

# Update board from cache + remove stale items
source .env && go run . --use-cache=true --output board --sync
```

### Workflow

1. **Dry run** — preview search queries and rate limits:
   ```bash
   go run .
   ```
2. **Fetch and cache** — pull live data:
   ```bash
   go run . --use-cache=false
   ```
3. **Iterate from cache** — tweak post-fetch filters, update board:
   ```bash
   go run . --use-cache=true --output board
   ```

## How It Works

1. **Builds search queries** — Constructs `repo:X label:Y is:open milestone:Z -label:W`
   style queries from the env var configuration
2. **Searches via GraphQL** — Uses GitHub's `search(type: ISSUE)` API which
   returns issues and PRs matching the query (server-side filtering)
3. **Deduplicates** — search naturally deduplicates (each issue appears once)
4. **Post-filters** — Applies involved-user and item-type filters client-side
5. **Outputs** — Prints to CLI or creates/updates a destination project board

## Caching

Responses are cached as JSON in `.cache/`:

```
.cache/items_kubernetes_v1.36_2026-02-09T22-30-00.json
```

`--use-cache=true` loads the most recent matching file.

## Project Structure

| File | Purpose |
|------|---------|
| `main.go` | CLI flags, config, post-fetch filtering, output routing |
| `graphql.go` | Generic GraphQL HTTP client |
| `query.go` | Search query building, GraphQL search execution |
| `board.go` | Destination board CRUD (find/create, add items, link repos, sync) |
| `cache.go` | JSON cache read/write |
| `ratelimit.go` | Rate limit checking, cost estimation, 429 handling |
| `.env` | Environment variables (not committed) |
