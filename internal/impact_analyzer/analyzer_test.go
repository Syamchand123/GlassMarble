package impact_analyzer

import (
	"strings"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/link"
)

func TestAnalyzeImpact(t *testing.T) {
	graph := akg.NewCodePropertyGraph("commit1")

	// Target: DB Store
	graph.Nodes = graph.Nodes.Set("db", &link.ResolvedNode{
		ID:   "db",
		Name: "UserStore",
		Kind: "STRUCT",
		FileSpec: link.LocationMeta{
			Path:      "internal/storage/store.go",
			LineStart: 10,
		},
	})

	// Direct caller: Service
	graph.Nodes = graph.Nodes.Set("svc", &link.ResolvedNode{
		ID:   "svc",
		Name: "UserService",
		Kind: "STRUCT",
		FileSpec: link.LocationMeta{
			Path:      "internal/service/user.go",
			LineStart: 20,
		},
	})

	// Direct caller: DB Unit Test
	graph.Nodes = graph.Nodes.Set("db_test", &link.ResolvedNode{
		ID:   "db_test",
		Name: "TestUserStore_Save",
		Kind: "FUNCTION",
		FileSpec: link.LocationMeta{
			Path:      "internal/storage/store_test.go",
			LineStart: 5,
		},
	})

	// Transitive caller: Controller
	graph.Nodes = graph.Nodes.Set("ctrl", &link.ResolvedNode{
		ID:   "ctrl",
		Name: "UserController",
		Kind: "STRUCT",
		FileSpec: link.LocationMeta{
			Path:      "internal/api/user_handler.go",
			LineStart: 15,
		},
	})

	// Entrypoint: Main
	graph.Nodes = graph.Nodes.Set("main", &link.ResolvedNode{
		ID:   "main",
		Name: "main",
		Kind: "ENTRYPOINT",
		FileSpec: link.LocationMeta{
			Path:      "cmd/server/main.go",
			LineStart: 8,
		},
	})

	// Inbound edges to "db": svc -> db, db_test -> db
	graph.InboundEdges = graph.InboundEdges.Set("db", []link.ResolvedEdge{
		{SourceID: "svc", TargetID: "db", Type: link.EdgeCalls},
		{SourceID: "db_test", TargetID: "db", Type: link.EdgeCalls},
	})

	// Inbound edges to "svc": ctrl -> svc
	graph.InboundEdges = graph.InboundEdges.Set("svc", []link.ResolvedEdge{
		{SourceID: "ctrl", TargetID: "svc", Type: link.EdgeCalls},
	})

	// Inbound edges to "ctrl": main -> ctrl
	graph.InboundEdges = graph.InboundEdges.Set("ctrl", []link.ResolvedEdge{
		{SourceID: "main", TargetID: "ctrl", Type: link.EdgeCalls},
	})

	rep, err := AnalyzeImpact(graph, "UserStore", ImpactOptions{MaxDepth: 5})
	if err != nil {
		t.Fatalf("AnalyzeImpact failed: %v", err)
	}

	if rep.TargetName != "UserStore" {
		t.Errorf("expected TargetName UserStore, got %s", rep.TargetName)
	}

	if rep.DirectDependentsCount != 2 {
		t.Errorf("expected 2 direct dependents (svc, db_test), got %d", rep.DirectDependentsCount)
	}

	if rep.TransitiveDependentsCount < 2 {
		t.Errorf("expected at least 2 transitive dependents (ctrl, main), got %d", rep.TransitiveDependentsCount)
	}

	if len(rep.ImpactedTestFiles) != 1 || rep.ImpactedTestFiles[0] != "internal/storage/store_test.go" {
		t.Errorf("expected store_test.go in impactedTestFiles, got %v", rep.ImpactedTestFiles)
	}

	if rep.RiskScore <= 0 {
		t.Errorf("expected positive risk score, got %d", rep.RiskScore)
	}

	mermaid := RenderMermaidImpact(rep)
	if !strings.Contains(mermaid, "flowchart BT") || !strings.Contains(mermaid, "UserStore") {
		t.Errorf("mermaid output invalid:\n%s", mermaid)
	}
}
