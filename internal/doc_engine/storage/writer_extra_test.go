// Package storage — writer_extra_test.go
//
// Whitebox coverage for the state-key vs target-path split (absolute
// writes keyed by relative DocSpec targets), the SQLite-default / JSON
// opt-out pin, migration idempotence, and the ExportStateJSON shape
// contract.
package storage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// extraBackends runs sub as a subtest once per backend. The name must not
// collide with existing helpers (state_test.go has no extra* identifiers).
func extraBackends(t *testing.T, sub func(t *testing.T)) {
	t.Helper()
	t.Run("sqlite", func(t *testing.T) {
		t.Setenv("GMB_DOC_STATE", "sqlite")
		sub(t)
	})
	t.Run("json", func(t *testing.T) {
		t.Setenv("GMB_DOC_STATE", "json")
		sub(t)
	})
}

// TestExtraWriteDocStateKeySplit guards the absolute/relative split-brain:
// WriteDoc writes targetPath (usually absolute) to disk but keys state by
// stateKey (always the repo-relative DocSpec target). Keying state by the
// absolute path would create a second document entry that hash lookups
// (which use the relative target) never find — every section would
// re-render forever.
func TestExtraWriteDocStateKeySplit(t *testing.T) {
	extraBackends(t, func(t *testing.T) {
		dir := t.TempDir()
		sm := NewStateManager(dir)

		absTarget := filepath.Join(dir, "docs", "arch.md")
		content := []byte("# Architecture\n\nBody.\n")
		res, err := WriteDoc(sm, absTarget, "docs/arch.md", content, "doc-arch", "overview", "abc123", "deterministic")
		require.NoError(t, err)
		require.True(t, res.Changed)

		state, err := sm.Load()
		require.NoError(t, err)
		require.Contains(t, state.Documents, "docs/arch.md",
			"state must be keyed by the relative DocSpec target")
		ds := state.Documents["docs/arch.md"]
		assert.True(t, strings.HasPrefix(ds.FileHash, "sha256:"))
		assert.Equal(t, "abc123", ds.LastUpdatedCommit)
		require.Contains(t, ds.Sections, "overview")
		assert.Equal(t, "deterministic", ds.Sections["overview"].RenderMode)

		// No absolute-path entry may exist anywhere in the keyspace.
		for key := range state.Documents {
			assert.False(t, filepath.IsAbs(key), "absolute state key %q splits the brain", key)
			assert.NotContains(t, key, dir, "state key %q leaks the workdir", key)
		}
		if len(state.Documents) != 1 {
			t.Errorf("expected exactly 1 document entry, got %d: %v", len(state.Documents), state.Documents)
		}

		// A byte-identical rewrite is a no-op that still resolves the key.
		res2, err := WriteDoc(sm, absTarget, "docs/arch.md", content, "doc-arch", "overview", "abc123", "deterministic")
		require.NoError(t, err)
		assert.False(t, res2.Changed)
		state2, err := sm.Load()
		require.NoError(t, err)
		assert.Len(t, state2.Documents, 1)
	})
}

// TestExtraWriteDocRelativeTargetAndKey guards the degenerate case where
// both arguments are relative: the key must equal the target exactly.
func TestExtraWriteDocRelativeTargetAndKey(t *testing.T) {
	extraBackends(t, func(t *testing.T) {
		dir := t.TempDir()
		sm := NewStateManager(dir)
		rel := filepath.Join("docs", "rel.md")
		_, err := WriteDoc(sm, filepath.Join(dir, rel), rel, []byte("# Rel\n"), "d", "s", "h", "llm")
		require.NoError(t, err)
		state, err := sm.Load()
		require.NoError(t, err)
		require.Contains(t, state.Documents, rel)
	})
}

// TestExtraBackendPin complements TestStateManager_DefaultIsSQLite with the
// explicit JSON opt-out direction.
func TestExtraBackendPin(t *testing.T) {
	t.Setenv("GMB_DOC_STATE", "json")
	dir := t.TempDir()
	sm := NewStateManager(dir)
	assert.False(t, sm.UsingSQLite(), "GMB_DOC_STATE=json must select the legacy backend")
	require.NoError(t, sm.Save(&DocEngineState{SchemaVersion: 1, Documents: map[string]*DocumentState{}}))
	assert.FileExists(t, filepath.Join(dir, "docs_state.json"))
	assert.NoFileExists(t, filepath.Join(dir, "docs_state.db"))

	// JSON round-trips doc + section state including the merge BASE body.
	state, err := sm.Load()
	require.NoError(t, err)
	ds := GetOrCreateDocState(state, "docs/k.md")
	ss := GetOrCreateSectionState(ds, "s")
	ss.ASTSubgraphHash = "h1"
	ss.RenderMode = "llm"
	ss.LastRenderedBody = "merged\n"
	require.NoError(t, sm.Save(state))
	loaded, err := sm.Load()
	require.NoError(t, err)
	require.Contains(t, loaded.Documents, "docs/k.md")
	assert.Equal(t, "merged\n", loaded.Documents["docs/k.md"].Sections["s"].LastRenderedBody)
}

// TestExtraMigrationIdempotent proves repeated SQLite opens over one JSON
// fixture converge: double Load, then a SQLite-side Save, then Load again —
// all snapshots carry the same documents, and the JSON source is copied,
// never moved or deleted.
func TestExtraMigrationIdempotent(t *testing.T) {
	t.Setenv("GMB_DOC_STATE", "json")
	dir := t.TempDir()
	fixture := &DocEngineState{
		SchemaVersion: 1,
		LastCommit:    "idem1",
		Documents: map[string]*DocumentState{
			"docs/a.md": {
				FileHash: "sha256:a", FreshnessScore: 90, CommitsBehind: 1,
				Sections: map[string]*SectionState{
					"s1": {ASTSubgraphHash: "h1", RenderMode: "llm", LastRenderedBody: "A\n"},
				},
			},
			"docs/b.md": {
				FileHash: "sha256:b",
				Sections: map[string]*SectionState{
					"s2": {ASTSubgraphHash: "h2", RenderMode: "deterministic"},
				},
			},
		},
	}
	require.NoError(t, NewStateManager(dir).Save(fixture))

	snapshot := func(s *DocEngineState) map[string]string {
		out := map[string]string{"last_commit": s.LastCommit}
		for target, ds := range s.Documents {
			out[target+"/file"] = ds.FileHash
			for id, ss := range ds.Sections {
				out[target+"/"+id+"/hash"] = ss.ASTSubgraphHash
				out[target+"/"+id+"/mode"] = ss.RenderMode
				out[target+"/"+id+"/body"] = ss.LastRenderedBody
			}
		}
		return out
	}

	t.Setenv("GMB_DOC_STATE", "sqlite")
	sm := NewStateManager(dir)
	first, err := sm.Load()
	require.NoError(t, err)
	second, err := sm.Load()
	require.NoError(t, err)
	assert.Equal(t, snapshot(first), snapshot(second), "back-to-back Loads must converge")

	// A SQLite-side write keeps every migrated row and adds the new one.
	extra := GetOrCreateDocState(second, "docs/c.md")
	extra.FileHash = "sha256:c"
	require.NoError(t, sm.Save(second))
	third, err := sm.Load()
	require.NoError(t, err)
	for k, v := range snapshot(first) {
		if thirdSnap := snapshot(third); thirdSnap[k] != v {
			t.Errorf("migrated entry %q changed across save: %q → %q", k, v, thirdSnap[k])
		}
	}
	assert.Contains(t, third.Documents, "docs/c.md")

	// The JSON source survives migration untouched.
	assert.FileExists(t, filepath.Join(dir, "docs_state.json"))
	raw, err := os.ReadFile(filepath.Join(dir, "docs_state.json"))
	require.NoError(t, err)
	var legacy DocEngineState
	require.NoError(t, json.Unmarshal(raw, &legacy))
	assert.Equal(t, "idem1", legacy.LastCommit)
}

// TestExtraExportStateJSONShape pins the interchange shape beyond struct
// round-tripping: exact top-level keys, relative document keys only, and
// section rows carrying the hash contract fields.
func TestExtraExportStateJSONShape(t *testing.T) {
	extraBackends(t, func(t *testing.T) {
		dir := t.TempDir()
		sm := NewStateManager(dir)
		state, err := sm.Load()
		require.NoError(t, err)
		ds := GetOrCreateDocState(state, "docs/shape.md")
		ds.FileHash = "sha256:shape"
		ds.FreshnessScore = 64
		ss := GetOrCreateSectionState(ds, "intro")
		ss.ASTSubgraphHash = "sh1"
		ss.RenderMode = "deterministic"
		require.NoError(t, sm.Save(state))

		data, err := ExportStateJSON(sm)
		require.NoError(t, err)
		var shape map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(data, &shape))
		for _, key := range []string{"schema_version", "last_commit", "generated_at", "documents"} {
			assert.Contains(t, shape, key, "export must carry top-level key %q", key)
		}
		var docs map[string]map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(shape["documents"], &docs))
		require.Contains(t, docs, "docs/shape.md")
		for key := range docs {
			assert.False(t, filepath.IsAbs(key), "export must not leak absolute keys: %q", key)
		}
		var doc map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(mustMarshal(t, docs["docs/shape.md"]), &doc))
		assert.Contains(t, doc, "sections")
		assert.Contains(t, doc, "freshness_score")
	})
}

func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	require.NoError(t, err)
	return raw
}
