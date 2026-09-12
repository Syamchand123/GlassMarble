package cmd

import (
	"path/filepath"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	doc_engine "github.com/Syamchand123/GlassMarble/internal/doc_engine"
)

// runDocEngine runs the Documentation Intelligence Engine after graph commit.
// Non-fatal by design: failures are logged as warnings and do not abort analysis.
func runDocEngine(storageDir string, tm *akg.AKGTransactionManager, commitHash string, verbose bool, noLLM bool) {
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
