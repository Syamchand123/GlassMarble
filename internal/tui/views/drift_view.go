package views

import (
	"fmt"

	"github.com/Syamchand123/GlassMarble/internal/drift"
	"github.com/Syamchand123/GlassMarble/internal/tui"
)

// RenderDrift renders the `gmb drift` report. The "Architecture Drift Report",
// "RESULT: PASS/FAIL" verdict lines, and "FORBIDDEN_DEPENDENCY" violation
// markers are preserved verbatim for tests.
func RenderDrift(rep *drift.Report) string {
	rows := []string{
		tui.StyleH2.Render("  Architecture Drift Report"),
		"",
		tui.KV("Layers declared", itoa(rep.LayersDefined)),
		tui.KV("Forbidden deps", itoa(rep.ForbiddenEdges)),
	}

	cycleLine := tui.KV("Cycle count", fmt.Sprintf("%d (budget %d)", rep.CycleCount, rep.CycleBudget))
	if rep.ExceedsBudget() {
		cycleLine += "  " + tui.StyleError.Render("✗ EXCEEDED")
	}
	rows = append(rows, cycleLine)

	rows = append(rows, "", "  "+tui.Divider("Violations", maxCardLine()))
	if len(rep.Violations) == 0 {
		rows = append(rows, tui.StyleMuted.Render("  (none)"))
	}
	for _, v := range rep.Violations {
		switch v.Kind {
		case drift.KindForbiddenDep:
			rows = append(rows,
				"  "+tui.StyleError.Render("✗ [FORBIDDEN_DEPENDENCY]")+"  "+tui.StyleError.Render(truncateLeft(v.SourceID, 52)),
				tui.Indent(tui.StyleWarningText.Render("→ ")+tui.StyleTextSecondary.Render(truncateLeft(v.TargetID, 52)), 4),
				tui.Indent(tui.StyleMuted.Render(v.Message), 4),
			)
		case drift.KindCycle:
			rows = append(rows,
				"  "+tui.StyleWarningText.Render("↻ [cycle]")+"  "+tui.StyleTextSecondary.Render(v.Message),
			)
		default:
			rows = append(rows,
				"  "+tui.StyleError.Render("✗ ["+string(v.Kind)+"]")+"  "+tui.StyleTextSecondary.Render(v.SourceID+" → "+v.TargetID),
			)
		}
	}

	rows = append(rows, "")
	if rep.ExceedsBudget() {
		rows = append(rows, tui.BadgeError.Render(fmt.Sprintf("  RESULT: FAIL — %d cycles exceed the declared budget of %d  ", rep.CycleCount, rep.CycleBudget)))
	} else if rep.ForbiddenEdges > 0 {
		rows = append(rows, tui.BadgeError.Render(fmt.Sprintf("  RESULT: FAIL — %d forbidden dep(s)  ", rep.ForbiddenEdges)))
	} else {
		rows = append(rows, tui.BadgeOK.Render("  RESULT: PASS — no drift detected  "))
	}

	return tui.StyleCard.Render("  " + joinLines(rows))
}
