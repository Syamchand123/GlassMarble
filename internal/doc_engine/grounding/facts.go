// Package grounding — facts.go
// Assembles the final, sanitized FactSheet for a section.
package grounding

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/link"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/catalog"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/config"
	doccontext "github.com/Syamchand123/GlassMarble/internal/doc_engine/grounding/context"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/grounding/resolve"
)

// rankedBySection carries the PageRank-ranked file map from applyContextLayer
// (which has no Out writer and cannot extend FactSheet — spec.go is frozen)
// to the renderer, which prints the top entries when Verbose (gap B2c).
//
// Key: doc.TargetPath + "\x00" + sectionID. A sync.Map (not a package-global
// single slot) so concurrent section rendering (GMB_DOC_PARALLEL) never mixes
// sections: each render stores under its own key and the serial merge phase
// takes it back right after. Entries are single-flight: TakeRankedMap deletes
// on read, so a stale entry can never leak into a later run.
var rankedBySection sync.Map // key string → []string of "path(score)"

// rankedMapKey builds the sync.Map key for one rendered section.
func rankedMapKey(docPath, sectionID string) string {
	return docPath + "\x00" + sectionID
}

// storeRankedMap records the ranked file list for one section (B2c emission).
func storeRankedMap(docPath, sectionID string, files []string) {
	rankedBySection.Store(rankedMapKey(docPath, sectionID), files)
}

// TakeRankedMap returns the ranked file list ("path(score)" strings,
// score-descending) stored for one section and clears the slot.
// ok=false when nothing was stored (nil graph, no seeds, or already taken).
func TakeRankedMap(docPath, sectionID string) (files []string, ok bool) {
	v, ok := rankedBySection.LoadAndDelete(rankedMapKey(docPath, sectionID))
	if !ok {
		return nil, false
	}
	files, _ = v.([]string)
	return files, true
}

// sectionWants reports whether sec's ground_with directives (after the same
// alias resolution CollectSectionFacts applies) request the given canonical
// directive. An empty GroundWith defaults to signatures+exported_symbols
// (CollectSectionFacts' own default), matching what that section would
// actually collect.
func sectionWants(sec *config.SectionSpec, canonical string) bool {
	directives := []string{"signatures", "exported_symbols"}
	if sec != nil && len(sec.GroundWith) > 0 {
		directives = sec.GroundWith
	}
	for _, d := range directives {
		if got, skip := resolveGroundWith(d); !skip && got == canonical {
			return true
		}
	}
	return false
}

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

	collector := NewCollector(graph).WithRepoRoot(repoRoot)
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
		// ConfigVars/Sentinels/ModifiedSentinels feed the section's own
		// PRIMARY content tables ("Configuration Variables", "Error
		// Catalog") — unlike AddedSymbols/ModifiedSymbols/RemovedSymbols
		// above, which only ever surface in the separately-labeled
		// "Recent Symbol Changes" block (a deliberate, clearly-marked
		// cross-cutting "what changed" note, fine to populate regardless
		// of this section's own focus). Merging dossier config-var/
		// sentinel deltas into these tables UNCONDITIONALLY — as this did
		// before — meant a section whose own ground_with never asked for
		// config_vars/sentinels at all (e.g. architecture's "overview",
		// grounded only in arch_intelligence+dependencies) could still
		// render a full Configuration Variables table sourced entirely
		// from the dossier, indistinguishable from real section content.
		// Worse, since the dossier's Added/Modified set varies per commit
		// (whatever else happened to be touched), the exact same
		// unchanged section could show a different table on every
		// regeneration. Gating on whether the section actually requested
		// that kind of grounding keeps these tables a pure function of
		// the section's own scope and ground_with, not of incidental
		// per-commit dossier contents.
		if sectionWants(sec, "config_vars") {
			for _, cv := range dossier.AddedConfigVars {
				if catalog.MatchesScope(&doc.Scope, cv.File) {
					payload.ConfigVars = append(payload.ConfigVars, cv)
				}
			}
			for _, rv := range dossier.RemovedConfigVars {
				payload.RemovedConfigVars = append(payload.RemovedConfigVars, rv)
			}
		}
		if sectionWants(sec, "sentinels") {
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
		}
	}

	// Callers: inbound CPG call edges into every payload symbol (B3: union
	// of inbound CALLS sources over in-scope and full-owned symbols,
	// deduped, sorted, and capped at maxPayloadCallers for prompt-budget
	// safety). CallFlow: entry points followed by their outbound callee
	// chain.
	//
	// Deliberately built ONLY from Symbols/AllSymbols (the section's stable
	// steady-state facts), never from the dossier's AddedSymbols/
	// ModifiedSymbols. Those are the current graph's full node set diffed
	// against whatever base graph happened to be available for THIS
	// commit — on a genesis run (no base graph yet) that is literally
	// every symbol in the repo, most of them outside this section's
	// scope/ground_with focus entirely. Seeding the caller search from them
	// let unrelated, unexported helpers (e.g. a private helper with no
	// error/interface relevance) leak into "Direct Callers" on whichever
	// commit happened to touch them, then silently drop back out on the
	// next commit once they were no longer "added" — the same section,
	// regenerated from an unchanged symbol, showing a different caller list
	// depending on incidental per-commit dossier contents. Restricting the
	// seed to Symbols/AllSymbols makes Callers a pure function of the
	// current graph and this section's scope, stable across regenerations.
	if graph != nil {
		seenCallers := make(map[string]bool)
		fqnSet := make(map[string]bool)
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
	applyContextLayer(doc, sec, payload, graph)

	// Render diagrams specified for this document
	for _, diagRef := range doc.Diagrams {
		// callgraph/sequence diagrams need a root entry point; the
		// archetype-configured DiagramRef (docs.yaml's diagrams: list)
		// never carries one on its own — only the doc's Scope.EntryPoints
		// does. Without this, EVERY document's default diagrams: list
		// rendered "No call graph edges detected" even when EntryPoints
		// was set, because GenerateDiagram received an empty ref.Entry
		// regardless. (A separate, opt-in inline `gmb:diagram` directive
		// path in renderer/engine.go already does this same auto-fill —
		// this mirrors it for the default, doc-config-driven diagram list
		// every archetype actually uses.)
		if (strings.EqualFold(diagRef.Type, "callgraph") || strings.EqualFold(diagRef.Type, "sequence")) &&
			diagRef.Entry == "" && len(doc.Scope.EntryPoints) > 0 {
			diagRef.Entry = doc.Scope.EntryPoints[0]
		}
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

// maxCrossRepoLookups caps cross-repo (GMB_EXTRA_REPOS) sidecar lookups per
// factsheet so one section with many unresolved symbols cannot fan out
// across every extra checkout.
const maxCrossRepoLookups = 20

// sortStrings sorts in place for deterministic payloads.
func sortStrings(items []string) {
	sort.Strings(items)
}

// applyPrecisionLayer (plan B1) upgrades symbol positions via SCIP → LSP →
// AST resolution, plus a cross-repo SCIP pass (gap B1e): after
// ResolveBatch, still-unresolved symbols whose FQN contains "/" (a
// repo-qualified hint, e.g. "otherrepo/pkg/file.go::Symbol") are looked up
// in the GMB_EXTRA_REPOS sidecars via resolve.ResolveCrossRepo. At most
// maxCrossRepoLookups cross-repo lookups run per factsheet (budget guard),
// in fqns order so results stay deterministic. Only higher-provenance
// answers (scip, scip:xrepo, lsp) override what the collectors extracted;
// AST answers merely confirm. Every symbol keeps a Provenance trail on the
// fact itself.
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
	// B1e cross-repo fallback: still-unresolved, repo-qualified hints only.
	// Deterministic: iterate fqns (deduped input order), not the map.
	if extra := resolve.ExtraRepoRoots(); len(extra) > 0 {
		lookups := 0
		for _, fqn := range fqns {
			if lookups >= maxCrossRepoLookups {
				break
			}
			r, ok := results[fqn]
			if !ok || (r.Provenance != resolve.ProvenanceUnresolved && r.Provenance != "") {
				continue
			}
			if !strings.Contains(fqn, "/") {
				continue
			}
			lookups++
			if xr := resolve.ResolveCrossRepo(fqn, extra, graph); xr.Provenance == resolve.ProvenanceCrossRepo {
				results[fqn] = xr
			}
		}
	}
	upgrade := func(s *config.SymbolFact) {
		r, ok := results[s.FQN]
		if !ok {
			return
		}
		if r.Provenance == "scip" || r.Provenance == "scip:xrepo" || r.Provenance == "lsp" {
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

// provenanceOr prefers the higher-precision source:
// scip/scip:xrepo > lsp > ast > unresolved.
func provenanceOr(current, next string) string {
	rank := map[string]int{"scip": 3, "scip:xrepo": 3, "lsp": 2, "ast": 1, "unresolved": 0}
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
//
// B2c emission: the ranked file map is also stashed in rankedBySection keyed
// by doc.TargetPath+sectionID (TakeRankedMap) so the renderer can print the
// top files when Verbose. sec may be nil (direct callers); then nothing is
// stashed but the payload layer still applies.
func applyContextLayer(doc *config.DocSpec, sec *config.SectionSpec, payload *config.GroundTruthPayload, graph *akg.CodePropertyGraph) {
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
	// Stash the ranked file map for verbose emission (score-descending,
	// same order SelectContext returned). Stored even when no new symbols
	// are appended — the map is still the honest relevance signal.
	if sec != nil {
		listed := make([]string, 0, len(ranked.Files))
		for _, f := range ranked.Files {
			listed = append(listed, fmt.Sprintf("%s(%.4f)", f.Path, f.Score))
		}
		storeRankedMap(doc.TargetPath, sec.ID, listed)
	}
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
