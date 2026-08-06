package visualization_engine

import (
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRenderersContract(t *testing.T) {
	tree := &types.LayoutTree{
		BoundaryName: "Root",
		Nodes: []*types.LayoutNode{
			{ID: "pkg/a.go::A", Name: "A", Kind: "STRUCT"},
		},
		Summary: &types.GraphSummary{NodeCount: 1},
	}

	renderers := []Renderer{
		&MermaidRenderer{},
		&PlantUMLRenderer{},
		&DOTRenderer{},
	}

	for _, r := range renderers {
		assert.NotEmpty(t, r.Format())
		assert.NotEmpty(t, r.Supported())

		out, err := r.Render(tree, types.UMLClass)
		require.NoError(t, err)
		assert.NotEmpty(t, out)
	}
}
