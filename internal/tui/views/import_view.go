package views

import (
	"github.com/Syamchand123/GlassMarble/internal/tui"
)

// RenderImportSuccess renders the `gmb import` success card. The "Imported AKG
// snapshot" phrase and node/edge counts are preserved for tests.
func RenderImportSuccess(inputPath, storageDir string, nodeCount, edgeCount int) string {
	rows := []string{
		tui.BadgeWarn.Render("  ⚠  Replacing active AKG snapshot  "),
		"",
		tui.KV("Source", tui.StyleCode.Render(inputPath)),
		tui.KV("Replacing", tui.StyleCode.Render(storageDir+"/akg.json")),
		"",
		tui.BadgeOK.Render("  ✓  Imported AKG snapshot  "),
		tui.KV("Nodes", itoa(nodeCount)),
		tui.KV("Edges", itoa(edgeCount)),
		tui.KV("WAL", tui.StyleMuted.Render("truncated after import")),
		"",
		tui.StyleMuted.Render("  Verify with: gmb doctor"),
	}
	return tui.StyleCard.Render("  " + joinLines(rows))
}
