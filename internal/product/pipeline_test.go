package product

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBuildDiagramParity verifies CLI/TUI/AI parity when invoking product.BuildDiagram (W7-01 / §11.1).
func TestBuildDiagramParity(t *testing.T) {
	tmpDir := t.TempDir()
	StatePath := filepath.Join(tmpDir, "akg_state.ttl")
	ttlContent := `@prefix gm: <https://glassmarble.dev/schema/> .
<file:pkg/a.go::Service> a gm:TypeDecl ;
    gm:name "Service" ;
    gm:kind "STRUCT" ;
    gm:filePath "pkg/a.go" ;
    gm:lineStart 1 .
`
	err := os.WriteFile(StatePath, []byte(ttlContent), 0644)
	require.NoError(t, err)

	req := DiagramRequest{
		StatePath:       StatePath,
		Type:          types.UMLClass,
		Scope:         types.ScopeGlobal,
		Format:        "mermaid",
		IncludeUnused: true,
	}

	res, err := BuildDiagramEx(req)
	require.NoError(t, err)
	assert.NotNil(t, res)

	markup, summary, err := BuildDiagram(req)
	require.NoError(t, err)
	assert.Equal(t, res.Markup, markup)
	assert.Equal(t, res.Summary, summary)
}
