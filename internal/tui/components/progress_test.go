package components

import (
	"strings"
	"testing"
	"time"
)

func TestNewPhaseProgressViewTotalOne(t *testing.T) {
	p := NewPhaseProgress("Tree-sitter Ingestion", 40)
	v := p.View()
	if !strings.Contains(v, "Tree-sitter Ingestion") {
		t.Errorf("view missing label:\n%s", v)
	}
	if strings.Contains(v, "✓") {
		t.Errorf("view should not show done marker before MarkDone:\n%s", v)
	}
}

func TestPhaseProgressMarkDoneRendersCheckmark(t *testing.T) {
	p := NewPhaseProgress("Committing graph", 40)
	_ = p.SetProgress(0, 1)
	if cmd := p.MarkDone(1234 * time.Millisecond); cmd == nil {
		t.Fatal("MarkDone should return a cmd")
	}
	if !p.IsDone() {
		t.Fatal("IsDone should be true after MarkDone")
	}
	v := p.View()
	if !strings.Contains(v, "✓") {
		t.Errorf("done view missing checkmark:\n%s", v)
	}
	if !strings.Contains(v, "1234ms") {
		t.Errorf("done view missing elapsed duration:\n%s", v)
	}
}

func TestPhaseProgressSetProgressPartial(t *testing.T) {
	p := NewPhaseProgress("Semantic Linking", 40)
	cmd := p.SetProgress(2, 4)
	if cmd == nil {
		t.Fatal("SetProgress should return a spring animation cmd")
	}
	if p.IsDone() {
		t.Error("partial progress must not mark done")
	}
}

func TestPhaseProgressUpdateForwardsCmd(t *testing.T) {
	p := NewPhaseProgress("Topology Aggregation", 40)
	cmd := p.SetProgress(1, 1)
	// Feed the model its own spring FrameMsg; the resulting cmd may be non-nil
	// (scheduling the next frame) or nil (spring settled).
	_, next := p.Update(cmd())
	_ = next
	if p.View() == "" {
		t.Error("view should render after update")
	}
}

func TestPhaseProgressZeroWidthDefault(t *testing.T) {
	p := NewPhaseProgress("Committing graph", 0)
	if p.View() == "" {
		t.Error("view should render with default width")
	}
}

func TestGMSpinnerFramesTick(t *testing.T) {
	s := NewGMSpinner("Analyzing...")
	if s.View() == "" {
		t.Error("spinner view empty")
	}
	if cmd := s.Tick(); cmd == nil {
		t.Fatal("Tick returned nil")
	}
	s2, cmd := s.Update(spinnerTickTestMsg{})
	_ = s2
	if cmd != nil {
		t.Error("unexpected cmd from a non-spinner message")
	}
}

type spinnerTickTestMsg struct{}
