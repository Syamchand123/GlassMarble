// Package renderer — prompts.go
// Prompt templates for Track A LLM Prose Actuator.
// Adheres strictly to Appendix A and Section 8 Stage 6 of the Master Plan.
package renderer

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/doc_engine/config"
)

// reservedStructuralHeadings are the section headings the renderer appends
// automatically after the LLM's prose (see deterministic.go's
// RenderReferenceAppendix / renderReferenceTables). The model must never
// write these itself — see BuildSystemPrompt rule 3 — or the finished
// section would show the same table or list twice.
var reservedStructuralHeadings = []string{
	"Endpoints", "Call Flow", "Direct Callers",
	"Functions and Methods", "Types and Interfaces", "Error Catalog",
	"Configuration Variables", "Architectural Milestones", "Recent Symbol Changes",
}

// BuildSystemPrompt constructs the immutable system prompt for the LLM actuator.
// Rules are numbered programmatically so the sequence is always gap-free
// (1-8 when maxWords==0, 1-9 otherwise).
//
// The model's job is the EXPLANATION only: clear, human-readable prose
// describing what the code does, why, and how the pieces relate — the
// reason this tool exists is to produce documentation a person can
// actually read, not a re-serialization of the fact sheet as a table. A
// grounded reference table and any diagram are appended automatically by
// the deterministic renderer (see RenderReferenceAppendix) — never asked
// of the model — so the two halves never duplicate or contradict each
// other, and the model's entire output budget goes to the writing itself.
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

	// Tone rides on the style rule (not its own number) so the numbering
	// shape never shifts.
	styleRule := fmt.Sprintf("Follow the style: %s.", voice)
	if tone != "" {
		styleRule = fmt.Sprintf("Follow the style: %s. Tone: %s.", voice, tone)
	}

	rules := []string{
		"Every function, type, error, or identifier you mention in backticks MUST appear in the fact_sheet.ground_truth. Do not reference any code entity not listed there.",
		"This grounding requirement is NOT limited to backtick-quoted identifiers — it covers every specific factual claim: an algorithm or cipher name (SHA-256, AES-GCM), a policy detail (a key-rotation interval, a token lifetime), a referenced document (an ADR or ticket number), a standard or protocol version. If ground_truth contains no evidence for a specific detail, do not state it in plain prose either, even if it is the kind of detail such a system typically has — describing what a system \"usually\" does instead of what THIS one's ground_truth actually shows is fabrication, not documentation. When ground_truth for this section is empty or has nothing relevant to SECTION INSTRUCTION below, say so plainly (e.g. \"No cryptographic primitives were found in this codebase's current scope\") instead of inventing a plausible-sounding architecture to fill the space.",
		"The reverse is NOT true: ground_truth commonly carries symbols of only marginal relevance to this section — broader-scope context pulled in for completeness, not because they belong in this section's explanation. Write about what SECTION INSTRUCTION below actually asks for. Do not feel obligated to mention, enumerate, or explicitly rule out (\"X is not a config variable\", \"Y does not belong here either\") every symbol merely because it happens to appear in ground_truth.",
		"When you name a code symbol, use its bare short name in backticks — `envInt`, `def`, `Port` — never the full qualified path or compound key from ground_truth (never write things like `pkg/config/config.go::envInt::param:def` or `file:pkg/config/config.go`). Those compound keys are internal identifiers for machine lookup, not English; if you need to describe a relationship between two symbols, say it in words, e.g. \"the `def` parameter of `envInt`\", not by quoting the compound key itself.",
		"Write PROSE ONLY: real explanatory paragraphs in plain English describing what this section covers, why it works the way it does, and how the pieces named in ground_truth fit together. Do not write markdown tables, a bullet-dump list of every symbol, or fenced ```mermaid``` diagram blocks — the tool appends a complete reference table and any diagram automatically, right after your text. Writing them yourself duplicates them.",
		fmt.Sprintf("Never write any of these heading lines yourself, in any form or wording — the tool adds them separately, always after your prose: %s.", strings.Join(quoteEach(reservedStructuralHeadings), ", ")),
		"ground_truth's added/modified/removed symbol lists are cross-cutting \"what changed recently\" reference data, already rendered as their own \"Recent Symbol Changes\" list right after your text — same as the reserved headings above. Do not restate, list, or summarize them in your prose (e.g. do not write a sentence like \"Recently added symbols include...\") unless the section instruction below is specifically about tracking or auditing recent changes.",
		"Do not rewrite or rephrase sentences from PRIOR PROSE that are still factually accurate. Only add, modify, or remove sentences that directly reflect the changes in ground_truth. Exception: this never excuses a violation of the other rules above — if a sentence in PRIOR PROSE quotes a compound key, narrates an irrelevant symbol, restates the recent-changes list, or otherwise breaks one of them, fix or remove that sentence even though the fact it's based on hasn't changed. \"Still factually accurate\" is not the same as \"already compliant.\"",
		"Output ONLY the prose markdown for the specified section. No preamble, no \"Here is the updated section\", no meta-commentary.",
		"Never narrate your reasoning, planning, or interpretation of this prompt. Do not write sentences about the task itself (e.g. \"We need to produce...\", \"The instruction says...\", \"Let's think about...\"). If you reason internally, keep it out of the output entirely — respond with the finished markdown ONLY, starting directly with its first sentence of real content.",
		styleRule,
		fmt.Sprintf("Avoid these words/phrases: %s.", jargon),
	}
	if maxWords > 0 {
		rules = append(rules, fmt.Sprintf("Keep this section under %d words.", maxWords))
	}

	var numbered strings.Builder
	for i, r := range rules {
		fmt.Fprintf(&numbered, "%d. %s\n", i+1, r)
	}

	return fmt.Sprintf(`You are a technical writer embedded in GlassMarble, an architecture intelligence tool. Real engineers read what you write to understand a codebase, so it must read like documentation a person wrote — not a re-formatted dump of data.
Your ONLY job is to write the EXPLANATION for the specified section, in your own words, grounded strictly in the provided JSON fact sheet.

HARD RULES — these are absolute and non-negotiable:
%s`, strings.TrimRight(numbered.String(), "\n"))
}

// quoteEach wraps each string in double quotes, for a readable inline list
// in a prompt (e.g. ["Reference", "Endpoints"] -> [`"Reference"`, `"Endpoints"`]).
func quoteEach(ss []string) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = fmt.Sprintf("%q", s)
	}
	return out
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

	// Only the PROSE half of the prior content goes to the model — never
	// the reference appendix (deterministic.go's RenderReferenceAppendix),
	// which the renderer regenerates fresh from the current fact sheet
	// every time regardless of what the model writes. Showing the model
	// its own old reference tables as "prior content to incrementally
	// edit" would invite it to rewrite or duplicate them despite rule 3
	// forbidding exactly that.
	prior := stripReferenceAppendix(fs.PriorSectionMarkdown)
	if strings.TrimSpace(prior) == "" {
		prior = "(No prior prose. Write the section's explanation from scratch.)"
	}

	return fmt.Sprintf(`FACT SHEET:
%s

SECTION INSTRUCTION:
%s

PRIOR PROSE (update this, do not recreate from scratch; a reference table and any diagram are appended separately and are not shown here):
%s

Output the updated section's explanatory prose now — no tables, no diagrams, no reserved headings:`,
		string(factBytes), instruction, prior), nil
}

// compoundIdentifierRe matches a backtick-quoted span that is unmistakably
// an AKG compound machine key rather than a short, human-readable name:
// it contains the "::" member separator, or carries a "file:"/"module:"
// pseudo-symbol prefix. Deliberately narrow — it must never fire on an
// ordinary short identifier, a plain file path someone legitimately
// quotes (e.g. `` `pkg/config/config.go` ``, which reads fine as-is), or
// prose punctuation.
var compoundIdentifierRe = regexp.MustCompile("`(file:[^`\\s]+|module:[^`\\s]+|[^`\\s]*::[^`\\s]*)`")

// humanizeCompoundIdentifiers is a deterministic backstop applied to the
// model's raw prose before gating: BuildSystemPrompt rule 3 tells the
// model to always use a symbol's bare short name rather than the raw
// compound key ground_truth carries it under (e.g. write `envInt`, never
// `pkg/config/config.go::envInt::param:def`) — but prompt compliance is
// never guaranteed, and a live-tested run showed the model occasionally
// still quoting the raw compound form despite the rule's explicit
// negative example. This rewrites the CONTENT of any such backtick span
// to symbolShortName's short form without touching the surrounding
// sentence, so grammar stays intact either way — "the `file:pkg/config/
// config.go` symbol" becomes "the `config.go` symbol", not a rewritten
// sentence.
func humanizeCompoundIdentifiers(text string) string {
	return compoundIdentifierRe.ReplaceAllStringFunc(text, func(m string) string {
		inner := m[1 : len(m)-1]
		return "`" + symbolShortName(inner) + "`"
	})
}

// stripReferenceAppendix removes a trailing reference-appendix block (see
// deterministic.go's RenderReferenceAppendix / referenceAppendixMarker)
// from previously-rendered section markdown, leaving just the prose the
// model actually authored.
func stripReferenceAppendix(markdown string) string {
	prose, _ := splitProseAndAppendix(markdown)
	return prose
}

// splitProseAndAppendix splits previously-rendered section markdown into
// the model-authored prose and the deterministic reference appendix (see
// deterministic.go's RenderReferenceAppendix), inclusive of its
// referenceAppendixMarker separator. appendix is "" when there is no such
// block. The two recombine as combineProseAndAppendix(prose, appendix).
func splitProseAndAppendix(markdown string) (prose, appendix string) {
	idx := strings.Index(markdown, referenceAppendixMarker)
	if idx < 0 {
		return markdown, ""
	}
	return strings.TrimRight(markdown[:idx], "\n"), markdown[idx:]
}

// SplitProseAndAppendix is the exported form of splitProseAndAppendix, for
// callers outside this package that need to compare just the grounded
// reference-appendix half of a section against a freshly-rendered one
// (e.g. operations.go's Diff, which previews changes without an LLM call
// and so can only ever meaningfully compare the deterministic half).
func SplitProseAndAppendix(markdown string) (prose, appendix string) {
	return splitProseAndAppendix(markdown)
}

// combineProseAndAppendix joins prose and a reference appendix (as
// returned by splitProseAndAppendix, or built fresh by
// DeterministicRenderer.RenderReferenceAppendix) into final section
// content, blank-line separated. appendix may be "".
func combineProseAndAppendix(prose, appendix string) string {
	content := strings.TrimRight(prose, "\n")
	if strings.TrimSpace(appendix) != "" {
		content += "\n\n" + strings.TrimRight(appendix, "\n")
	}
	return content + "\n"
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
