// Package views contains Lip Gloss-only static render passes for the
// GlassMarble commands. No BubbleTea event loop is involved; each function
// returns a ready-to-print string.
package views

import (
	"github.com/Syamchand123/GlassMarble/internal/tui"
)

// RenderInitSuccess renders the post-`gmb init` success card with the logo banner and actionable next steps.
func RenderInitSuccess(gmDir string, gitignoreUpdated bool) string {
	banner := tui.RenderLogoBanner()
	rows := []string{
		tui.StyleH2.Render("  ✓  GlassMarble Workspace Initialized"),
		"",
		tui.KV("Location", tui.StyleCode.Render(".glassmarble/")),
		tui.KV("Config", tui.StyleCode.Render(gmDir+"/config.yaml")),
		tui.KV("AKG State", tui.StyleCode.Render(gmDir+"/akg.json")),
	}
	if gitignoreUpdated {
		rows = append(rows, tui.KV("Gitignore", tui.StyleMuted.Render(".glassmarble added to .gitignore")))
	}
	rows = append(rows,
		"",
		"  "+tui.Divider("Next Steps", maxCardLine()),
		"  " + tui.StyleAccent.Render("1. ") + tui.StyleCode.Render("gmb doctor") + tui.StyleMuted.Render("         — Verify environment & parsers"),
		"  " + tui.StyleAccent.Render("2. ") + tui.StyleCode.Render("gmb analyze") + tui.StyleMuted.Render("        — Ingest & construct knowledge graph"),
		"  " + tui.StyleAccent.Render("3. ") + tui.StyleCode.Render("gmb visualize list") + tui.StyleMuted.Render(" — Explore available diagram types"),
	)
	card := tui.StyleCard.Render("  " + joinLines(rows))
	return banner + "\n" + card
}
