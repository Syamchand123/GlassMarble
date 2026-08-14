package link

import (
	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/render"
	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/types"
)

// RenderC4Diagram renders one of the 7 C4 model diagram types (W4-03 / §8.2)
// to the requested markup format (mermaid, plantuml, dot).
func RenderC4Diagram(tree *types.LayoutTree, t types.DiagramType, format string) string {
	return aggregate.RenderDiagramFormat(tree, t, format)
}
