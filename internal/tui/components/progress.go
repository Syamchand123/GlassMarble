package components

import (
	"fmt"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/tui"
	"github.com/charmbracelet/bubbles/progress"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// StageProgress is a single stage progress bar using the design-token colors:
// violet fill (ColorPrimary), gray-800 empty (ColorSurfaceCard), and a green
// "✓ Xms" marker once done. The fill animates with a Harmonica spring so
// abrupt jumps ease toward their target (§5 Phase 5, §7).
type StageProgress struct {
	model   progress.Model
	label   string
	current int
	total   int
	done    bool
	elapsed time.Duration
}

// NewStageProgress creates a labeled progress bar for a pipeline stage.
func NewStageProgress(label string, width int) StageProgress {
	if width <= 0 {
		width = 40
	}
	p := progress.New(
		progress.WithSolidFill(string(tui.ColorPrimary)),
		progress.WithFillCharacters('█', '░'),
		progress.WithWidth(width),
		progress.WithoutPercentage(),
	)
	// Empty fill uses the gray-800 surface token (not the bubbles default).
	p.EmptyColor = string(tui.ColorSurfaceCard)
	return StageProgress{model: p, label: label, total: 1}
}

// SetProgress updates current/total and eases the bar toward the new fraction
// with the spring. It returns the command that drives the animation frames.
func (s *StageProgress) SetProgress(current, total int) tea.Cmd {
	if total <= 0 {
		total = 1
	}
	s.current = current
	s.total = total
	fraction := float64(current) / float64(total)
	if fraction > 1 {
		fraction = 1
	}
	return s.model.SetPercent(fraction)
}

// MarkDone records completion and duration and snaps the bar to full.
func (s *StageProgress) MarkDone(d time.Duration) tea.Cmd {
	s.done = true
	s.elapsed = d
	s.current = s.total
	return s.model.SetPercent(1)
}

// IsDone reports whether the stage finished.
func (s *StageProgress) IsDone() bool { return s.done }

// View renders the progress bar with its label and status. The bar shows the
// spring-animated fraction.
func (s *StageProgress) View() string {
	label := lipgloss.NewStyle().Bold(true).Foreground(tui.ColorTextPrimary).Render(s.label)

	bar := s.model.View()

	status := ""
	if s.done {
		ms := s.elapsed.Milliseconds()
		status = lipgloss.NewStyle().Foreground(tui.ColorSuccess).Render(fmt.Sprintf("✓ %dms", ms))
	} else if s.current > 0 {
		status = lipgloss.NewStyle().Foreground(tui.ColorAccent).Render(
			fmt.Sprintf("%d/%d", s.current, s.total))
	}
	return fmt.Sprintf("%s\n%s %s", label, bar, status)
}

// Update forwards a message to the underlying progress model.
func (s *StageProgress) Update(msg tea.Msg) (StageProgress, tea.Cmd) {
	model, cmd := s.model.Update(msg)
	if p, ok := model.(progress.Model); ok {
		s.model = p
	}
	return *s, cmd
}
