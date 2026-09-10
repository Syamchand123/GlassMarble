// Package patcher — anchor.go
// Processes gmb:* directives extracted from managed zones.
package patcher

import (
	"fmt"
	"sort"
	"strings"
)

// Directive keys recognised in managed zones.
const (
	DirectiveFreeze      = "freeze"      // <!-- gmb:freeze -->      — lock section forever
	DirectivePin         = "pin"         // <!-- gmb:pin -->          — alias for freeze
	DirectiveInstruction = "instruction" // <!-- gmb:instruction: … --> — per-section prompt override
	DirectiveAssert      = "assert"      // <!-- gmb:assert: … -->    — doc-lint assertion
	DirectiveTodo        = "todo"        // <!-- gmb:todo: … -->      — open gap for engine to populate
	DirectiveDiagram     = "diagram"     // <!-- gmb:diagram:type:scope --> — living diagram inject point
	DirectiveMode        = "mode"        // <!-- gmb:mode:deterministic --> — render mode tag (informational)
)

// ProcessedDirectives is the resolved set of gmb:* directives for a zone.
type ProcessedDirectives struct {
	Frozen       bool   // freeze or pin applies
	Instruction  string // per-section LLM prompt override (empty = use SectionSpec.Instruction)
	Assert       string // doc-lint assertion text
	Todo         string // open doc gap description
	DiagramType  string // e.g. "callgraph"
	DiagramScope string
	Mode         string // "deterministic" if tagged
}

// ProcessDirectives converts the raw directive map (from parser) into a
// typed ProcessedDirectives struct for use in renderer and verifier.
func ProcessDirectives(raw map[string]string) ProcessedDirectives {
	if raw == nil {
		return ProcessedDirectives{}
	}
	pd := ProcessedDirectives{}
	if _, ok := raw[DirectiveFreeze]; ok {
		pd.Frozen = true
	}
	if _, ok := raw[DirectivePin]; ok {
		pd.Frozen = true
	}
	if v, ok := raw[DirectiveInstruction]; ok {
		pd.Instruction = v
	}
	if v, ok := raw[DirectiveAssert]; ok {
		pd.Assert = v
	}
	if v, ok := raw[DirectiveTodo]; ok {
		pd.Todo = v
	}
	if v, ok := raw[DirectiveDiagram]; ok {
		// format: type:scope (scope optional)
		parts := splitColon(v, 2)
		pd.DiagramType = parts[0]
		if len(parts) > 1 {
			pd.DiagramScope = parts[1]
		}
	}
	if v, ok := raw[DirectiveMode]; ok {
		pd.Mode = v
	}
	return pd
}

// EvaluateAsserts parses `<!-- gmb:assert: <rule> -->` lines in mdContent and
// evaluates each rule, returning failure message strings (empty = pass).
//
// Supported rules:
//   - `no-todo`: fails if any gmb:todo marker is present in the document.
//   - `symbols-covered:<csv>`: fails listing each CSV symbol for which
//     symbolExists returns false (a nil symbolExists fails every symbol).
//   - `freshness>=N`: cannot be evaluated statically; skipped silently.
//
// Unknown rule kinds are ignored (no failure).
func EvaluateAsserts(mdContent string, symbolExists func(string) bool) []string {
	var failures []string
	hasTodo := strings.Contains(mdContent, "gmb:todo")
	for _, line := range strings.Split(mdContent, "\n") {
		t := strings.TrimSpace(line)
		if !strings.HasPrefix(t, "<!--") || !strings.HasSuffix(t, "-->") {
			continue
		}
		inner := strings.TrimSpace(t[len("<!--") : len(t)-len("-->")])
		if !strings.HasPrefix(inner, "gmb:assert:") {
			continue
		}
		rule := strings.TrimSpace(strings.TrimPrefix(inner, "gmb:assert:"))
		if rule == "" {
			continue
		}
		lower := strings.ToLower(rule)
		switch {
		case lower == "no-todo":
			if hasTodo {
				failures = append(failures, "assert failed: no-todo violated: document contains gmb:todo markers")
			}
		case strings.HasPrefix(lower, "symbols-covered:"):
			csv := strings.TrimSpace(rule[len("symbols-covered:"):])
			for _, sym := range strings.Split(csv, ",") {
				s := strings.TrimSpace(sym)
				if s == "" {
					continue
				}
				if symbolExists == nil || !symbolExists(s) {
					failures = append(failures, fmt.Sprintf("assert failed: symbol %q not covered", s))
				}
			}
		case strings.HasPrefix(lower, "freshness"):
			// Freshness asserts require doc check context (state/freshness
			// scores); they cannot be evaluated statically — skip silently.
			continue
		default:
			// Unknown assert rule kinds are ignored.
			continue
		}
	}
	return failures
}

// PopulateTodos appends a deterministic draft line derived from grounding to
// content (e.g. "> TODO addressed from AKG: <N> exported symbols in scope:
// `A`, `B`, ...").
//
// The caller gates on todo presence (engine.go checks the managed zone for a
// `gmb:todo:` marker before calling). The helper itself is idempotent: if the
// draft line is already present, content is returned unchanged.
func PopulateTodos(content string, symbolNames []string) string {
	const draftMarker = "> TODO addressed from AKG:"
	if strings.Contains(content, draftMarker) {
		return content
	}
	seen := make(map[string]bool)
	var names []string
	for _, n := range symbolNames {
		n = strings.TrimSpace(n)
		if n == "" || seen[n] {
			continue
		}
		seen[n] = true
		names = append(names, n)
	}
	sort.Strings(names)

	var list string
	if len(names) == 0 {
		list = "none"
	} else {
		display := names
		suffix := ""
		if len(display) > 8 {
			display = display[:8]
			suffix = ", ..."
		}
		quoted := make([]string, len(display))
		for i, n := range display {
			quoted[i] = "`" + n + "`"
		}
		list = strings.Join(quoted, ", ") + suffix
	}
	line := fmt.Sprintf("%s %d exported symbols in scope: %s", draftMarker, len(names), list)
	if content != "" && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	return content + "\n" + line + "\n"
}

// BuildBeginMarker returns the standard <!-- gmb:begin:id --> comment line.
func BuildBeginMarker(sectionID string) string {
	return "<!-- gmb:begin:" + sectionID + " -->"
}

// BuildEndMarker returns the standard <!-- gmb:end:id --> comment line.
func BuildEndMarker(sectionID string) string {
	return "<!-- gmb:end:" + sectionID + " -->"
}

// BuildModeTag returns the <!-- gmb:mode:deterministic --> informational tag line.
func BuildModeTag(mode string) string {
	return "<!-- gmb:mode:" + mode + " -->"
}

// splitColon splits s by ":" up to n parts.
func splitColon(s string, n int) []string {
	var parts []string
	for i := 0; i < n-1; i++ {
		idx := indexOf(s, ':')
		if idx < 0 {
			break
		}
		parts = append(parts, s[:idx])
		s = s[idx+1:]
	}
	parts = append(parts, s)
	return parts
}

func indexOf(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}
