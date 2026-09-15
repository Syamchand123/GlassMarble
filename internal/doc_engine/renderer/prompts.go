// Package renderer — prompts.go
// Prompt templates for Track A LLM Prose Actuator.
// Adheres strictly to Appendix A and Section 8 Stage 6 of the Master Plan.
package renderer

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/doc_engine/config"
)

// BuildSystemPrompt constructs the immutable system prompt for the LLM actuator.
// Rules are numbered programmatically so the sequence is always gap-free
// (1-6 when maxWords==0, 1-7 otherwise).
func BuildSystemPrompt(style *config.StyleSpec, maxWords int) string {
	voice := "active, second-person, present tense"
	tone := ""
	jargon := "simply, just, leverage, obviously, utilize"

	if style != nil {
		if style.Voice != "" {
			voice = style.Voice
		}
		if style.Tone != "" {
			tone = style.Tone
		}
		if len(style.JargonBlacklist) > 0 {
			jargon = strings.Join(style.JargonBlacklist, ", ")
		}
	}

	// Tone rides on the style rule (not its own number) so the 1-6/1-7
	// numbering shape never shifts.
	styleRule := fmt.Sprintf("Follow the style: %s.", voice)
	if tone != "" {
		styleRule = fmt.Sprintf("Follow the style: %s. Tone: %s.", voice, tone)
	}

	rules := []string{
		"Every function, type, error, or identifier you mention in backticks MUST appear in the fact_sheet.ground_truth. Do not reference any code entity not listed there.",
		"Do not rewrite or rephrase sentences from prior_section_markdown that are still factually accurate. Only add, modify, or remove sentences that directly reflect the changes in ground_truth.",
		"Output ONLY the markdown for the specified section. No preamble, no \"Here is the updated section\", no meta-commentary.",
		styleRule,
		fmt.Sprintf("Avoid these words/phrases: %s.", jargon),
	}
	if maxWords > 0 {
		rules = append(rules, fmt.Sprintf("Keep this section under %d words.", maxWords))
	}
	rules = append(rules, "Embed the diagram verbatim if fact_sheet.ground_truth.diagram_mermaid is non-empty.")

	var numbered strings.Builder
	for i, r := range rules {
		fmt.Fprintf(&numbered, "%d. %s\n", i+1, r)
	}

	return fmt.Sprintf(`You are a technical documentation writer embedded in GlassMarble, an architecture intelligence tool.
Your ONLY job is to convert the provided JSON fact sheet into accurate, concise markdown prose for the specified section.

HARD RULES — these are absolute and non-negotiable:
%s`, strings.TrimRight(numbered.String(), "\n"))
}

// BuildUserPrompt constructs the user prompt containing the serialized FactSheet.
func BuildUserPrompt(fs *config.FactSheet) (string, error) {
	if fs == nil {
		return "", fmt.Errorf("doc_engine/prompts: nil FactSheet")
	}

	factBytes, err := json.MarshalIndent(fs, "", "  ")
	if err != nil {
		return "", fmt.Errorf("doc_engine/prompts: marshaling FactSheet: %w", err)
	}

	instruction := fs.SectionInstruction
	if instruction == "" {
		instruction = "Document the current state and recent changes accurately."
	}
	// PromptGuidance (e.g. the D2 Diátaxis quadrant contract) is LLM-only
	// steering text, deliberately kept out of SectionInstruction so Track B
	// never renders it to a reader — see PromptGuidance's doc comment.
	if fs.PromptGuidance != "" {
		instruction += "\n\n" + fs.PromptGuidance
	}

	prior := fs.PriorSectionMarkdown
	if strings.TrimSpace(prior) == "" {
		prior = "(No prior section content. Generate initial section documentation.)"
	}

	return fmt.Sprintf(`FACT SHEET:
%s

SECTION INSTRUCTION:
%s

PRIOR SECTION CONTENT (update this, do not recreate from scratch):
%s

Output the updated section markdown now:`,
		string(factBytes), instruction, prior), nil
}

// BuildRepairPrompt constructs a follow-up correction prompt when a quality gate fails.
func BuildRepairPrompt(fs *config.FactSheet, prevOutput string, gateErr error) string {
	errMsg := "Validation failed"
	if gateErr != nil {
		errMsg = gateErr.Error()
	}

	return fmt.Sprintf(`Your previous markdown output for section %q failed the GlassMarble Quality Firewall validation:
Error: %s

Previous output:
---
%s
---

Please correct the section markdown to completely resolve this error:
1. Ensure EVERY backtick-quoted identifier (`+"`SymbolName`"+`) appears explicitly in the fact_sheet.ground_truth. Do not reference any code entity not listed there.
2. Do NOT invent, assume, or hallucinate any function, method, parameter, type, or error identifier.
3. Fix any unclosed fenced code blocks or malformed table rows.
4. Output ONLY the corrected markdown for the section:`,
		fs.SectionID, errMsg, prevOutput)
}

// ────────────────────────────────────────────────────────────────────────────
// D2: Diátaxis quadrant prompts, compass check, reference completeness
// (ADD ONLY — existing code above untouched)
// ────────────────────────────────────────────────────────────────────────────

// QuadrantPrompt returns 2-4 sentences of quadrant-specific writing guidance
// for the Diátaxis quadrant ("tutorial", "how-to", "reference", "explanation").
// Unknown or empty quadrants return "".
func QuadrantPrompt(quadrant string) string {
	switch strings.ToLower(strings.TrimSpace(quadrant)) {
	case "reference":
		return "Reference documentation must be dry and complete: document every symbol in the code's own structure and order. " +
			"Mirror the code structure so each exported symbol is easy to locate from its definition site. " +
			"Do not use chatty prose, narrative, or tutorial language."
	case "tutorial":
		return "Tutorials teach by doing: guide the reader through an end-to-end lesson with verifiable steps. " +
			"Each step must be executable in order and produce an observable result before moving on. " +
			"Assume no prior familiarity and confirm progress at every stage of the lesson."
	case "how-to":
		return "How-to guides are goal-shaped: state the goal up front, list prerequisites, then give the minimal ordered steps. " +
			"End with a verifiable outcome so the reader knows the goal has been achieved. " +
			"Skip background explanation and focus on action."
	case "explanation":
		return "Explanations answer why: clarify the concepts, decisions, and context behind the design. " +
			"Link claims to ADRs, history, and trade-offs rather than listing steps or tables. " +
			"Do not give instructions; build understanding."
	default:
		return ""
	}
}

// compassTutorialVerbs are tutorial-flavored phrases that contradict a
// reference quadrant section instruction.
var compassTutorialVerbs = []string{"learn", "walk through", "getting started", "step-by-step"}

// compassActionVerbs are action verbs expected in tutorial/how-to titles
// and instructions.
var compassActionVerbs = []string{
	"use", "run", "create", "configure", "build",
	"deploy", "test", "verify", "migrate", "install",
}

// containsCompassActionVerb reports whether s contains one of the action
// verbs as a whole word (lowercase input). Whole-word matching avoids
// substring false positives such as "use" inside "because".
func containsCompassActionVerb(s string) bool {
	for _, w := range strings.FieldsFunc(s, func(r rune) bool {
		return r < 'a' || r > 'z'
	}) {
		for _, v := range compassActionVerbs {
			if w == v {
				return true
			}
		}
	}
	return false
}

// CompassCheck flags form-vs-quadrant contradictions between a section and
// its Diátaxis quadrant: reference instructions containing tutorial verbs,
// tutorial/how-to sections with no action verb in title+instruction, and
// explanation instructions that are purely tabular. Empty or unknown
// quadrants yield nil. Results are deterministic (input-ordered).
func CompassCheck(sectionTitle, instruction, quadrant string) []string {
	q := strings.ToLower(strings.TrimSpace(quadrant))
	if q == "" {
		return nil
	}
	lowerInstr := strings.ToLower(instruction)
	combined := strings.ToLower(sectionTitle + "\n" + instruction)
	var failures []string
	switch q {
	case "reference":
		for _, v := range compassTutorialVerbs {
			if strings.Contains(lowerInstr, v) {
				failures = append(failures, fmt.Sprintf("compass: reference section %q instruction contradicts quadrant (tutorial language %q)", sectionTitle, v))
			}
		}
	case "tutorial", "how-to":
		if !containsCompassActionVerb(combined) {
			failures = append(failures, fmt.Sprintf("compass: %s section %q has no action verb in title or instruction", q, sectionTitle))
		}
	case "explanation":
		if strings.Contains(lowerInstr, "table of") || strings.Contains(lowerInstr, "list of") {
			failures = append(failures, fmt.Sprintf("compass: explanation section %q instruction is purely tabular", sectionTitle))
		}
	default:
		return nil
	}
	return failures
}

// shortSymbolName strips package qualifiers ("a/b::T.M" → "M") so prose can
// be matched on the short name. Case is preserved (matching is case-sensitive).
func shortSymbolName(sym string) string {
	s := sym
	if i := strings.LastIndex(s, "::"); i >= 0 {
		s = s[i+2:]
	}
	if i := strings.LastIndex(s, "/"); i >= 0 {
		s = s[i+1:]
	}
	if i := strings.LastIndex(s, "."); i >= 0 {
		s = s[i+1:]
	}
	return s
}

// ReferenceCompleteness reports one "symbol %q not documented" failure per
// symbol absent from prose. A symbol counts as documented when either its
// full form or its short name appears verbatim (case-sensitive), backticked
// or plain. Empty symbol lists yield nil. Results follow input order.
func ReferenceCompleteness(symbols []string, prose string) []string {
	if len(symbols) == 0 {
		return nil
	}
	var missing []string
	for _, sym := range symbols {
		if sym == "" {
			continue
		}
		if strings.Contains(prose, sym) {
			continue
		}
		if short := shortSymbolName(sym); short != sym && short != "" && strings.Contains(prose, short) {
			continue
		}
		missing = append(missing, fmt.Sprintf("symbol %q not documented", sym))
	}
	return missing
}
