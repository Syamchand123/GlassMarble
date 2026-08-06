package stage4

import (
	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/stage3"
	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/types"
)

// RenderC4Diagram renders one of the 7 C4 model diagram types (W4-03 / §8.2)
// to the requested markup format (mermaid, plantuml, dot).
func RenderC4Diagram(tree *types.LayoutTree, t types.DiagramType, format string) string {
	return stage3.RenderDiagramFormat(tree, t, format)
}
