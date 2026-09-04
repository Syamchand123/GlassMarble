package akg

import (
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/link"
)

// orderingPayload builds a minimal, self-consistent LinkOutput.
func orderingPayload(commit string) *link.LinkOutput {
	return &link.LinkOutput{
		CommitHash: commit,
		GraphNodes: map[string]*link.ResolvedNode{
			"pkg/a.go::A": {Kind: "STRUCT", Name: "A", FileSpec: link.LocationMeta{Path: "pkg/a.go", LineStart: 1}},
			"pkg/b.go::B": {Kind: "STRUCT", Name: "B", FileSpec: link.LocationMeta{Path: "pkg/b.go", LineStart: 10}},
		},
		OutboundEdges: map[string][]link.ResolvedEdge{
			"pkg/a.go::A": {{Type: "calls", TargetID: "pkg/b.go::B", LineNumber: 5}},
		},
	}
}

// TestCommit_NotVisibleWhenPersistFails proves the durability ordering of
// ExecuteDeltaTransaction: a commit whose durable write fails must never
// become visible to in-process readers.
//
// Against the pre-fix ordering the shadow snapshot was promoted with
// PromoteShadowSnapshot BEFORE saveToDisk ran, so a failed (or crashed) write
// left the in-memory graph strictly ahead of akg.json: this process saw a
// commit that no restart would ever see.
func TestCommit_NotVisibleWhenPersistFails(t *testing.T) {
	dir := t.TempDir()
	tm, err := NewAKGTransactionManager(dir)
	if err != nil {
		t.Fatalf("new tm: %v", err)
	}
	defer tm.Close()

	before := tm.GetActiveSnapshot()
	beforeVersion := before.Version
	beforeNodes := before.Nodes.Len()

	// Force the durable write to fail after serialization but before the
	// atomic rename: the state-file budget gate rejects the staged temp file,
	// so akg.json is never replaced.
	tm.MaxStateBytes = 1

	if err := tm.ExecuteDeltaTransaction(orderingPayload("c1"), []string{"pkg/a.go", "pkg/b.go"}); err == nil {
		t.Fatal("ExecuteDeltaTransaction returned nil error despite a failed persist")
	}

	after := tm.GetActiveSnapshot()
	if _, ok := after.Nodes.Get("pkg/a.go::A"); ok {
		t.Error("failed commit is visible in memory: node pkg/a.go::A was promoted despite the persist failure")
	}
	if got := after.Nodes.Len(); got != beforeNodes {
		t.Errorf("node count = %d after failed commit, want %d (pre-commit state)", got, beforeNodes)
	}
	if after.Version != beforeVersion {
		t.Errorf("active graph version = %d after failed commit, want %d (pre-commit state)", after.Version, beforeVersion)
	}

	// The next successful commit must still work and must be visible.
	tm.MaxStateBytes = 0
	if err := tm.ExecuteDeltaTransaction(orderingPayload("c2"), []string{"pkg/a.go", "pkg/b.go"}); err != nil {
		t.Fatalf("commit after a failed commit: %v", err)
	}
	if _, ok := tm.GetActiveSnapshot().Nodes.Get("pkg/a.go::A"); !ok {
		t.Error("successful commit is not visible in memory")
	}
}

// TestCommit_MemoryMatchesDiskAfterFailedPersist reloads the store from disk
// and asserts the reloaded graph agrees with what the live process reports —
// the invariant the promote-before-persist ordering violated.
func TestCommit_MemoryMatchesDiskAfterFailedPersist(t *testing.T) {
	dir := t.TempDir()
	tm, err := NewAKGTransactionManager(dir)
	if err != nil {
		t.Fatalf("new tm: %v", err)
	}

	// One good commit so akg.json exists with a known state.
	if err := tm.ExecuteDeltaTransaction(orderingPayload("good"), []string{"pkg/a.go", "pkg/b.go"}); err != nil {
		t.Fatalf("first commit: %v", err)
	}
	liveNodes := tm.GetActiveSnapshot().Nodes.Len()

	// Now a commit that adds a node but cannot be persisted.
	tm.MaxStateBytes = 1
	payload := orderingPayload("bad")
	payload.GraphNodes["pkg/c.go::C"] = &link.ResolvedNode{Kind: "STRUCT", Name: "C", FileSpec: link.LocationMeta{Path: "pkg/c.go", LineStart: 3}}
	if err := tm.ExecuteDeltaTransaction(payload, []string{"pkg/a.go", "pkg/b.go", "pkg/c.go"}); err == nil {
		t.Fatal("expected the persist to be refused by the state budget")
	}
	tm.MaxStateBytes = 0
	tm.Close()

	if got := tm.GetActiveSnapshot().Nodes.Len(); got != liveNodes {
		t.Errorf("in-memory node count = %d after failed persist, want %d", got, liveNodes)
	}

	// A restart must observe exactly the same graph the live process reports.
	tm2, err := NewAKGTransactionManager(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer tm2.Close()
	if got := tm2.GetActiveSnapshot().Nodes.Len(); got != liveNodes {
		t.Errorf("reloaded node count = %d, but the live process reported %d: memory ran ahead of disk", got, liveNodes)
	}
	if _, ok := tm2.GetActiveSnapshot().Nodes.Get("pkg/c.go::C"); ok {
		t.Error("a node from an unpersisted commit survived the restart")
	}
}
