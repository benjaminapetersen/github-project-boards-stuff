package main

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const cacheDir = ".cache"

// cacheKey builds a filename-safe key from the query parameters.
func cacheKey(config Config) string {
	safe := func(s string) string {
		s = strings.ReplaceAll(s, "/", "-")
		s = strings.ReplaceAll(s, " ", "_")
		return s
	}

	parts := []string{"items", safe(config.Org)}
	if config.Milestone != "" {
		parts = append(parts, safe(config.Milestone))
	}
	for _, r := range config.Repos {
		parts = append(parts, safe(r))
	}
	parts = append(parts, time.Now().Format("2006-01-02T15-04-05"))

	return strings.Join(parts, "_") + ".json"
}

// cachePrefix returns the prefix all matching cache files share (without timestamp).
func cachePrefix(config Config) string {
	safe := func(s string) string {
		s = strings.ReplaceAll(s, "/", "-")
		s = strings.ReplaceAll(s, " ", "_")
		return s
	}

	parts := []string{"items", safe(config.Org)}
	if config.Milestone != "" {
		parts = append(parts, safe(config.Milestone))
	}
	for _, r := range config.Repos {
		parts = append(parts, safe(r))
	}

	return strings.Join(parts, "_") + "_"
}

// writeCache writes items as pretty-printed JSON to the cache directory.
func writeCache(config Config, items []ProjectItem) {
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		log.Printf("Warning: could not create cache dir: %v", err)
		return
	}

	filename := cacheKey(config)
	path := filepath.Join(cacheDir, filename)

	jsonData, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		log.Printf("Warning: could not marshal cache data: %v", err)
		return
	}

	if err := os.WriteFile(path, jsonData, 0o644); err != nil {
		log.Printf("Warning: could not write cache file: %v", err)
		return
	}

	log.Printf("Cached %d items to %s (%d bytes)", len(items), path, len(jsonData))
}

// readCacheLatest finds the most recent cache file matching the config prefix.
func readCacheLatest(config Config) []ProjectItem {
	prefix := cachePrefix(config)

	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		return nil
	}

	var latest string
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), prefix) && strings.HasSuffix(e.Name(), ".json") {
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

	var items []ProjectItem
	if err := json.Unmarshal(data, &items); err != nil {
		log.Printf("Warning: could not parse cache file %s: %v", path, err)
		return nil
	}

	log.Printf("Loaded %d items from cache: %s", len(items), path)
	return items
}
