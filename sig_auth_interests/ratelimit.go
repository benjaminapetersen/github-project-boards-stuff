package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/google/go-github/v57/github"
	"golang.org/x/oauth2"
)

// RateLimitError holds details about a GitHub 429 response.
type RateLimitError struct {
	StatusCode int
	RetryAfter string
	Body       string
}

func (e *RateLimitError) Error() string {
	now := time.Now()
	msg := fmt.Sprintf("GitHub rate limit exceeded (HTTP %d)", e.StatusCode)
	msg += fmt.Sprintf("\n  Current time:  %s", now.Format("2006-01-02 15:04:05 MST"))

	if e.RetryAfter != "" {
		msg += fmt.Sprintf("\n  Retry-After:   %s seconds", e.RetryAfter)
		if secs, err := time.ParseDuration(e.RetryAfter + "s"); err == nil {
			retryAt := now.Add(secs)
			msg += fmt.Sprintf("\n  Try again at:  %s", retryAt.Format("2006-01-02 15:04:05 MST"))
		}
	} else {
		msg += "\n  Retry-After:   (not provided)"
		msg += "\n  Try again at:  wait ~60 seconds and retry"
	}

	return msg
}

// --- Rate limit types ---

type RateLimitStatus struct {
	Core    RateLimitCategory `json:"core"`
	Search  RateLimitCategory `json:"search"`
	GraphQL RateLimitCategory `json:"graphql"`
}

type RateLimitCategory struct {
	Limit     int       `json:"limit"`
	Used      int       `json:"used"`
	Remaining int       `json:"remaining"`
	ResetAt   time.Time `json:"reset_at"`
}

type GraphQLRateLimitInfo struct {
	Login     string
	Limit     int
	Remaining int
	Used      int
	ResetAt   time.Time
	QueryCost int
}

// --- Fetchers ---

// fetchRESTRateLimits calls GET /rate_limit (free, does not count against quota).
func fetchRESTRateLimits(token string) (*RateLimitStatus, error) {
	ctx := context.Background()
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	tc := oauth2.NewClient(ctx, ts)
	client := github.NewClient(tc)

	limits, _, err := client.RateLimit.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetching rate limits: %w", err)
	}

	status := &RateLimitStatus{}
	if limits.Core != nil {
		status.Core = RateLimitCategory{
			Limit:     limits.Core.Limit,
			Used:      limits.Core.Limit - limits.Core.Remaining,
			Remaining: limits.Core.Remaining,
			ResetAt:   limits.Core.Reset.Time,
		}
	}
	if limits.Search != nil {
		status.Search = RateLimitCategory{
			Limit:     limits.Search.Limit,
			Used:      limits.Search.Limit - limits.Search.Remaining,
			Remaining: limits.Search.Remaining,
			ResetAt:   limits.Search.Reset.Time,
		}
	}
	if limits.GraphQL != nil {
		status.GraphQL = RateLimitCategory{
			Limit:     limits.GraphQL.Limit,
			Used:      limits.GraphQL.Limit - limits.GraphQL.Remaining,
			Remaining: limits.GraphQL.Remaining,
			ResetAt:   limits.GraphQL.Reset.Time,
		}
	}

	return status, nil
}

// fetchGraphQLRateLimit queries the GraphQL rateLimit object.
func fetchGraphQLRateLimit(gql *graphqlClient) (*GraphQLRateLimitInfo, error) {
	query := `query {
		viewer { login }
		rateLimit {
			limit
			remaining
			used
			resetAt
			cost
		}
	}`

	var result struct {
		Viewer struct {
			Login string `json:"login"`
		} `json:"viewer"`
		RateLimit struct {
			Limit     int    `json:"limit"`
			Remaining int    `json:"remaining"`
			Used      int    `json:"used"`
			ResetAt   string `json:"resetAt"`
			Cost      int    `json:"cost"`
		} `json:"rateLimit"`
	}

	err := gql.do(graphqlRequest{Query: query}, &result)
	if err != nil {
		return nil, fmt.Errorf("querying GraphQL rate limit: %w", err)
	}

	resetAt, _ := time.Parse(time.RFC3339, result.RateLimit.ResetAt)

	return &GraphQLRateLimitInfo{
		Login:     result.Viewer.Login,
		Limit:     result.RateLimit.Limit,
		Remaining: result.RateLimit.Remaining,
		Used:      result.RateLimit.Used,
		ResetAt:   resetAt,
		QueryCost: result.RateLimit.Cost,
	}, nil
}

// --- Display ---

func printRateLimitStatus(rest *RateLimitStatus, gql *GraphQLRateLimitInfo) {
	now := time.Now()
	fmt.Println()
	fmt.Println("=== GitHub Rate Limit Status ===")
	fmt.Printf("  Current time: %s\n\n", now.Format("2006-01-02 15:04:05 MST"))

	if rest != nil {
		fmt.Println("  REST API (core):")
		printCategory("    ", rest.Core)
		fmt.Println("  REST API (search):")
		printCategory("    ", rest.Search)
		fmt.Println("  REST API (graphql points via REST):")
		printCategory("    ", rest.GraphQL)
	}

	if gql != nil {
		fmt.Println("  GraphQL API (live query):")
		fmt.Printf("    Authenticated as: %s\n", gql.Login)
		fmt.Printf("    Limit:     %d points/hour\n", gql.Limit)
		fmt.Printf("    Used:      %d\n", gql.Used)
		fmt.Printf("    Remaining: %d\n", gql.Remaining)
		fmt.Printf("    Resets at: %s\n", gql.ResetAt.Local().Format("2006-01-02 15:04:05 MST"))
		fmt.Printf("    Probe cost: %d point(s)\n", gql.QueryCost)
		fmt.Println()
	}
}

func printCategory(indent string, cat RateLimitCategory) {
	fmt.Printf("%sLimit:     %d\n", indent, cat.Limit)
	fmt.Printf("%sUsed:      %d\n", indent, cat.Used)
	fmt.Printf("%sRemaining: %d\n", indent, cat.Remaining)
	fmt.Printf("%sResets at: %s\n", indent, cat.ResetAt.Local().Format("2006-01-02 15:04:05 MST"))
	fmt.Println()
}

// --- Cost Estimation ---

func estimateQueryCost(config Config) {
	fmt.Println("=== Estimated Query Cost ===")
	fmt.Println()

	gqlTotal := 0

	fmt.Println("  GraphQL API:")
	fmt.Println("  Search queries cost 1 point per page of 100 results.")
	fmt.Println()

	// Each search query (per label set) paginates at 100/page
	searchQueries := len(config.Labels)
	if searchQueries == 0 {
		searchQueries = 1
	}
	searchQueries *= len(config.Repos)
	if searchQueries == 0 {
		searchQueries = 1
	}

	fmt.Printf("    %d search query(ies) x N pages each\n", searchQueries)
	gqlTotal += searchQueries * 2 // rough estimate: 2 pages per query

	if config.OutputMode == "board" {
		fmt.Println("    1 point  - Resolve owner node ID")
		gqlTotal++
		fmt.Println("    1 point  - Find existing destination project")
		gqlTotal++
		fmt.Println("    1 point  - Create project (if not found)")
		gqlTotal++
		fmt.Println("    1 point  - List existing board items (dedup)")
		gqlTotal++
		fmt.Println("    N points - Add items to board (1 per item)")
		gqlTotal += 10

		if config.Sync {
			fmt.Println("    1 point  - List board items for stale removal")
			gqlTotal++
			fmt.Println("    N points - Remove stale items (1 per item)")
			gqlTotal += 5
		}

		if len(config.LinkRepos) > 0 {
			n := len(config.LinkRepos)
			fmt.Printf("    %d point(s) - Resolve repo node IDs\n", n)
			gqlTotal += n
			fmt.Printf("    %d point(s) - Link repos to project\n", n)
			gqlTotal += n
		}
	}

	fmt.Println()
	fmt.Printf("  TOTAL: ~%d+ GraphQL points (varies with data size)\n", gqlTotal)
	fmt.Println()
}

// --- Pre-flight check ---

func checkRateLimitOrAbort(config Config) {
	log.Println("Checking rate limit status...")

	rest, err := fetchRESTRateLimits(config.GitHubToken)
	if err != nil {
		log.Printf("Warning: could not fetch REST rate limits: %v", err)
	}

	gql := newGraphQLClient(config.GitHubToken)
	gqlInfo, err := fetchGraphQLRateLimit(gql)
	if err != nil {
		log.Printf("Warning: could not fetch GraphQL rate limits: %v", err)
	}

	printRateLimitStatus(rest, gqlInfo)

	if rest != nil && rest.Core.Remaining < 10 {
		log.Printf("WARNING: REST API core budget is very low (%d remaining). Resets at %s",
			rest.Core.Remaining, rest.Core.ResetAt.Local().Format("15:04:05 MST"))
	}

	if gqlInfo != nil && gqlInfo.Remaining < 10 {
		log.Printf("WARNING: GraphQL API budget is very low (%d points remaining). Resets at %s",
			gqlInfo.Remaining, gqlInfo.ResetAt.Local().Format("15:04:05 MST"))
	}

	if rest != nil {
		data := map[string]any{
			"rest":    rest,
			"graphql": gqlInfo,
			"checked": time.Now().Format(time.RFC3339),
		}
		jsonData, _ := json.MarshalIndent(data, "", "  ")
		log.Printf("Rate limit snapshot:\n%s", string(jsonData))
	}
}
