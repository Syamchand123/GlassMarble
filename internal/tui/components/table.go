package components

import (
	"github.com/Syamchand123/GlassMarble/internal/tui"
	"github.com/charmbracelet/bubbles/table"
)

// NewGMTable builds a themed bubbles table: bold violet header, primary-color
// selected row, and a standard border.
func NewGMTable(columns []table.Column, rows []table.Row) table.Model {
	headerStyle := tui.R.NewStyle().
		Bold(true).
		Foreground(tui.ColorPrimary).
		Padding(0, 1)

	selectedStyle := tui.R.NewStyle().
		Foreground(tui.ColorSurfaceDark).
		Background(tui.ColorPrimary).
		Bold(true)

	styles := table.DefaultStyles()
	styles.Header = headerStyle
	styles.Selected = selectedStyle
	styles.Cell = tui.R.NewStyle().Padding(0, 1)

	return table.New(
		table.WithColumns(columns),
		table.WithRows(rows),
		table.WithStyles(styles),
		table.WithFocused(true),
	)
}

// TableColumns is a convenience constructor for []table.Column.
func TableColumns(cols ...table.Column) []table.Column { return cols }

// TableRows is a convenience constructor for []table.Row.
func TableRows(rows ...table.Row) []table.Row { return rows }
