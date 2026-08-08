package arch_timeline

import (
	"os"
	"testing"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/archmodel"
)

func TestSnapshotStore_CreateAndGet(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "snapshot_store_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	store := NewSnapshotStore(tempDir)

	snap1 := &archmodel.ArchSnapshot{
		ID:           "snap-1",
		CommitHash:   "c123456789",
		Timestamp:    time.Now().Truncate(time.Second),
		TopologyHash: "hash-a",
	}

	if err := store.Create(snap1); err != nil {
		t.Fatalf("Failed to create snapshot 1: %v", err)
	}

	// Should not create a new snapshot if topology hash is the same
	snap2 := &archmodel.ArchSnapshot{
		ID:           "snap-2",
		CommitHash:   "c223456789",
		Timestamp:    time.Now().Truncate(time.Second),
		TopologyHash: "hash-a",
	}
	if err := store.Create(snap2); err != nil {
		t.Fatalf("Failed to create snapshot 2: %v", err)
	}

	entries := store.List()
	if len(entries) != 1 {
		t.Errorf("Expected 1 snapshot entry (due to same topology), got %d", len(entries))
	}

	// Create with different topology
	snap3 := &archmodel.ArchSnapshot{
		ID:           "snap-3",
		CommitHash:   "c323456789",
		Timestamp:    time.Now().Truncate(time.Second),
		TopologyHash: "hash-b",
	}
	if err := store.Create(snap3); err != nil {
		t.Fatalf("Failed to create snapshot 3: %v", err)
	}

	entries = store.List()
	if len(entries) != 2 {
		t.Errorf("Expected 2 snapshot entries, got %d", len(entries))
	}

	latest, err := store.Latest()
	if err != nil {
		t.Fatalf("Latest failed: %v", err)
	}
	if latest.CommitHash != "c323456789" {
		t.Errorf("Expected c323456789, got %s", latest.CommitHash)
	}

	getSnap, err := store.Get("c123")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if getSnap.CommitHash != "c123456789" {
		t.Errorf("Expected c123456789, got %s", getSnap.CommitHash)
	}
}
