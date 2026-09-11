// Package storage implements persistence for the Documentation Intelligence Engine.
//
// This file defines the docs_state.json schema and provides safe, atomic
// read/write operations with advisory file locking. All writes follow
// GlassMarble's established MVCC contract: tmp → fsync → rename → verify.
package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// ────────────────────────────────────────────────────────────────────────────
// docs_state.json schema
// ────────────────────────────────────────────────────────────────────────────

// DocEngineState is the top-level schema of .glassmarble/docs_state.json.
// It tracks the per-section AST subgraph hashes used to avoid redundant
// LLM calls and unnecessary file writes.
type DocEngineState struct {
	// SchemaVersion must equal 1. Used for future migrations.
	SchemaVersion int `json:"schema_version"`

	// LastCommit is the HEAD commit hash when this state was last updated.
	LastCommit string `json:"last_commit"`

	// GeneratedAt is when this state was last written.
	GeneratedAt time.Time `json:"generated_at"`

	// Documents maps DocSpec.TargetPath to per-document state.
	Documents map[string]*DocumentState `json:"documents"`
}

// DocumentState tracks the state of a single managed document.
type DocumentState struct {
	// FileHash is the SHA256 of the current file content on disk.
	FileHash string `json:"file_hash"`

	// LastUpdatedCommit is the commit hash when this doc was last regenerated.
	LastUpdatedCommit string `json:"last_updated_commit"`

	// LastUpdatedAt is when the document was last regenerated.
	LastUpdatedAt time.Time `json:"last_updated_at"`

	// FreshnessScore is the computed freshness (0-100%).
	// 100 means fully in sync with HEAD.
	FreshnessScore int `json:"freshness_score"`

	// CommitsBehind is the number of in-scope commits since LastUpdatedCommit
	// (master-plan Appendix B). Updated by Run() and reported live by Check().
	CommitsBehind int `json:"commits_behind,omitempty"`

	// Sections maps SectionSpec.ID to per-section state.
	Sections map[string]*SectionState `json:"sections"`
}

// SectionState tracks the state of a single managed section within a document.
type SectionState struct {
	// ASTSubgraphHash is SHA256 of the deterministic inputs for this section:
	// relevant symbol signatures + doc comments + section instruction.
	// If this matches the re-computed hash, the section is clean → 0 tokens.
	ASTSubgraphHash string `json:"ast_subgraph_hash"`

	// RenderMode records how this section was last rendered.
	// "llm" | "deterministic"
	RenderMode string `json:"render_mode"`

	// Provider records which LLM provider was used (e.g., "anthropic/claude-3-5-sonnet").
	// Empty for deterministic renders.
	Provider string `json:"provider,omitempty"`

	// LastTokenCost is the total token count (input + output) for the last LLM call.
	LastTokenCost int `json:"last_token_cost,omitempty"`

	// LastRenderMs is the wall-clock time in milliseconds for the last render.
	LastRenderMs int64 `json:"last_render_ms,omitempty"`

	// LastUpdatedAt is when this section was last regenerated.
	LastUpdatedAt time.Time `json:"last_updated_at,omitempty"`

	// LastRenderedBody is the merged managed-zone body written by the last
	// successful render. It serves as BASE for the next run's Stage 8
	// 3-way merge (BASE vs human-edited OURS vs fresh machine THEIRS).
	LastRenderedBody string `json:"last_rendered_body,omitempty"`
}

// ────────────────────────────────────────────────────────────────────────────
// StateManager — atomic read/write of docs_state.json
// ────────────────────────────────────────────────────────────────────────────

// StateManager provides safe read and write access to the doc-engine state.
// The default backend is SQLite WAL (docs_state.db); the legacy
// docs_state.json backend (atomic tmp → fsync → rename with a SHA256
// integrity check and a best-effort O_EXCL advisory lock file) is used only
// when GMB_DOC_STATE=json forces it. First SQLite open auto-migrates an
// existing docs_state.json (see ensureSQLiteMigrated in store_sqlite.go).
// Callers use Load/Save identically on either backend.
type StateManager struct {
	path     string
	lockPath string
	// dir is the storage directory (.glassmarble/).
	dir string
	// dbPath is the SQLite state file (storage dir + sqliteFileName).
	dbPath string
}

// NewStateManager creates a StateManager for the given storage directory
// (.glassmarble/).
func NewStateManager(storageDir string) *StateManager {
	return &StateManager{
		path:     filepath.Join(storageDir, "docs_state.json"),
		lockPath: filepath.Join(storageDir, "docs_state.lock"),
		dir:      storageDir,
		dbPath:   filepath.Join(storageDir, sqliteFileName),
	}
}

// Load reads the current state from whichever backend is active (SQLite when
// UsingSQLite is true, else docs_state.json).
// If no state exists yet, an empty state is returned (not an error).
func (sm *StateManager) Load() (*DocEngineState, error) {
	if sm.useSQLite() {
		return sm.loadFromSQLite()
	}
	return sm.loadJSON()
}

// loadJSON reads docs_state.json and returns the current state.
// If the file does not exist, an empty state is returned (not an error).
func (sm *StateManager) loadJSON() (*DocEngineState, error) {
	data, err := os.ReadFile(sm.path)
	if os.IsNotExist(err) {
		return sm.emptyState(), nil
	}
	if err != nil {
		return nil, fmt.Errorf("doc_engine: reading docs_state.json: %w", err)
	}

	var state DocEngineState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("doc_engine: parsing docs_state.json: %w", err)
	}
	if state.Documents == nil {
		state.Documents = make(map[string]*DocumentState)
	}
	return &state, nil
}

// Save writes state atomically to whichever backend is active (SQLite when
// UsingSQLite is true, else docs_state.json via tmp → fsync → rename with a
// SHA256 verify).
// Pattern: write to tmp file → fsync → rename → verify SHA256.
// An advisory lock file prevents concurrent writes.
func (sm *StateManager) Save(state *DocEngineState) error {
	if sm.useSQLite() {
		return sm.saveToSQLite(state)
	}
	return sm.saveJSON(state)
}

// saveJSON writes state to docs_state.json atomically.
// Pattern: write to tmp file → fsync → rename → verify SHA256.
// An advisory lock file prevents concurrent writes.
func (sm *StateManager) saveJSON(state *DocEngineState) error {
	// Acquire advisory lock.
	lockFile, err := acquireLock(sm.lockPath)
	if err != nil {
		return fmt.Errorf("doc_engine: acquiring docs_state.lock: %w", err)
	}
	defer releaseLock(lockFile)

	state.GeneratedAt = time.Now().UTC()

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("doc_engine: marshaling docs_state.json: %w", err)
	}

	// Compute expected SHA256 for post-write verification.
	expectedHash := sha256sum(data)

	tmpPath := sm.path + ".tmp"

	// Write to tmp.
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("doc_engine: writing docs_state.json.tmp: %w", err)
	}

	// fsync the tmp file to ensure durability before rename.
	if err := fsyncFile(tmpPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("doc_engine: fsync docs_state.json.tmp: %w", err)
	}

	// Atomic rename.
	if err := os.Rename(tmpPath, sm.path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("doc_engine: renaming docs_state.json: %w", err)
	}

	// Verify: re-read and check SHA256.
	written, err := os.ReadFile(sm.path)
	if err != nil {
		return fmt.Errorf("doc_engine: verifying docs_state.json: %w", err)
	}
	if got := sha256sum(written); got != expectedHash {
		return fmt.Errorf("doc_engine: docs_state.json integrity check failed (expected %s, got %s)", expectedHash, got)
	}

	return nil
}

// emptyState returns a new, empty DocEngineState with the current schema version.
func (sm *StateManager) emptyState() *DocEngineState {
	return &DocEngineState{
		SchemaVersion: 1,
		Documents:     make(map[string]*DocumentState),
	}
}

// GetOrCreateDocState returns the DocumentState for a target path,
// creating an empty one if it doesn't exist.
func GetOrCreateDocState(state *DocEngineState, targetPath string) *DocumentState {
	if state.Documents == nil {
		state.Documents = make(map[string]*DocumentState)
	}
	if ds, ok := state.Documents[targetPath]; ok {
		if ds.Sections == nil {
			ds.Sections = make(map[string]*SectionState)
		}
		return ds
	}
	ds := &DocumentState{
		Sections: make(map[string]*SectionState),
	}
	state.Documents[targetPath] = ds
	return ds
}

// GetOrCreateSectionState returns the SectionState for a section ID within
// a DocumentState, creating an empty one if it doesn't exist.
func GetOrCreateSectionState(ds *DocumentState, sectionID string) *SectionState {
	if ds.Sections == nil {
		ds.Sections = make(map[string]*SectionState)
	}
	if ss, ok := ds.Sections[sectionID]; ok {
		return ss
	}
	ss := &SectionState{}
	ds.Sections[sectionID] = ss
	return ss
}

// SetLastRenderedBody persists the merged section body written by a
// successful render so the next run can use it as the 3-way merge BASE.
// A nil StateManager is a no-op. State save failures are returned to the
// caller (non-fatal per P5: callers should treat them as warnings).
func SetLastRenderedBody(sm *StateManager, targetPath, sectionID, body string) error {
	if sm == nil {
		return nil
	}
	state, err := sm.Load()
	if err != nil {
		return fmt.Errorf("doc_engine: loading state: %w", err)
	}
	ds := GetOrCreateDocState(state, targetPath)
	ss := GetOrCreateSectionState(ds, sectionID)
	ss.LastRenderedBody = body
	if err := sm.Save(state); err != nil {
		return fmt.Errorf("doc_engine: saving section body: %w", err)
	}
	return nil
}

// ────────────────────────────────────────────────────────────────────────────
// Atomic File Writer — for .md files (used by patcher/writer)
// ────────────────────────────────────────────────────────────────────────────

// AtomicWriteFile writes content to targetPath using the MVCC contract:
// tmp → fsync → rename → verify SHA256.
//
// If the current file content is byte-identical to content, no write occurs
// (preserving zero git diff on no-op renders).
//
// On the first write, a .gmb.bak backup is created from the existing file.
// Returns (changed bool, err error).
func AtomicWriteFile(targetPath string, content []byte) (bool, error) {
	// Check if file exists and is byte-identical.
	existing, readErr := os.ReadFile(targetPath)
	if readErr == nil && byteEqual(existing, content) {
		return false, nil // byte-identical → no write, no git diff
	}

	// Create parent directory if needed.
	if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
		return false, fmt.Errorf("doc_engine: creating directory for %s: %w", targetPath, err)
	}

	// On first write, create a .gmb.bak backup of the existing file.
	backupPath := targetPath + ".gmb.bak"
	if readErr == nil {
		if _, backupErr := os.Stat(backupPath); os.IsNotExist(backupErr) {
			_ = os.WriteFile(backupPath, existing, 0644)
		}
	}

	tmpPath := targetPath + ".tmp"
	expectedHash := sha256sum(content)

	if err := os.WriteFile(tmpPath, content, 0644); err != nil {
		return false, fmt.Errorf("doc_engine: writing tmp file for %s: %w", targetPath, err)
	}

	if err := fsyncFile(tmpPath); err != nil {
		_ = os.Remove(tmpPath)
		return false, fmt.Errorf("doc_engine: fsync %s: %w", tmpPath, err)
	}

	if err := os.Rename(tmpPath, targetPath); err != nil {
		_ = os.Remove(tmpPath)
		return false, fmt.Errorf("doc_engine: renaming to %s: %w", targetPath, err)
	}

	// Verify.
	written, err := os.ReadFile(targetPath)
	if err != nil {
		return false, fmt.Errorf("doc_engine: verifying %s: %w", targetPath, err)
	}
	if got := sha256sum(written); got != expectedHash {
		return false, fmt.Errorf("doc_engine: integrity check failed for %s", targetPath)
	}

	return true, nil
}

// RestoreFromBackup restores targetPath from its .gmb.bak backup (created by
// AtomicWriteFile on first write). The restore itself goes through
// AtomicWriteFile, so a crash mid-restore still leaves either the pre-restore
// or the fully restored bytes on disk — never torn output. The backup file is
// kept so restores are repeatable. Returns an error when no backup exists.
func RestoreFromBackup(targetPath string) error {
	backupPath := targetPath + ".gmb.bak"
	backup, err := os.ReadFile(backupPath)
	if os.IsNotExist(err) {
		return fmt.Errorf("doc_engine: no backup %s to restore %s", backupPath, targetPath)
	}
	if err != nil {
		return fmt.Errorf("doc_engine: reading backup %s: %w", backupPath, err)
	}
	if _, err := AtomicWriteFile(targetPath, backup); err != nil {
		return fmt.Errorf("doc_engine: restoring %s from backup: %w", targetPath, err)
	}
	return nil
}

// FileHash computes and returns the SHA256 hex digest of a file's current content.
// Returns an empty string if the file cannot be read.
func FileHash(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return sha256sum(data)
}

// ────────────────────────────────────────────────────────────────────────────
// Internal helpers
// ────────────────────────────────────────────────────────────────────────────

func sha256sum(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func byteEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// fsyncFile opens a file and calls Sync() to flush to disk.
func fsyncFile(path string) error {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

// acquireLock creates an exclusive advisory lock file.
// This is a best-effort cooperative lock — it prevents two concurrent
// gmb processes from stomping each other's docs_state.json writes.
func acquireLock(lockPath string) (*os.File, error) {
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		// Another process holds the lock. Return the error to the caller;
		// the engine will warn and continue without updating state.
		return nil, err
	}
	_, _ = io.WriteString(f, fmt.Sprintf("%d\n", os.Getpid()))
	return f, nil
}

// releaseLock closes and removes the advisory lock file.
func releaseLock(f *os.File) {
	if f == nil {
		return
	}
	path := f.Name()
	f.Close()
	_ = os.Remove(path)
}
