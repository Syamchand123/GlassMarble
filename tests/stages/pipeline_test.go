package stages_test

// pipeline_test.go defines the shared phase-pipeline helper used by the
// phase 5/7/8 suites: it runs the REAL phases 1-4 (ingestion, normalization,
// aggregation, linking) over the harness sample project and commits the
// Linking output through the AKG transaction manager, returning the live
// graph. No CLI involved.

import (
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/ingest"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/normalize"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/aggregate"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/link"
	"github.com/Syamchand123/GlassMarble/tests/harness"
)

// analyzeProject builds the sample project into the sandbox, runs phases
// 1-4 over it, commits the Linking output via
// akg.NewAKGTransactionManager + ExecuteDeltaTransaction, and returns the
// transaction manager's active graph. Expensive (parses real source): call
// once per test, never inside a loop, never from a t.Parallel test.
func analyzeProject(t *testing.T, sb *harness.Sandbox) *akg.CodePropertyGraph {
	t.Helper()
	sb.SampleProject()

	cfg := ingest.DefaultConfig(sb.Root)
	ingestOut, err := ingest.RunIngestion(cfg)
	if err != nil {
		t.Fatalf("ingestion: %v", err)
	}

	payload, err := normalize.Normalize(ingestOut, "HEAD")
	if err != nil {
		t.Fatalf("normalization: %v", err)
	}

	aggregateOut, err := aggregate.Aggregate(payload, nil, sb.Root)
	if err != nil {
		t.Fatalf("aggregation: %v", err)
	}

	var modifiedFiles []string
	for rel := range payload.UpsertedTrees {
		modifiedFiles = append(modifiedFiles, rel)
	}
	modifiedFiles = append(modifiedFiles, payload.DeletedPaths...)

	linkOut, err := link.Link(aggregateOut, modifiedFiles, nil,
		link.LinkerConfig{LevelOfDetail: link.LevelArchitecture})
	if err != nil {
		t.Fatalf("linking: %v", err)
	}

	tm, err := akg.NewAKGTransactionManager(sb.GmDir)
	if err != nil {
		t.Fatalf("new transaction manager: %v", err)
	}
	if err := tm.ExecuteDeltaTransaction(linkOut, modifiedFiles); err != nil {
		t.Fatalf("commit delta transaction: %v", err)
	}
	return tm.GetActiveGraph()
}
