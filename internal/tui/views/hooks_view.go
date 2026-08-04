package views

import (
	"github.com/Syamchand123/GlassMarble/internal/tui"
)

// RenderHooksInstalled renders the `gmb hooks install` success card. The
// "installed successfully" phrase is preserved for tests.
func RenderHooksInstalled(hookPath, binary, targetDir string) string {
	rows := []string{
		tui.StyleOK.Render("  ✓  Git Hook Installed"),
		"",
		tui.KV("Path", tui.StyleCode.Render(hookPath)),
		tui.KV("Trigger", tui.StyleMuted.Render("After every git commit")),
		tui.KV("Action", tui.StyleCode.Render(binary+" analyze --dir "+targetDir)),
		"",
		tui.StyleMuted.Render("  GlassMarble post-commit hook installed successfully at " + hookPath),
		tui.StyleMuted.Render("  GlassMarble will now analyze your repo automatically on every commit."),
	}
	return tui.StyleCard.Render("  " + joinLines(rows))
}

// RenderHooksUninstalled renders the `gmb hooks uninstall` success message.
func RenderHooksUninstalled() string {
	return tui.StyleCard.Render("  " + tui.StyleOK.Render("  ✓  GlassMarble post-commit hook uninstalled successfully."))
}

// RenderHooksNone renders the no-hook-found message.
func RenderHooksNone() string {
	return tui.StyleCard.Render("  " + tui.StyleWarningText.Render("  No active GlassMarble post-commit hook found."))
}
