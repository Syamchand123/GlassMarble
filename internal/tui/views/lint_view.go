package views

import (
	"fmt"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/arch_linter"
	"github.com/Syamchand123/GlassMarble/internal/tui"
)

// RenderLintResult formats the full architectural lint result using Lip Gloss.
func RenderLintResult(res *arch_linter.LintResult, rulesFile string) string {
	if res == nil {
		return ""
	}

	var statusBadge string
	if res.Passed && res.WarningsCount == 0 {
		statusBadge = tui.BadgeOK.Render("  PASS  ")
	} else if res.Passed && res.WarningsCount > 0 {
		statusBadge = tui.BadgeWarn.Render("  PASS WITH WARNINGS  ")
	} else {
		statusBadge = tui.BadgeError.Render("  VIOLATIONS DETECTED  ")
	}

	header := tui.StyleH2.Render("  Architecture Linter Report")

	stats := []string{
		"  Rules File:   " + tui.StyleCode.Render(rulesFile),
		"  Status:       " + statusBadge,
		fmt.Sprintf("  Rules Run:    %d total (%d passed, %d violated)", res.RulesTotal, res.RulesPassed, res.RulesTotal-res.RulesPassed),
		fmt.Sprintf("  Violations:   %d total (%s errors, %s warnings)",
			res.ViolationsTotal,
			tui.StyleError.Render(fmt.Sprintf("%d", res.ErrorsCount)),
			tui.StyleWarningText.Render(fmt.Sprintf("%d", res.WarningsCount))),
	}

	summaryCard := tui.StyleCard.Render("  " + header + "\n\n" + joinLines(stats))

	if len(res.Violations) == 0 {
		perfectCard := tui.StyleCard.Render("  " + tui.StyleOK.Render("✓ No architectural layer or dependency violations detected! All rules passed."))
		return summaryCard + "\n\n" + perfectCard
	}

	var violationCards []string
	for i, v := range res.Violations {
		var badge string
		if v.Severity == arch_linter.SeverityError {
			badge = tui.BadgeError.Render(" " + string(v.Severity) + " ")
		} else {
			badge = tui.BadgeWarn.Render(" " + string(v.Severity) + " ")
		}

		vTitle := fmt.Sprintf("  #%d [%s] %s  %s", i+1, v.RuleID, v.RuleName, badge)
		vRows := []string{
			tui.StyleTextSecondary.Render(vTitle),
			"  " + tui.StyleAccent.Render(v.Message),
		}

		if v.SourcePath != "" && v.TargetPath != "" {
			src := v.SourcePath
			if v.SourceLine > 0 {
				src = fmt.Sprintf("%s:%d", src, v.SourceLine)
			}
			tgt := v.TargetPath
			if v.TargetLine > 0 {
				tgt = fmt.Sprintf("%s:%d", tgt, v.TargetLine)
			}
			vRows = append(vRows, "  Dependency:  "+tui.StyleCode.Render(src)+" → "+tui.StyleError.Render(tgt))
		} else if v.SourcePath != "" {
			vRows = append(vRows, "  Location:    "+tui.StyleCode.Render(v.SourcePath))
		}

		if v.Suggestion != "" {
			vRows = append(vRows, "  Suggestion:  "+tui.StyleMuted.Render(v.Suggestion))
		}

		violationCards = append(violationCards, tui.StyleCard.Render("  "+joinLines(vRows)))
	}

	return summaryCard + "\n\n" + strings.Join(violationCards, "\n\n")
}
