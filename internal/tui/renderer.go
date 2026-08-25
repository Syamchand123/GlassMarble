package tui

import (
	"os"

	"github.com/charmbracelet/lipgloss"
)

// R is the central lipgloss Renderer for GlassMarble.
// By routing all style creation through R, we ensure terminal capability
// detection (NO_COLOR, TERM=dumb, background color detection) happens once
// and consistently across all views and interactive Bubble Tea programs.
var R = lipgloss.NewRenderer(os.Stdout)

// HasDarkBackground returns whether the current terminal has a dark background.
func HasDarkBackground() bool {
	return R.HasDarkBackground()
}
