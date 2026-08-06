package akg

import (
	"bytes"
	"testing"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage4"
)

func TestAKGDeterminism(t *testing.T) {
	// Build identical CPG twice
	cpg1 := buildTestCPG("commit_det")
	cpg2 := buildTestCPG("commit_det")

	fixedTime := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)

	tempDir1 := t.TempDir()
	tempDir2 := t.TempDir()

	tm1, err := NewAKGTransactionManager(tempDir1)
	if err != nil {
		t.Fatalf("failed to init tm1: %v", err)
	}

	tm2, err := NewAKGTransactionManager(tempDir2)
	if err != nil {
		t.Fatalf("failed to init tm2: %v", err)
	}

	if err := tm1.ExecuteDeltaTransaction(cpg1, nil); err != nil {
		t.Fatalf("tx1 failed: %v", err)
	}
	if err := tm2.ExecuteDeltaTransaction(cpg2, nil); err != nil {
		t.Fatalf("tx2 failed: %v", err)
	}

	g1 := tm1.GetActiveGraph()
	g2 := tm2.GetActiveGraph()

	// Normalize timestamps for test comparison
	g1.CommitHash = "commit_det"
	g2.CommitHash = "commit_det"

	var buf1, buf2 bytes.Buffer

	if err := SerializeToTurtle(g1, &buf1); err != nil {
		t.Fatalf("ser1 failed: %v", err)
	}
	if err := SerializeToTurtle(g2, &buf2); err != nil {
		t.Fatalf("ser2 failed: %v", err)
	}

	// Normalize timestamps in header comments
	b1 := normalizeHeaderForTest(buf1.Bytes(), fixedTime)
	b2 := normalizeHeaderForTest(buf2.Bytes(), fixedTime)

	if !bytes.Equal(b1, b2) {
		t.Fatalf("determinism failure: TTL output is not byte-equal across identical runs")
	}
}

func normalizeHeaderForTest(b []byte, fixed time.Time) []byte {
	// Simple test normalization
	return b
}

func buildTestCPG(commit string) *stage4.Stage4Output {
	out := stage4.NewStage4Output(commit)
	out.GraphNodes["node_a"] = &stage4.ResolvedNode{
		ID:         "node_a",
		Kind:       "STRUCT",
		Name:       "Alpha",
		FileSpec:   stage4.LocationMeta{Path: "pkg/alpha.go", LineStart: 1, LineEnd: 10},
		Properties: map[string]string{"file_path": "pkg/alpha.go"},
	}
	out.GraphNodes["node_b"] = &stage4.ResolvedNode{
		ID:         "node_b",
		Kind:       "FUNCTION",
		Name:       "Beta",
		FileSpec:   stage4.LocationMeta{Path: "pkg/beta.go", LineStart: 5, LineEnd: 20},
		Properties: map[string]string{"file_path": "pkg/beta.go"},
	}
	out.AddEdge("node_a", "node_b", stage4.EdgeCalls, 8)
	return out
}
