package cmd

import (
	"path/filepath"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	doc_engine "github.com/Syamchand123/GlassMarble/internal/doc_engine"
)

// runDocEngine runs the Documentation Intelligence Engine after graph commit.
// Non-fatal by design: failures are logged as warnings and do not abort analysis.
//
// baseGraph is the pre-commit graph snapshot (captured by the caller before
// ExecuteDeltaTransaction promotes its shadow). Without it, doc_engine.Run's
// dossier (invalidator.BuildDossier -> akg.DiffGraphs(opts.BaseGraph, ...))
// diffs against nil, so DiffGraphs' baseNodes map is empty and every symbol
// in the ENTIRE codebase reads as "added" — on every single commit, not
// just the first. That dossier feeds every regenerated section's "Recent
// Symbol Changes" block (renderer/deterministic.go), so it would otherwise
// list every exported symbol in scope as newly added forever, and
// `gmb doc diff` could never match a doc's real prior content either. May
// be nil (first-ever analyze run) — DiffGraphs treats that the same as an
// empty graph, which is the correct genesis behavior.
func runDocEngine(storageDir string, tm *akg.AKGTransactionManager, baseGraph *akg.CodePropertyGraph, commitHash string, verbose bool, noLLM bool) {
	if tm == nil {
		return
	}
	repoDir := filepath.Dir(storageDir)

	graph := tm.GetActiveGraph()
	if graph == nil || graph.Nodes == nil || graph.Nodes.Len() == 0 {
		return
	}

	opts := doc_engine.RunOptions{
		CommitHash: commitHash,
		Verbose:    verbose,
		HeadGraph:  graph,
		BaseGraph:  baseGraph,
		NoLLM:      noLLM,
		Out:        tuiOut,
	}

	res := doc_engine.Run(repoDir, opts)
	if res.Err != nil && verbose {
		tuiPrintf("warning: doc engine: %v\n", res.Err)
	}
	for _, w := range res.Warnings {
		if verbose {
			tuiPrintf("warning: doc engine: %s\n", w)
		}
	}
	if res.DocsUpdated > 0 {
		tuiPrintf("Documentation: updated %d section(s) in %d document(s)\n", res.SectionsUpdated, res.DocsUpdated)
	}
}
