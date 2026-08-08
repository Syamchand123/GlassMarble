package developer_memory

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/archmodel"
)

func TestMemoryStore_Lifecycle(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "memory_store_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	store := NewMemoryStore(tempDir)

	// Test Save and Load Memory
	mem := &DeveloperMemory{
		ProjectID:       "proj-1",
		ComponentMemory: make(map[string]ComponentHistory),
	}
	mem.ComponentMemory["auth"] = ComponentHistory{
		Name:      "auth",
		FirstSeen: time.Now(),
		State:     StateActive,
	}

	if err := store.SaveMemory(mem); err != nil {
		t.Fatalf("SaveMemory failed: %v", err)
	}

	loaded, err := store.LoadMemory()
	if err != nil {
		t.Fatalf("LoadMemory failed: %v", err)
	}
	if loaded.ProjectID != "proj-1" {
		t.Errorf("Expected proj-1, got %s", loaded.ProjectID)
	}
	if _, ok := loaded.ComponentMemory["auth"]; !ok {
		t.Errorf("Expected auth component to be present")
	}

	// Test Append operations
	event := archmodel.ArchEvent{ID: "event-1", Kind: archmodel.EventServiceAdded}
	if err := store.AppendEvent(event); err != nil {
		t.Fatalf("AppendEvent failed: %v", err)
	}

	claim := KnowledgeClaim{ID: "claim-1", Subject: "auth", Predicate: "exists"}
	if err := store.AppendClaim(claim); err != nil {
		t.Fatalf("AppendClaim failed: %v", err)
	}

	entry := archmodel.TimelineEntry{CommitHash: "hash123", Title: "test"}
	if err := store.AppendTimelineEntry(entry); err != nil {
		t.Fatalf("AppendTimelineEntry failed: %v", err)
	}

	// Verify files created
	for _, file := range []string{"events.jsonl", "claims.jsonl", "timeline.jsonl"} {
		if _, err := os.Stat(filepath.Join(tempDir, file)); os.IsNotExist(err) {
			t.Errorf("Expected file %s to exist", file)
		}
	}
}
