package stage4

import (
	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/stage3"
	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/types"
)

// RenderUMLDiagram renders one of the 14 UML diagram types (W4-02 / §8.1)
// to the requested markup format (mermaid, plantuml, dot).
func RenderUMLDiagram(tree *types.LayoutTree, t types.DiagramType, format string) string {
	return stage3.RenderDiagramFormat(tree, t, format)
}
