// Package renderer implements Stage 6 of the Documentation Intelligence Engine pipeline.
//
// Dual-track architecture:
//   - Track A: LLM Prose Actuator (llm_actuator.go)
//   - Track B: Deterministic Renderer (deterministic.go)
//
// deterministic.go produces complete, useful, and verified documentation
// exclusively from AKG facts with zero LLM and zero network dependency.
package renderer

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/doc_engine/config"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/patcher"
)

// DeterministicRenderer renders markdown sections deterministically from a FactSheet.
type DeterministicRenderer struct{}

// NewDeterministicRenderer creates a new deterministic renderer instance.
func NewDeterministicRenderer() *DeterministicRenderer {
	return &DeterministicRenderer{}
}

// RenderSection renders a complete markdown section body from the given FactSheet.
// The output is tagged with <!-- gmb:mode:deterministic -->.
func (r *DeterministicRenderer) RenderSection(fs *config.FactSheet) (string, error) {
	if fs == nil {
		return "", fmt.Errorf("doc_engine/renderer: nil FactSheet")
	}

	var sb strings.Builder

	// Mode tag
	sb.WriteString(patcher.BuildModeTag("deterministic") + "\n\n")

	// Section title / header if available
	if fs.SectionInstruction != "" {
		sb.WriteString(fmt.Sprintf("> %s\n\n", fs.SectionInstruction))
	}

	// 1. Living Diagrams (Mermaid / PlantUML)
	//
	// GroundTruth.DiagramMermaid is deliberately NOT rendered here: it is
	// always a copy of Diagrams[0].Content (see facts.go's "section-level
	// diagram pointer... injected verbatim by the LLM actuator" — a Track-A
	// prompt convenience, not a second diagram). Rendering both meant every
	// section with 1+ configured diagrams showed its first diagram twice.
	for _, diagFact := range fs.GroundTruth.Diagrams {
		if strings.TrimSpace(diagFact.Content) != "" {
			diag := unwrapDiagramFences(diagFact.Content)
			sb.WriteString("```mermaid\n" + diag + "\n```\n\n")
		}
	}

	// 1b. HTTP Endpoints (method + path + handler). This is what makes the
	// "api" archetype's "Endpoints & Route Handlers" section — whose own
	// instruction promises "route paths, HTTP methods, handler functions"
	// — actually different from a generic function dump: without a real
	// Method/Path table, a handler function only ever appeared in the same
	// generic "Functions and Methods" table below as any other function.
	if len(fs.GroundTruth.Endpoints) > 0 {
		sb.WriteString("### Endpoints\n\n")
		sb.WriteString("| Method | Path | Handler | Description | File |\n")
		sb.WriteString("| --- | --- | --- | --- | --- |\n")
		for _, ep := range fs.GroundTruth.Endpoints {
			desc := cleanTableString(ep.Doc)
			if desc == "" {
				desc = "No doc comment provided."
			}
			// Handler is backtick-plain, never a link whose visible text is
			// the same identifier — like every other table here (see
			// Functions and Methods below), the clickable permalink goes in
			// its own File column with the file#line as link TEXT instead.
			// Doc almost always repeats the handler's own name as its first
			// word ("CreateTaskHandler handles..."); a [CreateTaskHandler]
			// link right next to that reads, after Gate 6 strips markdown
			// link/code syntax down to plain words for prose checking, as
			// the same bare word appearing twice in a row — a false
			// "doubled word" failure on perfectly correct content.
			sb.WriteString(fmt.Sprintf("| %s | `%s` | `%s` | %s | %s |\n",
				ep.Method, cleanInlineString(ep.Path), cleanInlineString(ep.Handler), desc,
				formatFileLink(ep.File, ep.Line, ep.Permalink)))
		}
		sb.WriteString("\n")
	}

	// 2. Call flow / callers
	if len(fs.GroundTruth.CallFlow) > 0 {
		sb.WriteString("### Call Flow\n\n")
		for i, step := range fs.GroundTruth.CallFlow {
			sb.WriteString(fmt.Sprintf("%d. `%s`\n", i+1, step))
		}
		sb.WriteString("\n")
	}

	if len(fs.GroundTruth.Callers) > 0 {
		sb.WriteString("### Direct Callers\n\n")
		for _, caller := range fs.GroundTruth.Callers {
			sb.WriteString(fmt.Sprintf("- `%s`\n", caller))
		}
		sb.WriteString("\n")
	}

	// 3. Exported functions and methods
	allSymbols := getActiveSymbols(fs)
	funcs := filterSymbols(allSymbols, []string{"func", "method"})
	if len(funcs) > 0 {
		sb.WriteString("### Functions and Methods\n\n")
		sb.WriteString("| Name | Signature | Description | File |\n")
		sb.WriteString("| --- | --- | --- | --- |\n")
		for _, sym := range funcs {
			name := symbolShortName(sym.FQN)
			sig := cleanTableString(sym.Signature)
			if sig == "" {
				sig = name
			}
			desc := cleanTableString(sym.Doc)
			if desc == "" {
				desc = "No doc comment provided."
			}
			fileLink := formatFileLink(sym.File, sym.Line, sym.Permalink)
			sb.WriteString(fmt.Sprintf("| `%s` | `%s` | %s | %s |\n", name, sig, desc, fileLink))
		}
		sb.WriteString("\n")
	}

	// 4. Structs and interfaces
	types := filterSymbols(allSymbols, []string{"struct", "interface", "type"})
	if len(types) > 0 {
		sb.WriteString("### Types and Interfaces\n\n")
		sb.WriteString("| Type | Kind | Description | File |\n")
		sb.WriteString("| --- | --- | --- | --- |\n")
		for _, sym := range types {
			name := symbolShortName(sym.FQN)
			desc := cleanTableString(sym.Doc)
			if desc == "" {
				desc = "No doc comment provided."
			}
			fileLink := formatFileLink(sym.File, sym.Line, sym.Permalink)
			sb.WriteString(fmt.Sprintf("| `%s` | %s | %s | %s |\n", name, sym.Kind, desc, fileLink))
		}
		sb.WriteString("\n")
	}

	// 5. Error Sentinels
	sentinels := fs.GroundTruth.Sentinels
	if len(sentinels) == 0 {
		sentinels = fs.GroundTruth.AddedSentinels
	}
	if len(sentinels) > 0 {
		sb.WriteString("### Error Catalog\n\n")
		sb.WriteString("| Error | Trigger Condition | Caller |\n")
		sb.WriteString("| --- | --- | --- |\n")
		for _, s := range sentinels {
			name := symbolShortName(s.FQN)
			trigger := cleanTableString(s.Doc)
			if trigger == "" {
				trigger = "Raised during execution failure."
			}
			caller := "-"
			if len(s.Callers) > 0 {
				caller = fmt.Sprintf("`%s`", s.Callers[0])
			}
			sb.WriteString(fmt.Sprintf("| `%s` | %s | %s |\n", name, trigger, caller))
		}
		sb.WriteString("\n")
	}

	// 7. Configuration Variables
	configVars := fs.GroundTruth.ConfigVars
	if len(configVars) == 0 {
		configVars = fs.GroundTruth.AddedConfigVars
	}
	if len(configVars) > 0 {
		sb.WriteString("### Configuration Variables\n\n")
		sb.WriteString("| Variable | Source | Default | Required | Description |\n")
		sb.WriteString("| --- | --- | --- | --- | --- |\n")
		for _, cv := range configVars {
			reqStr := "No"
			if cv.Required {
				reqStr = "Yes"
			}
			defVal := cv.Default
			if defVal == "" {
				defVal = cv.DefaultValue
			}
			if defVal == "" {
				defVal = "-"
			}
			desc := cleanTableString(cv.Doc)
			if desc == "" {
				desc = "Configured setting."
			}
			src := cv.Source
			if src == "" {
				src = "env"
			}
			sb.WriteString(fmt.Sprintf("| `%s` | %s | %s | %s | %s |\n", cv.Name, src, defVal, reqStr, desc))
		}
		sb.WriteString("\n")
	}

	// 8. Architectural Events / Changes
	if len(fs.GroundTruth.ArchEvents) > 0 {
		sb.WriteString("### Architectural Milestones\n\n")
		for _, ev := range fs.GroundTruth.ArchEvents {
			sb.WriteString(fmt.Sprintf("- %s\n", ev))
		}
		sb.WriteString("\n")
	}

	// 9. Added / Modified / Removed Symbol Changes
	hasDeltas := len(fs.GroundTruth.AddedSymbols) > 0 || len(fs.GroundTruth.ModifiedSymbols) > 0 || len(fs.GroundTruth.RemovedSymbols) > 0
	if hasDeltas {
		sb.WriteString("### Recent Symbol Changes\n\n")
		if len(fs.GroundTruth.AddedSymbols) > 0 {
			sb.WriteString("**Added:**\n")
			for _, sym := range fs.GroundTruth.AddedSymbols {
				sb.WriteString(fmt.Sprintf("- `%s`: `%s`\n", symbolShortName(sym.FQN), cleanInlineString(sym.Signature)))
			}
			sb.WriteString("\n")
		}
		if len(fs.GroundTruth.ModifiedSymbols) > 0 {
			sb.WriteString("**Modified:**\n")
			for _, mod := range fs.GroundTruth.ModifiedSymbols {
				sb.WriteString(fmt.Sprintf("- `%s`\n", symbolShortName(mod.FQN)))
			}
			sb.WriteString("\n")
		}
		if len(fs.GroundTruth.RemovedSymbols) > 0 {
			sb.WriteString("**Removed:**\n")
			for _, rem := range fs.GroundTruth.RemovedSymbols {
				sb.WriteString(fmt.Sprintf("- `%s`\n", symbolShortName(rem)))
			}
			sb.WriteString("\n")
		}
	}

	res := strings.TrimRight(sb.String(), "\n") + "\n"
	return res, nil
}

// ────────────────────────────────────────────────────────────────────────────
// Helpers
// ────────────────────────────────────────────────────────────────────────────

func getActiveSymbols(fs *config.FactSheet) []config.SymbolFact {
	if len(fs.GroundTruth.Symbols) > 0 {
		return fs.GroundTruth.Symbols
	}
	if len(fs.GroundTruth.AllSymbols) > 0 {
		return fs.GroundTruth.AllSymbols
	}
	return fs.GroundTruth.AddedSymbols
}

func filterSymbols(symbols []config.SymbolFact, kinds []string) []config.SymbolFact {
	var filtered []config.SymbolFact
	kindSet := make(map[string]bool)
	for _, k := range kinds {
		kindSet[normalizeKind(k)] = true
	}
	for _, s := range symbols {
		if kindSet[normalizeKind(s.Kind)] {
			filtered = append(filtered, s)
		}
	}
	return filtered
}

// normalizeKind case-folds AKG node kinds and maps common aliases so
// deterministic tables render regardless of the graph's kind vocabulary
// ("FUNCTION" vs "func", "CLASS" vs "struct", ...). Without this, real
// graphs silently produced empty tables.
func normalizeKind(k string) string {
	switch strings.ToLower(strings.TrimSpace(k)) {
	case "function", "functions", "func", "def", "subroutine":
		return "func"
	case "method", "methods":
		return "method"
	case "class", "classes", "struct", "structs", "record":
		return "struct"
	case "interface", "interfaces", "protocol", "trait":
		return "interface"
	case "type", "types", "typedef", "alias":
		return "type"
	default:
		return strings.ToLower(strings.TrimSpace(k))
	}
}

func symbolShortName(fqn string) string {
	parts := strings.Split(fqn, "::")
	if len(parts) > 1 {
		return parts[len(parts)-1]
	}
	subparts := strings.Split(fqn, ".")
	if len(subparts) > 1 {
		return subparts[len(subparts)-1]
	}
	return fqn
}

func cleanTableString(s string) string {
	s = strings.ReplaceAll(s, "|", "\\|")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", "")
	return strings.TrimSpace(s)
}

func cleanInlineString(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", "")
	return strings.TrimSpace(s)
}

func formatFileLink(file string, line int, permalink string) string {
	if permalink != "" {
		display := filepath.Base(file)
		if line > 0 {
			display = fmt.Sprintf("%s#L%d", display, line)
		}
		return fmt.Sprintf("[%s](%s)", display, permalink)
	}
	if file == "" {
		return "-"
	}
	display := filepath.Base(file)
	if line > 0 {
		display = fmt.Sprintf("%s:%d", display, line)
	}
	return fmt.Sprintf("`%s`", display)
}

// unwrapDiagramFences strips one layer of surrounding fenced-code markers
// (```mermaid ... ``` or bare ``` ... ```) so callers can wrap exactly
// once. Grounding may already return fenced blocks; wrapping those again
// produces nested fences whose unclosed tail swallows following anchors.
func unwrapDiagramFences(s string) string {
	t := strings.TrimSpace(s)
	if !strings.HasPrefix(t, "```") {
		return t
	}
	lines := strings.Split(t, "\n")
	if len(lines) < 2 {
		return strings.TrimSpace(strings.Trim(strings.TrimSpace(t), "`"))
	}
	// Drop the opening fence line (```mermaid, ```plantuml, or bare ```).
	rest := strings.Join(lines[1:], "\n")
	// Drop one closing fence line (a line that is only backticks, possibly
	// trailed by whitespace).
	restLines := strings.Split(rest, "\n")
	for len(restLines) > 0 && isFenceLine(restLines[len(restLines)-1]) {
		restLines = restLines[:len(restLines)-1]
	}
	return strings.TrimSpace(strings.Join(restLines, "\n"))
}

func isFenceLine(line string) bool {
	t := strings.TrimSpace(line)
	if len(t) < 3 || t[:3] != "```" {
		return false
	}
	return strings.Trim(t[3:], " \t") == ""
}
