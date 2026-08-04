package views

import (
	"fmt"

	"github.com/Syamchand123/GlassMarble/internal/ai_engine"
	"github.com/Syamchand123/GlassMarble/internal/tui"
)

// RenderAIDoctor renders the `gmb ai doctor` checklist. "All checks passed."
// is preserved for tests.
func RenderAIDoctor(rep *ai_engine.DoctorReport, maskedKey string) string {
	rows := []string{
		tui.StyleH2.Render("  AI Engine Doctor"),
		"",
		tui.KV("Provider", tui.StyleTextSecondary.Render(rep.Provider)+tui.StyleMuted.Render(" ("+rep.DisplayName+")")),
		tui.KV("Adapter", rep.Adapter),
		tui.KV("Model", tui.StyleTextSecondary.Render(rep.Model)),
		tui.KV("Base URL", defaultOrCustom(rep.BaseURL)),
		tui.KV("API Key", maskedKey+tui.StyleMuted.Render("  ("+rep.KeySource+")")),
		"",
		"  " + tui.Divider("Checks", 56),
	}

	if len(rep.Problems) == 0 {
		rows = append(rows, tui.BadgeOK.Render(" PASS ")+"  Config valid")
	} else {
		rows = append(rows, tui.BadgeError.Render(" FAIL ")+"  Config valid")
	}

	switch rep.PingStatus {
	case "ok":
		rows = append(rows, tui.BadgeOK.Render(" PASS ")+fmt.Sprintf("  Ping           ok (%.1fs)", rep.PingDuration.Seconds()))
	case "failed":
		rows = append(rows, tui.BadgeError.Render(" FAIL ")+"  Ping           failed")
	default:
		rows = append(rows, tui.BadgeWarn.Render(" SKIP ")+"  Ping           skipped (configuration problems above)")
	}

	if rep.AKGExists {
		rows = append(rows, tui.BadgeOK.Render(" PASS ")+fmt.Sprintf("  AKG present    %s, modified %s", humanBytes(rep.AKGSize), rep.AKGModified.Format("2006-01-02T15:04:05Z07:00")))
	} else {
		rows = append(rows, tui.BadgeError.Render(" FAIL ")+"  AKG present    not found — run `gmb analyze` first")
	}

	rows = append(rows, "")
	if len(rep.Problems) == 0 {
		rows = append(rows, tui.BadgeOK.Render("  ● All checks passed.  "))
	} else {
		rows = append(rows, tui.BadgeError.Render(fmt.Sprintf("  ● %d problem(s) found  ", len(rep.Problems))))
		for _, p := range rep.Problems {
			rows = append(rows, "    - "+tui.StyleError.Render(p))
		}
	}
	return tui.StyleCard.Render("  " + joinLines(rows))
}
