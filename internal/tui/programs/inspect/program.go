// Package inspect is the BubbleTea program for `gmb inspect --list` and
// `gmb inspect --search`. It renders streamed nodes in a themed table; Enter
// opens a detail card built from akg.QueryNode.
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
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	width  = 80
	height = 24
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
	viewDetail
)

type model struct {
	cfg    Config
	table  table.Model
	state  viewState
	detail string
}

// Run launches the table program.
func Run(cfg Config) error {
	m := newModel(cfg)
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithOutput(cfg.Out), tea.WithInput(cfg.In))
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
	columns := []table.Column{
		{Title: "KIND", Width: 12},
		{Title: "NAME", Width: 40},
		{Title: "FILE", Width: 42},
		{Title: "LINE", Width: 8},
	}
	rows := make([]table.Row, 0, len(cfg.Rows))
	for _, r := range cfg.Rows {
		rows = append(rows, table.Row{r.Kind, r.Name, r.File, fmt.Sprintf("%d", r.Line)})
	}
	t := components.NewGMTable(columns, rows)
	t.SetWidth(width)
	t.SetHeight(height - 4)
	return &model{cfg: cfg, table: t, state: viewList}
}

func (m *model) Init() tea.Cmd { return nil }

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if ok {
		switch key.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		}
	}
	switch m.state {
	case viewDetail:
		if ok {
			switch key.String() {
			case "esc", "left", "backspace":
				m.state = viewList
				return m, nil
			}
		}
		return m, nil
	default:
		if ok {
			switch key.String() {
			case "enter":
				idx := m.table.Cursor()
				if idx >= 0 && idx < len(m.cfg.Rows) {
					m.loadDetail(m.cfg.Rows[idx].ID)
				}
				return m, nil
			}
		}
		var cmd tea.Cmd
		m.table, cmd = m.table.Update(msg)
		return m, cmd
	}
}

func (m *model) loadDetail(id string) {
	node, out, in, err := akg.QueryNode(m.cfg.StorageDir, id)
	switch {
	case err != nil:
		m.detail = "Error: " + err.Error()
	case node == nil:
		m.detail = fmt.Sprintf("node ID '%s' not found in AKG", id)
	default:
		m.detail = views.RenderInspectDetail(node, out, in)
	}
	m.state = viewDetail
}

func (m *model) View() string {
	if m.state == viewDetail {
		header := components.RenderHeader("Node Detail", m.cfg.Title, width)
		status := components.RenderStatusBar(
			components.JoinKeyHints(
				components.KeyHint("←", "back"),
				components.KeyHint("q", "quit"),
			),
			"",
			width,
		)
		return lipgloss.JoinVertical(lipgloss.Left, header, tui.Indent(m.detail, 2), status)
	}
	header := components.RenderHeader("AKG Node Inspector", m.cfg.Title, width)
	status := components.RenderStatusBar(
		components.JoinKeyHints(
			components.KeyHint("↑↓", "navigate"),
			components.KeyHint("enter", "detail"),
			components.KeyHint("q", "quit"),
		),
		fmt.Sprintf("%d nodes", len(m.cfg.Rows)),
		width,
	)
	return lipgloss.JoinVertical(lipgloss.Left, header, m.table.View(), status)
}
