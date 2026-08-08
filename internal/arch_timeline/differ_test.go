package arch_timeline

import (
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/archmodel"
)

func TestDiff(t *testing.T) {
	base := &archmodel.ArchSnapshot{
		ID: "snap-1",
		Components: []archmodel.DetectedComponent{
			{Name: "auth"},
			{Name: "payment"},
		},
		Patterns: []archmodel.DetectedPattern{
			{Kind: "CQRS"},
		},
		Metrics: archmodel.ArchMetrics{
			GraphDensity: 0.1,
			CycleCount:   0,
		},
	}

	head := &archmodel.ArchSnapshot{
		ID: "snap-2",
		Components: []archmodel.DetectedComponent{
			{Name: "auth"},
			{Name: "inventory"},
		},
		Patterns: []archmodel.DetectedPattern{
			{Kind: "EventDriven"},
		},
		Metrics: archmodel.ArchMetrics{
			GraphDensity: 0.2,
			CycleCount:   2,
		},
	}

	delta := Diff(base, head)

	if len(delta.AddedComponents) != 1 || delta.AddedComponents[0] != "inventory" {
		t.Errorf("Expected added component 'inventory'")
	}
	if len(delta.RemovedComponents) != 1 || delta.RemovedComponents[0] != "payment" {
		t.Errorf("Expected removed component 'payment'")
	}
	if len(delta.PatternChanges) != 2 {
		t.Errorf("Expected 2 pattern changes, got %d", len(delta.PatternChanges))
	}

	if delta.MetricDelta.CycleDelta != 2 {
		t.Errorf("Expected CycleDelta=2, got %d", delta.MetricDelta.CycleDelta)
	}
}
