package link

import (
	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/render"
	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/types"
)

// RenderSpecializedDiagram renders one of the 10 specialized diagram types (W4-04 / §8.3)
// to the requested markup format (mermaid, plantuml, dot).
func RenderSpecializedDiagram(tree *types.LayoutTree, t types.DiagramType, format string) string {
	return aggregate.RenderDiagramFormat(tree, t, format)
}
