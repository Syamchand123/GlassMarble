// Package storage — state_test.go
// Tests for docs_state.json persistence and AtomicWriteFile.
package storage

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStateManager_LoadEmptyOnMissingFile(t *testing.T) {
	dir := t.TempDir()
	sm := NewStateManager(dir)

	state, err := sm.Load()
	require.NoError(t, err)
	require.NotNil(t, state)
	assert.Equal(t, 1, state.SchemaVersion)
	assert.Empty(t, state.Documents)
}

func TestStateManager_SaveAndLoad_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	sm := NewStateManager(dir)

	state := &DocEngineState{
		SchemaVersion: 1,
		LastCommit:    "abc123",
		GeneratedAt:   time.Now().UTC().Truncate(time.Second),
		Documents: map[string]*DocumentState{
			"docs/auth.md": {
				FileHash:          "sha256:deadbeef",
				LastUpdatedCommit: "abc123",
				FreshnessScore:    100,
				Sections: map[string]*SectionState{
					"interface": {
						ASTSubgraphHash: "sha256:cafebabe",
						RenderMode:      "llm",
						Provider:        "anthropic/claude-3-5-sonnet",
						LastTokenCost:   187,
						LastRenderMs:    1240,
					},
				},
			},
		},
	}

	require.NoError(t, sm.Save(state))

	loaded, err := sm.Load()
	require.NoError(t, err)

	assert.Equal(t, "abc123", loaded.LastCommit)
	assert.Equal(t, 1, loaded.SchemaVersion)
	require.Contains(t, loaded.Documents, "docs/auth.md")

	doc := loaded.Documents["docs/auth.md"]
	assert.Equal(t, 100, doc.FreshnessScore)
	assert.Equal(t, "sha256:deadbeef", doc.FileHash)
	require.Contains(t, doc.Sections, "interface")

	sec := doc.Sections["interface"]
	assert.Equal(t, "sha256:cafebabe", sec.ASTSubgraphHash)
	assert.Equal(t, "llm", sec.RenderMode)
	assert.Equal(t, 187, sec.LastTokenCost)
}

func TestStateManager_Save_IsAtomic(t *testing.T) {
	dir := t.TempDir()
	sm := NewStateManager(dir)

	// Verify no .tmp file lingers after a successful Save.
	state := &DocEngineState{SchemaVersion: 1, Documents: map[string]*DocumentState{}}
	require.NoError(t, sm.Save(state))

	tmpPath := filepath.Join(dir, "docs_state.json.tmp")
	_, err := os.Stat(tmpPath)
	assert.True(t, os.IsNotExist(err), ".tmp file should not exist after successful save")
}

func TestGetOrCreateDocState(t *testing.T) {
	state := &DocEngineState{
		SchemaVersion: 1,
		Documents:     make(map[string]*DocumentState),
	}

	ds := GetOrCreateDocState(state, "docs/auth.md")
	require.NotNil(t, ds)
	assert.NotNil(t, ds.Sections)

	// Second call returns the same instance.
	ds2 := GetOrCreateDocState(state, "docs/auth.md")
	assert.Same(t, ds, ds2)

	// Different path creates a new entry.
	ds3 := GetOrCreateDocState(state, "docs/arch.md")
	assert.NotSame(t, ds, ds3)
	assert.Len(t, state.Documents, 2)
}

func TestGetOrCreateSectionState(t *testing.T) {
	ds := &DocumentState{Sections: make(map[string]*SectionState)}

	ss := GetOrCreateSectionState(ds, "interface")
	require.NotNil(t, ss)

	// Second call returns same instance.
	ss2 := GetOrCreateSectionState(ds, "interface")
	assert.Same(t, ss, ss2)

	ss3 := GetOrCreateSectionState(ds, "errors")
	assert.NotSame(t, ss, ss3)
}

func TestAtomicWriteFile_WritesNewFile(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "docs", "auth.md")

	content := []byte("# Auth Module\n\nGenerated documentation.\n")
	changed, err := AtomicWriteFile(target, content)
	require.NoError(t, err)
	assert.True(t, changed)

	written, err := os.ReadFile(target)
	require.NoError(t, err)
	assert.Equal(t, content, written)
}

func TestAtomicWriteFile_NoopOnByteIdentical(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "auth.md")

	content := []byte("# Auth Module\n")
	require.NoError(t, os.WriteFile(target, content, 0644))

	// Second write with same content → no change.
	changed, err := AtomicWriteFile(target, content)
	require.NoError(t, err)
	assert.False(t, changed)
}

func TestAtomicWriteFile_ChangedOnDifferentContent(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "auth.md")

	original := []byte("# Auth\n")
	require.NoError(t, os.WriteFile(target, original, 0644))

	updated := []byte("# Auth Module\n\nUpdated content.\n")
	changed, err := AtomicWriteFile(target, updated)
	require.NoError(t, err)
	assert.True(t, changed)

	written, err := os.ReadFile(target)
	require.NoError(t, err)
	assert.Equal(t, updated, written)
}

func TestAtomicWriteFile_CreatesBackupOnFirstWrite(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "auth.md")

	original := []byte("# Original human content\n")
	require.NoError(t, os.WriteFile(target, original, 0644))

	_, err := AtomicWriteFile(target, []byte("# Updated by engine\n"))
	require.NoError(t, err)

	// .gmb.bak should contain the original content.
	backup, err := os.ReadFile(target + ".gmb.bak")
	require.NoError(t, err)
	assert.Equal(t, original, backup)
}

func TestAtomicWriteFile_NoTmpFileLeftOnSuccess(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "auth.md")

	_, err := AtomicWriteFile(target, []byte("content\n"))
	require.NoError(t, err)

	_, statErr := os.Stat(target + ".tmp")
	assert.True(t, os.IsNotExist(statErr))
}

func TestFileHash(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.md")
	require.NoError(t, os.WriteFile(path, []byte("hello"), 0644))

	hash := FileHash(path)
	assert.NotEmpty(t, hash)
	assert.Len(t, hash, 64) // SHA256 hex = 64 chars

	// Missing file returns empty string.
	assert.Empty(t, FileHash(filepath.Join(dir, "missing.md")))
}

func TestSha256sum_Deterministic(t *testing.T) {
	data := []byte("deterministic content")
	h1 := sha256sum(data)
	h2 := sha256sum(data)
	assert.Equal(t, h1, h2)
	assert.Len(t, h1, 64)
}

// ────────────────────────────────────────────────────────────────────────────
// SQLite WAL backend (A3): dual-backend default, round-trip, migration, export
// ────────────────────────────────────────────────────────────────────────────

// Behavior change (gap A3e): SQLite WAL is now the DEFAULT backend for new
// StateManagers; GMB_DOC_STATE=json selects the legacy JSON backend.
// JSON state is never orphaned — first SQLite open auto-migrates it.
func TestStateManager_DefaultIsSQLite(t *testing.T) {
	t.Setenv("GMB_DOC_STATE", "")
	dir := t.TempDir()
	sm := NewStateManager(dir)
	assert.True(t, sm.UsingSQLite(), "default backend must be SQLite (set GMB_DOC_STATE=json for legacy JSON)")

	state := &DocEngineState{SchemaVersion: 1, Documents: map[string]*DocumentState{}}
	require.NoError(t, sm.Save(state))
	assert.FileExists(t, filepath.Join(dir, "docs_state.db"))
	assert.NoFileExists(t, filepath.Join(dir, "docs_state.json"))
}

func TestStateManager_SQLiteRoundTrip(t *testing.T) {
	t.Setenv("GMB_DOC_STATE", "sqlite")
	dir := t.TempDir()
	sm := NewStateManager(dir)
	require.True(t, sm.UsingSQLite())

	fixedDoc := time.Date(2026, 5, 1, 12, 0, 0, 123456789, time.UTC)
	fixedSec := time.Date(2026, 5, 2, 8, 30, 0, 987654321, time.UTC)
	state := &DocEngineState{
		SchemaVersion: 1,
		LastCommit:    "abc123",
		Documents: map[string]*DocumentState{
			"docs/auth.md": {
				FileHash:          "sha256:deadbeef",
				LastUpdatedCommit: "abc123",
				LastUpdatedAt:     fixedDoc,
				FreshnessScore:    87,
				CommitsBehind:     2,
				Sections: map[string]*SectionState{
					"interface": {
						ASTSubgraphHash:  "sha256:cafebabe",
						RenderMode:       "llm",
						Provider:         "anthropic/claude-3-5-sonnet",
						LastTokenCost:    187,
						LastRenderMs:     1240,
						LastUpdatedAt:    fixedSec,
						LastRenderedBody: "# Interface\n\nBody.\n",
					},
					"untouched": {RenderMode: "deterministic"},
				},
			},
		},
	}
	require.NoError(t, sm.Save(state))

	dbPath := filepath.Join(dir, "docs_state.db")
	assert.FileExists(t, dbPath)

	// WAL mode must actually be on.
	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	defer db.Close()
	var mode string
	require.NoError(t, db.QueryRow(`PRAGMA journal_mode;`).Scan(&mode))
	assert.Equal(t, "wal", strings.ToLower(mode))

	loaded, err := sm.Load()
	require.NoError(t, err)
	assert.Equal(t, 1, loaded.SchemaVersion)
	assert.Equal(t, "abc123", loaded.LastCommit)
	require.Contains(t, loaded.Documents, "docs/auth.md")
	doc := loaded.Documents["docs/auth.md"]
	assert.Equal(t, "sha256:deadbeef", doc.FileHash)
	assert.Equal(t, 87, doc.FreshnessScore)
	assert.Equal(t, 2, doc.CommitsBehind)
	assert.True(t, fixedDoc.Equal(doc.LastUpdatedAt))
	require.Contains(t, doc.Sections, "interface")
	sec := doc.Sections["interface"]
	assert.Equal(t, "sha256:cafebabe", sec.ASTSubgraphHash)
	assert.Equal(t, "llm", sec.RenderMode)
	assert.Equal(t, "anthropic/claude-3-5-sonnet", sec.Provider)
	assert.Equal(t, 187, sec.LastTokenCost)
	assert.Equal(t, int64(1240), sec.LastRenderMs)
	assert.True(t, fixedSec.Equal(sec.LastUpdatedAt))
	assert.Equal(t, "# Interface\n\nBody.\n", sec.LastRenderedBody)
	// Zero-time section round-trips as zero time.
	assert.True(t, doc.Sections["untouched"].LastUpdatedAt.IsZero())
}

func TestStateManager_SQLiteMigratesFromJSON(t *testing.T) {
	// Backend pin (not a contract change): the legacy fixture must be
	// written as JSON, so force the JSON backend for setup; the migration
	// assertion below is unchanged.
	t.Setenv("GMB_DOC_STATE", "json")
	dir := t.TempDir()

	legacy := &DocEngineState{
		SchemaVersion: 1,
		LastCommit:    "mig123",
		Documents: map[string]*DocumentState{
			"docs/m.md": {
				FileHash:          "sha256:legacy",
				LastUpdatedCommit: "mig123",
				FreshnessScore:    100,
				Sections: map[string]*SectionState{
					"s": {ASTSubgraphHash: "h", RenderMode: "llm", LastRenderedBody: "body\n"},
				},
			},
		},
	}
	require.NoError(t, NewStateManager(dir).Save(legacy))

	// First SQLite open migrates JSON→SQLite.
	t.Setenv("GMB_DOC_STATE", "sqlite")
	sm := NewStateManager(dir)
	loaded, err := sm.Load()
	require.NoError(t, err)
	assert.Equal(t, "mig123", loaded.LastCommit)
	require.Contains(t, loaded.Documents, "docs/m.md")
	assert.Equal(t, "sha256:legacy", loaded.Documents["docs/m.md"].FileHash)
	assert.Equal(t, "body\n", loaded.Documents["docs/m.md"].Sections["s"].LastRenderedBody)

	// A post-migration SQLite write keeps migrated rows (no clobber).
	ds := GetOrCreateDocState(loaded, "docs/new.md")
	ds.FileHash = "sha256:new"
	require.NoError(t, sm.Save(loaded))
	reloaded, err := sm.Load()
	require.NoError(t, err)
	assert.Contains(t, reloaded.Documents, "docs/m.md")
	assert.Contains(t, reloaded.Documents, "docs/new.md")
}

func TestStateManager_SQLiteUsedWhenDBExists(t *testing.T) {
	t.Setenv("GMB_DOC_STATE", "sqlite")
	dir := t.TempDir()
	sm := NewStateManager(dir)
	require.NoError(t, sm.Save(&DocEngineState{
		SchemaVersion: 1,
		LastCommit:    "db1",
		Documents:     map[string]*DocumentState{},
	}))

	// Without the env force, the existing db still selects SQLite.
	t.Setenv("GMB_DOC_STATE", "")
	sm2 := NewStateManager(dir)
	assert.True(t, sm2.UsingSQLite())
	loaded, err := sm2.Load()
	require.NoError(t, err)
	assert.Equal(t, "db1", loaded.LastCommit)
}

func TestExportStateJSON(t *testing.T) {
	require.Error(t, func() error { _, err := ExportStateJSON(nil); return err }())

	// JSON backend export (backend pin: GMB_DOC_STATE=json forces JSON now
	// that SQLite is the default; assertions unchanged).
	t.Setenv("GMB_DOC_STATE", "json")
	dir := t.TempDir()
	sm := NewStateManager(dir)
	require.NoError(t, sm.Save(&DocEngineState{
		SchemaVersion: 1,
		LastCommit:    "exp1",
		Documents: map[string]*DocumentState{
			"docs/a.md": {FileHash: "sha256:a", Sections: map[string]*SectionState{}},
		},
	}))
	data, err := ExportStateJSON(sm)
	require.NoError(t, err)
	var decoded DocEngineState
	require.NoError(t, json.Unmarshal(data, &decoded))
	assert.Equal(t, "exp1", decoded.LastCommit)
	assert.Contains(t, decoded.Documents, "docs/a.md")

	// SQLite backend export has the identical shape.
	t.Setenv("GMB_DOC_STATE", "sqlite")
	dir2 := t.TempDir()
	sm2 := NewStateManager(dir2)
	require.NoError(t, sm2.Save(&DocEngineState{
		SchemaVersion: 1,
		LastCommit:    "exp2",
		Documents:     map[string]*DocumentState{},
	}))
	data2, err := ExportStateJSON(sm2)
	require.NoError(t, err)
	var decoded2 DocEngineState
	require.NoError(t, json.Unmarshal(data2, &decoded2))
	assert.Equal(t, "exp2", decoded2.LastCommit)
}
