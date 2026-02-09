package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/go-github/v57/github"
)

const cacheDir = ".cache"

// cacheKey builds a filename-safe key from the query parameters.
// Example: "issues_kubernetes_enhancements_sig-auth_v1.36_open_2026-02-09T14-30-00"
func cacheKey(config Config, prefix string) string {
	safe := func(s string) string {
		s = strings.ReplaceAll(s, "/", "-")
		s = strings.ReplaceAll(s, " ", "_")
		return s
	}

	parts := []string{prefix, config.RepoOwner, config.RepoName}
	for _, l := range config.Labels {
		parts = append(parts, safe(l))
	}
	for _, m := range config.Milestones {
		parts = append(parts, safe(m))
	}
	parts = append(parts, config.State)
	parts = append(parts, time.Now().Format("2006-01-02T15-04-05"))

	return strings.Join(parts, "_") + ".json"
}

// writeCache writes data as pretty-printed JSON to the cache directory.
func writeCache(filename string, data any) {
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		log.Printf("Warning: could not create cache dir: %v", err)
		return
	}

	path := filepath.Join(cacheDir, filename)

	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		log.Printf("Warning: could not marshal cache data: %v", err)
		return
	}

	if err := os.WriteFile(path, jsonData, 0o644); err != nil {
		log.Printf("Warning: could not write cache file: %v", err)
		return
	}

	log.Printf("Cached response to %s (%d bytes)", path, len(jsonData))
}

// readCacheLatest finds the most recent cache file matching the given prefix and
// query parameters (ignoring the timestamp portion). Returns nil if no cache found.
func readCacheLatest(config Config, prefix string) []*github.Issue {
	safe := func(s string) string {
		s = strings.ReplaceAll(s, "/", "-")
		s = strings.ReplaceAll(s, " ", "_")
		return s
	}

	// Build the prefix that all matching cache files would share
	parts := []string{prefix, config.RepoOwner, config.RepoName}
	for _, l := range config.Labels {
		parts = append(parts, safe(l))
	}
	for _, m := range config.Milestones {
		parts = append(parts, safe(m))
	}
	parts = append(parts, config.State)
	filePrefix := strings.Join(parts, "_") + "_"

	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		return nil
	}

	// Find the latest matching file (directory listing is sorted alphabetically,
	// and our timestamp format sorts chronologically)
	var latest string
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), filePrefix) && strings.HasSuffix(e.Name(), ".json") {
			latest = e.Name()
		}
	}

	if latest == "" {
		return nil
	}

	path := filepath.Join(cacheDir, latest)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	var issues []*github.Issue
	if err := json.Unmarshal(data, &issues); err != nil {
		log.Printf("Warning: could not parse cache file %s: %v", path, err)
		return nil
	}

	log.Printf("Loaded %d issues from cache: %s", len(issues), path)
	return issues
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
