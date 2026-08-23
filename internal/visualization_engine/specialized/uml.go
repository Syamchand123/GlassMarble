package specialized

import (
	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/render"
	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/types"
)

// RenderUMLDiagram renders one of the 14 UML diagram types (W4-02 / §8.1)
// to the requested markup format (mermaid, plantuml, dot).
func RenderUMLDiagram(tree *types.LayoutTree, t types.DiagramType, format string) string {
	return render.RenderDiagramFormat(tree, t, format)
}
