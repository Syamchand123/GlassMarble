package tui

import (
	"os"
	"strconv"
	"sync"

	"golang.org/x/term"
)

var (
	widthOnce sync.Once
	widthVal  int
	widthOK   bool
)

// OutputWidth reports the usable terminal width for rendering, and whether a
// width could be determined at all.
//
// It returns false when stdout is not a terminal (pipes, files, CI, the test
// suite), which lets callers fall back to a fixed width so captured output
// stays byte-for-byte deterministic. COLUMNS is honoured when set, so callers
// can force a width in scripts and tests.
//
// The result is resolved once per process: every view asking mid-render must
// see the same width, or a single frame would mix layouts.
func OutputWidth() (int, bool) {
	widthOnce.Do(func() {
		if env := os.Getenv("COLUMNS"); env != "" {
			if n, err := strconv.Atoi(env); err == nil && n > 0 {
				widthVal, widthOK = n, true
				return
			}
		}
		if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 0 {
			widthVal, widthOK = w, true
			return
		}
	})
	return widthVal, widthOK
}

// ResetOutputWidthForTest clears the memoised width so a test can exercise
// several terminal sizes in one process.
func ResetOutputWidthForTest() {
	widthOnce = sync.Once{}
	widthVal, widthOK = 0, false
}
