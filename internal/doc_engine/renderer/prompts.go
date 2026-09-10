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
