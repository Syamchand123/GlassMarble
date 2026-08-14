package views

import (
	"fmt"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/link"
	"github.com/Syamchand123/GlassMarble/internal/tui"
)

// DiffEntry is one committed graph transaction rendered in the `gmb diff` view.
type DiffEntry struct {
	TxID          int
	CommitHash    string
	Timestamp     string
	Status        string
	NodesAdded    int
	EdgesAdded    int
	ModifiedFiles int
	HasPayload    bool
}

// RenderDiff renders the `gmb diff` transaction list. The "No pending
// transactions" line and "Current:" header are preserved verbatim for tests.
func RenderDiff(commitHash string, schemaVersion int, graphVersion uint64, entries []DiffEntry) string {
	rows := []string{
		tui.StyleH2.Render("  Architectural Graph Mutation Diff"),
		"  Current: " + shortHash(commitHash) + fmt.Sprintf(" (schema v%d, graph version %d)", schemaVersion, graphVersion),
		"",
	}

	if len(entries) == 0 {
		rows = append(rows,
			tui.BadgeInfo.Render("  INFO  ")+"  No pending transactions: every committed transaction is fully persisted in akg.json.",
			"              The current akg.json is the fully persisted latest state.",
		)
		return tui.StyleCard.Render("  " + joinLines(rows))
	}

	rows = append(rows, "  "+itoa(len(entries))+" recorded transaction(s):", "")
	for _, e := range entries {
		statusPill := tui.BadgeWarn.Render("  pending  ")
		if e.Status == "COMMITTED" || e.Status == "committed" {
			statusPill = tui.BadgeOK.Render("  committed  ")
		}
		rows = append(rows,
			fmt.Sprintf("  tx #%d  %s  %s", e.TxID, shortHash(e.CommitHash), e.Timestamp),
			"  status  "+statusPill,
		)
		if e.HasPayload {
			rows = append(rows, fmt.Sprintf("  delta   +%d nodes  +%d edges", e.NodesAdded, e.EdgesAdded))
		}
		if e.ModifiedFiles > 0 {
			rows = append(rows, fmt.Sprintf("  files   %d changed", e.ModifiedFiles))
		}
		rows = append(rows, "")
	}
	return tui.StyleCard.Render("  " + joinLines(rows))
}

// OutboundEdgeCount totals outbound edges (shared with diff/compare views).
func OutboundEdgeCount(out map[string][]link.ResolvedEdge) int {
	n := 0
	for _, edges := range out {
		n += len(edges)
	}
	return n
}
