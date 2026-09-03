package arch_intelligence

import (
	"strings"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/link"
)

// libraryGraph models a package with no main(): two exported functions, one
// unexported helper nothing calls, and two variables. Without withEntrypoint
// nothing declares an entrypoint, which is the normal state of a library.
func libraryGraph(withEntrypoint bool) *akg.CodePropertyGraph {
	g := akg.NewCodePropertyGraph("test")
	add := func(id, kind, path string) {
		n := testNode(id, path)
		n.Kind = kind
		g.Nodes = g.Nodes.Set(id, n)
	}
	add("Serve", "FUNCTION", "lib/serve.go")
	add("Close", "FUNCTION", "lib/serve.go")
	add("orphanHelper", "FUNCTION", "lib/serve.go")
	add("bufSize", "VARIABLE", "lib/serve.go")
	add("serveBody", "VARIABLE", "lib/serve.go")
	if withEntrypoint {
		add("main", "FUNCTION", "cmd/main.go")
		addStructuralEdge(g, "main", "Serve", link.EdgeCalls)
		g.Entrypoints = []string{"main"}
	}
	return g
}

// TestReachabilityUndefinedWithoutEntrypoints pins the fix for a metric that
// scored a perfect result for a measurement that never ran. DeadCodeNodes
// returns nothing when there are no entrypoints -- correctly, since there is
// no evidence of deadness -- and the old ratio turned that empty result into
// "1.0 - 0/N" = 100% reachable, which then went into the AI evidence context
// and the pattern tool as fact.
func TestReachabilityUndefinedWithoutEntrypoints(t *testing.T) {
	snap := NewGraphSnapshot(libraryGraph(false))
	stats := DeadCodeStatsSnapshot(snap)
	if stats.Entrypoints != 0 {
		t.Fatalf("expected no entrypoints, got %d", stats.Entrypoints)
	}

	m := CalculateMetricsFromSnapshot(snap)
	if m.EntrypointCount != 0 {
		t.Errorf("EntrypointCount = %d, want 0", m.EntrypointCount)
	}
	if m.ReachableFromEntrypoints != 0 {
		t.Errorf("reachability must stay 0 (undefined) with no entrypoints, got %v",
			m.ReachableFromEntrypoints)
	}

	summary := MetricSummary(m)
	if strings.Contains(summary, "100% reachable") {
		t.Errorf("summary claims a perfect score for a measurement that never ran: %s", summary)
	}
	if !strings.Contains(summary, "not measured") {
		t.Errorf("summary should say reachability is unavailable, got: %s", summary)
	}
}

// TestReachabilityRatioUsesCandidatePopulation pins the second half: dead
// nodes are only ever code units outside excluded paths, so dividing by every
// node in the graph -- variables included -- silently understated deadness.
func TestReachabilityRatioUsesCandidatePopulation(t *testing.T) {
	snap := NewGraphSnapshot(libraryGraph(true))
	stats := DeadCodeStatsSnapshot(snap)

	if stats.Entrypoints != 1 {
		t.Fatalf("Entrypoints = %d, want 1", stats.Entrypoints)
	}
	// Candidates are the four FUNCTION nodes; the two VARIABLE nodes are not
	// eligible for deadness and must not pad the denominator.
	if stats.Candidates != 4 {
		t.Fatalf("Candidates = %d, want 4 (functions only, not variables)", stats.Candidates)
	}
	if stats.Candidates >= snap.Len() {
		t.Fatalf("candidate population should be smaller than the whole graph (%d vs %d)",
			stats.Candidates, snap.Len())
	}

	m := CalculateMetricsFromSnapshot(snap)
	want := 1.0 - float64(m.DeadCodeNodeCount)/float64(stats.Candidates)
	if diff := m.ReachableFromEntrypoints - want; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("ReachableFromEntrypoints = %v, want %v (dead %d over %d candidates)",
			m.ReachableFromEntrypoints, want, m.DeadCodeNodeCount, stats.Candidates)
	}
	// The old denominator was snap.Len(), which would have reported a
	// strictly rosier number for the same dead set.
	if m.DeadCodeNodeCount > 0 {
		oldWay := 1.0 - float64(m.DeadCodeNodeCount)/float64(snap.Len())
		if m.ReachableFromEntrypoints >= oldWay {
			t.Errorf("ratio over candidates (%v) should be stricter than over all nodes (%v)",
				m.ReachableFromEntrypoints, oldWay)
		}
	}
}

// TestDeadCodeNodesSnapshotUnchanged: the wrapper kept its old contract.
func TestDeadCodeNodesSnapshotUnchanged(t *testing.T) {
	if got := DeadCodeNodesSnapshot(nil); got != nil {
		t.Errorf("nil snapshot should yield nil, got %v", got)
	}
	if got := DeadCodeNodesSnapshot(NewGraphSnapshot(libraryGraph(false))); got != nil {
		t.Errorf("no entrypoints should yield nil, got %v", got)
	}
	dead := DeadCodeNodesSnapshot(NewGraphSnapshot(libraryGraph(true)))
	if len(dead) == 0 {
		t.Error("orphanHelper is unreachable and unexported; expected it reported dead")
	}
}
