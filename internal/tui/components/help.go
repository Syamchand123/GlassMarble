package components

import (
	"github.com/Syamchand123/GlassMarble/internal/tui"
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
)

// HelpOverlay wraps bubbles/help with GlassMarble branding and toggle state.
type HelpOverlay struct {
	help.Model
	Visible bool
	KeyMap  tui.KeyMap
}

// NewHelpOverlay constructs a branded help overlay.
func NewHelpOverlay(km tui.KeyMap) HelpOverlay {
	h := help.New()
	h.ShowAll = true
	h.Styles.ShortKey = tui.R.NewStyle().Foreground(tui.ColorAccent).Bold(true)
	h.Styles.ShortDesc = tui.R.NewStyle().Foreground(tui.ColorTextMuted)
	h.Styles.FullKey = tui.R.NewStyle().Foreground(tui.ColorAccent).Bold(true)
	h.Styles.FullDesc = tui.R.NewStyle().Foreground(tui.ColorTextMuted)
	h.Styles.FullSeparator = tui.R.NewStyle().Foreground(tui.ColorBorder)
	return HelpOverlay{
		Model:   h,
		Visible: false,
		KeyMap:  km,
	}
}

// Toggle flips the visibility of the help overlay.
func (h *HelpOverlay) Toggle() {
	h.Visible = !h.Visible
}

// View renders the help overlay if visible, or an empty string.
func (h HelpOverlay) View() string {
	if !h.Visible {
		return ""
	}
	content := h.Model.FullHelpView(h.KeyMap.FullHelp())
	header := tui.StyleH2.Render("Keyboard Shortcuts (? to close)")
	body := header + "\n\n" + content
	return tui.StyleCard.Render(body)
}

// ShortView renders a one-line summary footer of primary keys.
func (h HelpOverlay) ShortView(bindings []key.Binding) string {
	return h.Model.ShortHelpView(bindings)
}
