package review

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestQueueAndListPending(t *testing.T) {
	root := t.TempDir()

	// Missing file → empty, nil error.
	got, err := ListPending(root)
	if err != nil {
		t.Fatalf("ListPending on missing file: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty pending, got %v", got)
	}

	id, err := Queue(root, ReviewItem{
		Kind:      "conflict",
		DocPath:   "docs/auth.md",
		SectionID: "intro",
		Summary:   "merge conflict in intro",
		Detail:    "both sides added a paragraph",
	})
	if err != nil {
		t.Fatalf("Queue failed: %v", err)
	}
	if !strings.HasPrefix(id, "r") || len(id) != 9 {
		t.Errorf("ID shape: want r+8 hex, got %q", id)
	}

	pending, err := ListPending(root)
	if err != nil {
		t.Fatalf("ListPending failed: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending, got %d", len(pending))
	}
	it := pending[0]
	if it.ID != id || it.Status != StatusPending || it.Kind != "conflict" {
		t.Errorf("roundtrip mismatch: %+v", it)
	}
	if it.CreatedAt == "" {
		t.Error("CreatedAt not stamped")
	}

	// Distinct enqueues get distinct IDs.
	id2, err := Queue(root, ReviewItem{Kind: "conflict", DocPath: "docs/auth.md", SectionID: "intro", Summary: "merge conflict in intro"})
	if err != nil {
		t.Fatalf("second Queue failed: %v", err)
	}
	if id2 == id {
		t.Error("duplicate IDs for distinct enqueues")
	}
}

func TestQueueDefaultsAndValidation(t *testing.T) {
	root := t.TempDir()
	id, err := Queue(root, ReviewItem{Kind: "prose", Summary: "voice drift"})
	if err != nil {
		t.Fatalf("Queue failed: %v", err)
	}
	pending, _ := ListPending(root)
	if len(pending) != 1 || pending[0].ID != id || pending[0].Status != StatusPending {
		t.Errorf("empty status must default to pending: %+v", pending)
	}
	if _, err := Queue(root, ReviewItem{Kind: "prose", Status: "bogus"}); err == nil {
		t.Error("unknown status must be rejected")
	}
}

func TestResolveApproveAndReject(t *testing.T) {
	root := t.TempDir()
	idA, _ := Queue(root, ReviewItem{Kind: "snippet-fix", DocPath: "docs/a.md", Summary: "fix arity"})
	idR, _ := Queue(root, ReviewItem{Kind: "adr-draft", DocPath: "docs/adr/x.md", Summary: "draft ADR"})

	if err := Resolve(root, idA, true, "looks right"); err != nil {
		t.Fatalf("approve failed: %v", err)
	}
	if err := Resolve(root, idR, false, "wrong callout"); err != nil {
		t.Fatalf("reject failed: %v", err)
	}

	pending, err := ListPending(root)
	if err != nil || len(pending) != 0 {
		t.Fatalf("expected no pending after resolve, got %v, %v", pending, err)
	}
	p, a, r, o, err := Stats(root)
	if err != nil || p != 0 || a != 1 || r != 1 || o != 0 {
		t.Errorf("stats = %d/%d/%d/%d, %v; want 0/1/1/0", p, a, r, o, err)
	}

	// Resolved items carry stamp + reason.
	raw, _ := os.ReadFile(filepath.Join(root, ".glassmarble", "review.json"))
	var items []ReviewItem
	if err := json.Unmarshal(raw, &items); err != nil {
		t.Fatalf("queue file must stay valid JSON: %v", err)
	}
	byID := map[string]ReviewItem{}
	for _, it := range items {
		byID[it.ID] = it
	}
	if byID[idA].ResolvedAt == "" || byID[idA].Reason != "looks right" {
		t.Errorf("approve stamp missing: %+v", byID[idA])
	}
	if byID[idR].Status != StatusRejected || byID[idR].Reason != "wrong callout" {
		t.Errorf("reject stamp missing: %+v", byID[idR])
	}

	// Re-resolve (overturn) is allowed for non-terminal states.
	if err := Resolve(root, idA, false, "on second thought"); err != nil {
		t.Errorf("overturn must be allowed: %v", err)
	}
	if _, a, r, _, _ := Stats(root); a != 0 || r != 2 {
		t.Errorf("after overturn want 0/2 approved/rejected, got %d/%d", a, r)
	}
}

func TestResolveUnknownID(t *testing.T) {
	root := t.TempDir()
	if _, err := Queue(root, ReviewItem{Kind: "prose", Summary: "x"}); err != nil {
		t.Fatal(err)
	}
	if err := Resolve(root, "rdeadbeef", true, ""); err == nil {
		t.Error("resolving unknown id must fail")
	} else if !strings.Contains(err.Error(), "unknown id") {
		t.Errorf("error should name the problem, got %q", err)
	}
	if err := Resolve(root, "rdeadbeef", true, ""); err == nil {
		t.Error("resolving against a missing file must fail")
	}
}

func TestRecordRevertObservedTerminal(t *testing.T) {
	root := t.TempDir()
	if err := RecordRevert(root, "docs/auth.md", "intro", "human rewrote the table"); err != nil {
		t.Fatalf("RecordRevert failed: %v", err)
	}

	// Reverts never surface as pending work.
	pending, err := ListPending(root)
	if err != nil || len(pending) != 0 {
		t.Fatalf("revert must not be pending, got %v, %v", pending, err)
	}
	p, a, r, o, err := Stats(root)
	if err != nil || p != 0 || a != 0 || r != 0 || o != 1 {
		t.Errorf("stats = %d/%d/%d/%d, %v; want 0/0/0/1", p, a, r, o, err)
	}

	raw, _ := os.ReadFile(filepath.Join(root, ".glassmarble", "review.json"))
	var items []ReviewItem
	if err := json.Unmarshal(raw, &items); err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Kind != "revert" || items[0].Status != StatusObserved {
		t.Fatalf("revert record wrong: %+v", items)
	}
	if items[0].ResolvedAt == "" || items[0].DocPath != "docs/auth.md" || items[0].SectionID != "intro" {
		t.Errorf("revert record incomplete: %+v", items[0])
	}

	// Resolve must refuse the terminal observed state.
	if err := Resolve(root, items[0].ID, true, "change mind"); err == nil {
		t.Error("transition from observed must fail")
	} else if !strings.Contains(err.Error(), "terminal") {
		t.Errorf("error should say terminal, got %q", err)
	}
}

func TestPersistenceRoundtrip(t *testing.T) {
	root := t.TempDir()
	id, err := Queue(root, ReviewItem{Kind: "gate5-discard", DocPath: "docs/b.md", SectionID: "s", Summary: "churn-only rewrite discarded"})
	if err != nil {
		t.Fatal(err)
	}
	// Reload from disk in a fresh view: file must be a JSON array.
	raw, err := os.ReadFile(filepath.Join(root, ".glassmarble", "review.json"))
	if err != nil {
		t.Fatalf("queue file missing: %v", err)
	}
	var items []ReviewItem
	if err := json.Unmarshal(raw, &items); err != nil {
		t.Fatalf("queue file not a JSON array: %v", err)
	}
	if len(items) != 1 || items[0].ID != id {
		t.Fatalf("roundtrip mismatch: %+v", items)
	}
	if err := Resolve(root, id, true, "ok"); err != nil {
		t.Fatal(err)
	}
	// Second reload reflects the resolution.
	raw2, _ := os.ReadFile(filepath.Join(root, ".glassmarble", "review.json"))
	var items2 []ReviewItem
	if err := json.Unmarshal(raw2, &items2); err != nil || len(items2) != 1 || items2[0].Status != StatusApproved {
		t.Fatalf("resolution did not persist: %+v, %v", items2, err)
	}
}

func TestCorruptFileError(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".glassmarble")
	_ = os.MkdirAll(dir, 0755)
	_ = os.WriteFile(filepath.Join(dir, "review.json"), []byte("{not valid json"), 0644)

	if _, err := ListPending(root); err == nil {
		t.Error("ListPending on corrupt file must fail")
	}
	if _, err := Queue(root, ReviewItem{Kind: "prose"}); err == nil {
		t.Error("Queue on corrupt file must fail (no silent overwrite)")
	}
	if err := Resolve(root, "r12345678", true, ""); err == nil {
		t.Error("Resolve on corrupt file must fail")
	}
	if _, _, _, _, err := Stats(root); err == nil {
		t.Error("Stats on corrupt file must fail")
	}
	if err := RecordRevert(root, "docs/x.md", "s", "why"); err == nil {
		t.Error("RecordRevert on corrupt file must fail")
	}
}

func TestStatsEmpty(t *testing.T) {
	p, a, r, o, err := Stats(t.TempDir())
	if err != nil || p != 0 || a != 0 || r != 0 || o != 0 {
		t.Errorf("missing queue stats = %d/%d/%d/%d, %v; want zeros", p, a, r, o, err)
	}
}
