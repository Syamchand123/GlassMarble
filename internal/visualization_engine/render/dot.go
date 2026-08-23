package render

import (
	"fmt"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/types"
)

// RenderDOTDiagram renders the layout tree as a Graphviz DOT digraph with default theme.
func RenderDOTDiagram(tree *types.LayoutTree, t types.DiagramType) string {
	return RenderDOTDiagramWithTheme(tree, t, "modern")
}

// RenderDOTDiagramWithTheme renders the layout tree as a Graphviz DOT digraph with custom theme attributes.
func RenderDOTDiagramWithTheme(tree *types.LayoutTree, t types.DiagramType, themeName string) string {
	theme := GetTheme(themeName)
	var sb strings.Builder
	sb.WriteString("digraph G {\n")
	sb.WriteString("    rankdir=TB;\n")
	sb.WriteString(theme.EmitDOTGraphAttrs())
	sb.WriteString(fmt.Sprintf("    label=\"%s Diagram\";\n", getDiagramTitle(tree, string(t))))

	for _, node := range collectAllNodes(tree) {
		id := sanitizeName(node.ID)
		name := sanitizeMermaidLabel(node.Name)
		if name == "" {
			name = sanitizeMermaidLabel(node.ID)
		}

		arch := ClassifyNodeArchetype(node)
		shape := "box"
		switch arch {
		case ArchDataStore:
			shape = "cylinder"
		case ArchExternalAPI:
			shape = "component"
		case ArchEntrypoint:
			shape = "oval"
		case ArchEventBus:
			shape = "hexagon"
		case ArchGateway:
			shape = "diamond"
		case ArchParser, ArchRenderer:
			shape = "trapezium"
		default:
			shape = "box"
		}

		style := theme.ArchetypeStyles[arch.ClassName()]
		sb.WriteString(fmt.Sprintf("    %s [label=\"%s\\n%s\", shape=%s, fillcolor=\"%s\", color=\"%s\", fontcolor=\"%s\"];\n",
			id, arch.Stereotype(), name, shape, style.Fill, style.Stroke, style.TextColor))
	}

	for _, edge := range tree.Edges {
		src := sanitizeName(edge.SourceID)
		tgt := sanitizeName(edge.TargetID)
		label := shortPredicate(edge.Predicate)

		style := "solid"
		color := theme.EdgeColor
		if edge.IsCycle {
			style = "bold"
			color = theme.CycleEdge
		} else if strings.Contains(edge.Predicate, "dataFlow") || strings.Contains(edge.Predicate, "pointsTo") {
			style = "bold"
			color = theme.DataFlowEdge
		} else if strings.Contains(edge.Predicate, "controlFlow") || strings.Contains(edge.Predicate, "async") {
			style = "dashed"
			color = theme.AsyncEdge
		}

		sb.WriteString(fmt.Sprintf("    %s -> %s [label=\"%s\", style=%s, color=\"%s\"];\n", src, tgt, label, style, color))
	}

	renderDOTSummaryFooter(tree, &sb)

	sb.WriteString("}\n")
	return sb.String()
}
