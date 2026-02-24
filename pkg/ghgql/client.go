// Package ghgql provides a lightweight GraphQL client for the GitHub API.
package ghgql

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"golang.org/x/oauth2"
)

// Endpoint is the GitHub GraphQL API URL.
const Endpoint = "https://api.github.com/graphql"

// RESTEndpoint is the GitHub REST API base URL.
const RESTEndpoint = "https://api.github.com"

// Client is an authenticated GitHub GraphQL API client.
type Client struct {
	HTTPClient *http.Client
	Token      string
}

// NewClient creates a new GraphQL client authenticated with the given PAT.
func NewClient(token string) *Client {
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	tc := oauth2.NewClient(context.Background(), ts)
	return &Client{HTTPClient: tc, Token: token}
}

// Request is a GraphQL request body.
type Request struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables,omitempty"`
}

type graphqlResponse struct {
	Data   json.RawMessage `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors,omitempty"`
}

// Do sends a GraphQL request and unmarshals the response data into result.
func (c *Client) Do(req Request, result any) error {
	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal graphql request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(context.Background(), "POST", Endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(httpReq)
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

// DoREST sends a REST API request to the GitHub REST API.
// method is the HTTP method (GET, POST, PATCH, DELETE).
// path is the URL path (e.g., "/users/{owner}/projects/{number}/views").
// body is marshaled to JSON for the request body (nil for GET/DELETE).
// result is unmarshaled from the JSON response (nil to ignore response body).
func (c *Client) DoREST(method, path string, body any, result any) error {
	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal REST body: %w", err)
		}
		reqBody = bytes.NewReader(b)
	}

	url := RESTEndpoint + path
	httpReq, err := http.NewRequestWithContext(context.Background(), method, url, reqBody)
	if err != nil {
		return fmt.Errorf("create REST request: %w", err)
	}
	httpReq.Header.Set("Accept", "application/vnd.github+json")
	httpReq.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if body != nil {
		httpReq.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.HTTPClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("REST request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read REST response: %w", err)
	}

	if resp.StatusCode == http.StatusTooManyRequests {
		retryAfter := resp.Header.Get("Retry-After")
		return &RateLimitError{
			StatusCode: resp.StatusCode,
			RetryAfter: retryAfter,
			Body:       string(respBody),
		}
	}

	if resp.StatusCode >= 400 {
		return fmt.Errorf("REST %s %s HTTP %d: %s", method, path, resp.StatusCode, string(respBody))
	}

	if result != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, result); err != nil {
			return fmt.Errorf("unmarshal REST response: %w", err)
		}
	}

	return nil
}

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
