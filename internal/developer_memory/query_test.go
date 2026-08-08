package developer_memory

import (
	"os"
	"testing"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/archmodel"
)

func setupTestStore(t *testing.T) *MemoryStore {
	tempDir, err := os.MkdirTemp("", "query_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	store := NewMemoryStore(tempDir)
	builder := NewMemoryBuilder(store)

	ts := time.Now()
	events := []archmodel.ArchEvent{
		{
			ID:         "event-1",
			Kind:       archmodel.EventServiceAdded,
			CommitHash: "c1",
			Timestamp:  ts.Add(-10 * time.Hour),
			Components: []string{"RedisCache"},
		},
		{
			ID:         "event-2",
			Kind:       archmodel.EventServiceAdded,
			CommitHash: "c2",
			Timestamp:  ts.Add(-5 * time.Hour),
			Components: []string{"PaymentService"},
		},
	}

	if err := builder.ProcessEvents(events); err != nil {
		t.Fatalf("ProcessEvents failed: %v", err)
	}

	return store
}

func TestQueryMemory(t *testing.T) {
	store := setupTestStore(t)
	defer os.RemoveAll(store.dir)

	// Query for 'redis'
	res := QueryMemory(store, "redis")
	if len(res.Components) != 1 || res.Components[0].Name != "RedisCache" {
		t.Errorf("Expected to find RedisCache component, got %v", res.Components)
	}

	// Query for non-existent
	res2 := QueryMemory(store, "unknown")
	if len(res2.Components) != 0 {
		t.Errorf("Expected 0 components, got %d", len(res2.Components))
	}
}

func TestGetComponentTimeline(t *testing.T) {
	store := setupTestStore(t)
	defer os.RemoveAll(store.dir)

	entries := GetComponentTimeline(store, "rediscache")
	if len(entries) != 1 {
		t.Fatalf("Expected 1 entry, got %d", len(entries))
	}

	if entries[0].CommitHash != "c1" {
		t.Errorf("Expected hash c1, got %s", entries[0].CommitHash)
	}
}
