package arch_intelligence

import (
	"testing"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/archmodel"
)

func TestGenerateEvents(t *testing.T) {
	base := &archmodel.ArchSnapshot{
		Components: []archmodel.DetectedComponent{},
	}
	head := &archmodel.ArchSnapshot{
		Components: []archmodel.DetectedComponent{
			{Name: "c1"},
		},
	}
	diff := &akg.GraphDiff{}
	meta := CommitMeta{Hash: "abcdef", Timestamp: time.Now()}

	events := GenerateEvents(base, head, diff, meta)

	if len(events) != 1 {
		t.Errorf("Expected 1 event, got %d", len(events))
	}

	if len(events) > 0 && events[0].Kind != archmodel.EventServiceAdded {
		t.Errorf("Expected EventServiceAdded, got %v", events[0].Kind)
	}
}
