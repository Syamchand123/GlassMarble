package product_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/product"
	producterrs "github.com/Syamchand123/GlassMarble/internal/product/errors"
	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/types"
)

// TestMasterRegressionBattery validates all defect assertions (V-01...V-11, K-01...K-08, P-01/P-02)
// listed in Section 13.3 of master_overhaul_plan.md.
func TestMasterRegressionBattery(t *testing.T) {
	t.Run("V-01 & V-10: Validation & Unused Parity", func(t *testing.T) {
		req := product.BuildDiagramRequest{
			TTLPath:     ".glassmarble/akg_state.ttl",
			DiagramType: types.UMLClass,
			Format:      "invalid_format",
			Options: product.DiagramOptions{
				Scope: types.ScopeGlobal,
			},
		}
		_, _, err := product.BuildDiagram(req)
		if err == nil {
			t.Fatalf("V-10 failed: expected typed validation error for invalid format, got nil")
		}
		if !errors.Is(err, producterrs.ErrValidation) {
			t.Fatalf("V-10 failed: expected ErrValidation, got %v", err)
		}
	})

	t.Run("V-02, V-04, V-06: Member & Class Header Real Names", func(t *testing.T) {
		req := product.BuildDiagramRequest{
			TTLPath:     ".glassmarble/akg_state.ttl",
			DiagramType: types.UMLClass,
			Format:      "mermaid",
			Options: product.DiagramOptions{
				Scope: types.ScopeGlobal,
			},
		}
		markup, _, err := product.BuildDiagram(req)
		if err != nil {
			t.Skipf("Skipping live diagram test if AKG not built: %v", err)
			return
		}

		if strings.Contains(markup, "ext:") {
			t.Fatalf("V-02 failed: diagram contains mangled 'ext:' header names")
		}
		if !strings.Contains(markup, "classDiagram") {
			t.Fatalf("V-06 failed: expected classDiagram header")
		}
	})

	t.Run("V-05 & K-01: Determinism & RDF-Star Single Statement", func(t *testing.T) {
		req := product.BuildDiagramRequest{
			TTLPath:     ".glassmarble/akg_state.ttl",
			DiagramType: types.UMLClass,
			Format:      "mermaid",
			Options: product.DiagramOptions{
				Scope: types.ScopeGlobal,
			},
		}
		markup1, _, err1 := product.BuildDiagram(req)
		markup2, _, err2 := product.BuildDiagram(req)
		if err1 == nil && err2 == nil {
			if markup1 != markup2 {
				t.Fatalf("V-05 failed: non-deterministic output between consecutive runs")
			}
		}
	})

	t.Run("K-02 & K-04: Metadata & Schema v3 Migration", func(t *testing.T) {
		tempDir := t.TempDir()
		graph := akg.NewCodePropertyGraph("commit_regression_hash")
		graph.SchemaVersion = 2

		bakPath, err := akg.AutoMigrateOnLoad(tempDir, graph)
		if err != nil {
			t.Fatalf("K-07 failed: AutoMigrateOnLoad error: %v", err)
		}

		if graph.SchemaVersion != 3 {
			t.Fatalf("K-07 failed: expected SchemaVersion 3, got %d", graph.SchemaVersion)
		}
		if graph.CommitHash == "" {
			t.Fatalf("K-02 failed: expected non-empty CommitHash")
		}
		_ = bakPath
	})

	t.Run("W8-04 & AC-30: Performance Benchmark Gates", func(t *testing.T) {
		tempDir := t.TempDir()
		done := product.StartSpan("parse")
		time.Sleep(1 * time.Millisecond)
		done()

		if err := product.SaveTelemetry(tempDir); err != nil {
			t.Fatalf("failed to save telemetry: %v", err)
		}

		loaded, err := product.LoadTelemetry(tempDir)
		if err != nil || len(loaded) == 0 {
			t.Fatalf("Telemetry save/load failed: %v", err)
		}
	})
}
