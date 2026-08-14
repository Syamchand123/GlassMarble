package stages_test

// architecture timeline event-model semantics tests (archmodel): state tags, timeline
// JSON round-trip, and snapshot building.
//
// Discrepancy from API_REFERENCE.md: archmodel.BuildSnapshot(graph, now)
// does not exist. Snapshot building lives in
// arch_timeline.BuildSnapshot(in SnapshotInput), which requires a non-zero
// timestamp, a graph, and validates evidence on components/patterns/smells.

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/arch_intelligence"
	"github.com/Syamchand123/GlassMarble/internal/arch_timeline"
	"github.com/Syamchand123/GlassMarble/internal/archmodel"
	"github.com/Syamchand123/GlassMarble/internal/evidence"
	"github.com/Syamchand123/GlassMarble/tests/harness"
)

func TestStateTags(t *testing.T) {
	if got := archmodel.StateTag("CURRENT"); got != "state=CURRENT" {
		t.Errorf("StateTag(CURRENT) = %q, want state=CURRENT", got)
	}
	if got := archmodel.StateFromTags([]string{"state=REMOVED"}); got != "REMOVED" {
		t.Errorf("StateFromTags(state=REMOVED) = %q, want REMOVED", got)
	}
	if got := archmodel.StateFromTags([]string{"state=REMOVED", "state=CURRENT"}); got != "REMOVED" {
		t.Errorf("StateFromTags(first tag wins) = %q, want REMOVED", got)
	}
	if got := archmodel.StateFromTags([]string{"unrelated", "state="}); got != "" {
		t.Errorf("StateFromTags(bare/unknown tags) = %q, want \"\"", got)
	}
}

func TestArchEventStateChangeTag(t *testing.T) {
	now := time.Now().UTC()
	b := evidence.Bundle{}
	b.Add(evidence.EvidenceItem{
		Source: evidence.SourceRule, Reference: "AGING", Confidence: 0.9, Timestamp: now,
	})
	ev := archmodel.ArchEvent{
		ID:         "evt_aging_1",
		Kind:       archmodel.EventStateChanged,
		CommitHash: "abcdef1234567890",
		Timestamp:  now,
		Title:      "cache deprecated",
		Components: []string{"cache"},
		Evidence:   b,
		Tags:       []string{archmodel.StateTag("DEPRECATED")},
	}
	if got := archmodel.StateFromTags(ev.Tags); got != "DEPRECATED" {
		t.Errorf("StateFromTags(StateTag(...)) = %q, want DEPRECATED", got)
	}
}

func TestTimelineEntryJSONRoundTrip(t *testing.T) {
	now := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	in := archmodel.TimelineEntry{
		Timestamp:   now,
		CommitHash:  "abcdef1234567890",
		Version:     "1",
		Title:       "add cache layer",
		Description: "introduced a cache",
		EventKind:   archmodel.EventCachingAdded,
		Components:  []string{"cache"},
		Intent:      "reduce latency",
		Tags:        []string{"state=CURRENT"},
	}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal timeline entry: %v", err)
	}
	var out archmodel.TimelineEntry
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal timeline entry: %v", err)
	}
	if !out.Timestamp.Equal(in.Timestamp) || out.CommitHash != in.CommitHash ||
		out.Version != in.Version || out.Title != in.Title || out.Description != in.Description ||
		out.EventKind != in.EventKind || len(out.Components) != 1 || out.Components[0] != "cache" ||
		out.Intent != in.Intent || len(out.Tags) != 1 || out.Tags[0] != in.Tags[0] {
		t.Errorf("round-trip mismatch:\n in: %+v\nout: %+v", in, out)
	}
}

func TestBuildSnapshotFromGraph(t *testing.T) {
	sb := harness.NewSandbox(t)
	graph := analyzeProject(t, sb)
	now := time.Date(2026, 5, 6, 7, 8, 9, 0, time.UTC)

	res := arch_intelligence.NewEngine(graph).Run()
	input := arch_timeline.SnapshotInput{
		Graph:      graph,
		CommitHash: "abcdef1234567890",
		Version:    "3",
		Timestamp:  now,
		Components: res.Components,
		Patterns:   res.Patterns,
		Smells:     res.Smells,
		Metrics:    res.Metrics,
	}

	snap, err := arch_timeline.BuildSnapshot(input)
	if err != nil {
		t.Fatalf("BuildSnapshot: %v", err)
	}
	if !strings.HasPrefix(snap.ID, "snap_") {
		t.Errorf("snapshot ID = %q, want snap_ prefix", snap.ID)
	}
	if snap.NodeCount != graph.Nodes.Len() || snap.NodeCount == 0 {
		t.Errorf("NodeCount = %d, graph nodes = %d", snap.NodeCount, graph.Nodes.Len())
	}
	if len(snap.Components) == 0 {
		t.Error("snapshot carries no components from the graph analysis")
	}
	if len(snap.AKGJSON) == 0 {
		t.Error("snapshot does not embed the graph (AKGJSON empty)")
	}
	if !snap.Timestamp.Equal(now) {
		t.Errorf("snapshot timestamp = %v, want %v", snap.Timestamp, now)
	}

	again, err := arch_timeline.BuildSnapshot(input)
	if err != nil {
		t.Fatalf("second BuildSnapshot: %v", err)
	}
	if again.ID != snap.ID {
		t.Errorf("snapshot ID not deterministic: %q vs %q", snap.ID, again.ID)
	}

	if _, err := arch_timeline.BuildSnapshot(arch_timeline.SnapshotInput{Graph: graph}); err == nil {
		t.Error("BuildSnapshot with zero timestamp must error")
	}
}
