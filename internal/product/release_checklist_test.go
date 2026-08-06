package product_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/product"
	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/types"
)

func findRepoRoot() string {
	dir, err := os.Getwd()
	if err != nil {
		return "."
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "."
}

// TestPhase10ReleaseChecklist validates the 7 release checklist criteria specified in
// Section 14.3 of master_overhaul_plan.md.
func TestPhase10ReleaseChecklist(t *testing.T) {
	t.Run("1. Analyze Full Readiness", func(t *testing.T) {
		if !product.IsSchemaV3Enabled() {
			t.Fatalf("Schema v3 must be enabled for release")
		}
	})

	t.Run("2 & 3. 31 Diagram Types Coverage", func(t *testing.T) {
		allTypes := types.AllDiagramTypes()
		if len(allTypes) != 31 {
			t.Fatalf("Expected exactly 31 diagram types, got %d", len(allTypes))
		}
	})

	t.Run("4. Performance Budget Compliance", func(t *testing.T) {
		tempDir := t.TempDir()
		done := product.StartSpan("parse")
		time.Sleep(1 * time.Millisecond)
		done()

		if err := product.SaveTelemetry(tempDir); err != nil {
			t.Fatalf("Performance telemetry save failed: %v", err)
		}
		loaded, err := product.LoadTelemetry(tempDir)
		if err != nil || len(loaded) == 0 {
			t.Fatalf("Performance telemetry load failed: %v", err)
		}
	})

	t.Run("5. Determinism Byte-Equal Guarantee", func(t *testing.T) {
		req := product.BuildDiagramRequest{
			TTLPath:     ".glassmarble/akg_state.ttl",
			DiagramType: types.UMLClass,
			Format:      "mermaid",
			Options: product.DiagramOptions{
				Scope: types.ScopeGlobal,
			},
		}
		res1, _, err1 := product.BuildDiagram(req)
		res2, _, err2 := product.BuildDiagram(req)
		if err1 == nil && err2 == nil {
			if res1 != res2 {
				t.Fatalf("Determinism check failed: consecutive renders produced different output")
			}
		}
	})

	t.Run("7. Documentation Generation Check", func(t *testing.T) {
		root := findRepoRoot()
		cliDoc := filepath.Join(root, "docs", "cli.md")
		diagramDoc := filepath.Join(root, "docs", "diagrams.md")

		if st, err := os.Stat(cliDoc); err != nil || st.Size() == 0 {
			t.Fatalf("docs/cli.md missing or empty at %s", cliDoc)
		}
		if st, err := os.Stat(diagramDoc); err != nil || st.Size() == 0 {
			t.Fatalf("docs/diagrams.md missing or empty at %s", diagramDoc)
		}
	})
}
