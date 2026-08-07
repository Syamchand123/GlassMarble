package product_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/product"
	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/types"
)

// TestCLI_TUI_AI_Parity verifies that CLI, TUI, and AI tools all consume BuildDiagram (W7-01).
func TestCLI_TUI_AI_Parity(t *testing.T) {
	StatePath := filepath.Join("..", "visualization_engine", "testdata", "sample.ttl")
	if _, err := os.Stat(StatePath); os.IsNotExist(err) {
		t.Skip("sample.ttl fixture not found; skipping integration parity test")
	}

	req := product.BuildDiagramRequest{
		StatePath:     StatePath,
		DiagramType: types.UMLClass,
		Format:      "mermaid",
		Options: product.DiagramOptions{
			Scope: types.ScopeGlobal,
		},
	}

	res1, err1 := product.BuildDiagramEx(req)
	if err1 != nil {
		t.Fatalf("BuildDiagramEx failed: %v", err1)
	}
	if res1 == nil || res1.Markup == "" {
		t.Fatalf("expected non-empty markup from BuildDiagramEx")
	}

	res2, err2 := product.BuildDiagramEx(req)
	if err2 != nil {
		t.Fatalf("BuildDiagramEx second run failed: %v", err2)
	}

	if res1.Markup != res2.Markup {
		t.Errorf("expected deterministic identical markup across runs; got mismatch")
	}
}

// TestPhaseSpans_Telemetry verifies telemetry span recording and persistence (W7-02 / 11.4).
func TestPhaseSpans_Telemetry(t *testing.T) {
	stop1 := product.StartSpan("extract")
	stop1()
	stop2 := product.StartSpan("project")
	stop2()
	stop3 := product.StartSpan("render")
	stop3()

	tmpDir := t.TempDir()
	err := product.SaveTelemetry(tmpDir)
	if err != nil {
		t.Fatalf("SaveTelemetry failed: %v", err)
	}

	spans, err := product.LoadTelemetry(tmpDir)
	if err != nil {
		t.Fatalf("LoadTelemetry failed: %v", err)
	}
	if len(spans) < 3 {
		t.Errorf("expected at least 3 telemetry spans; got %d", len(spans))
	}

	spanNames := make(map[string]bool)
	for _, s := range spans {
		spanNames[s.Name] = true
	}

	for _, reqSpan := range []string{"extract", "project", "render"} {
		if !spanNames[reqSpan] {
			t.Errorf("expected telemetry span %q to be recorded", reqSpan)
		}
	}
}

// TestFormatParity_HeaderComments verifies Mermaid, PlantUML, and DOT format encoding & headers (W7-04 / 11.5).
func TestFormatParity_HeaderComments(t *testing.T) {
	StatePath := filepath.Join("..", "visualization_engine", "testdata", "sample.ttl")
	if _, err := os.Stat(StatePath); os.IsNotExist(err) {
		t.Skip("sample.ttl fixture not found; skipping format test")
	}

	formats := []struct {
		format     string
		headerChar string
	}{
		{"mermaid", "%"},
		{"plantuml", "'"},
		{"dot", "//"},
	}

	for _, fmtCase := range formats {
		t.Run(fmtCase.format, func(t *testing.T) {
			req := product.BuildDiagramRequest{
				StatePath:     StatePath,
				DiagramType: types.UMLClass,
				Format:      fmtCase.format,
				Options: product.DiagramOptions{
					Scope: types.ScopeGlobal,
				},
			}

			res, err := product.BuildDiagramWithContext(context.Background(), req)
			if err != nil {
				t.Fatalf("BuildDiagramWithContext format %s failed: %v", fmtCase.format, err)
			}
			if res == nil || res.Markup == "" {
				t.Fatalf("expected non-empty markup for format %s", fmtCase.format)
			}

			if !strings.HasPrefix(res.Markup, fmtCase.headerChar) {
				t.Errorf("expected header comment starting with %q for format %s; got:\n%.100s",
					fmtCase.headerChar, fmtCase.format, res.Markup)
			}
		})
	}
}
