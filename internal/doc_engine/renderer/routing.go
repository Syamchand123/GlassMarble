// Package renderer — routing.go
//
// v1 heuristic model routing (gap D5): RouteForFactSheet decides, from
// ground-truth shape and section-instruction keywords already present on the
// FactSheet, whether a section wants LLM prose ("llm") or is better served
// by the cheap deterministic renderer ("deterministic").
//
// Routing rule (deliberate bias: default "llm", only positive deterministic
// signals reroute, so existing behavior is preserved unless a section is
// clearly tabular):
//
//   - deterministic when the instruction asks for tabular/list output
//     ("table of", "list of/all", "enumerate", "catalog", "matrix",
//     "reference" as a noun phrase) — tables don't need prose; OR when the
//     ground-truth payload is large (>routingDeterministicTokenFloor
//     estimated tokens) AND the instruction carries no narrative verbs
//     (large dumps render faithfully as tables, expensively as prose).
//   - "llm" otherwise, especially on narrative verbs (explain, describe,
//     guide, discuss, why, tutorial, walkthrough, overview, compare).
//
// Wiring without engine changes: LLMActuator.Render checks the route first
// and returns a "routed to deterministic" error for deterministic sections.
// The orchestrator's existing Track A→B fallback ladder (engine.go,
// forbidden to this change) catches ANY actuator error — warning +
// deterministic render + fallback counter — so routing REUSES the fallback
// ladder with zero engine edits. Repair is never rerouted: it is an explicit
// follow-up to an LLM attempt.
package renderer

import (
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/doc_engine/config"
)

// RouteLLM and RouteDeterministic are the RouteForFactSheet outcomes.
const (
	RouteLLM           = "llm"
	RouteDeterministic = "deterministic"
)

// routingDeterministicTokenFloor is the estimated-token size above which a
// non-narrative section prefers the deterministic renderer.
const routingDeterministicTokenFloor = 2000

// routingDeterministicPhrases are instruction markers for tabular output.
var routingDeterministicPhrases = []string{
	"table of",
	"list of",
	"list all",
	"enumerate",
	"catalog",
	"matrix",
	"reference table",
	"api reference",
}

// routingNarrativeVerbs are instruction markers for prose output.
var routingNarrativeVerbs = []string{
	"explain",
	"describe",
	"guide",
	"discuss",
	"tutorial",
	"walkthrough",
	"walk through",
	"overview",
	"compare",
	" rationale",
	" trade-off",
	" tradeoff",
	" why ",
}

// RouteForFactSheet returns RouteDeterministic or RouteLLM for fs.
// A nil FactSheet routes to LLM (no signal → preserve current behavior).
func RouteForFactSheet(fs *config.FactSheet) string {
	if fs == nil {
		return RouteLLM
	}
	instr := strings.ToLower(fs.SectionInstruction)
	for _, p := range routingDeterministicPhrases {
		if strings.Contains(instr, p) {
			return RouteDeterministic
		}
	}
	if hasRoutingNarrativeVerb(instr) {
		return RouteLLM
	}
	if EstimateTokens(fs) > routingDeterministicTokenFloor {
		return RouteDeterministic
	}
	return RouteLLM
}

// hasRoutingNarrativeVerb reports whether the lowered instruction carries a
// narrative verb. "why" needs word boundaries ("why" inside "anyway" must
// not route); other verbs match as substrings.
func hasRoutingNarrativeVerb(instr string) bool {
	for _, v := range routingNarrativeVerbs {
		t := strings.TrimSpace(v)
		if t == "why" {
			for _, word := range strings.FieldsFunc(instr, func(r rune) bool {
				return r < 'a' || r > 'z'
			}) {
				if word == "why" {
					return true
				}
			}
			continue
		}
		if strings.Contains(instr, t) {
			return true
		}
	}
	return false
}

// EstimateTokens approximates the LLM input size for a FactSheet at ~4
// chars/token over the instruction, prior body, and ground-truth counts.
// Heuristic only (v1 routing input, not billing).
func EstimateTokens(fs *config.FactSheet) int {
	if fs == nil {
		return 0
	}
	chars := len(fs.SectionInstruction) + len(fs.PriorSectionMarkdown)
	gt := fs.GroundTruth
	chars += len(gt.DiagramMermaid)
	for _, s := range gt.AddedSymbols {
		chars += len(s.FQN) + len(s.Signature) + len(s.Doc)
	}
	for _, s := range gt.AllSymbols {
		chars += len(s.FQN) + len(s.Signature) + len(s.Doc)
	}
	for _, s := range gt.Symbols {
		chars += len(s.FQN) + len(s.Signature) + len(s.Doc)
	}
	for _, d := range gt.ModifiedSymbols {
		chars += len(d.FQN) + len(d.Before) + len(d.After)
	}
	chars += len(gt.RemovedSymbols) * 40
	chars += len(gt.CallFlow) * 40
	chars += len(gt.Callers) * 40
	for _, v := range gt.DocComments {
		chars += len(v)
	}
	for _, c := range gt.ConfigVars {
		chars += len(c.Name) + len(c.Doc)
	}
	for _, c := range gt.AddedConfigVars {
		chars += len(c.Name) + len(c.Doc)
	}
	for _, e := range gt.Sentinels {
		chars += len(e.FQN) + len(e.Value) + len(e.Doc)
	}
	for _, e := range gt.AddedSentinels {
		chars += len(e.FQN) + len(e.Value) + len(e.Doc)
	}
	for _, s := range gt.ArchEvents {
		chars += len(s)
	}
	return chars / 4
}
