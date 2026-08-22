package link

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTransitiveExtendsClosure (W1-12, §5.4.1/A-02): A→B→C→D emits
// transitive EXTENDS edges at depth ≥ 2, capped at maxInheritanceDepth.
func TestTransitiveExtendsClosure(t *testing.T) {
	cpg := NewLinkOutput("HEAD")
	for _, id := range []string{"A", "B", "C", "D"} {
		cpg.GraphNodes[id] = &ResolvedNode{ID: id, Kind: "CLASS", Name: id}
	}
	cpg.AddEdge("A", "B", EdgeExtends, 1)
	cpg.AddEdge("B", "C", EdgeExtends, 2)
	cpg.AddEdge("C", "D", EdgeExtends, 3)

	emitTransitiveHierarchy(cpg)

	// A → B (direct), A → C, A → D (transitive).
	targets := map[string]bool{}
	for _, e := range cpg.OutboundEdges["A"] {
		if e.Type == EdgeExtends {
			targets[e.TargetID] = true
		}
	}
	assert.True(t, targets["B"], "direct parent")
	assert.True(t, targets["C"], "transitive grandparent")
	assert.True(t, targets["D"], "transitive depth 3")
}

// TestTransitiveExtendsDepthCap (W1-12): chains deeper than
// maxInheritanceDepth produce no edges beyond the cap.
func TestTransitiveExtendsDepthCap(t *testing.T) {
	cpg := NewLinkOutput("HEAD")
	ids := []string{"N0", "N1", "N2", "N3", "N4", "N5", "N6", "N7"}
	for _, id := range ids {
		cpg.GraphNodes[id] = &ResolvedNode{ID: id, Kind: "CLASS", Name: id}
	}
	for i := 0; i+1 < len(ids); i++ {
		cpg.AddEdge(ids[i], ids[i+1], EdgeExtends, i)
	}

	emitTransitiveHierarchy(cpg)

	// N0's ancestors: N1..N7 (depth 1..7). Emitted: depth 2..5 = N2..N5.
	count := 0
	for _, e := range cpg.OutboundEdges["N0"] {
		if e.Type == EdgeExtends {
			count++
		}
	}
	require.Equal(t, 1+4, count, "N0: direct (N1) + depth-2..5 (N2,N3,N4,N5), cap at 5")
}

// TestTransitiveImplementsClosure (W1-12): IMPLEMENTS chains close under
// the same predicate (gm:inheritsFrom).
func TestTransitiveImplementsClosure(t *testing.T) {
	cpg := NewLinkOutput("HEAD")
	cpg.GraphNodes["X"] = &ResolvedNode{ID: "X", Kind: "STRUCT", Name: "X"}
	cpg.GraphNodes["Y"] = &ResolvedNode{ID: "Y", Kind: "INTERFACE", Name: "Y"}
	cpg.GraphNodes["Z"] = &ResolvedNode{ID: "Z", Kind: "INTERFACE", Name: "Z"}
	cpg.AddEdge("X", "Y", EdgeImplements, 1)
	cpg.AddEdge("Y", "Z", EdgeImplements, 2)

	emitTransitiveHierarchy(cpg)

	targets := map[string]bool{}
	for _, e := range cpg.OutboundEdges["X"] {
		if e.Type == EdgeImplements {
			targets[e.TargetID] = true
		}
	}
	assert.True(t, targets["Y"], "direct interface")
	assert.True(t, targets["Z"], "transitive interface (inheritsFrom)")
}

// TestResolveTypeToFQNPredeclared (GAP-TYP-03): predeclared Go types never
// resolve to user-defined nodes, even when a node shares the primitive name.
func TestResolveTypeToFQNPredeclared(t *testing.T) {
	cpg := NewLinkOutput("HEAD")
	cpg.GraphNodes["svc.go::string"] = &ResolvedNode{ID: "svc.go::string", Kind: "STRUCT", Name: "string"}
	cpg.GraphNodes["svc.go::Error"] = &ResolvedNode{ID: "svc.go::Error", Kind: "STRUCT", Name: "Error"}

	for _, raw := range []string{"string", "*string", "[]byte", "int64", "error", "any", "bool", "uintptr"} {
		assert.Equal(t, "", resolveTypeToFQN(raw, "svc.go", nil, cpg), "predeclared %q must not resolve", raw)
	}

	// Non-predeclared names still resolve normally.
	got := resolveTypeToFQN("Error", "svc.go", nil, cpg)
	assert.Equal(t, "svc.go::Error", got, "user-defined type still resolves")
}

// TestNameToNodeIDIndex (W1-12 / A-15): the type-name index resolves
// exactly and the delta map shadows the shared base on collisions.
func TestNameToNodeIDIndex(t *testing.T) {
	cpg := NewLinkOutput("HEAD")
	cpg.GraphNodes["a.go::User"] = &ResolvedNode{ID: "a.go::User", Kind: "STRUCT", Name: "User"}
	cpg.GraphNodes["b.go::User"] = &ResolvedNode{ID: "b.go::User", Kind: "STRUCT", Name: "User"}
	cpg.GraphNodes["ext:thing"] = &ResolvedNode{ID: "ext:thing", Kind: "EXTERNAL_SDK", Name: "User"}

	idx := cpg.nameToNodeID()
	// First-writer-wins within the delta map (iteration order is
	// nondeterministic, but the result must be one of the two type nodes).
	assert.Contains(t, []string{"a.go::User", "b.go::User"}, idx["User"])
	assert.Len(t, idx, 1)
	assert.Equal(t, idx, cpg.nameToNodeID(), "index must be cached, not rebuilt")

	// Delta shadows base on the same name.
	base := NewLinkOutput("HEAD")
	base.GraphNodes["old.go::Widget"] = &ResolvedNode{ID: "old.go::Widget", Kind: "CLASS", Name: "Widget"}
	delta := &LinkOutput{
		CommitHash:    "HEAD",
		GraphNodes:    map[string]*ResolvedNode{},
		baseNodes:     base.GraphNodes,
		OutboundEdges: map[string][]ResolvedEdge{},
		InboundEdges:  map[string][]ResolvedEdge{},
	}
	delta.GraphNodes["new.go::Widget"] = &ResolvedNode{ID: "new.go::Widget", Kind: "CLASS", Name: "Widget"}
	assert.Equal(t, "new.go::Widget", delta.nameToNodeID()["Widget"])
}
