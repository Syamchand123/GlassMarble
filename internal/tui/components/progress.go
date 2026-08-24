package components

import (
	"fmt"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/tui"
	"github.com/charmbracelet/bubbles/progress"
	tea "github.com/charmbracelet/bubbletea"
)

// PhaseProgress is a single phase progress bar using the design-token colors:
// violet fill (ColorPrimary), gray-800 empty (ColorSurfaceCard), and a green
// "✓ Xms" marker once done. The fill animates with a Harmonica spring so
// abrupt jumps ease toward their target (§5 Phase 5, §7).
type PhaseProgress struct {
	model   progress.Model
	label   string
	current int
	total   int
	done    bool
	elapsed time.Duration
}

// NewPhaseProgress creates a labeled progress bar for a pipeline phase.
func NewPhaseProgress(label string, width int) PhaseProgress {
	if width <= 0 {
		width = 40
	}
	fillColor := tui.ColorPrimary.Dark
	emptyColor := tui.ColorSurfaceCard.Dark
	if !tui.HasDarkBackground() {
		fillColor = tui.ColorPrimary.Light
		emptyColor = tui.ColorSurfaceCard.Light
	}
	p := progress.New(
		progress.WithSolidFill(fillColor),
		progress.WithFillCharacters('█', '░'),
		progress.WithWidth(width),
		progress.WithoutPercentage(),
	)
	// Empty fill uses the surface card token (not the bubbles default).
	p.EmptyColor = emptyColor
	return PhaseProgress{model: p, label: label, total: 1}
}

// SetProgress updates current/total and eases the bar toward the new fraction
// with the spring. It returns the command that drives the animation frames.
func (s *PhaseProgress) SetProgress(current, total int) tea.Cmd {
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
func (s *PhaseProgress) MarkDone(d time.Duration) tea.Cmd {
	s.done = true
	s.elapsed = d
	s.current = s.total
	return s.model.SetPercent(1)
}

// IsDone reports whether the phase finished.
func (s *PhaseProgress) IsDone() bool { return s.done }

// View renders the progress bar with its label and status. The bar shows the
// spring-animated fraction.
func (s *PhaseProgress) View() string {
	label := tui.R.NewStyle().Bold(true).Foreground(tui.ColorTextPrimary).Render(s.label)

	bar := s.model.View()

	status := ""
	if s.done {
		ms := s.elapsed.Milliseconds()
		status = tui.R.NewStyle().Foreground(tui.ColorSuccess).Render(fmt.Sprintf("✓ %dms", ms))
	} else if s.current > 0 {
		status = tui.R.NewStyle().Foreground(tui.ColorAccent).Render(
			fmt.Sprintf("%d/%d", s.current, s.total))
	}
	return fmt.Sprintf("%s\n%s %s", label, bar, status)
}

// Update forwards a message to the underlying progress model.
func (s *PhaseProgress) Update(msg tea.Msg) (PhaseProgress, tea.Cmd) {
	model, cmd := s.model.Update(msg)
	if p, ok := model.(progress.Model); ok {
		s.model = p
	}
	return *s, cmd
}
