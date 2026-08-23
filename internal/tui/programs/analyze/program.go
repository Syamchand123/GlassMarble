// Package analyze is the BubbleTea program for `gmb analyze`. It only holds
// display state: phase progress, spinner, elapsed time. The pipeline itself
// lives in cmd/ and is injected as a RunFn so this package never imports cmd.
package analyze

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/tui"
	"github.com/Syamchand123/GlassMarble/internal/tui/components"
	tea "github.com/charmbracelet/bubbletea"
)

// Options carries the analysis flags into the program.
type Options struct {
	TargetDir      string
	CommitHash     string
	Full           bool
	Workers        int
	Verbose        bool
	LinkLevel      string
	MacroInference string
	MaxNodes       int
	AbortOnLimit   bool
}

// Summary mirrors the human "Analyzed N files | ..." QA numbers.
type Summary struct {
	TargetDir     string
	FilesAnalyzed int
	Nodes         int
	Edges         int
	VirtualNodes  int
	DanglingEdges int
	StateBytes    int64
	Duration      time.Duration
}

// RunFn executes the full pipeline, reporting phase boundaries through
// progress, and returns the QA summary of the completed run.
type RunFn func(progress func(phase int, name string, current, total int)) (Summary, error)

// phaseNames lists the pipeline boundaries in order (phases 1..4 + commit).
var phaseNames = [...]string{
	"Tree-sitter Ingestion",
	"GAST Normalization",
	"Topology Aggregation",
	"Semantic Linking",
	"Committing graph",
}

type phaseState struct {
	progress components.PhaseProgress
	started  time.Time
	running  bool
}

type model struct {
	opts    Options
	run     RunFn
	spinner components.GMSpinner
	phases  [5]phaseState
	done    bool
	summary Summary
	err     error
	elapsed time.Duration
	started time.Time
	width   int
}

// PhaseStartMsg marks the beginning of a pipeline phase.
type PhaseStartMsg struct {
	phase int
	name  string
}

// PhaseCompleteMsg reports phase progress and/or completion.
type PhaseCompleteMsg struct {
	phase   int
	name    string
	current int
	total   int
}

// AnalysisDoneMsg carries the QA summary of a completed run.
type AnalysisDoneMsg struct {
	summary Summary
}

// AnalysisErrMsg reports a failed pipeline run.
type AnalysisErrMsg struct {
	err error
}

type tickMsg time.Time

func newModel(opts Options, run RunFn) model {
	m := model{
		opts:    opts,
		run:     run,
		spinner: components.NewGMSpinner("Analyzing..."),
		started: time.Now(),
	}
	for i := range m.phases {
		m.phases[i].progress = components.NewPhaseProgress(phaseNames[i], 40)
	}
	return m
}

// RunAnalyze launches the interactive analysis program and prints the styled
// summary or error card on completion.
func RunAnalyze(opts Options, run RunFn, in io.Reader, out io.Writer) error {
	p := tea.NewProgram(newModel(opts, run), tea.WithOutput(out), tea.WithInput(in))
	go func() {
		defer func() {
			if r := recover(); r != nil {
				p.Send(AnalysisErrMsg{err: fmt.Errorf("analysis panicked: %v", r)})
			}
		}()
		summary, err := run(func(phase int, name string, current, total int) {
			if current == 0 {
				p.Send(PhaseStartMsg{phase: phase, name: name})
				return
			}
			p.Send(PhaseCompleteMsg{phase: phase, name: name, current: current, total: total})
		})
		if err != nil {
			p.Send(AnalysisErrMsg{err: err})
			return
		}
		p.Send(AnalysisDoneMsg{summary: summary})
	}()
	final, err := p.Run()
	if err != nil {
		return err
	}
	m, ok := final.(model)
	if !ok {
		return nil
	}
	if m.err != nil {
		fmt.Fprintln(out, renderErrorCard(m.err, m.width))
		return m.err
	}
	if m.done {
		fmt.Fprintln(out, renderSummaryCard(m.summary, m.width))
	}
	return nil
}

func (m model) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick(), tickCmd())
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		return m, nil
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
	case tickMsg:
		m.elapsed = time.Since(m.started)
		return m, tickCmd()
	case PhaseStartMsg:
		idx := msg.phase - 1
		if idx < 0 || idx >= len(m.phases) {
			return m, nil
		}
		st := &m.phases[idx]
		cmd := st.progress.SetProgress(0, 1)
		st.started = time.Now()
		st.running = true
		return m, cmd
	case PhaseCompleteMsg:
		idx := msg.phase - 1
		if idx < 0 || idx >= len(m.phases) {
			return m, nil
		}
		st := &m.phases[idx]
		var cmd tea.Cmd
		if msg.current >= msg.total {
			cmd = st.progress.MarkDone(time.Since(st.started))
			st.running = false
		} else {
			cmd = st.progress.SetProgress(msg.current, maxInt(msg.total, msg.current))
		}
		return m, cmd
	case AnalysisDoneMsg:
		m.done = true
		m.summary = msg.summary
		return m, tea.Quit
	case AnalysisErrMsg:
		m.err = msg.err
		return m, tea.Quit
	}
	// Forward every message to the phase progress bars so the Harmonica spring
	// animation frames drive the fills.
	cmds := make([]tea.Cmd, 0, len(m.phases)+1)
	for i := range m.phases {
		var cmd tea.Cmd
		m.phases[i].progress, cmd = m.phases[i].progress.Update(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	var cmd tea.Cmd
	m.spinner, cmd = m.spinner.Update(msg)
	if cmd != nil {
		cmds = append(cmds, cmd)
	}
	return m, tea.Batch(cmds...)
}

func (m model) View() string {
	if m.done || m.err != nil {
		return ""
	}
	width := m.width
	if width <= 0 {
		width = 80
	}
	var b strings.Builder
	b.WriteString(components.RenderHeader("analyze", "building AKG", width))
	b.WriteString("\n\n")
	b.WriteString(tui.KV("Directory", m.opts.TargetDir))
	mode := "incremental (git delta)"
	if m.opts.Full {
		mode = "full scan"
	}
	b.WriteString("\n")
	b.WriteString(tui.KV("Mode", mode))
	b.WriteString("\n\n")
	for i := range m.phases {
		b.WriteString(phaseLine(m.phases[i]))
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(tui.StyleMuted.Render("Elapsed: " + m.elapsed.Round(100*time.Millisecond).String()))
	b.WriteString("\n")
	b.WriteString(m.spinner.View())
	return tui.StyleCard.Render(strings.TrimRight(b.String(), "\n"))
}

func renderSummaryCard(sum Summary, width int) string {
	var b strings.Builder
	b.WriteString(components.RenderHeader("analyze", "complete", width))
	b.WriteString("\n")
	b.WriteString(tui.StyleH2.Render("  ✓  Analysis Complete"))
	b.WriteString("\n\n")
	b.WriteString(tui.KV("Files Analyzed", fmt.Sprintf("%d", sum.FilesAnalyzed)))
	b.WriteString("\n")
	b.WriteString(tui.KV("Nodes", fmt.Sprintf("%d", sum.Nodes)))
	b.WriteString("\n")
	b.WriteString(tui.KV("Edges", fmt.Sprintf("%d", sum.Edges)))
	b.WriteString("\n")
	b.WriteString(tui.KV("Virtual Nodes", fmt.Sprintf("%d", sum.VirtualNodes)))
	b.WriteString("\n")
	b.WriteString(tui.KV("Dangling", fmt.Sprintf("%d", sum.DanglingEdges)))
	b.WriteString("\n")
	b.WriteString(tui.KV("State Size", humanBytes(sum.StateBytes)))
	b.WriteString("\n")
	b.WriteString(tui.KV("Duration", sum.Duration.Round(time.Millisecond).String()))
	if sum.DanglingEdges > 0 {
		b.WriteString("\n\n")
		b.WriteString(tui.BadgeWarn.Render(fmt.Sprintf("  %d dangling edges — run `gmb analyze --full` to rebuild", sum.DanglingEdges)))
	} else {
		b.WriteString("\n\n")
		b.WriteString(tui.BadgeOK.Render("  No issues detected — AKG is healthy"))
	}
	return tui.StyleCard.Render(b.String())
}

func renderErrorCard(err error, width int) string {
	var b strings.Builder
	b.WriteString(components.RenderHeader("analyze", "failed", width))
	b.WriteString("\n")
	b.WriteString(tui.StyleError.Render("  ✗  Analysis Failed"))
	b.WriteString("\n\n")
	b.WriteString(tui.StyleTextSecondary.Render("  " + err.Error()))
	return tui.StyleCard.Render(b.String())
}

func phaseLine(st phaseState) string {
	label := phaseLabel(st)
	bar := st.progress.View()
	if label == "" {
		return bar
	}
	return tui.StyleMuted.Render("  "+label) + " " + bar
}

// phaseLabel returns a pending / running / done status pill for a phase.
func phaseLabel(st phaseState) string {
	if st.progress.IsDone() {
		return tui.BadgeOK.Render("  done  ")
	}
	if st.running {
		return tui.BadgeInfo.Render("  running  ")
	}
	return tui.StyleMuted.Render("  pending  ")
}

func tickCmd() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func humanBytes(b int64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.1fGB", float64(b)/(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.1fMB", float64(b)/(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.1fKB", float64(b)/(1<<10))
	default:
		return fmt.Sprintf("%dB", b)
	}
}
