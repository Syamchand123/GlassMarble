// Package stages_test exercises the GlassMarble analysis pipeline phases 1-4
// and their storage backends directly (no CLI). Every test builds its own
// sandbox via harness.NewSandbox and calls the phase APIs in-process.
package stages_test

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/arch_intelligence"
	"github.com/Syamchand123/GlassMarble/internal/arch_timeline"
	"github.com/Syamchand123/GlassMarble/internal/archmodel"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/ingest"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/normalize"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/aggregate"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/link"
	"github.com/Syamchand123/GlassMarble/internal/developer_memory"
	"github.com/Syamchand123/GlassMarble/internal/evidence"
	"github.com/Syamchand123/GlassMarble/internal/learning"
	"github.com/Syamchand123/GlassMarble/tests/harness"
)

// slashed renders a string with forward slashes so golden matches hold on
// every platform (phase outputs use native separators on Windows).
func slashed(s string) string {
	return strings.ReplaceAll(s, "\\", "/")
}

// lookupPath finds a map entry by its slash-normalized key. Phase outputs
// key maps by RelPath, which is native-separated on Windows ("cmd\api\main.go")
// while tests reason in forward-slash form.
func lookupPath[V any](m map[string]V, want string) (V, bool) {
	if v, ok := m[want]; ok {
		return v, true
	}
	for k, v := range m {
		if filepath.ToSlash(k) == want {
			return v, true
		}
	}
	var zero V
	return zero, false
}

// newSampleSandbox builds a sandbox pre-populated with the canonical sample
// project fixture (Go API + service + repo + cache, Python, JS, vendored and
// generated files, hidden dir, oversized doc).
func newSampleSandbox(t *testing.T) *harness.Sandbox {
	t.Helper()
	sb := harness.NewSandbox(t)
	sb.SampleProject()
	return sb
}

// runAnalysisPipeline executes the ingest-to-aggregate pipeline over the sandbox contents and
// returns every intermediate output plus the list of ingested file paths
// (suitable as phase 4 modifiedFiles).
func runAnalysisPipeline(t *testing.T, sb *harness.Sandbox, commitHash string) (*ingest.IngestOutput, *normalize.NormalizeOutput, *aggregate.AggregateOutput, []string) {
	t.Helper()
	out, err := ingest.RunIngestion(ingest.DefaultConfig(sb.Root))
	if err != nil {
		t.Fatalf("ingest.RunIngestion: %v", err)
	}
	payload, err := normalize.Normalize(out, commitHash)
	if err != nil {
		t.Fatalf("normalize.Normalize: %v", err)
	}
	agg, err := aggregate.Aggregate(payload, nil, sb.Root)
	if err != nil {
		t.Fatalf("aggregate.Aggregate: %v", err)
	}
	modified := make([]string, 0, len(out.Updated))
	for _, res := range out.Updated {
		modified = append(modified, res.RelPath)
	}
	return out, payload, agg, modified
}

// runPipeline executes the full ingest-to-link pipeline over a fresh sample sandbox
// at the requested level of detail and returns the linked CPG delta.
func runPipeline(t *testing.T, sb *harness.Sandbox, commitHash, level string) *link.LinkOutput {
	t.Helper()
	_, _, agg, modified := runAnalysisPipeline(t, sb, commitHash)
	linked, err := link.Link(agg, modified, akg.NewCodePropertyGraph(commitHash), link.LinkerConfig{LevelOfDetail: level})
	if err != nil {
		t.Fatalf("link.Link(%q): %v", level, err)
	}
	return linked
}

// TestC4_1PatternFeedbackChainAlive asserts the C4-1 feedback chain is alive:
// GenerateEvents must populate Components[0] with the pattern name so that
// learner.PatternFeedback (which requires Components[0]) can resolve
// ACCEPT/REJECT corrections. This is the GenerateEvents -> developer_memory ->
// PatternFeedback integration that was dead before the fix (Components was nil).
func TestC4_1PatternFeedbackChainAlive(t *testing.T) {
	now := time.Now().UTC()
	base := &archmodel.ArchSnapshot{
		ID:         "snap_base",
		CommitHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Timestamp:  now.Add(-time.Hour),
		Components: []archmodel.DetectedComponent{{ID: "comp1", Name: "comp1"}},
		Patterns:   []archmodel.DetectedPattern{},
		Metrics:    archmodel.ArchMetrics{TotalNodes: 1},
	}
	head := &archmodel.ArchSnapshot{
		ID:         "snap_head",
		CommitHash: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		Timestamp:  now,
		Components: []archmodel.DetectedComponent{{ID: "comp1", Name: "comp1"}},
		Patterns: []archmodel.DetectedPattern{
			{Kind: archmodel.PatternCleanArchitecture, Name: "Clean", Confidence: 0.9, Evidence: evidence.NewBundle(evidence.EvidenceItem{Source: evidence.SourceGit, Reference: "PR-01", Confidence: 0.9, Timestamp: now})},
		},
		Metrics: archmodel.ArchMetrics{TotalNodes: 1},
	}
	meta := arch_intelligence.CommitMeta{Hash: head.CommitHash, Timestamp: now}
	events := arch_intelligence.GenerateEvents(base, head, nil, meta)
	var patEv *archmodel.ArchEvent
	for i := range events {
		if events[i].Kind == archmodel.EventPatternDetected {
			patEv = &events[i]
			break
		}
	}
	if patEv == nil {
		t.Fatalf("GenerateEvents produced no PATTERN_DETECTED event: %+v", events)
	}
	if len(patEv.Components) == 0 || patEv.Components[0] != string(archmodel.PatternCleanArchitecture) {
		t.Fatalf("PATTERN_DETECTED Components = %v, want [%q]", patEv.Components, archmodel.PatternCleanArchitecture)
	}
	if len(patEv.AffectedIDs) == 0 || patEv.AffectedIDs[0] != string(archmodel.PatternCleanArchitecture) {
		t.Fatalf("PATTERN_DETECTED AffectedIDs = %v, want [%q]", patEv.AffectedIDs, archmodel.PatternCleanArchitecture)
	}
	mem := &developer_memory.DeveloperMemory{
		Events:      events,
		TotalEvents: len(events),
	}
	sb := harness.NewSandbox(t)
	learner := learning.NewLearnerForRepo(sb.Root)
	if _, err := learner.Correct(learning.Correction{Kind: learning.CorrectionKindReject, TargetID: patEv.ID, Reason: "test reject"}, mem); err != nil {
		t.Fatalf("Correct REJECT: %v", err)
	}
	_, rejected, err := learner.PatternFeedback(mem)
	if err != nil {
		t.Fatalf("PatternFeedback: %v", err)
	}
	found := false
	for _, r := range rejected {
		if r == string(archmodel.PatternCleanArchitecture) {
			found = true
		}
	}
	if !found {
		t.Errorf("PatternFeedback rejected = %v, want to contain %q (chain was dead before fix)", rejected, archmodel.PatternCleanArchitecture)
	}
	if _, err := learner.Correct(learning.Correction{Kind: learning.CorrectionKindAccept, TargetID: patEv.ID}, mem); err != nil {
		t.Fatalf("Correct ACCEPT: %v", err)
	}
	preferred, _, err := learner.PatternFeedback(mem)
	if err != nil {
		t.Fatalf("PatternFeedback after ACCEPT: %v", err)
	}
	foundPref := false
	for _, p := range preferred {
		if p == string(archmodel.PatternCleanArchitecture) {
			foundPref = true
		}
	}
	if !foundPref {
		t.Errorf("PatternFeedback preferred = %v, want to contain %q", preferred, archmodel.PatternCleanArchitecture)
	}
}

// TestC4_2TopologyHashIncludesNodes asserts computeTopologyHash is not edge-only
// (C4-2): two snapshots whose edge sets are identical but whose node sets differ
// (isolated node added) must not share the same TopologyHash, otherwise the
// snapshot store would skip the second write and lose history. The fix made the
// hash include sorted node IDs.
func TestC4_2TopologyHashIncludesNodes(t *testing.T) {
	now := time.Now().UTC()
	buildGraph := func(extraNode bool) *akg.CodePropertyGraph {
		g := akg.NewCodePropertyGraph("topo-test")
		g.Nodes = g.Nodes.Set("a.go::A", &link.ResolvedNode{ID: "a.go::A", Kind: "STRUCT", Name: "A", FileSpec: link.LocationMeta{Path: "a.go", LineStart: 1}})
		g.Nodes = g.Nodes.Set("b.go::B", &link.ResolvedNode{ID: "b.go::B", Kind: "STRUCT", Name: "B", FileSpec: link.LocationMeta{Path: "b.go", LineStart: 1}})
		if extraNode {
			g.Nodes = g.Nodes.Set("isolated.go::Iso", &link.ResolvedNode{ID: "isolated.go::Iso", Kind: "STRUCT", Name: "Iso", FileSpec: link.LocationMeta{Path: "isolated.go", LineStart: 1}})
		}
		g.OutboundEdges = g.OutboundEdges.Set("a.go::A", []link.ResolvedEdge{{SourceID: "a.go::A", TargetID: "b.go::B", Type: link.EdgeCalls}})
		g.InboundEdges = g.InboundEdges.Set("b.go::B", []link.ResolvedEdge{{SourceID: "a.go::A", TargetID: "b.go::B", Type: link.EdgeCalls}})
		return g
	}
	mkSnap := func(g *akg.CodePropertyGraph, commit string, ts time.Time) *archmodel.ArchSnapshot {
		ev := evidence.NewBundle(evidence.EvidenceItem{Source: evidence.SourceGit, Reference: "test", Confidence: 0.9, Timestamp: ts})
		snap, err := arch_timeline.BuildSnapshot(arch_timeline.SnapshotInput{
			Graph:      g,
			CommitHash: commit,
			Version:    "1",
			Timestamp:  ts,
			Components: []archmodel.DetectedComponent{{ID: "c1", Name: "c1", Evidence: ev}},
			Metrics:    archmodel.ArchMetrics{TotalNodes: g.Nodes.Len(), TotalEdges: 1},
		})
		if err != nil {
			t.Fatalf("BuildSnapshot: %v", err)
		}
		return snap
	}
	g1 := buildGraph(false)
	g2 := buildGraph(true)
	s1 := mkSnap(g1, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", now)
	s2 := mkSnap(g2, "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", now.Add(time.Second))
	sb := harness.NewSandbox(t)
	store, err := arch_timeline.NewSnapshotStore(sb.Path(".glassmarble", "snapshots"))
	if err != nil {
		t.Fatalf("NewSnapshotStore: %v", err)
	}
	ok, err := store.Create(s1)
	if err != nil || !ok {
		t.Fatalf("Create s1 = %v, err %v, want true", ok, err)
	}
	ok2, err := store.Create(s2)
	if err != nil {
		t.Fatalf("Create s2 err: %v", err)
	}
	if !ok2 {
		t.Errorf("Create s2 was skip-written despite different topology (node-only change lost) — C4-2 not fixed")
	}
	if s1.TopologyHash == s2.TopologyHash {
		t.Errorf("TopologyHash collision: isolated node did not change hash (edge-only hash bug C4-2): %q", s1.TopologyHash)
	}
	if s1.TopologyHash == "" || s2.TopologyHash == "" {
		t.Errorf("TopologyHash still empty after Create: %q %q", s1.TopologyHash, s2.TopologyHash)
	}
}
