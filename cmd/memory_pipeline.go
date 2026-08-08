package cmd

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/arch_intelligence"
	"github.com/Syamchand123/GlassMarble/internal/arch_timeline"
	"github.com/Syamchand123/GlassMarble/internal/archmodel"
	"github.com/Syamchand123/GlassMarble/internal/config"
	"github.com/Syamchand123/GlassMarble/internal/developer_memory"
	"github.com/Syamchand123/GlassMarble/internal/git"
)

// runMemoryStage is the Stage 5 + Stage 6 wiring point (master plan §13.1).
// It runs architectural intelligence on the committed graph exactly once,
// then:
//
//  1. persists the Stage 5 result to .glassmarble/intelligence/latest.json,
//  2. builds an ArchSnapshot and stores it in .glassmarble/snapshots/
//     (skip-writes when the topology is unchanged),
//  3. generates ArchEvents by diffing against the previous snapshot
//     (skipped on the very first analysis — there is nothing to diff
//     against, and GenerateEvents requires a non-nil base),
//  4. folds the events into developer memory (.glassmarble/memory/),
//     idempotently — re-analyzing the same commit never duplicates memory.
//
// The entire stage is non-fatal (§15.6): a failure here warns and continues,
// because `gmb analyze` must never fail after the graph is committed.
func runMemoryStage(storageDir string, tm *akg.AKGTransactionManager, commitHash string, verbose bool) {
	graph := tm.GetActiveGraph()
	if graph == nil || graph.Nodes == nil || graph.Nodes.Len() == 0 {
		return
	}

	res := runIntelligence(graph, storageDir, verbose)

	fmt.Printf("Stage 5: %d components | %d patterns | %d smells | %d cycles | %d layer violations\n",
		len(res.Components), len(res.Patterns), len(res.Smells), res.Metrics.CycleCount, res.Metrics.LayerViolationCount)
	for _, p := range res.Patterns {
		fmt.Printf("  pattern: %s (confidence %.2f)\n", p.Name, p.Confidence)
	}
	for _, s := range res.Smells {
		fmt.Printf("  smell: [%s] %s\n", s.Severity, s.Title)
	}

	// 1. Persist the Stage 5 result (watch mode also updates this file on
	// uncommitted saves — it is the "current state" contract).
	if err := writeIntelligenceLatest(storageDir, res); err != nil && verbose {
		fmt.Printf("warning: could not persist intelligence/latest.json: %v\n", err)
	}

	// 2. Snapshot. Failures here are warnings: snapshots and memory are
	// derived state, not the graph. One store instance for the whole stage —
	// two instances could disagree on the index mid-run.
	store, err := arch_timeline.NewSnapshotStore(snapshotDir(storageDir))
	if err != nil {
		if verbose {
			fmt.Printf("warning: snapshot store unavailable: %v\n", err)
		}
		return
	}
	prevSnap, _ := store.Latest()

	snap, _, err := buildAndStoreSnapshot(filepath.Dir(storageDir), graph, commitHash, res, store, false)
	if err != nil {
		if verbose {
			fmt.Printf("warning: snapshot build failed: %v\n", err)
		}
		return
	}

	// 3+4. Event generation + memory ingestion.
	var events []archmodel.ArchEvent
	if prevSnap != nil {
		events = arch_intelligence.GenerateEvents(prevSnap, snap, nil, arch_intelligence.CommitMeta{
			Hash:      commitHash,
			Timestamp: snap.Timestamp,
		})
	} else if verbose {
		fmt.Println("Stage 6: no previous snapshot — skipping event generation (first analysis)")
	}

	store6 := developer_memory.NewStoreForRepo(filepath.Dir(storageDir)).WithLogger(func(format string, args ...any) {
		fmt.Printf("warning: "+format+"\n", args...)
	})
	builder := developer_memory.NewMemoryBuilderWithOptions(store6,
		developer_memory.WithProjectID(projectIDFor(filepath.Dir(storageDir))))

	appended, err := builder.ProcessEvents(events)
	if err != nil {
		fmt.Printf("warning: developer memory ingestion failed: %v\n", err)
		return
	}
	if appended > 0 {
		fmt.Printf("Stage 6: recorded %d architectural event(s) into developer memory\n", appended)
	} else if len(events) == 0 {
		if verbose {
			fmt.Println("Stage 6: no architectural changes since the previous analysis")
		}
	}
}

// runIntelligence runs the Stage 5 engine once on a committed graph with the
// repository's intelligence configuration.
func runIntelligence(graph *akg.CodePropertyGraph, storageDir string, verbose bool) arch_intelligence.Stage5Result {
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
			fmt.Printf(format+"\n", args...)
		}))
	}
	return arch_intelligence.NewEngineWithOptions(graph, opts...).Run()
}

// buildAndStoreSnapshot builds an ArchSnapshot for the committed graph and
// persists it through store (skip-writing when the topology is unchanged —
// see SnapshotStore.Create). The snapshot timestamp is the commit's author
// time (master plan §5.5 / D3), so snapshots and the timeline are ordered by
// when the change happened, not when analysis ran; uncommitted states (watch
// mode) fall back to now. The git-history order hint (rev-list --count)
// keeps same-second commits correctly ordered. Returns the snapshot and
// whether a file was written.
func buildAndStoreSnapshot(repoDir string, graph *akg.CodePropertyGraph, commitHash string, res arch_intelligence.Stage5Result, store *arch_timeline.SnapshotStore, noGraph bool) (*archmodel.ArchSnapshot, bool, error) {
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
	wrote, err := store.Create(snap)
	if err != nil {
		return nil, false, err
	}
	return snap, wrote, nil
}

// snapshotDir is the .glassmarble/snapshots directory.
func snapshotDir(storageDir string) string {
	return filepath.Join(storageDir, "snapshots")
}

// writeIntelligenceLatest persists the Stage 5 result to
// .glassmarble/intelligence/latest.json via an atomic temp+rename write.
func writeIntelligenceLatest(storageDir string, res arch_intelligence.Stage5Result) error {
	dir := filepath.Join(storageDir, "intelligence")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(res, "", "  ")
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
