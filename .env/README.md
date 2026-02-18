# Environment Files

Each binary has its own `.env.example` file here. To use one:

```bash
# Copy the example for the binary you want to run
cp .env/sig-auth-projects.env.example .env/sig-auth-projects.env

# Edit with your real values
vi .env/sig-auth-projects.env

# Source it before running the binary
source .env/sig-auth-projects.env
go run ./cmd/sig-auth-projects
```

**Important:** All variables use `export` so that child processes (like `go run`)
inherit them via `os.Getenv()`. Without `export`, `source` only sets variables in
the current shell — they won't be visible to any programs you launch.

## Files

| File | Binary |
|---|---|
| `kube-enhancements.env.example` | `cmd/kube-enhancements` — REST issue queries |
| `sig-auth-projects.env.example` | `cmd/sig-auth-projects` — GraphQL board scan |
| `sig-auth-search.env.example` | `cmd/sig-auth-search` — GraphQL search API |
| `check-rate-limits.env.example` | `cmd/check-rate-limits` — Rate limit checker |
| `foo.env.example` | `cmd/foo` — Original prototype |

The `.env` files (with real secrets) are gitignored. Only `.env.example` templates are tracked.
