package invalidator

import (
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/doc_engine/catalog"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/config"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func rootGateCatalog() *catalog.Catalog {
	docs := []config.DocSpec{
		{
			ID:         "root",
			TargetPath: "README.md",
			Scope:      config.ScopeRule{Paths: []string{"cmd/**"}},
			Sections: []config.SectionSpec{
				{ID: "overview", Title: "Overview", Managed: true},
			},
		},
	}
	return catalog.New(docs)
}

func TestFindDirtySections_RootGateSkipsPrivateChange(t *testing.T) {
	inv := New(rootGateCatalog())
	dossier := &config.GlobalCommitDossier{
		CommitIntent: "FIX_BUG",
		AddedSymbols: []config.SymbolFact{
			{FQN: "cmd/app::runHelper", File: "cmd/app/main.go"},
		},
	}
	dirty, err := inv.FindDirtySections(dossier, nil, nil, nil)
	require.NoError(t, err)
	assert.Empty(t, dirty, "root docs must not invalidate on private changes")
}

func TestFindDirtySections_RootGateAllowsPublicChange(t *testing.T) {
	inv := New(rootGateCatalog())
	dossier := &config.GlobalCommitDossier{
		CommitIntent: "ADD_FEATURE",
		AddedSymbols: []config.SymbolFact{
			{FQN: "cmd/app::RunServer", File: "cmd/app/main.go"},
		},
	}
	dirty, err := inv.FindDirtySections(dossier, nil, nil, nil)
	require.NoError(t, err)
	require.Len(t, dirty, 1)
	assert.Equal(t, "root", dirty[0].DocID)
	// Reason carries intent + change-kind context.
	assert.Contains(t, dirty[0].Reason, "intent=ADD_FEATURE")
}

func TestFindDirtySections_RootGateAllowsArchEvents(t *testing.T) {
	inv := New(rootGateCatalog())
	dossier := &config.GlobalCommitDossier{
		CommitIntent: "REFACTOR",
		ArchEvents:   []string{"SERVICE_ADDED"},
		AddedSymbols: []config.SymbolFact{{FQN: "cmd/app::runHelper", File: "cmd/app/main.go"}},
	}
	dirty, err := inv.FindDirtySections(dossier, nil, nil, nil)
	require.NoError(t, err)
	require.Len(t, dirty, 1)
}

// TestFindDirtySections_NeverRenderedRootBypassesGate guards a real bug
// found via live testing: a freshly scaffolded root/aggregate document
// (README.md, docs/architecture.md) has no prior content and no
// SectionState in docs_state.json yet — but the cascade gate above ran
// unconditionally for that doc level regardless, so a section with
// nothing in it never got its first real content unless the commit that
// happened to trigger evaluation also carried a "critical" architectural
// event or public-surface change. In practice this meant the
// architecture archetype only ever rendered under --force. A real
// (non-nil) empty DocEngineState — exactly what StateManager.Load()
// returns for a repo that has never run before — must let a
// never-rendered section through even on an otherwise-private change.
func TestFindDirtySections_NeverRenderedRootBypassesGate(t *testing.T) {
	inv := New(rootGateCatalog())
	dossier := &config.GlobalCommitDossier{
		CommitIntent: "FIX_BUG",
		AddedSymbols: []config.SymbolFact{
			{FQN: "cmd/app::runHelper", File: "cmd/app/main.go"},
		},
	}
	freshState := &storage.DocEngineState{Documents: map[string]*storage.DocumentState{}}
	dirty, err := inv.FindDirtySections(dossier, freshState, nil, nil)
	require.NoError(t, err)
	require.Len(t, dirty, 1, "a never-rendered root section must populate even on a private change")
	assert.Equal(t, "root", dirty[0].DocID)
}

// TestFindDirtySections_AlreadyRenderedRootStillGated is the companion
// case: once a section has real persisted state (it has been rendered at
// least once), the cascade gate must apply exactly as before — a private
// change must NOT re-trigger it.
func TestFindDirtySections_AlreadyRenderedRootStillGated(t *testing.T) {
	inv := New(rootGateCatalog())
	dossier := &config.GlobalCommitDossier{
		CommitIntent: "FIX_BUG",
		AddedSymbols: []config.SymbolFact{
			{FQN: "cmd/app::runHelper", File: "cmd/app/main.go"},
		},
	}
	renderedState := &storage.DocEngineState{Documents: map[string]*storage.DocumentState{
		"README.md": {Sections: map[string]*storage.SectionState{
			"overview": {ASTSubgraphHash: "already-rendered-once"},
		}},
	}}
	dirty, err := inv.FindDirtySections(dossier, renderedState, nil, nil)
	require.NoError(t, err)
	assert.Empty(t, dirty, "an already-rendered root section must still be gated on a private change")
}

func TestIsPublicSurfaceChange(t *testing.T) {
	assert.False(t, isPublicSurfaceChange(nil))
	assert.False(t, isPublicSurfaceChange(&config.GlobalCommitDossier{}))
	assert.True(t, isPublicSurfaceChange(&config.GlobalCommitDossier{ArchEvents: []string{"X"}}))
	assert.True(t, isPublicSurfaceChange(&config.GlobalCommitDossier{
		RemovedSymbols: []string{"cmd/app::OldCommand"},
	}))
	assert.False(t, isPublicSurfaceChange(&config.GlobalCommitDossier{
		RemovedSymbols: []string{"cmd/app::oldHelper"},
	}))
}
