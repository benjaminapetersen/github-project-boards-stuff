package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"golang.org/x/oauth2"
)

const graphqlEndpoint = "https://api.github.com/graphql"

// graphqlClient wraps an authenticated HTTP client for GitHub GraphQL calls.
type graphqlClient struct {
	httpClient *http.Client
	token      string
}

func newGraphQLClient(token string) *graphqlClient {
	ctx := context.Background()
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	tc := oauth2.NewClient(ctx, ts)
	return &graphqlClient{httpClient: tc, token: token}
}

type graphqlRequest struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables,omitempty"`
}

type graphqlResponse struct {
	Data   json.RawMessage `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors,omitempty"`
}

func (c *graphqlClient) do(req graphqlRequest, result any) error {
	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal graphql request: %w", err)
	}

	ctx := context.Background()
	httpReq, err := http.NewRequestWithContext(ctx, "POST", graphqlEndpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("graphql request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode == http.StatusTooManyRequests {
		retryAfter := resp.Header.Get("Retry-After")
		return &RateLimitError{
			StatusCode: resp.StatusCode,
			RetryAfter: retryAfter,
			Body:       string(respBody),
		}
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("graphql HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var gqlResp graphqlResponse
	if err := json.Unmarshal(respBody, &gqlResp); err != nil {
		return fmt.Errorf("unmarshal response: %w", err)
	}

	if len(gqlResp.Errors) > 0 {
		msgs := make([]string, len(gqlResp.Errors))
		for i, e := range gqlResp.Errors {
			msgs[i] = e.Message
		}
		return fmt.Errorf("graphql errors: %s", strings.Join(msgs, "; "))
	}

	if result != nil {
		if err := json.Unmarshal(gqlResp.Data, result); err != nil {
			return fmt.Errorf("unmarshal data: %w", err)
		}
	}

	return nil
}
