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
	if fs.GroundTruth.DiagramMermaid != "" {
		diag := strings.TrimSpace(fs.GroundTruth.DiagramMermaid)
		sb.WriteString("```mermaid\n" + diag + "\n```\n\n")
	}
	for _, diagFact := range fs.GroundTruth.Diagrams {
		if strings.TrimSpace(diagFact.Content) != "" {
			diag := strings.TrimSpace(diagFact.Content)
			sb.WriteString("```mermaid\n" + diag + "\n```\n\n")
		}
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

	// 5. Database Schemas / Models
	if len(fs.GroundTruth.Schemas) > 0 {
		sb.WriteString("### Data Schemas\n\n")
		sb.WriteString("| Schema | Table | Definition | File |\n")
		sb.WriteString("| --- | --- | --- | --- |\n")
		for _, sc := range fs.GroundTruth.Schemas {
			tbl := sc.Table
			if tbl == "" {
				tbl = "-"
			}
			def := cleanTableString(sc.Definition)
			fileLink := formatFileLink(sc.File, sc.Line, "")
			sb.WriteString(fmt.Sprintf("| `%s` | %s | %s | %s |\n", sc.Name, tbl, def, fileLink))
		}
		sb.WriteString("\n")
	}

	// 6. Error Sentinels
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
		kindSet[k] = true
	}
	for _, s := range symbols {
		if kindSet[s.Kind] {
			filtered = append(filtered, s)
		}
	}
	return filtered
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
