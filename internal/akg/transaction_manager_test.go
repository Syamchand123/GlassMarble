package akg

import (
	"os"
	"testing"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage4"
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


