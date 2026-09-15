// Gate 3: AKG Symbol Linter.
// Extracts every backtick-quoted identifier from generated markdown and verifies
// each one exists in the AKG symbol index or in the stdlib allowlist.
// A miss = hallucination detected → fail.
package verifier

import (
	"regexp"
)

// backtickRe matches Go-style backtick-quoted single identifiers: `Foo`, `Bar`, `ErrX`.
// Does NOT match multi-word code spans like `func Foo(x int)` — those are
// statements, not symbol references. The heuristic: no spaces and no parens.
var backtickRe = regexp.MustCompile("`([A-Za-z_][A-Za-z0-9_.]+)`")

// stdlibAllowlist is a set of common Go standard library identifiers that
// appear in documentation prose but don't exist in the project's AKG.
// Add to this list as needed; it is intentionally conservative.
var stdlibAllowlist = map[string]bool{
	// context
	"context.Context": true,
	"context.Background": true,
	"context.WithCancel": true,
	"context.WithTimeout": true,
	// errors
	"errors.New": true,
	"errors.Is": true,
	"errors.As": true,
	"fmt.Errorf": true,
	// io
	"io.Writer": true,
	"io.Reader": true,
	"io.EOF": true,
	// os
	"os.Stderr": true,
	"os.Stdout": true,
	"os.Stdin": true,
	"os.Exit": true,
	"os.Getenv": true,
	// sync
	"sync.Mutex": true,
	"sync.RWMutex": true,
	"sync.WaitGroup": true,
	"sync.Once": true,
	// net/http
	"http.Handler": true,
	"http.HandlerFunc": true,
	"http.Request": true,
	"http.ResponseWriter": true,
	// built-in types
	"string": true,
	"int":    true,
	"int64":  true,
	"bool":   true,
	"error":  true,
	"byte":   true,
	"rune":   true,
	// common patterns in doc prose
	"nil":   true,
	"true":  true,
	"false": true,
	// Common CLI/ops tools: a runbook or onboarding section legitimately
	// says "run `netstat -tlnp` to confirm the port is listening" — a
	// completely normal operational instruction, not a code-symbol claim,
	// but backtickRe can't tell the two apart by shape alone (both are
	// bare identifiers). Without this, that section hard-fails Gate 3 as
	// an "unresolved symbol" and ships with no content at all — found via
	// live testing against the runbook archetype specifically, the one
	// archetype whose whole job is describing operational commands. Not
	// exhaustive (no allowlist can be, against an open-ended set of shell
	// tools), but covers the ones that come up routinely.
	"netstat":    true,
	"curl":       true,
	"wget":       true,
	"ping":       true,
	"traceroute": true,
	"dig":        true,
	"nslookup":   true,
	"ssh":        true,
	"scp":        true,
	"ps":         true,
	"top":        true,
	"htop":       true,
	"kill":       true,
	"systemctl":  true,
	"journalctl": true,
	"docker":     true,
	"kubectl":    true,
	"grep":       true,
	"awk":        true,
	"sed":        true,
	"tail":       true,
	"head":       true,
	"cat":        true,
	"ls":         true,
	"df":         true,
	"du":         true,
	"lsof":       true,
	"tcpdump":    true,
	"strace":     true,
	"nc":         true,
	"jq":         true,
	"ss":         true,
}

// checkSymbols extracts all backtick identifiers from content and verifies them.
func checkSymbols(content string, akg AKGSymbolIndex) *GateError {
	matches := backtickRe.FindAllStringSubmatch(content, -1)
	for _, m := range matches {
		ident := m[1]
		if stdlibAllowlist[ident] {
			continue
		}
		if akg.HasSymbol(ident) {
			continue
		}
		return &GateError{Gate: 3, Message: "unresolved symbol: `" + ident + "` not found in AKG or stdlib allowlist"}
	}
	return nil
}
