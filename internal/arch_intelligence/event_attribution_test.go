package arch_intelligence

import (
	"testing"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/archmodel"
)

func snapWith(commit string, comps ...string) *archmodel.ArchSnapshot {
	s := &archmodel.ArchSnapshot{CommitHash: commit, Timestamp: time.Now()}
	for _, c := range comps {
		s.Components = append(s.Components, archmodel.DetectedComponent{ID: c, Name: c})
	}
	return s
}

// TestGenerateEvents_RecordsTheBaselineItDiffedAgainst pins the attribution
// fix. Structural events come from diffing two snapshots, so when analysis has
// not run for several commits the diff spans all of them — yet every event
// carried only CommitHash, the commit analysis happened to run AT, presenting a
// range of work as one commit's doing. Re-analysing every commit to attribute
// precisely is not on the table; claiming precision that was never measured is
// the part that is fixable.
func TestGenerateEvents_RecordsTheBaselineItDiffedAgainst(t *testing.T) {
	base := snapWith("aaaa1111", "svc-a")
	head := snapWith("dddd4444", "svc-a", "svc-b")

	events := GenerateEvents(base, head, nil, CommitMeta{
		Hash:      "dddd4444",
		Timestamp: time.Now(),
	})
	if len(events) == 0 {
		t.Fatal("adding a component should generate at least one event")
	}
	for _, e := range events {
		if e.CommitHash != "dddd4444" {
			t.Errorf("CommitHash = %q, want the head commit", e.CommitHash)
		}
		if e.BaseCommitHash != "aaaa1111" {
			t.Errorf("BaseCommitHash = %q, want the baseline snapshot's commit — "+
				"without it a multi-commit span is indistinguishable from one commit",
				e.BaseCommitHash)
		}
	}
}

// TestGenerateEvents_FirstAnalysisHasNoBaseline: with no prior snapshot there
// is no range to describe, and the field stays empty rather than echoing head.
func TestGenerateEvents_FirstAnalysisHasNoBaseline(t *testing.T) {
	head := snapWith("dddd4444", "svc-a")
	for _, e := range GenerateEvents(nil, head, nil, CommitMeta{Hash: "dddd4444"}) {
		if e.BaseCommitHash != "" {
			t.Errorf("BaseCommitHash = %q, want empty on a first analysis", e.BaseCommitHash)
		}
	}
}

// TestGenerateEvents_ExplicitBaseHashWins: a caller that knows better than the
// snapshot may say so.
func TestGenerateEvents_ExplicitBaseHashWins(t *testing.T) {
	base := snapWith("aaaa1111", "svc-a")
	head := snapWith("dddd4444", "svc-a", "svc-b")

	events := GenerateEvents(base, head, nil, CommitMeta{
		Hash:     "dddd4444",
		BaseHash: "cccc3333",
	})
	if len(events) == 0 {
		t.Fatal("expected events")
	}
	for _, e := range events {
		if e.BaseCommitHash != "cccc3333" {
			t.Errorf("BaseCommitHash = %q, want the explicitly supplied cccc3333", e.BaseCommitHash)
		}
	}
}
