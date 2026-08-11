package stages_test

// pipeline_test.go defines the shared stage-pipeline helper used by the
// stage 5/7/8 suites: it runs the REAL stages 1-4 (ingestion, normalization,
// aggregation, linking) over the harness sample project and commits the
// Stage 4 output through the AKG transaction manager, returning the live
// graph. No CLI involved.

import (
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage1"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage2"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage3"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage4"
	"github.com/Syamchand123/GlassMarble/tests/harness"
)

// analyzeProject builds the sample project into the sandbox, runs stages
// 1-4 over it, commits the Stage 4 output via
// akg.NewAKGTransactionManager + ExecuteDeltaTransaction, and returns the
// transaction manager's active graph. Expensive (parses real source): call
// once per test, never inside a loop, never from a t.Parallel test.
func analyzeProject(t *testing.T, sb *harness.Sandbox) *akg.CodePropertyGraph {
	t.Helper()
	sb.SampleProject()

	cfg := stage1.DefaultConfig(sb.Root)
	stage1Out, err := stage1.RunIngestion(cfg)
	if err != nil {
		t.Fatalf("stage 1 ingestion: %v", err)
	}

	payload, err := stage2.Normalize(stage1Out, "HEAD")
	if err != nil {
		t.Fatalf("stage 2 normalization: %v", err)
	}

	stage3Out, err := stage3.Aggregate(payload, nil, sb.Root)
	if err != nil {
		t.Fatalf("stage 3 aggregation: %v", err)
	}

	var modifiedFiles []string
	for rel := range payload.UpsertedTrees {
		modifiedFiles = append(modifiedFiles, rel)
	}
	modifiedFiles = append(modifiedFiles, payload.DeletedPaths...)

	stage4Out, err := stage4.Link(stage3Out, modifiedFiles, nil,
		stage4.LinkerConfig{LevelOfDetail: stage4.LevelArchitecture})
	if err != nil {
		t.Fatalf("stage 4 linking: %v", err)
	}

	tm, err := akg.NewAKGTransactionManager(sb.GmDir)
	if err != nil {
		t.Fatalf("new transaction manager: %v", err)
	}
	if err := tm.ExecuteDeltaTransaction(stage4Out, modifiedFiles); err != nil {
		t.Fatalf("commit delta transaction: %v", err)
	}
	return tm.GetActiveGraph()
}
