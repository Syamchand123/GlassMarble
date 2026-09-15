package renderer

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/link"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/config"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/storage"
)

func diagramTestGraph() *akg.CodePropertyGraph {
	g := akg.NewCodePropertyGraph("test")
	g.Nodes = g.Nodes.Set("internal/auth/jwt.go::ValidateToken", &link.ResolvedNode{
		ID:   "internal/auth/jwt.go::ValidateToken",
		Name: "ValidateToken",
		Kind: "FUNCTION",
		FileSpec: link.LocationMeta{
			Path:      "internal/auth/jwt.go",
			LineStart: 25,
			LineEnd:   50,
		},
		Properties: map[string]string{
			"signature": "func ValidateToken(token string) (*Claims, error)",
		},
	})
	g.OutboundEdges = g.OutboundEdges.Set("internal/auth/jwt.go::ValidateToken", []link.ResolvedEdge{})
	return g
}

func writeZoneDoc(t *testing.T, root, target, sectionID, inner string) {
	t.Helper()
	content := "# Doc\n\n<!-- gmb:begin:" + sectionID + " -->\n" + inner + "<!-- gmb:end:" + sectionID + " -->\n"
	requireWrite(t, filepath.Join(root, target), content)
}

func requireWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestProcessDocument_DiagramDirectiveInjected(t *testing.T) {
	tempDir := t.TempDir()
	storageDir := filepath.Join(tempDir, ".glassmarble")
	_ = os.MkdirAll(storageDir, 0755)
	sm := storage.NewStateManager(storageDir)
	writeZoneDoc(t, tempDir, "docs/arch.md", "arch", "<!-- gmb:diagram:callgraph -->\nPrior body.\n")

	doc := &config.DocSpec{
		ID:         "arch",
		TargetPath: "docs/arch.md",
		Scope: config.ScopeRule{
			Paths:       []string{"internal/auth/**"},
			EntryPoints: []string{"internal/auth/jwt.go::ValidateToken"},
		},
		Sections: []config.SectionSpec{
			{ID: "arch", Title: "Arch", Managed: true, GroundWith: []string{"signatures"}},
		},
	}
	orch := NewOrchestrator(OrchestratorOptions{NoLLM: true})
	changed, _, warnings, _, err := orch.ProcessDocument(context.Background(), tempDir, doc, []string{"arch"}, diagramTestGraph(), nil, sm, "c1")
	if err != nil {
		t.Fatalf("ProcessDocument failed: %v (warnings: %v)", err, warnings)
	}
	if !changed {
		t.Fatalf("expected changed=true")
	}
	out, err := os.ReadFile(filepath.Join(tempDir, "docs/arch.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "```mermaid") {
		t.Errorf("expected injected mermaid diagram, got:\n%s", out)
	}
}

func TestProcessDocument_TodoPopulated(t *testing.T) {
	tempDir := t.TempDir()
	storageDir := filepath.Join(tempDir, ".glassmarble")
	_ = os.MkdirAll(storageDir, 0755)
	sm := storage.NewStateManager(storageDir)
	writeZoneDoc(t, tempDir, "docs/gap.md", "gap", "<!-- gmb:todo: document login flow -->\nPrior body.\n")

	doc := &config.DocSpec{
		ID:         "gap",
		TargetPath: "docs/gap.md",
		Scope:      config.ScopeRule{Paths: []string{"internal/auth/**"}},
		Sections: []config.SectionSpec{
			{ID: "gap", Title: "Gap", Managed: true, GroundWith: []string{"exported_symbols"}},
		},
	}
	orch := NewOrchestrator(OrchestratorOptions{NoLLM: true})
	_, _, _, _, err := orch.ProcessDocument(context.Background(), tempDir, doc, []string{"gap"}, diagramTestGraph(), nil, sm, "c1")
	if err != nil {
		t.Fatalf("ProcessDocument failed: %v", err)
	}
	out, err := os.ReadFile(filepath.Join(tempDir, "docs/gap.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "> TODO addressed from AKG: 1 exported symbols in scope: `ValidateToken`") {
		t.Errorf("expected populated todo draft line, got:\n%s", out)
	}
}
