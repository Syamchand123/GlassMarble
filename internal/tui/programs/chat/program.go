// Package chat renders the interactive "gmb ai chat" REPL: a scrollable
// history viewport over a multi-line input. AI messages stream into the
// viewport as tokens arrive, tool-call events render as styled rows, and the
// session is persisted through a callback supplied by cmd/ai.go. All business
// logic (agent loop, session persistence) stays in internal/; this package
// only holds display state.
package chat

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/ai_engine"
	"github.com/Syamchand123/GlassMarble/internal/ai_engine/agent"
	"github.com/Syamchand123/GlassMarble/internal/ai_engine/provider"
	"github.com/Syamchand123/GlassMarble/internal/ai_engine/session"
	"github.com/Syamchand123/GlassMarble/internal/tui"
	"github.com/Syamchand123/GlassMarble/internal/tui/components"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// StreamTokenMsg carries one streamed answer fragment.
type StreamTokenMsg struct{ Delta string }

// ToolCallMsg reports a tool invocation from the agent loop.
type ToolCallMsg struct{ Name, Args string }

// ToolResultMsg reports the outcome of a tool invocation.
type ToolResultMsg struct {
	Name  string
	OK    bool
	Bytes int
}

// TurnCompleteMsg is sent when the agent finishes one turn.
type TurnCompleteMsg struct{ Res *agent.Result }

type turnErrorMsg struct{ err error }

// entry is one rendered chat bubble (user, AI, or a system status line).
type entry struct {
	role   string // "user" | "ai" | "system"
	text   string
	events []string
	live   bool
	err    string
}

type model struct {
	program *tea.Program
	ctx     context.Context
	cancel  context.CancelFunc

	engine  *ai_engine.Engine
	sess    *session.Session
	sessDir string
	opts    ai_engine.AgentOptions
	apply   func(sess *session.Session, res *agent.Result, dir string)
	save    func(text string) (string, error)

	width  int
	height int

	history viewport.Model
	input   textarea.Model
	spinner components.GMSpinner

	entries    []entry
	busy       bool
	quitting   bool
	lastAnswer string
}

// Run launches the chat program. opts carries the base agent options
// (tool set); per-turn history is filled in by the program. apply persists a
// completed turn to the session file; save writes the last answer to an
// artifact and returns its path.
func Run(ctx context.Context, engine *ai_engine.Engine, sess *session.Session, sessDir string, opts ai_engine.AgentOptions, apply func(sess *session.Session, res *agent.Result, dir string), save func(text string) (string, error), in io.Reader, out io.Writer) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	ta := textarea.New()
	ta.Placeholder = "Ask the GlassMarble AI Architect… (Enter sends, Ctrl+C exits)"
	ta.ShowLineNumbers = false
	ta.KeyMap.InsertNewline = key.NewBinding(
		key.WithKeys("ctrl+j", "shift+enter"),
		key.WithHelp("ctrl+j", "newline"),
	)
	ta.SetHeight(3)
	ta.Focus()

	m := &model{
		ctx:     ctx,
		cancel:  cancel,
		engine:  engine,
		sess:    sess,
		sessDir: sessDir,
		opts:    opts,
		apply:   apply,
		save:    save,
		width:   80,
		height:  24,
		history: components.NewGMViewport(78, 16),
		input:   ta,
		spinner: components.NewGMSpinner("Thinking…"),
	}
	m.restoreHistory()

	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion(), tea.WithOutput(out), tea.WithInput(in))
	m.program = p
	final, err := p.Run()
	if err != nil {
		return err
	}
	m = final.(*model)

	if sess.Turns > 0 {
		fmt.Fprintf(out, "Session %s: %d turns, %d messages, %d tokens, cost %s (resume with `gmb ai chat --session %s`)\n",
			sess.ID, sess.Turns, len(sess.Messages), sess.Usage.TotalTokens,
			formatCost(sess.CostUSD, sess.Usage.TotalTokens > 0), sess.ID)
	}
	return nil
}

func (m *model) restoreHistory() {
	for _, msg := range m.sess.Messages {
		switch msg.Role {
		case provider.RoleUser:
			m.entries = append(m.entries, entry{role: "user", text: msg.Content})
		case provider.RoleAssistant:
			if msg.Content != "" {
				m.entries = append(m.entries, entry{role: "ai", text: msg.Content})
			}
		}
	}
	m.lastAnswer = lastAssistantText(m.sess.Messages)
	m.refreshHistory()
}

func (m *model) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick(), m.input.Focus())
}

func (m *model) submit() (tea.Model, tea.Cmd) {
	text := strings.TrimSpace(m.input.Value())
	if text == "" {
		return m, nil
	}
	m.input.Reset()
	m.sess.Trim(m.engine.Config.MaxSessionMessages)

	m.entries = append(m.entries, entry{role: "user", text: text})
	m.entries = append(m.entries, entry{role: "ai", live: true})
	m.busy = true
	m.refreshHistory()

	return m, m.ask(m.sess.Messages, text)
}

func (m *model) ask(history []provider.Message, query string) tea.Cmd {
	return func() tea.Msg {
		opts := m.opts
		opts.History = history
		opts.OnEvent = func(ev agent.Event) {
			switch ev.Type {
			case "tool_call":
				m.program.Send(ToolCallMsg{Name: ev.ToolName, Args: strings.TrimSpace(ev.ToolArgs)})
			case "tool_result":
				m.program.Send(ToolResultMsg{Name: ev.ToolName, OK: ev.OK, Bytes: ev.ResultBytes})
			}
		}
		opts.OnStream = func(delta string) {
			m.program.Send(StreamTokenMsg{Delta: delta})
		}
		res, err := m.engine.AskAgent(m.ctx, query, opts)
		if err != nil {
			return turnErrorMsg{err: err}
		}
		return TurnCompleteMsg{Res: res}
	}
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.input.SetWidth(msg.Width - 2)
		m.refreshHistory()
		return m, nil
	case tea.KeyMsg:
		switch {
		case msg.Type == tea.KeyCtrlC:
			m.quitting = true
			if m.cancel != nil {
				m.cancel()
			}
			return m, tea.Quit
		case msg.Type == tea.KeyEnter && !m.busy:
			return m.submit()
		case msg.Type == tea.KeyCtrlL:
			m.entries = nil
			m.refreshHistory()
			return m, nil
		case msg.Type == tea.KeyCtrlN:
			m.newSession()
			return m, nil
		case msg.Type == tea.KeyCtrlS:
			return m.saveLast()
		}
		var inputCmd, vpCmd tea.Cmd
		m.input, inputCmd = m.input.Update(msg)
		m.history, vpCmd = m.history.Update(msg)
		return m, tea.Batch(inputCmd, vpCmd)
	case StreamTokenMsg:
		if m.busy {
			m.appendToLiveText(msg.Delta)
			m.refreshHistory()
		}
		return m, nil
	case ToolCallMsg:
		if m.busy {
			m.resetLiveText()
			m.appendToLiveEvent(tui.StyleAccent.Render("→ " + msg.Name + "(" + msg.Args + ")"))
			m.refreshHistory()
		}
		return m, nil
	case ToolResultMsg:
		if m.busy {
			status := "ok"
			if !msg.OK {
				status = "error"
			}
			line := "← " + msg.Name + ": " + status + fmt.Sprintf(" (%d bytes)", msg.Bytes)
			if msg.OK {
				m.appendToLiveEvent(tui.StyleOK.Render(line))
			} else {
				m.appendToLiveEvent(tui.StyleError.Render(line))
			}
			m.refreshHistory()
		}
		return m, nil
	case TurnCompleteMsg:
		if m.busy {
			res := msg.Res
			m.finalizeLive(res.Text)
			m.busy = false
			m.lastAnswer = res.Text
			if m.apply != nil {
				m.apply(m.sess, res, m.sessDir)
			}
			m.refreshHistory()
		}
		return m, nil
	case turnErrorMsg:
		if m.busy {
			m.setLiveError(msg.err)
			m.busy = false
			m.refreshHistory()
		}
		return m, nil
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *model) newSession() {
	m.sess = session.Create(m.sessDir, m.engine.Config.Provider, m.engine.Config.Model)
	_ = m.sess.Save(m.sessDir)
	m.entries = nil
	m.lastAnswer = ""
	m.busy = false
	m.input.Reset()
	m.refreshHistory()
}

func (m *model) saveLast() (tea.Model, tea.Cmd) {
	if m.lastAnswer == "" || m.save == nil {
		return m, nil
	}
	path, err := m.save(m.lastAnswer)
	if err != nil {
		m.entries = append(m.entries, entry{role: "system", text: "Save failed: " + err.Error()})
	} else {
		m.entries = append(m.entries, entry{role: "system", text: "Saved last answer to " + path})
	}
	m.refreshHistory()
	return m, nil
}

func (m *model) lastAI() *entry {
	if len(m.entries) == 0 || m.entries[len(m.entries)-1].role != "ai" {
		m.entries = append(m.entries, entry{role: "ai"})
	}
	return &m.entries[len(m.entries)-1]
}

func (m *model) appendToLiveText(delta string) {
	m.lastAI().text += delta
}

func (m *model) resetLiveText() {
	m.lastAI().text = ""
}

func (m *model) appendToLiveEvent(styled string) {
	e := m.lastAI()
	e.events = append(e.events, styled)
}

func (m *model) finalizeLive(text string) {
	e := m.lastAI()
	e.live = false
	if text != "" {
		e.text = text
	}
}

func (m *model) setLiveError(err error) {
	e := m.lastAI()
	e.live = false
	e.err = err.Error()
}

func (m *model) refreshHistory() {
	var b strings.Builder
	for _, e := range m.entries {
		b.WriteString(m.renderEntry(e))
		b.WriteString("\n\n")
	}
	m.history.SetContent(b.String())
	m.history.GotoBottom()
}

func (m *model) renderEntry(e entry) string {
	switch e.role {
	case "system":
		return tui.StyleMuted.Render(e.text)
	case "user":
		box := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(tui.ColorPrimary).
			Padding(0, 1).
			Width(m.width - 8).
			Render(e.text)
		return lipgloss.JoinVertical(lipgloss.Left, tui.StyleLabel.Render("You"), box)
	default:
		var body strings.Builder
		for _, ev := range e.events {
			body.WriteString(ev)
			body.WriteString("\n")
		}
		if e.text != "" {
			body.WriteString(e.text)
		}
		if e.err != "" {
			if body.Len() > 0 {
				body.WriteString("\n\n")
			}
			body.WriteString(tui.StyleError.Render("Error: " + e.err))
		}
		text := body.String()
		if e.live && text == "" {
			text = m.spinner.View()
		}
		if text == "" {
			text = "…"
		}
		box := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(tui.ColorAccent).
			Padding(0, 1).
			Width(m.width - 8).
			Render(text)
		return lipgloss.JoinVertical(lipgloss.Left, tui.StyleLabel.Render("GlassMarble AI"), box)
	}
}

func (m *model) View() string {
	contentHeight := m.height - 8
	if contentHeight < 4 {
		contentHeight = 4
	}
	m.history.Width = m.width - 2
	m.history.Height = contentHeight

	header := components.RenderHeader("AI Architect", m.sess.Provider+"/"+m.sess.Model, m.width)
	status := components.RenderStatusBar(
		components.JoinKeyHints(
			components.KeyHint("Enter", "send"),
			components.KeyHint("Ctrl+C", "exit"),
			components.KeyHint("Ctrl+L", "clear"),
			components.KeyHint("Ctrl+N", "new"),
			components.KeyHint("Ctrl+S", "save"),
		),
		m.statusRight(),
		m.width,
	)
	return lipgloss.JoinVertical(lipgloss.Left, header, m.history.View(), m.input.View(), status)
}

func (m *model) statusRight() string {
	msgCount := 0
	for _, e := range m.entries {
		if e.role == "user" || e.role == "ai" {
			msgCount++
		}
	}
	right := fmt.Sprintf("Session %s │ %d messages │ %d tokens", shortID(m.sess.ID), msgCount, m.sess.Usage.TotalTokens)
	if m.sess.CostUSD > 0 {
		right += fmt.Sprintf(" │ cost $%.4f", m.sess.CostUSD)
	}
	return right
}

func shortID(id string) string {
	if len(id) > 19 {
		return id[:19]
	}
	return id
}

func lastAssistantText(messages []provider.Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == provider.RoleAssistant && messages[i].Content != "" {
			return messages[i].Content
		}
	}
	return ""
}

func formatCost(usd float64, estimated bool) string {
	if usd <= 0 {
		return "n/a"
	}
	return fmt.Sprintf("$%.4f", usd)
}
