package stage3

import (
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/types"
)

// RenderDiagramFormat dispatches to the appropriate renderer based on the format string (plantuml, dot, or mermaid).
func RenderDiagramFormat(tree *types.LayoutTree, t types.DiagramType, format string) string {
	if strings.EqualFold(format, "plantuml") {
		return RenderPlantUMLDiagram(tree, t)
	}
	if strings.EqualFold(format, "dot") || strings.EqualFold(format, "graphviz") {
		return RenderDOTDiagram(tree, t)
	}
	return RenderDiagram(tree, t)
}
