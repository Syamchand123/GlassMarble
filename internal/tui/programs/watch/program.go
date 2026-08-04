// Package watch is the BubbleTea program for `gmb watch`. Business logic
// (fsnotify helpers, git fingerprint, the analysis pipeline) lives in cmd/ and
// is injected here as callbacks, so this package never imports cmd.
package watch

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/tui"
	"github.com/Syamchand123/GlassMarble/internal/tui/components"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/fsnotify/fsnotify"
)

// Options carries the watch flags into the program.
type Options struct {
	TargetDir  string
	CommitHash string
	Interval   time.Duration
}

// RunAnalysisFn executes the analysis pipeline, reporting stage boundaries.
type RunAnalysisFn func(progress func(stage int, name string, current, total int)) error

// RegisterFn registers fsnotify watches for a directory tree.
type RegisterFn func(w *fsnotify.Watcher) error

// EventRelevantFn filters fsnotify events to source-like paths.
type EventRelevantFn func(w *fsnotify.Watcher, ev fsnotify.Event) bool

// FingerprintFn summarizes the git working-tree state for change detection.
type FingerprintFn func() string

type model struct {
	opts        Options
	fingerprint FingerprintFn
	runAnalysis RunAnalysisFn
	p           *tea.Program

	spinner         components.GMSpinner
	viewport        viewport.Model
	log             []string
	analyzing       bool
	lastFingerprint string
	debounceToken   int
	currentStage    string
	stageCurrent    int
	stageTotal      int
	started         time.Time
	width           int
	height          int
}

type fsEventMsg struct{}

type fsErrMsg struct {
	err error
}

type debounceMsg struct {
	token int
}

type fingerprintMsg struct {
	fp string
}

type progressMsg struct {
	stage   int
	name    string
	current int
	total   int
}

type analyzeResultMsg struct {
	err     error
	initial bool
}

type startAnalysisMsg struct{}

type tickMsg time.Time

// RunWatch runs the interactive watcher until Ctrl+C is pressed.
func RunWatch(opts Options, register RegisterFn, relevant EventRelevantFn, fingerprint FingerprintFn, runAnalysis RunAnalysisFn, in io.Reader, out io.Writer) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("failed to start filesystem watcher: %w", err)
	}
	defer watcher.Close()

	if err := register(watcher); err != nil {
		return fmt.Errorf("failed to register watches: %w", err)
	}

	m := newModel(opts, fingerprint, runAnalysis)
	p := tea.NewProgram(m, tea.WithOutput(out), tea.WithInput(in))
	m.p = p

	go func() {
		for {
			select {
			case ev, ok := <-watcher.Events:
				if !ok {
					return
				}
				if relevant(watcher, ev) {
					p.Send(fsEventMsg{})
				}
			case werr, ok := <-watcher.Errors:
				if !ok {
					return
				}
				p.Send(fsErrMsg{err: werr})
			}
		}
	}()

	if _, err := p.Run(); err != nil {
		return err
	}
	fmt.Fprintln(out, "Watcher stopped.")
	return nil
}

func newModel(opts Options, fingerprint FingerprintFn, runAnalysis RunAnalysisFn) *model {
	m := &model{
		opts:        opts,
		fingerprint: fingerprint,
		runAnalysis: runAnalysis,
		spinner:     components.NewGMSpinner("Analyzing..."),
		viewport:    components.NewGMViewport(60, 12),
		started:     time.Now(),
	}
	m.lastFingerprint = m.fingerprint()
	return m
}

func (m *model) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick(), tickCmd(), initialAnalysisCmd())
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.viewport.Width = maxInt(20, msg.Width-4)
		m.viewport.Height = maxInt(3, msg.Height-14)
		return m, nil
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
	case tickMsg:
		return m, tickCmd()
	case startAnalysisMsg:
		m.analyzing = true
		m.addLog(tui.StyleMuted.Render(ts()) + " " + tui.StyleAccent.Render("Initial analysis..."))
		return m, m.startAnalysisCmd(true)
	case fsEventMsg:
		m.debounceToken++
		tok := m.debounceToken
		interval := m.opts.Interval
		if interval <= 0 {
			interval = 500 * time.Millisecond
		}
		return m, tea.Tick(interval, func(t time.Time) tea.Msg {
			return debounceMsg{token: tok}
		})
	case debounceMsg:
		if msg.token != m.debounceToken || m.analyzing {
			return m, nil
		}
		return m, fingerprintCmd(m)
	case fingerprintMsg:
		if msg.fp == m.lastFingerprint {
			m.addLog(tui.StyleMuted.Render(ts()) + " " + tui.StyleMuted.Render("Checked — no changes"))
			return m, nil
		}
		m.lastFingerprint = msg.fp
		m.analyzing = true
		m.addLog(tui.StyleMuted.Render(ts()) + " " + tui.StyleAccent.Render("Repository changes detected — running analysis..."))
		return m, m.startAnalysisCmd(false)
	case progressMsg:
		m.currentStage = msg.name
		m.stageCurrent = msg.current
		m.stageTotal = msg.total
		return m, nil
	case analyzeResultMsg:
		m.analyzing = false
		switch {
		case msg.err != nil:
			m.addLog(tui.StyleMuted.Render(ts()) + " " + tui.StyleError.Render("✗ Analysis failed: " + msg.err.Error()))
		case msg.initial:
			m.addLog(tui.StyleMuted.Render(ts()) + " " + tui.StyleOK.Render("✓ Initial analysis complete"))
			m.addLog(tui.StyleMuted.Render(ts()) + " " + tui.StyleMuted.Render("Watching for changes..."))
		default:
			m.addLog(tui.StyleMuted.Render(ts()) + " " + tui.StyleOK.Render("✓ Analysis complete"))
		}
		return m, nil
	case fsErrMsg:
		m.addLog(tui.StyleMuted.Render(ts()) + " " + tui.StyleWarningText.Render("watcher error: " + msg.err.Error()))
		return m, nil
	}
	var cmd tea.Cmd
	m.spinner, cmd = m.spinner.Update(msg)
	return m, cmd
}

func (m *model) View() string {
	width := m.width
	if width <= 0 {
		width = 80
	}
	var b strings.Builder
	b.WriteString(components.RenderHeader("watch", "Ctrl+C to stop", width))
	b.WriteString("\n\n")
	b.WriteString(tui.KV("Watching", m.opts.TargetDir))
	b.WriteString("\n")
	b.WriteString(tui.KV("Interval", m.opts.Interval.String()))

	status := tui.StyleMuted.Render(m.spinner.View() + " Idle — waiting for changes")
	if m.analyzing {
		s := m.spinner.View()
		if m.currentStage != "" {
			s += " — " + m.currentStage
			if m.stageTotal > 0 {
				s += fmt.Sprintf(" %d/%d", m.stageCurrent, m.stageTotal)
			}
		}
		status = tui.StyleAccent.Render(s)
	}
	b.WriteString("\n")
	b.WriteString(tui.KV("Status", status))
	b.WriteString("\n\n")
	b.WriteString(tui.Divider("Activity Log", maxInt(20, width-4)))
	b.WriteString("\n")
	b.WriteString(m.viewport.View())
	b.WriteString("\n")
	b.WriteString(components.RenderStatusBar(
		components.JoinKeyHints(components.KeyHint("Ctrl+C", "stop")),
		"Running for: "+formatDuration(time.Since(m.started)),
		width,
	))
	return tui.StyleCard.Render(strings.TrimRight(b.String(), "\n"))
}

func (m *model) addLog(line string) {
	m.log = append(m.log, line)
	m.viewport.SetContent(strings.Join(m.log, "\n"))
	m.viewport.GotoBottom()
}

func (m *model) startAnalysisCmd(initial bool) tea.Cmd {
	return func() tea.Msg {
		go func() {
			err := m.runAnalysis(func(stage int, name string, current, total int) {
				m.p.Send(progressMsg{stage: stage, name: name, current: current, total: total})
			})
			m.p.Send(analyzeResultMsg{err: err, initial: initial})
		}()
		return nil
	}
}

func initialAnalysisCmd() tea.Cmd {
	return func() tea.Msg { return startAnalysisMsg{} }
}

func fingerprintCmd(m *model) tea.Cmd {
	return func() tea.Msg {
		return fingerprintMsg{fp: m.fingerprint()}
	}
}

func tickCmd() tea.Cmd {
	return tea.Tick(200*time.Millisecond, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func ts() string {
	return time.Now().Format("15:04:05")
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	return fmt.Sprintf("%dm %ds", int(d.Minutes()), int(d.Seconds())%60)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
