package analyze

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// TestQuitMarksDetached pins the honesty contract around quitting.
//
// The pipeline runs in its own goroutine and cannot be torn down mid
// transaction without risking a half-written graph, so quitting detaches the
// UI rather than stopping the work. Previously q simply returned tea.Quit and
// the user saw the program exit while analysis kept writing to the AKG, with
// no indication that anything was still happening.
func TestQuitMarksDetached(t *testing.T) {
	for _, key := range []string{"q", "esc", "ctrl+c"} {
		m := newModel(Options{TargetDir: "."}, nil)
		next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
		if key == "esc" {
			next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
		}
		if key == "ctrl+c" {
			next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
		}
		got, ok := next.(model)
		if !ok {
			t.Fatalf("%s: unexpected model type", key)
		}
		if !got.detached {
			t.Errorf("%s: quitting a running analysis should mark it detached", key)
		}
		if cmd == nil {
			t.Errorf("%s: expected a quit command", key)
		}
	}
}

// TestQuitAfterCompletionIsNotDetached: once the run finished there is nothing
// left in flight, so no background-work notice should be printed.
func TestQuitAfterCompletionIsNotDetached(t *testing.T) {
	m := newModel(Options{TargetDir: "."}, nil)
	m.done = true
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if got := next.(model); got.detached {
		t.Error("quitting a finished run must not report background work")
	}
}

// TestHelpToggle covers the '?' overlay the program previously lacked.
func TestHelpToggle(t *testing.T) {
	m := newModel(Options{TargetDir: "."}, nil)
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	shown := next.(model)
	if !shown.showHelp {
		t.Fatal("? should open the help overlay")
	}
	if shown.View() == "" {
		t.Error("help overlay rendered empty")
	}
	again, _ := shown.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	if again.(model).showHelp {
		t.Error("? should toggle the overlay closed")
	}
}
