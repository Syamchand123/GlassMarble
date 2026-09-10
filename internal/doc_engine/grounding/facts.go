// Package grounding — facts.go
// Assembles the final, sanitized FactSheet for a section.
package grounding

import (
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/akg"
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
