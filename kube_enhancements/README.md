# Kube Enhancements

A Go script that queries [kubernetes/enhancements](https://github.com/kubernetes/enhancements)
issues by label and milestone, and optionally creates/updates a GitHub Projects V2 board with the results.

## Board

- Example link:
  - https://github.com/kubernetes/enhancements/issues?q=is%3Aissue%20state%3Aopen%20label%3Asig%2Fauth

## Setup

### 1. GitHub Token

A **classic** personal access token is required (fine-grained PATs do not support the Projects V2 API).

1. Go to https://github.com/settings/tokens → **Generate new token (classic)**
2. Select scopes:
   - `project` — required for creating/updating project boards
   - `public_repo` — required for reading issues from public repos
3. Copy the token (starts with `ghp_`)

### 2. Environment Variables

Copy and edit the `.env` file:

```bash
cp .env.example .env
# edit .env with your token and preferences
```

| Variable | Required | Description |
|----------|----------|-------------|
| `GITHUB_TOKEN` | Yes | Classic PAT (`ghp_...`) with `project` + `public_repo` scopes |
| `LABELS` | Yes | Comma-separated labels, e.g. `sig/auth` or `sig/auth,sig/node` |
| `MILESTONES` | No | Comma-separated milestone titles, e.g. `v1.36` or `v1.36,v1.35` |
| `USERS` | No | Comma-separated GitHub usernames to filter by assignee |
| `REPO_OWNER` | No | GitHub org (default: `kubernetes`) |
| `REPO_NAME` | No | GitHub repo (default: `enhancements`) |
| `STATE` | No | Issue state: `open`, `closed`, or `all` (default: `open`) |
| `BOARD_OWNER` | For board mode | GitHub user/org that owns the project board |
| `BOARD_NAME` | No | Override the auto-generated board name |
| `LINK_REPOS` | No | Comma-separated repos to link to the board (see below) |

Source the file before running:

```bash
source .env
```

## Usage

The script defaults to **dry-run** mode. It always checks your rate limit status
and shows an estimated cost before doing anything.

### CLI Flags

| Flag | Description |
|------|-------------|
| `--use-cache=true\|false` | `false` = fetch live from GitHub. `true` = use cached data. **Omit entirely for dry-run.** |
| `--output cli\|board` | Output mode (default: `cli`) |
| `--sync` | With `--output board`, remove board items not in current query |

### Examples

```bash
# Dry run (default) — check rate limits and show estimated cost, no API queries
source .env && go run .

# Fetch live issues and print to terminal
source .env && go run . --use-cache=false

# Print issues from cache (no API calls for issues)
source .env && go run . --use-cache=true

# Fetch live issues and create/update a GitHub project board
source .env && go run . --use-cache=false --output board

# Update project board from cached data
source .env && go run . --use-cache=true --output board

# Update board from cache and remove stale items no longer matching the query
source .env && go run . --use-cache=true --output board --sync

# Filter by specific users
USERS="enj,aramase" go run . --use-cache=false
```

### Workflow

A typical workflow looks like:

1. **Dry run** — check rate limits and estimated cost:
   ```bash
   go run .
   ```
2. **Fetch and cache** — pull live data from GitHub:
   ```bash
   go run . --use-cache=false
   ```
3. **Iterate from cache** — tweak output, update board, etc. without burning API calls:
   ```bash
   go run . --use-cache=true --output board
   ```

## Caching

Responses are cached as pretty-printed JSON in `.cache/`. Files are named with
the query parameters and a timestamp:

```
.cache/issues_kubernetes_enhancements_sig-auth_v1.36_open_2026-02-09T15-08-17.json
```

This lets you replay and inspect data without re-querying GitHub. The `--use-cache=true`
flag loads the most recent matching cache file.

## Rate Limits

The script always checks `GET /rate_limit` (which is free and does not count
against your budget) before doing anything. In board mode it also queries the
GraphQL `rateLimit` object to show your point budget.

If GitHub returns a 429 (rate limited), the script prints the `Retry-After`
header, the current time, and a suggested retry time.

## Board Naming

When using `--output board`, the project board is auto-named from your query
parameters. For example:

- Labels `sig/auth` + milestone `v1.36` → **"k8s enhancements sig/auth v1.36"**

Override with the `BOARD_NAME` env var if you prefer a custom name.

## Linking Repos

Set `LINK_REPOS` to a comma-separated list of repositories to link to the
project board.  Repos without an `owner/` prefix automatically use `BOARD_OWNER`:

```bash
# Links benjaminapetersen/github-project-boards-stuff and benjaminapetersen/lunch-and-learn-microsoft
export LINK_REPOS="github-project-boards-stuff,lunch-and-learn-microsoft"

# Explicit owner/repo also works, and can be mixed
export LINK_REPOS="someorg/other-repo,my-local-repo"
```

Already-linked repos are silently skipped.

## Project Structure

| File | Purpose |
|------|---------|
| `main.go` | CLI flags, config, issue fetching, output routing |
| `board.go` | GraphQL client, project board CRUD operations |
| `cache.go` | Response caching and 429 error formatting |
| `ratelimit.go` | Rate limit checking and cost estimation |
| `.env` | Environment variables (not committed) |