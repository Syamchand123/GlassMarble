package arch_timeline

import (
	"reflect"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/archmodel"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage4"
)

func diffSnapshot(comps []archmodel.DetectedComponent, pats []archmodel.DetectedPattern, smells []archmodel.ArchSmell, m archmodel.ArchMetrics, id string) *archmodel.ArchSnapshot {
	return &archmodel.ArchSnapshot{
		ID:         id,
		Components: comps,
		Patterns:   pats,
		Smells:     smells,
		Metrics:    m,
	}
}

func TestDiff_AddedRemovedPatternsSmells(t *testing.T) {
	base := diffSnapshot(
		[]archmodel.DetectedComponent{{ID: "1", Name: "auth"}, {ID: "2", Name: "payment"}},
		[]archmodel.DetectedPattern{{Kind: "CQRS", Name: "cqrs", Components: []string{"1"}}},
		[]archmodel.ArchSmell{{Kind: archmodel.SmellGodObject, Title: "god", AffectedIDs: []string{"2"}}},
		archmodel.ArchMetrics{GraphDensity: 0.1, CycleCount: 0},
		"snap-1",
	)
	head := diffSnapshot(
		[]archmodel.DetectedComponent{{ID: "1", Name: "auth"}, {ID: "3", Name: "inventory"}},
		[]archmodel.DetectedPattern{{Kind: "EventDriven", Name: "ed", Components: []string{"3"}}},
		[]archmodel.ArchSmell{{Kind: archmodel.SmellGodObject, Title: "god", AffectedIDs: []string{"1"}}},
		archmodel.ArchMetrics{GraphDensity: 0.2, CycleCount: 2, AvgFanIn: 5},
		"snap-2",
	)

	res := Diff(base, head)
	if res == nil {
		t.Fatal("Diff returned nil")
	}
	delta := res.Delta
	if delta.BaseSnapshot != "snap-1" || delta.HeadSnapshot != "snap-2" {
		t.Errorf("snapshot ids: %+v", delta)
	}
	if !reflect.DeepEqual(delta.AddedComponents, []string{"inventory"}) {
		t.Errorf("added = %v, want [inventory]", delta.AddedComponents)
	}
	if !reflect.DeepEqual(delta.RemovedComponents, []string{"payment"}) {
		t.Errorf("removed = %v, want [payment]", delta.RemovedComponents)
	}
	if len(delta.PatternChanges) != 2 {
		t.Errorf("pattern changes = %d, want 2", len(delta.PatternChanges))
	}
	if len(delta.SmellChanges) != 2 {
		// Same kind+title but a different affected component → the old
		// instance resolved and a new one was introduced.
		t.Errorf("smell changes = %d, want 2", len(delta.SmellChanges))
	}
	if delta.MetricDelta.CycleDelta != 2 || delta.MetricDelta.DensityDelta != 0.1 {
		t.Errorf("metric deltas: %+v", delta.MetricDelta)
	}
	if delta.MetricDelta.CouplingTrend != "INCREASING" {
		t.Errorf("coupling trend = %q, want INCREASING", delta.MetricDelta.CouplingTrend)
	}
}

func TestDiff_NilSafety(t *testing.T) {
	if res := Diff(nil, nil); res != nil {
		t.Error("Diff(nil, nil) must return nil")
	}
	if res := Diff(nil, &archmodel.ArchSnapshot{}); res != nil {
		t.Error("Diff(nil, head) must return nil")
	}
	if res := Diff(&archmodel.ArchSnapshot{}, nil); res != nil {
		t.Error("Diff(base, nil) must return nil")
	}
}

func TestDiff_CohesionTrend(t *testing.T) {
	base := diffSnapshot(nil, nil, nil, archmodel.ArchMetrics{LCOM4: 4}, "b")
	head := diffSnapshot(nil, nil, nil, archmodel.ArchMetrics{LCOM4: 2}, "h")
	if got := Diff(base, head).Delta.MetricDelta.CohesionTrend; got != "IMPROVING" {
		t.Errorf("LCOM4 down → cohesion IMPROVING, got %q", got)
	}
	head.Metrics.LCOM4 = 7
	if got := Diff(base, head).Delta.MetricDelta.CohesionTrend; got != "DEGRADING" {
		t.Errorf("LCOM4 up → cohesion DEGRADING, got %q", got)
	}
	head.Metrics.LCOM4 = 4
	if got := Diff(base, head).Delta.MetricDelta.CohesionTrend; got != "STABLE" {
		t.Errorf("LCOM4 equal → cohesion STABLE, got %q", got)
	}
}

func TestDiff_SummaryLine(t *testing.T) {
	res := Diff(
		diffSnapshot(
			[]archmodel.DetectedComponent{{ID: "1", Name: "auth"}, {ID: "2", Name: "old"}},
			nil, nil, archmodel.ArchMetrics{}, "b"),
		diffSnapshot(
			[]archmodel.DetectedComponent{{ID: "1", Name: "auth"}, {ID: "3", Name: "new"}},
			nil, nil, archmodel.ArchMetrics{}, "h"),
	)
	line := res.Delta.MetricDelta.SummaryLine
	for _, want := range []string{"+1/-1", "cycles", "coupling", "cohesion"} {
		if !contains(line, want) {
			t.Errorf("summary line %q missing %q", line, want)
		}
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// TestDiff_RenameDetection: a component whose ID changed but whose nodes are
// largely shared must be reported as a rename, not as add+remove.
func TestDiff_RenameDetection(t *testing.T) {
	base := diffSnapshot(
		[]archmodel.DetectedComponent{{
			ID: "old-id", Name: "PaymentService", Kind: archmodel.ComponentService,
			NodeIDs: []string{"n1", "n2", "n3", "n4"},
		}},
		nil, nil, archmodel.ArchMetrics{}, "b")
	head := diffSnapshot(
		[]archmodel.DetectedComponent{{
			ID: "new-id", Name: "PaymentsService", Kind: archmodel.ComponentService,
			NodeIDs: []string{"n1", "n2", "n3", "n4"},
		}},
		nil, nil, archmodel.ArchMetrics{}, "h")

	res := Diff(base, head)
	if len(res.Renames) != 1 {
		t.Fatalf("renames = %d, want 1 (got %+v)", len(res.Renames), res.Renames)
	}
	r := res.Renames[0]
	if r.OldName != "PaymentService" || r.NewName != "PaymentsService" {
		t.Errorf("rename = %s→%s", r.OldName, r.NewName)
	}
	if r.NodeOverlap != 4 {
		t.Errorf("node overlap = %d, want 4", r.NodeOverlap)
	}
	// Renamed components must not appear in the raw add/remove lists.
	if len(res.Delta.AddedComponents) != 0 || len(res.Delta.RemovedComponents) != 0 {
		t.Errorf("rename leaked into membership delta: added=%v removed=%v", res.Delta.AddedComponents, res.Delta.RemovedComponents)
	}
}

func TestDiff_SplitAndMerge(t *testing.T) {
	// Split: billing (6 nodes) → billing-core (3) + billing-web (3).
	base := diffSnapshot(
		[]archmodel.DetectedComponent{{
			ID: "b", Name: "billing", NodeIDs: []string{"n1", "n2", "n3", "n4", "n5", "n6"},
		}},
		nil, nil, archmodel.ArchMetrics{}, "b")
	head := diffSnapshot(
		[]archmodel.DetectedComponent{
			{ID: "c", Name: "billing-core", NodeIDs: []string{"n1", "n2", "n3"}},
			{ID: "w", Name: "billing-web", NodeIDs: []string{"n4", "n5", "n6"}},
			{ID: "x", Name: "other", NodeIDs: []string{"n9", "n10"}},
		},
		nil, nil, archmodel.ArchMetrics{}, "h")

	res := Diff(base, head)
	if len(res.Splits) != 1 {
		t.Fatalf("splits = %d, want 1 (got %+v)", len(res.Splits), res.Splits)
	}
	sp := res.Splits[0]
	if sp.Source != "billing" {
		t.Errorf("split source = %q, want billing", sp.Source)
	}
	if !reflect.DeepEqual(sp.Targets, []string{"billing-core", "billing-web"}) {
		t.Errorf("split targets = %v", sp.Targets)
	}
	// "other" shares no nodes → must not join the split.
	if len(res.Delta.AddedComponents) != 1 || res.Delta.AddedComponents[0] != "other" {
		t.Errorf("unrelated added component should stay a plain addition: %v", res.Delta.AddedComponents)
	}

	// Merge: the inverse.
	res2 := Diff(head, base)
	if len(res2.Merges) != 1 {
		t.Fatalf("merges = %d, want 1 (got %+v)", len(res2.Merges), res2.Merges)
	}
	mg := res2.Merges[0]
	if mg.Target != "billing" {
		t.Errorf("merge target = %q, want billing", mg.Target)
	}
	if !reflect.DeepEqual(mg.Sources, []string{"billing-core", "billing-web"}) {
		t.Errorf("merge sources = %v", mg.Sources)
	}
}

func TestDiff_DependencyChanges(t *testing.T) {
	base := diffSnapshot(
		[]archmodel.DetectedComponent{
			{ID: "1", Name: "auth", Dependencies: []string{"2"}},
			{ID: "2", Name: "db"},
		},
		nil, nil, archmodel.ArchMetrics{}, "b")
	head := diffSnapshot(
		[]archmodel.DetectedComponent{
			{ID: "1", Name: "auth", Dependencies: []string{"2", "3"}},
			{ID: "2", Name: "db"},
			{ID: "3", Name: "cache"},
		},
		nil, nil, archmodel.ArchMetrics{}, "h")

	res := Diff(base, head)
	var added, removed []DependencyChange
	for _, d := range res.DependencyChanges {
		if d.Added {
			added = append(added, d)
		} else {
			removed = append(removed, d)
		}
	}
	if len(added) != 1 || added[0].Source != "1" || added[0].Target != "3" {
		t.Errorf("added deps = %+v, want auth→cache", added)
	}
	if len(removed) != 0 {
		t.Errorf("removed deps = %+v, want none", removed)
	}
}

// TestDiff_StructuralGraphDiff: with both graphs embedded, Diff must return a
// real structural diff; with either side missing (--no-graph), it must be nil.
func TestDiff_StructuralGraphDiff(t *testing.T) {
	baseGraph := akg.NewCodePropertyGraph("base")
	baseGraph.Nodes = baseGraph.Nodes.Set("a", &stage4.ResolvedNode{ID: "a", Kind: "FUNCTION", Name: "A"})
	headGraph := akg.NewCodePropertyGraph("head")
	headGraph.Nodes = headGraph.Nodes.Set("a", &stage4.ResolvedNode{ID: "a", Kind: "FUNCTION", Name: "A"})
	headGraph.Nodes = headGraph.Nodes.Set("b", &stage4.ResolvedNode{ID: "b", Kind: "FUNCTION", Name: "B"})

	base, err := BuildSnapshot(SnapshotInput{Graph: baseGraph, CommitHash: "b", Timestamp: snapBaseTime})
	if err != nil {
		t.Fatalf("BuildSnapshot(base): %v", err)
	}
	head, err := BuildSnapshot(SnapshotInput{Graph: headGraph, CommitHash: "h", Timestamp: snapBaseTime})
	if err != nil {
		t.Fatalf("BuildSnapshot(head): %v", err)
	}

	res := Diff(base, head)
	if res.Graph == nil {
		t.Fatal("structural diff missing when both graphs are embedded")
	}
	if len(res.Graph.NodesAdded) != 1 || res.Graph.NodesAdded[0].ID != "b" {
		t.Errorf("nodes added = %+v, want [b]", res.Graph.NodesAdded)
	}

	// Missing graph on one side → no structural diff.
	noGraph := SnapshotInput{Graph: headGraph, CommitHash: "h", Timestamp: snapBaseTime, NoGraph: true}
	headNG, err := BuildSnapshot(noGraph)
	if err != nil {
		t.Fatalf("BuildSnapshot(no-graph): %v", err)
	}
	if res := Diff(base, headNG); res.Graph != nil {
		t.Error("structural diff must be nil when one snapshot has no graph")
	}
}

// TestDiff_Deterministic: diffing the same pair twice must produce identical
// output (slices sorted, map iteration order never leaks).
func TestDiff_Deterministic(t *testing.T) {
	base := diffSnapshot(
		[]archmodel.DetectedComponent{{ID: "z", Name: "zeta"}, {ID: "a", Name: "alpha"}, {ID: "q", Name: "queen"}},
		[]archmodel.DetectedPattern{{Kind: "CQRS", Name: "cqrs"}, {Kind: "DDD", Name: "ddd"}},
		[]archmodel.ArchSmell{{Kind: archmodel.SmellGodObject, Title: "god"}, {Kind: archmodel.SmellTightCoupling, Title: "tight"}},
		archmodel.ArchMetrics{},
		"b")
	head := diffSnapshot(
		[]archmodel.DetectedComponent{{ID: "z", Name: "zeta"}, {ID: "b", Name: "beta"}},
		[]archmodel.DetectedPattern{{Kind: "CQRS", Name: "cqrs"}},
		[]archmodel.ArchSmell{{Kind: archmodel.SmellGodObject, Title: "god"}},
		archmodel.ArchMetrics{},
		"h")

	a, b := Diff(base, head), Diff(base, head)
	if !reflect.DeepEqual(a, b) {
		t.Error("Diff must be deterministic across calls")
	}
	if !reflect.DeepEqual(a.Delta.AddedComponents, []string{"beta"}) {
		t.Errorf("added = %v", a.Delta.AddedComponents)
	}
	if !reflect.DeepEqual(a.Delta.RemovedComponents, []string{"alpha", "queen"}) {
		t.Errorf("removed = %v (must be sorted)", a.Delta.RemovedComponents)
	}
}

func TestDiff_ZeroValueBase(t *testing.T) {
	base := &archmodel.ArchSnapshot{ID: "empty"}
	head := &archmodel.ArchSnapshot{ID: "h", Components: []archmodel.DetectedComponent{{Name: "x"}}}
	res := Diff(base, head)
	if res == nil {
		t.Fatal("Diff with an empty base must not panic or return nil")
	}
	if len(res.Delta.AddedComponents) != 1 {
		t.Errorf("added = %v, want [x]", res.Delta.AddedComponents)
	}
}
