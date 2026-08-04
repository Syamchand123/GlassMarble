package views

import (
	"fmt"

	"github.com/Syamchand123/GlassMarble/internal/ai_engine/session"
	"github.com/Syamchand123/GlassMarble/internal/tui"
)

// RenderSessions renders the `gmb ai sessions` table. Session IDs and the
// "N session(s)" footer are preserved for tests.
func RenderSessions(list []session.Summary) string {
	if len(list) == 0 {
		return tui.StyleCard.Render("  " + tui.StyleMuted.Render("  No saved sessions. Start one with `gmb ai chat`."))
	}

	rows := []string{
		tui.StyleH2.Render(fmt.Sprintf("  AI Chat Sessions — %d session(s)", len(list))),
		"",
		"  " + tui.StyleLabel.Render(pad("ID", 20)+pad("UPDATED", 20)+pad("PROVIDER/MODEL", 28)+pad("MSGS", 7)+pad("TURNS", 8)+pad("TOKENS", 10)+pad("COST", 9)+pad("TOOLS", 7)),
		"  " + tui.Divider("", 96),
	}
	for _, s := range list {
		rows = append(rows, fmt.Sprintf("  %s %s %s %5d %6d %8d %9s %7d",
			tui.StyleAccent.Render(pad(s.ID, 20)),
			tui.StyleMuted.Render(pad(s.Updated.Format("2006-01-02 15:04"), 20)),
			tui.StyleTextSecondary.Render(pad(s.Provider+"/"+s.Model, 28)),
			s.Messages, s.Turns, s.Tokens, costOrNA(s.CostUSD, s.Tokens > 0), s.ToolCalls))
	}
	rows = append(rows, "",
		tui.StyleMuted.Render(fmt.Sprintf("  %d session(s). Resume with: gmb ai chat --session <id>", len(list))))
	return tui.StyleCard.Render("  " + joinLines(rows))
}

func costOrNA(usd float64, hasUsage bool) string {
	if usd <= 0 {
		return "n/a"
	}
	return fmt.Sprintf("$%.4f", usd)
}
