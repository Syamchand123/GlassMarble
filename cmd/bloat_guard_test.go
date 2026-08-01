package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage1"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage2"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage3"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage4"
)

// TestBloatRegressionGuard runs the REAL ingestion pipeline over this
// repository and fails when the node/edge counts exceed the budget
// (AUDIT Issue 5 Phase 5C-8: the bloat scoreboard must only go down, never
// up across refactors). Budgets were calibrated on the healthy baseline
// (2,308 nodes / 5,479 edges) with generous headroom, so a regression that
// adds a noisy node-producing pass trips this test.
func TestBloatRegressionGuard(t *testing.T) {
	repoRoot, err := findRepoRoot()
	if err != nil {
		t.Skipf("repository root not found: %v", err)
	}

	cfg := stage1.DefaultConfig(repoRoot)
	if _, err := os.Stat(filepath.Join(repoRoot, ".git")); err == nil {
		cfg.GitTrackedOnly = true
	}

	stage1Out, err := stage1.RunIngestion(cfg)
	if err != nil {
		t.Fatalf("stage 1 ingestion failed: %v", err)
	}

	stage2Payload, err := stage2.Normalize(stage1Out, "HEAD")
	if err != nil {
		t.Fatalf("stage 2 normalization failed: %v", err)
	}

	stage3Out, err := stage3.Aggregate(stage2Payload, nil)
	if err != nil {
		t.Fatalf("stage 3 aggregation failed: %v", err)
	}

	var modifiedFiles []string
	for relPath := range stage2Payload.UpsertedTrees {
		modifiedFiles = append(modifiedFiles, relPath)
	}
	modifiedFiles = append(modifiedFiles, stage2Payload.DeletedPaths...)

	// Architecture detail level = the default analysis configuration.
	cpg, err := stage4.Link(stage3Out, modifiedFiles, nil, stage4.LinkerConfig{LevelOfDetail: stage4.LevelArchitecture})
	if err != nil {
		t.Fatalf("stage 4 linker failed: %v", err)
	}

	nodes := len(cpg.GraphNodes)

	// The persistence layer collapses parallel edges sharing
	// (source, predicate, target) into one canonical triple, so the bloat
	// metric is the DEDUPLICATED edge count — the size the AKG actually
	// stores (5,479 on the healthy baseline).
	type keyT struct{ s, p, t string }
	seen := make(map[keyT]struct{})
	for _, edgesByType := range cpg.OutboundEdges {
		for _, e := range edgesByType {
			seen[keyT{e.SourceID, string(e.Type), e.TargetID}] = struct{}{}
		}
	}
	edges := len(seen)

	const (
		nodeBudget = 3500
		edgeBudget = 9000
	)
	if nodes < 1000 {
		t.Errorf("sanity: expected the pipeline to produce a substantial graph, got %d nodes (pipeline may be broken)", nodes)
	}
	if edges < 3000 {
		t.Errorf("sanity: expected a substantial edge count, got %d (pipeline may be broken)", edges)
	}
	if nodes > nodeBudget {
		t.Errorf("bloat regression: %d nodes exceeds the %d-node budget — a noisy node producer was added", nodes, nodeBudget)
	}
	if edges > edgeBudget {
		t.Errorf("bloat regression: %d edges exceeds the %d-edge budget — a noisy edge producer was added", edges, edgeBudget)
	}
}

// findRepoRoot walks up from the test working directory until it finds go.mod.
func findRepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", os.ErrNotExist
		}
		dir = parent
	}
}
