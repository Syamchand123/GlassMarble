package arch_timeline

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/archmodel"
)

func snap(id, commit string, ts time.Time) *archmodel.ArchSnapshot {
	return &archmodel.ArchSnapshot{
		ID:           "snap_" + strings.Repeat(id, 8),
		CommitHash:   commit,
		Timestamp:    ts,
		TopologyHash: "topo-" + id,
	}
}

func testStore(t *testing.T) (*SnapshotStore, string) {
	t.Helper()
	dir := t.TempDir()
	store, err := NewSnapshotStore(dir)
	if err != nil {
		t.Fatalf("NewSnapshotStore: %v", err)
	}
	return store, dir
}

func mustCreate(t *testing.T, store *SnapshotStore, s *archmodel.ArchSnapshot) bool {
	t.Helper()
	wrote, err := store.Create(s)
	if err != nil {
		t.Fatalf("Create(%s): %v", s.ID, err)
	}
	return wrote
}

func TestStore_CreateAndGet(t *testing.T) {
	store, _ := testStore(t)
	t1 := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	wrote := mustCreate(t, store, snap("a", "c123456789", t1))
	if !wrote {
		t.Error("first snapshot must be written")
	}
	// Same topology, different commit → skip-write.
	wrote = mustCreate(t, store, snap("a", "c223456789", t1.Add(time.Hour)))
	if wrote {
		t.Error("same-topology snapshot must not be written")
	}
	// New topology → written.
	mustCreate(t, store, snap("b", "c323456789", t1.Add(2*time.Hour)))

	entries := store.List()
	if len(entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(entries))
	}

	latest, err := store.Latest()
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if latest.CommitHash != "c323456789" {
		t.Errorf("latest commit = %q, want c323456789", latest.CommitHash)
	}

	getSnap, err := store.Get("c123")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if getSnap.CommitHash != "c123456789" {
		t.Errorf("Get commit = %q, want c123456789", getSnap.CommitHash)
	}

	byID, err := store.GetBySnapshotID("snap_a" + strings.Repeat("a", 7))
	if err != nil {
		t.Fatalf("GetBySnapshotID: %v", err)
	}
	if byID.CommitHash != "c123456789" {
		t.Errorf("GetBySnapshotID commit = %q, want c123456789", byID.CommitHash)
	}
	// The 8-hex file portion must work too.
	if _, err := store.GetBySnapshotID("a" + strings.Repeat("a", 7)); err != nil {
		t.Fatalf("GetBySnapshotID(short): %v", err)
	}

	if _, err := store.Get("nope"); err == nil {
		t.Error("Get on an unknown prefix must error")
	}
	if _, err := store.GetBySnapshotID("nope"); err == nil {
		t.Error("GetBySnapshotID on an unknown ID must error")
	}
}

func TestStore_IdempotentCreate(t *testing.T) {
	store, dir := testStore(t)
	ts := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	s := snap("a", "c1", ts)

	mustCreate(t, store, s)
	if wrote := mustCreate(t, store, s); wrote {
		t.Error("re-creating the identical snapshot must be a no-op")
	}
	if got := store.Count(); got != 1 {
		t.Errorf("count = %d, want 1", got)
	}

	// The on-disk index must contain exactly one entry.
	indexPath := filepath.Join(dir, "index.json")
	data, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("read index: %v", err)
	}
	var entries []SnapshotIndexEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		t.Fatalf("index is not valid JSON: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("index entries = %d, want 1", len(entries))
	}
}

// TestStore_LatestUsesMaxTimestamp: Latest must pick the newest by timestamp,
// not the last appended — Create dedupes/skip-writes can leave the index
// unordered with respect to append order.
func TestStore_LatestUsesMaxTimestamp(t *testing.T) {
	store, _ := testStore(t)
	t1 := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	mustCreate(t, store, snap("a", "c1", t1))
	mustCreate(t, store, snap("b", "c2", t1.Add(2*time.Hour)))
	// Skip-write: no index append.
	mustCreate(t, store, snap("b", "c3", t1.Add(3*time.Hour)))

	latest, err := store.Latest()
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if latest.CommitHash != "c2" {
		t.Errorf("Latest = %q, want c2 (newest timestamp, not last appended)", latest.CommitHash)
	}
	if latest.ID != "snap_"+strings.Repeat("b", 8) {
		t.Errorf("Latest ID = %q, want snap_bbbbbbbb", latest.ID)
	}

	// Out-of-order creation timestamps: Latest must still pick the max.
	store2, _ := testStore(t)
	t2 := time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)
	mustCreate(t, store2, snap("a", "c1", t2.Add(2*time.Hour)))
	mustCreate(t, store2, snap("b", "c2", t2))
	latest2, err := store2.Latest()
	if err != nil {
		t.Fatalf("Latest(out-of-order): %v", err)
	}
	if latest2.CommitHash != "c1" {
		t.Errorf("Latest(out-of-order) = %q, want c1 (max timestamp)", latest2.CommitHash)
	}
	entries2 := store2.List()
	if !entries2[0].Timestamp.Before(entries2[1].Timestamp) {
		t.Errorf("List must return timestamp-ascending entries")
	}
}

func TestStore_EmptyCommitHashNoCollision(t *testing.T) {
	store, dir := testStore(t)
	ts := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	// Two uncommitted (watch-mode) states with distinct content: old naming
	// wrote both to "snap_.json", silently overwriting the first.
	s1 := snap("a", "", ts)
	s2 := snap("b", "", ts.Add(time.Hour))
	mustCreate(t, store, s1)
	mustCreate(t, store, s2)

	if got := store.Count(); got != 2 {
		t.Fatalf("count = %d, want 2 — empty-commit snapshots must not collide", got)
	}
	matches, err := filepath.Glob(filepath.Join(dir, "snap_*.json"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(matches) != 2 {
		t.Errorf("snapshot files = %d, want 2 distinct content-addressed files", len(matches))
	}
}

func TestStore_NearestAt(t *testing.T) {
	store, _ := testStore(t)
	t1 := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	mustCreate(t, store, snap("a", "c1", t1))
	mustCreate(t, store, snap("b", "c2", t1.Add(2*time.Hour)))
	mustCreate(t, store, snap("c", "c3", t1.Add(4*time.Hour)))

	// Exact hit.
	s, err := store.NearestAt(t1)
	if err != nil {
		t.Fatalf("NearestAt(exact): %v", err)
	}
	if s.CommitHash != "c1" {
		t.Errorf("NearestAt(exact) = %q, want c1", s.CommitHash)
	}
	// Between snapshots → preceding.
	s, err = store.NearestAt(t1.Add(3 * time.Hour))
	if err != nil {
		t.Fatalf("NearestAt(mid): %v", err)
	}
	if s.CommitHash != "c2" {
		t.Errorf("NearestAt(mid) = %q, want c2", s.CommitHash)
	}
	// Before everything → oldest.
	s, err = store.NearestAt(t1.Add(-time.Hour))
	if err != nil {
		t.Fatalf("NearestAt(before): %v", err)
	}
	if s.CommitHash != "c1" {
		t.Errorf("NearestAt(before) = %q, want c1", s.CommitHash)
	}
	// After everything → newest.
	s, err = store.NearestAt(t1.Add(24 * time.Hour))
	if err != nil {
		t.Fatalf("NearestAt(after): %v", err)
	}
	if s.CommitHash != "c3" {
		t.Errorf("NearestAt(after) = %q, want c3", s.CommitHash)
	}
}

// TestStore_SelfHealingIndex: a corrupt index.json must not lose history —
// the store rebuilds it from the snapshot files.
func TestStore_SelfHealingIndex(t *testing.T) {
	store, dir := testStore(t)
	ts := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	mustCreate(t, store, snap("a", "c1", ts))
	mustCreate(t, store, snap("b", "c2", ts.Add(time.Hour)))

	// Corrupt the index.
	indexPath := filepath.Join(dir, "index.json")
	if err := os.WriteFile(indexPath, []byte("{not valid json"), 0o644); err != nil {
		t.Fatalf("corrupt index: %v", err)
	}

	if got := store.Count(); got != 2 {
		t.Errorf("count after corruption = %d, want 2 (self-healed from files)", got)
	}
	latest, err := store.Latest()
	if err != nil {
		t.Fatalf("Latest after corruption: %v", err)
	}
	if latest.CommitHash != "c2" {
		t.Errorf("Latest = %q, want c2", latest.CommitHash)
	}

	// The index must have been rewritten as valid JSON.
	data, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("read healed index: %v", err)
	}
	var entries []SnapshotIndexEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		t.Fatalf("healed index is not valid JSON: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("healed index entries = %d, want 2", len(entries))
	}
}

// TestStore_MissingIndexRebuild: deleting index.json must not lose history.
func TestStore_MissingIndexRebuild(t *testing.T) {
	store, dir := testStore(t)
	ts := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	mustCreate(t, store, snap("a", "c1", ts))
	if err := os.Remove(filepath.Join(dir, "index.json")); err != nil {
		t.Fatalf("remove index: %v", err)
	}
	if got := store.Count(); got != 1 {
		t.Errorf("count = %d, want 1 (rebuilt from files)", got)
	}
}

// TestStore_SkipWriteKeepsOrdering: skip-written snapshots must not leave
// the index out of timestamp order (Create must not append a skipped entry).
func TestStore_SkipWriteKeepsOrdering(t *testing.T) {
	store, _ := testStore(t)
	t1 := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	mustCreate(t, store, snap("a", "c1", t1))
	mustCreate(t, store, snap("b", "c2", t1.Add(2*time.Hour)))
	mustCreate(t, store, snap("a", "c3", t1.Add(3*time.Hour))) // old topology again → skipped

	entries := store.List()
	if len(entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(entries))
	}
	if !entries[0].Timestamp.Before(entries[1].Timestamp) {
		t.Errorf("index must stay timestamp-ordered: %v, %v", entries[0].Timestamp, entries[1].Timestamp)
	}
}

func TestStore_ErrorsOnNilAndEmpty(t *testing.T) {
	if _, err := NewSnapshotStore(""); err == nil {
		t.Error("NewSnapshotStore(\"\") must error")
	}
	store, _ := testStore(t)

	if _, err := store.Create(nil); err == nil {
		t.Error("Create(nil) must error")
	}
	if _, err := store.Create(&archmodel.ArchSnapshot{Timestamp: time.Now()}); err == nil {
		t.Error("Create without ID must error")
	}
	if _, err := store.Create(&archmodel.ArchSnapshot{ID: "snap_x"}); err == nil {
		t.Error("Create with zero timestamp must error")
	}

	if _, err := store.Get(""); err == nil {
		t.Error("Get(\"\") must error")
	}
	if _, err := store.GetBySnapshotID(""); err == nil {
		t.Error("GetBySnapshotID(\"\") must error")
	}
	if _, err := store.Latest(); err == nil {
		t.Error("Latest on an empty store must error")
	}
	if _, err := store.NearestAt(time.Now()); err == nil {
		t.Error("NearestAt on an empty store must error")
	}
}

// TestStore_SnapshotFileRoundTrip: whatever is stored must come back intact,
// including the embedded graph.
func TestStore_SnapshotFileRoundTrip(t *testing.T) {
	store, _ := testStore(t)
	ts := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	in, err := BuildSnapshot(snapshotInput("c1"))
	if err != nil {
		t.Fatalf("BuildSnapshot: %v", err)
	}
	in.Timestamp = ts
	mustCreate(t, store, in)

	got, err := store.Get("c1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID != in.ID || got.CommitHash != in.CommitHash {
		t.Errorf("round-trip identity mismatch: %+v vs %+v", got, in)
	}
	if len(got.AKGJSON) == 0 || got.NodeCount != in.NodeCount {
		t.Errorf("embedded graph not preserved: nodes %d/%d", got.NodeCount, in.NodeCount)
	}
	if len(got.Components) != 1 || len(got.Patterns) != 1 || len(got.Smells) != 1 {
		t.Errorf("analysis payload not preserved: %+v", got)
	}
}
