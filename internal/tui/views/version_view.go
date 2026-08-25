package views

import (
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/tui"
)

// RenderVersion renders the branded `gmb version` output.
// Optional extras can be provided in the order: commit, date, builtBy.
func RenderVersion(version string, extras ...string) string {
	name := tui.StyleAccent.Render("GlassMarble")
	displayVer := "v" + strings.TrimPrefix(version, "v")
	badge := tui.BadgeInfo.Render("  " + displayVer + "  ")
	tag := tui.StyleMuted.Render("AI Architecture Intelligence Platform")
	out := "  " + name + "  " + badge + "  " + tag

	var metaParts []string
	if len(extras) > 0 && extras[0] != "" && extras[0] != "none" {
		metaParts = append(metaParts, "commit: "+extras[0])
	}
	if len(extras) > 1 && extras[1] != "" && extras[1] != "unknown" {
		metaParts = append(metaParts, "date: "+extras[1])
	}
	if len(extras) > 2 && extras[2] != "" && extras[2] != "dev" {
		metaParts = append(metaParts, "built-by: "+extras[2])
	}

	if len(metaParts) > 0 {
		out += "\n  " + tui.StyleMuted.Render(strings.Join(metaParts, "  ·  "))
	}
	return out
}
