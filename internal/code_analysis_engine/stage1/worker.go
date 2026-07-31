package stage1

import (
	"runtime"
)

// resolveWorkerCount picks a sensible default when the caller asked for 0
// workers. Tree-sitter parsers are not safe for concurrent re-use; bind one
// OS thread per worker and cap at GOMAXPROCS to avoid scheduler thrash.
func resolveWorkerCount(requested int) int {
	if requested > 0 {
		return requested
	}
	n := runtime.GOMAXPROCS(0)
	if n <= 0 {
		n = 1
	}
	if n > 8 {
		n = 8
	}
	return n
}
