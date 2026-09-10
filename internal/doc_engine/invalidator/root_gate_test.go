package invalidator

import (
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/doc_engine/catalog"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/config"
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
