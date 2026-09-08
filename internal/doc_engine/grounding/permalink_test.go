package grounding

import (
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/link"
	"github.com/stretchr/testify/assert"
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
