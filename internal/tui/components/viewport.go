package components

import (
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/tui"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
)

// NewGMViewport builds a themed scrollable viewport with key bindings matching §12 standard.
func NewGMViewport(width, height int) viewport.Model {
	if width < 20 {
		width = 40
	}
	if height < 3 {
		height = 10
	}
	vp := viewport.New(width, height)
	vp.KeyMap.PageUp = key.NewBinding(key.WithKeys("pgup", "b"))
	vp.KeyMap.PageDown = key.NewBinding(key.WithKeys("pgdown", "f", "space"))
	vp.KeyMap.HalfPageUp = key.NewBinding(key.WithKeys("ctrl+u"))
	vp.KeyMap.HalfPageDown = key.NewBinding(key.WithKeys("ctrl+d"))
	vp.KeyMap.Up = key.NewBinding(key.WithKeys("up", "k"))
	vp.KeyMap.Down = key.NewBinding(key.WithKeys("down", "j"))
	vp.MouseWheelEnabled = true
	return vp
}

// StyleViewportContent wraps content that will be placed inside the viewport.
func StyleViewportContent(content string) string {
	return tui.R.NewStyle().Padding(0, 2).Render(content)
}

// ScrollPosition renders "↑↓ 3/12 pages" style indicator (page / total pages).
func ScrollPosition(vp viewport.Model) string {
	total := vp.TotalLineCount()
	pageHeight := vp.Height
	if pageHeight < 1 {
		pageHeight = 1
	}
	pages := (total + pageHeight - 1) / pageHeight
	if pages < 1 {
		pages = 1
	}
	current := vp.YOffset/pageHeight + 1
	if current < 1 {
		current = 1
	}
	if current > pages {
		current = pages
	}
	return tui.StyleMuted.Render(strings.Repeat(" ", 2) + "↑↓ " + itoa(current) + "/" + itoa(pages) + " pages")
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	digits := []byte{}
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}
