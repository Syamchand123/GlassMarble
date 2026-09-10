package invalidator

import (
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/doc_engine/config"
)

func TestDirtyQueuePriorityAndBudget(t *testing.T) {
	refs := []config.DirtySectionRef{
		{DocID: "mod", SectionID: "interface"},
		{DocID: "sec", SectionID: "threat-model"},
		{DocID: "cfg", SectionID: "env-vars"},
	}
	q := NewDirtyQueue(refs)
	q.AssignPriorities()
	q.Sort()
	items := q.Items()
	if len(items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(items))
	}
	// Security first, config last.
	if items[0].DocID != "sec" {
		t.Errorf("expected security section first, got %+v", items[0])
	}
	if items[2].DocID != "cfg" {
		t.Errorf("expected config section last, got %+v", items[2])
	}

	q.EnforceBudget(2)
	if q.Len() != 2 {
		t.Fatalf("expected budget truncation to 2, got %d", q.Len())
	}
	if q.Items()[0].DocID != "sec" {
		t.Errorf("budget must keep highest priority, got %+v", q.Items()[0])
	}
}
