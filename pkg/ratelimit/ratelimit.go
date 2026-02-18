// Package ratelimit provides GitHub API rate-limit checking and display.
package ratelimit

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/benjaminapetersen/github-project-boards-stuff/pkg/ghgql"
	"github.com/google/go-github/v57/github"
	"golang.org/x/oauth2"
)

// Status holds REST API rate limit information.
type Status struct {
	Core    Category `json:"core"`
	Search  Category `json:"search"`
	GraphQL Category `json:"graphql"`
}

// Category is one bucket of the REST rate-limit response.
type Category struct {
	Limit     int       `json:"limit"`
	Used      int       `json:"used"`
	Remaining int       `json:"remaining"`
	ResetAt   time.Time `json:"reset_at"`
}

// GraphQLInfo holds GraphQL-specific rate limit information.
type GraphQLInfo struct {
	Login     string
	Limit     int
	Remaining int
	Used      int
	ResetAt   time.Time
	QueryCost int
}

// FetchREST calls GET /rate_limit (free — does not count against quota).
func FetchREST(token string) (*Status, error) {
	ctx := context.Background()
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	tc := oauth2.NewClient(ctx, ts)
	client := github.NewClient(tc)

	limits, _, err := client.RateLimit.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetching rate limits: %w", err)
	}

	status := &Status{}
	if limits.Core != nil {
		status.Core = Category{
			Limit:     limits.Core.Limit,
			Used:      limits.Core.Limit - limits.Core.Remaining,
			Remaining: limits.Core.Remaining,
			ResetAt:   limits.Core.Reset.Time,
		}
	}
	if limits.Search != nil {
		status.Search = Category{
			Limit:     limits.Search.Limit,
			Used:      limits.Search.Limit - limits.Search.Remaining,
			Remaining: limits.Search.Remaining,
			ResetAt:   limits.Search.Reset.Time,
		}
	}
	if limits.GraphQL != nil {
		status.GraphQL = Category{
			Limit:     limits.GraphQL.Limit,
			Used:      limits.GraphQL.Limit - limits.GraphQL.Remaining,
			Remaining: limits.GraphQL.Remaining,
			ResetAt:   limits.GraphQL.Reset.Time,
		}
	}

	return status, nil
}

// FetchGraphQL queries the GraphQL rateLimit object (costs 1 point).
func FetchGraphQL(gql *ghgql.Client) (*GraphQLInfo, error) {
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

	err := gql.Do(ghgql.Request{Query: query}, &result)
	if err != nil {
		return nil, fmt.Errorf("querying GraphQL rate limit: %w", err)
	}

	resetAt, _ := time.Parse(time.RFC3339, result.RateLimit.ResetAt)

	return &GraphQLInfo{
		Login:     result.Viewer.Login,
		Limit:     result.RateLimit.Limit,
		Remaining: result.RateLimit.Remaining,
		Used:      result.RateLimit.Used,
		ResetAt:   resetAt,
		QueryCost: result.RateLimit.Cost,
	}, nil
}

// PrintStatus prints a human-readable summary of rate limit status.
func PrintStatus(rest *Status, gql *GraphQLInfo) {
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

func printCategory(indent string, cat Category) {
	fmt.Printf("%sLimit:     %d\n", indent, cat.Limit)
	fmt.Printf("%sUsed:      %d\n", indent, cat.Used)
	fmt.Printf("%sRemaining: %d\n", indent, cat.Remaining)
	fmt.Printf("%sResets at: %s\n", indent, cat.ResetAt.Local().Format("2006-01-02 15:04:05 MST"))
	fmt.Println()
}

// CheckAndWarn performs a pre-flight rate-limit check and prints warnings.
// It checks both REST and GraphQL limits. The GET /rate_limit call is free;
// the GraphQL probe costs 1 point.
func CheckAndWarn(token string) {
	log.Println("Checking rate limit status...")

	rest, err := FetchREST(token)
	if err != nil {
		log.Printf("Warning: could not fetch REST rate limits: %v", err)
	}

	gql := ghgql.NewClient(token)
	gqlInfo, err := FetchGraphQL(gql)
	if err != nil {
		log.Printf("Warning: could not fetch GraphQL rate limits: %v", err)
	}

	PrintStatus(rest, gqlInfo)

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
