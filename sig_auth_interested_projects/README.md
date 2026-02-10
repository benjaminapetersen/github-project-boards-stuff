# SIG-Auth Interests

A Go tool that queries GitHub Projects V2 boards across an organization via the
GraphQL API, collects items (issues, PRs, draft issues) with their custom field
values, and optionally writes them to a destination project board you own.

This is useful for building a personal tracking board from items spread across
multiple org-level project boards — e.g. pulling sig/auth-relevant enhancements
from the Kubernetes org into your own board.

## Setup

### 1. GitHub Token

A **classic** personal access token is required (fine-grained PATs do not
support the Projects V2 API).

1. Go to https://github.com/settings/tokens → **Generate new token (classic)**
2. Select scopes:
   - `read:project` — read project board data from any public project
   - `project` — create/update your own project boards
   - `public_repo` — read issues from public repos
   - `read:org` — list org projects
3. Copy the token (starts with `ghp_`)

### 2. Environment Variables

Copy and edit the `.env` file:

```bash
cp .env.example .env
# edit .env with your token and preferences
```

| Variable | Required | Description |
|----------|----------|-------------|
| `GITHUB_TOKEN` | Yes | Classic PAT with scopes listed above |
| `GITHUB_ORG` | Yes | GitHub organization, e.g. `kubernetes` |
| `GITHUB_MILESTONE` | No | Milestone to filter by, e.g. `v1.36` |
| `GITHUB_INVOLVED` | No | Comma-separated usernames; matches items assigned to or authored by these users |
| `GITHUB_REPOS` | No | Comma-separated repos (e.g. `kubernetes/enhancements`). Omit for org-wide. |
| `GITHUB_ITEM_TYPES` | No | Comma-separated: `issue`, `pr`, `draft` (default: all) |
| `GITHUB_EXCLUDE_STATES` | No | States to exclude (default: `CLOSED,MERGED`). Set to `""` to keep all. |
| `GITHUB_EXCLUDE_STATUSES` | No | Board Status values to exclude (default: `Done`). Set to `""` to keep all. |
| `GITHUB_EXCLUDE_LABELS` | No | Labels to exclude (e.g. `lifecycle/rotten,lifecycle/stale`) |
| `GITHUB_SIG_LABELS` | No | Require at least one of these `sig/` labels (e.g. `sig/auth,sig/security`). Empty = no filter. |
| `DEST_BOARD_OWNER` | For board mode | User/org that will own the destination board |
| `DEST_BOARD_NAME` | For board mode | Name for the destination project board |
| `DEST_LINK_REPOS` | No | Comma-separated repos to link to the destination board |

Source the file before running:

```bash
source .env
```

## Usage

The script defaults to **dry-run** mode. It always checks rate limit status
and shows estimated cost before doing anything.

### CLI Flags

| Flag | Description |
|------|-------------|
| `--use-cache=true\|false` | `false` = fetch live. `true` = use cache. **Omit for dry-run.** |
| `--output cli\|board` | Output mode (default: `cli`) |
| `--sync` | With `--output board`, remove board items not in current query |

### Examples

```bash
# Dry run — check rate limits and estimated cost
source .env && go run .

# Fetch live from GitHub, print to terminal
source .env && go run . --use-cache=false

# Print from cache
source .env && go run . --use-cache=true

# Fetch live and create/update destination board
source .env && go run . --use-cache=false --output board

# Update board from cache
source .env && go run . --use-cache=true --output board

# Update board from cache + remove stale items
source .env && go run . --use-cache=true --output board --sync
```

### Workflow

1. **Dry run** — check rate limits:
   ```bash
   go run .
   ```
2. **Fetch and cache** — pull live data:
   ```bash
   go run . --use-cache=false
   ```
3. **Iterate from cache** — tweak filters, update board without API calls:
   ```bash
   go run . --use-cache=true --output board
   ```

## How It Works

1. **Discovers projects** — Lists all Projects V2 in the GitHub org
2. **Fetches items** — For each project, pages through items with custom field
   values (Status, Stage, PRR, etc.) via GraphQL
3. **Filters** — Applies state/status/label/sig/involved-user/repo/item-type filters
4. **Outputs** — Prints to CLI or creates/updates a destination project board

### What gets captured per item

- Issue/PR number, title, URL, state
- Repository, milestone, labels, author, assignees
- **Custom field values** from the source project (Status, Stage, PRR, etc.)
- Which project the item came from

## Caching

Responses are cached as JSON in `.cache/`:

```
.cache/items_kubernetes_v1.36_2026-02-09T15-08-17.json
```

`--use-cache=true` loads the most recent matching file.

## Rate Limits

The tool always checks `GET /rate_limit` (free) and the GraphQL `rateLimit`
object before doing work. If GitHub returns a 429, the tool prints the
Retry-After header and suggested retry time.

## Project Structure

| File | Purpose |
|------|---------|
| `main.go` | CLI flags, config, filtering, output routing |
| `graphql.go` | Generic GraphQL HTTP client |
| `query.go` | Org project discovery, item fetching with field values |
| `board.go` | Destination board CRUD (find/create, add items, link repos, sync) |
| `cache.go` | JSON cache read/write |
| `ratelimit.go` | Rate limit checking, cost estimation, 429 handling |
| `.env` | Environment variables (not committed) |
