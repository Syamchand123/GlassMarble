package views

import (
	"fmt"

	"github.com/Syamchand123/GlassMarble/internal/tui"
)

// DependencyEdge is one resolved dependency edge.
type DependencyEdge struct {
	Type       string
	OtherID    string
	LineNumber int
}

// RenderDependencySummary renders the repository-wide dependency summary.
func RenderDependencySummary(totalNodes, outMappings, inMappings int, topNodes []TopDependencyNode) string {
	rows := []string{
		tui.StyleH2.Render("  Repository Dependency Summary"),
		"",
		tui.KV("Total Graph Nodes", itoa(totalNodes)),
		tui.KV("Total Outbound Edge Mappings", itoa(outMappings)),
		tui.KV("Total Inbound Edge Mappings", itoa(inMappings)),
		"",
		tui.StyleLabel.Render("Top Dependency Nodes:"),
	}
	for _, n := range topNodes {
		rows = append(rows, tui.Indent(tui.StyleAccent.Render(n.ID)+tui.StyleMuted.Render(fmt.Sprintf(" (%d outbound dependencies)", n.Outbound)), 2))
	}
	return tui.StyleCard.Render("  " + joinLines(rows))
}

// TopDependencyNode is a ranked dependency hub.
type TopDependencyNode struct {
	ID       string
	Outbound int
}

// RenderDependencyTarget renders the two-panel outbound/inbound dependency
// report for a matched target node.
func RenderDependencyTarget(target string, outbound, inbound []DependencyEdge) string {
	rows := []string{
		tui.StyleH2.Render(fmt.Sprintf("  Dependency Analysis: '%s'", target)),
		"",
		"  Node: " + tui.StyleCode.Render(target),
	}

	rows = append(rows, "", "  "+tui.Divider(fmt.Sprintf("Outbound Dependencies (%d)", len(outbound)), maxCardLine()))
	if len(outbound) == 0 {
		rows = append(rows, "  Direct Outbound Dependencies: None")
	} else {
		for _, e := range outbound {
			rows = append(rows, fmt.Sprintf("  %s %s  %s",
				edgeArrow("out", e.Type), edgePill(e.Type), edgeTarget(e.OtherID, e.LineNumber)))
		}
	}

	rows = append(rows, "", "  "+tui.Divider(fmt.Sprintf("Inbound Callers (%d)", len(inbound)), maxCardLine()))
	if len(inbound) == 0 {
		rows = append(rows, "  Direct Inbound Callers/Dependents: None")
	} else {
		rows = append(rows, "  Direct Inbound Callers/Dependents:")
		for _, e := range inbound {
			rows = append(rows, fmt.Sprintf("  %s %s  %s",
				edgeArrow("in", e.Type), edgePill(e.Type), edgeTarget(e.OtherID, e.LineNumber)))
		}
	}

	return tui.StyleCard.Render("  " + joinLines(rows))
}

// edgeArrow returns the direction glyph: cyan "→" for outbound, blue "←" for inbound.
func edgeArrow(dir, edgeType string) string {
	if dir == "out" {
		return tui.StyleAccent.Render("→")
	}
	return tui.StyleInfoText.Render("←")
}

// edgePill colors an edge type tag.
func edgePill(edgeType string) string {
	switch edgeType {
	case "calls", "call":
		return tui.StyleAccent.Render("[" + edgeType + "]")
	case "imports", "import":
		return tui.StyleInfoText.Render("[" + edgeType + "]")
	case "implements", "impl":
		return tui.StylePrimaryText.Render("[" + edgeType + "]")
	default:
		return tui.StyleMuted.Render("[" + edgeType + "]")
	}
}

// edgeTarget renders the target id + line number.
func edgeTarget(id string, line int) string {
	return tui.StyleTextSecondary.Render(truncateLeft(id, 60)) + tui.StyleMuted.Render(fmt.Sprintf("  L%d", line))
}
