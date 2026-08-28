// Package ai_query renders the one-shot "gmb ai <question>" flow as a
// BubbleTea program: a spinner while the agent works (non-streaming) or a live
// token viewport with tool-call event pills (streaming), then the final answer
// in a scrollable viewport. The agent runs on the program's event loop with
// program-local callbacks that forward progress as tea messages.
package ai_query

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/ai_engine"
	"github.com/Syamchand123/GlassMarble/internal/ai_engine/agent"
	"github.com/Syamchand123/GlassMarble/internal/tui"
	"github.com/Syamchand123/GlassMarble/internal/tui/components"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type toolCallMsg struct{ name, args string }
type toolResultMsg struct {
	name  string
	ok    bool
	bytes int
}
type streamDeltaMsg struct{ delta string }
type turnDoneMsg struct{ res *agent.Result }
type agentErrMsg struct{ err error }

type model struct {
	program *tea.Program
	ctx     context.Context
	cancel  context.CancelFunc

	engine    *ai_engine.Engine
	question  string
	opts      ai_engine.AgentOptions
	streaming bool
	verbose   bool
	save      func(text string) error

	width   int
	height  int
	vp      viewport.Model
	spinner components.GMSpinner

	events   []string
	answer   strings.Builder
	done     bool
	res      *agent.Result
	err      error
	quitting bool
}

// Run executes the one-shot query. save, when non-nil, persists the final
// answer after the program exits (cmd/ai.go closes over --save and rootDir).
func Run(ctx context.Context, engine *ai_engine.Engine, question string, opts ai_engine.AgentOptions, streaming, verbose bool, in io.Reader, out io.Writer, save func(text string) error) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	m := &model{
		ctx:       ctx,
		cancel:    cancel,
		engine:    engine,
		question:  question,
		opts:      opts,
		streaming: streaming,
		verbose:   verbose,
		save:      save,
		width:     80,
		height:    24,
		vp:        components.NewGMViewport(78, 18),
		spinner:   components.NewGMSpinner("Consulting " + engine.Config.Model + " (" + engine.Config.Provider + ")..."),
	}

	p := tea.NewProgram(m, tea.WithOutput(out), tea.WithInput(in))
	m.program = p
	final, err := p.Run()
	if err != nil {
		return err
	}
	m = final.(*model)
	if m.save != nil && m.done && m.err == nil {
		text := m.answer.String()
		if text == "" && m.res != nil {
			text = m.res.Text
		}
		if text != "" {
			return m.save(text)
		}
	}
	return nil
}

func (m *model) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick(), m.runAgent)
}

func (m *model) runAgent() tea.Msg {
	opts := m.opts
	opts.OnEvent = func(ev agent.Event) {
		switch ev.Type {
		case "tool_call":
			m.program.Send(toolCallMsg{name: ev.ToolName, args: ev.ToolArgs})
		case "tool_result":
			m.program.Send(toolResultMsg{name: ev.ToolName, ok: ev.OK, bytes: ev.ResultBytes})
		}
	}
	opts.OnStream = func(delta string) {
		m.program.Send(streamDeltaMsg{delta: delta})
	}
	res, err := m.engine.AskAgent(m.ctx, m.question, opts)
	if err != nil {
		return agentErrMsg{err: err}
	}
	return turnDoneMsg{res: res}
}

func (m *model) refreshVP() {
	// Wrap content to viewport inner width to avoid right-side truncation.
	inner := m.vp.Width - 4
	if inner < 20 {
		inner = 20
	}
	wrapped := wrapToWidth(m.content(), inner)
	m.vp.SetContent(components.StyleViewportContent(wrapped))
	m.vp.GotoBottom()
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = maxInt(40, msg.Width)
		m.height = maxInt(10, msg.Height)
		m.vp.Width = maxInt(20, msg.Width-4)
		m.vp.Height = maxInt(5, msg.Height-6)
		m.refreshVP()
		return m, nil
	case tea.MouseMsg:
		var cmd tea.Cmd
		m.vp, cmd = m.vp.Update(msg)
		return m, cmd
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "esc", "ctrl+c":
			m.quitting = true
			if m.cancel != nil {
				m.cancel()
			}
			return m, tea.Quit
		case "pgup", "b":
			m.vp.PageUp()
			return m, nil
		case "pgdown", "f", " ":
			m.vp.PageDown()
			return m, nil
		case "ctrl+u":
			m.vp.HalfViewUp()
			return m, nil
		case "ctrl+d":
			m.vp.HalfViewDown()
			return m, nil
		case "home", "g":
			m.vp.GotoTop()
			return m, nil
		case "end", "G":
			m.vp.GotoBottom()
			return m, nil
		}
		var cmd tea.Cmd
		m.vp, cmd = m.vp.Update(msg)
		return m, cmd
	case toolCallMsg:
		args := strings.TrimSpace(msg.args)
		if len(args) > 120 {
			args = args[:120] + "…"
		}
		label := " tool " + msg.name + " "
		if args != "" {
			label += "(" + args + ") "
		}
		m.events = append(m.events, tui.StyleAccent.Render("→")+" "+tui.BadgeInfo.Render(label))
		m.answer.Reset()
		m.refreshVP()
		return m, nil
	case toolResultMsg:
		status := "ok"
		if !msg.ok {
			status = "error"
		}
		if msg.ok {
			m.events = append(m.events, tui.StyleOK.Render("←")+" "+tui.BadgeOK.Render(" "+status+" ")+" "+tui.StyleMuted.Render(msg.name+fmt.Sprintf(" (%d bytes)", msg.bytes)))
		} else {
			m.events = append(m.events, tui.StyleError.Render("←")+" "+tui.BadgeError.Render(" "+status+" ")+" "+tui.StyleMuted.Render(msg.name))
		}
		m.refreshVP()
		return m, nil
	case streamDeltaMsg:
		m.answer.WriteString(msg.delta)
		m.refreshVP()
		return m, nil
	case turnDoneMsg:
		m.done = true
		m.res = msg.res
		if m.answer.Len() == 0 {
			m.answer.WriteString(msg.res.Text)
		}
		m.refreshVP()
		return m, nil
	case agentErrMsg:
		m.done = true
		m.err = msg.err
		m.refreshVP()
		return m, nil
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *model) content() string {
	var b strings.Builder
	for _, e := range m.events {
		b.WriteString(e)
		b.WriteString("\n")
	}
	if len(m.events) > 0 {
		b.WriteString("\n")
	}
	b.WriteString(tui.Divider("Answer", m.width) + "\n\n")
	b.WriteString(formatMarkdown(m.answer.String()))

	if m.err != nil {
		b.WriteString("\n\n" + tui.StyleError.Render("Error: "+m.err.Error()))
	} else if m.done && m.res != nil {
		if m.res.StoppedReason != "" {
			b.WriteString("\n\n" + tui.StyleWarningText.Render("Note: "+reasonLabel(m.res.StoppedReason)))
		}
		if m.verbose {
			b.WriteString("\n\n" + tui.StyleMuted.Render(verboseLine(m.res)))
		}
		b.WriteString("\n\n" + m.tokenCostFooter())
	}
	return b.String()
}

// tokenCostFooter renders a small cost/turn summary line when the answer is
// complete. Token counts are intentionally omitted from the UI to avoid
// confusion when providers do not report usage (see Bug 3).
func (m *model) tokenCostFooter() string {
	if m.res == nil {
		return ""
	}
	cost := "n/a"
	if m.res.CostUSD > 0 {
		cost = fmt.Sprintf("$%.4f", m.res.CostUSD)
	}
	line := fmt.Sprintf("%s · %d turns · %d tool-calls",
		cost, m.res.Turns, len(m.res.ToolCalls))
	return tui.Divider("", m.width) + "\n" + tui.StyleMuted.Render(line)
}

// formatMarkdown applies lightweight markdown formatting to the agent's answer:
// headings, code fences, inline code, bold, and lists.
func formatMarkdown(text string) string {
	var out strings.Builder
	inFence := false
	lines := strings.Split(text, "\n")
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "```"):
			inFence = !inFence
			out.WriteString(tui.StyleAccent.Render(trimmed))
		case inFence:
			out.WriteString(tui.StyleCode.Render(" " + line + " "))
		case strings.HasPrefix(trimmed, "###"):
			out.WriteString(tui.StyleAccent.Bold(true).Render(strings.TrimPrefix(trimmed, "###")))
		case strings.HasPrefix(trimmed, "##"):
			out.WriteString(tui.StyleH2.Render(strings.TrimPrefix(trimmed, "##")))
		case strings.HasPrefix(trimmed, "#"):
			out.WriteString(tui.StyleH1.Render(strings.TrimPrefix(trimmed, "#")))
		case strings.HasPrefix(trimmed, "> "):
			out.WriteString(tui.StyleMuted.Render("▎ " + strings.TrimPrefix(trimmed, "> ")))
		case strings.HasPrefix(trimmed, "- "), strings.HasPrefix(trimmed, "* "):
			out.WriteString(tui.StyleAccent.Render("• ") + strings.TrimPrefix(strings.TrimPrefix(trimmed, "- "), "* "))
		case strings.HasPrefix(trimmed, "**"):
			out.WriteString(tui.StyleTextSecondary.Bold(true).Render(strings.Trim(trimmed, "*")))
		default:
			out.WriteString(highlightInline(line))
		}
		out.WriteString("\n")
	}
	return strings.TrimSuffix(out.String(), "\n")
}

// highlightInline colors `inline code` spans and keeps the rest of the line as
// plain text.
func highlightInline(line string) string {
	var out strings.Builder
	for {
		start := strings.Index(line, "`")
		if start < 0 {
			out.WriteString(line)
			break
		}
		out.WriteString(line[:start])
		rest := line[start+1:]
		end := strings.Index(rest, "`")
		if end < 0 {
			out.WriteString("`" + rest)
			break
		}
		out.WriteString(tui.StyleCode.Render(rest[:end]))
		line = rest[end+1:]
	}
	return out.String()
}

func (m *model) View() string {
	var body string
	if !m.streaming && !m.done {
		body = m.spinner.View()
	} else {
		body = m.vp.View()
	}
	header := components.RenderHeader("AI Query", m.engine.Config.Model+"/"+m.engine.Config.Provider, m.width)
	status := components.RenderStatusBar(
		components.JoinKeyHints(
			components.KeyHint("q", "quit"),
			components.KeyHint("↑↓", "scroll"),
		),
		m.statusRight(),
		m.width,
	)
	return lipgloss.JoinVertical(lipgloss.Left, header, "", body, "", status)
}

func (m *model) statusRight() string {
	switch {
	case m.done && m.res != nil:
		return fmt.Sprintf("turns=%d tool-calls=%d", m.res.Turns, len(m.res.ToolCalls))
	case m.streaming:
		return "streaming…"
	default:
		return "working…"
	}
}

func reasonLabel(reason string) string {
	switch reason {
	case "turn_limit":
		return "stopped after max tool rounds"
	case "token_budget":
		return "token budget exceeded"
	case "cost_budget":
		return "estimated cost budget exceeded"
	}
	return reason
}

func verboseLine(res *agent.Result) string {
	cost := "n/a"
	if res.CostUSD > 0 {
		cost = fmt.Sprintf("$%.4f", res.CostUSD)
	}
	if res.Usage.TotalTokens > 0 {
		return fmt.Sprintf("Tokens: prompt=%d completion=%d total=%d | cost=%s | turns=%d tool-calls=%d",
			res.Usage.PromptTokens, res.Usage.CompletionTokens, res.Usage.TotalTokens,
			cost, res.Turns, len(res.ToolCalls))
	}
	return fmt.Sprintf("cost=%s | turns=%d tool-calls=%d", cost, res.Turns, len(res.ToolCalls))
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// wrapToWidth word-wraps s to display width w (via lipgloss.Width).
// It preserves existing line breaks and hard-breaks over-long tokens.
// ANSI sequences are ignored via lipgloss.Width.
func wrapToWidth(s string, w int) string {
	if w <= 0 {
		return s
	}
	var out []string
	for _, para := range strings.Split(s, "\n") {
		if para == "" {
			out = append(out, "")
			continue
		}
		if lipgloss.Width(para) <= w {
			out = append(out, para)
			continue
		}
		var cur string
		curW := 0
		flush := func() {
			if cur != "" {
				out = append(out, cur)
				cur = ""
				curW = 0
			}
		}
		for _, word := range strings.Fields(para) {
			ww := lipgloss.Width(word)
			if ww > w {
				if cur != "" {
					flush()
				}
				runes := []rune(word)
				seg := ""
				segW := 0
				for _, r := range runes {
					rw := lipgloss.Width(string(r))
					if segW+rw > w {
						out = append(out, seg)
						seg = ""
						segW = 0
					}
					seg += string(r)
					segW += rw
				}
				if seg != "" {
					cur = seg
					curW = segW
				}
				continue
			}
			if cur == "" {
				cur = word
				curW = ww
			} else if curW+1+ww <= w {
				cur += " " + word
				curW += 1 + ww
			} else {
				flush()
				cur = word
				curW = ww
			}
		}
		flush()
	}
	return strings.Join(out, "\n")
}

