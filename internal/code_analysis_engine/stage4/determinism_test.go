package stage4

import (
	"fmt"
	"sort"
	"strings"
	"testing"
)

// TestSortDeterministicEdgeOrder verifies the W1-16 sort invariant: every
// emitted edge list is ordered by (Type, TargetID, LineNumber, ...).
func TestSortDeterministicEdgeOrder(t *testing.T) {
	cpg := NewStage4Output("")
	// Deliberately scrambled insertion order.
	cpg.AddEdge("b", "x", EdgeCalls, 5)
	cpg.AddEdge("a", "y", EdgeCalls, 1)
	cpg.AddEdge("a", "x", EdgeExtends, 2)
	cpg.AddEdge("a", "x", EdgeCalls, 2)
	cpg.AddEdgeProperties("a", "x", EdgeExtends, 2, 1.0,
		map[string]string{"gm:provenance": "ast"})
	cpg.AddEdge("a", "x", EdgeExtends, 1)

	SortDeterministic(cpg)

	out := cpg.OutboundEdges["a"]
	if len(out) != 5 {
		t.Fatalf("outbound edges = %d, want 5", len(out))
	}
	for i := 1; i < len(out); i++ {
		if !edgeOrderedOutbound(out[i-1], out[i]) {
			t.Fatalf("outbound list not sorted at %d: %+v !<= %+v", i, out[i-1], out[i])
		}
	}
	// Spot-check exact canonical order: Type primary, then TargetID.
	wantOrder := []string{"CALLS/2", "CALLS/1", "EXTENDS/1", "EXTENDS/2", "EXTENDS/2"}
	gotOrder := make([]string, 0, len(out))
	for _, e := range out {
		gotOrder = append(gotOrder, fmt.Sprintf("%s/%d", e.Type, e.LineNumber))
	}
	for i, want := range wantOrder {
		if gotOrder[i] != want {
			t.Errorf("position %d = %s, want %s (full: %v)", i, gotOrder[i], want, gotOrder)
		}
	}

	for i := 1; i < len(cpg.InboundEdges["x"]); i++ {
		prev, cur := cpg.InboundEdges["x"][i-1], cpg.InboundEdges["x"][i]
		if cur.Type < prev.Type || (cur.Type == prev.Type && cur.SourceID < prev.SourceID) {
			t.Fatalf("inbound list not sorted: %+v !<= %+v", prev, cur)
		}
	}
}

// edgeOrderedOutbound mirrors the sort key: (Type, TargetID, LineNumber,
// SourceID, properties) — enough for the test's assertions.
func edgeOrderedOutbound(a, b ResolvedEdge) bool {
	if a.Type != b.Type {
		return a.Type < b.Type
	}
	if a.TargetID != b.TargetID {
		return a.TargetID < b.TargetID
	}
	return a.LineNumber <= b.LineNumber
}

// TestLinkOutputStableAcrossRuns is the V-05 determinism gate: three Link()
// runs on identical input produce byte-identical edge streams (the W1-16
// sort removes map-iteration and pass-scheduling jitter).
func TestLinkOutputStableAcrossRuns(t *testing.T) {
	stage3Out := buildRichTestStage3Out()
	modified := []string{"service.go", "other.go"}

	var canonical string
	for run := 0; run < 3; run++ {
		out, err := Link(stage3Out, modified, nil, LinkerConfig{LevelOfDetail: LevelArchitecture})
		if err != nil {
			t.Fatalf("run %d: Link() error: %v", run, err)
		}
		var sb strings.Builder
		for _, src := range sortedKeys(out.OutboundEdges) {
			for _, e := range out.OutboundEdges[src] {
				sb.WriteString(src)
				sb.WriteString("|")
				sb.WriteString(string(e.Type))
				sb.WriteString("|")
				sb.WriteString(e.TargetID)
				sb.WriteString("|")
				sb.WriteString(fmt.Sprintf("%d", e.LineNumber))
				sb.WriteString("\n")
			}
		}
		if run == 0 {
			canonical = sb.String()
		} else if sb.String() != canonical {
			t.Fatalf("run %d output differs from run 0 (non-deterministic edge stream)", run)
		}
	}
}

// TestCleanupDeterministicRewrite verifies rewriteNodeIDs produces the same
// adjacency order regardless of map iteration (W1-16 hardening of W1-14).
func TestCleanupDeterministicRewrite(t *testing.T) {
	cpg := NewStage4Output("")
	mangled := []string{
		`ext:akgerrs "github.com/Syamchand123/GlassMarble/internal/errors"`,
		`ext:queue "github.com/Syamchand123/GlassMarble/internal/queue"`,
	}
	for i, id := range mangled {
		cpg.GraphNodes[id] = &ResolvedNode{ID: id, Kind: "EXTERNAL_SDK", Name: id}
		cpg.AddEdge("file:bridge.go", id, EdgeDependsOn, i+1)
		cpg.AddEdge(id, "file:bridge.go", EdgeReferences, i+2)
	}

	var canonical string
	for run := 0; run < 3; run++ {
		cp := NewStage4Output("")
		for id, n := range cpg.GraphNodes {
			nn := *n
			cp.GraphNodes[id] = &nn
		}
		cp.OutboundEdges = cloneEdges(cpg.OutboundEdges)
		cp.InboundEdges = cloneEdges(cpg.InboundEdges)
		CleanupCPG(nil, cp)

		var sb strings.Builder
		for _, src := range sortedKeys(cp.OutboundEdges) {
			for _, e := range cp.OutboundEdges[src] {
				sb.WriteString(src + "|" + string(e.Type) + "|" + e.TargetID + "\n")
			}
		}
		if run == 0 {
			canonical = sb.String()
			continue
		}
		if got := sb.String(); got != canonical {
			t.Fatalf("run %d cleanup rewrite differs from run 0", run)
		}
	}
}

func cloneEdges(m map[string][]ResolvedEdge) map[string][]ResolvedEdge {
	out := make(map[string][]ResolvedEdge, len(m))
	for k, v := range m {
		out[k] = append([]ResolvedEdge(nil), v...)
	}
	return out
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
