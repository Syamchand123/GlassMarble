package tui

import (
	"fmt"
	"io"
	"sync/atomic"
)

// quiet is the process-wide --quiet gate. It is set once from the root
// command's PersistentPreRunE and read by the output helpers below.
var quiet atomic.Bool

// SetQuiet enables or disables suppression of non-error output.
func SetQuiet(v bool) { quiet.Store(v) }

// Quiet reports whether non-error output should be suppressed.
func Quiet() bool { return quiet.Load() }

// Fprintln writes a line unless --quiet is active. Diagnostics and errors must
// not go through this helper — they belong on stderr and are always shown.
func Fprintln(w io.Writer, a ...any) {
	if quiet.Load() {
		return
	}
	fmt.Fprintln(w, a...)
}

// Fprintf writes formatted output unless --quiet is active.
func Fprintf(w io.Writer, format string, a ...any) {
	if quiet.Load() {
		return
	}
	fmt.Fprintf(w, format, a...)
}

// Fprint writes output unless --quiet is active.
func Fprint(w io.Writer, a ...any) {
	if quiet.Load() {
		return
	}
	fmt.Fprint(w, a...)
}
