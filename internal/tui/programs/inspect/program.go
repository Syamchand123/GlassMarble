// Package inspect is the BubbleTea program for `gmb inspect --list` and
// `gmb inspect --search`. It renders streamed nodes in a themed table; Enter
// opens a scrollable detail card built asynchronously from akg.QueryNode.
package inspect

import (
	"fmt"
	"io"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	producterrs "github.com/Syamchand123/GlassMarble/internal/product/errors"
	"github.com/Syamchand123/GlassMarble/internal/tui"
	"github.com/Syamchand123/GlassMarble/internal/tui/components"
	"github.com/Syamchand123/GlassMarble/internal/tui/views"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// NodeRow is one table row collected by cmd/inspect.go from the AKG stream.
type NodeRow struct {
	ID, Kind, Name, File string
	Line                 int
}

// Config carries the streamed rows into the program.
type Config struct {
	Title      string
	Rows       []NodeRow
	StorageDir string
	In         io.Reader
	Out        io.Writer
}

type viewState int

const (
	viewList viewState = iota
	viewLoading
	viewDetail
)

type detailMsg struct {
	detail string
}

type model struct {
	cfg      Config
	table    table.Model
	viewport viewport.Model
	help     components.HelpOverlay
	state    viewState
	detail   string
	width    int
	height   int
}

// Run launches the table program.
func Run(cfg Config) error {
	m := newModel(cfg)
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithOutput(cfg.Out), tea.WithInput(cfg.In), tea.WithMouseCellMotion())
	_, err := p.Run()
	return err
}

// RenderDetail renders a static node detail card for interactive `[node_id]`
// inspection (no event loop).
func RenderDetail(out io.Writer, storageDir, id string) error {
	node, outEdges, inEdges, err := akg.QueryNode(storageDir, id)
	if err != nil {
		return fmt.Errorf("failed to open AKG database: %w", err)
	}
	if node == nil {
		return producterrs.Tagged(fmt.Sprintf("node ID '%s' not found in AKG", id), producterrs.ErrEntryNotFound)
	}
	fmt.Fprintln(out, views.RenderInspectDetail(node, outEdges, inEdges))
	return nil
}

func newModel(cfg Config) *model {
	m := &model{
		cfg:      cfg,
		state:    viewList,
		width:    80,
		height:   24,
		viewport: components.NewGMViewport(80, 18),
		help:     components.NewHelpOverlay(tui.DefaultKeyMap()),
	}
	m.rebuildTable(80)
	return m
}

func (m *model) rebuildTable(w int) {
	kindWidth := 12
	lineWidth := 8
	nameWidth := 30
	fileWidth := 30
	if w > 100 {
		nameWidth = 40
		fileWidth = 42
	} else if w < 70 {
		fileWidth = maxInt(12, w-kindWidth-nameWidth-lineWidth-6)
	}

	columns := []table.Column{
		{Title: "KIND", Width: kindWidth},
		{Title: "NAME", Width: nameWidth},
		{Title: "FILE", Width: fileWidth},
		{Title: "LINE", Width: lineWidth},
	}
	rows := make([]table.Row, 0, len(m.cfg.Rows))
	for _, r := range m.cfg.Rows {
		rows = append(rows, table.Row{r.Kind, r.Name, r.File, fmt.Sprintf("%d", r.Line)})
	}
	t := components.NewGMTable(columns, rows)
	t.SetWidth(w - 4)
	t.SetHeight(maxInt(5, m.height-6))
	m.table = t
}

func (m *model) Init() tea.Cmd { return nil }

func (m *model) loadDetailCmd(id string) tea.Cmd {
	return func() tea.Msg {
		node, out, in, err := akg.QueryNode(m.cfg.StorageDir, id)
		var detail string
		switch {
		case err != nil:
			detail = "Error: " + err.Error()
		case node == nil:
			detail = fmt.Sprintf("node ID '%s' not found in AKG", id)
		default:
			detail = views.RenderInspectDetail(node, out, in)
		}
		return detailMsg{detail: detail}
	}
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = maxInt(40, msg.Width)
		m.height = maxInt(10, msg.Height)
		m.rebuildTable(m.width)
		m.viewport.Width = m.width - 4
		m.viewport.Height = maxInt(5, m.height-6)
		return m, nil
	case detailMsg:
		m.state = viewDetail
		m.detail = msg.detail
		m.viewport.SetContent(components.StyleViewportContent(m.detail))
		m.viewport.GotoTop()
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
		}
		switch m.state {
		case viewDetail:
			switch msg.String() {
			case "esc", "left", "backspace", "enter":
				m.state = viewList
				return m, nil
			case "g", "home":
				m.viewport.GotoTop()
				return m, nil
			case "G", "end":
				m.viewport.GotoBottom()
				return m, nil
			}
			var vpCmd tea.Cmd
			m.viewport, vpCmd = m.viewport.Update(msg)
			return m, vpCmd
		default:
			switch msg.String() {
			case "enter", "right":
				idx := m.table.Cursor()
				if idx >= 0 && idx < len(m.cfg.Rows) {
					m.state = viewLoading
					return m, m.loadDetailCmd(m.cfg.Rows[idx].ID)
				}
				return m, nil
			}
			var cmd tea.Cmd
			m.table, cmd = m.table.Update(msg)
			return m, cmd
		}
	}

	return m, nil
}

func (m *model) View() string {
	if m.help.Visible {
		return m.help.View()
	}
	w := maxInt(40, m.width)
	switch m.state {
	case viewLoading:
		header := components.RenderHeader("AKG Node Inspector", "Loading detail...", w)
		status := components.RenderStatusBar(
			components.JoinKeyHints(components.KeyHint("q", "quit")),
			"loading...",
			w,
		)
		return lipgloss.JoinVertical(lipgloss.Left, header, tui.StyleCard.Render("Fetching node and edge details..."), status)
	case viewDetail:
		header := components.RenderHeader("Node Detail", m.cfg.Title, w)
		status := components.RenderStatusBar(
			components.JoinKeyHints(
				components.KeyHint("esc/←", "back"),
				components.KeyHint("↑↓/jk", "scroll"),
				components.KeyHint("?", "help"),
				components.KeyHint("q", "quit"),
			),
			components.ScrollPosition(m.viewport),
			w,
		)
		return lipgloss.JoinVertical(lipgloss.Left, header, m.viewport.View(), status)
	default:
		header := components.RenderHeader("AKG Node Inspector", m.cfg.Title, w)
		status := components.RenderStatusBar(
			components.JoinKeyHints(
				components.KeyHint("↑↓/jk", "navigate"),
				components.KeyHint("enter", "detail"),
				components.KeyHint("?", "help"),
				components.KeyHint("q", "quit"),
			),
			fmt.Sprintf("%d nodes", len(m.cfg.Rows)),
			w,
		)
		return lipgloss.JoinVertical(lipgloss.Left, header, m.table.View(), status)
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
