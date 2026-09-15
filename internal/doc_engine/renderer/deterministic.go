// Package renderer implements Stage 6 of the Documentation Intelligence Engine pipeline.
//
// Dual-track architecture:
//   - Track A: LLM Prose Actuator (llm_actuator.go) — the default, mandatory
//     path. Writes the actual explanation in plain English from the
//     FactSheet's ground truth; the engine requires a working LLM provider
//     for this and refuses to generate rather than silently substituting
//     raw data for prose (see Orchestrator.RenderSection in engine.go).
//   - Track B: Deterministic Renderer (deterministic.go) — grounded fact
//     tables with zero LLM and zero network dependency. Two uses: (1) the
//     explicit, user-requested `--no-llm` mode (RenderSection: the complete,
//     standalone document some users deliberately want — offline, free,
//     no prose); (2) appended below the LLM's prose in the default path
//     (RenderReferenceAppendix), so every document pairs a real
//     explanation with a grounded lookup table, never one instead of the
//     other.
package renderer

import (
	"fmt"
	"path"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/Syamchand123/GlassMarble/internal/doc_engine/config"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/patcher"
)

// DeterministicRenderer renders markdown sections deterministically from a FactSheet.
type DeterministicRenderer struct{}

// NewDeterministicRenderer creates a new deterministic renderer instance.
func NewDeterministicRenderer() *DeterministicRenderer {
	return &DeterministicRenderer{}
}

// RenderSection renders a complete markdown section body from the given
// FactSheet: diagrams + the reference tables, with the deterministic mode
// tag and instruction blockquote. This is the explicit --no-llm path — the
// complete, standalone document a user gets when they deliberately choose
// no LLM, not a partial fallback.
func (r *DeterministicRenderer) RenderSection(fs *config.FactSheet) (string, error) {
	if fs == nil {
		return "", fmt.Errorf("doc_engine/renderer: nil FactSheet")
	}

	var sb strings.Builder
	sb.WriteString(patcher.BuildModeTag("deterministic") + "\n\n")
	if fs.SectionInstruction != "" {
		sb.WriteString(fmt.Sprintf("> %s\n\n", fs.SectionInstruction))
	}
	sb.WriteString(r.renderDiagramsBlock(fs))
	sb.WriteString(r.renderReferenceTables(fs))

	res := strings.TrimRight(sb.String(), "\n") + "\n"
	return res, nil
}

// referenceAppendixMarker separates the LLM's prose from the deterministic
// lookup tables below it: a horizontal rule plus a bold (not heading-level)
// label. Deliberately not a "### Reference" heading — the individual
// tables already have their own ### headings (Configuration Variables,
// Functions and Methods, ...), and for the common case of a section with
// only ONE populated table, an extra wrapping heading directly above it
// with nothing of its own in between reads as a redundant, oddly-empty
// heading stacked on another heading. A rule+label separates prose from
// data just as clearly without that.
const referenceAppendixMarker = "---\n\n**Reference**\n\n"

// RenderReferenceAppendix builds the grounded lookup appendix appended
// below the LLM's prose in the default (mandatory-LLM) path: diagrams,
// then every reference table after referenceAppendixMarker. No mode tag
// or instruction blockquote — the prose above already carries those.
// Returns "" if the FactSheet has nothing to show (no diagrams, no facts).
func (r *DeterministicRenderer) RenderReferenceAppendix(fs *config.FactSheet) string {
	if fs == nil {
		return ""
	}
	var sb strings.Builder
	sb.WriteString(r.renderDiagramsBlock(fs))
	if ref := r.renderReferenceTables(fs); strings.TrimSpace(ref) != "" {
		sb.WriteString(referenceAppendixMarker)
		sb.WriteString(ref)
	}
	return strings.TrimRight(sb.String(), "\n")
}

// renderDiagramsBlock renders the section's living diagrams (Mermaid /
// PlantUML) as fenced code blocks.
//
// GroundTruth.DiagramMermaid is deliberately NOT rendered here: it is
// always a copy of Diagrams[0].Content (see facts.go's "section-level
// diagram pointer... injected verbatim by the LLM actuator" — a Track-A
// prompt convenience, not a second diagram). Rendering both meant every
// section with 1+ configured diagrams showed its first diagram twice.
func (r *DeterministicRenderer) renderDiagramsBlock(fs *config.FactSheet) string {
	var sb strings.Builder
	for _, diagFact := range fs.GroundTruth.Diagrams {
		if strings.TrimSpace(diagFact.Content) != "" {
			diag := unwrapDiagramFences(diagFact.Content)
			sb.WriteString("```mermaid\n" + diag + "\n```\n\n")
		}
	}
	return sb.String()
}

// renderReferenceTables renders every grounded fact table/list — endpoints,
// call flow, callers, functions/methods, types/interfaces, error catalog,
// config variables, architectural milestones, and recent symbol changes —
// with no heading, mode tag, or diagrams of its own. Shared by RenderSection
// (the standalone --no-llm document) and RenderReferenceAppendix (the
// lookup appendix under the LLM's prose).
func (r *DeterministicRenderer) renderReferenceTables(fs *config.FactSheet) string {
	var sb strings.Builder

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
			sb.WriteString(fmt.Sprintf("%d. `%s`\n", i+1, stripPseudoSymbolPrefix(step)))
		}
		sb.WriteString("\n")
	}

	if len(fs.GroundTruth.Callers) > 0 {
		sb.WriteString("### Direct Callers\n\n")
		for _, caller := range fs.GroundTruth.Callers {
			sb.WriteString(fmt.Sprintf("- `%s`\n", stripPseudoSymbolPrefix(caller)))
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

	return sb.String()
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

// symbolShortName reduces an AKG FQN to its human-readable short form.
// Handles the id shapes this engine actually produces, checked in this
// specific order because some shapes are ambiguous prefixes of others:
//
//  1. "path::Name" (an AKG member reference) — the segment after the LAST
//     "::", e.g. "pkg/config/config.go::envInt::param:def" -> "param:def".
//  2. "file:path" / "module:path" (file/module-level pseudo-symbols) — the
//     path's base name, e.g. "file:pkg/config/config.go" -> "config.go".
//  3. A dotted qualified name whose FINAL segment starts with an uppercase
//     letter — the Go/Java convention for an exported type or member —
//     e.g. "internal/auth.SessionManager" -> "SessionManager", even though
//     that string also contains "/": checking this before the bare-path
//     rule below matters, because path.Base on the full string would
//     wrongly return "auth.SessionManager". A lowercase file extension
//     never matches this (see case 4 for why that distinction is safe).
//  4. A bare path (contains "/", none of the above matched — so any dotted
//     suffix, like ".go", is a lowercase file extension, not an exported
//     name) — the base name, e.g. "pkg/config/config.go" -> "config.go".
//
// Anything matching none of these — including a bare filename with no
// directory and no exported-looking suffix, e.g. "config.go" — is
// returned unchanged rather than chopped down to its extension.
func symbolShortName(fqn string) string {
	if idx := strings.LastIndex(fqn, "::"); idx >= 0 {
		return fqn[idx+2:]
	}
	if rest, ok := strings.CutPrefix(fqn, "file:"); ok {
		return path.Base(rest)
	}
	if rest, ok := strings.CutPrefix(fqn, "module:"); ok {
		return path.Base(rest)
	}
	if subparts := strings.Split(fqn, "."); len(subparts) > 1 {
		last := subparts[len(subparts)-1]
		if r := []rune(last); len(r) > 0 && unicode.IsUpper(r[0]) {
			return last
		}
	}
	if strings.Contains(fqn, "/") {
		return path.Base(fqn)
	}
	return fqn
}

// stripPseudoSymbolPrefix removes a "file:"/"module:" pseudo-symbol prefix
// (reducing to the path's base name, e.g. "file:pkg/config/config.go" ->
// "config.go") and otherwise returns ref unchanged. Used for Call Flow
// steps and Direct Callers — unlike symbolShortName, it deliberately does
// NOT collapse a "::"-chained ref (e.g. "pkg/api/handler.go::Handler::
// CreateTaskHandler") down to its last segment: that receiver-type
// qualification is exactly what makes one caller distinguishable from
// another same-named method elsewhere, so it must survive here even
// though the same chain would rightly get shortened inside prose.
func stripPseudoSymbolPrefix(ref string) string {
	if rest, ok := strings.CutPrefix(ref, "file:"); ok {
		return path.Base(rest)
	}
	if rest, ok := strings.CutPrefix(ref, "module:"); ok {
		return path.Base(rest)
	}
	return ref
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
