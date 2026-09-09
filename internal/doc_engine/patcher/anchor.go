// Package patcher — anchor.go
// Processes gmb:* directives extracted from managed zones.
package patcher

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
	Frozen      bool   // freeze or pin applies
	Instruction string // per-section LLM prompt override (empty = use SectionSpec.Instruction)
	Assert      string // doc-lint assertion text
	Todo        string // open doc gap description
	DiagramType string // e.g. "callgraph"
	DiagramScope string
	Mode        string // "deterministic" if tagged
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
