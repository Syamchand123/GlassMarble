package views

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/tui"
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
		limit := maxCardLine()
		if lipgloss.Width(r) > limit {
			runes := []rune(r)
			w := 0
			i := 0
			for ; i < len(runes); i++ {
				w += lipgloss.Width(string(runes[i]))
				if w > limit-1 {
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
	// Measured and cut on runes: slicing by byte offset could split a UTF-8
	// sequence and emit replacement characters mid-path.
	if lipgloss.Width(s) <= max || max < 4 {
		return s
	}
	r := []rune(s)
	keep := max - 3
	for keep > 0 && lipgloss.Width(string(r[len(r)-keep:])) > max-3 {
		keep--
	}
	return "..." + string(r[len(r)-keep:])
}

// wrapText word-wraps s to the given display width (runes counted by
// lipgloss.Width), splitting on spaces and hard-breaking over-long tokens.
// Used where full message text must stay visible inside a card instead of
// being truncated away (e.g. `gmb ai doctor` problem details).
func wrapText(s string, width int) []string {
	if width <= 0 {
		return []string{s}
	}
	var lines []string
	cur := ""
	curW := 0
	flush := func() {
		if cur != "" {
			lines = append(lines, strings.TrimRight(cur, " "))
			cur = ""
			curW = 0
		}
	}
	for _, word := range strings.Fields(s) {
		w := 0
		for _, r := range word {
			w += lipgloss.Width(string(r))
		}
		if curW > 0 && curW+1+w > width {
			flush()
		}
		if w > width {
			// Hard-break a single over-long token.
			if cur != "" {
				flush()
			}
			runes := []rune(word)
			seg := ""
			segW := 0
			for _, r := range runes {
				rw := lipgloss.Width(string(r))
				if segW+rw > width {
					lines = append(lines, seg)
					seg = ""
					segW = 0
				}
				seg += string(r)
				segW += rw
			}
			if seg != "" {
				cur = seg
				curW = segW
			}
			continue
		}
		if cur == "" {
			cur = word
		} else {
			cur += " " + word
		}
		curW += 1 + w
	}
	flush()
	return lines
}

// maxCardLine is the longest rendered line allowed inside a styled card.
// Cards must not auto-size to an over-long line (e.g. a long absolute path),
// since a width that large inflates every padded line and can blow past the
// Windows os.Pipe 4KB buffer used by the CLI tests.
//
// On a real terminal it follows the window so wide terminals are not wasted
// and narrow ones do not wrap into garbage; when output is redirected (pipes,
// CI, the test suite) it stays at the fixed legacy width so captured output
// remains deterministic.
func maxCardLine() int {
	w, ok := tui.OutputWidth()
	if !ok {
		return 100
	}
	// leave room for the card border (2), its horizontal padding (4) and the
	// two-space indent every row carries
	w -= 8
	if w < 24 {
		return 24
	}
	if w > 140 {
		return 140
	}
	return w
}
