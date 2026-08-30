package views

import (
	"fmt"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/impact_analyzer"
	"github.com/Syamchand123/GlassMarble/internal/tui"
)

// RenderImpactReport formats the blast-radius analysis report using Lip Gloss.
func RenderImpactReport(rep *impact_analyzer.ImpactReport) string {
	if rep == nil {
		return ""
	}

	var riskBadge string
	switch rep.RiskLevel {
	case "CRITICAL":
		riskBadge = tui.BadgeError.Render("  CRITICAL RISK  ")
	case "HIGH":
		riskBadge = tui.BadgeError.Render("  HIGH RISK  ")
	case "MEDIUM":
		riskBadge = tui.BadgeWarn.Render("  MEDIUM RISK  ")
	default:
		riskBadge = tui.BadgeOK.Render("  LOW RISK  ")
	}

	meter := renderRiskMeter(rep.RiskScore)

	header := tui.StyleH2.Render("  Refactoring Blast-Radius & Impact Analysis")

	summaryRows := []string{
		"  Target Symbol:     " + tui.StyleAccent.Render(rep.TargetName) + "  " + tui.StyleMuted.Render("("+rep.TargetKind+")"),
		"  Declared In:       " + tui.StyleCode.Render(rep.TargetFile),
		"  Risk Assessment:   " + meter + " " + riskBadge,
		fmt.Sprintf("  Total Impact:      %d nodes across %d files (Depth: %d)", rep.TotalImpactedNodes, rep.TotalImpactedFiles, rep.MaxDepthReached),
		fmt.Sprintf("  Direct Callers:    %d", rep.DirectDependentsCount),
		fmt.Sprintf("  Transitive:        %d", rep.TransitiveDependentsCount),
	}

	summaryCard := tui.StyleCard.Render("  " + header + "\n\n" + joinLines(summaryRows))

	var sections []string
	sections = append(sections, summaryCard)

	// 1. Direct Dependents List
	if len(rep.DirectDependents) > 0 {
		var dRows []string
		dRows = append(dRows, tui.StyleTextSecondary.Render(fmt.Sprintf("  Direct Dependents (%d):", len(rep.DirectDependents))))
		limit := 10
		if len(rep.DirectDependents) < limit {
			limit = len(rep.DirectDependents)
		}
		for i := 0; i < limit; i++ {
			d := rep.DirectDependents[i]
			tag := "⚡"
			if d.IsTest {
				tag = "🧪"
			} else if d.IsEntry {
				tag = "🚀"
			}
			loc := d.File
			if d.Line > 0 {
				loc = fmt.Sprintf("%s:%d", loc, d.Line)
			}
			dRows = append(dRows, fmt.Sprintf("    %s %s (%s)  %s", tag, tui.StyleCode.Render(d.Name), d.Kind, tui.StyleMuted.Render(loc)))
		}
		if len(rep.DirectDependents) > limit {
			dRows = append(dRows, tui.StyleMuted.Render(fmt.Sprintf("    ... and %d more direct dependents", len(rep.DirectDependents)-limit)))
		}
		sections = append(sections, tui.StyleCard.Render("  "+joinLines(dRows)))
	}

	// 2. Impacted Entrypoints & Test Suites
	if len(rep.ImpactedEntrypoints) > 0 || len(rep.ImpactedTestFiles) > 0 {
		var epRows []string
		if len(rep.ImpactedEntrypoints) > 0 {
			epRows = append(epRows, tui.StyleTextSecondary.Render(fmt.Sprintf("  Exposed Entrypoints (%d):", len(rep.ImpactedEntrypoints))))
			for _, ep := range rep.ImpactedEntrypoints {
				epRows = append(epRows, "    🚀 "+tui.StyleAccent.Render(ep))
			}
			epRows = append(epRows, "")
		}
		if len(rep.ImpactedTestFiles) > 0 {
			epRows = append(epRows, tui.StyleTextSecondary.Render(fmt.Sprintf("  Impacted Test Suites (%d):", len(rep.ImpactedTestFiles))))
			for _, tf := range rep.ImpactedTestFiles {
				epRows = append(epRows, "    🧪 "+tui.StyleCode.Render(tf))
			}
		}
		sections = append(sections, tui.StyleCard.Render("  "+joinLines(epRows)))
	}

	// 3. Recommended Test Command
	if rep.RecommendedTestCommand != "" {
		cmdRows := []string{
			tui.StyleTextSecondary.Render("  Recommended Regression Test Command:"),
			"  " + tui.StyleOK.Render(rep.RecommendedTestCommand),
		}
		sections = append(sections, tui.StyleCard.Render("  "+joinLines(cmdRows)))
	}

	return strings.Join(sections, "\n\n")
}

func renderRiskMeter(score int) string {
	totalBlocks := 15
	filled := (score * totalBlocks) / 100
	if filled > totalBlocks {
		filled = totalBlocks
	}

	var sb strings.Builder
	sb.WriteString("[")
	for i := 0; i < totalBlocks; i++ {
		if i < filled {
			sb.WriteString("█")
		} else {
			sb.WriteString("░")
		}
	}
	sb.WriteString(fmt.Sprintf("] %d/100", score))
	return sb.String()
}
