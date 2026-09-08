package invalidator

import (
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/doc_engine/catalog"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/config"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFastBail_EmptyCatalog(t *testing.T) {
	cat := catalog.New(nil)
	bail, _, reason, err := FastBail("", "c1", cat, nil, false)
	require.NoError(t, err)
	assert.True(t, bail)
	assert.Contains(t, reason, "no documents configured")
}

func TestFastBail_AlreadyProcessed(t *testing.T) {
	docs := []config.DocSpec{{ID: "d1", TargetPath: "docs/d1.md", Scope: config.ScopeRule{Paths: []string{"internal/**"}}}}
	cat := catalog.New(docs)
	state := &storage.DocEngineState{
		LastCommit: "commit-123",
	}

	// Not forced -> should bail
	bail, _, reason, err := FastBail("", "commit-123", cat, state, false)
	require.NoError(t, err)
	assert.True(t, bail)
	assert.Contains(t, reason, "already processed")

	// Forced -> should not bail on already-processed
	bailForce, _, _, errForce := FastBail("", "commit-123", cat, state, true)
	require.NoError(t, errForce)
	assert.False(t, bailForce)
}

func TestFindDirtySections_HashMatchingSkips(t *testing.T) {
	docs := []config.DocSpec{
		{
			ID:         "auth-doc",
			TargetPath: "docs/auth.md",
			Scope:      config.ScopeRule{Paths: []string{"internal/auth/**"}},
			Sections: []config.SectionSpec{
				{
					ID:          "interface",
					Title:       "Interface",
					Instruction: "Table of types",
					Managed:     true,
				},
			},
		},
	}
	cat := catalog.New(docs)
	inv := New(cat)

	// Compute expected current hash for no symbols
	expectedHash := HashSection(&docs[0].Sections[0], nil, nil)

	// Persist state with this exact hash
	state := &storage.DocEngineState{
		Documents: map[string]*storage.DocumentState{
			"docs/auth.md": {
				Sections: map[string]*storage.SectionState{
					"interface": {
						ASTSubgraphHash: expectedHash,
					},
				},
			},
		},
	}

	dossier := &config.GlobalCommitDossier{
		AddedSymbols: []config.SymbolFact{
			{FQN: "internal/auth.Login", File: "internal/auth/login.go"},
		},
	}

	// 1. When state hash matches current hash, section is clean -> 0 dirty sections!
	dirty, err := inv.FindDirtySections(dossier, state, nil, nil)
	require.NoError(t, err)
	assert.Empty(t, dirty, "unchanged hash should produce zero dirty sections (0 tokens)")

	// 2. When state hash does NOT match, section is dirty!
	state.Documents["docs/auth.md"].Sections["interface"].ASTSubgraphHash = "different-hash"
	dirtyModified, err := inv.FindDirtySections(dossier, state, nil, nil)
	require.NoError(t, err)
	require.Len(t, dirtyModified, 1)
	assert.Equal(t, "auth-doc", dirtyModified[0].DocID)
	assert.Equal(t, "interface", dirtyModified[0].SectionID)
}

func TestFindDirtySections_BudgetEnforced(t *testing.T) {
	docs := []config.DocSpec{
		{
			ID:         "doc1",
			TargetPath: "docs/doc1.md",
			Scope:      config.ScopeRule{Paths: []string{"internal/**"}},
			Sections: []config.SectionSpec{
				{ID: "sec1", Title: "Sec 1", Managed: true},
				{ID: "sec2", Title: "Sec 2", Managed: true},
				{ID: "sec3", Title: "Sec 3", Managed: true},
				{ID: "sec4", Title: "Sec 4", Managed: true},
				{ID: "sec5", Title: "Sec 5", Managed: true},
			},
		},
	}
	cat := catalog.New(docs)
	inv := New(cat)

	constraints := &config.GlobalConstraints{
		MaxDocUpdatesPerCommit: 2,
	}

	dirty, err := inv.FindDirtySections(nil, nil, nil, constraints)
	require.NoError(t, err)
	assert.Len(t, dirty, 2, "should enforce budget of 2 updates per commit")
}
