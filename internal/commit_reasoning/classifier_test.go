package commit_reasoning

import (
	"testing"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/arch_intelligence"
	"github.com/Syamchand123/GlassMarble/internal/arch_timeline"
	"github.com/Syamchand123/GlassMarble/internal/archmodel"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage4"
	"github.com/Syamchand123/GlassMarble/internal/git"
)

var testTS = time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)

func testMeta() *git.CommitMeta {
	return &git.CommitMeta{
		Hash:      "deadbeefcafe0123456789abcdef0123456789abcd",
		Timestamp: testTS,
		Subject:   "wire up payment",
		Body:      "fixes #42",
	}
}

func testInput(extra func(*ClassifyInput)) ClassifyInput {
	in := ClassifyInput{Meta: testMeta()}
	if extra != nil {
		extra(&in)
	}
	return in
}

func comp(id, name string, deps ...string) archmodel.DetectedComponent {
	return archmodel.DetectedComponent{ID: id, Name: name, Dependencies: deps}
}

func TestClassifyComponentPass(t *testing.T) {
	in := testInput(func(in *ClassifyInput) {
		in.BaseSnap = &archmodel.ArchSnapshot{Components: []archmodel.DetectedComponent{comp("c1", "auth")}}
		in.HeadSnap = &archmodel.ArchSnapshot{
			Components: []archmodel.DetectedComponent{comp("c1", "auth"), comp("c2", "billing")},
		}
	})
	changes := ClassifyChange(in)

	var got []archmodel.EventKind
	for _, c := range changes {
		got = append(got, c.Kind)
	}
	if len(got) != 1 || got[0] != archmodel.EventServiceAdded {
		t.Fatalf("want exactly one SERVICE_ADDED, got %v", got)
	}
	added := changes[0]
	if len(added.AffectedIDs) != 1 || added.AffectedIDs[0] != "c2" {
		t.Errorf("AffectedIDs = %v, want [c2]", added.AffectedIDs)
	}
	if added.Evidence.IsEmpty() {
		t.Error("evidence must be non-empty")
	}
}

func TestClassifyComponentPass_RemovedAndSplitAndMerge(t *testing.T) {
	in := testInput(func(in *ClassifyInput) {
		in.BaseSnap = &archmodel.ArchSnapshot{
			Components: []archmodel.DetectedComponent{
				comp("b1", "monolith", "d1"),
				comp("gone", "gone"),
			},
		}
		in.HeadSnap = &archmodel.ArchSnapshot{
			Components: []archmodel.DetectedComponent{
				comp("h1", "billing-core"),
				comp("h2", "billing-web"),
				comp("h3", "merged"),
			},
		}
	})
	diff := arch_timeline.Diff(in.BaseSnap, in.HeadSnap)
	diff.Splits = append(diff.Splits, arch_timeline.ServiceSplit{Source: "monolith", Targets: []string{"billing-core", "billing-web"}})
	diff.Merges = append(diff.Merges, arch_timeline.ServiceMerge{Target: "merged", Sources: []string{"billing-core", "billing-web"}})
	// Remove the split/merge participants from the raw delta the way Diff does.
	diff.Delta.AddedComponents = nil
	diff.Delta.RemovedComponents = []string{"gone"}
	in.Diff = diff

	changes := ClassifyChange(in)
	kinds := make(map[archmodel.EventKind]int)
	for _, c := range changes {
		kinds[c.Kind]++
	}
	if kinds[archmodel.EventServiceRemoved] != 1 {
		t.Errorf("want 1 SERVICE_REMOVED, got %d", kinds[archmodel.EventServiceRemoved])
	}
	if kinds[archmodel.EventServiceSplit] != 1 {
		t.Errorf("want 1 SERVICE_SPLIT, got %d", kinds[archmodel.EventServiceSplit])
	}
	if kinds[archmodel.EventServiceMerged] != 1 {
		t.Errorf("want 1 SERVICE_MERGED, got %d", kinds[archmodel.EventServiceMerged])
	}
}

func TestClassifyDependencyPass_Matches5DEventID(t *testing.T) {
	base := &archmodel.ArchSnapshot{Components: []archmodel.DetectedComponent{comp("svc", "service")}}
	head := &archmodel.ArchSnapshot{Components: []archmodel.DetectedComponent{comp("svc", "service", "db")}}

	in := testInput(func(in *ClassifyInput) {
		in.BaseSnap = base
		in.HeadSnap = head
	})
	changes := ClassifyChange(in)

	// Stage 8's dependency event must produce the identical ID that the
	// Stage 5D generator would produce for the same commit.
	fiveD := arch_intelligence.GenerateEvents(base, head, nil, arch_intelligence.CommitMeta{Hash: testMeta().Hash, Timestamp: testTS})

	var stage8ID string
	for _, c := range changes {
		if c.Kind == archmodel.EventDependencyAdded {
			stage8ID = arch_intelligence.EventID(testMeta().Hash, c.Kind, c.AffectedIDs)
			if len(c.AffectedIDs) != 2 || c.AffectedIDs[0] != "svc" || c.AffectedIDs[1] != "db" {
				t.Errorf("affected = %v, want [svc db]", c.AffectedIDs)
			}
		}
	}
	if stage8ID == "" {
		t.Fatal("no DEPENDENCY_ADDED classified")
	}
	for _, ev := range fiveD {
		if ev.Kind == archmodel.EventDependencyAdded && ev.ID != stage8ID {
			t.Errorf("ID mismatch: stage8=%q stage5d=%q", stage8ID, ev.ID)
		}
	}
}

func TestClassifySmellPass_LayerViolation(t *testing.T) {
	in := testInput(func(in *ClassifyInput) {
		in.BaseSnap = &archmodel.ArchSnapshot{}
		in.HeadSnap = &archmodel.ArchSnapshot{
			Smells: []archmodel.ArchSmell{
				{Kind: archmodel.SmellLayerViolation, Title: "web -> db", AffectedIDs: []string{"n1", "n2"}},
			},
		}
	})
	changes := ClassifyChange(in)
	if len(changes) != 1 || changes[0].Kind != archmodel.EventLayerViolation {
		t.Fatalf("want one LAYER_VIOLATION, got %+v", changes)
	}
	if changes[0].AffectedIDs != nil {
		t.Errorf("layer violation affected must stay nil for 5D dedup, got %v", changes[0].AffectedIDs)
	}
}

func TestClassifySmellPass_DeduplicatesExistingSmells(t *testing.T) {
	s := archmodel.ArchSmell{Kind: archmodel.SmellLayerViolation, Title: "web -> db", AffectedIDs: []string{"n1", "n2"}}
	in := testInput(func(in *ClassifyInput) {
		in.BaseSnap = &archmodel.ArchSnapshot{Smells: []archmodel.ArchSmell{s}}
		in.HeadSnap = &archmodel.ArchSnapshot{Smells: []archmodel.ArchSmell{s}}
	})
	if changes := ClassifyChange(in); len(changes) != 0 {
		t.Fatalf("unchanged smells must not re-fire, got %+v", changes)
	}
}

func TestClassifyGraphPass(t *testing.T) {
	head := akg.NewCodePropertyGraph("head")
	head.Nodes = head.Nodes.Set("mod:pay", &stage4.ResolvedNode{ID: "mod:pay", Kind: "MODULE", Name: "PaymentModule", FileSpec: stage4.LocationMeta{Path: "internal/pay/mod.go"}})
	head.Nodes = head.Nodes.Set("db:postgres", &stage4.ResolvedNode{ID: "db:postgres", Kind: "DATABASE", Name: "Postgres", FileSpec: stage4.LocationMeta{Path: "db.go"}})
	head.Nodes = head.Nodes.Set("endpoint:GET:/pay", &stage4.ResolvedNode{ID: "endpoint:GET:/pay", Kind: "ENDPOINT", Name: "GET /pay", FileSpec: stage4.LocationMeta{Path: "api.go"}})
	head.Nodes = head.Nodes.Set("sink:SQL", &stage4.ResolvedNode{ID: "sink:SQL", Kind: "SINK", Name: "SQL sink", FileSpec: stage4.LocationMeta{Path: "api.go"}})
	head.Nodes = head.Nodes.Set("svc:pay", &stage4.ResolvedNode{ID: "svc:pay", Kind: "CLASS", Name: "PaymentService", FileSpec: stage4.LocationMeta{Path: "internal/pay/pay.go"}})
	head.Nodes = head.Nodes.Set("redis", &stage4.ResolvedNode{ID: "redis", Kind: "EXTERNAL", Name: "redis", FileSpec: stage4.LocationMeta{Path: "cache.go"}})

	in := testInput(func(in *ClassifyInput) {
		in.HeadGraph = head
		in.GraphDiff = &akg.GraphDiff{
			NodesAdded: []akg.DiffNode{{ID: "mod:pay", Kind: "MODULE", Name: "PaymentModule"}},
			EdgesAdded: []akg.DiffEdge{
				{Type: string(stage4.EdgeQueriesDB), SourceID: "svc:pay", TargetID: "db:postgres"},
				{Type: string(stage4.EdgePublishes), SourceID: "svc:pay", TargetID: "events:paid"},
				{Type: string(stage4.EdgeSubscribes), SourceID: "svc:pay", TargetID: "events:refunded"},
				{Type: string(stage4.EdgeDependsOn), SourceID: "svc:pay", TargetID: "redis"},
				{Type: string(stage4.EdgeExposesEndpoint), SourceID: "svc:pay", TargetID: "endpoint:GET:/pay"},
				{Type: string(stage4.EdgeSecuritySink), SourceID: "svc:pay", TargetID: "sink:SQL"},
			},
		}
	})

	changes := ClassifyChange(in)
	kinds := make(map[archmodel.EventKind]int)
	for _, c := range changes {
		kinds[c.Kind]++
	}
	want := map[archmodel.EventKind]int{
		archmodel.EventServiceAdded:    1, // mod:pay module (unowned)
		archmodel.EventDataStoreAdded:  1, // grouped per datastore target
		archmodel.EventAsyncIntroduced: 2, // publishes + subscribes pairs
		archmodel.EventCachingAdded:    1, // redis target
		archmodel.EventAPIAdded:        1, // endpoints per source
		archmodel.EventSecurityAdded:   1, // security sink per source
	}
	for k, n := range want {
		if kinds[k] != n {
			t.Errorf("%s: want %d, got %d", k, n, kinds[k])
		}
	}
}

func TestClassifyGraphPass_OwnedModulesAreSkipped(t *testing.T) {
	in := testInput(func(in *ClassifyInput) {
		in.HeadSnap = &archmodel.ArchSnapshot{
			Components: []archmodel.DetectedComponent{{ID: "pay", Name: "payment", NodeIDs: []string{"mod:pay"}}},
		}
		in.GraphDiff = &akg.GraphDiff{
			NodesAdded: []akg.DiffNode{{ID: "mod:pay", Kind: "MODULE", Name: "PaymentModule"}},
		}
	})
	if changes := ClassifyChange(in); len(changes) != 0 {
		t.Fatalf("owned module must not double-fire SERVICE_ADDED, got %+v", changes)
	}
}

func TestClassifyGraphPass_CacheWordBoundaries(t *testing.T) {
	in := testInput(func(in *ClassifyInput) {
		in.HeadGraph = akg.NewCodePropertyGraph("head")
		in.GraphDiff = &akg.GraphDiff{
			EdgesAdded: []akg.DiffEdge{
				{Type: string(stage4.EdgeDependsOn), SourceID: "s1", TargetID: "cacheable-thing"},
				{Type: string(stage4.EdgeDependsOn), SourceID: "s2", TargetID: "my-cache"},
			},
		}
	})
	changes := ClassifyChange(in)
	if len(changes) != 1 || changes[0].Kind != archmodel.EventCachingAdded {
		t.Fatalf("want exactly one CACHING_ADDED (my-cache), got %+v", changes)
	}
	if len(changes[0].AffectedIDs) != 1 || changes[0].AffectedIDs[0] != "my-cache" {
		t.Errorf("affected = %v, want [my-cache]", changes[0].AffectedIDs)
	}
}

func TestClassifyCyclePass_SCCComparison(t *testing.T) {
	// base: a -> b (no cycle); head: a -> b -> a (cycle)
	base := akg.NewCodePropertyGraph("base")
	base.Nodes = base.Nodes.Set("a", &stage4.ResolvedNode{ID: "a", Kind: "FUNCTION", Name: "A"})
	base.Nodes = base.Nodes.Set("b", &stage4.ResolvedNode{ID: "b", Kind: "FUNCTION", Name: "B"})
	addEdge(t, base, "a", "b")

	head := akg.NewCodePropertyGraph("head")
	head.Nodes = base.Nodes
	head.Nodes = head.Nodes.Set("a", &stage4.ResolvedNode{ID: "a", Kind: "FUNCTION", Name: "A"})
	head.Nodes = head.Nodes.Set("b", &stage4.ResolvedNode{ID: "b", Kind: "FUNCTION", Name: "B"})
	head.OutboundEdges = base.OutboundEdges
	head.InboundEdges = base.InboundEdges
	addEdge(t, head, "b", "a")

	in := testInput(func(in *ClassifyInput) {
		in.BaseGraph = base
		in.HeadGraph = head
	})
	changes := ClassifyChange(in)
	if len(changes) != 1 || changes[0].Kind != archmodel.EventCycleIntroduced {
		t.Fatalf("want one CYCLE_INTRODUCED, got %+v", changes)
	}
	if changes[0].AffectedIDs != nil {
		t.Errorf("cycle affected must stay nil for 5D dedup, got %v", changes[0].AffectedIDs)
	}
}

func TestClassifyCyclePass_MetricFallback(t *testing.T) {
	in := testInput(func(in *ClassifyInput) {
		in.BaseSnap = &archmodel.ArchSnapshot{Metrics: archmodel.ArchMetrics{CycleCount: 2}}
		in.HeadSnap = &archmodel.ArchSnapshot{Metrics: archmodel.ArchMetrics{CycleCount: 1}}
	})
	changes := ClassifyChange(in)
	if len(changes) != 1 || changes[0].Kind != archmodel.EventCycleResolved {
		t.Fatalf("want one CYCLE_RESOLVED, got %+v", changes)
	}
}

func TestClassifyChange_NilSafe(t *testing.T) {
	// Every pass must be safe on an empty input — the junior version panicked
	// on a nil graphDiff.
	changes := ClassifyChange(ClassifyInput{})
	if len(changes) != 0 {
		t.Fatalf("nil input produced %d changes, want 0", len(changes))
	}
	// Partially populated inputs must also be safe.
	changes = ClassifyChange(ClassifyInput{Meta: testMeta(), HeadSnap: &archmodel.ArchSnapshot{}})
	if len(changes) != 0 {
		t.Fatalf("partial input produced %d changes, want 0", len(changes))
	}
}

func TestClassifyChange_Deterministic(t *testing.T) {
	in := testInput(func(in *ClassifyInput) {
		in.BaseSnap = &archmodel.ArchSnapshot{Components: []archmodel.DetectedComponent{comp("c1", "auth")}}
		in.HeadSnap = &archmodel.ArchSnapshot{Components: []archmodel.DetectedComponent{comp("c1", "auth"), comp("c2", "billing", "c3"), comp("c3", "db")}}
		in.GraphDiff = &akg.GraphDiff{
			EdgesAdded: []akg.DiffEdge{
				{Type: string(stage4.EdgeQueriesDB), SourceID: "svc:pay", TargetID: "db:postgres"},
				{Type: string(stage4.EdgePublishes), SourceID: "svc:pay", TargetID: "events:paid"},
			},
		}
	})
	first := ClassifyChange(in)
	second := ClassifyChange(in)
	if len(first) != len(second) {
		t.Fatalf("runs disagree on count: %d vs %d", len(first), len(second))
	}
	for i := range first {
		a, b := first[i], second[i]
		if a.Kind != b.Kind || len(a.AffectedIDs) != len(b.AffectedIDs) || a.Summary != b.Summary {
			t.Errorf("run %d disagrees: %+v vs %+v", i, a, b)
		}
		for j := range a.AffectedIDs {
			if a.AffectedIDs[j] != b.AffectedIDs[j] {
				t.Errorf("run %d affected[%d]: %q vs %q", i, j, a.AffectedIDs[j], b.AffectedIDs[j])
			}
		}
	}
}

func addEdge(t *testing.T, g *akg.CodePropertyGraph, src, tgt string) {
	t.Helper()
	out, _ := g.OutboundEdges.Get(src)
	in, _ := g.InboundEdges.Get(tgt)
	g.OutboundEdges = g.OutboundEdges.Set(src, append(out, stage4.ResolvedEdge{SourceID: src, TargetID: tgt, Type: stage4.EdgeDependsOn}))
	g.InboundEdges = g.InboundEdges.Set(tgt, append(in, stage4.ResolvedEdge{SourceID: src, TargetID: tgt, Type: stage4.EdgeDependsOn}))
}
