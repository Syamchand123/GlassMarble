package product

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildDiagram_Validation(t *testing.T) {
	_, _, err := BuildDiagram(DiagramRequest{})
	assert.Error(t, err)

	_, _, err = BuildDiagram(DiagramRequest{
		TTLPath: "dummy.ttl",
		Format:  "invalid-format",
	})
	assert.Error(t, err)
}

func TestBuildDiagram_Success(t *testing.T) {
	tmpDir := t.TempDir()
	ttlPath := filepath.Join(tmpDir, "akg_state.ttl")

	ttlContent := `@prefix gm: <http://glassmarble.org/schema#> .

<http://glassmarble.org/node/pkg/app.go::Main> a gm:Struct ;
    gm:name "Main" .
`
	require.NoError(t, os.WriteFile(ttlPath, []byte(ttlContent), 0644))

	req := DiagramRequest{
		TTLPath:       ttlPath,
		Type:          types.UMLClass,
		Format:        "mermaid",
		IncludeUnused: true,
	}

	markup, summary, err := BuildDiagram(req)
	require.NoError(t, err)
	assert.Contains(t, markup, "classDiagram")
	assert.Contains(t, markup, "Main")
	assert.NotNil(t, summary)
	assert.Equal(t, 1, summary.NodeCount)
}
