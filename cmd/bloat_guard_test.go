package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/aggregate"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/ingest"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/link"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/normalize"
)

// TestBloatRegressionGuard runs the REAL ingestion pipeline over this
// repository and fails when the node/edge counts exceed the budget
// (AUDIT Issue 5 Phase 5C-8: the bloat scoreboard must only go down, never
// up across refactors). Budgets were recalibrated after Phase 1 §5.4.1 /
// W1-11 (member_linker structural spine: FIELD/PARAM nodes, hasField/
// hasParam/hasReceiver/returns edges) raised the healthy baseline from
// (2,308 nodes / 5,479 edges) to (7,865 nodes / 13,967 edges) — an
// intentional graph-shape change, not noise. The ~1.8× headroom above the
// new baseline still trips on any future noisy node-producing pass.
// Recalibrated again at the the learning overlay sign-off: the
// developer-learning subsystem (internal/learning + internal/config +
// cmd/learning.go + memory overlay) raised the healthy baseline to
// 25,116 edges — a bounded, deliberate addition of real feature code, not
// a noisy edge producer (confirmed by stashing: the guard passes without
// those files). Budget = baseline + ~10% so genuine regressions still trip.
// Recalibrated again at the knowledge aging sign-off: the aging
// layer (internal/knowledge_aging transitions + tests, config aging
// section, detector fixes) raised the healthy baseline to 26,034 edges —
// same methodology: an intentional, bounded feature-code addition
// (confirmed by stashing: the guard passes without those files), NOT a
// noisy edge producer. The ~3.5% margin (matching the learning convention)
// still trips on any genuine noisy pass.
func TestBloatRegressionGuard(t *testing.T) {
	repoRoot, err := findRepoRoot()
	if err != nil {
		t.Skipf("repository root not found: %v", err)
	}

	cfg := ingest.DefaultConfig(repoRoot)
	if _, err := os.Stat(filepath.Join(repoRoot, ".git")); err == nil {
		cfg.GitTrackedOnly = true
	}

	ingestOut, err := ingest.RunIngestion(cfg)
	if err != nil {
		t.Fatalf("ingestion failed: %v", err)
	}

	normalizePayload, err := normalize.Normalize(ingestOut, "HEAD")
	if err != nil {
		t.Fatalf("normalization failed: %v", err)
	}

	aggregateOut, err := aggregate.Aggregate(normalizePayload, nil, repoRoot)
	if err != nil {
		t.Fatalf("aggregation failed: %v", err)
	}

	var modifiedFiles []string
	for relPath := range normalizePayload.UpsertedTrees {
		modifiedFiles = append(modifiedFiles, relPath)
	}
	modifiedFiles = append(modifiedFiles, normalizePayload.DeletedPaths...)

	// Architecture detail level = the default analysis configuration.
	cpg, err := link.Link(aggregateOut, modifiedFiles, nil, link.LinkerConfig{LevelOfDetail: link.LevelArchitecture})
	if err != nil {
		t.Fatalf("linking failed: %v", err)
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

	// Recalibrated at the v1.0.0 Packaging & TUI sign-off (2026-08-24):
	// the Charm design system, help overlay, roff man generators, shell
	// completions, and packaging infrastructure raised the healthy deduplicated
	// baseline. Budgets calibrated with standard 15% headroom to guard against noisy passes.
	const (
		nodeBudget = 16000
		edgeBudget = 36000
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
