// Package grounding — facts.go
// Assembles the final, sanitized FactSheet for a section.
package grounding

import (
	"sort"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/link"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/catalog"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/config"
	doccontext "github.com/Syamchand123/GlassMarble/internal/doc_engine/grounding/context"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/grounding/resolve"
)

// AssembleFactSheet builds a complete, sanitized FactSheet for a single section.
func AssembleFactSheet(
	doc *config.DocSpec,
	sec *config.SectionSpec,
	graph *akg.CodePropertyGraph,
	dossier *config.GlobalCommitDossier,
	priorMarkdown string,
	repoRoot string,
) *config.FactSheet {
	if doc == nil {
		return nil
	}

	collector := NewCollector(graph)
	// Best-effort: unknown ground_with values surface as a collection error
	// for direct callers; the render pipeline proceeds with partial facts.
	payload, _ := collector.CollectSectionFacts(sec, &doc.Scope)

	// Filter dossier changes for this document's scope
	if dossier != nil {
		for _, added := range dossier.AddedSymbols {
			if catalog.MatchesScope(&doc.Scope, added.File) {
				payload.AddedSymbols = append(payload.AddedSymbols, added)
			}
		}
		for _, mod := range dossier.ModifiedSymbols {
			parts := strings.Split(mod.FQN, "::")
			if len(parts) > 1 && catalog.MatchesScope(&doc.Scope, parts[0]) {
				payload.ModifiedSymbols = append(payload.ModifiedSymbols, mod)
			}
		}
		for _, rem := range dossier.RemovedSymbols {
			parts := strings.Split(rem, "::")
			if len(parts) > 1 && catalog.MatchesScope(&doc.Scope, parts[0]) {
				payload.RemovedSymbols = append(payload.RemovedSymbols, rem)
			}
		}
		for _, cv := range dossier.AddedConfigVars {
			if catalog.MatchesScope(&doc.Scope, cv.File) {
				payload.ConfigVars = append(payload.ConfigVars, cv)
			}
		}
		for _, sf := range dossier.AddedSentinels {
			if catalog.MatchesScope(&doc.Scope, sf.File) {
				payload.Sentinels = append(payload.Sentinels, sf)
			}
		}
		for _, sd := range dossier.ModifiedSentinels {
			file := fqnFilePart(sd.FQN)
			if file == "" || catalog.MatchesScope(&doc.Scope, file) {
				payload.ModifiedSentinels = append(payload.ModifiedSentinels, sd)
			}
		}
		for _, rv := range dossier.RemovedConfigVars {
			payload.RemovedConfigVars = append(payload.RemovedConfigVars, rv)
		}
	}

	// Callers: inbound CPG call edges into added/modified symbols AND every
	// other payload symbol (B3: union of inbound CALLS sources over ALL
	// payload symbols — added, modified, in-scope, and full-owned — deduped,
	// sorted, and capped at maxPayloadCallers for prompt-budget safety).
	// CallFlow: entry points followed by their outbound callee chain.
	if graph != nil {
		seenCallers := make(map[string]bool)
		fqnSet := make(map[string]bool)
		for _, s := range payload.AddedSymbols {
			fqnSet[s.FQN] = true
		}
		for _, m := range payload.ModifiedSymbols {
			fqnSet[m.FQN] = true
		}
		for _, s := range payload.Symbols {
			fqnSet[s.FQN] = true
		}
		for _, s := range payload.AllSymbols {
			fqnSet[s.FQN] = true
		}
		for fqn := range fqnSet {
			if fqn == "" {
				continue
			}
			for _, e := range graph.GetInboundEdges(fqn) {
				if e.Type == link.EdgeCalls && e.SourceID != "" && !seenCallers[e.SourceID] {
					seenCallers[e.SourceID] = true
					payload.Callers = append(payload.Callers, e.SourceID)
				}
			}
		}
		sortStrings(payload.Callers)
		if len(payload.Callers) > maxPayloadCallers {
			payload.Callers = payload.Callers[:maxPayloadCallers]
		}
		for _, ep := range doc.Scope.EntryPoints {
			payload.CallFlow = append(payload.CallFlow, ep)
			for _, e := range graph.GetOutboundEdges(ep) {
				if e.Type == link.EdgeCalls {
					payload.CallFlow = append(payload.CallFlow, e.TargetID)
				}
			}
		}
	}

	// DocComments: FQN → doc comment index over every collected symbol.
	if payload.DocComments == nil {
		payload.DocComments = make(map[string]string)
	}
	for _, s := range payload.Symbols {
		if s.Doc != "" {
			payload.DocComments[s.FQN] = s.Doc
		}
	}
	for _, s := range payload.AllSymbols {
		if s.Doc != "" {
			payload.DocComments[s.FQN] = s.Doc
		}
	}
	for _, s := range payload.Sentinels {
		if s.Doc != "" {
			payload.DocComments[s.FQN] = s.Doc
		}
	}

	// B1 precision layer: upgrade symbol positions via SCIP → LSP → AST
	// resolution. Only SCIP/LSP answers override (higher provenance);
	// AST answers confirm what the collectors already extracted.
	// B2 relevance layer: PageRank-ranked neighborhood context within a
	// fixed token budget (default 1000, Aider --map-tokens pattern).
	// Both are deterministic: identical inputs → identical payloads, so
	// section hashes stay stable across runs.
	applyPrecisionLayer(doc, payload, graph, repoRoot)
	applyContextLayer(doc, payload, graph)

	// Render diagrams specified for this document
	for _, diagRef := range doc.Diagrams {
		rendered, err := GenerateDiagram(diagRef, graph)
		if err == nil && rendered != "" {
			payload.Diagrams = append(payload.Diagrams, config.DiagramFact{
				Type:    diagRef.Type,
				Content: rendered,
			})
		}
	}
	// Section-level diagram pointer: the first rendered diagram, injected
	// verbatim by the LLM actuator per the prompt contract.
	if len(payload.Diagrams) > 0 {
		payload.DiagramMermaid = payload.Diagrams[0].Content
	}

	// Self-heal permalinks in prior markdown
	updatedPrior := UpdatePermalinksInMarkdown(priorMarkdown, graph)

	sectionID := ""
	instruction := ""
	maxWords := 0
	if sec != nil {
		sectionID = sec.ID
		instruction = sec.Instruction
		maxWords = sec.MaxWords
	}

	sheet := &config.FactSheet{
		DocID:                doc.ID,
		SectionID:            sectionID,
		SectionInstruction:   instruction,
		MaxWords:             maxWords,
		GroundTruth:          *payload,
		PriorSectionMarkdown: updatedPrior,
	}

	// Enterprise Security Gate: Scrub all secrets and PII before passing to renderer/LLM
	return SanitizeFactSheet(sheet)
}

// fqnFilePart extracts the file portion of an FQN ("path::Name" → "path").
func fqnFilePart(fqn string) string {
	if idx := strings.Index(fqn, "::"); idx >= 0 {
		return fqn[:idx]
	}
	return ""
}

// maxPayloadCallers caps payload.Callers (union of inbound callers over all
// payload symbols) so caller context stays within prompt budget.
const maxPayloadCallers = 20

// sortStrings sorts in place for deterministic payloads.
func sortStrings(items []string) {
	sort.Strings(items)
}

// applyPrecisionLayer (plan B1) upgrades symbol positions via SCIP → LSP →
// AST resolution. Only higher-provenance answers (scip, lsp) override what
// the collectors extracted; AST answers merely confirm. Every symbol keeps
// a Provenance trail on the fact itself.
func applyPrecisionLayer(doc *config.DocSpec, payload *config.GroundTruthPayload, graph *akg.CodePropertyGraph, repoRoot string) {
	if payload == nil || doc == nil {
		return
	}
	var fqns []string
	seen := make(map[string]bool)
	collect := func(fqn string) {
		if fqn != "" && !seen[fqn] {
			seen[fqn] = true
			fqns = append(fqns, fqn)
		}
	}
	for i := range payload.Symbols {
		collect(payload.Symbols[i].FQN)
	}
	for i := range payload.AllSymbols {
		collect(payload.AllSymbols[i].FQN)
	}
	if len(fqns) == 0 {
		return
	}
	results := resolve.ResolveBatch(fqns, graph, repoRoot)
	upgrade := func(s *config.SymbolFact) {
		r, ok := results[s.FQN]
		if !ok {
			return
		}
		if r.Provenance == "scip" || r.Provenance == "lsp" {
			if r.File != "" {
				s.File = r.File
			}
			if r.Line > 0 {
				s.Line = r.Line
				end := r.EndLine
				if end < r.Line {
					end = r.Line
				}
				s.Permalink = FormatPermalink(r.File, r.Line, end)
			}
		}
		s.Provenance = provenanceOr(s.Provenance, r.Provenance)
	}
	for i := range payload.Symbols {
		upgrade(&payload.Symbols[i])
	}
	for i := range payload.AllSymbols {
		upgrade(&payload.AllSymbols[i])
	}
}

// provenanceOr prefers the higher-precision source: scip > lsp > ast.
func provenanceOr(current, next string) string {
	rank := map[string]int{"scip": 3, "lsp": 2, "ast": 1, "unresolved": 0}
	if rank[next] > rank[current] {
		return next
	}
	if current == "" {
		return next
	}
	return current
}

// payloadCovers reports whether a ranked symbol is already grounded in the
// payload (same FQN), to avoid duplicating facts.
func payloadCovers(payload *config.GroundTruthPayload, fqn string) bool {
	for _, s := range payload.Symbols {
		if s.FQN == fqn {
			return true
		}
	}
	for _, s := range payload.AllSymbols {
		if s.FQN == fqn {
			return true
		}
	}
	return false
}

// applyContextLayer (plan B2) appends PageRank-ranked neighborhood symbols
// (the callers/types that explain WHY, capped for prompt-budget safety) as
// Kind:"context" facts. Deterministic output keeps section hashes stable.
func applyContextLayer(doc *config.DocSpec, payload *config.GroundTruthPayload, graph *akg.CodePropertyGraph) {
	if payload == nil || doc == nil || graph == nil {
		return
	}
	var seeds []string
	for _, s := range payload.AddedSymbols {
		seeds = append(seeds, s.FQN)
	}
	for _, m := range payload.ModifiedSymbols {
		seeds = append(seeds, m.FQN)
	}
	seeds = append(seeds, doc.Scope.EntryPoints...)
	if len(seeds) == 0 {
		return
	}
	ranked := doccontext.SelectContext(graph, seeds, defaultContextBudgetTokens)
	added := 0
	for _, f := range ranked.Files {
		for _, sym := range f.Symbols {
			if added >= maxContextSymbols || payloadCovers(payload, sym.FQN) {
				continue
			}
			payload.Symbols = append(payload.Symbols, config.SymbolFact{
				FQN:        sym.FQN,
				Kind:       "context",
				File:       sym.File,
				Line:       sym.Line,
				Permalink:  FormatPermalink(sym.File, sym.Line, sym.Line),
				Signature:  sym.FQN,
				Provenance: "pagerank",
			})
			added++
		}
	}
}

const (
	// defaultContextBudgetTokens mirrors Aider's --map-tokens default scale
	// for relevance-ranked context allowances.
	defaultContextBudgetTokens = 1000
	// maxContextSymbols bounds FactSheet growth from the relevance layer.
	maxContextSymbols = 10
)
