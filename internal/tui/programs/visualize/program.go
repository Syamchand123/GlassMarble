// Package visualize is the BubbleTea program for `gmb visualize`. It shows a
// branded spinner while the diagram pipeline runs, then renders the generated
// markup in a scrollable viewport with save/regenerate/quit keybindings.
package visualize

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/product"
	"github.com/Syamchand123/GlassMarble/internal/tui"
	"github.com/Syamchand123/GlassMarble/internal/tui/components"
	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/types"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	width  = 80
	height = 24
)

// Config carries the command inputs into the program. Business logic stays in
// internal/visualization_engine; this struct only relays the options.
type Config struct {
	DiagType    types.DiagramType
	TTLPath     string
	Opts        types.QueryOptions
	SaveFile    string
	OutputFlag  string
	FormatFlag  string
	StoragePath string
	SummaryFlag bool
	In          io.Reader
	Out         io.Writer
}

type state int

const (
	stateLoading state = iota
	stateReady
	stateSaved
)

type generateDoneMsg struct {
	markup   string
	summary  *types.GraphSummary
	duration time.Duration
}

type generateErrMsg struct{ err error }

type model struct {
	cfg      Config
	spinner  components.GMSpinner
	viewport viewport.Model

	state     state
	mu        sync.Mutex
	progress  string
	markup    string
	summary   *types.GraphSummary
	duration  time.Duration
	savedPath string
	err       error
}

// Run launches the program. Progress is forwarded to the spinner label by the
// worker goroutine via a shared mutex-protected field, so no tea messages are
// needed for streaming stages.
func Run(cfg Config) error {
	m := newModel(cfg)
	p := tea.NewProgram(m, tea.WithOutput(cfg.Out), tea.WithInput(cfg.In))
	_, err := p.Run()
	return err
}

func newModel(cfg Config) *model {
	m := &model{
		cfg:     cfg,
		spinner: components.NewGMSpinner("Preparing..."),
		state:   stateLoading,
	}
	m.viewport = components.NewGMViewport(width, height-6)
	return m
}

func (m *model) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick(), m.generate())
}

func (m *model) generate() tea.Cmd {
	return func() tea.Msg {
		opts := m.cfg.Opts
		var summary *types.GraphSummary
		opts.OnSummary = func(s *types.GraphSummary) { summary = s }
		opts.OnProgress = func(stage, detail string) {
			msg := stage
			if detail != "" {
				msg += " " + detail
			}
			m.mu.Lock()
			m.progress = msg
			m.mu.Unlock()
		}
		start := time.Now()
		req := product.DiagramRequest{
			TTLPath:       m.cfg.TTLPath,
			Type:          m.cfg.DiagType,
			Scope:         opts.Scope,
			ScopePath:     opts.ScopePath,
			Entry:         opts.EntryPointID,
			Depth:         opts.MaxDepth,
			IncludeUnused: opts.IncludeUnused,
			Format:        opts.Format,
			OnProgress:    opts.OnProgress,
			// Forward the --link-level flag so TUI-launched diagrams run at
			// the requested linkage level, not the architecture default
			// (GAP-H-05).
			Options: product.DiagramOptions{
				LinkLevel:    opts.LinkLevel,
				Scope:        opts.Scope,
				ScopePath:    opts.ScopePath,
				Entry:        opts.EntryPointID,
				Depth:        opts.MaxDepth,
				IncludeUnused: opts.IncludeUnused,
				MaxNodes:     opts.MaxNodes,
				ChangedFiles: opts.ChangedFiles,
			},
		}
		markup, summary, err := product.BuildDiagram(req)
		if err != nil {
			return generateErrMsg{err: err}
		}
		return generateDoneMsg{markup: markup, summary: summary, duration: time.Since(start)}
	}
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		}
		switch m.state {
		case stateSaved:
			m.state = stateReady
			return m, nil
		case stateReady:
			switch msg.String() {
			case "s":
				if m.cfg.SaveFile == "" {
					m.err = fmt.Errorf("no --save file configured")
					return m, nil
				}
				if err := m.save(); err != nil {
					m.err = err
				} else {
					m.state = stateSaved
				}
				return m, nil
			case "r":
				m.state = stateLoading
				m.markup = ""
				m.progress = ""
				m.err = nil
				return m, tea.Batch(m.spinner.Tick(), m.generate())
			}
		}
	case generateDoneMsg:
		m.state = stateReady
		m.markup = msg.markup
		m.summary = msg.summary
		m.duration = msg.duration
		if m.cfg.SaveFile == "" && m.cfg.OutputFlag != "" {
			if err := m.writeOutput(); err != nil {
				m.err = err
			}
		}
		vpHeight := height - 3
		if m.summary != nil {
			vpHeight = height - 8
		}
		m.viewport = components.NewGMViewport(width, vpHeight)
		m.viewport.SetContent(components.StyleViewportContent(highlightMarkup(m.markup)))
		m.viewport.GotoTop()
		return m, nil
	case generateErrMsg:
		m.state = stateReady
		m.err = msg.err
		m.markup = fmt.Sprintf("Error: %v", msg.err)
		m.viewport.SetContent(m.markup)
		return m, nil
	}

	var cmd tea.Cmd
	m.spinner, cmd = m.spinner.Update(msg)
	if m.state == stateReady {
		var vpCmd tea.Cmd
		m.viewport, vpCmd = m.viewport.Update(msg)
		cmd = tea.Batch(cmd, vpCmd)
	}
	return m, cmd
}

func (m *model) save() error {
	fileName := m.cfg.SaveFile
	if !strings.HasSuffix(fileName, ".md") {
		fileName += ".md"
	}
	marblesDir := filepath.Join(m.cfg.StoragePath, ".glassmarble", "marbles")
	if err := os.MkdirAll(marblesDir, 0755); err != nil {
		return fmt.Errorf("failed to create marbles directory: %w", err)
	}
	filePath := filepath.Join(marblesDir, fileName)
	langTag := "mermaid"
	if strings.EqualFold(m.cfg.FormatFlag, "plantuml") {
		langTag = "plantuml"
	} else if strings.EqualFold(m.cfg.FormatFlag, "dot") || strings.EqualFold(m.cfg.FormatFlag, "graphviz") {
		langTag = "dot"
	}
	mdContent := fmt.Sprintf("```%s\n%s\n```\n", langTag, m.markup)
	if err := os.WriteFile(filePath, []byte(mdContent), 0644); err != nil {
		return fmt.Errorf("failed to save marble file: %w", err)
	}
	m.savedPath = filePath
	return nil
}

func (m *model) writeOutput() error {
	return os.WriteFile(m.cfg.OutputFlag, []byte(m.markup), 0644)
}

func (m *model) View() string {
	switch m.state {
	case stateLoading:
		return m.loadingView()
	case stateSaved:
		return m.savedView()
	default:
		return m.readyView()
	}
}

func (m *model) loadingView() string {
	m.mu.Lock()
	progress := m.progress
	m.mu.Unlock()
	label := "Generating " + displayName(m.cfg.DiagType) + " Diagram"
	header := components.RenderHeader(label, "GlassMarble", width)
	spinnerLine := m.spinner.View() + " " + progress
	source := tui.KV("Source", m.cfg.TTLPath)
	scope := tui.KV("Scope", scopeLabel(m.cfg.Opts))
	card := tui.StyleCard.Render(tui.Indent(spinnerLine+"\n\n"+source+"\n"+scope, 2))
	status := components.RenderStatusBar(
		components.JoinKeyHints(components.KeyHint("q", "quit")),
		"working...",
		width,
	)
	return lipgloss.JoinVertical(lipgloss.Left, header, card, status)
}

func (m *model) readyView() string {
	label := displayName(m.cfg.DiagType) + " Diagram"
	subtitle := fmt.Sprintf("done in %.1fs", m.duration.Seconds())
	header := components.RenderHeader(label, subtitle, width)

	blocks := []string{}
	if m.summary != nil && m.cfg.SummaryFlag {
		blocks = append(blocks, tui.Indent(renderSummary(m.summary), 2))
	}
	blocks = append(blocks, m.viewport.View())
	content := strings.Join(blocks, "\n\n")

	hints := []string{components.KeyHint("r", "regenerate")}
	if m.cfg.SaveFile != "" {
		hints = append([]string{components.KeyHint("s", "save")}, hints...)
	}
	hints = append(hints, components.KeyHint("q", "quit"))
	left := components.JoinKeyHints(hints...)
	right := "↑↓ scroll " + components.ScrollPosition(m.viewport)
	if m.err != nil {
		right = m.err.Error()
	} else if m.cfg.SaveFile == "" && m.cfg.OutputFlag != "" {
		right = "written to " + m.cfg.OutputFlag
	}
	status := components.RenderStatusBar(left, right, width)
	return lipgloss.JoinVertical(lipgloss.Left, header, content, status)
}

// highlightMarkup applies lightweight Lip Gloss syntax coloring to Mermaid /
// PlantUML / DOT markup: diagram headers in primary, `class` declarations and
// relationship arrows in accent, and comments/attributes in muted.
func highlightMarkup(markup string) string {
	var out strings.Builder
	for _, line := range strings.Split(markup, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "```"):
			out.WriteString(tui.StyleAccent.Render(line))
		case strings.HasPrefix(trimmed, "classDiagram"),
			strings.HasPrefix(trimmed, "graph"),
			strings.HasPrefix(trimmed, "flowchart"),
			strings.HasPrefix(trimmed, "sequenceDiagram"),
			strings.HasPrefix(trimmed, "stateDiagram"),
			strings.HasPrefix(trimmed, "erDiagram"),
			strings.HasPrefix(trimmed, "@start"):
			out.WriteString(tui.StylePrimaryText.Bold(true).Render(line))
		case strings.HasPrefix(trimmed, "class "),
			strings.HasPrefix(trimmed, "interface "),
			strings.HasPrefix(trimmed, "enum "):
			out.WriteString(highlightClassDecl(line))
		case strings.HasPrefix(trimmed, "//"), strings.HasPrefix(trimmed, "%%"), strings.HasPrefix(trimmed, "#"):
			out.WriteString(tui.StyleMuted.Render(line))
		case strings.Contains(trimmed, "-->") ||
			strings.Contains(trimmed, "--|>") ||
			strings.Contains(trimmed, "..>") ||
			strings.Contains(trimmed, "->"):
			out.WriteString(highlightRelation(line))
		default:
			out.WriteString(line)
		}
		out.WriteString("\n")
	}
	return strings.TrimSuffix(out.String(), "\n")
}

// highlightClassDecl colors "class <Name>" with the name in accent.
func highlightClassDecl(line string) string {
	idx := strings.Index(line, "class ")
	if idx < 0 {
		return line
	}
	head := line[:idx+len("class ")]
	rest := line[idx+len("class "):]
	nameEnd := strings.IndexAny(rest, " {<{")
	if nameEnd < 0 {
		nameEnd = len(rest)
	}
	name := rest[:nameEnd]
	return tui.StyleMuted.Render(head) + tui.StyleAccent.Render(name) + rest[nameEnd:]
}

// highlightRelation colors a relationship arrow line: target in success green.
func highlightRelation(line string) string {
	arrow := ""
	for _, a := range []string{"-->", "--|>", "..>", "->"} {
		if strings.Contains(line, a) {
			arrow = a
			break
		}
	}
	if arrow == "" {
		return line
	}
	idx := strings.Index(line, arrow)
	return line[:idx] + tui.StylePrimaryText.Bold(true).Render(arrow) + line[idx+len(arrow):]
}

func (m *model) savedView() string {
	header := components.RenderHeader("Marble Saved", "GlassMarble", width)
	body := tui.StyleOK.Render("✓ Marble saved successfully to") + "\n\n" +
		tui.Indent(tui.StyleCode.Render(m.savedPath), 2)
	card := tui.StyleCard.Render(tui.Indent(body, 2))
	status := components.RenderStatusBar(
		components.JoinKeyHints(components.KeyHint("enter", "back"), components.KeyHint("q", "quit")),
		"",
		width,
	)
	return lipgloss.JoinVertical(lipgloss.Left, header, card, status)
}

func renderSummary(s *types.GraphSummary) string {
	return strings.Join([]string{
		tui.StyleH2.Render("=== Graph Summary ==="),
		tui.KV("Nodes", fmt.Sprintf("%d | Edges: %d | Density: %.4f", s.NodeCount, s.EdgeCount, s.Density)),
		tui.KV("Diameter", fmt.Sprintf("%d | Avg Path Length: %.2f", s.Diameter, s.AvgPathLength)),
		tui.KV("Clusters", fmt.Sprintf("%d | Largest SCC: %d | God Objects: %d", s.ClusterCount, s.LargestSCCSize, s.GodObjectCount)),
	}, "\n")
}

func displayName(d types.DiagramType) string {
	s := strings.ToLower(string(d))
	s = strings.TrimPrefix(s, "uml_")
	s = strings.TrimPrefix(s, "c4_")
	return strings.ReplaceAll(s, "_", " ")
}

func scopeLabel(opts types.QueryOptions) string {
	switch opts.Scope {
	case types.ScopeFolder:
		return "folder:" + opts.ScopePath
	case types.ScopeFile:
		return "file:" + opts.ScopePath
	default:
		return "global"
	}
}
