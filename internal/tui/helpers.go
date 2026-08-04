package tui

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/terminal"
	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"
)

// IsInteractive reports whether a full-screen BubbleTea program or Huh form
// can be launched for the given command. It requires ALL of:
//
//   - the process not to be a Go test binary (testing.Testing()),
//   - the process stderr to be a character device (terminal.IsTTY), and
//   - the command's input and output to be real terminals on their fds.
//
// The fd + test checks guarantee the cmd/ test suite never enters an event
// loop: tests either wire strings.Reader/bytes.Buffer via SetIn/SetOut (not
// *os.File → rejected), or they call Execute() with a pipe writer or the
// default os.Stdin/os.Stdout (a non-terminal fd or a *.test binary →
// rejected, and a piped/grep'd fd is not a terminal → rejected). Real CLI
// usage ("gmb ... --prune" from a terminal) passes every check.
func IsInteractive(in io.Reader, out io.Writer) bool {
	if testing.Testing() {
		return false
	}
	if !terminal.IsTTY() {
		return false
	}
	inFile, inOK := in.(*os.File)
	outFile, outOK := out.(*os.File)
	if !inOK || !outOK {
		return false
	}
	if !term.IsTerminal(int(inFile.Fd())) || !term.IsTerminal(int(outFile.Fd())) {
		return false
	}
	return true
}

// Columns lays out two styled blocks side by side. leftWidth controls the
// left column width; the right column gets the remaining space.
func Columns(left, right string, leftWidth int) string {
	return lipgloss.JoinHorizontal(lipgloss.Top, left, right)
}

// Divider renders a labeled horizontal divider:
//
//	─── Label ────────────────────────────────────
func Divider(label string, width int) string {
	if width <= 0 {
		width = 60
	}
	label = " " + strings.TrimSpace(label) + " "
	prefix := StyleDivider.Render("───")
	mid := StyleDivider.Render(strings.Repeat("─", width))
	if label != "  " {
		mid = StyleDivider.Render(strings.Repeat("─", 2)) + " " +
			StyleH2.Render(strings.TrimSpace(label)) + " " +
			StyleDivider.Render(strings.Repeat("─", width))
	}
	return prefix + mid
}

// KV renders a key-value row: key in ColorTextSecondary, value in
// ColorTextPrimary, with a fixed-width key column for alignment.
func KV(key, value string) string {
	return StyleLabel.Render(padRight(key, 20)) + "  " + value
}

// KVStyled is like KV but renders the value with the provided style.
func KVStyled(key string, value string, valueStyle lipgloss.Style) string {
	return StyleLabel.Render(padRight(key, 20)) + "  " + valueStyle.Render(value)
}

// Indent indents a block of text by n spaces.
func Indent(text string, n int) string {
	pad := strings.Repeat(" ", n)
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		if l != "" {
			lines[i] = pad + l
		}
	}
	return strings.Join(lines, "\n")
}

// KeyValueCard renders a card containing rows of key/value pairs, wrapping the
// given body in the standard StyleCard.
func KeyValueCard(rows []string) string {
	return StyleCard.Render(strings.Join(rows, "\n"))
}

func padRight(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-len(s))
}
