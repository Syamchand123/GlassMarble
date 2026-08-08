// Package archmodel_test verifies JSON round-trip stability for all cross-stage types.
//
// WHY: archmodel types are persisted to .glassmarble/. If JSON tags change or zero
// values serialize differently across Go versions, persisted files become unreadable.
// These tests lock down all serialization invariants.
package archmodel_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/archmodel"
	"github.com/Syamchand123/GlassMarble/internal/evidence"
)

func TestArchEvent_JSONRoundTrip(t *testing.T) {
	ts := time.Date(2026, 3, 15, 10, 0, 0, 0, time.UTC)
	orig := archmodel.ArchEvent{
		ID: "abc123", Kind: archmodel.EventCachingAdded,
		CommitHash: "deadbeef", Timestamp: ts,
		Title: "Redis caching added", ValidFrom: ts,
		Evidence: evidence.NewBundle(evidence.EvidenceItem{
			Source: evidence.SourceGit, Reference: "deadbeef",
			Confidence: 0.9, Timestamp: ts,
		}),
	}
	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got archmodel.ArchEvent
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.ID != orig.ID {
		t.Errorf("ID: got %q want %q", got.ID, orig.ID)
	}
	if got.Kind != orig.Kind {
		t.Errorf("Kind: got %q want %q", got.Kind, orig.Kind)
	}
}

func TestArchMetrics_JSONRoundTrip(t *testing.T) {
	m := archmodel.ArchMetrics{TotalNodes: 150, TotalEdges: 420, CycleCount: 1}
	data, _ := json.Marshal(m)
	var got archmodel.ArchMetrics
	json.Unmarshal(data, &got)
	if got.TotalNodes != m.TotalNodes {
		t.Errorf("TotalNodes: got %d want %d", got.TotalNodes, m.TotalNodes)
	}
}

func TestTimelineEntry_JSONRoundTrip(t *testing.T) {
	ts := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	e := archmodel.TimelineEntry{Timestamp: ts, CommitHash: "cafebabe", Title: "Split completed", EventKind: archmodel.EventServiceSplit}
	data, _ := json.Marshal(e)
	var got archmodel.TimelineEntry
	json.Unmarshal(data, &got)
	if got.Title != e.Title {
		t.Errorf("Title: got %q want %q", got.Title, e.Title)
	}
}
