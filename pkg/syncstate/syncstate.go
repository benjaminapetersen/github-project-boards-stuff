// Package syncstate tracks which items have been synced to a board so that
// the program can:
//  1. Resume from a crash — skip items already written in this run.
//  2. Run incrementally — skip items whose updatedAt date hasn't changed
//     since the last completed sync.
package syncstate

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"
)

// State is the top-level structure persisted to disk.
type State struct {
	// StartedAt is when this sync run began.
	StartedAt time.Time `json:"started_at"`

	// CompletedAt is set when all items have been processed.
	// Empty/zero means the run was interrupted and can be resumed.
	CompletedAt *time.Time `json:"completed_at,omitempty"`

	// LastFlush is updated every time the state is written to disk,
	// so a human can see when progress was last saved.
	LastFlush *time.Time `json:"last_flush,omitempty"`

	// BoardOwner + BoardName identify the destination board.
	BoardOwner string `json:"board_owner"`
	BoardName  string `json:"board_name"`
	ProjectID  string `json:"project_id"`

	// Counters tracks running totals for the current sync.
	// Updated on every flush so the file always reflects current progress.
	Counters Counters `json:"counters"`

	// Items maps NodeID → per-item sync record.
	Items map[string]ItemRecord `json:"items"`

	// Errors records items that failed during this run (for human audit).
	// Keyed by NodeID (or a description if no NodeID). Kept small — only
	// the last error message per item.
	Errors map[string]ErrorRecord `json:"errors,omitempty"`

	// path is the on-disk location (not serialized).
	path string `json:"-"`
}

// Counters holds aggregate progress numbers for a sync run.
// The JSON field names are chosen so that `jq .counters` gives a quick summary.
type Counters struct {
	TotalItems int `json:"total_items"` // total items in source (enhancements + issues after dedup)
	Processed  int `json:"processed"`   // items examined so far (== added + skipped + unchanged + errors)
	Added      int `json:"added"`       // newly added to board
	Updated    int `json:"updated"`     // already on board, fields updated
	Skipped    int `json:"skipped"`     // skipped (error or no NodeID)
	Unchanged  int `json:"unchanged"`   // skipped because updatedAt matched (incremental)
	FieldsSet  int `json:"fields_set"`  // items that had custom fields written
	ErrorCount int `json:"errors"`      // items that hit an error (add or field-set)
	Removed    int `json:"removed"`     // stale items removed from board
}

// ErrorRecord stores a single per-item error for audit purposes.
type ErrorRecord struct {
	Number  int    `json:"number"`
	Message string `json:"message"`
	At      string `json:"at"`
}

// ItemRecord tracks an individual item's sync status.
type ItemRecord struct {
	NodeID    string `json:"node_id"`
	Number    int    `json:"number"`
	UpdatedAt string `json:"updated_at"` // source issue/PR updatedAt (YYYY-MM-DD or RFC3339)
	SyncedAt  string `json:"synced_at"`  // when we last wrote this item to the board
}

// DefaultPath returns the standard location for the sync-state file.
func DefaultPath() string {
	return filepath.Join(".cache", "team-board", "sync-state.json")
}

// Load reads an existing sync-state file. Returns nil (no error) if the file
// does not exist. Returns an error only on I/O or parse failure.
func Load(path string) (*State, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read sync state: %w", err)
	}

	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parse sync state: %w", err)
	}
	s.path = path
	return &s, nil
}

// New creates a fresh State for a new sync run.
func New(path, boardOwner, boardName, projectID string) *State {
	return &State{
		StartedAt:  time.Now(),
		BoardOwner: boardOwner,
		BoardName:  boardName,
		ProjectID:  projectID,
		Items:      make(map[string]ItemRecord),
		Errors:     make(map[string]ErrorRecord),
		path:       path,
	}
}

// IsComplete returns true if the recorded run finished successfully.
func (s *State) IsComplete() bool {
	return s != nil && s.CompletedAt != nil && !s.CompletedAt.IsZero()
}

// MatchesBoard returns true if the state matches the given board destination.
func (s *State) MatchesBoard(owner, name, projectID string) bool {
	if s == nil {
		return false
	}
	return s.BoardOwner == owner && s.BoardName == name && s.ProjectID == projectID
}

// NeedsSync decides whether a given item must be synced.
//   - In resume mode (incomplete state, same board): skip items already in the record.
//   - In incremental mode (complete state): skip items whose updatedAt hasn't changed.
//   - Otherwise: sync the item.
//
// Returns (shouldSync bool, reason string).
func (s *State) NeedsSync(nodeID, updatedAt string) (bool, string) {
	if s == nil {
		return true, "no prior state"
	}

	rec, exists := s.Items[nodeID]
	if !exists {
		return true, "new item"
	}

	// Resume mode: state is incomplete (crashed mid-run).
	if !s.IsComplete() {
		return false, "already synced in this run"
	}

	// Incremental mode: state is complete (prior run finished).
	if updatedAt != "" && rec.UpdatedAt != "" && updatedAt == rec.UpdatedAt {
		return false, "unchanged since last sync"
	}

	return true, "updated since last sync"
}

// RecordItem marks an item as synced.
func (s *State) RecordItem(nodeID string, number int, updatedAt string) {
	s.Items[nodeID] = ItemRecord{
		NodeID:    nodeID,
		Number:    number,
		UpdatedAt: updatedAt,
		SyncedAt:  time.Now().Format(time.RFC3339),
	}
}

// RecordError records a per-item error for later audit.
func (s *State) RecordError(nodeID string, number int, msg string) {
	if s.Errors == nil {
		s.Errors = make(map[string]ErrorRecord)
	}
	key := nodeID
	if key == "" {
		key = fmt.Sprintf("unknown-%d", number)
	}
	s.Errors[key] = ErrorRecord{
		Number:  number,
		Message: msg,
		At:      time.Now().Format(time.RFC3339),
	}
	s.Counters.ErrorCount = len(s.Errors)
}

// SetTotal records the total number of source items to process.
func (s *State) SetTotal(n int) {
	s.Counters.TotalItems = n
}

// UpdateCounters replaces the running counters in bulk.
// Call this before Flush() to keep the on-disk file current.
func (s *State) UpdateCounters(added, updated, skipped, unchanged, fieldsSet, removed int) {
	s.Counters.Added = added
	s.Counters.Updated = updated
	s.Counters.Skipped = skipped
	s.Counters.Unchanged = unchanged
	s.Counters.FieldsSet = fieldsSet
	s.Counters.Removed = removed
	s.Counters.Processed = added + updated + skipped + unchanged + s.Counters.ErrorCount
}

// MarkComplete marks the sync run as successfully finished.
func (s *State) MarkComplete() {
	now := time.Now()
	s.CompletedAt = &now
}

// Save writes the state to disk. It creates parent directories as needed.
func (s *State) Save() error {
	if s.path == "" {
		return fmt.Errorf("sync state path not set")
	}

	// Stamp last-flush so a human can see when progress was last saved.
	now := time.Now()
	s.LastFlush = &now

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create sync state dir: %w", err)
	}

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal sync state: %w", err)
	}

	// Write to a temp file first, then rename for atomic replacement.
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write sync state tmp: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return fmt.Errorf("rename sync state: %w", err)
	}

	return nil
}

// Flush saves the state to disk, logging a warning on error.
// Intended for use inside the sync loop where a save failure is non-fatal.
func (s *State) Flush() {
	if err := s.Save(); err != nil {
		log.Printf("Warning: could not save sync state: %v", err)
	}
}

// Summary returns a human-readable summary line.
func (s *State) Summary() string {
	if s == nil {
		return "no prior sync state"
	}
	status := "INCOMPLETE (can resume)"
	if s.IsComplete() {
		status = fmt.Sprintf("complete (%s)", s.CompletedAt.Local().Format("2006-01-02 15:04"))
	}
	c := s.Counters
	return fmt.Sprintf(
		"%s | total=%d processed=%d added=%d updated=%d skipped=%d unchanged=%d errors=%d fields_set=%d removed=%d | started %s",
		status, c.TotalItems, c.Processed, c.Added, c.Updated, c.Skipped, c.Unchanged, c.ErrorCount, c.FieldsSet, c.Removed,
		s.StartedAt.Local().Format("2006-01-02 15:04"),
	)
}
