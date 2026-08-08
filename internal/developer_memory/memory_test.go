package developer_memory

import (
	"encoding/json"
	"testing"
	"time"
)

func TestDeveloperMemory_JSON(t *testing.T) {
	mem := DeveloperMemory{
		ProjectID:   "test-project",
		LastUpdated: time.Now().Truncate(time.Millisecond),
		TotalEvents: 1,
	}

	data, err := json.Marshal(mem)
	if err != nil {
		t.Fatalf("Failed to marshal DeveloperMemory: %v", err)
	}

	var decoded DeveloperMemory
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal DeveloperMemory: %v", err)
	}

	if decoded.ProjectID != mem.ProjectID {
		t.Errorf("Expected ProjectID %q, got %q", mem.ProjectID, decoded.ProjectID)
	}
}
