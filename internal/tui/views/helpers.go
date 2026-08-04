package views

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// joinLines joins rows with newlines, stripping a leading blank line so cards
// render without an empty top margin. Each row is bounded to maxCardLine
// visible columns so cards never auto-size to an over-long line (e.g. a long
// absolute path): a wide card inflates every padded line and can exceed the
// 4KB os.Pipe buffer used by the CLI tests on Windows.
func joinLines(rows []string) string {
	bounded := make([]string, 0, len(rows))
	for _, r := range rows {
		if lipgloss.Width(r) > maxCardLine {
			runes := []rune(r)
			w := 0
			i := 0
			for ; i < len(runes); i++ {
				w += lipgloss.Width(string(runes[i]))
				if w > maxCardLine-1 {
					break
				}
			}
			bounded = append(bounded, string(runes[:i])+"…")
			continue
		}
		bounded = append(bounded, r)
	}
	out := strings.Join(bounded, "\n")
	out = strings.TrimPrefix(out, "\n")
	return out
}

// itoa renders an int as a plain decimal string.
func itoa(n int) string {
	return strconv.Itoa(n)
}

// formatBytes renders a byte count as a human string (e.g. "2.4 MB").
func formatBytes(b int64) string {
	const mb = 1024 * 1024
	if b >= mb {
		return fmt.Sprintf("%.1f MB", float64(b)/float64(mb))
	}
	return fmt.Sprintf("%.1f KB", float64(b)/1024)
}

// humanBytes renders a byte count compactly (e.g. "2.4MB", "0KB").
func humanBytes(b int64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.1fGB", float64(b)/(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.1fMB", float64(b)/(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.1fKB", float64(b)/(1<<10))
	default:
		return fmt.Sprintf("%dB", b)
	}
}

// shortHash shortens a commit hash for display (12 chars), "(none)" when empty.
func shortHash(h string) string {
	if len(h) > 12 {
		return h[:12]
	}
	if h == "" {
		return "(none)"
	}
	return h
}

// truncateLeft shortens an over-long identifier from the left with an ellipsis
// prefix (the interesting suffix, e.g. the symbol name, is kept).
func truncateLeft(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return "..." + s[len(s)-max+3:]
}

// maxCardLine is the longest rendered line allowed inside a styled card. Cards
// must not auto-size to an over-long line (e.g. a long absolute path), since a
// width that large inflates every padded line and can blow past the Windows
// os.Pipe 4KB buffer used by the CLI tests.
const maxCardLine = 100
