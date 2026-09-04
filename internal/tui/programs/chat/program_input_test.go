package chat

import (
	"context"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/tui/components"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
)

// newTestModel builds a chat model with a focused composer, mirroring Run's
// setup without starting a bubbletea program or touching the AI engine.
func newTestModel() *model {
	ta := textarea.New()
	ta.ShowLineNumbers = false
	ta.SetHeight(3)
	ta.Focus()
	return &model{
		ctx:     context.Background(),
		cancel:  func() {},
		width:   80,
		height:  24,
		history: components.NewGMViewport(78, 16),
		input:   ta,
		spinner: components.NewGMSpinner("Thinking…"),
	}
}

// typeRunes feeds each rune to the model as a key press, the way a terminal
// would while the user types.
func typeRunes(t *testing.T, m *model, s string) *model {
	t.Helper()
	cur := m
	for _, r := range s {
		var msg tea.KeyMsg
		if r == ' ' {
			msg = tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}
		} else {
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
		}
		next, _ := cur.Update(msg)
		cur = next.(*model)
	}
	return cur
}

// TestComposerAcceptsAllLetters is a regression test for vi-style viewport
// bindings that were matched before the composer saw the key: b, f, g, G, j, k
// and space were swallowed as scroll commands, so those characters simply
// could not be typed into the chat box.
func TestComposerAcceptsAllLetters(t *testing.T) {
	cases := []string{
		"before",               // b, f, e...
		"go",                   // g
		"Growing",              // G
		"jkbf gG",              // every previously-captured key, incl. space
		"debug the func graph", // realistic sentence
	}
	for _, want := range cases {
		m := typeRunes(t, newTestModel(), want)
		if got := m.input.Value(); got != want {
			t.Errorf("composer dropped characters: typed %q, composer holds %q", want, got)
		}
	}
}

// TestScrollKeysStillWorkWhenComposerBlurred keeps the vi-style aliases alive
// for the case they were meant for.
func TestScrollKeysStillWorkWhenComposerBlurred(t *testing.T) {
	m := newTestModel()
	m.input.Blur()
	before := m.input.Value()

	for _, k := range []string{"b", "f", "g", "G", "j", "k"} {
		next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)})
		m = next.(*model)
	}
	if m.input.Value() != before {
		t.Errorf("blurred composer should not receive navigation keys, got %q", m.input.Value())
	}
}

// TestNonTypeableScrollKeysAlwaysActive verifies keys that can never be typed
// stay bound regardless of focus.
func TestNonTypeableScrollKeysAlwaysActive(t *testing.T) {
	m := newTestModel() // composer focused
	for _, kt := range []tea.KeyType{tea.KeyPgUp, tea.KeyPgDown, tea.KeyHome, tea.KeyEnd} {
		next, _ := m.Update(tea.KeyMsg{Type: kt})
		m = next.(*model)
	}
	if m.input.Value() != "" {
		t.Errorf("navigation keys leaked into the composer: %q", m.input.Value())
	}
}

// TestEscQuits covers the missing quit binding: chat previously exited only on
// ctrl+c while every other program also accepted a plain key.
func TestEscQuits(t *testing.T) {
	m := newTestModel()
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(*model)
	if !m.quitting {
		t.Error("esc should mark the program as quitting")
	}
	if cmd == nil {
		t.Error("esc should return a quit command")
	}
}
