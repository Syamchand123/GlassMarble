package tools_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/ai_engine/tools"
)

// TestDiagramTypesTool verifies the diagram vocabulary tool.
func TestDiagramTypesTool(t *testing.T) {
	env := newTestEnv(t)
	v, err := call(t, env, "diagram_types", "")
	if err != nil {
		t.Fatalf("diagram_types: %v", err)
	}
	d := dataOf(t, v)
	if d["count"] != float64(31) {
		t.Errorf("count = %v, want 31", d["count"])
	}
	typesList, _ := d["types"].([]any)
	if len(typesList) != 31 {
		t.Fatalf("types = %d entries, want 31", len(typesList))
	}
	seen := make(map[string]bool)
	for _, ti := range typesList {
		m := ti.(map[string]any)
		typ, _ := m["type"].(string)
		seen[typ] = true
		if m["family"] == "" || m["description"] == "" {
			t.Errorf("entry missing fields: %v", m)
		}
	}
	for _, want := range []string{"UML_CLASS", "UML_SEQUENCE", "C4_CONTAINER", "ER_DIAGRAM", "CALL_GRAPH", "DEPENDENCY_GRAPH", "CHANGE_IMPACT"} {
		if !seen[want] {
			t.Errorf("catalog missing %s", want)
		}
	}
}

// TestDiagramGenerateTool generates a dependency-graph mermaid diagram from
// the synthetic AKG and checks the markup shape.
func TestDiagramGenerateTool(t *testing.T) {
	env := newTestEnv(t)
	v, err := call(t, env, "diagram_generate", `{"type": "DEPENDENCY_GRAPH"}`)
	if err != nil {
		t.Fatalf("diagram_generate: %v", err)
	}
	markup, ok := v.(string)
	if !ok {
		t.Fatalf("expected raw markup, got %T", v)
	}
	if !strings.Contains(markup, "flowchart") && !strings.Contains(markup, "graph ") {
		t.Errorf("markup does not look like mermaid:\n%s", markup)
	}
	if len(markup) < 100 {
		t.Errorf("markup suspiciously short (%d bytes):\n%s", len(markup), markup)
	}
}

// TestDiagramGenerateLenientType normalizes "c4 container" to C4_CONTAINER.
func TestDiagramGenerateLenientType(t *testing.T) {
	env := newTestEnv(t)
	if _, err := call(t, env, "diagram_generate", `{"type": "c4 container"}`); err != nil {
		t.Fatalf("lenient type name: %v", err)
	}
}

// TestDiagramGenerateUnknownType rejects unknown type names.
func TestDiagramGenerateUnknownType(t *testing.T) {
	env := newTestEnv(t)
	if _, err := call(t, env, "diagram_generate", `{"type": "NOPE"}`); err == nil || !strings.Contains(err.Error(), "unknown diagram type") {
		t.Fatalf("expected unknown-type error, got %v", err)
	}
}

// TestDiagramGenerateSequenceRequiresEntry mirrors the visualize CLI rule.
func TestDiagramGenerateSequenceRequiresEntry(t *testing.T) {
	env := newTestEnv(t)
	if _, err := call(t, env, "diagram_generate", `{"type": "UML_SEQUENCE"}`); err == nil || !strings.Contains(err.Error(), "entry") {
		t.Fatalf("expected entry-point error, got %v", err)
	}
}

// TestDiagramGenerateMissingAKG recommends gmb analyze when no TTL exists.
func TestDiagramGenerateMissingAKG(t *testing.T) {
	dir := t.TempDir()
	env := &tools.Env{RootDir: dir}
	if _, err := call(t, env, "diagram_generate", `{"type": "DEPENDENCY_GRAPH"}`); err == nil || !strings.Contains(err.Error(), "gmb analyze") {
		t.Fatalf("expected missing-AKG error, got %v", err)
	}
}

// TestDiagramGenerateSave writes to .glassmarble/marbles/ and returns a receipt.
func TestDiagramGenerateSave(t *testing.T) {
	env := newTestEnv(t)
	v, err := call(t, env, "diagram_generate", `{"type": "DEPENDENCY_GRAPH", "save": true}`)
	if err != nil {
		t.Fatalf("diagram_generate save: %v", err)
	}
	d := dataOf(t, v)
	path, _ := d["saved"].(string)
	if path == "" || !strings.Contains(path, filepath.Join(".glassmarble", "marbles")) {
		t.Fatalf("saved path = %q", path)
	}
	if d["type"] != "DEPENDENCY_GRAPH" {
		t.Errorf("type = %v", d["type"])
	}
	if n, _ := d["bytes"].(float64); n < 100 {
		t.Errorf("bytes = %v, want >= 100", d["bytes"])
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read saved diagram: %v", err)
	}
	if len(content) == 0 {
		t.Error("saved diagram is empty")
	}
}

// TestDiagramSummaryTool verifies the graph summary fields. CALL_GRAPH is
// used because the DEPENDENCY_GRAPH extraction filter still matches the OLD
// vocabulary (gm:TypeDecl/gm:File/gm:Namespace); updating that config is a
// visualization-batch coordination item (see AUDIT report §7).
func TestDiagramSummaryTool(t *testing.T) {
	env := newTestEnv(t)
	v, err := call(t, env, "diagram_summary", `{"type": "CALL_GRAPH"}`)
	if err != nil {
		t.Fatalf("diagram_summary: %v", err)
	}
	d := dataOf(t, v)
	if d["node_count"].(float64) < 1 {
		t.Errorf("node_count = %v, want >= 1", d["node_count"])
	}
	for _, k := range []string{"edge_count", "density", "diameter", "avg_path_length", "cluster_count", "largest_scc_size", "god_object_count", "bipartite_score"} {
		if _, ok := d[k]; !ok {
			t.Errorf("summary missing field %s: %v", k, d)
		}
	}
	if _, err := call(t, env, "diagram_summary", `{"type": "NOPE"}`); err == nil {
		t.Fatal("expected unknown-type error")
	}
}
