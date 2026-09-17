// Package invalidator — diff_filter.go
// Implements the Stage 1 fast-bail evaluator and section-level invalidation logic.
package invalidator

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/link"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/catalog"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/config"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/storage"
	"github.com/Syamchand123/GlassMarble/internal/git"
)

// FastBail evaluates whether the documentation pipeline can safely exit immediately.
// Target latency: < 15ms.
//
// Fast-bail returns true (bail) when:
//  1. The commit hash is already processed in docs_state.json (and !force).
//  2. No code files in any doc's scope were touched by the commit.
//  3. The commit only modified markdown documentation files or tests, and no doc-spec targets.
func FastBail(repoDir string, commitHash string, cat *catalog.Catalog, state *storage.DocEngineState, force bool) (bail bool, changedFiles []string, reason string, err error) {
	if cat == nil || len(cat.Docs()) == 0 {
		return true, nil, "no documents configured in catalog", nil
	}

	// 1. Check if already processed
	if !force && commitHash != "" && state != nil && state.LastCommit == commitHash {
		return true, nil, "commit already processed", nil
	}

	if repoDir == "" || commitHash == "" {
		return false, nil, "", nil
	}

	// 2. Read changed files from commit
	meta, err := git.ReadCommit(repoDir, commitHash)
	if err != nil {
		// If git read fails, do not bail — proceed safely
		return false, nil, "", nil
	}

	changedFiles = meta.Files
	if len(changedFiles) == 0 {
		// Root commit or no files changed
		return false, changedFiles, "", nil
	}

	// 3. Fast path: commit only touched tests and/or docs → bail.
	// Pure string checks, no network, early returns (<15ms path).
	allTestOrDocs := true
	for _, f := range changedFiles {
		lf := strings.ToLower(f)
		if !(strings.HasSuffix(lf, "_test.go") || strings.HasSuffix(lf, ".md")) {
			allTestOrDocs = false
			break
		}
	}
	if allTestOrDocs && !force {
		return true, changedFiles, "commit only modified tests and/or documentation files", nil
	}

	// 4. Check if any NON-TEST file matches any doc scope.
	// _test.go files never count as scope matches on their own: a test-only
	// change inside a scope must bail unless a real scope file also changed.
	hasScopeMatch := false
	for _, file := range changedFiles {
		if strings.HasSuffix(strings.ToLower(file), "_test.go") {
			continue
		}
		for _, doc := range cat.Docs() {
			if cat.MatchesPath(doc.ID, file) {
				hasScopeMatch = true
				break
			}
		}
		if hasScopeMatch {
			break
		}
	}

	if !hasScopeMatch && !force {
		return true, changedFiles, "no files in any document's scope were touched", nil
	}

	// 5. Check if diff only contains documentation files (.md)
	allDocs := true
	for _, f := range changedFiles {
		if !strings.HasSuffix(strings.ToLower(f), ".md") {
			allDocs = false
			break
		}
	}
	if allDocs && !force {
		return true, changedFiles, "commit only modified documentation files", nil
	}

	return false, changedFiles, "", nil
}

// Invalidator coordinates reverse-index lookups and AST subgraph hash comparisons
// to discover the exact set of dirty sections.
type Invalidator struct {
	catalog *catalog.Catalog
	index   *catalog.ReverseIndex
}

// New constructs an Invalidator for the given Catalog.
func New(cat *catalog.Catalog) *Invalidator {
	return &Invalidator{
		catalog: cat,
		index:   catalog.NewReverseIndex(cat),
	}
}

// FindDirtySections evaluates which document sections are dirty for the given commit.
// It compares the newly computed AST subgraph hash against docs_state.json.
// Sections whose hash is unchanged produce 0 tokens and 0 disk writes.
func (inv *Invalidator) FindDirtySections(
	dossier *config.GlobalCommitDossier,
	state *storage.DocEngineState,
	headGraph *akg.CodePropertyGraph,
	constraints *config.GlobalConstraints,
) ([]config.DirtySectionRef, error) {
	if inv.catalog == nil || len(inv.catalog.Docs()) == 0 {
		return nil, nil
	}

	// 1. Find candidate dirty sections from changed files via ReverseIndex
	var candidates []config.DirtySectionRef
	if dossier != nil && len(dossier.AddedSymbols)+len(dossier.ModifiedSymbols)+len(dossier.RemovedSymbols) > 0 {
		var changedFiles []string
		for _, s := range dossier.AddedSymbols {
			if s.File != "" {
				changedFiles = append(changedFiles, s.File)
			}
		}
		for _, s := range dossier.ModifiedSymbols {
			// Modified symbol FQN might contain file prefix
			parts := strings.Split(s.FQN, "::")
			if len(parts) > 1 {
				changedFiles = append(changedFiles, parts[0])
			}
		}
		candidates = inv.index.FindDirtySectionsForFiles(changedFiles)
	}

	// If candidates is empty but we have documents, check all documents (e.g. initial run or full scan)
	if len(candidates) == 0 {
		for _, doc := range inv.catalog.Docs() {
			for _, sec := range doc.Sections {
				if sec.Managed && !sec.Freeze {
					candidates = append(candidates, config.DirtySectionRef{
						DocID:     doc.ID,
						DocPath:   doc.TargetPath,
						SectionID: sec.ID,
						Priority:  3,
						Reason:    "initial sync or full evaluation",
					})
				}
			}
		}
	}

	// 2. Perform AST subgraph hashing for each candidate section
	var confirmedDirty []config.DirtySectionRef

	for _, cand := range candidates {
		doc := inv.catalog.Get(cand.DocID)
		if doc == nil {
			continue
		}

		// Handle cascade rules for aggregate-level documents: an
		// aggregate doc (e.g. architecture.md) only invalidates on a
		// critical architectural event, by design — this is deliberate
		// even on the very first render, so a freshly scaffolded
		// architecture doc stays unpopulated until something actually
		// architecturally significant happens. Use --force to populate
		// one immediately (e.g. for initial setup).
		level := catalog.ClassifyDocLevel(doc)
		if level == catalog.DocLevelAggregate && dossier != nil {
			if !catalog.ShouldCascadeToAggregate(dossier.ArchEvents, dossier.CommitIntent) {
				// Skip aggregate update if no critical architectural events occurred
				continue
			}
		}

		// DocLevelRoot gate (mirrors the aggregate check): root docs
		// (README.md, docs/index.md) invalidate ONLY on public-surface
		// changes. Leaf behavior is unchanged.
		if level == catalog.DocLevelRoot && dossier != nil {
			if !isPublicSurfaceChange(dossier) {
				continue
			}
		}

		// Find SectionSpec
		var secSpec *config.SectionSpec
		for i := range doc.Sections {
			if doc.Sections[i].ID == cand.SectionID {
				secSpec = &doc.Sections[i]
				break
			}
		}

		// Collect symbols in scope from headGraph
		symbols := collectSymbolsForScope(&doc.Scope, headGraph)

		// Compute section hash
		currentHash := HashSection(secSpec, symbols, doc.Diagrams)

		// Compare with persisted hash in docs_state.json
		if state != nil {
			if ds, ok := state.Documents[doc.TargetPath]; ok && ds != nil {
				if ss, ok := ds.Sections[cand.SectionID]; ok && ss != nil {
					if ss.ASTSubgraphHash == currentHash && currentHash != "" {
						// Subgraph hash is identical -> facts haven't changed!
						// 0 tokens, 0 git diff.
						continue
					}
				}
			}
		}

		// Hash differs or not yet tracked -> dirty!
		// The Reason carries intent + change-kind context so the dossier
		// equivalent (DirtySections list) stays complete downstream.
		cand.Reason = dossierReason(dossier, cand)
		confirmedDirty = append(confirmedDirty, cand)
	}

	// 3. Enforce budget constraint
	maxUpdates := 10
	if constraints != nil && constraints.MaxDocUpdatesPerCommit > 0 {
		maxUpdates = constraints.MaxDocUpdatesPerCommit
	}
	return catalog.EnforceBudget(confirmedDirty, maxUpdates), nil
}

// structuralRootEvents lists the B3 structural dossier events that always
// invalidate root docs (defense in depth: even if the generic ArchEvents
// check below ever narrows, cycle/layering/public-surface deltas must still
// reach README-level documents).
var structuralRootEvents = map[string]bool{
	"CYCLE_INTRODUCED":       true,
	"CYCLE_RESOLVED":         true,
	"LAYER_VIOLATION":        true,
	"PUBLIC_SURFACE_CHANGED": true,
}

// isPublicSurfaceChange reports whether the dossier carries a public-surface
// change: an added/removed EXPORTED symbol under cmd/ paths, any
// architectural events, or — explicitly — any structural cycle/layering/
// public-surface event.
func isPublicSurfaceChange(dossier *config.GlobalCommitDossier) bool {
	if dossier == nil {
		return false
	}
	for _, ev := range dossier.ArchEvents {
		if structuralRootEvents[strings.ToUpper(strings.TrimSpace(ev))] {
			return true
		}
	}
	if len(dossier.ArchEvents) > 0 {
		return true
	}
	for _, s := range dossier.AddedSymbols {
		if isExportedShortName(s.FQN) && isCmdPath(s.File) {
			return true
		}
		if s.File == "" && isExportedShortName(s.FQN) && isCmdPath(fqnFilePart(s.FQN)) {
			return true
		}
	}
	for _, fqn := range dossier.RemovedSymbols {
		if isExportedShortName(fqn) && isCmdPath(fqnFilePart(fqn)) {
			return true
		}
	}
	for _, mod := range dossier.ModifiedSymbols {
		if isExportedShortName(mod.FQN) && isCmdPath(fqnFilePart(mod.FQN)) {
			return true
		}
	}
	return false
}

// SectionHash recomputes the exact Stage-3 hash for one section using the
// same inputs FindDirtySections compares against (scope symbols + diagram
// refs). The render pipeline calls this post-render so the persisted hash
// matches what the next run recomputes — without it every rendered section
// would read as dirty again and the zero-churn guarantee would collapse.
func SectionHash(doc *config.DocSpec, sec *config.SectionSpec, graph *akg.CodePropertyGraph) string {
	if doc == nil || sec == nil {
		return ""
	}
	return HashSection(sec, collectSymbolsForScope(&doc.Scope, graph), doc.Diagrams)
}

// dossierReason builds the rich per-section Reason string carrying the commit
// intent and the change kind breakdown from the dossier.
func dossierReason(dossier *config.GlobalCommitDossier, cand config.DirtySectionRef) string {
	if dossier == nil {
		return fmt.Sprintf("AST subgraph hash modified for section %q (no dossier context)", cand.SectionID)
	}
	intent := dossier.CommitIntent
	if intent == "" {
		intent = "unknown"
	}
	return fmt.Sprintf("AST subgraph hash modified (intent=%s; +%d added/~%d modified/-%d removed symbols; +%d/±%d sentinels; %d arch events; section %s in %s)",
		intent,
		len(dossier.AddedSymbols),
		len(dossier.ModifiedSymbols),
		len(dossier.RemovedSymbols),
		len(dossier.AddedSentinels),
		len(dossier.ModifiedSentinels),
		len(dossier.ArchEvents),
		cand.SectionID,
		cand.DocPath,
	)
}

// isExportedShortName reports whether an FQN's short symbol name is exported
// (starts with an uppercase letter).
func isExportedShortName(fqn string) bool {
	name := fqn
	if idx := strings.LastIndex(name, "::"); idx >= 0 {
		name = name[idx+2:]
	}
	if idx := strings.LastIndex(name, "."); idx >= 0 {
		name = name[idx+1:]
	}
	if idx := strings.LastIndex(name, "/"); idx >= 0 {
		name = name[idx+1:]
	}
	if name == "" {
		return false
	}
	return unicode.IsUpper([]rune(name)[0])
}

// isCmdPath reports whether a repo-relative path lives under cmd/.
func isCmdPath(p string) bool {
	norm := strings.ReplaceAll(strings.TrimSpace(p), "\\", "/")
	norm = strings.TrimPrefix(norm, "./")
	return norm == "cmd" || strings.HasPrefix(norm, "cmd/") || strings.Contains(norm, "/cmd/")
}

// fqnFilePart extracts the file portion of an FQN ("path::Name" → "path").
func fqnFilePart(fqn string) string {
	if idx := strings.Index(fqn, "::"); idx >= 0 {
		return fqn[:idx]
	}
	return fqn
}

// collectSymbolsForScope filters nodes in headGraph that fall within scope.Paths.
func collectSymbolsForScope(scope *config.ScopeRule, graph *akg.CodePropertyGraph) []config.SymbolFact {
	if scope == nil || graph == nil || graph.Nodes == nil {
		return nil
	}

	var symbols []config.SymbolFact
	graph.Nodes.Iterate(func(id string, n *link.ResolvedNode) {
		if n == nil {
			return
		}
		path := n.FileSpec.Path
		if path == "" {
			return
		}

		// Check if node file matches scope
		if catalog.MatchesScope(scope, path) {
			fact := config.SymbolFact{
				FQN:  id,
				Kind: string(n.Kind),
				File: path,
				Line: n.FileSpec.LineStart,
			}
			populateSymbolFact(&fact, n)
			symbols = append(symbols, fact)
		}
	})

	return symbols
}
