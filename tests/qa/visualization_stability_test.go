package qa_test

import (
	"strings"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/render"
	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/types"
	"github.com/Syamchand123/GlassMarble/tests/harness"
)

// TestVisualizationStressPythonClass ensures the previously broken
// VisualizationStressProject (triple quotes, dict unpacking, template markers)
// now renders a valid mermaid classDiagram without OPEN_IN_STRUCT errors.
func TestVisualizationStressPythonClass(t *testing.T) {
	sb := harness.NewSandbox(t)
	sb.VisualizationStressProject()
	sb.GitInit()
	out, err := harness.RunGmb(t, sb, "analyze")
	if err != nil {
		t.Fatalf("analyze: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Analyzed") {
		t.Logf("analyze output: %s", out)
	}
	// Render a subset of diagram types via CLI and validate basic syntax
	diagrams := []string{"class", "state", "er", "dependency", "callgraph", "flowchart"}
	for _, d := range diagrams {
		out, err := harness.RunGmb(t, sb, "visualize", d, "--format", "mermaid")
		if err != nil {
			t.Fatalf("visualize %s: %v\n%s", d, err, out)
		}
		if strings.TrimSpace(out) == "" {
			t.Errorf("visualize %s produced empty output", d)
			continue
		}
		if strings.Contains(out, "'''") || strings.Contains(out, "{{") || strings.Contains(out, "{**") {
			t.Errorf("visualize %s contains illegal template markers: %q", d, out[:min(500, len(out))])
		}
		// Mermaid class diagrams must not contain unbalanced quotes
		for i, line := range strings.Split(out, "\n") {
			if strings.Count(line, "\"")%2 != 0 {
				t.Errorf("visualize %s line %d unbalanced quotes: %q", d, i+1, line)
			}
		}
	}
}

// TestVisualizationAllFormatsForPolyglot verifies that every renderer
// (mermaid, plantuml, dot) produces non-empty output for a polyglot project.
func TestVisualizationAllFormatsForPolyglot(t *testing.T) {
	sb := harness.NewSandbox(t)
	sb.PolyglotProject()
	sb.GitInit()
	if out, err := harness.RunGmb(t, sb, "analyze"); err != nil {
		t.Fatalf("analyze: %v\n%s", err, out)
	}
	formats := []string{"mermaid", "plantuml", "dot"}
	diagrams := []string{"dependency", "class", "state"}
	for _, f := range formats {
		for _, d := range diagrams {
			out, err := harness.RunGmb(t, sb, "visualize", d, "--format", f)
			if err != nil {
				t.Fatalf("visualize %s --format %s: %v\n%s", d, f, err, out)
			}
			if strings.TrimSpace(out) == "" {
				t.Errorf("visualize %s --format %s empty", d, f)
			}
			if f == "mermaid" && !strings.Contains(out, "%%") && !strings.Contains(out, "flowchart") && !strings.Contains(out, "classDiagram") && !strings.Contains(out, "sequenceDiagram") {
				t.Logf("mermaid %s header check soft: %s", d, out[:min(100, len(out))])
			}
			if f == "plantuml" && !strings.Contains(out, "@startuml") {
				t.Errorf("plantuml %s missing @startuml", d)
			}
			if f == "dot" && !strings.Contains(out, "digraph") {
				t.Errorf("dot %s missing digraph", d)
			}
		}
	}
}

// TestRenderDirectClassDiagramSanitization directly tests the sanitizer
// with adversarial member names/types that previously caused OPEN_IN_STRUCT.
func TestRenderDirectClassDiagramSanitization(t *testing.T) {
	cases := []struct {
		name string
		tree *types.LayoutTree
	}{
		{
			name: "python triple-quote and dict unpack",
			tree: &types.LayoutTree{
				BoundaryName: "Root",
				Nodes: []*types.LayoutNode{
					{ID: "src/state.py::SessionState", Name: "SessionState", Kind: "gm:Class", Code: "self.env = '''Combine\n{**env_vars\nreturn \"value\"\n{{key}} = '''Replace\nnot if True:\n"},
				},
			},
		},
		{
			name: "go struct with backticks and generics",
			tree: &types.LayoutTree{
				BoundaryName: "Root",
				Nodes: []*types.LayoutNode{
					{ID: "pkg/model.go::Service", Name: "Service", Kind: "gm:Struct", Code: "Name string `json:\"name\"`\nAge int `json:\"age\"`\nHandle func() error\n"},
				},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, fmt := range []string{"mermaid", "plantuml", "dot"} {
				var out string
				switch fmt {
				case "mermaid":
					out = render.RenderDiagram(tc.tree, types.UMLClass)
				case "plantuml":
					out = render.RenderPlantUMLDiagram(tc.tree, types.UMLClass)
				case "dot":
					out = render.RenderDOTDiagram(tc.tree, types.UMLClass)
				}
				if strings.Contains(out, "'''") || strings.Contains(out, "{**") {
					t.Errorf("%s output still contains illegal markers: %q", fmt, out)
				}
				if strings.TrimSpace(out) == "" {
					t.Errorf("%s empty output", fmt)
				}
			}
		})
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
