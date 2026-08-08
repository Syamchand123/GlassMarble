package developer_memory

import (
	"os"
	"testing"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/archmodel"
)

func TestMemoryBuilder_ProcessEvents(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "builder_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	store := NewMemoryStore(tempDir)
	builder := NewMemoryBuilder(store)

	ts := time.Now()
	events := []archmodel.ArchEvent{
		{
			ID:         "event-1",
			Kind:       archmodel.EventServiceAdded,
			CommitHash: "c1",
			Timestamp:  ts,
			Components: []string{"PaymentService"},
		},
	}

	if err := builder.ProcessEvents(events); err != nil {
		t.Fatalf("ProcessEvents failed: %v", err)
	}

	mem, err := store.LoadMemory()
	if err != nil {
		t.Fatalf("LoadMemory failed: %v", err)
	}

	if mem.TotalEvents != 1 {
		t.Errorf("Expected TotalEvents=1, got %d", mem.TotalEvents)
	}

	if len(mem.ComponentMemory) != 1 {
		t.Errorf("Expected 1 component, got %d", len(mem.ComponentMemory))
	}

	comp, ok := mem.ComponentMemory["PaymentService"]
	if !ok {
		t.Errorf("Expected PaymentService to exist")
	} else {
		if comp.State != StateActive {
			t.Errorf("Expected StateActive, got %s", comp.State)
		}
	}

	if len(mem.Timeline) != 1 {
		t.Errorf("Expected 1 timeline entry, got %d", len(mem.Timeline))
	}
}
