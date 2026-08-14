package normalize

import (
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/types"
)

func TestBuildLayoutTree(t *testing.T) {
	sub := &types.VirtualSubgraph{
		Nodes: map[string]*types.TTLNode{
			"a.go::A": {ID: "a.go::A", Name: "A", Kind: "gm:Executable"},
			"b.go::B": {ID: "b.go::B", Name: "B", Kind: "gm:Executable"},
		},
		Edges: []types.TTLEdge{
			{SourceID: "a.go::A", TargetID: "b.go::B", Predicate: "gm:calls"},
		},
	}
	opts := types.QueryOptions{Scope: types.ScopeGlobal}
	tree := BuildLayoutTree(sub, opts)
	if tree == nil {
		t.Fatal("expected non-nil LayoutTree")
	}
	if tree.BoundaryName != "Root" {
		t.Errorf("expected Root boundary, got %s", tree.BoundaryName)
	}
	if tree.Summary == nil {
		t.Fatal("expected non-nil Summary")
	}
	if tree.Summary.NodeCount != 2 {
		t.Errorf("expected NodeCount 2, got %d", tree.Summary.NodeCount)
	}
}

func TestBuildLayoutTreeEmptySubgraph(t *testing.T) {
	sub := &types.VirtualSubgraph{
		Nodes: map[string]*types.TTLNode{},
		Edges: nil,
	}
	opts := types.QueryOptions{Scope: types.ScopeGlobal}
	tree := BuildLayoutTree(sub, opts)
	if tree == nil {
		t.Fatal("expected non-nil LayoutTree for empty subgraph")
	}
	if tree.BoundaryName != "Root" {
		t.Errorf("expected Root boundary, got %s", tree.BoundaryName)
	}
}

func TestBuildLayoutTreeWithEntryPoint(t *testing.T) {
	sub := &types.VirtualSubgraph{
		Nodes: map[string]*types.TTLNode{
			"a.go::A": {ID: "a.go::A", Name: "A", Kind: "gm:Executable", IsEntrypoint: true},
			"b.go::B": {ID: "b.go::B", Name: "B", Kind: "gm:Executable"},
		},
		Edges: []types.TTLEdge{
			{SourceID: "a.go::A", TargetID: "b.go::B", Predicate: "gm:calls"},
		},
	}
	opts := types.QueryOptions{Scope: types.ScopeGlobal}
	tree := BuildLayoutTree(sub, opts)
	foundEntryPoint := false
	for _, node := range tree.Nodes {
		if node.IsEntrypoint {
			foundEntryPoint = true
			break
		}
	}
	if !foundEntryPoint {
		t.Error("expected at least one entrypoint marked node")
	}
}

func TestPruneDeadComponents(t *testing.T) {
	sub := &types.VirtualSubgraph{
		Nodes: map[string]*types.TTLNode{
			"a.go::A": {ID: "a.go::A", Name: "A", Kind: "gm:Executable"},
			"b.go::B": {ID: "b.go::B", Name: "B", Kind: "gm:Executable"},
			"c.go::C": {ID: "c.go::C", Name: "C", Kind: "gm:Executable"},
		},
		Edges: []types.TTLEdge{
			{SourceID: "a.go::A", TargetID: "b.go::B", Predicate: "gm:calls"},
		},
	}
	opts := types.QueryOptions{Scope: types.ScopeGlobal, IncludeUnused: false}
	filtered := pruneDeadComponents(sub, opts)
	if len(filtered) != 2 {
		t.Errorf("expected 2 nodes after pruning (A, B have edges), got %d", len(filtered))
	}
	if _, ok := filtered["c.go::C"]; ok {
		t.Error("node C should have been pruned (no edges)")
	}
}

func TestPruneDeadComponentsIncludeUnused(t *testing.T) {
	sub := &types.VirtualSubgraph{
		Nodes: map[string]*types.TTLNode{
			"a.go::A": {ID: "a.go::A", Name: "A", Kind: "gm:Executable"},
			"b.go::B": {ID: "b.go::B", Name: "B", Kind: "gm:Executable"},
		},
		Edges: nil,
	}
	opts := types.QueryOptions{Scope: types.ScopeGlobal, IncludeUnused: true}
	filtered := pruneDeadComponents(sub, opts)
	if len(filtered) != 2 {
		t.Errorf("expected 2 nodes with IncludeUnused=true, got %d", len(filtered))
	}
}

func TestCollapseEdges(t *testing.T) {
	edges := []types.TTLEdge{
		{SourceID: "a", TargetID: "b", Predicate: "gm:calls"},
		{SourceID: "a", TargetID: "b", Predicate: "gm:calls"},
		{SourceID: "b", TargetID: "c", Predicate: "gm:calls"},
	}
	collapsed := collapseEdges(edges)
	if len(collapsed) != 2 {
		t.Errorf("expected 2 collapsed edges (one duplicated), got %d", len(collapsed))
	}
	for _, e := range collapsed {
		if e.SourceID == "a" && e.TargetID == "b" && e.Weight != 2 {
			t.Errorf("expected weight 2 for a->b edge, got %d", e.Weight)
		}
	}
}

func TestCollapseEdgesEmpty(t *testing.T) {
	collapsed := collapseEdges(nil)
	if len(collapsed) != 0 {
		t.Errorf("expected 0 collapsed edges, got %d", len(collapsed))
	}
}

func TestSortLayoutTree(t *testing.T) {
	tree := &types.LayoutTree{
		BoundaryName: "Root",
		Nodes: []*types.LayoutNode{
			{Name: "Z", LineStart: 10},
			{Name: "A", LineStart: 1},
		},
	}
	sortLayoutTree(tree)
	if tree.Nodes[0].Name != "A" {
		t.Errorf("expected sorted order, first node name should be A, got %s", tree.Nodes[0].Name)
	}
}

func TestDetectAndMarkCycles(t *testing.T) {
	nodes := map[string]*types.TTLNode{
		"a": {ID: "a"},
		"b": {ID: "b"},
	}
	edges := []types.LayoutEdge{
		{SourceID: "a", TargetID: "b", Predicate: "gm:calls"},
		{SourceID: "b", TargetID: "a", Predicate: "gm:calls"},
	}
	result := detectAndMarkCycles(nodes, edges)
	hasCycle := false
	for _, e := range result {
		if e.IsCycle {
			hasCycle = true
			break
		}
	}
	if !hasCycle {
		t.Error("expected cycle detection for a<->b cycle")
	}
}

func TestGetOrCreateBoundary(t *testing.T) {
	root := &types.LayoutTree{BoundaryName: "Root"}
	treeMap := make(map[string]*types.LayoutTree)
	treeMap[""] = root

	b1 := getOrCreateBoundary("a/b", treeMap, root)
	if b1 == nil {
		t.Fatal("expected non-nil boundary")
	}
	if b1.BoundaryName != "b" {
		t.Errorf("expected boundary name 'b', got '%s'", b1.BoundaryName)
	}

	b2 := getOrCreateBoundary("a/b", treeMap, root)
	if b1 != b2 {
		t.Error("expected same boundary when fetching existing path")
	}
}

// TestSortNodesEmpty verifies that sortNodes on an empty slice does not panic.
func TestSortNodesEmpty(t *testing.T) {
	var nodes []*types.LayoutNode
	sortNodes(nodes) // must not panic
	if len(nodes) != 0 {
		t.Errorf("expected 0 nodes, got %d", len(nodes))
	}
}

// TestSortNodesByLineStart verifies that sortNodes orders nodes by ascending LineStart.
func TestSortNodesByLineStart(t *testing.T) {
	nodes := []*types.LayoutNode{
		{Name: "C", LineStart: 30},
		{Name: "A", LineStart: 5},
		{Name: "B", LineStart: 15},
	}
	sortNodes(nodes)
	expected := []int{5, 15, 30}
	for i, want := range expected {
		if nodes[i].LineStart != want {
			t.Errorf("position %d: expected LineStart %d, got %d", i, want, nodes[i].LineStart)
		}
	}
}

// TestSortNodesSinkLast verifies that DATABASE PrimitiveType nodes are sorted after non-sink nodes.
func TestSortNodesSinkLast(t *testing.T) {
	nodes := []*types.LayoutNode{
		{Name: "DBNode", LineStart: 1, PrimitiveType: "DATABASE"},
		{Name: "Regular", LineStart: 10, PrimitiveType: ""},
		{Name: "Disk", LineStart: 2, PrimitiveType: "DISK_IO"},
	}
	sortNodes(nodes)
	// First node must not be a sink
	if nodes[0].PrimitiveType == "DATABASE" || nodes[0].PrimitiveType == "DISK_IO" || nodes[0].PrimitiveType == "NETWORK_IO" {
		t.Errorf("expected non-sink node first, got PrimitiveType=%q", nodes[0].PrimitiveType)
	}
	// Last two nodes must both be sinks
	for i := 1; i <= 2; i++ {
		pt := nodes[i].PrimitiveType
		if pt != "DATABASE" && pt != "DISK_IO" && pt != "NETWORK_IO" {
			t.Errorf("position %d: expected sink PrimitiveType, got %q", i, pt)
		}
	}
}

// TestBuildLayoutTreeExEmpty verifies that passing nil subgraph returns a valid non-nil LayoutTree.
func TestBuildLayoutTreeExEmpty(t *testing.T) {
	tree := BuildLayoutTreeEx(nil, nil, nil, types.QueryOptions{Scope: types.ScopeGlobal}, types.UMLClass)
	if tree == nil {
		t.Fatal("expected non-nil LayoutTree for nil subgraph")
	}
	if tree.BoundaryName != "Root" {
		t.Errorf("expected BoundaryName 'Root', got %q", tree.BoundaryName)
	}
	if tree.Summary == nil {
		t.Error("expected non-nil Summary for nil subgraph")
	}
}

// TestGetDirectoryPath verifies directory path derivation for various node kinds and diagram types.
func TestGetDirectoryPath(t *testing.T) {
	communities := map[string]string{}

	tests := []struct {
		name      string
		nodeID    string
		kind      string
		diagType  types.DiagramType
		wantEmpty bool
		wantSub   string
	}{
		{
			name:      "simple file node returns directory path",
			nodeID:    "internal/foo/bar.go::MyFunc",
			kind:      "gm:Executable",
			diagType:  types.UMLPackage,
			wantEmpty: false,
			wantSub:   "internal/foo",
		},
		{
			name:      "root-level file returns empty dir",
			nodeID:    "main.go::main",
			kind:      "gm:Executable",
			diagType:  types.UMLPackage,
			wantEmpty: true,
		},
		{
			name:      "namespace kind uses full path",
			nodeID:    "internal/pkg::MyPkg",
			kind:      "gm:Namespace",
			diagType:  types.DependencyGraph,
			wantEmpty: false,
			wantSub:   "internal/pkg",
		},
		{
			name:      "layered architecture internal path returns layer",
			nodeID:    "internal/visualization_engine/foo.go::Func",
			kind:      "gm:Executable",
			diagType:  types.LayeredArchitecture,
			wantEmpty: false,
			wantSub:   "Layer",
		},
		{
			name:      "layered architecture cmd path returns CLI Layer",
			nodeID:    "cmd/main.go::Run",
			kind:      "gm:Executable",
			diagType:  types.LayeredArchitecture,
			wantEmpty: false,
			wantSub:   "CLI Layer",
		},
		{
			name:      "UML component internal path returns Subsystem",
			nodeID:    "internal/engine/foo.go::Func",
			kind:      "gm:Executable",
			diagType:  types.UMLComponent,
			wantEmpty: false,
			wantSub:   "Subsystem",
		},
		{
			name:      "file: prefix is stripped",
			nodeID:    "file:internal/foo/bar.go::MyFunc",
			kind:      "gm:Executable",
			diagType:  types.UMLPackage,
			wantEmpty: false,
			wantSub:   "internal/foo",
		},
		{
			name:     "module: prefix is stripped",
			nodeID:   "module:github.com/foo/bar::Pkg",
			kind:     "gm:Executable",
			diagType: types.UMLPackage,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := getDirectoryPath(tc.nodeID, tc.kind, tc.diagType, communities)
			if tc.wantEmpty {
				if got != "" {
					t.Errorf("expected empty string, got %q", got)
				}
				return
			}
			if tc.wantSub == "" {
				return // no assertion needed for this case
			}
			if got == "" {
				t.Errorf("expected non-empty directory path, got empty string")
				return
			}
			if !aggContainsStr(got, tc.wantSub) {
				t.Errorf("expected result %q to contain %q", got, tc.wantSub)
			}
		})
	}
}

// aggContainsStr returns true if s contains the substring sub.
func aggContainsStr(s, sub string) bool {
	if len(sub) == 0 {
		return true
	}
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
