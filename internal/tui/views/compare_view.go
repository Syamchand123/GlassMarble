package views

import (
	"fmt"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/tui"
	"github.com/charmbracelet/lipgloss"
)

// RenderCompare renders the `gmb compare` architecture diff as a two-column
// side-by-side view (§A.1): added nodes/edges on the left, removed on the
// right. The "AKG Architecture Diff", "Nodes added:", "Edges added:", and node
// symbol names are preserved for tests.
func RenderCompare(diff *akg.GraphDiff) string {
	rows := []string{
		tui.StyleH2.Render("  AKG Architecture Diff"),
		fmt.Sprintf("  Base: %s  →  Head: %s", shortHash(diff.BaseCommit), shortHash(diff.HeadCommit)),
		"",
		"  " + tui.Divider("Summary", 56),
		"  Nodes added:   " + itoa(len(diff.NodesAdded)) +
			tui.StyleMuted.Render("  (+ ") + tui.StyleOK.Render(itoa(len(diff.NodesAdded))) + tui.StyleMuted.Render(" / - ") + tui.StyleError.Render(itoa(len(diff.NodesRemoved))) + tui.StyleMuted.Render(")"),
		"  Edges added:   " + itoa(len(diff.EdgesAdded)) +
			tui.StyleMuted.Render("  (+ ") + tui.StyleOK.Render(itoa(len(diff.EdgesAdded))) + tui.StyleMuted.Render(" / - ") + tui.StyleError.Render(itoa(len(diff.EdgesRemoved))) + tui.StyleMuted.Render(")"),
		"  Files changed: " + itoa(len(diff.FilesChanged)),
	}

	empty := len(diff.NodesAdded) == 0 && len(diff.NodesRemoved) == 0 &&
		len(diff.EdgesAdded) == 0 && len(diff.EdgesRemoved) == 0
	if empty {
		rows = append(rows, "", tui.StyleOK.Render("  No architectural changes between the two snapshots."))
		return tui.StyleCard.Render("  " + joinLines(rows))
	}

	if len(diff.NodesAdded) > 0 || len(diff.NodesRemoved) > 0 {
		rows = append(rows, "", "  "+tui.Divider("Nodes", 56))
		left := renderNodeColumn("Added", diff.NodesAdded, "+", tui.StyleOK)
		right := renderNodeColumn("Removed", diff.NodesRemoved, "-", tui.StyleError)
		rows = append(rows, tui.Indent(tui.Columns(left, right, 34), 2))
	}
	if len(diff.EdgesAdded) > 0 || len(diff.EdgesRemoved) > 0 {
		rows = append(rows, "", "  "+tui.Divider("Edges", 56))
		left := renderEdgeColumn("Added", diff.EdgesAdded, "+", tui.StyleOK)
		right := renderEdgeColumn("Removed", diff.EdgesRemoved, "-", tui.StyleError)
		rows = append(rows, tui.Indent(tui.Columns(left, right, 34), 2))
	}
	return tui.StyleCard.Render("  " + joinLines(rows))
}

// renderNodeColumn renders one half of the two-column node diff.
func renderNodeColumn(title string, nodes []akg.DiffNode, marker string, style lipgloss.Style) string {
	var b strings.Builder
	b.WriteString(tui.StyleH2.Render(title+" Nodes") + "\n")
	if len(nodes) == 0 {
		b.WriteString(tui.StyleMuted.Render("  (none)") + "\n")
	}
	for _, n := range nodes {
		b.WriteString(style.Render(marker) + " " + truncateLeft(n.ID, 30) +
			tui.StyleMuted.Render(" ["+n.Kind+"]") + "\n")
	}
	return b.String()
}

// renderEdgeColumn renders one half of the two-column edge diff.
func renderEdgeColumn(title string, edges []akg.DiffEdge, marker string, style lipgloss.Style) string {
	var b strings.Builder
	b.WriteString(tui.StyleH2.Render(title+" Edges") + "\n")
	if len(edges) == 0 {
		b.WriteString(tui.StyleMuted.Render("  (none)") + "\n")
	}
	for _, e := range edges {
		b.WriteString(style.Render(marker) + " " + edgePill(e.Type) + " " +
			truncateLeft(e.SourceID, 14) + " " + tui.StyleAccent.Render("→") + " " +
			truncateLeft(e.TargetID, 14) + "\n")
	}
	return b.String()
}
