package views

import (
	"fmt"

	"github.com/Syamchand123/GlassMarble/internal/tui"
)

// RenderExportSuccess renders the `gmb export` success card. The "Exported AKG
// snapshot" phrase is preserved for tests.
func RenderExportSuccess(format, path string, nodeCount int, sizeBytes int64) string {
	formatBadge := tui.BadgeInfo.Render("  GraphJSON (.json)  ")
	if format == "turtle" {
		formatBadge = tui.BadgeOK.Render("  Turtle (.ttl)  ")
	}
	rows := []string{
		tui.StyleOK.Render("  ✓  Exported AKG snapshot ("+itoa(nodeCount)+" nodes)"),
		"",
		tui.KV("Format", formatBadge),
		tui.KV("File", tui.StyleCode.Render(path)),
		tui.KV("Nodes", itoa(nodeCount)),
		tui.KV("Size", humanBytes(sizeBytes)),
		"",
		tui.StyleMuted.Render("  Use this file with:"),
		tui.Indent(tui.StyleCode.Render("gmb compare base.json "+path), 2),
		tui.Indent(tui.StyleCode.Render("gmb import "+path), 2),
	}
	return tui.StyleCard.Render("  " + joinLines(rows))
}

// RenderExportUnsupported renders the unsupported-format error card.
func RenderExportUnsupported(ext string) string {
	return tui.StyleCard.Render("  " + tui.StyleError.Render(fmt.Sprintf("  ✗ Unsupported output format %q (use .json or .ttl)", ext)))
}
