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
)

// AssembleFactSheet builds a complete, sanitized FactSheet for a single section.
func AssembleFactSheet(
	doc *config.DocSpec,
	sec *config.SectionSpec,
	graph *akg.CodePropertyGraph,
	dossier *config.GlobalCommitDossier,
	priorMarkdown string,
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

	// Callers: inbound CPG call edges into added/modified symbols.
	// CallFlow: entry points followed by their outbound callee chain.
	if graph != nil {
		seenCallers := make(map[string]bool)
		changedFQNs := make([]string, 0, len(payload.AddedSymbols)+len(payload.ModifiedSymbols))
		for _, s := range payload.AddedSymbols {
			changedFQNs = append(changedFQNs, s.FQN)
		}
		for _, m := range payload.ModifiedSymbols {
			changedFQNs = append(changedFQNs, m.FQN)
		}
		for _, fqn := range changedFQNs {
			for _, e := range graph.GetInboundEdges(fqn) {
				if e.Type == link.EdgeCalls && !seenCallers[e.SourceID] {
					seenCallers[e.SourceID] = true
					payload.Callers = append(payload.Callers, e.SourceID)
				}
			}
		}
		sortStrings(payload.Callers)
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

// sortStrings sorts in place for deterministic payloads.
func sortStrings(items []string) {
	sort.Strings(items)
}
