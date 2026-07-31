package types

import (
	"testing"
)

func TestDiagramTypeConstants(t *testing.T) {
	if UMLClass != "UML_CLASS" {
		t.Errorf("expected UML_CLASS, got %s", UMLClass)
	}
	if C4Context != "C4_CONTEXT" {
		t.Errorf("expected C4_CONTEXT, got %s", C4Context)
	}
	if DependencyGraph != "DEPENDENCY_GRAPH" {
		t.Errorf("expected DEPENDENCY_GRAPH, got %s", DependencyGraph)
	}
}

func TestScopeLevelValues(t *testing.T) {
	if ScopeGlobal != 0 {
		t.Errorf("expected ScopeGlobal=0, got %d", ScopeGlobal)
	}
	if ScopeFolder != 1 {
		t.Errorf("expected ScopeFolder=1, got %d", ScopeFolder)
	}
	if ScopeFile != 2 {
		t.Errorf("expected ScopeFile=2, got %d", ScopeFile)
	}
}

func TestQueryOptionsDefault(t *testing.T) {
	opts := QueryOptions{}
	if opts.Scope != ScopeGlobal {
		t.Errorf("expected default Scope to be ScopeGlobal")
	}
	if opts.Format != "" {
		t.Errorf("expected default Format to be empty")
	}
	if opts.MaxDepth != 0 {
		t.Errorf("expected default MaxDepth to be 0")
	}
}

func TestPipelineConfigDefaults(t *testing.T) {
	cfg := PipelineConfig{}
	if cfg.EnableMetrics {
		t.Error("expected EnableMetrics to be false by default")
	}
	if cfg.EnableCommunities {
		t.Error("expected EnableCommunities to be false by default")
	}
}

func TestPipelineStageOrder(t *testing.T) {
	if StageParse != 0 {
		t.Errorf("expected StageParse=0, got %d", StageParse)
	}
	if StageScope != 1 {
		t.Errorf("expected StageScope=1, got %d", StageScope)
	}
	if StageRender != 6 {
		t.Errorf("expected StageRender=6, got %d", StageRender)
	}
}

func TestGraphSummaryDefault(t *testing.T) {
	s := GraphSummary{}
	if s.NodeCount != 0 {
		t.Errorf("expected NodeCount=0, got %d", s.NodeCount)
	}
}

func TestLayoutNodeDefault(t *testing.T) {
	n := LayoutNode{}
	if n.PageRank != 0 {
		t.Errorf("expected PageRank=0, got %f", n.PageRank)
	}
	if n.IsHotspot {
		t.Error("expected IsHotspot to be false")
	}
	if n.IsBottleneck {
		t.Error("expected IsBottleneck to be false")
	}
	if n.IsGodObject {
		t.Error("expected IsGodObject to be false")
	}
}

func TestExtractionConfigDefault(t *testing.T) {
	cfg := ExtractionConfig{}
	if cfg.MaxDepth != 0 {
		t.Errorf("expected MaxDepth=0, got %d", cfg.MaxDepth)
	}
	if cfg.Name != "" {
		t.Errorf("expected Name empty, got %s", cfg.Name)
	}
}
