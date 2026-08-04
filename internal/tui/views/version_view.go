package views

import (
	"github.com/Syamchand123/GlassMarble/internal/tui"
)

// RenderVersion renders the branded `gmb version` one-liner.
func RenderVersion(version string) string {
	name := tui.StyleAccent.Render("GlassMarble")
	badge := tui.BadgeInfo.Render("  v" + version + "  ")
	tag := tui.StyleMuted.Render("AI Architecture Intelligence Platform")
	return "  " + name + "  " + badge + "  " + tag
}
