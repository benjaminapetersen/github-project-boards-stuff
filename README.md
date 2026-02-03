# GitHub Project Boards Automation

Automation script to find all your commits, PRs, and issues from the last month in Kubernetes organization repositories and list them on a GitHub project board.

## Features

- Searches for commits in Kubernetes organization repositories
- Finds pull requests you've authored in Kubernetes org
- Finds issues you've created in Kubernetes org
- Filters results to the last month
- Displays summary of found items
- Can be configured to add items to a GitHub Project Board

## Prerequisites

- Go 1.21 or later
- GitHub Personal Access Token with appropriate permissions:
  - `repo` - Full control of private repositories
  - `read:org` - Read org and team membership
  - `project` - Full control of projects (if using project board features)

## Installation

```bash
go mod download
go build -o github-board-automation
```

## Usage

### Environment Variables

Set the following environment variables:

```bash
export GITHUB_TOKEN="your_github_token"
export GITHUB_USERNAME="your_github_username"
export PROJECT_ID="your_project_id" # Optional - only needed if adding to project board
```

### Running the Script

```bash
# Just search and display results
./github-board-automation

# Or run directly with go
go run main.go
```

### Example Output

```
2024/01/01 12:00:00 Starting GitHub Project Board automation for Kubernetes organization...
2024/01/01 12:00:00 Looking for activity from user: yourusername
2024/01/01 12:00:00 Date range: 2023-12-01 to 2024-01-01
2024/01/01 12:00:05 Found 15 commits
2024/01/01 12:00:08 Found 3 PRs
2024/01/01 12:00:10 Found 2 issues

Summary:
- Commits: 15
- Pull Requests: 3
- Issues: 2
```

## GitHub Project Board Setup

To create a GitHub project board:

1. Go to https://github.com/users/YOUR_USERNAME/projects
2. Click "New project"
3. Choose a template or start from scratch
4. Once created, get the project ID from the URL or API
5. Set the `PROJECT_ID` environment variable

## Notes

- The script searches only within the **Kubernetes** organization
- Date range is automatically set to the last 30 days
- GitHub's Search API has rate limits (30 requests per minute for authenticated requests)
- Project V2 API integration requires GraphQL mutations (structure provided in code)

## Development

```bash
# Run tests (when available)
go test ./...

# Build
go build -o github-board-automation

# Format code
go fmt ./...
```

## License

MIT
