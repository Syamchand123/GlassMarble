package views

import (
	"fmt"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/tui"
)

// HotspotRow is one ranked hotspot symbol.
type HotspotRow struct {
	Rank      int
	Name      string
	Kind      string
	InDegree  int
	OutDegree int
	Primitive string
}

// RenderHotspot renders the ranked hotspot table. The "Top N Architectural
// Hotspots" title and symbol names are preserved for tests.
func RenderHotspot(top int, rows []HotspotRow) string {
	lines := []string{
		tui.StyleH2.Render(fmt.Sprintf("  Top %d Architectural Hotspots (Ranked by In-Degree Centrality)", top)),
		"",
	}

	medals := []string{"🥇", "🥈", "🥉"}
	var body []string
	for _, r := range rows {
		rank := fmt.Sprintf("%d", r.Rank)
		if r.Rank-1 < len(medals) {
			rank = medals[r.Rank-1] + " " + rank
		}
		name := truncateLeft(r.Name, 46)
		inStyle := tui.StyleOK
		if r.InDegree > 30 {
			inStyle = tui.StyleError
		} else if r.InDegree >= 15 {
			inStyle = tui.StyleWarningText
		}
		kind := tui.StyleMuted.Render(pad("["+r.Kind+"]", 12))
		prim := primitiveBadge(r.Primitive)
		body = append(body, fmt.Sprintf("  %s  %-48s %s %10s %10s  %s",
			rank, name, kind, inStyle.Render(itoa(r.InDegree)), itoa(r.OutDegree), prim))
	}

	if len(body) > 0 {
		lines = append(lines, body...)
	} else {
		lines = append(lines, tui.StyleMuted.Render("  No hotspots found."))
	}

	return tui.StyleCard.Render("  " + joinLines(lines))
}

// primitiveBadge renders a colored primitive-type pill.
func primitiveBadge(p string) string {
	switch strings.ToUpper(p) {
	case "NETWORK_IO", "NET_IO":
		return tui.BadgeInfo.Render("  " + p + "  ")
	case "DATABASE", "DB":
		return tui.BadgeWarn.Render("  " + p + "  ")
	default:
		return tui.BadgeOK.Render("  " + p + "  ")
	}
}

// pad right-pads s to width.
func pad(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-len(s))
}
