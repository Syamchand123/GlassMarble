package renderer

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/doc_engine/config"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/storage"
)

// TestProcessDocument_GatesSixSevenWire proves Gates 6 (prose) and 7
// (references) execute inside ProcessDocument on the deterministic track:
// strict jargon violation → warning (never fatal on Track B); permalink to
// a nonexistent file → reference-integrity warning.
func TestProcessDocument_GatesSixSevenWire(t *testing.T) {
	tempDir := t.TempDir()
	storageDir := filepath.Join(tempDir, ".glassmarble")
	_ = os.MkdirAll(storageDir, 0755)
	sm := storage.NewStateManager(storageDir)
	writeZoneDoc(t, tempDir, "docs/arch.md", "arch", "Prior body.\n")

	doc := &config.DocSpec{
		ID:         "arch",
		TargetPath: "docs/arch.md",
		Style:      &config.StyleSpec{JargonBlacklist: []string{"Table"}, StrictProse: true},
		Scope: config.ScopeRule{
			Paths:       []string{"internal/auth/**"},
			EntryPoints: []string{"internal/auth/jwt.go::ValidateToken"},
		},
		Sections: []config.SectionSpec{
			{ID: "arch", Title: "Arch", Managed: true, Instruction: "Table of subsystem interface.", GroundWith: []string{"signatures"}},
		},
	}
	orch := NewOrchestrator(OrchestratorOptions{NoLLM: true})
	_, _, warnings, err := orch.ProcessDocument(context.Background(), tempDir, doc, []string{"arch"}, diagramTestGraph(), nil, sm, "c1")
	if err != nil {
		t.Fatalf("ProcessDocument failed: %v (warnings: %v)", err, warnings)
	}
	joined := strings.Join(warnings, "\n")
	if !strings.Contains(joined, "prose") {
		t.Errorf("expected Gate 6 prose warning (strict jargon 'Table'), got warnings:\n%s", joined)
	}
	// internal/auth/jwt.go does not exist in tempDir → permalink is broken.
	if !strings.Contains(joined, "reference integrity") {
		t.Errorf("expected Gate 7 reference-integrity warning, got warnings:\n%s", joined)
	}
}
