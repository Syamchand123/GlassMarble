// Package views contains Lip Gloss-only static render passes for the
// GlassMarble commands. No BubbleTea event loop is involved; each function
// returns a ready-to-print string.
package views

import (
	"github.com/Syamchand123/GlassMarble/internal/tui"
)

// RenderInitSuccess renders the post-`gmb init` success card.
func RenderInitSuccess(gmDir string, gitignoreUpdated bool) string {
	rows := []string{
		tui.StyleH2.Render("  ✓  GlassMarble Workspace Initialized"),
		"",
		tui.KV("Location", tui.StyleCode.Render(".glassmarble/")),
		tui.KV("Config", tui.StyleCode.Render(gmDir+"/config.yaml")),
		tui.KV("AKG State", tui.StyleCode.Render(gmDir+"/akg_state.ttl")),
	}
	if gitignoreUpdated {
		rows = append(rows, tui.KV("Gitignore", tui.StyleMuted.Render(".glassmarble added to .gitignore")))
	}
	rows = append(rows,
		"",
		tui.StyleMuted.Render("  Next step: gmb analyze"),
	)
	return tui.StyleCard.Render("  " + joinLines(rows))
}
