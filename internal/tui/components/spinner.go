package components

import (
	"time"

	"github.com/Syamchand123/GlassMarble/internal/tui"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
)

// GMSpinner wraps bubbles/spinner with the GlassMarble brand: quarter-circle
// frames in accent cyan.
type GMSpinner struct {
	model spinner.Model
	label string
}

// NewGMSpinner returns a branded spinner with an optional label.
func NewGMSpinner(label string) GMSpinner {
	s := spinner.New()
	s.Spinner = spinner.Spinner{Frames: tui.SpinnerFrames, FPS: time.Second / 4}
	s.Style = tui.R.NewStyle().Foreground(tui.ColorAccent)
	return GMSpinner{model: s, label: label}
}

// Tick returns the spinner's Tick command for use in tea.Batch/Init.
func (g GMSpinner) Tick() tea.Cmd {
	return g.model.Tick
}

// Update forwards a message to the underlying spinner model.
func (g GMSpinner) Update(msg tea.Msg) (GMSpinner, tea.Cmd) {
	model, cmd := g.model.Update(msg)
	g.model = model
	return g, cmd
}

// View renders the current spinner frame plus the label.
func (g GMSpinner) View() string {
	return g.model.View() + " " + g.label
}

// SetLabel updates the spinner label.
func (g *GMSpinner) SetLabel(label string) {
	g.label = label
}

// Model exposes the underlying spinner.Model.
func (g GMSpinner) Model() spinner.Model { return g.model }
