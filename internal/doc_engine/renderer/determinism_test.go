package renderer

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/doc_engine/config"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/storage"
)

// TestProcessDocument_DeterministicRerun is the plan Section 13.3
// determinism gate at engine scope: rendering the same section twice on
// the same inputs must produce byte-identical files (zero git churn).
func TestProcessDocument_DeterministicRerun(t *testing.T) {
	tempDir := t.TempDir()
	storageDir := filepath.Join(tempDir, ".glassmarble")
	_ = os.MkdirAll(storageDir, 0755)
	sm := storage.NewStateManager(storageDir)

	doc := &config.DocSpec{
		ID:         "det-doc",
		TargetPath: "docs/det.md",
		Title:      "Determinism",
		Mode:       config.ModeManagedSections,
		Sections: []config.SectionSpec{
			{ID: "overview", Title: "Overview", Instruction: "Describe.", Managed: true, GroundWith: []string{"signatures"}},
		},
	}
	orch := NewOrchestrator(OrchestratorOptions{NoLLM: true})
	ctx := context.Background()

	if _, _, _, _, err := orch.ProcessDocument(ctx, tempDir, doc, []string{"overview"}, nil, nil, sm, "c1"); err != nil {
		t.Fatalf("first render failed: %v", err)
	}
	first, err := os.ReadFile(filepath.Join(tempDir, "docs/det.md"))
	if err != nil {
		t.Fatalf("reading first render: %v", err)
	}

	// Fresh orchestrator + fresh state load: same inputs, must be identical.
	sm2 := storage.NewStateManager(storageDir)
	orch2 := NewOrchestrator(OrchestratorOptions{NoLLM: true})
	if _, _, _, _, err := orch2.ProcessDocument(ctx, tempDir, doc, []string{"overview"}, nil, nil, sm2, "c1"); err != nil {
		t.Fatalf("second render failed: %v", err)
	}
	second, err := os.ReadFile(filepath.Join(tempDir, "docs/det.md"))
	if err != nil {
		t.Fatalf("reading second render: %v", err)
	}
	if string(first) != string(second) {
		t.Errorf("non-deterministic render:\n--- first ---\n%s\n--- second ---\n%s", first, second)
	}

	// Zero-churn contract: the render must persist the exact Stage-3
	// section hash so the next invalidation reads "clean → 0 tokens".
	st, err := sm2.Load()
	if err != nil {
		t.Fatalf("loading state: %v", err)
	}
	ds, ok := st.Documents["docs/det.md"]
	if !ok || ds == nil {
		t.Fatalf("state missing document entry (want relative key %q)", "docs/det.md")
	}
	ss, ok := ds.Sections["overview"]
	if !ok || ss == nil || ss.ASTSubgraphHash == "" {
		t.Errorf("section hash not persisted — next run would re-render (zero-churn broken)")
	}
}

// BenchmarkRenderTrackB tracks the plan Section 13.4 <20ms deterministic
// render budget (benchmark only; the gate is asserted in CI dashboards).
func BenchmarkRenderTrackB(b *testing.B) {
	orch := NewOrchestrator(OrchestratorOptions{NoLLM: true})
	fs := &config.FactSheet{
		DocID:              "bench",
		SectionID:          "overview",
		SectionInstruction: "Describe.",
		GroundTruth: config.GroundTruthPayload{
			Symbols: []config.SymbolFact{
				{FQN: "internal/x.Foo", Kind: "func", Signature: "func Foo() error", Doc: "Foo does x.", File: "internal/x/x.go", Line: 10},
			},
		},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := orch.renderTrackB(fs, nil); err != nil {
			b.Fatalf("renderTrackB failed: %v", err)
		}
	}
}
