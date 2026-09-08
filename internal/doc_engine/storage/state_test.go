// Package storage — state_test.go
// Tests for docs_state.json persistence and AtomicWriteFile.
package storage

import (
	"os"
	"path/filepath"
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
