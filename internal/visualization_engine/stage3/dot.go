package stage3

import (
	"fmt"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/types"
)

// RenderDOTDiagram renders the layout tree as a Graphviz DOT digraph with styled nodes and edges.
func RenderDOTDiagram(tree *types.LayoutTree, t types.DiagramType) string {
	var sb strings.Builder
	sb.WriteString("digraph G {\n")
	sb.WriteString("    rankdir=TB;\n")
	sb.WriteString("    node [shape=box, style=rounded];\n")
	sb.WriteString(fmt.Sprintf("    label=\"%s Diagram\";\n", getDiagramTitle(tree, string(t))))

	for _, node := range collectAllNodes(tree) {
		id := sanitizeName(node.ID)
		name := sanitizeMermaidLabel(node.Name)
		if name == "" {
			name = sanitizeMermaidLabel(node.ID)
		}
		shape := "box"
		if node.Kind == "gm:Database" || strings.Contains(node.PrimitiveType, "DATABASE") {
			shape = "cylinder"
		} else if node.Kind == "gm:ExternalSystem" {
			shape = "component"
		} else if node.Kind == "gm:User" {
			shape = "ellipse"
		} else if node.Kind == "gm:Executable" || node.Kind == "gm:Function" || node.Kind == "gm:Method" {
			shape = "box3d"
		}
		sb.WriteString(fmt.Sprintf("    %s [label=\"%s\", shape=%s];\n", id, name, shape))
	}

	for _, edge := range tree.Edges {
		src := sanitizeName(edge.SourceID)
		tgt := sanitizeName(edge.TargetID)
		label := shortPredicate(edge.Predicate)

		style := "solid"
		color := "black"
		if edge.IsCycle {
			style = "bold"
			color = "red"
		} else if strings.Contains(edge.Predicate, "dataFlow") || strings.Contains(edge.Predicate, "pointsTo") {
			style = "dashed"
			color = "blue"
		} else if strings.Contains(edge.Predicate, "controlFlow") {
			style = "dotted"
			color = "green"
		}

		sb.WriteString(fmt.Sprintf("    %s -> %s [label=\"%s\", style=%s, color=%s];\n", src, tgt, label, style, color))
	}

	renderDOTSummaryFooter(tree, &sb)

	sb.WriteString("}\n")
	return sb.String()
}

