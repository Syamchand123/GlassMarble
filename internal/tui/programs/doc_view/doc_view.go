// Package doc_view implements the Bubble Tea interactive reader for `gmb doc view`.
// Provides scrollable markdown viewing, section navigation, fuzzy search,
// and Enter-to-open-in-$EDITOR for code symbols.
package doc_view

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var permalinkRe = regexp.MustCompile(`\[([^\]]+)\]\(([^)]+#L(\d+)(?:-L\d+)?)\)`)

// Config carries parameters into the doc viewer.
type Config struct {
	Title      string
	TargetPath string
	Content    string
	In         io.Reader
	Out        io.Writer
}

type model struct {
	cfg        Config
	viewport   viewport.Model
	search     textinput.Model
	searching  bool
	lines      []string
	symbols    []symbolLink
	width      int
	height     int
	statusMsg  string
}

type symbolLink struct {
	Display string
	File    string
	Line    int
}

// Run launches the doc view Bubble Tea program.
func Run(cfg Config) error {
	p := tea.NewProgram(
		newModel(cfg),
		tea.WithInput(cfg.In),
		tea.WithOutput(cfg.Out),
		tea.WithAltScreen(),
	)
	_, err := p.Run()
	return err
}

func newModel(cfg Config) model {
	ti := textinput.New()
	ti.Placeholder = "Search sections or symbols (/)..."
	ti.CharLimit = 100
	ti.Width = 40

	links := extractSymbolLinks(cfg.Content)

	return model{
		cfg:       cfg,
		search:    ti,
		lines:     strings.Split(cfg.Content, "\n"),
		symbols:   links,
	}
}

func extractSymbolLinks(content string) []symbolLink {
	var links []symbolLink
	matches := permalinkRe.FindAllStringSubmatch(content, -1)
	for _, m := range matches {
		if len(m) >= 4 {
			filePart := strings.Split(m[2], "#")[0]
			line, _ := strconv.Atoi(m[3])
			links = append(links, symbolLink{
				Display: m[1],
				File:    filePart,
				Line:    line,
			})
		}
	}
	return links
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		headerHeight := 4
		footerHeight := 2
		vHeight := msg.Height - headerHeight - footerHeight
		if vHeight < 1 {
			vHeight = 1
		}
		m.viewport = viewport.New(msg.Width-4, vHeight)
		m.viewport.SetContent(m.cfg.Content)

	case tea.KeyMsg:
		if m.searching {
			switch msg.Type {
			case tea.KeyEnter:
				m.searching = false
				m.scrollToQuery(m.search.Value())
				return m, nil
			case tea.KeyEsc:
				m.searching = false
				m.search.Blur()
				return m, nil
			default:
				m.search, cmd = m.search.Update(msg)
				return m, cmd
			}
		}

		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "/":
			m.searching = true
			m.search.Focus()
			return m, textinput.Blink
		case "enter":
			// If symbols exist in this document, open the first symbol in editor
			if len(m.symbols) > 0 {
				sym := m.symbols[0]
				if err := openEditor(sym.File, sym.Line); err != nil {
					m.statusMsg = fmt.Sprintf("Error opening editor: %v", err)
				} else {
					m.statusMsg = fmt.Sprintf("Opened %s:%d in editor", sym.File, sym.Line)
				}
			}
		}
	}

	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

func (m *model) scrollToQuery(query string) {
	if query == "" {
		return
	}
	query = strings.ToLower(query)
	for i, line := range m.lines {
		if strings.Contains(strings.ToLower(line), query) {
			m.viewport.SetYOffset(i)
			m.statusMsg = fmt.Sprintf("Found %q at line %d", query, i+1)
			return
		}
	}
	m.statusMsg = fmt.Sprintf("Pattern %q not found", query)
}

func (m model) View() string {
	if m.width == 0 {
		return "Loading..."
	}

	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#FFFFFF")).
		Background(lipgloss.Color("#7D56F4")).
		Padding(0, 2).
		Width(m.width)

	subHeaderStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#A0A0A0")).
		Padding(0, 2)

	header := headerStyle.Render("GlassMarble Doc Reader — " + m.cfg.Title)
	subHeader := subHeaderStyle.Render(fmt.Sprintf("File: %s | Press '/' to search, 'enter' to open symbol, 'q' to quit", m.cfg.TargetPath))

	var searchBar string
	if m.searching {
		searchBar = "  " + m.search.View() + "\n"
	}

	status := ""
	if m.statusMsg != "" {
		status = "  " + lipgloss.NewStyle().Foreground(lipgloss.Color("#04B575")).Render(m.statusMsg) + "\n"
	}

	footer := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#666666")).
		Padding(0, 2).
		Render(fmt.Sprintf("%d%% scrolled", int(m.viewport.ScrollPercent()*100)))

	content := lipgloss.NewStyle().Padding(0, 2).Render(m.viewport.View())

	return fmt.Sprintf("%s\n%s\n%s%s%s\n%s", header, subHeader, searchBar, status, content, footer)
}

func openEditor(filePath string, line int) error {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = os.Getenv("VISUAL")
	}
	if editor == "" {
		if runtime.GOOS == "windows" {
			editor = "notepad"
		} else {
			editor = "nano"
		}
	}

	var args []string
	target := filePath
	if line > 0 && (strings.Contains(editor, "vi") || strings.Contains(editor, "code")) {
		if strings.Contains(editor, "code") {
			args = append(args, "-g", fmt.Sprintf("%s:%d", filePath, line))
		} else {
			args = append(args, fmt.Sprintf("+%d", line), filePath)
		}
	} else {
		args = append(args, target)
	}

	c := exec.Command(editor, args...)
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Start()
}
