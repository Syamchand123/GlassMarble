package views

import (
	"fmt"

	doc_engine "github.com/Syamchand123/GlassMarble/internal/doc_engine"
	"github.com/Syamchand123/GlassMarble/internal/tui"
	"github.com/charmbracelet/lipgloss"
)

// DocStatusData carries the values needed to render the `gmb doc status` dashboard.
type DocStatusData struct {
	GlobalFreshness int
	AllFresh        bool
	Documents       []doc_engine.DocumentCheckResult
	Warnings        []string
	Failures        []string
}

// RenderDocStatus renders the interactive or static Lip Gloss status table for managed docs.
func RenderDocStatus(d DocStatusData) string {
	// Derived from the per-document Status column below, NOT from
	// len(d.Warnings)/len(d.Failures): those slices also carry advisory,
	// non-freshness content (diataxis compass suggestions, frontmatter/TOC
	// lint, reference-completeness hints) that doc_engine.Check appends
	// for documents that are otherwise 100% fresh. Keying the banner off
	// their raw counts made every managed doc's routine style advice flip
	// this headline to "DRIFTING" even while every row underneath read
	// "100% FRESH" — a direct, visible contradiction. Scanning Documents
	// keeps the banner consistent with what the table actually shows.
	badge := tui.BadgeOK.Render("  ● FRESH  ")
	worst := "fresh"
	for _, doc := range d.Documents {
		switch doc.Status {
		case "stale", "missing":
			worst = "stale"
		case "warn":
			if worst == "fresh" {
				worst = "warn"
			}
		}
	}
	switch worst {
	case "stale":
		badge = tui.BadgeError.Render("  ✗ DRIFTED  ")
	case "warn":
		badge = tui.BadgeWarn.Render("  ⚠ DRIFTING  ")
	}

	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7D56F4"))
	colStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#A0A0A0"))

	var rows []string
	rows = append(rows, headerStyle.Render("  GlassMarble Documentation Intelligence Dashboard")+"   "+badge)
	rows = append(rows, "")
	rows = append(rows, fmt.Sprintf("  Global Freshness: %d%% across %d managed document(s)", d.GlobalFreshness, len(d.Documents)))
	rows = append(rows, "")

	// Table header
	headerRow := fmt.Sprintf("  %-35s %-20s %-12s %-12s",
		colStyle.Render("DOCUMENT"),
		colStyle.Render("ID"),
		colStyle.Render("FRESHNESS"),
		colStyle.Render("STATUS"),
	)
	rows = append(rows, headerRow)
	rows = append(rows, "  "+lipgloss.NewStyle().Foreground(lipgloss.Color("#444444")).Render("─────────────────────────────────────────────────────────────────────────────"))

	for _, doc := range d.Documents {
		var statusBadge string
		freshStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#04B575"))

		switch doc.Status {
		case "fresh":
			statusBadge = tui.BadgeOK.Render(" FRESH ")
		case "warn":
			statusBadge = tui.BadgeWarn.Render(" WARN ")
			freshStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#E5C07B"))
		case "missing":
			statusBadge = tui.BadgeError.Render(" MISSING ")
			freshStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#E06C75"))
		default:
			statusBadge = tui.BadgeError.Render(" STALE ")
			freshStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#E06C75"))
		}

		freshnessStr := freshStyle.Render(fmt.Sprintf("%3d%%", doc.Freshness))

		row := fmt.Sprintf("  %-35s %-20s %-12s %s",
			doc.TargetPath,
			doc.ID,
			freshnessStr,
			statusBadge,
		)
		rows = append(rows, row)
	}

	rows = append(rows, "")
	if len(d.Failures) > 0 {
		for _, f := range d.Failures {
			rows = append(rows, "  "+tui.BadgeError.Render(" FAIL ")+" "+f)
		}
	}
	if len(d.Warnings) > 0 {
		for _, w := range d.Warnings {
			rows = append(rows, "  "+tui.BadgeWarn.Render(" WARN ")+" "+w)
		}
	}

	return tui.StyleCard.Render(joinLines(rows))
}
