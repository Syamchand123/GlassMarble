package components

import (
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/tui"
	"github.com/charmbracelet/lipgloss"
)

// RenderStatusBar draws the standard bottom status bar:
//
//	[ q quit ]  [ ↑↓ scroll ]  ──────  Elapsed: 3.2s
func RenderStatusBar(left, right string, width int) string {
	if width <= 0 {
		width = 60
	}
	leftStyled := tui.R.NewStyle().Foreground(tui.ColorDim).Render(left)
	rightStyled := tui.R.NewStyle().Foreground(tui.ColorDim).Render(right)

	fillLen := width - lipgloss.Width(leftStyled) - lipgloss.Width(rightStyled) - 1
	if fillLen < 1 {
		fillLen = 1
	}
	fill := tui.StyleDivider.Render(strings.Repeat("─", fillLen))
	return tui.R.NewStyle().Foreground(tui.ColorBorder).Render(leftStyled + fill + rightStyled)
}

// KeyHint renders a single keybinding hint in the "[ q quit ]" style.
func KeyHint(key, action string) string {
	return tui.R.NewStyle().Foreground(tui.ColorDim).Render(
		"[" + tui.R.NewStyle().Foreground(tui.ColorAccent).Render(key) + " " + action + "]",
	)
}

// JoinKeyHints joins multiple key hints with spaces.
func JoinKeyHints(hints ...string) string {
	return strings.Join(hints, "  ")
}
