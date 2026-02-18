// Package cache provides JSON file caching for API responses.
package cache

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Timestamp returns a filename-safe timestamp for the current time.
func Timestamp() string {
	return time.Now().Format("2006-01-02T15-04-05")
}

// SafeString replaces characters that are problematic in filenames.
func SafeString(s string) string {
	s = strings.ReplaceAll(s, "/", "-")
	s = strings.ReplaceAll(s, " ", "_")
	return s
}

// Write saves data as pretty-printed JSON to a cache file.
// dir is the cache directory, key is the full filename (including extension).
// Returns the full path of the created file.
func Write(dir, key string, data any) string {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Printf("Warning: could not create cache dir: %v", err)
		return ""
	}

	path := filepath.Join(dir, key)

	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		log.Printf("Warning: could not marshal cache data: %v", err)
		return ""
	}

	if err := os.WriteFile(path, jsonData, 0o644); err != nil {
		log.Printf("Warning: could not write cache file: %v", err)
		return ""
	}

	log.Printf("Cached data to %s (%d bytes)", path, len(jsonData))
	return path
}

// ReadLatest finds the most recent cache file whose name starts with prefix
// and unmarshals it into the target slice type.
func ReadLatest[T any](dir, prefix string) ([]T, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var latest string
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), prefix) && strings.HasSuffix(e.Name(), ".json") {
			latest = e.Name()
		}
	}

	if latest == "" {
		return nil, nil
	}

	path := filepath.Join(dir, latest)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var items []T
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, fmt.Errorf("unmarshal %s: %w", path, err)
	}

	log.Printf("Loaded %d items from cache: %s", len(items), path)
	return items, nil
}

// Clean removes old cache files in dir whose name starts with prefix,
// keeping only the keep newest. Files are sorted by name (which embeds a
// timestamp). Returns the number of files removed.
func Clean(dir, prefix string, keep int) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}

	var matches []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), prefix) && strings.HasSuffix(e.Name(), ".json") {
			matches = append(matches, e.Name())
		}
	}

	// Sorted ascending by name (timestamp in name ensures chronological order)
	sort.Strings(matches)

	if len(matches) <= keep {
		return 0, nil
	}

	toRemove := matches[:len(matches)-keep]
	removed := 0
	for _, name := range toRemove {
		path := filepath.Join(dir, name)
		if err := os.Remove(path); err != nil {
			log.Printf("Warning: could not remove cache file %s: %v", path, err)
		} else {
			log.Printf("  Removed old cache file: %s", name)
			removed++
		}
	}
	return removed, nil
}

// DefaultCacheLimit is the number of cache files to keep per prefix when no
// explicit limit is provided.
const DefaultCacheLimit = 5

// Enforce runs cache cleanup on dir, keeping limit files per prefix.
// If limit <= 0 it falls back to DefaultCacheLimit.
func Enforce(dir string, limit int) {
	if limit <= 0 {
		limit = DefaultCacheLimit
	}
	removed, err := CleanAll(dir, limit)
	if err != nil {
		log.Printf("Warning: cache cleanup error: %v", err)
		return
	}
	if removed > 0 {
		log.Printf("Cache cleanup: removed %d old file(s) (keeping %d per prefix)", removed, limit)
	}
}

// CleanAll removes old cache files across all known prefixes in the given dir,
// keeping only keep newest per prefix. Returns total files removed.
func CleanAll(dir string, keep int) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}

	// Discover prefixes: everything before the first timestamp-like pattern.
	// Our files look like "enhancements_2025-01-15T10-30-00.json" or
	// "issues_2025-01-15T10-30-05.json", so the prefix is everything
	// before the date portion (YYYY-).
	prefixSet := make(map[string]bool)
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".json") {
			continue
		}
		// Find the first digit sequence that looks like a year (4 digits followed by -)
		for i := 0; i < len(name)-5; i++ {
			if name[i] >= '0' && name[i] <= '9' &&
				name[i+1] >= '0' && name[i+1] <= '9' &&
				name[i+2] >= '0' && name[i+2] <= '9' &&
				name[i+3] >= '0' && name[i+3] <= '9' &&
				name[i+4] == '-' {
				prefixSet[name[:i]] = true
				break
			}
		}
	}

	total := 0
	for prefix := range prefixSet {
		n, err := Clean(dir, prefix, keep)
		if err != nil {
			return total, fmt.Errorf("cleaning prefix %q: %w", prefix, err)
		}
		total += n
	}
	return total, nil
}
