package aggregate

import (
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/types"
)

// RenderDiagramFormat dispatches to the appropriate renderer based on the format string (plantuml, dot, html, or mermaid).
func RenderDiagramFormat(tree *types.LayoutTree, t types.DiagramType, format string) string {
	return RenderDiagramFormatOptions(tree, t, types.QueryOptions{Format: format, Theme: "modern", Direction: "auto"})
}

// RenderDiagramFormatOptions dispatches to the renderer honoring Theme, Direction, and Target format.
func RenderDiagramFormatOptions(tree *types.LayoutTree, t types.DiagramType, opts types.QueryOptions) string {
	format := opts.Format
	themeName := opts.Theme
	if themeName == "" {
		themeName = "modern"
	}
	direction := opts.Direction
	if direction == "" {
		direction = "auto"
	}

	if strings.EqualFold(format, "plantuml") {
		return RenderPlantUMLDiagramWithTheme(tree, t, themeName)
	}
	if strings.EqualFold(format, "dot") || strings.EqualFold(format, "graphviz") {
		return RenderDOTDiagramWithTheme(tree, t, themeName)
	}
	if strings.EqualFold(format, "html") {
		mermaidMarkup := RenderDiagramWithTheme(tree, t, themeName, direction)
		var summary *types.GraphSummary
		if tree != nil {
			summary = tree.Summary
		}
		return RenderHTMLStudio(mermaidMarkup, t, summary, themeName)
	}

	return RenderDiagramWithTheme(tree, t, themeName, direction)
}
