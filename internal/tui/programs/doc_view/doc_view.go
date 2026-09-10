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
	cfg         Config
	viewport    viewport.Model
	search      textinput.Model
	searching   bool
	lines       []string
	symbols     []symbolLink
	matches     []searchMatch
	selectedIdx int
	width       int
	height      int
	statusMsg   string
}

type symbolLink struct {
	Display string
	File    string
	Line    int
}

// searchMatch is a ranked fuzzy-search candidate: either a symbol link or a
// content line. File/Line allow Enter-to-open; ContentLine is the 0-based
// viewport line for line matches (symbols scroll to their definition when known).
type searchMatch struct {
	Kind        string // "symbol" | "line"
	Display     string
	File        string
	Line        int
	ContentLine int
	Score       float64
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
				m.search.Blur()
				m.openSelected()
				return m, nil
			case tea.KeyEsc:
				m.searching = false
				m.search.Blur()
				return m, nil
			case tea.KeyUp:
				if m.selectedIdx > 0 {
					m.selectedIdx--
				}
				return m, nil
			case tea.KeyDown:
				if m.selectedIdx < len(m.matches)-1 {
					m.selectedIdx++
				}
				return m, nil
			default:
				m.search, cmd = m.search.Update(msg)
				m.refilter(m.search.Value())
				return m, cmd
			}
		}

		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "/":
			m.searching = true
			m.search.Focus()
			m.refilter(m.search.Value())
			return m, textinput.Blink
		case "enter":
			m.openSelected()
			return m, nil
		case "j", "down":
			if len(m.matches) > 0 {
				if m.selectedIdx < len(m.matches)-1 {
					m.selectedIdx++
				}
				m.scrollToSelected()
				return m, nil
			}
		case "k", "up":
			if len(m.matches) > 0 {
				if m.selectedIdx > 0 {
					m.selectedIdx--
				}
				m.scrollToSelected()
				return m, nil
			}
		}
	}

	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

// refilter recomputes ranked fuzzy matches for query over symbol links and
// content lines, resetting the selection to the TOP match.
func (m *model) refilter(query string) {
	m.matches = rankMatches(query, m.symbols, m.lines)
	m.selectedIdx = 0
}

// openSelected opens the currently SELECTED match in $EDITOR at file:line
// (falling back to the first symbol link, then to scrolling to the top
// content match). Line-only matches scroll the viewport when they carry no file.
func (m *model) openSelected() {
	if len(m.matches) > 0 && m.selectedIdx >= 0 && m.selectedIdx < len(m.matches) {
		sel := m.matches[m.selectedIdx]
		if sel.File != "" {
			if err := openEditor(sel.File, sel.Line); err != nil {
				m.statusMsg = fmt.Sprintf("Error opening editor: %v", err)
			} else {
				m.statusMsg = fmt.Sprintf("Opened %s:%d in editor", sel.File, sel.Line)
			}
			return
		}
		m.viewport.SetYOffset(sel.ContentLine)
		m.statusMsg = fmt.Sprintf("Jumped to line %d", sel.ContentLine+1)
		return
	}
	m.scrollToQuery(m.search.Value())
	if len(m.symbols) > 0 {
		sym := m.symbols[0]
		if err := openEditor(sym.File, sym.Line); err != nil {
			m.statusMsg = fmt.Sprintf("Error opening editor: %v", err)
		} else {
			m.statusMsg = fmt.Sprintf("Opened %s:%d in editor", sym.File, sym.Line)
		}
	}
}

// scrollToSelected scrolls the viewport preview to the selected match when it
// maps to a content line.
func (m *model) scrollToSelected() {
	if len(m.matches) == 0 || m.selectedIdx < 0 || m.selectedIdx >= len(m.matches) {
		return
	}
	sel := m.matches[m.selectedIdx]
	if sel.Kind == "line" {
		m.viewport.SetYOffset(sel.ContentLine)
	}
}

// fuzzyScore scores query against target as a subsequence match
// (case-insensitive): score = matched-chars/len(query) weighted by
// consecutive runs. All query chars must appear in order; otherwise 0.
// Exact-substring targets earn a bonus so they rank above scattered matches.
func fuzzyScore(query, target string) float64 {
	q := strings.ToLower(query)
	t := strings.ToLower(target)
	if len(q) == 0 || len(t) == 0 {
		return 0
	}
	qi, ti := 0, 0
	matched := 0
	longestRun, curRun := 0, 0
	lastMatch := -2
	gaps := 0
	for qi < len(q) && ti < len(t) {
		if q[qi] == t[ti] {
			matched++
			if ti == lastMatch+1 {
				curRun++
			} else {
				if curRun > longestRun {
					longestRun = curRun
				}
				curRun = 1
				if lastMatch >= 0 {
					gaps += ti - lastMatch - 1
				}
			}
			lastMatch = ti
			qi++
		}
		ti++
	}
	if curRun > longestRun {
		longestRun = curRun
	}
	if matched < len(q) {
		return 0
	}
	score := float64(matched) / float64(len(q))
	score *= 1 + 0.5*float64(longestRun)/float64(len(q))
	if strings.Contains(t, q) {
		score += 0.75
	}
	score -= 0.001 * float64(gaps)
	return score
}

// rankMatches scores symbolLink candidates and content lines against query and
// returns them sorted by score (descending, stable). Empty query returns the
// symbol list in order (TOP match = first symbol).
func rankMatches(query string, symbols []symbolLink, lines []string) []searchMatch {
	if strings.TrimSpace(query) == "" {
		var out []searchMatch
		for _, s := range symbols {
			out = append(out, searchMatch{Kind: "symbol", Display: s.Display, File: s.File, Line: s.Line, Score: 1})
		}
		return out
	}
	var out []searchMatch
	for _, s := range symbols {
		best := fuzzyScore(query, s.Display)
		if f := fuzzyScore(query, s.File); f > best {
			best = f
		}
		if best > 0 {
			out = append(out, searchMatch{Kind: "symbol", Display: s.Display, File: s.File, Line: s.Line, Score: best + 0.5})
		}
	}
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if s := fuzzyScore(query, trimmed); s > 0 {
			out = append(out, searchMatch{Kind: "line", Display: trimLine(trimmed, 80), ContentLine: i, Score: s})
		}
	}
	sortMatches(out)
	if len(out) > 20 {
		out = out[:20]
	}
	return out
}

func sortMatches(ms []searchMatch) {
	for i := 1; i < len(ms); i++ {
		for j := i; j > 0 && ms[j].Score > ms[j-1].Score; j-- {
			ms[j], ms[j-1] = ms[j-1], ms[j]
		}
	}
}

func trimLine(s string, maxLen int) string {
	if len(s) > maxLen {
		return s[:maxLen-3] + "..."
	}
	return s
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
		searchBar += renderMatches(m.matches, m.selectedIdx)
	} else if len(m.matches) > 0 {
		searchBar = renderMatches(m.matches, m.selectedIdx)
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

// renderMatches renders the ranked filter list with the selected row
// highlighted (j/k or up/down to move, Enter opens the selected match).
func renderMatches(matches []searchMatch, selected int) string {
	if len(matches) == 0 {
		return ""
	}
	selStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFFFFF")).Background(lipgloss.Color("#7D56F4"))
	rowStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#A0A0A0"))
	var sb strings.Builder
	limit := len(matches)
	if limit > 8 {
		limit = 8
	}
	for i := 0; i < limit; i++ {
		prefix := "    "
		row := fmt.Sprintf("%s [%s] %.2f", matches[i].Kind, matches[i].Display, matches[i].Score)
		if i == selected {
			sb.WriteString("  ▸ " + selStyle.Render(row) + "\n")
		} else {
			sb.WriteString(prefix + rowStyle.Render(row) + "\n")
		}
	}
	if len(matches) > limit {
		sb.WriteString(rowStyle.Render(fmt.Sprintf("    … %d more", len(matches)-limit)) + "\n")
	}
	return sb.String()
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
