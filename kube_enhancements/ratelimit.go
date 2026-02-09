package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/google/go-github/v57/github"
)

// RateLimitStatus holds the rate limit info we care about.
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

// fetchRateLimits calls GET /rate_limit (does NOT count against your rate limit).
func fetchRateLimits(ctx context.Context, client *github.Client) (*RateLimitStatus, error) {
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

// fetchGraphQLRateLimit queries the GraphQL API's rateLimit object to get
// the current point budget and the cost of a lightweight probe query.
func fetchGraphQLRateLimit(ctx context.Context, gql *graphqlClient) (*GraphQLRateLimitInfo, error) {
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

	err := gql.do(ctx, graphqlRequest{Query: query}, &result)
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

type GraphQLRateLimitInfo struct {
	Login     string
	Limit     int
	Remaining int
	Used      int
	ResetAt   time.Time
	QueryCost int // cost of the probe query itself (typically 1)
}

// printRateLimitStatus prints a human-readable summary of rate limit status.
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
		fmt.Println("  REST API (graphql points):")
		printCategory("    ", rest.GraphQL)
	}

	if gql != nil {
		fmt.Println("  GraphQL API (live query):")
		fmt.Printf("    Authenticated as: %s\n", gql.Login)
		fmt.Printf("    Limit:     %d points/hour\n", gql.Limit)
		fmt.Printf("    Used:      %d\n", gql.Used)
		fmt.Printf("    Remaining: %d\n", gql.Remaining)
		fmt.Printf("    Resets at: %s\n", gql.ResetAt.Local().Format("2006-01-02 15:04:05 MST"))
		fmt.Printf("    Probe query cost: %d point(s)\n", gql.QueryCost)
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

// estimateQueryCost prints a clear breakdown and total of the expected API cost.
func estimateQueryCost(config Config) {
	fmt.Println("=== Estimated Query Cost ===")
	fmt.Println()

	restTotal := 0
	gqlTotal := 0

	// --- REST API ---
	fmt.Println("  REST API (core):")
	fmt.Println("  Each call costs 1 point against your core budget.")
	fmt.Println()

	if len(config.Milestones) > 0 {
		fmt.Println("    1 call   - List milestones (resolve names to IDs)")
		restTotal++
		n := len(config.Milestones)
		fmt.Printf("    %d call(s) - List issues (%d milestone(s), 1 page each minimum)\n", n, n)
		restTotal += n
	} else {
		fmt.Println("    1 call   - List issues (1 page minimum)")
		restTotal++
	}
	fmt.Println("             (add more if results span multiple pages of 100)")
	fmt.Println()
	fmt.Printf("  REST total: ~%d point(s) minimum\n", restTotal)
	fmt.Println()

	// --- GraphQL API ---
	if config.OutputMode == "board" {
		fmt.Println("  GraphQL API (project board operations):")
		fmt.Println("  Read queries cost 1 point. Mutations cost 1 point (primary) + 5 points (secondary).")
		fmt.Println()

		fmt.Println("    1 point  - Resolve owner node ID")
		gqlTotal++
		fmt.Println("    1 point  - Find existing project")
		gqlTotal++
		fmt.Println("    1 point  - Create project (only if it doesn't exist yet)")
		gqlTotal++
		fmt.Println("    1 point  - List existing board items (dedup check)")
		gqlTotal++
		fmt.Println("    N points - Add issues to board (1 per issue, skips duplicates)")
		// We don't know N yet, but give a sense
		gqlTotal += 10 // rough placeholder

		if config.Sync {
			fmt.Println("    1 point  - List board items for stale removal")
			gqlTotal++
			fmt.Println("    N points - Remove stale items (1 per item)")
			gqlTotal += 5 // rough placeholder
		}

		if len(config.LinkRepos) > 0 {
			n := len(config.LinkRepos)
			fmt.Printf("    %d point(s) - Resolve repository node IDs\n", n)
			gqlTotal += n
			fmt.Printf("    %d point(s) - Link project to repositories (1 per repo, skips already linked)\n", n)
			gqlTotal += n
		}
		fmt.Println()
		fmt.Printf("  GraphQL total: ~%d+ points (varies with number of issues)\n", gqlTotal)
		fmt.Println()
		fmt.Println("  Secondary rate limit: mutations count 5 pts each (max 2,000/min).")
	} else {
		fmt.Println("  GraphQL API: 0 points (not used in CLI output mode)")
	}

	fmt.Println()
	fmt.Println("  -------------------------")
	if config.OutputMode == "board" {
		fmt.Printf("  TOTAL: ~%d REST points + ~%d+ GraphQL points\n", restTotal, gqlTotal)
	} else {
		fmt.Printf("  TOTAL: ~%d REST point(s), 0 GraphQL points\n", restTotal)
	}
	fmt.Println("  -------------------------")
	fmt.Println()
}

// checkRateLimitOrAbort checks rate limits and aborts if remaining budget is too low.
func checkRateLimitOrAbort(ctx context.Context, client *github.Client, config Config) {
	log.Println("Checking rate limit status...")

	rest, err := fetchRateLimits(ctx, client)
	if err != nil {
		log.Printf("Warning: could not fetch REST rate limits: %v", err)
	}

	var gqlInfo *GraphQLRateLimitInfo
	if config.OutputMode == "board" {
		gql := newGraphQLClient(ctx, config.GitHubToken)
		gqlInfo, err = fetchGraphQLRateLimit(ctx, gql)
		if err != nil {
			log.Printf("Warning: could not fetch GraphQL rate limits: %v", err)
		}
	}

	printRateLimitStatus(rest, gqlInfo)

	// Warn if REST core budget is very low
	if rest != nil && rest.Core.Remaining < 10 {
		log.Printf("WARNING: REST API core budget is very low (%d remaining). Resets at %s",
			rest.Core.Remaining, rest.Core.ResetAt.Local().Format("15:04:05 MST"))
	}

	// Warn if GraphQL budget is very low
	if gqlInfo != nil && gqlInfo.Remaining < 10 {
		log.Printf("WARNING: GraphQL API budget is very low (%d points remaining). Resets at %s",
			gqlInfo.Remaining, gqlInfo.ResetAt.Local().Format("15:04:05 MST"))
	}

	// Cache the rate limit response for reference
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
