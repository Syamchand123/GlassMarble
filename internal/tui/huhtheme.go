package tui

import (
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
)

// HuhTheme returns a branded Huh theme aligning with the GlassMarble design tokens (§6).
func HuhTheme() *huh.Theme {
	t := huh.ThemeCharm()
	primary := ColorPrimary.Dark
	accent := ColorAccent.Dark
	if !HasDarkBackground() {
		primary = ColorPrimary.Light
		accent = ColorAccent.Light
	}

	t.Focused.Title = t.Focused.Title.Foreground(lipgloss.AdaptiveColor(ColorTextPrimary)).Bold(true)
	t.Focused.Description = t.Focused.Description.Foreground(lipgloss.AdaptiveColor(ColorTextMuted))
	t.Focused.FocusedButton = t.Focused.FocusedButton.Background(lipgloss.AdaptiveColor(ColorPrimary)).Foreground(lipgloss.Color("#FFFFFF")).Bold(true)
	t.Focused.BlurredButton = t.Focused.BlurredButton.Foreground(lipgloss.AdaptiveColor(ColorTextSecondary))
	t.Focused.SelectSelector = t.Focused.SelectSelector.Foreground(lipgloss.AdaptiveColor(ColorAccent)).SetString("❯ ")
	t.Focused.SelectedOption = t.Focused.SelectedOption.Foreground(lipgloss.AdaptiveColor(ColorAccent)).Bold(true)
	t.Focused.UnselectedOption = t.Focused.UnselectedOption.Foreground(lipgloss.AdaptiveColor(ColorTextPrimary))
	t.Focused.TextInput.Prompt = t.Focused.TextInput.Prompt.Foreground(lipgloss.AdaptiveColor(ColorAccent))
	t.Focused.TextInput.Cursor = t.Focused.TextInput.Cursor.Foreground(lipgloss.AdaptiveColor(ColorPrimary))

	_ = primary
	_ = accent
	return t
}
