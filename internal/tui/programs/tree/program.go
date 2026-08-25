// Package tree is the BubbleTea program for `gmb tree`. It renders the
// pre-built file->symbol tree lines in a scrollable viewport with kind-colored
// tags.
package tree

import (
	"fmt"
	"io"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/tui"
	"github.com/Syamchand123/GlassMarble/internal/tui/components"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Config carries the already-built tree lines from cmd/tree.go. The program
// only styles and scrolls them; building stays in the command.
type Config struct {
	Lines []string
	Depth int
	In    io.Reader
	Out   io.Writer
}

type model struct {
	viewport viewport.Model
	help     components.HelpOverlay
	lines    []string
	depth    int
	width    int
	height   int
}

// Run launches the tree viewport program.
func Run(cfg Config) error {
	m := newModel(cfg)
	p := tea.NewProgram(m, tea.WithOutput(cfg.Out), tea.WithInput(cfg.In), tea.WithMouseCellMotion())
	_, err := p.Run()
	return err
}

func newModel(cfg Config) *model {
	m := &model{
		lines:  cfg.Lines,
		depth:  cfg.Depth,
		width:  80,
		height: 24,
		help:   components.NewHelpOverlay(tui.DefaultKeyMap()),
	}
	m.viewport = components.NewGMViewport(80, 20)
	m.viewport.SetContent(components.StyleViewportContent(strings.Join(colorize(cfg.Lines), "\n")))
	return m
}

func (m *model) Init() tea.Cmd { return nil }

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = maxInt(40, msg.Width)
		m.height = maxInt(10, msg.Height)
		m.viewport.Width = m.width - 4
		m.viewport.Height = maxInt(5, m.height-6)
		return m, nil
	case tea.KeyMsg:
		if m.help.Visible && msg.String() != "?" && msg.String() != "q" && msg.String() != "esc" {
			m.help.Visible = false
		}
		switch msg.String() {
		case "?":
			m.help.Toggle()
			return m, nil
		case "q", "ctrl+c":
			return m, tea.Quit
		case "g", "home":
			m.viewport.GotoTop()
			return m, nil
		case "G", "end":
			m.viewport.GotoBottom()
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

func (m *model) View() string {
	if m.help.Visible {
		return m.help.View()
	}
	w := maxInt(40, m.width)
	header := components.RenderHeader("Architecture Workspace Tree", fmt.Sprintf("depth: %d", m.depth), w)
	status := components.RenderStatusBar(
		components.JoinKeyHints(
			components.KeyHint("↑↓/jk", "scroll"),
			components.KeyHint("g/G", "top/bottom"),
			components.KeyHint("?", "help"),
			components.KeyHint("q", "quit"),
		),
		components.ScrollPosition(m.viewport),
		w,
	)
	return lipgloss.JoinVertical(lipgloss.Left, header, m.viewport.View(), status)
}

func colorize(lines []string) []string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		out = append(out, colorizeLine(line))
	}
	return out
}

func colorizeLine(line string) string {
	switch {
	case strings.HasPrefix(line, "=== "):
		return tui.StyleH2.Render(line)
	case strings.HasPrefix(line, "├── "):
		return tui.StylePrimaryText.Bold(true).Render(line)
	case strings.HasPrefix(line, "│   └── "):
		head := line
		branch := "│   └── "
		if idx := strings.Index(line, branch); idx >= 0 {
			head = line[idx+len(branch):]
		}
		return branch + colorizeSymbol(head)
	case strings.HasPrefix(line, "└── "):
		return tui.StyleMuted.Render(line)
	}
	return tui.StyleTextSecondary.Render(line)
}

func colorizeSymbol(sym string) string {
	var prim string
	if pIdx := strings.Index(sym, " <"); pIdx >= 0 && strings.HasSuffix(sym, ">") {
		prim = sym[pIdx+2 : len(sym)-1]
		sym = sym[:pIdx]
	}
	var kind string
	var name string
	if idx := strings.LastIndex(sym, " ["); idx >= 0 && strings.HasSuffix(sym, "]") {
		name = sym[:idx]
		kind = sym[idx+2 : len(sym)-1]
	} else {
		name = sym
	}
	out := tui.StyleTextSecondary.Render("  ")
	out += tui.StylePrimaryText.Render(name)
	if kind != "" {
		out += tui.StyleMuted.Render(" [" + kind + "]")
	}
	if prim != "" {
		out += "  " + primitiveBadge(prim)
	}
	return out
}

func primitiveBadge(p string) string {
	switch strings.ToUpper(p) {
	case "NETWORK_IO", "NET_IO":
		return tui.BadgeInfo.Render("  " + p + "  ")
	case "DATABASE", "DB":
		return tui.BadgeWarn.Render("  " + p + "  ")
	default:
		return tui.BadgeOK.Render("  " + p + "  ")
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
