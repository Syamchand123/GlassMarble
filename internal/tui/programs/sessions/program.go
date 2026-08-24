// Package sessions renders the "gmb ai sessions" interactive table. The
// delete confirmation runs as a top-level Huh form outside the table program
// (nested BubbleTea renderers do not compose cleanly), so the table program
// quits, the form runs, and the table relaunches with the refreshed rows.
package sessions

import (
	"fmt"
	"io"
	"sort"

	"github.com/Syamchand123/GlassMarble/internal/ai_engine/session"
	"github.com/Syamchand123/GlassMarble/internal/tui"
	"github.com/Syamchand123/GlassMarble/internal/tui/components"
	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
)

type action int

const (
	actionQuit action = iota
	actionDelete
	actionResume
)

// sortCol selects the sort column; sortDesc toggles the direction.
type sortCol int

const (
	sortID sortCol = iota
	sortUpdated
	sortProvider
)

type model struct {
	list     []session.Summary
	tbl      table.Model
	count    int
	width    int
	height   int
	action   action
	selected string
	sortCol  sortCol
	sortDesc bool
	help     components.HelpOverlay
}

// Run shows the session table and handles navigation, delete confirmation,
// and resume hints. onDelete removes a session by id.
func Run(dir string, in io.Reader, out io.Writer, onDelete func(id string) error) error {
	for {
		list, err := session.List(dir)
		if err != nil {
			return err
		}
		if len(list) == 0 {
			fmt.Fprintln(out, "No saved sessions. Start one with `gmb ai chat`.")
			return nil
		}

		m := newModel(list)
		p := tea.NewProgram(m, tea.WithOutput(out), tea.WithInput(in), tea.WithAltScreen(), tea.WithMouseCellMotion())
		final, err := p.Run()
		if err != nil {
			return err
		}
		m = final.(model)

		switch m.action {
		case actionDelete:
			confirmed, err := confirmDelete(m.selected, in, out)
			if err != nil {
				return err
			}
			if confirmed {
				if err := onDelete(m.selected); err != nil {
					return err
				}
			}
		case actionResume:
			fmt.Fprintf(out, "Resume with: gmb ai chat --session %s\n", m.selected)
			return nil
		default:
			return nil
		}
	}
}

func newModel(list []session.Summary) model {
	m := model{
		list:     list,
		count:    len(list),
		width:    80,
		height:   14,
		sortCol:  sortUpdated,
		sortDesc: true,
		help:     components.NewHelpOverlay(tui.DefaultKeyMap()),
	}
	m.rebuild()
	return m
}

// rebuild re-sorts the session list by the active column and refreshes the
// table rows.
func (m *model) rebuild() {
	rows := make([]session.Summary, len(m.list))
	copy(rows, m.list)
	sort.SliceStable(rows, func(i, j int) bool {
		var less bool
		switch m.sortCol {
		case sortID:
			less = rows[i].ID < rows[j].ID
		case sortProvider:
			less = rows[i].Provider+"/"+rows[i].Model < rows[j].Provider+"/"+rows[j].Model
		default:
			less = rows[i].Updated.Before(rows[j].Updated)
		}
		if m.sortDesc {
			return !less
		}
		return less
	})

	idWidth := 22
	updatedWidth := 20
	provWidth := 36
	if m.width > 0 && m.width < 80 {
		provWidth = m.width - idWidth - updatedWidth - 6
		if provWidth < 12 {
			provWidth = 12
		}
	}

	columns := []table.Column{
		{Title: "ID", Width: idWidth},
		{Title: "UPDATED", Width: updatedWidth},
		{Title: "PROVIDER/MODEL", Width: provWidth},
	}
	tblRows := make([]table.Row, 0, len(rows))
	for _, s := range rows {
		tblRows = append(tblRows, table.Row{
			s.ID,
			s.Updated.Format("2006-01-02 15:04"),
			s.Provider + "/" + s.Model,
		})
	}
	m.tbl = components.NewGMTable(columns, tblRows)
}

func confirmDelete(id string, in io.Reader, out io.Writer) (bool, error) {
	var confirmed bool
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Delete session?").
				Description("This removes the saved conversation transcript.").
				Affirmative("Delete").
				Negative("Cancel").
				Value(&confirmed),
		),
	).
		WithInput(in).
		WithOutput(out).
		WithTheme(tui.HuhTheme())

	if err := form.Run(); err != nil {
		return false, err
	}
	return confirmed, nil
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.rebuild()
		m.tbl.SetWidth(msg.Width - 4)
		m.tbl.SetHeight(msg.Height - 6)
		return m, nil
	case tea.KeyMsg:
		if m.help.Visible && msg.String() != "?" && msg.String() != "q" && msg.String() != "esc" {
			m.help.Visible = false
		}
		switch msg.String() {
		case "?":
			m.help.Toggle()
			return m, nil
		case "q", "esc", "ctrl+c":
			m.action = actionQuit
			return m, tea.Quit
		case "d":
			row := m.tbl.SelectedRow()
			if len(row) > 0 {
				m.selected = row[0]
				m.action = actionDelete
				return m, tea.Quit
			}
		case "r", "enter":
			row := m.tbl.SelectedRow()
			if len(row) > 0 {
				m.selected = row[0]
				m.action = actionResume
				return m, tea.Quit
			}
		case "g", "home":
			m.tbl.GotoTop()
			return m, nil
		case "G", "end":
			m.tbl.GotoBottom()
			return m, nil
		case "1", "2", "3":
			next := sortID
			if msg.String() == "2" {
				next = sortUpdated
			} else if msg.String() == "3" {
				next = sortProvider
			}
			if m.sortCol == next {
				m.sortDesc = !m.sortDesc
			} else {
				m.sortCol = next
				m.sortDesc = next == sortUpdated
			}
			m.rebuild()
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.tbl, cmd = m.tbl.Update(msg)
	return m, cmd
}

func (m model) View() string {
	if m.help.Visible {
		return m.help.View()
	}
	header := components.RenderHeader("AI Chat Sessions",
		fmt.Sprintf("%d session(s)", m.count), m.width)
	body := m.tbl.View()
	keyRow := components.JoinKeyHints(
		components.KeyHint("↑↓/jk", "navigate"),
		components.KeyHint("1/2/3", "sort"),
		components.KeyHint("enter/r", "resume"),
		components.KeyHint("d", "delete"),
		components.KeyHint("?", "help"),
		components.KeyHint("q", "quit"),
	)
	status := components.RenderStatusBar(keyRow, m.sortLabel(), m.width)
	return lipgloss.JoinVertical(lipgloss.Left,
		header,
		"",
		body,
		"",
		status,
	)
}

// sortLabel shows the active sort column and direction in the status bar.
func (m model) sortLabel() string {
	name := "ID"
	switch m.sortCol {
	case sortUpdated:
		name = "UPDATED"
	case sortProvider:
		name = "PROVIDER/MODEL"
	}
	dir := "asc"
	if m.sortDesc {
		dir = "desc"
	}
	return "sort: " + name + " " + dir
}
