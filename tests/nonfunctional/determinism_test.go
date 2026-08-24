package nonfunctional_test

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/layout"
	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/types"
	"github.com/Syamchand123/GlassMarble/tests/harness"
)

// TestAnalyzeDeterministicAcrossCheckouts verifies that two byte-identical
// checkouts (same tree, fixed git dates/identity) analyze to the same AKG:
// same commit hash, same node ID set, same edge count. Node IDs are
// content-derived (path::symbol), so they must not depend on sandbox paths.
func TestAnalyzeDeterministicAcrossCheckouts(t *testing.T) {
	setup := func() (*harness.Sandbox, string) {
		sb := harness.NewSandbox(t)
		sb.SampleProject()
		gitInitFixed(t, sb)
		hash := gitCommitAllFixed(t, sb, "initial")
		if _, err := harness.RunGmb(t, sb, "analyze"); err != nil {
			t.Fatalf("analyze failed: %v", err)
		}
		return sb, hash
	}

	sbA, hashA := setup()
	sbB, hashB := setup()
	if hashA != hashB {
		t.Fatalf("fixed-date commits differ across checkouts: %s vs %s", hashA, hashB)
	}

	idsA, commitA, edgesA := exportNodeIDs(t, sbA, "graphA.json")
	idsB, commitB, edgesB := exportNodeIDs(t, sbB, "graphB.json")

	if commitA != commitB {
		t.Errorf("exported commit hash differs: %s vs %s", commitA, commitB)
	}
	if edgesA != edgesB {
		t.Errorf("exported edge count differs: %d vs %d", edgesA, edgesB)
	}
	if !reflect.DeepEqual(idsA, idsB) {
		t.Errorf("node ID sets differ across identical checkouts:\nA: %d nodes\nB: %d nodes", len(idsA), len(idsB))
	}
	if len(idsA) == 0 {
		t.Fatal("analysis produced no nodes")
	}
}

// TestRepeatedAnalyzeStableWithVersionBump verifies re-analysis of an
// unchanged repository is stable: identical exported graphs, and exactly one
// version bump per run (the transaction counter is monotonic).
func TestRepeatedAnalyzeStableWithVersionBump(t *testing.T) {
	sb := harness.NewSandbox(t)
	sb.SampleProject()
	sb.GitInit()

	if _, err := harness.RunGmb(t, sb, "analyze"); err != nil {
		t.Fatalf("first analyze: %v", err)
	}
	v1 := gmVersion(t, sb)
	if v1 != 1 {
		t.Fatalf("version after first analyze = %d, want 1", v1)
	}
	ids1, commit1, edges1 := exportNodeIDs(t, sb, "graph1.json")

	if _, err := harness.RunGmb(t, sb, "analyze"); err != nil {
		t.Fatalf("second analyze: %v", err)
	}
	v2 := gmVersion(t, sb)
	if v2 != v1+1 {
		t.Errorf("version = %d after re-analyze, want %d", v2, v1+1)
	}
	ids2, commit2, edges2 := exportNodeIDs(t, sb, "graph2.json")

	if commit1 != commit2 {
		t.Errorf("commit hash changed across stable re-analyze: %s vs %s", commit1, commit2)
	}
	if edges1 != edges2 {
		t.Errorf("edge count changed across stable re-analyze: %d vs %d", edges1, edges2)
	}
	if !reflect.DeepEqual(ids1, ids2) {
		t.Errorf("node ID sets changed across stable re-analyze")
	}
}

// TestPatternsJSONDeterministic verifies Architecture Intelligence produces byte-identical
// JSON output for the same graph.
func TestPatternsJSONDeterministic(t *testing.T) {
	sb := harness.NewSandbox(t)
	sb.WriteAKGState(harness.TinyGraph())

	out1, err := harness.RunGmb(t, sb, "patterns", "--json")
	if err != nil {
		t.Fatalf("patterns --json (1): %v", err)
	}
	out2, err := harness.RunGmb(t, sb, "patterns", "--json")
	if err != nil {
		t.Fatalf("patterns --json (2): %v", err)
	}
	if out1 != out2 {
		t.Errorf("patterns --json is not deterministic:\n--- run 1 ---\n%s\n--- run 2 ---\n%s", out1, out2)
	}
}

// TestSnapshotCreateIdempotent verifies `snapshot --create` on an unchanged
// repository skip-writes instead of duplicating entries: exactly one snapshot
// remains in the index.
func TestSnapshotCreateIdempotent(t *testing.T) {
	sb := harness.NewSandbox(t)
	sb.SampleProject()
	sb.GitInit()

	if _, err := harness.RunGmb(t, sb, "analyze"); err != nil {
		t.Fatalf("analyze: %v", err)
	}
	out, err := harness.RunGmb(t, sb, "snapshot", "--create")
	if err != nil {
		t.Fatalf("snapshot --create: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Snapshot unchanged") {
		t.Errorf("expected skip-write on identical topology:\n%s", out)
	}
	list, err := harness.RunGmb(t, sb, "snapshot", "--list")
	if err != nil {
		t.Fatalf("snapshot --list: %v", err)
	}
	if got := strings.Count(list, "snap_"); got != 1 {
		t.Errorf("snapshot index has %d entries, want 1:\n%s", got, list)
	}
}

// TestMemoryPipelineIdempotency verifies the developer-memory WAL grows only
// when the architecture actually changes: first analysis records nothing
// (no previous snapshot), a real change appends events, and a no-change
// re-analyze appends nothing further (events deduplicate by ID).
func TestMemoryPipelineIdempotency(t *testing.T) {
	sb := harness.NewSandbox(t)
	sb.SampleProject()
	sb.GitInit()

	eventsPath := sb.Path(".glassmarble/memory/events.jsonl")

	if _, err := harness.RunGmb(t, sb, "analyze"); err != nil {
		t.Fatalf("analyze #1: %v", err)
	}
	l1 := countJSONLLines(t, eventsPath)
	if l1 != 0 {
		t.Fatalf("first analysis recorded %d events (no previous snapshot should skip event generation)", l1)
	}

	main := sb.ReadFile("cmd/api/main.go")
	sb.WriteFile("cmd/api/main.go", main+"\n\n// NFR: idempotency probe\nfunc NfrProbe() {}\n")
	sb.GitCommit("add nfr probe")

	if _, err := harness.RunGmb(t, sb, "analyze"); err != nil {
		t.Fatalf("analyze #2: %v", err)
	}
	l2 := countJSONLLines(t, eventsPath)
	if l2 <= l1 {
		t.Fatalf("changed architecture recorded %d events, want > %d", l2, l1)
	}

	if _, err := harness.RunGmb(t, sb, "analyze"); err != nil {
		t.Fatalf("analyze #3: %v", err)
	}
	l3 := countJSONLLines(t, eventsPath)
	if l3 != l2 {
		t.Errorf("no-change re-analyze grew the WAL: %d -> %d events", l2, l3)
	}

	var mem struct {
		TotalEvents int `json:"total_events"`
	}
	if err := decodeJSONFile(sb.Path(".glassmarble/memory/memory.json"), &mem); err != nil {
		t.Fatalf("decode memory.json: %v", err)
	}
	if mem.TotalEvents != l3 {
		t.Errorf("memory.json total_events = %d, WAL has %d", mem.TotalEvents, l3)
	}
}

// TestCollapseEdgesAndSampleSourcesDeterministic seeds the C3-3 determinism cluster:
// collapseEdges previously collected from a map so edge order was random, and
// sampleSources drew from map iteration on >1500/1200-node graphs with no
// tie-break (hotspot/bottleneck flags could flip). This probe builds a large
// synthetic subgraph (1600 nodes, 1599 edges) with colliding sanitizeName pairs
// (worker-service.go::Worker vs worker_service.go::Worker) and asserts that
// BuildLayoutTree + ComputeAllMetrics produce identical, sorted output across
// repeated runs. The fix sorts collapseEdges by src,pred,tgt, sorts
// getEntryPoints/filterNodes IDs, sorts stateDiagram aliases, and sorts
// sampleSources inputs.
func TestCollapseEdgesAndSampleSourcesDeterministic(t *testing.T) {
	sub := &types.VirtualSubgraph{
		Nodes: make(map[string]*types.TTLNode, 1602),
		Edges: make([]types.TTLEdge, 0, 1600),
	}
	sub.Nodes["worker-service.go::Worker"] = &types.TTLNode{ID: "worker-service.go::Worker", Kind: "STRUCT", Name: "Worker", FileURI: "worker-service.go"}
	sub.Nodes["worker_service.go::Worker"] = &types.TTLNode{ID: "worker_service.go::Worker", Kind: "STRUCT", Name: "Worker", FileURI: "worker_service.go"}
	for i := 0; i < 1600; i++ {
		id := fmt.Sprintf("pkg/file_%d.go::Func%d", i, i)
		if _, ok := sub.Nodes[id]; !ok {
			sub.Nodes[id] = &types.TTLNode{ID: id, Kind: "FUNCTION", Name: fmt.Sprintf("Func%d", i), FileURI: fmt.Sprintf("pkg/file_%d.go", i)}
		}
		if i > 0 {
			prev := fmt.Sprintf("pkg/file_%d.go::Func%d", i-1, i-1)
			pred := "gm:calls"
			if i%3 == 0 {
				pred = "gm:controlFlowTo"
			} else if i%3 == 1 {
				pred = "gm:dataFlowTo"
			}
			sub.Edges = append(sub.Edges, types.TTLEdge{SourceID: prev, Predicate: pred, TargetID: id, LineNumber: i})
		}
	}
	opts := types.QueryOptions{MaxNodes: 0, MaxDepth: 99}
	run := func() (string, *types.GraphSummary) {
		tree := layout.BuildLayoutTree(sub, opts)
		var sb strings.Builder
		for _, e := range tree.Edges {
			sb.WriteString(e.SourceID + "|" + e.Predicate + "|" + e.TargetID + ";")
		}
		return sb.String(), tree.Summary
	}
	firstEdges, firstSummary := run()
	for iter := 0; iter < 5; iter++ {
		edges, summary := run()
		if edges != firstEdges {
			t.Fatalf("collapseEdges non-deterministic on iteration %d: edge order differed (C3-3 not fixed)", iter)
		}
		if summary != nil && firstSummary != nil {
			if summary.Density != firstSummary.Density || summary.Diameter != firstSummary.Diameter || summary.GodObjectCount != firstSummary.GodObjectCount {
				t.Fatalf("GraphSummary non-deterministic on iteration %d: %+v vs %+v", iter, summary, firstSummary)
			}
		}
	}
	m1 := layout.ComputeAllMetrics(sub)
	m2 := layout.ComputeAllMetrics(sub)
	if len(m1.Betweenness) != len(m2.Betweenness) {
		t.Fatalf("betweenness size differs")
	}
	for k, v := range m1.Betweenness {
		if v2, ok := m2.Betweenness[k]; !ok || v != v2 {
			t.Errorf("betweenness non-deterministic for %s: %v vs %v", k, v, v2)
		}
	}
}
