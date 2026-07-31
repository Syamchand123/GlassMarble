package terminal

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// IsTTY detects if standard output is a terminal
func IsTTY() bool {
	if fileInfo, _ := os.Stderr.Stat(); (fileInfo.Mode() & os.ModeCharDevice) != 0 {
		return true
	}
	return false
}

// Colors
const (
	Reset  = "\033[0m"
	Red    = "\033[31m"
	Green  = "\033[32m"
	Yellow = "\033[33m"
	Blue   = "\033[34m"
	Purple = "\033[35m"
	Cyan   = "\033[36m"
	Gray   = "\033[37m"
)

// UseColors determines if colors should be used based on TTY and NO_COLOR env var
func UseColors() bool {
	if !IsTTY() {
		return false
	}
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	return true
}

func Colorize(color string, text string) string {
	if !UseColors() {
		return text
	}
	return color + text + Reset
}

// Spinner provides a simple terminal spinner animation
type Spinner struct {
	message string
	frames  []string
	stop    chan struct{}
	running bool
}

func NewSpinner() *Spinner {
	return &Spinner{
		frames: []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"},
		stop:   make(chan struct{}),
	}
}

func (s *Spinner) Start(message string) {
	s.message = message
	if !IsTTY() {
		fmt.Fprintf(os.Stderr, "%s...\n", s.message)
		return
	}

	if s.running {
		return
	}
	s.running = true

	go func() {
		i := 0
		for {
			select {
			case <-s.stop:
				fmt.Fprint(os.Stderr, "\r\033[K") // clear line
				s.running = false
				s.stop = make(chan struct{})
				return
			default:
				fmt.Fprintf(os.Stderr, "\r%s %s", Colorize(Cyan, s.frames[i%len(s.frames)]), s.message)
				i++
				time.Sleep(100 * time.Millisecond)
			}
		}
	}()
}

func (s *Spinner) Stop(finalMessage string) {
	if !IsTTY() {
		fmt.Fprintln(os.Stderr, finalMessage)
		return
	}
	close(s.stop)
	time.Sleep(10 * time.Millisecond) // Wait for go routine to clear
	fmt.Fprintf(os.Stderr, "\r%s\n", finalMessage)
}

// ProgressBar provides a simple progress bar
func ProgressBar(prefix string, current, total int) {
	if !IsTTY() || total == 0 {
		return
	}
	width := 40
	percent := float64(current) / float64(total)
	completed := int(percent * float64(width))

	bar := strings.Repeat("█", completed) + strings.Repeat("░", width-completed)

	fmt.Fprintf(os.Stderr, "\r%s [%s] %d%% (%d/%d)", prefix, Colorize(Green, bar), int(percent*100), current, total)
	if current == total {
		fmt.Fprintln(os.Stderr)
	}
}
