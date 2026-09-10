package grounding

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/link"
	docconfig "github.com/Syamchand123/GlassMarble/internal/doc_engine/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormatPermalink(t *testing.T) {
	assert.Equal(t, "main.go#L10", FormatPermalink("main.go", 10, 10))
	assert.Equal(t, "internal/auth/jwt.go#L42-L89", FormatPermalink("internal/auth/jwt.go", 42, 89))
	assert.Equal(t, "README.md", FormatPermalink("README.md", 0, 0))
}

func TestResolvePermalink(t *testing.T) {
	graph := akg.NewCodePropertyGraph("commit-1")
	graph.Nodes = graph.Nodes.Set("internal/auth/jwt.go::ValidateToken", &link.ResolvedNode{
		ID:   "internal/auth/jwt.go::ValidateToken",
		Name: "ValidateToken",
		FileSpec: link.LocationMeta{
			Path:      "internal/auth/jwt.go",
			LineStart: 45,
			LineEnd:   72,
		},
	})

	// Exact FQN
	file, start, end, ok := ResolvePermalink("internal/auth/jwt.go::ValidateToken", graph)
	assert.True(t, ok)
	assert.Equal(t, "internal/auth/jwt.go", file)
	assert.Equal(t, 45, start)
	assert.Equal(t, 72, end)

	// Suffix/Name lookup
	file2, start2, end2, ok2 := ResolvePermalink("ValidateToken", graph)
	assert.True(t, ok2)
	assert.Equal(t, "internal/auth/jwt.go", file2)
	assert.Equal(t, 45, start2)
	assert.Equal(t, 72, end2)

	// Missing
	_, _, _, ok3 := ResolvePermalink("NonExistent", graph)
	assert.False(t, ok3)
}

func TestUpdatePermalinksInMarkdown(t *testing.T) {
	graph := akg.NewCodePropertyGraph("commit-2")
	// Symbol shifted from L20-L30 to L50-L65 in the latest commit
	graph.Nodes = graph.Nodes.Set("internal/auth/jwt.go::ValidateToken", &link.ResolvedNode{
		ID:   "internal/auth/jwt.go::ValidateToken",
		Name: "ValidateToken",
		FileSpec: link.LocationMeta{
			Path:      "internal/auth/jwt.go",
			LineStart: 50,
			LineEnd:   65,
		},
	})

	markdown := "See [ValidateToken](internal/auth/jwt.go#L20-L30) for details."
	updated := UpdatePermalinksInMarkdown(markdown, graph)

	expected := "See [ValidateToken](internal/auth/jwt.go#L50-L65) for details."
	assert.Equal(t, expected, updated, "permalink line numbers must self-heal to match current AST")
}

func TestHealAllManagedDocs(t *testing.T) {
	root := t.TempDir()
	// Go source with an exported symbol at a known line.
	src := "package auth\n\n// ValidateToken validates.\nfunc ValidateToken() {}\n"
	require.NoError(t, os.MkdirAll(filepath.Join(root, "internal", "auth"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "internal", "auth", "jwt.go"), []byte(src), 0644))

	// Managed doc with a stale permalink (wrong lines).
	md := "<!-- gmb:begin:api -->\nSee [ValidateToken](internal/auth/jwt.go#L99-L199).\n<!-- gmb:end:api -->\n"
	require.NoError(t, os.MkdirAll(filepath.Join(root, "docs"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "docs", "auth.md"), []byte(md), 0644))

	docs := []docconfig.DocSpec{
		{
			ID:         "auth",
			TargetPath: "docs/auth.md",
			Sections: []docconfig.SectionSpec{
				{ID: "api", Title: "API", Managed: true},
			},
		},
	}

	healed, err := HealAllManagedDocs(root, docs)
	require.NoError(t, err)
	assert.Equal(t, 1, healed)

	after, err := os.ReadFile(filepath.Join(root, "docs", "auth.md"))
	require.NoError(t, err)
	assert.Contains(t, string(after), "internal/auth/jwt.go#L4")

	// Second run: already healed → no changes, zero count.
	healedAgain, err := HealAllManagedDocs(root, docs)
	require.NoError(t, err)
	assert.Equal(t, 0, healedAgain)
}
