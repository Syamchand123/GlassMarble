package cmd

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/arch_intelligence"
	"github.com/Syamchand123/GlassMarble/internal/arch_timeline"
	"github.com/Syamchand123/GlassMarble/internal/archmodel"
	"github.com/Syamchand123/GlassMarble/internal/commit_reasoning"
	"github.com/Syamchand123/GlassMarble/internal/config"
	"github.com/Syamchand123/GlassMarble/internal/developer_memory"
	"github.com/Syamchand123/GlassMarble/internal/git"
)

// runMemoryPipeline is the intelligence + memory wiring point (master plan §13.1).
// It runs architectural intelligence on the committed graph exactly once,
// then: (wrapper with no snapshot opts for backward compat)
func runMemoryPipeline(storageDir string, tm *akg.AKGTransactionManager, commitHash string, verbose bool) {
	runMemoryPipelineWithSnapshotOpts(storageDir, tm, commitHash, verbose, false, 0)
}

func runMemoryPipelineWithSnapshotOpts(storageDir string, tm *akg.AKGTransactionManager, commitHash string, verbose bool, forceNoGraph bool, snapshotKeep int) {
	// Force NoGraph from CLI flag takes precedence
	if forceNoGraph {
		// will be handled in buildAndStoreSnapshot via noGraph param
	}
	//
//  1. persists the intelligence result to .glassmarble/intelligence/latest.json,
//  2. builds an ArchSnapshot and stores it in .glassmarble/snapshots/
//     (skip-writes when the topology is unchanged),
//  3. generates ArchEvents by diffing against the previous snapshot
//     (skipped on the very first analysis — there is nothing to diff
//     against, and GenerateEvents requires a non-nil base),
//  4. folds the events into developer memory (.glassmarble/memory/),
//     idempotently — re-analyzing the same commit never duplicates memory.
//
// The entire phase is non-fatal (§15.6): a failure here warns and continues,
// because `gmb analyze` must never fail after the graph is committed.
graph := tm.GetActiveGraph()
	if graph == nil || graph.Nodes == nil || graph.Nodes.Len() == 0 {
		return
	}

	res := runIntelligence(graph, storageDir, verbose)

	tuiPrintf("Intelligence: %d components | %d patterns | %d smells | %d cycles | %d layer violations\n",
		len(res.Components), len(res.Patterns), len(res.Smells), res.Metrics.CycleCount, res.Metrics.LayerViolationCount)
	for _, p := range res.Patterns {
		tuiPrintf("  pattern: %s (confidence %.2f)\n", p.Name, p.Confidence)
	}
	for _, s := range res.Smells {
		tuiPrintf("  smell: [%s] %s\n", s.Severity, s.Title)
	}

	// 1. Persist the intelligence result (watch mode also updates this file on
	// uncommitted saves — it is the "current state" contract).
	if err := writeIntelligenceLatest(storageDir, res); err != nil && verbose {
		tuiPrintf("warning: could not persist intelligence/latest.json: %v\n", err)
	}

	// 2. Snapshot. Failures here are warnings: snapshots and memory are
	// derived state, not the graph. One store instance for the whole phase —
	// two instances could disagree on the index mid-run.
	store, err := arch_timeline.NewSnapshotStore(snapshotDir(storageDir))
	if err != nil {
		if verbose {
			tuiPrintf("warning: snapshot store unavailable: %v\n", err)
		}
		return
	}
	prevSnap, _ := store.Latest()

	// Effective no-graph: CLI flag wins over auto-threshold.
	effectiveNoGraph := forceNoGraph
	snap, _, err := buildAndStoreSnapshotWithKeep(filepath.Dir(storageDir), graph, commitHash, res, store, effectiveNoGraph, snapshotKeep)
	if err != nil {
		if verbose {
			tuiPrintf("warning: snapshot build failed: %v\n", err)
		}
		return
	}

	// 3+4. Event generation + memory ingestion. commit reasoning
	// runs FIRST: its events share the component inference event-ID scheme and are enriched
	// with intent, PR/issue refs and impact, and the memory builder keeps
	// the first occurrence — so the reasoned events win the dedup and the
	// identical component inference twins are dropped (master plan §13.1).
	var events []archmodel.ArchEvent
	var graphDiff *akg.GraphDiff
	if prevSnap != nil && commitHash != "" {
		cfg := config.DefaultIntelligenceConfig()
		if local, lerr := loadIntelligenceConfig(storageDir); lerr == nil {
			cfg = local
		}
		extractor := commit_reasoning.NewIntentExtractor()
		if cfg.LLMIntentEnabled {
			if llm := newIntentLLM(filepath.Dir(storageDir)); llm != nil {
				extractor = commit_reasoning.NewIntentExtractor(commit_reasoning.WithLLM(llm))
			}
		}
		reasoner := commit_reasoning.NewReasoner(
			commit_reasoning.WithConfig(cfg),
			commit_reasoning.WithLayerForbidden(cfgForbiddenPairs(storageDir)),
			commit_reasoning.WithIntentExtractor(extractor),
		)
		// Replay the previous snapshot's graph so cycle/layer rules have a
		// base state; nil when the previous snapshot was --no-graph.
		baseGraph, _ := arch_timeline.Replay(prevSnap)
		if baseGraph != nil {
			graphDiff = akg.DiffGraphs(baseGraph, graph)
		}
		reasoned, rerr := reasoner.ReasonCommit(context.Background(), commit_reasoning.ReasonInput{
			RepoDir:    filepath.Dir(storageDir),
			CommitHash: commitHash,
			BaseSnap:   prevSnap,
			HeadSnap:   snap,
			GraphDiff:  graphDiff,
			BaseGraph:  baseGraph,
			HeadGraph:  graph,
		})
		if rerr != nil {
			if verbose {
				tuiPrintf("warning: commit reasoning failed: %v\n", rerr)
			}
		} else {
			events = append(events, reasoned...)
			if len(reasoned) > 0 {
				tuiPrintf("Commit reasoning: reasoned %d architectural change(s)\n", len(reasoned))
			}
		}

		events = append(events, arch_intelligence.GenerateEvents(prevSnap, snap, graphDiff, arch_intelligence.CommitMeta{
			Hash:      commitHash,
			Timestamp: snap.Timestamp,
		})...)
	} else if verbose {
		tuiPrintln("Memory: no previous snapshot — skipping event generation (first analysis)")
	}

	store6 := developer_memory.NewStoreForRepo(filepath.Dir(storageDir)).WithLogger(func(format string, args ...any) {
		tuiPrintf("warning: "+format+"\n", args...)
	})
	builder := developer_memory.NewMemoryBuilderWithOptions(store6,
		developer_memory.WithProjectID(projectIDFor(filepath.Dir(storageDir))))

	appended, err := builder.ProcessEvents(events)
	if err != nil {
		tuiPrintf("warning: developer memory ingestion failed: %v\n", err)
		return
	}
	if appended > 0 {
		tuiPrintf("Memory: recorded %d architectural event(s) into developer memory\n", appended)
	} else if len(events) == 0 {
		if verbose {
			tuiPrintln("Memory: no architectural changes since the previous analysis")
		}
	}
}

// runIntelligence runs the Architecture Intelligence engine once on a committed graph with the
// repository's intelligence configuration.
func runIntelligence(graph *akg.CodePropertyGraph, storageDir string, verbose bool) arch_intelligence.IntelligenceResult {
	cfg := config.DefaultIntelligenceConfig()
	if local, lerr := loadIntelligenceConfig(storageDir); lerr == nil {
		cfg = local
	}
	opts := []arch_intelligence.EngineOption{
		arch_intelligence.WithConfig(cfg),
		arch_intelligence.WithLayerForbidden(cfgForbiddenPairs(storageDir)),
	}
	if verbose {
		opts = append(opts, arch_intelligence.WithLogger(func(format string, args ...any) {
			tuiPrintf(format+"\n", args...)
		}))
	}
	return arch_intelligence.NewEngineWithOptions(graph, opts...).Run()
}

// buildAndStoreSnapshot builds an ArchSnapshot (backward compat, no keep override).
func buildAndStoreSnapshot(repoDir string, graph *akg.CodePropertyGraph, commitHash string, res arch_intelligence.IntelligenceResult, store *arch_timeline.SnapshotStore, noGraph bool) (*archmodel.ArchSnapshot, bool, error) {
	return buildAndStoreSnapshotWithKeep(repoDir, graph, commitHash, res, store, noGraph, 0)
}

func buildAndStoreSnapshotWithKeep(repoDir string, graph *akg.CodePropertyGraph, commitHash string, res arch_intelligence.IntelligenceResult, store *arch_timeline.SnapshotStore, noGraph bool, keepOverride int) (*archmodel.ArchSnapshot, bool, error) {
	ts := time.Now().UTC()
	var order int64
	if commitHash != "" && repoDir != "" {
		if ct, err := git.GetCommitTimestamp(repoDir, commitHash); err == nil {
			ts = ct
		}
		if o, err := git.GetCommitOrder(repoDir, commitHash); err == nil {
			order = o
		}
	}

	// Auto-threshold: if caller did not explicitly request NoGraph, consult
	// intelligence config (RCA-1). Large graphs (>15k nodes or >8 MB) auto-
	// omit the embedded graph to keep snapshots ~KB instead of 50 MB.
	if !noGraph {
		storageDir := filepath.Join(repoDir, ".glassmarble")
		if cfg, err := loadIntelligenceConfig(storageDir); err == nil {
			nodes := 0
			if graph != nil && graph.Nodes != nil {
				nodes = graph.Nodes.Len()
			}
			stateBytes := int64(nodes * 1300) // avg compact bytes/node
			if cfg.SnapshotShouldOmitGraph(nodes, stateBytes) {
				noGraph = true
			}
		}
	}

	snap, err := arch_timeline.BuildSnapshot(arch_timeline.SnapshotInput{
		Graph:      graph,
		CommitHash: commitHash,
		Timestamp:  ts,
		Order:      order,
		Components: res.Components,
		Patterns:   res.Patterns,
		Smells:     res.Smells,
		Metrics:    res.Metrics,
		NoGraph:    noGraph,
	})
	if err != nil {
		return nil, false, err
	}
	// Retention cap from intelligence config (P1), CLI override wins.
	maxCount := 0
	if keepOverride > 0 {
		maxCount = keepOverride
	} else if cfg, err := loadIntelligenceConfig(filepath.Join(repoDir, ".glassmarble")); err == nil && cfg.SnapshotMaxCount > 0 {
		maxCount = cfg.SnapshotMaxCount
	}
	var wrote bool
	if maxCount > 0 {
		wrote, err = store.CreateWithOptions(snap, arch_timeline.SnapshotCreateOptions{MaxCount: maxCount})
	} else {
		wrote, err = store.Create(snap)
	}
	if err != nil {
		return nil, false, err
	}
	return snap, wrote, nil
}

// snapshotDir is the .glassmarble/snapshots directory.
func snapshotDir(storageDir string) string {
	return filepath.Join(storageDir, "snapshots")
}

// writeIntelligenceLatest persists the intelligence result to
// .glassmarble/intelligence/latest.json via an atomic temp+rename write.
func writeIntelligenceLatest(storageDir string, res arch_intelligence.IntelligenceResult) error {
	dir := filepath.Join(storageDir, "intelligence")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(res)
	if err != nil {
		return err
	}
	path := filepath.Join(dir, "latest.json")
	tmp, err := os.CreateTemp(dir, ".latest.json.tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// projectIDFor derives the stable project identifier for a repository:
// sha256 of its absolute path (master plan §1.5). Deterministic across runs
// and machines with the same checkout location.
func projectIDFor(absDir string) string {
	sum := sha256.Sum256([]byte(absDir))
	return "proj_" + hex.EncodeToString(sum[:8])
}
