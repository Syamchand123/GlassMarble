package catalog

import (
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/doc_engine/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCatalog_ScopeMatching(t *testing.T) {
	docs := []config.DocSpec{
		{
			ID:         "auth-module",
			TargetPath: "docs/auth.md",
			Scope: config.ScopeRule{
				Paths:        []string{"internal/auth/**"},
				ExcludePaths: []string{"internal/auth/testutil/**"},
			},
		},
		{
			ID:         "cmd-root",
			TargetPath: "docs/cli.md",
			Scope: config.ScopeRule{
				Paths: []string{"cmd/*.go"},
			},
		},
		{
			ID:         "system-arch",
			TargetPath: "docs/architecture.md",
			Scope: config.ScopeRule{
				Paths: []string{"internal/**", "cmd/**"},
			},
		},
	}

	cat := New(docs)
	require.Equal(t, 3, len(cat.Docs()))

	// Test direct scope match
	assert.True(t, cat.MatchesPath("auth-module", "internal/auth/jwt.go"))
	assert.True(t, cat.MatchesPath("auth-module", "internal/auth/sub/handler.go"))

	// Test excluded path
	assert.False(t, cat.MatchesPath("auth-module", "internal/auth/testutil/mock.go"))

	// Test un-related path
	assert.False(t, cat.MatchesPath("auth-module", "internal/db/db.go"))

	// Test cmd glob
	assert.True(t, cat.MatchesPath("cmd-root", "cmd/root.go"))
	assert.False(t, cat.MatchesPath("cmd-root", "cmd/sub/helper.go"))

	// Test MatchingDocs
	matching := cat.MatchingDocs("internal/auth/jwt.go")
	require.Len(t, matching, 2) // auth-module and system-arch
	docIDs := []string{matching[0].ID, matching[1].ID}
	assert.Contains(t, docIDs, "auth-module")
	assert.Contains(t, docIDs, "system-arch")
}

func TestCatalog_GetAndGetByTarget(t *testing.T) {
	docs := []config.DocSpec{
		{ID: "doc-1", TargetPath: "docs/one.md"},
		{ID: "doc-2", TargetPath: "docs/two.md"},
	}
	cat := New(docs)

	assert.NotNil(t, cat.Get("doc-1"))
	assert.Equal(t, "docs/one.md", cat.Get("doc-1").TargetPath)
	assert.Nil(t, cat.Get("nonexistent"))

	assert.NotNil(t, cat.GetByTarget("docs/two.md"))
	assert.Equal(t, "doc-2", cat.GetByTarget("docs/two.md").ID)
	// Case-insensitive / path separator normalization
	assert.NotNil(t, cat.GetByTarget("docs\\two.md"))
}

func TestReverseIndex(t *testing.T) {
	docs := []config.DocSpec{
		{
			ID:         "auth",
			TargetPath: "docs/auth.md",
			Scope: config.ScopeRule{
				Paths:       []string{"internal/auth/**"},
				EntryPoints: []string{"internal/auth/jwt.go::Validate"},
			},
			Sections: []config.SectionSpec{
				{ID: "interface", Title: "Interface", Managed: true},
				{ID: "security", Title: "Security", Managed: true},
				{ID: "notes", Title: "Notes", Managed: false}, // Human-only
			},
		},
	}
	cat := New(docs)
	idx := NewReverseIndex(cat)

	// Entry points should be indexed
	dirty := idx.FindDirtySectionsForSymbol("internal/auth/jwt.go::Validate")
	require.NotEmpty(t, dirty)
	// Both managed sections should appear, unmanaged should not
	sectionIDs := []string{}
	for _, d := range dirty {
		sectionIDs = append(sectionIDs, d.SectionID)
	}
	assert.Contains(t, sectionIDs, "interface")
	assert.Contains(t, sectionIDs, "security")
	assert.NotContains(t, sectionIDs, "notes")

	// Find by changed file
	fileDirty := idx.FindDirtySectionsForFiles([]string{"internal/auth/token.go"})
	require.Len(t, fileDirty, 2)
}
