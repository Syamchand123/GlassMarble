package views

import (
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/ai_engine/provider"
	"github.com/Syamchand123/GlassMarble/internal/tui"
)

// RenderModels renders the `gmb ai models` provider card list. Provider names,
// "key env", and the "* = configured provider" footer are preserved for tests.
// keyIsSet reports whether a key is configured for the provider's key env var.
func RenderModels(registry []provider.Meta, active string, keyIsSet func(envVar string) bool) string {
	rows := []string{
		tui.StyleH2.Render("  Available AI Providers"),
		"",
	}

	for _, m := range registry {
		marker := tui.StyleMuted.Render("  ")
		if m.Name == active {
			marker = tui.StylePrimaryText.Render("● ")
		}
		keyBadge := tui.BadgeWarn.Render("  no key  ")
		if !m.RequiresKey {
			keyBadge = tui.BadgeInfo.Render("  no key required  ")
		} else if keyIsSet(m.KeyEnvVar) {
			keyBadge = tui.BadgeOK.Render("  key set ✓  ")
		}
		name := tui.StyleTextSecondary.Render(m.Name)
		if m.Name == active {
			name = tui.StylePrimaryText.Bold(true).Render(m.Name + " *")
		}
		rows = append(rows,
			"  "+marker+"  "+name+"  "+tui.StyleLabel.Render(pad(m.DisplayName, 24))+"  "+keyBadge,
			"     adapter: "+tui.StyleAccent.Render(string(m.Adapter))+"  │  "+tui.StyleMuted.Render("base URL: "+defaultOrCustom(m.DefaultBaseURL)),
			"     key env: "+tui.StyleCode.Render(m.KeyEnvVar),
		)
		if len(m.Models) > 0 {
			rows = append(rows, "     models:  "+tui.StyleTextSecondary.Render(strings.Join(m.Models, ", ")))
		} else {
			rows = append(rows, "     models:  "+tui.StyleMuted.Render("(set any model with --model)"))
		}
		rows = append(rows, "")
	}

	rows = append(rows, tui.StyleMuted.Render("* = configured provider"))
	rows = append(rows, tui.StyleMuted.Render("Configure: gmb ai configure"))
	return tui.StyleCard.Render("  " + joinLines(rows))
}

func defaultOrCustom(v string) string {
	if v == "" {
		return "(set --base-url)"
	}
	return v
}
