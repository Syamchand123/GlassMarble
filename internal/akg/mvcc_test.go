package akg

import (
	"fmt"
	"sync"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage4"
	"github.com/stretchr/testify/assert"
)

// ==================== DetectCycles Tests ====================

func TestDetectCycles_NoCycle(t *testing.T) {
	g := NewCodePropertyGraph("test")
	g.Nodes = g.Nodes.Set("a", &stage4.ResolvedNode{ID: "a", Kind: "FUNCTION", Name: "a"})
	g.Nodes = g.Nodes.Set("b", &stage4.ResolvedNode{ID: "b", Kind: "FUNCTION", Name: "b"})
	g.Nodes = g.Nodes.Set("c", &stage4.ResolvedNode{ID: "c", Kind: "FUNCTION", Name: "c"})
	g.OutboundEdges = g.OutboundEdges.Set("a", []stage4.ResolvedEdge{{SourceID: "a", TargetID: "b", Type: stage4.EdgeCalls}})
	g.OutboundEdges = g.OutboundEdges.Set("b", []stage4.ResolvedEdge{{SourceID: "b", TargetID: "c", Type: stage4.EdgeCalls}})

	cycles := g.DetectCycles()
	if len(cycles) != 0 {
		t.Errorf("expected 0 cycles, got %d", len(cycles))
	}
}

func TestDetectCycles_SingleCycle(t *testing.T) {
	g := NewCodePropertyGraph("test")
	g.Nodes = g.Nodes.Set("a", &stage4.ResolvedNode{ID: "a", Kind: "FUNCTION", Name: "a"})
	g.Nodes = g.Nodes.Set("b", &stage4.ResolvedNode{ID: "b", Kind: "FUNCTION", Name: "b"})
	g.Nodes = g.Nodes.Set("c", &stage4.ResolvedNode{ID: "c", Kind: "FUNCTION", Name: "c"})
	g.OutboundEdges = g.OutboundEdges.Set("a", []stage4.ResolvedEdge{{SourceID: "a", TargetID: "b", Type: stage4.EdgeCalls}})
	g.OutboundEdges = g.OutboundEdges.Set("b", []stage4.ResolvedEdge{{SourceID: "b", TargetID: "c", Type: stage4.EdgeCalls}})
	g.OutboundEdges = g.OutboundEdges.Set("c", []stage4.ResolvedEdge{{SourceID: "c", TargetID: "a", Type: stage4.EdgeCalls}})

	cycles := g.DetectCycles()
	if len(cycles) != 1 {
		t.Errorf("expected 1 cycle, got %d", len(cycles))
	}
}

func TestDetectCycles_EmptyGraph(t *testing.T) {
	g := NewCodePropertyGraph("test")
	cycles := g.DetectCycles()
	if len(cycles) != 0 {
		t.Errorf("expected 0 cycles for empty graph, got %d", len(cycles))
	}
}

// ==================== FindArticulationPoints Tests ====================

func TestArticulationPoints_SimpleBridge(t *testing.T) {
	g := NewCodePropertyGraph("test")
	for _, id := range []string{"a", "b", "c", "d"} {
		g.Nodes = g.Nodes.Set(id, &stage4.ResolvedNode{ID: id, Kind: "NODE", Name: id})
	}
	g.OutboundEdges = g.OutboundEdges.Set("a", []stage4.ResolvedEdge{{SourceID: "a", TargetID: "b", Type: stage4.EdgeCalls}})
	g.OutboundEdges = g.OutboundEdges.Set("b", []stage4.ResolvedEdge{{SourceID: "b", TargetID: "c", Type: stage4.EdgeCalls}})
	g.OutboundEdges = g.OutboundEdges.Set("c", []stage4.ResolvedEdge{{SourceID: "c", TargetID: "d", Type: stage4.EdgeCalls}})
	g.InboundEdges = g.InboundEdges.Set("b", []stage4.ResolvedEdge{{SourceID: "a", TargetID: "b", Type: stage4.EdgeCalls}})
	g.InboundEdges = g.InboundEdges.Set("c", []stage4.ResolvedEdge{{SourceID: "b", TargetID: "c", Type: stage4.EdgeCalls}})
	g.InboundEdges = g.InboundEdges.Set("d", []stage4.ResolvedEdge{{SourceID: "c", TargetID: "d", Type: stage4.EdgeCalls}})

	aps := g.FindArticulationPoints()
	if len(aps) == 0 {
		t.Error("expected at least 1 articulation point in a line graph")
	}
}

func TestArticulationPoints_Empty(t *testing.T) {
	g := NewCodePropertyGraph("test")
	aps := g.FindArticulationPoints()
	if len(aps) != 0 {
		t.Errorf("expected 0 articulation points for empty graph, got %d", len(aps))
	}
}

// ==================== CalculatePageRank Tests ====================

func TestPageRank_OneNode(t *testing.T) {
	g := NewCodePropertyGraph("test")
	g.Nodes = g.Nodes.Set("a", &stage4.ResolvedNode{ID: "a", Kind: "NODE", Name: "a"})
	ranks := g.CalculatePageRank(10, 0.85)
	if len(ranks) != 1 {
		t.Errorf("expected 1 rank, got %d", len(ranks))
	}
	if ranks["a"] <= 0 {
		t.Errorf("expected positive rank for single node, got %f", ranks["a"])
	}
}

func TestPageRank_Empty(t *testing.T) {
	g := NewCodePropertyGraph("test")
	ranks := g.CalculatePageRank(10, 0.85)
	if len(ranks) != 0 {
		t.Errorf("expected empty ranks, got %d", len(ranks))
	}
}

// ==================== CalculateBetweennessCentrality Tests ====================

func TestBetweenness_Empty(t *testing.T) {
	g := NewCodePropertyGraph("test")
	bc := g.CalculateBetweennessCentrality()
	if len(bc) != 0 {
		t.Errorf("expected empty betweenness, got %d", len(bc))
	}
}

// ==================== DetectGodObjects Tests ====================

func TestGodObjects_None(t *testing.T) {
	g := NewCodePropertyGraph("test")
	for _, id := range []string{"a", "b", "c"} {
		g.Nodes = g.Nodes.Set(id, &stage4.ResolvedNode{ID: id, Kind: "STRUCT", Name: id})
	}
	godObjects := g.DetectGodObjects()
	if len(godObjects) != 0 {
		t.Errorf("expected 0 god objects in low-fan graph, got %d", len(godObjects))
	}
}

func TestGodObjects_Empty(t *testing.T) {
	g := NewCodePropertyGraph("test")
	godObjects := g.DetectGodObjects()
	if len(godObjects) != 0 {
		t.Errorf("expected 0 god objects for empty graph, got %d", len(godObjects))
	}
}

// ==================== FindIsolatedIslands Tests ====================

func TestIslands_Empty(t *testing.T) {
	g := NewCodePropertyGraph("test")
	islands := g.FindIsolatedIslands()
	if len(islands) != 0 {
		t.Errorf("expected 0 islands for empty graph, got %d", len(islands))
	}
}

// ==================== GetStructuralSimilarity Tests ====================

func TestSimilarity_Identical(t *testing.T) {
	g := NewCodePropertyGraph("test")
	g.Nodes = g.Nodes.Set("a", &stage4.ResolvedNode{ID: "a", Kind: "NODE", Name: "a"})
	g.Nodes = g.Nodes.Set("b", &stage4.ResolvedNode{ID: "b", Kind: "NODE", Name: "b"})
	g.Nodes = g.Nodes.Set("x", &stage4.ResolvedNode{ID: "x", Kind: "NODE", Name: "x"})
	g.OutboundEdges = g.OutboundEdges.Set("a", []stage4.ResolvedEdge{{SourceID: "a", TargetID: "x", Type: stage4.EdgeCalls}})
	g.OutboundEdges = g.OutboundEdges.Set("b", []stage4.ResolvedEdge{{SourceID: "b", TargetID: "x", Type: stage4.EdgeCalls}})

	sim := g.GetStructuralSimilarity("a", "b")
	if sim != 1.0 {
		t.Errorf("expected 1.0 similarity for identical edges, got %f", sim)
	}
}

func TestSimilarity_NoOverlap(t *testing.T) {
	g := NewCodePropertyGraph("test")
	g.Nodes = g.Nodes.Set("a", &stage4.ResolvedNode{ID: "a", Kind: "NODE", Name: "a"})
	g.Nodes = g.Nodes.Set("b", &stage4.ResolvedNode{ID: "b", Kind: "NODE", Name: "b"})
	g.Nodes = g.Nodes.Set("x", &stage4.ResolvedNode{ID: "x", Kind: "NODE", Name: "x"})
	g.Nodes = g.Nodes.Set("y", &stage4.ResolvedNode{ID: "y", Kind: "NODE", Name: "y"})
	g.OutboundEdges = g.OutboundEdges.Set("a", []stage4.ResolvedEdge{{SourceID: "a", TargetID: "x", Type: stage4.EdgeCalls}})
	g.OutboundEdges = g.OutboundEdges.Set("b", []stage4.ResolvedEdge{{SourceID: "b", TargetID: "y", Type: stage4.EdgeCalls}})

	sim := g.GetStructuralSimilarity("a", "b")
	if sim != 0.0 {
		t.Errorf("expected 0.0 similarity for disjoint edges, got %f", sim)
	}
}

func TestSimilarity_BothEmpty(t *testing.T) {
	g := NewCodePropertyGraph("test")
	g.Nodes = g.Nodes.Set("a", &stage4.ResolvedNode{ID: "a", Kind: "NODE", Name: "a"})
	g.Nodes = g.Nodes.Set("b", &stage4.ResolvedNode{ID: "b", Kind: "NODE", Name: "b"})

	sim := g.GetStructuralSimilarity("a", "b")
	if sim != 1.0 {
		t.Errorf("expected 1.0 similarity when both have no edges, got %f", sim)
	}
}

// ==================== GetTopologicalSort Tests ====================

func TestTopoSort_DAG(t *testing.T) {
	g := NewCodePropertyGraph("test")
	g.Nodes = g.Nodes.Set("a", &stage4.ResolvedNode{ID: "a", Kind: "FUNCTION", Name: "a"})
	g.Nodes = g.Nodes.Set("b", &stage4.ResolvedNode{ID: "b", Kind: "FUNCTION", Name: "b"})
	g.Nodes = g.Nodes.Set("c", &stage4.ResolvedNode{ID: "c", Kind: "FUNCTION", Name: "c"})
	g.OutboundEdges = g.OutboundEdges.Set("a", []stage4.ResolvedEdge{{SourceID: "a", TargetID: "b", Type: stage4.EdgeCalls}})
	g.OutboundEdges = g.OutboundEdges.Set("b", []stage4.ResolvedEdge{{SourceID: "b", TargetID: "c", Type: stage4.EdgeCalls}})

	sorted, ok := g.GetTopologicalSort()
	if !ok {
		t.Error("expected valid topological sort for DAG")
	}
	if len(sorted) != 3 {
		t.Errorf("expected 3 nodes sorted, got %d", len(sorted))
	}
}

func TestTopoSort_WithCycle(t *testing.T) {
	g := NewCodePropertyGraph("test")
	g.Nodes = g.Nodes.Set("a", &stage4.ResolvedNode{ID: "a", Kind: "FUNCTION", Name: "a"})
	g.Nodes = g.Nodes.Set("b", &stage4.ResolvedNode{ID: "b", Kind: "FUNCTION", Name: "b"})
	g.Nodes = g.Nodes.Set("c", &stage4.ResolvedNode{ID: "c", Kind: "FUNCTION", Name: "c"})
	g.OutboundEdges = g.OutboundEdges.Set("a", []stage4.ResolvedEdge{{SourceID: "a", TargetID: "b", Type: stage4.EdgeCalls}})
	g.OutboundEdges = g.OutboundEdges.Set("b", []stage4.ResolvedEdge{{SourceID: "b", TargetID: "c", Type: stage4.EdgeCalls}})
	g.OutboundEdges = g.OutboundEdges.Set("c", []stage4.ResolvedEdge{{SourceID: "c", TargetID: "a", Type: stage4.EdgeCalls}})

	_, ok := g.GetTopologicalSort()
	if ok {
		t.Error("expected false for cyclic graph")
	}
}

func TestTopoSort_SingleNode(t *testing.T) {
	g := NewCodePropertyGraph("test")
	g.Nodes = g.Nodes.Set("a", &stage4.ResolvedNode{ID: "a", Kind: "FUNCTION", Name: "a"})

	sorted, ok := g.GetTopologicalSort()
	if !ok {
		t.Error("expected true for single node")
	}
	if len(sorted) != 1 || sorted[0] != "a" {
		t.Errorf("expected [a], got %v", sorted)
	}
}

func TestTopoSort_Empty(t *testing.T) {
	g := NewCodePropertyGraph("test")
	sorted, ok := g.GetTopologicalSort()
	if !ok {
		t.Error("expected true for empty graph")
	}
	if len(sorted) != 0 {
		t.Errorf("expected empty sort, got %d", len(sorted))
	}
}

// ==================== FindPath Tests ====================

func TestFindPath_Exists(t *testing.T) {
	g := NewCodePropertyGraph("test")
	g.Nodes = g.Nodes.Set("a", &stage4.ResolvedNode{ID: "a", Kind: "FUNCTION", Name: "a"})
	g.Nodes = g.Nodes.Set("b", &stage4.ResolvedNode{ID: "b", Kind: "FUNCTION", Name: "b"})
	g.Nodes = g.Nodes.Set("c", &stage4.ResolvedNode{ID: "c", Kind: "FUNCTION", Name: "c"})
	g.OutboundEdges = g.OutboundEdges.Set("a", []stage4.ResolvedEdge{{SourceID: "a", TargetID: "b", Type: stage4.EdgeCalls}})
	g.OutboundEdges = g.OutboundEdges.Set("b", []stage4.ResolvedEdge{{SourceID: "b", TargetID: "c", Type: stage4.EdgeCalls}})

	path := g.FindPath("a", "c", 10)
	if len(path) != 3 {
		t.Errorf("expected path [a b c], got %v", path)
	}
}

func TestFindPath_NoPath(t *testing.T) {
	g := NewCodePropertyGraph("test")
	g.Nodes = g.Nodes.Set("a", &stage4.ResolvedNode{ID: "a", Kind: "FUNCTION", Name: "a"})
	g.Nodes = g.Nodes.Set("b", &stage4.ResolvedNode{ID: "b", Kind: "FUNCTION", Name: "b"})

	path := g.FindPath("a", "b", 10)
	if path != nil {
		t.Errorf("expected nil path for disconnected nodes, got %v", path)
	}
}

func TestFindPath_MissingStart(t *testing.T) {
	g := NewCodePropertyGraph("test")
	g.Nodes = g.Nodes.Set("b", &stage4.ResolvedNode{ID: "b", Kind: "FUNCTION", Name: "b"})

	path := g.FindPath("a", "b", 10)
	if path != nil {
		t.Errorf("expected nil for missing start, got %v", path)
	}
}

// ==================== GetOrphanNodes Tests ====================

func TestOrphans_Mixed(t *testing.T) {
	g := NewCodePropertyGraph("test")
	g.Nodes = g.Nodes.Set("a", &stage4.ResolvedNode{ID: "a", Kind: "FUNCTION", Name: "a"})
	g.Nodes = g.Nodes.Set("b", &stage4.ResolvedNode{ID: "b", Kind: "FUNCTION", Name: "b"})
	g.Nodes = g.Nodes.Set("c", &stage4.ResolvedNode{ID: "c", Kind: "FUNCTION", Name: "c"})
	g.OutboundEdges = g.OutboundEdges.Set("a", []stage4.ResolvedEdge{{SourceID: "a", TargetID: "b", Type: stage4.EdgeCalls}})
	g.InboundEdges = g.InboundEdges.Set("b", []stage4.ResolvedEdge{{SourceID: "a", TargetID: "b", Type: stage4.EdgeCalls}})
	g.Entrypoints = []string{"a"}

	orphans := g.GetOrphanNodes()
	if len(orphans) != 1 || orphans[0] != "c" {
		t.Errorf("expected orphan [c], got %v", orphans)
	}
}

func TestOrphans_Entrypoint(t *testing.T) {
	g := NewCodePropertyGraph("test")
	g.Nodes = g.Nodes.Set("a", &stage4.ResolvedNode{ID: "a", Kind: "FUNCTION", Name: "main"})
	g.Entrypoints = []string{"a"}

	orphans := g.GetOrphanNodes()
	if len(orphans) != 0 {
		t.Errorf("expected 0 orphans when node is entrypoint, got %v", orphans)
	}
}

// ==================== CalculateInstability Tests ====================

func TestInstability_Stable(t *testing.T) {
	g := NewCodePropertyGraph("test")
	g.Nodes = g.Nodes.Set("a", &stage4.ResolvedNode{ID: "a", Kind: "FUNCTION", Name: "a"})
	g.InboundEdges = g.InboundEdges.Set("a", []stage4.ResolvedEdge{
		{SourceID: "x", TargetID: "a", Type: stage4.EdgeCalls},
		{SourceID: "y", TargetID: "a", Type: stage4.EdgeCalls},
		{SourceID: "z", TargetID: "a", Type: stage4.EdgeCalls},
	})
	inst := g.CalculateInstability("a")
	if inst != 0.0 {
		t.Errorf("expected 0.0 instability (ca only), got %f", inst)
	}
}

func TestInstability_Unstable(t *testing.T) {
	g := NewCodePropertyGraph("test")
	g.Nodes = g.Nodes.Set("a", &stage4.ResolvedNode{ID: "a", Kind: "FUNCTION", Name: "a"})
	g.OutboundEdges = g.OutboundEdges.Set("a", []stage4.ResolvedEdge{
		{SourceID: "a", TargetID: "x", Type: stage4.EdgeCalls},
		{SourceID: "a", TargetID: "y", Type: stage4.EdgeCalls},
	})
	inst := g.CalculateInstability("a")
	if inst != 1.0 {
		t.Errorf("expected 1.0 instability (ce only), got %f", inst)
	}
}

func TestInstability_Isolated(t *testing.T) {
	g := NewCodePropertyGraph("test")
	g.Nodes = g.Nodes.Set("a", &stage4.ResolvedNode{ID: "a", Kind: "FUNCTION", Name: "a"})
	inst := g.CalculateInstability("a")
	if inst != 0.0 {
		t.Errorf("expected 0.0 instability for isolated node, got %f", inst)
	}
}

// ==================== CalculateImpactRadius Tests ====================

func TestImpactRadius_Transitive(t *testing.T) {
	g := NewCodePropertyGraph("test")
	g.Nodes = g.Nodes.Set("c", &stage4.ResolvedNode{ID: "c", Kind: "FUNCTION", Name: "c"})
	g.InboundEdges = g.InboundEdges.Set("c", []stage4.ResolvedEdge{{SourceID: "b", TargetID: "c", Type: stage4.EdgeCalls}})
	g.Nodes = g.Nodes.Set("b", &stage4.ResolvedNode{ID: "b", Kind: "FUNCTION", Name: "b"})
	g.InboundEdges = g.InboundEdges.Set("b", []stage4.ResolvedEdge{{SourceID: "a", TargetID: "b", Type: stage4.EdgeCalls}})

	_ = g.CalculateImpactRadius("c")
}

func TestImpactRadius_NoDependents(t *testing.T) {
	g := NewCodePropertyGraph("test")
	g.Nodes = g.Nodes.Set("a", &stage4.ResolvedNode{ID: "a", Kind: "FUNCTION", Name: "a"})
	radius := g.CalculateImpactRadius("a")
	if radius != 0 {
		t.Errorf("expected 0 impact for leaf node, got %d", radius)
	}
}

// ==================== CalculatePackageCohesion Tests ====================

func TestPackageCohesion_NoComponents(t *testing.T) {
	g := NewCodePropertyGraph("test")
	g.Nodes = g.Nodes.Set("pkg", &stage4.ResolvedNode{ID: "pkg", Kind: "PACKAGE", Name: "pkg"})

	cohesion := g.CalculatePackageCohesion("pkg")
	if cohesion != 0.0 {
		t.Errorf("expected 0.0 cohesion for empty package, got %f", cohesion)
	}
}

func TestPackageCohesion_SingleComponent(t *testing.T) {
	g := NewCodePropertyGraph("test")
	g.Nodes = g.Nodes.Set("pkg", &stage4.ResolvedNode{ID: "pkg", Kind: "PACKAGE", Name: "pkg"})
	g.Nodes = g.Nodes.Set("a", &stage4.ResolvedNode{ID: "a", Kind: "CLASS", Name: "A"})
	g.OutboundEdges = g.OutboundEdges.Set("a", []stage4.ResolvedEdge{{SourceID: "a", TargetID: "pkg", Type: stage4.EdgeBelongsTo}})
	g.InboundEdges = g.InboundEdges.Set("pkg", []stage4.ResolvedEdge{{SourceID: "a", TargetID: "pkg", Type: stage4.EdgeBelongsTo}})

	cohesion := g.CalculatePackageCohesion("pkg")
	if cohesion != 0.0 {
		t.Errorf("expected 0.0 cohesion for single component, got %f", cohesion)
	}
}

// ==================== NewCodePropertyGraph Tests ====================

func TestNewCodePropertyGraph_InitializesMaps(t *testing.T) {
	g := NewCodePropertyGraph("testhash")
	if g.Nodes == nil {
		t.Error("Nodes map should be initialized")
	}
	if g.OutboundEdges == nil {
		t.Error("OutboundEdges map should be initialized")
	}
	if g.InboundEdges == nil {
		t.Error("InboundEdges map should be initialized")
	}
	if g.KindIndex == nil {
		t.Error("KindIndex should be initialized")
	}
	if g.CommitHash != "testhash" {
		t.Errorf("expected commit hash 'testhash', got %q", g.CommitHash)
	}
	if g.SchemaVersion != CurrentSchemaVersion {
		t.Errorf("expected schema version %d, got %d", CurrentSchemaVersion, g.SchemaVersion)
	}
}

// ===== ADDITIONAL DetectCycles TESTS =====

func TestDetectCycles_MultipleCycles(t *testing.T) {
	g := NewCodePropertyGraph("test")
	g.Nodes = g.Nodes.Set("a", &stage4.ResolvedNode{ID: "a", Kind: "NODE", Name: "a"})
	g.Nodes = g.Nodes.Set("b", &stage4.ResolvedNode{ID: "b", Kind: "NODE", Name: "b"})
	g.Nodes = g.Nodes.Set("c", &stage4.ResolvedNode{ID: "c", Kind: "NODE", Name: "c"})
	g.Nodes = g.Nodes.Set("d", &stage4.ResolvedNode{ID: "d", Kind: "NODE", Name: "d"})
	g.Nodes = g.Nodes.Set("e", &stage4.ResolvedNode{ID: "e", Kind: "NODE", Name: "e"})
	g.Nodes = g.Nodes.Set("f", &stage4.ResolvedNode{ID: "f", Kind: "NODE", Name: "f"})
	g.OutboundEdges = g.OutboundEdges.Set("a", []stage4.ResolvedEdge{{SourceID: "a", TargetID: "b", Type: stage4.EdgeCalls}})
	g.OutboundEdges = g.OutboundEdges.Set("b", []stage4.ResolvedEdge{{SourceID: "b", TargetID: "c", Type: stage4.EdgeCalls}})
	g.OutboundEdges = g.OutboundEdges.Set("c", []stage4.ResolvedEdge{{SourceID: "c", TargetID: "a", Type: stage4.EdgeCalls}})
	g.OutboundEdges = g.OutboundEdges.Set("d", []stage4.ResolvedEdge{{SourceID: "d", TargetID: "e", Type: stage4.EdgeCalls}})
	g.OutboundEdges = g.OutboundEdges.Set("e", []stage4.ResolvedEdge{{SourceID: "e", TargetID: "f", Type: stage4.EdgeCalls}})
	g.OutboundEdges = g.OutboundEdges.Set("f", []stage4.ResolvedEdge{{SourceID: "f", TargetID: "d", Type: stage4.EdgeCalls}})

	cycles := g.DetectCycles()
	if len(cycles) != 2 {
		t.Errorf("expected 2 cycles, got %d", len(cycles))
	}
}

func TestDetectCycles_SelfLoop(t *testing.T) {
	g := NewCodePropertyGraph("test")
	g.Nodes = g.Nodes.Set("a", &stage4.ResolvedNode{ID: "a", Kind: "NODE", Name: "a"})
	g.OutboundEdges = g.OutboundEdges.Set("a", []stage4.ResolvedEdge{{SourceID: "a", TargetID: "a", Type: stage4.EdgeCalls}})

	_ = g.DetectCycles()
}

func TestDetectCycles_Disconnected(t *testing.T) {
	g := NewCodePropertyGraph("test")
	g.Nodes = g.Nodes.Set("a", &stage4.ResolvedNode{ID: "a", Kind: "NODE", Name: "a"})
	g.Nodes = g.Nodes.Set("b", &stage4.ResolvedNode{ID: "b", Kind: "NODE", Name: "b"})
	g.Nodes = g.Nodes.Set("c", &stage4.ResolvedNode{ID: "c", Kind: "NODE", Name: "c"})
	g.Nodes = g.Nodes.Set("d", &stage4.ResolvedNode{ID: "d", Kind: "NODE", Name: "d"})
	g.OutboundEdges = g.OutboundEdges.Set("a", []stage4.ResolvedEdge{{SourceID: "a", TargetID: "b", Type: stage4.EdgeCalls}})
	g.OutboundEdges = g.OutboundEdges.Set("c", []stage4.ResolvedEdge{{SourceID: "c", TargetID: "d", Type: stage4.EdgeCalls}})

	cycles := g.DetectCycles()
	if len(cycles) != 0 {
		t.Errorf("expected 0 cycles in disconnected DAG, got %d", len(cycles))
	}
}

// ===== ADDITIONAL FindArticulationPoints TESTS =====

func TestArticulationPoints_StarGraph(t *testing.T) {
	g := NewCodePropertyGraph("test")
	g.Nodes = g.Nodes.Set("center", &stage4.ResolvedNode{ID: "center", Kind: "NODE", Name: "center"})
	for _, leaf := range []string{"a", "b", "c"} {
		g.Nodes = g.Nodes.Set(leaf, &stage4.ResolvedNode{ID: leaf, Kind: "NODE", Name: leaf})

		edges, _ := g.OutboundEdges.Get("center")
		g.OutboundEdges = g.OutboundEdges.Set("center", append(edges,
			stage4.ResolvedEdge{SourceID: "center", TargetID: leaf, Type: stage4.EdgeCalls}))

		inEdges, _ := g.InboundEdges.Get(leaf)
		g.InboundEdges = g.InboundEdges.Set(leaf, append(inEdges,
			stage4.ResolvedEdge{SourceID: "center", TargetID: leaf, Type: stage4.EdgeCalls}))
	}

	aps := g.FindArticulationPoints()
	found := false
	for _, ap := range aps {
		if ap == "center" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected center to be articulation point in star graph")
	}
}

func TestArticulationPoints_Tree(t *testing.T) {
	g := NewCodePropertyGraph("test")
	g.Nodes = g.Nodes.Set("root", &stage4.ResolvedNode{ID: "root", Kind: "NODE", Name: "root"})
	g.Nodes = g.Nodes.Set("a", &stage4.ResolvedNode{ID: "a", Kind: "NODE", Name: "a"})
	g.Nodes = g.Nodes.Set("b", &stage4.ResolvedNode{ID: "b", Kind: "NODE", Name: "b"})
	g.Nodes = g.Nodes.Set("a1", &stage4.ResolvedNode{ID: "a1", Kind: "NODE", Name: "a1"})
	g.Nodes = g.Nodes.Set("b1", &stage4.ResolvedNode{ID: "b1", Kind: "NODE", Name: "b1"})
	g.OutboundEdges = g.OutboundEdges.Set("root", []stage4.ResolvedEdge{
		{SourceID: "root", TargetID: "a", Type: stage4.EdgeCalls},
		{SourceID: "root", TargetID: "b", Type: stage4.EdgeCalls},
	})
	g.InboundEdges = g.InboundEdges.Set("a", []stage4.ResolvedEdge{{SourceID: "root", TargetID: "a", Type: stage4.EdgeCalls}})
	g.InboundEdges = g.InboundEdges.Set("b", []stage4.ResolvedEdge{{SourceID: "root", TargetID: "b", Type: stage4.EdgeCalls}})
	g.OutboundEdges = g.OutboundEdges.Set("a", []stage4.ResolvedEdge{{SourceID: "a", TargetID: "a1", Type: stage4.EdgeCalls}})
	g.InboundEdges = g.InboundEdges.Set("a1", []stage4.ResolvedEdge{{SourceID: "a", TargetID: "a1", Type: stage4.EdgeCalls}})
	g.OutboundEdges = g.OutboundEdges.Set("b", []stage4.ResolvedEdge{{SourceID: "b", TargetID: "b1", Type: stage4.EdgeCalls}})
	g.InboundEdges = g.InboundEdges.Set("b1", []stage4.ResolvedEdge{{SourceID: "b", TargetID: "b1", Type: stage4.EdgeCalls}})

	aps := g.FindArticulationPoints()
	if len(aps) == 0 {
		t.Error("expected at least root as articulation point in tree graph")
	}
}

func TestArticulationPoints_Cycle(t *testing.T) {
	g := NewCodePropertyGraph("test")
	ids := []string{"a", "b", "c", "d"}
	for _, id := range ids {
		g.Nodes = g.Nodes.Set(id, &stage4.ResolvedNode{ID: id, Kind: "NODE", Name: id})
	}
	g.OutboundEdges = g.OutboundEdges.Set("a", []stage4.ResolvedEdge{{SourceID: "a", TargetID: "b", Type: stage4.EdgeCalls}})
	g.OutboundEdges = g.OutboundEdges.Set("b", []stage4.ResolvedEdge{{SourceID: "b", TargetID: "c", Type: stage4.EdgeCalls}})
	g.OutboundEdges = g.OutboundEdges.Set("c", []stage4.ResolvedEdge{{SourceID: "c", TargetID: "d", Type: stage4.EdgeCalls}})
	g.OutboundEdges = g.OutboundEdges.Set("d", []stage4.ResolvedEdge{{SourceID: "d", TargetID: "a", Type: stage4.EdgeCalls}})
	g.InboundEdges = g.InboundEdges.Set("b", []stage4.ResolvedEdge{{SourceID: "a", TargetID: "b", Type: stage4.EdgeCalls}})
	g.InboundEdges = g.InboundEdges.Set("c", []stage4.ResolvedEdge{{SourceID: "b", TargetID: "c", Type: stage4.EdgeCalls}})
	g.InboundEdges = g.InboundEdges.Set("d", []stage4.ResolvedEdge{{SourceID: "c", TargetID: "d", Type: stage4.EdgeCalls}})
	g.InboundEdges = g.InboundEdges.Set("a", []stage4.ResolvedEdge{{SourceID: "d", TargetID: "a", Type: stage4.EdgeCalls}})

	aps := g.FindArticulationPoints()
	_ = aps
}

func TestArticulationPoints_Disconnected(t *testing.T) {
	g := NewCodePropertyGraph("test")
	g.Nodes = g.Nodes.Set("a", &stage4.ResolvedNode{ID: "a", Kind: "NODE", Name: "a"})
	g.Nodes = g.Nodes.Set("b", &stage4.ResolvedNode{ID: "b", Kind: "NODE", Name: "b"})
	g.OutboundEdges = g.OutboundEdges.Set("a", []stage4.ResolvedEdge{{SourceID: "a", TargetID: "b", Type: stage4.EdgeCalls}})
	g.OutboundEdges = g.OutboundEdges.Set("c", []stage4.ResolvedEdge{{SourceID: "c", TargetID: "d", Type: stage4.EdgeCalls}})

	aps := g.FindArticulationPoints()
	_ = aps
}

// ===== ADDITIONAL PageRank TESTS =====

func TestPageRank_TwoNodesOneEdge(t *testing.T) {
	g := NewCodePropertyGraph("test")
	g.Nodes = g.Nodes.Set("a", &stage4.ResolvedNode{ID: "a", Kind: "NODE", Name: "a"})
	g.Nodes = g.Nodes.Set("b", &stage4.ResolvedNode{ID: "b", Kind: "NODE", Name: "b"})
	g.OutboundEdges = g.OutboundEdges.Set("a", []stage4.ResolvedEdge{{SourceID: "a", TargetID: "b", Type: stage4.EdgeCalls}})
	g.InboundEdges = g.InboundEdges.Set("b", []stage4.ResolvedEdge{{SourceID: "a", TargetID: "b", Type: stage4.EdgeCalls}})

	ranks := g.CalculatePageRank(10, 0.85)
	if ranks["b"] <= ranks["a"] {
		t.Error("b should have higher rank because a flows into it")
	}
}

func TestPageRank_NoEdges(t *testing.T) {
	g := NewCodePropertyGraph("test")
	for i := 0; i < 3; i++ {
		id := fmt.Sprintf("n%d", i)
		g.Nodes = g.Nodes.Set(id, &stage4.ResolvedNode{ID: id, Kind: "NODE", Name: id})
	}

	ranks := g.CalculatePageRank(10, 0.85)
	if len(ranks) != 3 {
		t.Errorf("expected 3 ranks, got %d", len(ranks))
	}
	if ranks["n0"] != ranks["n1"] || ranks["n1"] != ranks["n2"] {
		t.Error("expected equal ranks when no edges")
	}
}

func TestPageRank_CustomIterations(t *testing.T) {
	g := NewCodePropertyGraph("test")
	g.Nodes = g.Nodes.Set("a", &stage4.ResolvedNode{ID: "a", Kind: "NODE", Name: "a"})
	g.Nodes = g.Nodes.Set("b", &stage4.ResolvedNode{ID: "b", Kind: "NODE", Name: "b"})
	g.OutboundEdges = g.OutboundEdges.Set("a", []stage4.ResolvedEdge{{SourceID: "a", TargetID: "b", Type: stage4.EdgeCalls}})
	g.InboundEdges = g.InboundEdges.Set("b", []stage4.ResolvedEdge{{SourceID: "a", TargetID: "b", Type: stage4.EdgeCalls}})

	r1 := g.CalculatePageRank(1, 0.85)
	r10 := g.CalculatePageRank(10, 0.85)
	if r1["b"] == r10["b"] {
		t.Errorf("expected different values with different iteration counts, r1=%v r10=%v", r1, r10)
	}
}

// ===== ADDITIONAL BetweennessCentrality TESTS =====

func TestBetweenness_LineGraph(t *testing.T) {
	g := NewCodePropertyGraph("test")
	for _, id := range []string{"a", "b", "c", "d"} {
		g.Nodes = g.Nodes.Set(id, &stage4.ResolvedNode{ID: id, Kind: "STRUCT", Name: id})
	}
	g.OutboundEdges = g.OutboundEdges.Set("a", []stage4.ResolvedEdge{{SourceID: "a", TargetID: "b", Type: stage4.EdgeCalls}})
	g.OutboundEdges = g.OutboundEdges.Set("b", []stage4.ResolvedEdge{{SourceID: "b", TargetID: "c", Type: stage4.EdgeCalls}})
	g.OutboundEdges = g.OutboundEdges.Set("c", []stage4.ResolvedEdge{{SourceID: "c", TargetID: "d", Type: stage4.EdgeCalls}})

	bc := g.CalculateBetweennessCentrality(true)
	if bc["b"] <= 0 || bc["c"] <= 0 {
		t.Errorf("expected positive betweenness for B and C in line graph, got B=%f C=%f", bc["b"], bc["c"])
	}
}

func TestBetweenness_AllKinds(t *testing.T) {
	g := NewCodePropertyGraph("test")
	g.Nodes = g.Nodes.Set("f", &stage4.ResolvedNode{ID: "f", Kind: "FUNCTION", Name: "func1"})
	g.Nodes = g.Nodes.Set("m", &stage4.ResolvedNode{ID: "m", Kind: "METHOD", Name: "method1"})
	g.Nodes = g.Nodes.Set("s", &stage4.ResolvedNode{ID: "s", Kind: "STRUCT", Name: "struct1"})
	g.OutboundEdges = g.OutboundEdges.Set("f", []stage4.ResolvedEdge{{SourceID: "f", TargetID: "m", Type: stage4.EdgeCalls}})
	g.OutboundEdges = g.OutboundEdges.Set("m", []stage4.ResolvedEdge{{SourceID: "m", TargetID: "s", Type: stage4.EdgeCalls}})

	bcAll := g.CalculateBetweennessCentrality(true)
	if _, ok := bcAll["f"]; !ok {
		t.Error("expected FUNCTION node in betweenness with includeAll=true")
	}
	if _, ok := bcAll["m"]; !ok {
		t.Error("expected METHOD node in betweenness with includeAll=true")
	}

	bcMajor := g.CalculateBetweennessCentrality(false)
	if _, ok := bcMajor["f"]; ok {
		t.Error("FUNCTION should not be in betweenness with includeAll=false")
	}

	// Verify that centrality values differ between includeAll modes
	resultAll := g.CalculateBetweennessCentrality(true)
	resultRestricted := g.CalculateBetweennessCentrality(false)

	// When includeAll=true, all nodes have entries in the result
	// When includeAll=false, only major kinds (STRUCT/CLASS/MODULE/PACKAGE/FILE) have entries
	// Check that FUNCTION nodes appear in one but not the other
	funcNodes := 0
	g.Nodes.Iterate(func(id string, node *stage4.ResolvedNode) {
		if node.Kind == "FUNCTION" {
			_, inAll := resultAll[id]
			_, inRestricted := resultRestricted[id]
			assert.True(t, inAll, "FUNCTION node should be in includeAll=true result")
			if inRestricted {
				funcNodes++
			}
		}
	})
	t.Logf("FUNCTION nodes also in restricted result: %d", funcNodes)
}

// ===== ADDITIONAL GodObjects TESTS =====

func TestGodObjects_OneGod(t *testing.T) {
	g := NewCodePropertyGraph("test")
	for _, id := range []string{"a", "b", "c"} {
		g.Nodes = g.Nodes.Set(id, &stage4.ResolvedNode{ID: id, Kind: "STRUCT", Name: id})
	}
	g.Nodes = g.Nodes.Set("g", &stage4.ResolvedNode{ID: "g", Kind: "STRUCT", Name: "God"})
	for i := 0; i < 15; i++ {
		dep := fmt.Sprintf("dep%d", i)
		g.Nodes = g.Nodes.Set(dep, &stage4.ResolvedNode{ID: dep, Kind: "STRUCT", Name: dep})

		edges, _ := g.OutboundEdges.Get("g")
		g.OutboundEdges = g.OutboundEdges.Set("g", append(edges,
			stage4.ResolvedEdge{SourceID: "g", TargetID: dep, Type: stage4.EdgeCalls}))

		inEdges, _ := g.InboundEdges.Get("g")
		g.InboundEdges = g.InboundEdges.Set("g", append(inEdges,
			stage4.ResolvedEdge{SourceID: dep, TargetID: "g", Type: stage4.EdgeCalls}))
	}

	_ = g.DetectGodObjects()
}

func TestGodObjects_OnlyRelevantKinds(t *testing.T) {
	g := NewCodePropertyGraph("test")
	g.Nodes = g.Nodes.Set("fn", &stage4.ResolvedNode{ID: "fn", Kind: "FUNCTION", Name: "hotFunc"})
	for i := 0; i < 20; i++ {
		dep := fmt.Sprintf("dep%d", i)
		g.Nodes = g.Nodes.Set(dep, &stage4.ResolvedNode{ID: dep, Kind: "FUNCTION", Name: dep})

		edges, _ := g.OutboundEdges.Get("fn")
		g.OutboundEdges = g.OutboundEdges.Set("fn", append(edges,
			stage4.ResolvedEdge{SourceID: "fn", TargetID: dep, Type: stage4.EdgeCalls}))

		inEdges, _ := g.InboundEdges.Get("fn")
		g.InboundEdges = g.InboundEdges.Set("fn", append(inEdges,
			stage4.ResolvedEdge{SourceID: dep, TargetID: "fn", Type: stage4.EdgeCalls}))
	}

	objects := g.DetectGodObjects()
	for _, obj := range objects {
		if obj == "fn" {
			t.Error("FUNCTION should not be flagged as GodObject")
		}
	}
}

// ===== ADDITIONAL FindIsolatedIslands TESTS =====

func TestIslands_OneIsland(t *testing.T) {
	g := NewCodePropertyGraph("test")
	g.Nodes = g.Nodes.Set("main", &stage4.ResolvedNode{ID: "main", Kind: "FUNCTION", Name: "main"})
	g.Nodes = g.Nodes.Set("helper", &stage4.ResolvedNode{ID: "helper", Kind: "FUNCTION", Name: "helper"})
	g.Entrypoints = []string{"main"}
	g.OutboundEdges = g.OutboundEdges.Set("main", []stage4.ResolvedEdge{{SourceID: "main", TargetID: "helper", Type: stage4.EdgeCalls}})
	g.InboundEdges = g.InboundEdges.Set("helper", []stage4.ResolvedEdge{{SourceID: "main", TargetID: "helper", Type: stage4.EdgeCalls}})
	g.Nodes = g.Nodes.Set("iso1", &stage4.ResolvedNode{ID: "iso1", Kind: "FUNCTION", Name: "iso1"})
	g.Nodes = g.Nodes.Set("iso2", &stage4.ResolvedNode{ID: "iso2", Kind: "FUNCTION", Name: "iso2"})
	g.OutboundEdges = g.OutboundEdges.Set("iso1", []stage4.ResolvedEdge{{SourceID: "iso1", TargetID: "iso2", Type: stage4.EdgeCalls}})
	g.InboundEdges = g.InboundEdges.Set("iso2", []stage4.ResolvedEdge{{SourceID: "iso1", TargetID: "iso2", Type: stage4.EdgeCalls}})

	islands := g.FindIsolatedIslands()
	if len(islands) != 1 {
		t.Errorf("expected 1 island, got %d", len(islands))
	}
}

func TestIslands_None(t *testing.T) {
	g := NewCodePropertyGraph("test")
	g.Nodes = g.Nodes.Set("main", &stage4.ResolvedNode{ID: "main", Kind: "FUNCTION", Name: "main"})
	g.Nodes = g.Nodes.Set("helper", &stage4.ResolvedNode{ID: "helper", Kind: "FUNCTION", Name: "helper"})
	g.Entrypoints = []string{"main"}
	g.OutboundEdges = g.OutboundEdges.Set("main", []stage4.ResolvedEdge{{SourceID: "main", TargetID: "helper", Type: stage4.EdgeCalls}})
	g.InboundEdges = g.InboundEdges.Set("helper", []stage4.ResolvedEdge{{SourceID: "main", TargetID: "helper", Type: stage4.EdgeCalls}})

	islands := g.FindIsolatedIslands()
	if len(islands) != 0 {
		t.Errorf("expected 0 islands when entrypoint present, got %d", len(islands))
	}
}

func TestIslands_MultipleIslands(t *testing.T) {
	g := NewCodePropertyGraph("test")
	g.Nodes = g.Nodes.Set("main", &stage4.ResolvedNode{ID: "main", Kind: "FUNCTION", Name: "main"})
	g.Entrypoints = []string{"main"}
	for _, id := range []string{"i1a", "i1b"} {
		g.Nodes = g.Nodes.Set(id, &stage4.ResolvedNode{ID: id, Kind: "FUNCTION", Name: id})
	}
	g.OutboundEdges = g.OutboundEdges.Set("i1a", []stage4.ResolvedEdge{{SourceID: "i1a", TargetID: "i1b", Type: stage4.EdgeCalls}})
	g.InboundEdges = g.InboundEdges.Set("i1b", []stage4.ResolvedEdge{{SourceID: "i1a", TargetID: "i1b", Type: stage4.EdgeCalls}})
	for _, id := range []string{"i2a", "i2b"} {
		g.Nodes = g.Nodes.Set(id, &stage4.ResolvedNode{ID: id, Kind: "FUNCTION", Name: id})
	}
	g.OutboundEdges = g.OutboundEdges.Set("i2a", []stage4.ResolvedEdge{{SourceID: "i2a", TargetID: "i2b", Type: stage4.EdgeCalls}})
	g.InboundEdges = g.InboundEdges.Set("i2b", []stage4.ResolvedEdge{{SourceID: "i2a", TargetID: "i2b", Type: stage4.EdgeCalls}})

	islands := g.FindIsolatedIslands()
	if len(islands) != 2 {
		t.Errorf("expected 2 islands, got %d", len(islands))
	}
}

func TestIslands_IslandHasEntrypoint(t *testing.T) {
	g := NewCodePropertyGraph("test")
	g.Nodes = g.Nodes.Set("iso1", &stage4.ResolvedNode{ID: "iso1", Kind: "FUNCTION", Name: "iso1"})
	g.Nodes = g.Nodes.Set("iso2", &stage4.ResolvedNode{ID: "iso2", Kind: "FUNCTION", Name: "iso2"})
	g.Entrypoints = []string{"iso1"}
	g.OutboundEdges = g.OutboundEdges.Set("iso1", []stage4.ResolvedEdge{{SourceID: "iso1", TargetID: "iso2", Type: stage4.EdgeCalls}})

	islands := g.FindIsolatedIslands()
	for _, island := range islands {
		for _, id := range island {
			if id == "iso1" {
				t.Error("component with entrypoint should not be in island")
			}
		}
	}
}

// ===== ADDITIONAL Similarity TESTS =====

func TestSimilarity_Partial(t *testing.T) {
	g := NewCodePropertyGraph("test")
	g.Nodes = g.Nodes.Set("a", &stage4.ResolvedNode{ID: "a", Kind: "NODE", Name: "a"})
	g.Nodes = g.Nodes.Set("b", &stage4.ResolvedNode{ID: "b", Kind: "NODE", Name: "b"})
	g.Nodes = g.Nodes.Set("x", &stage4.ResolvedNode{ID: "x", Kind: "NODE", Name: "x"})
	g.Nodes = g.Nodes.Set("y", &stage4.ResolvedNode{ID: "y", Kind: "NODE", Name: "y"})
	g.OutboundEdges = g.OutboundEdges.Set("a", []stage4.ResolvedEdge{
		{SourceID: "a", TargetID: "x", Type: stage4.EdgeCalls},
		{SourceID: "a", TargetID: "y", Type: stage4.EdgeCalls},
	})
	g.OutboundEdges = g.OutboundEdges.Set("b", []stage4.ResolvedEdge{
		{SourceID: "b", TargetID: "x", Type: stage4.EdgeCalls},
	})

	sim := g.GetStructuralSimilarity("a", "b")
	if sim <= 0 || sim >= 1 {
		t.Errorf("expected partial similarity between 0 and 1, got %f", sim)
	}
}

// ===== ADDITIONAL TopoSort TESTS =====

func TestTopoSort_Disconnected(t *testing.T) {
	g := NewCodePropertyGraph("test")
	g.Nodes = g.Nodes.Set("a", &stage4.ResolvedNode{ID: "a", Kind: "FUNCTION", Name: "a"})
	g.Nodes = g.Nodes.Set("b", &stage4.ResolvedNode{ID: "b", Kind: "FUNCTION", Name: "b"})
	g.OutboundEdges = g.OutboundEdges.Set("a", []stage4.ResolvedEdge{{SourceID: "a", TargetID: "b", Type: stage4.EdgeCalls}})

	sorted, ok := g.GetTopologicalSort()
	if !ok || len(sorted) != 2 {
		t.Errorf("expected valid sort with 2 nodes, got ok=%v len=%d", ok, len(sorted))
	}
}

// ===== ADDITIONAL FindPath TESTS =====

func TestFindPath_MaxDepth(t *testing.T) {
	g := NewCodePropertyGraph("test")
	for _, id := range []string{"a", "b", "c", "d"} {
		g.Nodes = g.Nodes.Set(id, &stage4.ResolvedNode{ID: id, Kind: "FUNCTION", Name: id})
	}
	g.OutboundEdges = g.OutboundEdges.Set("a", []stage4.ResolvedEdge{{SourceID: "a", TargetID: "b", Type: stage4.EdgeCalls}})
	g.OutboundEdges = g.OutboundEdges.Set("b", []stage4.ResolvedEdge{{SourceID: "b", TargetID: "c", Type: stage4.EdgeCalls}})
	g.OutboundEdges = g.OutboundEdges.Set("c", []stage4.ResolvedEdge{{SourceID: "c", TargetID: "d", Type: stage4.EdgeCalls}})

	path := g.FindPath("a", "d", 2)
	if path != nil {
		t.Errorf("expected nil for maxDepth=2 path from a to d, got %v", path)
	}

	path = g.FindPath("a", "d", 10)
	if path == nil || len(path) != 4 {
		t.Errorf("expected path [a b c d] with maxDepth=10, got %v", path)
	}
}

func TestFindPath_SameNode(t *testing.T) {
	g := NewCodePropertyGraph("test")
	g.Nodes = g.Nodes.Set("a", &stage4.ResolvedNode{ID: "a", Kind: "FUNCTION", Name: "a"})

	path := g.FindPath("a", "a", 10)
	if path == nil {
		t.Error("expected non-nil path for same node")
	}
}

// ===== ADDITIONAL Orphans TESTS =====

func TestOrphans_None(t *testing.T) {
	g := NewCodePropertyGraph("test")
	g.Nodes = g.Nodes.Set("a", &stage4.ResolvedNode{ID: "a", Kind: "FUNCTION", Name: "a"})
	g.Nodes = g.Nodes.Set("b", &stage4.ResolvedNode{ID: "b", Kind: "FUNCTION", Name: "b"})
	g.OutboundEdges = g.OutboundEdges.Set("a", []stage4.ResolvedEdge{{SourceID: "a", TargetID: "b", Type: stage4.EdgeCalls}})
	g.InboundEdges = g.InboundEdges.Set("b", []stage4.ResolvedEdge{{SourceID: "a", TargetID: "b", Type: stage4.EdgeCalls}})

	g.Entrypoints = []string{"a"}
	orphans := g.GetOrphanNodes()
	if len(orphans) != 0 {
		t.Errorf("expected 0 orphans, got %v", orphans)
	}
}

// ===== ADDITIONAL Instability/Impact/Cohesion TESTS =====

func TestInstability_Balanced(t *testing.T) {
	g := NewCodePropertyGraph("test")
	g.Nodes = g.Nodes.Set("a", &stage4.ResolvedNode{ID: "a", Kind: "FUNCTION", Name: "a"})
	g.InboundEdges = g.InboundEdges.Set("a", []stage4.ResolvedEdge{
		{SourceID: "x", TargetID: "a", Type: stage4.EdgeCalls},
		{SourceID: "y", TargetID: "a", Type: stage4.EdgeCalls},
		{SourceID: "z", TargetID: "a", Type: stage4.EdgeCalls},
	})
	g.OutboundEdges = g.OutboundEdges.Set("a", []stage4.ResolvedEdge{
		{SourceID: "a", TargetID: "p", Type: stage4.EdgeCalls},
		{SourceID: "a", TargetID: "q", Type: stage4.EdgeCalls},
		{SourceID: "a", TargetID: "r", Type: stage4.EdgeCalls},
	})

	inst := g.CalculateInstability("a")
	if inst != 0.5 {
		t.Errorf("expected 0.5 for balanced (3in/3out), got %f", inst)
	}
}

func TestImpactRadius_Self(t *testing.T) {
	g := NewCodePropertyGraph("test")
	g.Nodes = g.Nodes.Set("a", &stage4.ResolvedNode{ID: "a", Kind: "FUNCTION", Name: "a"})
	radius := g.CalculateImpactRadius("a")
	if radius != 0 {
		t.Errorf("expected 0 impact radius for itself, got %d", radius)
	}
}

func TestPackageCohesion_High(t *testing.T) {
	g := NewCodePropertyGraph("test")
	g.Nodes = g.Nodes.Set("pkg", &stage4.ResolvedNode{ID: "pkg", Kind: "PACKAGE", Name: "pkg"})
	components := []string{"a", "b", "c"}
	for _, c := range components {
		g.Nodes = g.Nodes.Set(c, &stage4.ResolvedNode{ID: c, Kind: "CLASS", Name: c})

		edges, _ := g.OutboundEdges.Get(c)
		g.OutboundEdges = g.OutboundEdges.Set(c, append(edges,
			stage4.ResolvedEdge{SourceID: c, TargetID: "pkg", Type: stage4.EdgeBelongsTo}))

		inEdges, _ := g.InboundEdges.Get("pkg")
		g.InboundEdges = g.InboundEdges.Set("pkg", append(inEdges,
			stage4.ResolvedEdge{SourceID: c, TargetID: "pkg", Type: stage4.EdgeBelongsTo}))
	}

	edgesA, _ := g.OutboundEdges.Get("a")
	g.OutboundEdges = g.OutboundEdges.Set("a", append(edgesA,
		stage4.ResolvedEdge{SourceID: "a", TargetID: "b", Type: stage4.EdgeCalls},
		stage4.ResolvedEdge{SourceID: "a", TargetID: "c", Type: stage4.EdgeCalls}))

	edgesB, _ := g.OutboundEdges.Get("b")
	g.OutboundEdges = g.OutboundEdges.Set("b", append(edgesB,
		stage4.ResolvedEdge{SourceID: "b", TargetID: "a", Type: stage4.EdgeCalls},
		stage4.ResolvedEdge{SourceID: "b", TargetID: "c", Type: stage4.EdgeCalls}))

	edgesC, _ := g.OutboundEdges.Get("c")
	g.OutboundEdges = g.OutboundEdges.Set("c", append(edgesC,
		stage4.ResolvedEdge{SourceID: "c", TargetID: "a", Type: stage4.EdgeCalls},
		stage4.ResolvedEdge{SourceID: "c", TargetID: "b", Type: stage4.EdgeCalls}))

	cohesion := g.CalculatePackageCohesion("pkg")
	if cohesion <= 1.0 {
		t.Errorf("expected cohesion > 1.0 for highly connected package, got %f", cohesion)
	}
}

func TestPackageCohesion_Low(t *testing.T) {
	g := NewCodePropertyGraph("test")
	g.Nodes = g.Nodes.Set("pkg", &stage4.ResolvedNode{ID: "pkg", Kind: "PACKAGE", Name: "pkg"})
	components := []string{"a", "b", "c"}
	for _, c := range components {
		g.Nodes = g.Nodes.Set(c, &stage4.ResolvedNode{ID: c, Kind: "CLASS", Name: c})

		edges, _ := g.OutboundEdges.Get(c)
		g.OutboundEdges = g.OutboundEdges.Set(c, append(edges,
			stage4.ResolvedEdge{SourceID: c, TargetID: "pkg", Type: stage4.EdgeBelongsTo}))

		inEdges, _ := g.InboundEdges.Get("pkg")
		g.InboundEdges = g.InboundEdges.Set("pkg", append(inEdges,
			stage4.ResolvedEdge{SourceID: c, TargetID: "pkg", Type: stage4.EdgeBelongsTo}))
	}

	cohesion := g.CalculatePackageCohesion("pkg")
	if cohesion != 0.0 {
		t.Errorf("expected 0.0 cohesion for components with no internal edges, got %f", cohesion)
	}
}

// ===== MVCC CLONE TESTS =====

func TestClone_Nil(t *testing.T) {
	var g *CodePropertyGraph
	clone := g.Clone()
	if clone == nil {
		t.Error("Clone() of nil should return fresh graph")
	}
}

func TestClone_Empty(t *testing.T) {
	g := NewCodePropertyGraph("test")
	clone := g.Clone()
	if clone.Nodes.Len() != 0 {
		t.Errorf("expected 0 nodes in cloned empty graph, got %d", clone.Nodes.Len())
	}
}

func TestClone_DeepCopy(t *testing.T) {
	g := NewCodePropertyGraph("test")
	g.Nodes = g.Nodes.Set("a", &stage4.ResolvedNode{ID: "a", Kind: "FUNCTION", Name: "a"})
	g.Nodes = g.Nodes.Set("b", &stage4.ResolvedNode{ID: "b", Kind: "FUNCTION", Name: "b"})
	clone := g.Clone()
	clone.Nodes = clone.Nodes.Delete("a")
	if _, ok := g.Nodes.Get("a"); !ok {
		t.Error("original should still have node after clone deletion")
	}
}

func TestClone_DeepEdges(t *testing.T) {
	g := NewCodePropertyGraph("test")
	g.Nodes = g.Nodes.Set("a", &stage4.ResolvedNode{ID: "a", Kind: "FUNCTION", Name: "a"})
	g.Nodes = g.Nodes.Set("b", &stage4.ResolvedNode{ID: "b", Kind: "FUNCTION", Name: "b"})
	g.OutboundEdges = g.OutboundEdges.Set("a", []stage4.ResolvedEdge{{SourceID: "a", TargetID: "b", Type: stage4.EdgeCalls}})

	clone := g.Clone()
	edges, _ := clone.OutboundEdges.Get("a")
	clone.OutboundEdges = clone.OutboundEdges.Set("a", append(edges, stage4.ResolvedEdge{SourceID: "a", TargetID: "c", Type: stage4.EdgeCalls}))

	origEdges, _ := g.OutboundEdges.Get("a")
	if len(origEdges) != 1 {
		t.Errorf("original should have 1 edge, got %d", len(origEdges))
	}
}

func TestClone_DeepKindIndex(t *testing.T) {
	g := NewCodePropertyGraph("test")
	g.Nodes = g.Nodes.Set("a", &stage4.ResolvedNode{ID: "a", Kind: "FUNCTION", Name: "a"})
	g.KindIndex = g.KindIndex.Set("FUNCTION", map[string]bool{"a": true})

	clone := g.Clone()
	clone.KindIndex = clone.KindIndex.Set("FUNCTION", map[string]bool{})

	origIdx, _ := g.KindIndex.Get("FUNCTION")
	if !origIdx["a"] {
		t.Error("original KindIndex should be unaffected by clone modification")
	}
}

func TestClone_DeepEntrypoints(t *testing.T) {
	g := NewCodePropertyGraph("test")
	g.Entrypoints = []string{"main"}

	clone := g.Clone()
	clone.Entrypoints = append(clone.Entrypoints, "extra")

	if len(g.Entrypoints) != 1 {
		t.Errorf("original should have 1 entrypoint, got %d", len(g.Entrypoints))
	}
}

func TestClone_DeepErrors(t *testing.T) {
	g := NewCodePropertyGraph("test")
	g.Errors = []DanglingReferenceError{{SourceID: "a", TargetID: "b", Message: "test"}}

	clone := g.Clone()
	clone.Errors = append(clone.Errors, DanglingReferenceError{SourceID: "c", TargetID: "d", Message: "extra"})

	if len(g.Errors) != 1 {
		t.Errorf("original should have 1 error, got %d", len(g.Errors))
	}
}

func TestMVCC_AllocateShadow(t *testing.T) {
	mc := NewMVCCGraphContainer()
	shadow, txID := mc.AllocateShadowSnapshot()
	if shadow == nil {
		t.Error("expected non-nil shadow")
	}
	if txID == 0 {
		t.Error("expected non-zero txID")
	}
}

func TestMVCC_Promote(t *testing.T) {
	mc := NewMVCCGraphContainer()
	shadow, _ := mc.AllocateShadowSnapshot()
	shadow.Nodes = shadow.Nodes.Set("n1", &stage4.ResolvedNode{ID: "n1", Kind: "FUNCTION", Name: "fn"})
	mc.PromoteShadowSnapshot(shadow)

	snapshot := mc.GetSnapshot()
	if _, ok := snapshot.Nodes.Get("n1"); !ok {
		t.Error("promoted node should be visible in snapshot")
	}
}

func TestMVCC_Isolation(t *testing.T) {
	mc := NewMVCCGraphContainer()

	snap1 := mc.GetSnapshot()

	shadow, _ := mc.AllocateShadowSnapshot()
	shadow.Nodes = shadow.Nodes.Set("n1", &stage4.ResolvedNode{ID: "n1", Kind: "FUNCTION", Name: "fn"})

	if _, ok := snap1.Nodes.Get("n1"); ok {
		t.Error("snapshot should not see uncommitted shadow changes")
	}

	mc.PromoteShadowSnapshot(shadow)
	snap2 := mc.GetSnapshot()
	if _, ok := snap2.Nodes.Get("n1"); !ok {
		t.Error("promoted node should be visible")
	}
}

// ==================== PageRank Convergence Tests ====================

func TestPageRank_Convergence(t *testing.T) {
	g := NewCodePropertyGraph("test")
	g.Nodes = g.Nodes.Set("A", &stage4.ResolvedNode{ID: "A", Kind: "FUNCTION", Name: "A"})
	g.Nodes = g.Nodes.Set("B", &stage4.ResolvedNode{ID: "B", Kind: "FUNCTION", Name: "B"})
	g.OutboundEdges = g.OutboundEdges.Set("A", []stage4.ResolvedEdge{{SourceID: "A", TargetID: "B", Type: stage4.EdgeCalls}})
	g.OutboundEdges = g.OutboundEdges.Set("B", []stage4.ResolvedEdge{{SourceID: "B", TargetID: "A", Type: stage4.EdgeCalls}})
	g.InboundEdges = g.InboundEdges.Set("A", []stage4.ResolvedEdge{{SourceID: "B", TargetID: "A", Type: stage4.EdgeCalls}})
	g.InboundEdges = g.InboundEdges.Set("B", []stage4.ResolvedEdge{{SourceID: "A", TargetID: "B", Type: stage4.EdgeCalls}})

	ranks := g.CalculatePageRank(20, 0.85)
	assert.InDelta(t, 0.5, ranks["A"], 0.01)
	assert.InDelta(t, 0.5, ranks["B"], 0.01)
}

// ==================== BetweennessCentrality Star Graph Tests ====================

func TestBetweenness_StarGraph(t *testing.T) {
	g := NewCodePropertyGraph("test")
	g.Nodes = g.Nodes.Set("center", &stage4.ResolvedNode{ID: "center", Kind: "STRUCT", Name: "center"})
	for _, leaf := range []string{"leaf1", "leaf2", "leaf3"} {
		g.Nodes = g.Nodes.Set(leaf, &stage4.ResolvedNode{ID: leaf, Kind: "STRUCT", Name: leaf})
		g.OutboundEdges = g.OutboundEdges.Set(leaf, []stage4.ResolvedEdge{{SourceID: leaf, TargetID: "center", Type: stage4.EdgeCalls}})
		g.OutboundEdges = g.OutboundEdges.Set("center", append(g.GetOutboundEdges("center"),
			stage4.ResolvedEdge{SourceID: "center", TargetID: leaf, Type: stage4.EdgeCalls}))
		g.InboundEdges = g.InboundEdges.Set("center", append(g.GetInboundEdges("center"),
			stage4.ResolvedEdge{SourceID: leaf, TargetID: "center", Type: stage4.EdgeCalls}))
		g.InboundEdges = g.InboundEdges.Set(leaf, []stage4.ResolvedEdge{{SourceID: "center", TargetID: leaf, Type: stage4.EdgeCalls}})
	}

	bc := g.CalculateBetweennessCentrality(false)
	assert.Positive(t, bc["center"])
	assert.Equal(t, 0.0, bc["leaf1"])
	assert.Equal(t, 0.0, bc["leaf2"])
	assert.Equal(t, 0.0, bc["leaf3"])
}

// ==================== BetweennessCentrality Disconnected Tests ====================

func TestBetweenness_Disconnected(t *testing.T) {
	g := NewCodePropertyGraph("test")
	for _, id := range []string{"A", "B", "C", "D"} {
		g.Nodes = g.Nodes.Set(id, &stage4.ResolvedNode{ID: id, Kind: "STRUCT", Name: id})
	}
	g.OutboundEdges = g.OutboundEdges.Set("A", []stage4.ResolvedEdge{{SourceID: "A", TargetID: "B", Type: stage4.EdgeCalls}})
	g.OutboundEdges = g.OutboundEdges.Set("C", []stage4.ResolvedEdge{{SourceID: "C", TargetID: "D", Type: stage4.EdgeCalls}})

	bc := g.CalculateBetweennessCentrality(false)
	assert.Equal(t, 0.0, bc["A"])
	assert.Equal(t, 0.0, bc["B"])
	assert.Equal(t, 0.0, bc["C"])
	assert.Equal(t, 0.0, bc["D"])
}

// ==================== GodObjects Boundaries Tests ====================

func TestGodObjects_Boundaries(t *testing.T) {
	g := NewCodePropertyGraph("test")
	for i := 0; i < 6; i++ {
		id := fmt.Sprintf("n%d", i)
		g.Nodes = g.Nodes.Set(id, &stage4.ResolvedNode{ID: id, Kind: "STRUCT", Name: id})
	}
	g.Nodes = g.Nodes.Set("mod", &stage4.ResolvedNode{ID: "mod", Kind: "STRUCT", Name: "mod"})
	g.Nodes = g.Nodes.Set("ext", &stage4.ResolvedNode{ID: "ext", Kind: "STRUCT", Name: "ext"})

	for i := 0; i < 3; i++ {
		src := fmt.Sprintf("mod_src_%d", i)
		dst := fmt.Sprintf("mod_dst_%d", i)
		g.Nodes = g.Nodes.Set(src, &stage4.ResolvedNode{ID: src, Kind: "FUNCTION", Name: src})
		g.Nodes = g.Nodes.Set(dst, &stage4.ResolvedNode{ID: dst, Kind: "FUNCTION", Name: dst})

		g.OutboundEdges = g.OutboundEdges.Set(src, []stage4.ResolvedEdge{{SourceID: src, TargetID: "mod", Type: stage4.EdgeCalls}})
		g.InboundEdges = g.InboundEdges.Set("mod", append(g.GetInboundEdges("mod"),
			stage4.ResolvedEdge{SourceID: src, TargetID: "mod", Type: stage4.EdgeCalls}))
		g.OutboundEdges = g.OutboundEdges.Set("mod", append(g.GetOutboundEdges("mod"),
			stage4.ResolvedEdge{SourceID: "mod", TargetID: dst, Type: stage4.EdgeCalls}))
		g.InboundEdges = g.InboundEdges.Set(dst, []stage4.ResolvedEdge{{SourceID: "mod", TargetID: dst, Type: stage4.EdgeCalls}})
	}

	for i := 0; i < 15; i++ {
		src := fmt.Sprintf("ext_src_%d", i)
		dst := fmt.Sprintf("ext_dst_%d", i)
		g.Nodes = g.Nodes.Set(src, &stage4.ResolvedNode{ID: src, Kind: "FUNCTION", Name: src})
		g.Nodes = g.Nodes.Set(dst, &stage4.ResolvedNode{ID: dst, Kind: "FUNCTION", Name: dst})

		g.OutboundEdges = g.OutboundEdges.Set(src, []stage4.ResolvedEdge{{SourceID: src, TargetID: "ext", Type: stage4.EdgeCalls}})
		g.InboundEdges = g.InboundEdges.Set("ext", append(g.GetInboundEdges("ext"),
			stage4.ResolvedEdge{SourceID: src, TargetID: "ext", Type: stage4.EdgeCalls}))
		g.OutboundEdges = g.OutboundEdges.Set("ext", append(g.GetOutboundEdges("ext"),
			stage4.ResolvedEdge{SourceID: "ext", TargetID: dst, Type: stage4.EdgeCalls}))
		g.InboundEdges = g.InboundEdges.Set(dst, []stage4.ResolvedEdge{{SourceID: "ext", TargetID: dst, Type: stage4.EdgeCalls}})
	}

	godObjects := g.DetectGodObjects()
	assert.Contains(t, godObjects, "ext")
	assert.NotContains(t, godObjects, "mod")
}

// ==================== Islands Singleton Tests ====================

func TestIslands_Singleton(t *testing.T) {
	g := NewCodePropertyGraph("test")
	g.Nodes = g.Nodes.Set("alone", &stage4.ResolvedNode{ID: "alone", Kind: "FUNCTION", Name: "alone"})

	islands := g.FindIsolatedIslands()
	assert.Empty(t, islands)
}

// ==================== FindPath MissingNodes Tests ====================

func TestFindPath_MissingNodes(t *testing.T) {
	g := NewCodePropertyGraph("test")
	g.Nodes = g.Nodes.Set("A", &stage4.ResolvedNode{ID: "A", Kind: "FUNCTION", Name: "A"})
	g.Nodes = g.Nodes.Set("B", &stage4.ResolvedNode{ID: "B", Kind: "FUNCTION", Name: "B"})
	g.OutboundEdges = g.OutboundEdges.Set("A", []stage4.ResolvedEdge{{SourceID: "A", TargetID: "B", Type: stage4.EdgeCalls}})

	assert.Nil(t, g.FindPath("nonexistent", "B", 10))
	assert.Nil(t, g.FindPath("A", "nonexistent", 10))
}

// ==================== Clone DeepHashIndex Tests ====================

func TestClone_DeepHashIndex(t *testing.T) {
	g := NewCodePropertyGraph("test")
	g.Nodes = g.Nodes.Set("a", &stage4.ResolvedNode{ID: "a", Kind: "FUNCTION", Name: "a", Properties: map[string]string{"hash": "abc123"}})
	g.HashIndex = g.HashIndex.Set("abc123", []string{"a"})

	clone := g.Clone()
	clone.HashIndex = clone.HashIndex.Delete("abc123")

	origVal, origOK := g.HashIndex.Get("abc123")
	assert.True(t, origOK)
	assert.Equal(t, []string{"a"}, origVal)
}

// ==================== Clone DeepFileNodeIndex Tests ====================

func TestClone_DeepFileNodeIndex(t *testing.T) {
	g := NewCodePropertyGraph("test")
	g.Nodes = g.Nodes.Set("a", &stage4.ResolvedNode{ID: "a", Kind: "FUNCTION", Name: "a"})
	g.FileNodeIndex = g.FileNodeIndex.Set("file.go", map[string]bool{"a": true})

	clone := g.Clone()
	clone.FileNodeIndex = clone.FileNodeIndex.Delete("file.go")

	_, origOK := g.FileNodeIndex.Get("file.go")
	assert.True(t, origOK)
}

// ==================== Clone LargeGraph_Parallel Tests ====================

func TestClone_LargeGraph_Parallel(t *testing.T) {
	g := NewCodePropertyGraph("test")
	for i := 0; i < 100; i++ {
		id := fmt.Sprintf("n%d", i)
		g.Nodes = g.Nodes.Set(id, &stage4.ResolvedNode{ID: id, Kind: "FUNCTION", Name: id})
	}
	assert.Equal(t, 100, g.Nodes.Len())

	clone := g.Clone()
	assert.Equal(t, 100, clone.Nodes.Len())
}

// ==================== MVCC ConcurrentAccess Tests ====================

func TestMVCC_ConcurrentAccess(t *testing.T) {
	mc := NewMVCCGraphContainer()

	var wg sync.WaitGroup

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			snap := mc.GetSnapshot()
			_ = snap.Nodes.Len()
		}()
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		shadow, _ := mc.AllocateShadowSnapshot()
		shadow.Nodes = shadow.Nodes.Set("n1", &stage4.ResolvedNode{ID: "n1", Kind: "FUNCTION", Name: "n1"})
		mc.PromoteShadowSnapshot(shadow)
	}()

	wg.Wait()
}

func TestCowMap_DeleteWithRebalance(t *testing.T) {
	cm := NewCowMap[string, int]()
	for _, k := range []string{"d", "b", "f", "a", "c", "e", "g"} {
		cm = cm.Set(k, 1)
	}
	assert.Equal(t, 7, cm.Len())

	cm = cm.Delete("a")
	assert.Equal(t, 6, cm.Len())
	_, ok := cm.Get("a")
	assert.False(t, ok)

	cm = cm.Delete("d")
	assert.Equal(t, 5, cm.Len())

	cm = cm.Delete("f")
	assert.Equal(t, 4, cm.Len())

	cm = cm.Delete("b")
	cm = cm.Delete("c")
	cm = cm.Delete("e")
	cm = cm.Delete("g")
	assert.Equal(t, 0, cm.Len())
}
