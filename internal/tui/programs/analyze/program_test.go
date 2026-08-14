package analyze

import (
	"errors"
	"testing"
	"time"
)

func TestMaxInt(t *testing.T) {
	cases := []struct {
		a, b, want int
	}{
		{3, 5, 5},
		{9, 2, 9},
		{-1, 0, 0},
		{4, 4, 4},
	}
	for _, c := range cases {
		if got := maxInt(c.a, c.b); got != c.want {
			t.Errorf("maxInt(%d,%d)=%d want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestHumanBytes(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{512, "512B"},
		{2048, "2.0KB"},
		{5 << 20, "5.0MB"},
		{2 << 30, "2.0GB"},
	}
	for _, c := range cases {
		if got := humanBytes(c.in); got != c.want {
			t.Errorf("humanBytes(%d)=%q want %q", c.in, got, c.want)
		}
	}
}

func TestPhaseStartOutOfRange(t *testing.T) {
	m := newModel(Options{}, nil)
	// A phase index outside [1,5] must not panic and must not flip running.
	for _, bad := range []int{0, 6, 99, -1} {
		next, cmd := m.Update(PhaseStartMsg{phase: bad, name: "bogus"})
		m = next.(model)
		if cmd != nil {
			t.Errorf("phase %d returned a cmd, want nil", bad)
		}
		for i := range m.phases {
			if m.phases[i].running {
				t.Errorf("phase %d marked running for bad phase msg %d", i, bad)
			}
		}
	}
}

func TestPhaseCompleteExceedsTotalMarksDone(t *testing.T) {
	m := newModel(Options{}, nil)
	next, _ := m.Update(PhaseStartMsg{phase: 1, name: "Tree-sitter Ingestion"})
	m = next.(model)
	if !m.phases[0].running {
		t.Fatal("phase 1 should be running after PhaseStartMsg")
	}
	next, cmd := m.Update(PhaseCompleteMsg{phase: 1, current: 10, total: 5})
	m = next.(model)
	if cmd == nil {
		t.Fatal("expected a MarkDone command")
	}
	if !m.phases[0].progress.IsDone() {
		t.Error("phase 1 should be marked done when current >= total")
	}
	if m.phases[0].running {
		t.Error("phase 1 should no longer be running after completion")
	}
}

func TestPhaseCompletePartialKeepsRunning(t *testing.T) {
	m := newModel(Options{}, nil)
	next, _ := m.Update(PhaseStartMsg{phase: 2, name: "GAST Normalization"})
	m = next.(model)
	next, cmd := m.Update(PhaseCompleteMsg{phase: 2, current: 3, total: 10})
	m = next.(model)
	if cmd == nil {
		t.Fatal("expected a SetProgress command for a partial update")
	}
	if m.phases[1].progress.IsDone() {
		t.Error("partial progress must not mark the phase done")
	}
	if !m.phases[1].running {
		t.Error("phase 2 should remain running after a partial update")
	}
}

func TestAnalysisDoneMsgSetsSummaryAndQuits(t *testing.T) {
	m := newModel(Options{}, nil)
	sum := Summary{FilesAnalyzed: 12, Nodes: 34, Edges: 56}
	next, cmd := m.Update(AnalysisDoneMsg{summary: sum})
	m = next.(model)
	if cmd == nil {
		t.Fatal("expected tea.Quit after AnalysisDoneMsg")
	}
	if !m.done {
		t.Error("model should be done")
	}
	if m.summary.FilesAnalyzed != 12 || m.summary.Nodes != 34 {
		t.Errorf("summary not stored: %+v", m.summary)
	}
}

func TestAnalysisErrMsgStoresError(t *testing.T) {
	m := newModel(Options{}, nil)
	sentinel := errors.New("boom")
	next, cmd := m.Update(AnalysisErrMsg{err: sentinel})
	m = next.(model)
	if cmd == nil {
		t.Fatal("expected tea.Quit after AnalysisErrMsg")
	}
	if !errors.Is(m.err, sentinel) {
		t.Errorf("error not stored: %v", m.err)
	}
}

func TestTickCmdProducesTickMsg(t *testing.T) {
	cmd := tickCmd()
	if cmd == nil {
		t.Fatal("tickCmd returned nil")
	}
	// tea.Cmd is a func; calling it must yield a tickMsg.
	msg := cmd()
	if _, ok := msg.(tickMsg); !ok {
		t.Fatalf("tick command returned %T, want tickMsg", msg)
	}
}

func TestNewModelInitializesAllPhases(t *testing.T) {
	m := newModel(Options{}, nil)
	for i := range m.phases {
		if m.phases[i].progress.View() == "" {
			t.Errorf("phase %d progress has empty view", i)
		}
	}
}

func TestElapsedTicksForward(t *testing.T) {
	m := newModel(Options{}, nil)
	next, cmd := m.Update(tickMsg(time.Now()))
	m = next.(model)
	if cmd == nil {
		t.Fatal("tick should reschedule the next tick")
	}
	if m.elapsed < 0 {
		t.Error("elapsed should be non-negative after a tick")
	}
}
