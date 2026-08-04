package components

import (
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/tui"
	"github.com/charmbracelet/lipgloss"
)

// RenderHeader draws the standard top header bar used by BubbleTea programs:
//
//	─ GlassMarble — <commandName> ─────── <subtitle> ─
func RenderHeader(commandName, subtitle string, width int) string {
	left := lipgloss.NewStyle().Foreground(tui.ColorPrimary).Bold(true).Render("GlassMarble")
	cmd := lipgloss.NewStyle().Foreground(tui.ColorTextPrimary).Bold(true).Render(commandName)

	leftText := "─ " + left + " — " + cmd + " ─"
	right := tui.StyleMuted.Render(subtitle)

	if width <= 0 {
		width = 60
	}
	fillLen := width - lipgloss.Width(leftText) - lipgloss.Width(right) - 1
	if fillLen < 1 {
		fillLen = 1
	}
	fill := tui.StyleDivider.Render(strings.Repeat("─", fillLen))
	return lipgloss.NewStyle().Foreground(tui.ColorBorder).Render(leftText + fill + right)
}
