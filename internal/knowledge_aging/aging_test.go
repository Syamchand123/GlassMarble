package knowledge_aging

import (
	"testing"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/archmodel"
	"github.com/Syamchand123/GlassMarble/internal/developer_memory"
	"github.com/Syamchand123/GlassMarble/internal/evidence"
)

func TestAger(t *testing.T) {
	now := time.Now()

	mem := &developer_memory.DeveloperMemory{
		GlobalMemory: []developer_memory.KnowledgeClaim{
			{ID: "g1", ValidFrom: now.Add(-365 * 24 * time.Hour), Evidence: evidence.Bundle{PrimarySource: evidence.SourceCode}}, // Code decay -> 0.5
		},
		ComponentMemory: map[string]developer_memory.ComponentHistory{
			"CacheModule": {
				State: developer_memory.StateActive,
				Claims: []developer_memory.KnowledgeClaim{
					{ID: "c1", ValidFrom: now.Add(-90 * 24 * time.Hour), Evidence: evidence.Bundle{PrimarySource: evidence.SourceLLM}}, // LLM decay -> 0.5
				},
			},
			"OldModule": {
				State: developer_memory.StateActive,
			},
			"ExpModule": {
				State:  developer_memory.StateExperimental,
				Events: []string{"e1", "e2", "e3"}, // Promoted
			},
			"DepModule": {
				State: developer_memory.StateDeprecated,
				LastSeen: now.Add(-200 * 24 * time.Hour), // Historical
			},
		},
	}

	snap := &archmodel.ArchSnapshot{
		Patterns: []archmodel.DetectedPattern{
			{Components: []string{"CacheModule"}}, // Ref keeps it Deprecated, not Removed
		},
	}

	ager := NewAger(mem)
	transitions := ager.Age(snap, now)

	// 1. Verify freshness
	if mem.GlobalMemory[0].FreshnessScore != 0.5 {
		t.Errorf("Expected GlobalMemory score to be 0.5, got %f", mem.GlobalMemory[0].FreshnessScore)
	}
	if mem.ComponentMemory["CacheModule"].Claims[0].FreshnessScore != 0.5 {
		t.Errorf("Expected Component score to be 0.5, got %f", mem.ComponentMemory["CacheModule"].Claims[0].FreshnessScore)
	}

	// 2. Verify transitions
	if len(transitions) != 4 {
		t.Fatalf("Expected 4 transitions, got %d", len(transitions))
	}

	expectedTransitions := map[string]developer_memory.KnowledgeState{
		"CacheModule": developer_memory.StateDeprecated,
		"OldModule":   developer_memory.StateRemoved,
		"ExpModule":   developer_memory.StateActive,
		"DepModule":   developer_memory.KnowledgeState("HISTORICAL"),
	}

	for _, tr := range transitions {
		expected, ok := expectedTransitions[tr.Component]
		if !ok {
			t.Errorf("Unexpected transition for %s", tr.Component)
		} else if tr.NewState != expected {
			t.Errorf("Expected %s -> %s, got %s", tr.Component, expected, tr.NewState)
		}
	}
}
