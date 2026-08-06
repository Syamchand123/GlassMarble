package stage4

import (
	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/stage3"
	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/types"
)

// RenderSpecializedDiagram renders one of the 10 specialized diagram types (W4-04 / §8.3)
// to the requested markup format (mermaid, plantuml, dot).
func RenderSpecializedDiagram(tree *types.LayoutTree, t types.DiagramType, format string) string {
	return stage3.RenderDiagramFormat(tree, t, format)
}
