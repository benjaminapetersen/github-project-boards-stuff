// Package ghgql provides a lightweight GraphQL client for the GitHub API.
package ghgql

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"
)

// Endpoint is the GitHub GraphQL API URL.
const Endpoint = "https://api.github.com/graphql"

// RESTEndpoint is the GitHub REST API base URL.
const RESTEndpoint = "https://api.github.com"

// Default rate-limit settings.
const (
	DefaultMinDelay   = 350 * time.Millisecond // minimum gap between requests (~3 req/s)
	DefaultMaxRetries = 5                       // max retries on rate-limit errors
)

// Client is an authenticated GitHub GraphQL API client with built-in
// rate-limit handling: request pacing, automatic retry with back-off,
// and proactive sleep when the budget is nearly exhausted.
type Client struct {
	HTTPClient *http.Client
	Token      string

	// MinDelay is the minimum interval between consecutive API requests.
	// Set to 0 to disable pacing. Default: DefaultMinDelay.
	MinDelay time.Duration

	// MaxRetries is the maximum number of retries when a rate-limit error
	// is encountered. Default: DefaultMaxRetries.
	MaxRetries int

	mu      sync.Mutex
	lastReq time.Time // timestamp of the most recent request
}

// NewClient creates a new GraphQL client authenticated with the given PAT.
func NewClient(token string) *Client {
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	tc := oauth2.NewClient(context.Background(), ts)
	return &Client{
		HTTPClient: tc,
		Token:      token,
		MinDelay:   DefaultMinDelay,
		MaxRetries: DefaultMaxRetries,
	}
}

// pace sleeps if needed so that consecutive requests are spaced at least
// MinDelay apart. This prevents burning through the budget too quickly.
func (c *Client) pace() {
	if c.MinDelay <= 0 {
		return
	}
	c.mu.Lock()
	elapsed := time.Since(c.lastReq)
	if wait := c.MinDelay - elapsed; wait > 0 {
		c.mu.Unlock()
		time.Sleep(wait)
		c.mu.Lock()
	}
	c.lastReq = time.Now()
	c.mu.Unlock()
}

// sleepForRateLimit computes and sleeps for the appropriate back-off duration.
// It uses the Retry-After header when available, otherwise exponential back-off.
// Returns true to signal the caller should retry.
func sleepForRateLimit(attempt int, retryAfterHeader string, resp *http.Response) {
	var wait time.Duration

	// 1) Try Retry-After header (seconds).
	if retryAfterHeader != "" {
		if secs, err := strconv.Atoi(retryAfterHeader); err == nil && secs > 0 {
			wait = time.Duration(secs)*time.Second + time.Second // +1s buffer
		}
	}

	// 2) Try x-ratelimit-reset header (Unix epoch seconds).
	if wait == 0 && resp != nil {
		if resetStr := resp.Header.Get("x-ratelimit-reset"); resetStr != "" {
			if epoch, err := strconv.ParseInt(resetStr, 10, 64); err == nil {
				resetAt := time.Unix(epoch, 0)
				wait = time.Until(resetAt) + 2*time.Second
			}
		}
	}

	// 3) Exponential back-off fallback: 5s, 15s, 45s, 135s, ...
	if wait <= 0 {
		wait = time.Duration(5<<uint(attempt)) * time.Second
		if wait > 5*time.Minute {
			wait = 5 * time.Minute
		}
	}

	log.Printf("Rate limit hit (attempt %d) — sleeping %s before retrying...", attempt+1, wait.Round(time.Second))
	time.Sleep(wait)
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

// isRateLimitGraphQLError checks whether a GraphQL error response contains
// a rate-limit error message (HTTP 200 but the server says budget is exhausted).
func isRateLimitGraphQLError(gqlResp *graphqlResponse) bool {
	for _, e := range gqlResp.Errors {
		lower := strings.ToLower(e.Message)
		if strings.Contains(lower, "rate limit") ||
			strings.Contains(lower, "abuse") ||
			strings.Contains(lower, "secondary rate") {
			return true
		}
	}
	return false
}

// Do sends a GraphQL request and unmarshals the response data into result.
// It automatically retries on rate-limit errors (HTTP 429 and GraphQL-level)
// with exponential back-off and request pacing.
func (c *Client) Do(req Request, result any) error {
	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal graphql request: %w", err)
	}

	maxRetries := c.MaxRetries
	if maxRetries <= 0 {
		maxRetries = DefaultMaxRetries
	}

	for attempt := 0; attempt <= maxRetries; attempt++ {
		c.pace()

		httpReq, err := http.NewRequestWithContext(context.Background(), "POST", Endpoint, bytes.NewReader(body))
		if err != nil {
			return fmt.Errorf("create request: %w", err)
		}
		httpReq.Header.Set("Content-Type", "application/json")

		resp, err := c.HTTPClient.Do(httpReq)
		if err != nil {
			return fmt.Errorf("graphql request: %w", err)
		}

		respBody, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return fmt.Errorf("read response: %w", err)
		}

		// HTTP 429 — explicit rate limit.
		if resp.StatusCode == http.StatusTooManyRequests {
			if attempt < maxRetries {
				sleepForRateLimit(attempt, resp.Header.Get("Retry-After"), resp)
				continue
			}
			retryAfter := resp.Header.Get("Retry-After")
			return &RateLimitError{
				StatusCode: resp.StatusCode,
				RetryAfter: retryAfter,
				Body:       string(respBody),
			}
		}

		// HTTP 403 — may also be a rate limit (secondary/abuse detection).
		if resp.StatusCode == http.StatusForbidden {
			bodyLower := strings.ToLower(string(respBody))
			if strings.Contains(bodyLower, "rate limit") || strings.Contains(bodyLower, "abuse") {
				if attempt < maxRetries {
					sleepForRateLimit(attempt, resp.Header.Get("Retry-After"), resp)
					continue
				}
				return &RateLimitError{
					StatusCode: resp.StatusCode,
					RetryAfter: resp.Header.Get("Retry-After"),
					Body:       string(respBody),
				}
			}
		}

		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("graphql HTTP %d: %s", resp.StatusCode, string(respBody))
		}

		var gqlResp graphqlResponse
		if err := json.Unmarshal(respBody, &gqlResp); err != nil {
			return fmt.Errorf("unmarshal response: %w", err)
		}

		// GraphQL-level rate limit error (HTTP 200 but error message).
		if isRateLimitGraphQLError(&gqlResp) {
			if attempt < maxRetries {
				sleepForRateLimit(attempt, resp.Header.Get("Retry-After"), resp)
				continue
			}
			msgs := make([]string, len(gqlResp.Errors))
			for i, e := range gqlResp.Errors {
				msgs[i] = e.Message
			}
			return fmt.Errorf("graphql rate limit exhausted after %d retries: %s", maxRetries, strings.Join(msgs, "; "))
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

	return fmt.Errorf("graphql request failed after %d retries", maxRetries)
}

// DoREST sends a REST API request to the GitHub REST API.
// method is the HTTP method (GET, POST, PATCH, DELETE).
// path is the URL path (e.g., "/users/{owner}/projects/{number}/views").
// body is marshaled to JSON for the request body (nil for GET/DELETE).
// result is unmarshaled from the JSON response (nil to ignore response body).
// It automatically retries on rate-limit errors with exponential back-off.
func (c *Client) DoREST(method, path string, body any, result any) error {
	var reqJSON []byte
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal REST body: %w", err)
		}
		reqJSON = b
	}

	maxRetries := c.MaxRetries
	if maxRetries <= 0 {
		maxRetries = DefaultMaxRetries
	}

	for attempt := 0; attempt <= maxRetries; attempt++ {
		c.pace()

		var reqBody io.Reader
		if reqJSON != nil {
			reqBody = bytes.NewReader(reqJSON)
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

		respBody, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return fmt.Errorf("read REST response: %w", err)
		}

		if resp.StatusCode == http.StatusTooManyRequests {
			if attempt < maxRetries {
				sleepForRateLimit(attempt, resp.Header.Get("Retry-After"), resp)
				continue
			}
			retryAfter := resp.Header.Get("Retry-After")
			return &RateLimitError{
				StatusCode: resp.StatusCode,
				RetryAfter: retryAfter,
				Body:       string(respBody),
			}
		}

		// HTTP 403 may be a secondary/abuse rate limit.
		if resp.StatusCode == http.StatusForbidden {
			bodyLower := strings.ToLower(string(respBody))
			if strings.Contains(bodyLower, "rate limit") || strings.Contains(bodyLower, "abuse") {
				if attempt < maxRetries {
					sleepForRateLimit(attempt, resp.Header.Get("Retry-After"), resp)
					continue
				}
				return &RateLimitError{
					StatusCode: resp.StatusCode,
					RetryAfter: resp.Header.Get("Retry-After"),
					Body:       string(respBody),
				}
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

	return fmt.Errorf("REST %s %s failed after %d retries", method, path, maxRetries)
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
