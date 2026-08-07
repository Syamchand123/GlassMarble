package akg

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage4"
	akgerrs "github.com/Syamchand123/GlassMarble/internal/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExecuteDelta_NilPayload(t *testing.T) {
	tm := &AKGTransactionManager{
		container: NewMVCCGraphContainer(),
	}
	err := tm.ExecuteDeltaTransaction(nil, nil)
	if err != nil {
		t.Errorf("expected no error for nil payload, got %v", err)
	}
}

// TestMaxTTLBytesGuardRejectsOversizedCommit: a delta whose staged TTL would
// exceed the --max-ttl-mb budget is refused and the previous good file is
// kept (AUDIT Issue 4 Phase 4A-4).
func TestMaxTTLBytesGuardRejectsOversizedCommit(t *testing.T) {
	dir := t.TempDir()
	// 1-byte budget: any serialized graph exceeds it, so the commit must be
	// refused and the previous good file kept.
	tm, err := NewAKGTransactionManagerWithOptions(dir, 1)
	require.NoError(t, err)
	defer tm.Close()

	payload := stage4.NewStage4Output("testhash")
	payload.GraphNodes["n1"] = &stage4.ResolvedNode{ID: "n1", Kind: "FUNCTION", Name: "testFunc"}

	err = tm.ExecuteDeltaTransaction(payload, []string{"test.go"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--max-ttl-mb")

	// The commit must not leave a state file behind.
	if _, statErr := os.Stat(filepath.Join(dir, "akg.json")); !os.IsNotExist(statErr) {
		t.Error("oversized commit must not leave a state file behind")
	}

	// WAL must not block future recovery (rollback on failed save).
	walPath := filepath.Join(dir, "wal", "akg_transactions.wal")
	if st, statErr := os.Stat(walPath); statErr == nil && st.Size() > 0 {
		t.Errorf("WAL must be truncated after a refused commit, got %d bytes", st.Size())
	}
}

// TestMaxTTLBytesGuardRejectsOversizedLoad: an existing state file larger
// than the budget is refused at load (AUDIT Issue 4 Phase 4A-4).
func TestMaxTTLBytesGuardRejectsOversizedLoad(t *testing.T) {
	dir := t.TempDir()
	tm, err := NewAKGTransactionManager(dir)
	require.NoError(t, err)
	payload := stage4.NewStage4Output("testhash")
	payload.GraphNodes["n1"] = &stage4.ResolvedNode{ID: "n1", Kind: "FUNCTION", Name: "testFunc"}
	require.NoError(t, tm.ExecuteDeltaTransaction(payload, []string{"test.go"}))
	tm.Close()

	// 1-byte budget: any existing state file is refused on reload.
	_, err = NewAKGTransactionManagerWithOptions(dir, 1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--max-ttl-mb")

	// A fresh database (no state file) is always allowed.
	fresh := t.TempDir()
	tm2, err := NewAKGTransactionManagerWithOptions(fresh, 1)
	require.NoError(t, err)
	tm2.Close()
}

func TestExecuteDelta_AddNode(t *testing.T) {
	dir := t.TempDir()
	tm, err := NewAKGTransactionManager(dir)
	if err != nil {
		t.Fatalf("failed to create TM: %v", err)
	}
	defer tm.Close()

	payload := stage4.NewStage4Output("testhash")
	payload.GraphNodes["n1"] = &stage4.ResolvedNode{
		ID: "n1", Kind: "FUNCTION", Name: "testFunc",
	}

	err = tm.ExecuteDeltaTransaction(payload, []string{"test.go"})
	if err != nil {
		t.Fatalf("delta execution failed: %v", err)
	}

	snapshot := tm.GetActiveSnapshot()
	if node, ok := snapshot.GetNode("n1"); !ok {
		t.Error("expected node n1 to exist after delta")
	} else if node.Name != "testFunc" {
		t.Errorf("expected name testFunc, got %s", node.Name)
	}
}

func TestExecuteDelta_AddEdge(t *testing.T) {
	dir := t.TempDir()
	tm, err := NewAKGTransactionManager(dir)
	if err != nil {
		t.Fatalf("failed to create TM: %v", err)
	}
	defer tm.Close()

	payload := stage4.NewStage4Output("testhash")
	payload.GraphNodes["a"] = &stage4.ResolvedNode{ID: "a", Kind: "FUNCTION", Name: "a"}
	payload.GraphNodes["b"] = &stage4.ResolvedNode{ID: "b", Kind: "FUNCTION", Name: "b"}
	payload.AddEdge("a", "b", stage4.EdgeCalls, 1)

	if err := tm.ExecuteDeltaTransaction(payload, []string{"test.go"}); err != nil {
		t.Fatalf("delta failed: %v", err)
	}

	snapshot := tm.GetActiveSnapshot()
	edges := snapshot.GetOutboundEdges("a")
	if len(edges) != 1 {
		t.Errorf("expected 1 outbound edge from a, got %d", len(edges))
	}
}

func TestSubscribe_ReceivesEvent(t *testing.T) {
	dir := t.TempDir()
	tm, err := NewAKGTransactionManager(dir)
	if err != nil {
		t.Fatalf("failed to create TM: %v", err)
	}
	defer tm.Close()

	ch := tm.Subscribe()
	payload := stage4.NewStage4Output("testhash")
	payload.GraphNodes["n1"] = &stage4.ResolvedNode{ID: "n1", Kind: "FUNCTION", Name: "fn"}

	if err := tm.ExecuteDeltaTransaction(payload, []string{"test.go"}); err != nil {
		t.Fatalf("delta failed: %v", err)
	}

	event := <-ch
	if event.NodeCount != 1 {
		t.Errorf("expected node_count=1, got %d", event.NodeCount)
	}
}

// ===== ADDITIONAL TRANSACTION MANAGER TESTS =====

func TestExecuteDelta_EmptyPayload(t *testing.T) {
	dir := t.TempDir()
	tm, err := NewAKGTransactionManager(dir)
	if err != nil {
		t.Fatalf("failed to create TM: %v", err)
	}
	defer tm.Close()

	payload := stage4.NewStage4Output("testhash")
	err = tm.ExecuteDeltaTransaction(payload, []string{"test.go"})
	if err != nil {
		t.Fatalf("empty payload delta failed: %v", err)
	}
}

func TestExecuteDelta_DeleteNodes(t *testing.T) {
	dir := t.TempDir()
	tm, err := NewAKGTransactionManager(dir)
	if err != nil {
		t.Fatalf("failed to create TM: %v", err)
	}
	defer tm.Close()

	payload := stage4.NewStage4Output("testhash")
	payload.GraphNodes["n1"] = &stage4.ResolvedNode{
		ID: "n1", Kind: "FUNCTION", Name: "toDelete",
		FileSpec: stage4.LocationMeta{Path: "test.go"},
	}
	if err := tm.ExecuteDeltaTransaction(payload, []string{"test.go"}); err != nil {
		t.Fatalf("add delta failed: %v", err)
	}

	payload2 := stage4.NewStage4Output("testhash")
	if err := tm.ExecuteDeltaTransaction(payload2, []string{"test.go"}); err != nil {
		t.Fatalf("delete delta failed: %v", err)
	}

	snapshot := tm.GetActiveSnapshot()
	if _, ok := snapshot.GetNode("n1"); ok {
		t.Error("node should have been deleted")
	}
}

func TestExecuteDelta_SweepRemovesOldEdges(t *testing.T) {
	dir := t.TempDir()
	tm, err := NewAKGTransactionManager(dir)
	if err != nil {
		t.Fatalf("failed to create TM: %v", err)
	}
	defer tm.Close()

	payload := stage4.NewStage4Output("testhash")
	payload.GraphNodes["a"] = &stage4.ResolvedNode{
		ID: "a", Kind: "FUNCTION", Name: "a", FileSpec: stage4.LocationMeta{Path: "a.go"},
	}
	payload.GraphNodes["b"] = &stage4.ResolvedNode{
		ID: "b", Kind: "FUNCTION", Name: "b", FileSpec: stage4.LocationMeta{Path: "b.go"},
	}
	payload.AddEdge("a", "b", stage4.EdgeCalls, 1)
	if err := tm.ExecuteDeltaTransaction(payload, []string{"a.go", "b.go"}); err != nil {
		t.Fatalf("add delta failed: %v", err)
	}

	payload2 := stage4.NewStage4Output("testhash")
	if err := tm.ExecuteDeltaTransaction(payload2, []string{"b.go"}); err != nil {
		t.Fatalf("sweep delta failed: %v", err)
	}
}

func TestExecuteDelta_AddNodes(t *testing.T) {
	dir := t.TempDir()
	tm, err := NewAKGTransactionManager(dir)
	if err != nil {
		t.Fatalf("failed to create TM: %v", err)
	}
	defer tm.Close()

	payload := stage4.NewStage4Output("testhash")
	payload.GraphNodes["node1"] = &stage4.ResolvedNode{ID: "node1", Kind: "CLASS", Name: "Node1"}
	payload.GraphNodes["node2"] = &stage4.ResolvedNode{ID: "node2", Kind: "FUNCTION", Name: "Node2"}
	payload.GraphNodes["node3"] = &stage4.ResolvedNode{ID: "node3", Kind: "INTERFACE", Name: "Node3"}

	if err := tm.ExecuteDeltaTransaction(payload, []string{"test.go"}); err != nil {
		t.Fatalf("delta execution failed: %v", err)
	}

	graph := tm.GetActiveSnapshot()
	for _, id := range []string{"node1", "node2", "node3"} {
		if _, ok := graph.GetNode(id); !ok {
			t.Errorf("expected node %s to exist after delta", id)
		}
	}
}

func TestExecuteDelta_AddEdges(t *testing.T) {
	dir := t.TempDir()
	tm, err := NewAKGTransactionManager(dir)
	if err != nil {
		t.Fatalf("failed to create TM: %v", err)
	}
	defer tm.Close()

	payload := stage4.NewStage4Output("testhash")
	payload.GraphNodes["a"] = &stage4.ResolvedNode{ID: "a", Kind: "CLASS", Name: "A"}
	payload.GraphNodes["b"] = &stage4.ResolvedNode{ID: "b", Kind: "CLASS", Name: "B"}
	payload.AddEdge("a", "b", stage4.EdgeCalls, 1)

	if err := tm.ExecuteDeltaTransaction(payload, []string{"test.go"}); err != nil {
		t.Fatalf("delta execution failed: %v", err)
	}

	snapshot := tm.GetActiveSnapshot()
	outEdges := snapshot.GetOutboundEdges("a")
	if len(outEdges) != 1 {
		t.Fatalf("expected 1 outbound edge from a, got %d", len(outEdges))
	}
	if outEdges[0].TargetID != "b" {
		t.Errorf("expected edge target b, got %s", outEdges[0].TargetID)
	}
	if outEdges[0].Type != stage4.EdgeCalls {
		t.Errorf("expected EdgeCalls type, got %s", outEdges[0].Type)
	}
}

func TestExecuteDelta_MultipleDeltas(t *testing.T) {
	dir := t.TempDir()
	tm, err := NewAKGTransactionManager(dir)
	if err != nil {
		t.Fatalf("failed to create TM: %v", err)
	}
	defer tm.Close()

	payload1 := stage4.NewStage4Output("hash1")
	payload1.GraphNodes["node1"] = &stage4.ResolvedNode{ID: "node1", Kind: "CLASS", Name: "First"}
	if err := tm.ExecuteDeltaTransaction(payload1, []string{"a.go"}); err != nil {
		t.Fatalf("first delta failed: %v", err)
	}

	payload2 := stage4.NewStage4Output("hash2")
	payload2.GraphNodes["node2"] = &stage4.ResolvedNode{ID: "node2", Kind: "FUNCTION", Name: "Second"}
	if err := tm.ExecuteDeltaTransaction(payload2, []string{"b.go"}); err != nil {
		t.Fatalf("second delta failed: %v", err)
	}

	snapshot := tm.GetActiveSnapshot()
	if _, ok := snapshot.GetNode("node1"); !ok {
		t.Error("expected node1 from first delta to exist")
	}
	if _, ok := snapshot.GetNode("node2"); !ok {
		t.Error("expected node2 from second delta to exist")
	}
}

func TestExecuteDelta_DanglingReferenceAudit(t *testing.T) {
	dir := t.TempDir()
	tm, err := NewAKGTransactionManager(dir)
	if err != nil {
		t.Fatalf("failed to create TM: %v", err)
	}
	defer tm.Close()

	payload := stage4.NewStage4Output("testhash")
	payload.GraphNodes["source"] = &stage4.ResolvedNode{ID: "source", Kind: "FUNCTION", Name: "source"}
	payload.AddEdge("source", "nonexistent", stage4.EdgeCalls, 42)

	// The merge sweep (Step C.4) drops the dangling edge and records it in
	// graph.Errors; the commit succeeds with zero dangling edges persisted
	// (AUDIT Issue 5 Phase 5A-1 engine-side zero-dangling guard).
	if err := tm.ExecuteDeltaTransaction(payload, []string{"test.go"}); err != nil {
		t.Fatalf("delta execution failed: %v", err)
	}

	snapshot := tm.GetActiveSnapshot()
	if len(snapshot.Errors) == 0 {
		t.Error("expected dangling reference errors for edge to nonexistent node")
	}
	found := false
	for _, errRec := range snapshot.Errors {
		if errRec.SourceID == "source" && errRec.TargetID == "nonexistent" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected DanglingReferenceError from source to nonexistent, got errors: %+v", snapshot.Errors)
	}
	if q := MeasureGraphQuality(snapshot); q.DanglingEdges != 0 {
		t.Errorf("dangling edges survived the merge sweep: %v", q)
	}
}

func TestExecuteDelta_MacroInferenceFires(t *testing.T) {
	dir := t.TempDir()
	tm, err := NewAKGTransactionManager(dir)
	if err != nil {
		t.Fatalf("failed to create TM: %v", err)
	}
	defer tm.Close()

	payload := stage4.NewStage4Output("testhash")
	payload.GraphNodes["svc"] = &stage4.ResolvedNode{
		ID: "svc", Kind: "CLASS", Name: "MyService",
	}

	if err := tm.ExecuteDeltaTransaction(payload, []string{"test.go"}); err != nil {
		t.Fatalf("delta execution failed: %v", err)
	}

	snapshot := tm.GetActiveSnapshot()
	rules, ok := snapshot.MacroRules.Get("svc")
	if !ok {
		t.Fatal("expected MacroRules entry for svc after inference")
	}
	hasServiceRule := false
	for _, r := range rules {
		if contains(r, "Service") || contains(r, "Business Logic") {
			hasServiceRule = true
			break
		}
	}
	if !hasServiceRule {
		t.Errorf("expected Service Layer rule for MyService, got rules: %v", rules)
	}
}

func TestGetActiveSnapshot_Immutable(t *testing.T) {
	dir := t.TempDir()
	tm, err := NewAKGTransactionManager(dir)
	if err != nil {
		t.Fatalf("failed to create TM: %v", err)
	}
	defer tm.Close()

	snapshot := tm.GetActiveSnapshot()

	payload := stage4.NewStage4Output("testhash")
	payload.GraphNodes["newNode"] = &stage4.ResolvedNode{
		ID: "newNode", Kind: "CLASS", Name: "NewNode",
	}
	if err := tm.ExecuteDeltaTransaction(payload, []string{"test.go"}); err != nil {
		t.Fatalf("delta execution failed: %v", err)
	}

	if _, ok := snapshot.GetNode("newNode"); ok {
		t.Error("old snapshot should not contain the new node (snapshot should be immutable)")
	}

	newSnapshot := tm.GetActiveSnapshot()
	if _, ok := newSnapshot.GetNode("newNode"); !ok {
		t.Error("current snapshot should contain the new node")
	}
}

func TestLock_AcquireRelease(t *testing.T) {
	dir := t.TempDir()
	tm, err := NewAKGTransactionManager(dir)
	if err != nil {
		t.Fatalf("failed to create TM: %v", err)
	}
	defer tm.Close()

	if err := tm.AcquireLock(); err != nil {
		t.Fatalf("first AcquireLock failed: %v", err)
	}
	tm.ReleaseLock()

	if err := tm.AcquireLock(); err != nil {
		t.Fatalf("second AcquireLock (re-acquire) failed: %v", err)
	}
	tm.ReleaseLock()
}

func TestLock_Contention(t *testing.T) {
	dir := t.TempDir()
	tm, err := NewAKGTransactionManager(dir)
	if err != nil {
		t.Fatalf("failed to create TM: %v", err)
	}
	defer tm.Close()

	if err := tm.AcquireLock(); err != nil {
		t.Fatalf("first AcquireLock failed: %v", err)
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- tm.AcquireLock()
	}()

	time.Sleep(100 * time.Millisecond)
	tm.ReleaseLock()

	select {
	case err := <-errCh:
		if err != nil {
			t.Errorf("lock contention should resolve after first holder releases: %v", err)
		}
		tm.ReleaseLock()
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for lock contention to resolve")
	}
}

func TestGetActiveGraph(t *testing.T) {
	dir, _ := os.MkdirTemp("", "akg_get_active")
	defer os.RemoveAll(dir)
	tm, err := NewAKGTransactionManager(dir)
	require.NoError(t, err)
	defer tm.Close()

	graph := tm.GetActiveGraph()
	assert.NotNil(t, graph)
	assert.Equal(t, 0, graph.Nodes.Len())
}

// TestRecover_BoundedReplay verifies that WAL entries already captured in the
// TTL (txID <= maxAppliedTx from the gm:version metadata) are skipped, while
// newer committed entries are replayed exactly once (AUDIT Issue 3 Phase 3B-7).
func TestRecover_BoundedReplay(t *testing.T) {
	dir := t.TempDir()
	tm, err := NewAKGTransactionManager(dir)
	require.NoError(t, err)
	defer tm.Close()

	payload := stage4.NewStage4Output("hash1")
	payload.GraphNodes["real1"] = &stage4.ResolvedNode{ID: "real1", Kind: "FUNCTION", Name: "real1"}
	require.NoError(t, tm.ExecuteDeltaTransaction(payload, []string{"a.go"}))
	// After a successful commit the WAL is truncated; graph.Version is now 1.

	// Simulate a crash window: stale committed entry for the already-applied
	// tx 1 (must be skipped) and a new committed entry tx 2 (must be replayed).
	require.NoError(t, tm.wal.AppendEntry(&WALEntry{
		TxID: 1, CommitHash: "hash1", Status: WALStatusStarted, ModifiedFiles: []string{"a.go"},
		Payload: &stage4.Stage4Output{CommitHash: "hash1", GraphNodes: map[string]*stage4.ResolvedNode{
			"staleNode": {ID: "staleNode", Kind: "FUNCTION", Name: "stale"},
		}},
	}))
	require.NoError(t, tm.wal.MarkCommitted(1))
	require.NoError(t, tm.wal.AppendEntry(&WALEntry{
		TxID: 2, CommitHash: "hash2", Status: WALStatusStarted, ModifiedFiles: []string{"b.go"},
		Payload: &stage4.Stage4Output{CommitHash: "hash2", GraphNodes: map[string]*stage4.ResolvedNode{
			"replayed2": {ID: "replayed2", Kind: "FUNCTION", Name: "replayed2"},
		}},
	}))
	require.NoError(t, tm.wal.MarkCommitted(2))

	require.NoError(t, tm.Recover())

	graph := tm.GetActiveSnapshot()
	require.Equal(t, uint64(2), graph.Version, "replay bound should advance to newest replayed tx")
	if _, ok := graph.GetNode("real1"); !ok {
		t.Error("node from committed delta must survive")
	}
	if _, ok := graph.GetNode("replayed2"); !ok {
		t.Error("newer committed WAL entry must be replayed")
	}
	if _, ok := graph.GetNode("staleNode"); ok {
		t.Error("stale WAL entry at or below maxAppliedTx must NOT be replayed")
	}
}

// TestSchemaVersionMismatchRejected verifies that a persisted TTL with a newer
// schema version fails loudly instead of silently loading an empty graph
// (AUDIT Issue 3 Phase 3A-3).
func TestSchemaVersionMismatchRejected(t *testing.T) {
	dir := t.TempDir()
	StatePath := filepath.Join(dir, "akg_state.ttl")
	ttl := `@prefix gm: <http://glassmarble.org/ontology#> .
@prefix rdf: <http://www.w3.org/1999/02/22-rdf-syntax-ns#> .
<http://glassmarble.org/node/metadata> a gm:MetaData ;
    gm:schemaVersion "99" ;
    gm:commitHash "future" ;
    gm:version "7" .
`
	require.NoError(t, os.WriteFile(StatePath, []byte(ttl), 0644))

	_, err := NewAKGTransactionManager(dir)
	require.Error(t, err)
	assert.ErrorIs(t, err, akgerrs.ErrSchemaVersion)
}

// TestDeleteRoundTrip_NoResurrection verifies that a deleted node is dropped
// from the persisted akg.json and never resurrects after a reload (AUDIT
// Issue 3 Phase 3B-6). Since Phase C the store is rewritten in full on every
// commit, so deleted nodes are simply absent from the JSON document — no
// tombstone blocks are needed.
func TestDeleteRoundTrip_NoResurrection(t *testing.T) {
	dir := t.TempDir()
	tm, err := NewAKGTransactionManager(dir)
	require.NoError(t, err)

	payload := stage4.NewStage4Output("hash1")
	for i := 0; i < 5; i++ {
		id := fmt.Sprintf("keep%d", i)
		payload.GraphNodes[id] = &stage4.ResolvedNode{
			ID: id, Kind: "FUNCTION", Name: id,
			FileSpec: stage4.LocationMeta{Path: fmt.Sprintf("keep%d.go", i)},
		}
	}
	payload.GraphNodes["gone"] = &stage4.ResolvedNode{
		ID: "gone", Kind: "FUNCTION", Name: "gone",
		FileSpec: stage4.LocationMeta{Path: "gone.go"},
	}
	files := []string{"keep0.go", "keep1.go", "keep2.go", "keep3.go", "keep4.go", "gone.go"}
	require.NoError(t, tm.ExecuteDeltaTransaction(payload, files))

	// Sweep: empty payload for the same file removes the node.
	require.NoError(t, tm.ExecuteDeltaTransaction(stage4.NewStage4Output("hash1"), []string{"gone.go"}))
	tm.Close()

	// The rewritten JSON store must not contain the deleted node.
	data, err := os.ReadFile(filepath.Join(dir, "akg.json"))
	require.NoError(t, err)
	var doc GraphJSON
	require.NoError(t, json.Unmarshal(data, &doc))
	for _, n := range doc.Nodes {
		if n.ID == "gone" {
			t.Fatalf("deleted node must be absent from akg.json:\n%s", string(data))
		}
	}

	reloaded, err := NewAKGTransactionManager(dir)
	require.NoError(t, err)
	defer reloaded.Close()
	if _, ok := reloaded.GetActiveSnapshot().GetNode("gone"); ok {
		t.Error("deleted node resurrected after reload")
	}
	for i := 0; i < 5; i++ {
		id := fmt.Sprintf("keep%d", i)
		if _, ok := reloaded.GetActiveSnapshot().GetNode(id); !ok {
			t.Errorf("surviving node %s was lost after reload", id)
		}
	}
}

// TestDeltaPersistsEntrypointsAndZones verifies the delta serializer carries
// gm:isEntrypoint and gm:primitiveZone so incremental saves preserve them
// (AUDIT Issue 3 Phase 3B-8).
func TestDeltaPersistsEntrypointsAndZones(t *testing.T) {
	dir := t.TempDir()
	tm, err := NewAKGTransactionManager(dir)
	require.NoError(t, err)
	defer tm.Close()

	payload := stage4.NewStage4Output("hash1")
	payload.GraphNodes["ep1"] = &stage4.ResolvedNode{ID: "ep1", Kind: "FUNCTION", Name: "main"}
	payload.GraphNodes["mod1"] = &stage4.ResolvedNode{ID: "mod1", Kind: "MODULE", Name: "app"}
	payload.EntrypointRegistry = []string{"ep1"}
	payload.FolderZones = map[string]string{"mod1": "user"}
	require.NoError(t, tm.ExecuteDeltaTransaction(payload, []string{"a.go"}))

	reloaded, err := NewAKGTransactionManager(dir)
	require.NoError(t, err)
	defer reloaded.Close()

	snapshot := reloaded.GetActiveSnapshot()
	require.Contains(t, snapshot.Entrypoints, "ep1")
	if zone, ok := snapshot.FolderZones.Get("mod1"); !ok || zone != "user" {
		t.Errorf("expected folder zone user for mod1, got %q (ok=%v)", zone, ok)
	}
}

// TestDanglingEdgeSweepNeverPersists verifies the engine-side zero-dangling
// guard (AUDIT Issue 5 Phase 5A-1): a delta whose edge targets a missing
// node commits cleanly — the sweep drops the edge, the WAL truncates, and
// the reloaded graph contains zero dangling edges.
func TestDanglingEdgeSweepNeverPersists(t *testing.T) {
	dir := t.TempDir()
	tm, err := NewAKGTransactionManager(dir)
	require.NoError(t, err)
	defer tm.Close()

	// Seed a clean baseline state.
	seed := stage4.NewStage4Output("hash1")
	seed.GraphNodes["a.go::ok"] = &stage4.ResolvedNode{ID: "a.go::ok", Kind: "FUNCTION", Name: "ok"}
	require.NoError(t, tm.ExecuteDeltaTransaction(seed, []string{"a.go"}))

	// A delta carrying an edge to a node that does not exist anywhere.
	bad := stage4.NewStage4Output("hash2")
	bad.GraphNodes["a.go::caller"] = &stage4.ResolvedNode{ID: "a.go::caller", Kind: "FUNCTION", Name: "caller"}
	bad.OutboundEdges["a.go::caller"] = []stage4.ResolvedEdge{
		{SourceID: "a.go::caller", TargetID: "missing::callee", Type: stage4.EdgeCalls, LineNumber: 5},
	}
	require.NoError(t, tm.ExecuteDeltaTransaction(bad, []string{"a.go"}))

	// The sweep must have dropped the dangling edge without touching the
	// surviving baseline node.
	snapshot := tm.GetActiveSnapshot()
	require.Zero(t, MeasureGraphQuality(snapshot).DanglingEdges)
	_, ok := snapshot.GetNode("a.go::ok")
	require.True(t, ok, "baseline node must survive the sweep")

	// The WAL truncates after the commit: a fresh manager opens cleanly and
	// the persisted file carries no dangling edges.
	reloaded, err := NewAKGTransactionManager(dir)
	require.NoError(t, err)
	defer reloaded.Close()
	reloadedSnapshot := reloaded.GetActiveSnapshot()
	require.Zero(t, MeasureGraphQuality(reloadedSnapshot).DanglingEdges)
	require.Equal(t, 2, reloadedSnapshot.Nodes.Len())
}
