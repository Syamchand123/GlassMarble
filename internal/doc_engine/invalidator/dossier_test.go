package invalidator

import (
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/link"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeriveArchEvents_Keywords(t *testing.T) {
	events := deriveArchEvents("Split auth service into layers\n\nExtract database interface to break cycle")
	assert.Contains(t, events, "COMPONENT_SPLIT")
	assert.Contains(t, events, "CYCLE_INTRODUCED")
	assert.Contains(t, events, "NEW_DATABASE_LAYER")
	assert.Contains(t, events, "LAYER_VIOLATION")
	assert.Contains(t, events, "SERVICE_ADDED")
	assert.Contains(t, events, "INTERFACE_CHANGED")
	assert.Contains(t, events, "SECURITY_BOUNDARY_CHANGED")

	// Deterministic order: fixed keyword order regardless of message order.
	again := deriveArchEvents("auth cycle split")
	assert.Equal(t, []string{"COMPONENT_SPLIT", "CYCLE_INTRODUCED", "SECURITY_BOUNDARY_CHANGED"}, again)

	// No keywords → no events.
	assert.Empty(t, deriveArchEvents("Fix typo in README"))
	assert.Empty(t, deriveArchEvents(""))
}

// ── B3 structural-event helpers ─────────────────────────────────────────────

func dossierTestNode(id, file, name, sig string) *link.ResolvedNode {
	return &link.ResolvedNode{
		ID:   id,
		Name: name,
		Kind: "FUNCTION",
		FileSpec: link.LocationMeta{
			Path:      file,
			LineStart: 1,
			LineEnd:   5,
		},
		Properties: map[string]string{"signature": sig},
	}
}

func dossierTestGraph(commit string, nodes ...*link.ResolvedNode) *akg.CodePropertyGraph {
	g := akg.NewCodePropertyGraph(commit)
	for _, n := range nodes {
		g.Nodes = g.Nodes.Set(n.ID, n)
	}
	return g
}

func dossierAddEdge(g *akg.CodePropertyGraph, src, dst string, typ link.RelationshipType) {
	e := link.ResolvedEdge{SourceID: src, TargetID: dst, Type: typ}
	out, _ := g.OutboundEdges.Get(src)
	g.OutboundEdges = g.OutboundEdges.Set(src, append(out, e))
	in, _ := g.InboundEdges.Get(dst)
	g.InboundEdges = g.InboundEdges.Set(dst, append(in, e))
}

func TestDeriveStructuralEvents_ComponentAdded(t *testing.T) {
	serve := dossierTestNode("internal/a/f.go::Serve", "internal/a/f.go", "Serve", "func Serve()")
	base := dossierTestGraph("base", serve)
	head := dossierTestGraph("head", serve,
		dossierTestNode("internal/b/g.go::Handle", "internal/b/g.go", "Handle", "func Handle()"))
	// New package dir + added symbol, nothing else.
	assert.Equal(t, []string{"COMPONENT_ADDED", "PUBLIC_SURFACE_CHANGED"},
		deriveStructuralEvents(base, head))
}

func TestDeriveStructuralEvents_ComponentRemoved(t *testing.T) {
	serve := dossierTestNode("internal/a/f.go::Serve", "internal/a/f.go", "Serve", "func Serve()")
	base := dossierTestGraph("base", serve,
		dossierTestNode("internal/b/g.go::Handle", "internal/b/g.go", "Handle", "func Handle()"))
	head := dossierTestGraph("head", serve)
	assert.Equal(t, []string{"COMPONENT_REMOVED", "PUBLIC_SURFACE_CHANGED"},
		deriveStructuralEvents(base, head))
}

func TestDeriveStructuralEvents_CycleIntroduced(t *testing.T) {
	a := dossierTestNode("internal/svc/a.go::A", "internal/svc/a.go", "A", "func A()")
	b := dossierTestNode("internal/svc/a.go::B", "internal/svc/a.go", "B", "func B()")
	base := dossierTestGraph("base", a, b)
	head := dossierTestGraph("head", a, b)
	dossierAddEdge(head, a.ID, b.ID, link.EdgeCalls)
	dossierAddEdge(head, b.ID, a.ID, link.EdgeCalls)
	assert.Equal(t, []string{"CYCLE_INTRODUCED"}, deriveStructuralEvents(base, head))
}

func TestDeriveStructuralEvents_CycleResolved(t *testing.T) {
	a := dossierTestNode("internal/svc/a.go::A", "internal/svc/a.go", "A", "func A()")
	b := dossierTestNode("internal/svc/a.go::B", "internal/svc/a.go", "B", "func B()")
	base := dossierTestGraph("base", a, b)
	dossierAddEdge(base, a.ID, b.ID, link.EdgeCalls)
	dossierAddEdge(base, b.ID, a.ID, link.EdgeCalls)
	head := dossierTestGraph("head", a, b)
	dossierAddEdge(head, a.ID, b.ID, link.EdgeCalls)
	assert.Equal(t, []string{"CYCLE_RESOLVED"}, deriveStructuralEvents(base, head))
}

func TestDeriveStructuralEvents_LayerViolation(t *testing.T) {
	load := dossierTestNode("pkg/store/s.go::Load", "pkg/store/s.go", "Load", "func Load()")
	main := dossierTestNode("cmd/app/m.go::Main", "cmd/app/m.go", "Main", "func Main()")
	base := dossierTestGraph("base", load, main)
	head := dossierTestGraph("head", load, main)
	// Upward dependency: pkg (rank 1) → cmd (rank 3) is forbidden.
	dossierAddEdge(head, load.ID, main.ID, link.EdgeCalls)
	assert.Equal(t, []string{"LAYER_VIOLATION"}, deriveStructuralEvents(base, head))
}

func TestDeriveStructuralEvents_DownwardEdgeAllowed(t *testing.T) {
	load := dossierTestNode("pkg/store/s.go::Load", "pkg/store/s.go", "Load", "func Load()")
	main := dossierTestNode("cmd/app/m.go::Main", "cmd/app/m.go", "Main", "func Main()")
	base := dossierTestGraph("base", load, main)
	head := dossierTestGraph("head", load, main)
	// Downward dependency: cmd (rank 3) → pkg (rank 1) is legal.
	dossierAddEdge(head, main.ID, load.ID, link.EdgeCalls)
	assert.Empty(t, deriveStructuralEvents(base, head))
}

func TestDeriveStructuralEvents_PublicSurfaceChanged(t *testing.T) {
	base := dossierTestGraph("base",
		dossierTestNode("internal/a/f.go::DoThing", "internal/a/f.go", "DoThing", "func DoThing() int"))
	head := dossierTestGraph("head",
		dossierTestNode("internal/a/f.go::DoThing", "internal/a/f.go", "DoThing", "func DoThing() string"))
	assert.Equal(t, []string{"PUBLIC_SURFACE_CHANGED"}, deriveStructuralEvents(base, head))
}

func TestDeriveStructuralEvents_UnexportedModificationIgnored(t *testing.T) {
	base := dossierTestGraph("base",
		dossierTestNode("internal/a/f.go::doThing", "internal/a/f.go", "doThing", "func doThing() int"))
	head := dossierTestGraph("head",
		dossierTestNode("internal/a/f.go::doThing", "internal/a/f.go", "doThing", "func doThing() string"))
	assert.Empty(t, deriveStructuralEvents(base, head))
}

func TestMergeArchEvents_StructuralFirstDeduped(t *testing.T) {
	assert.Equal(t,
		[]string{"COMPONENT_ADDED", "CYCLE_INTRODUCED", "SERVICE_ADDED"},
		mergeArchEvents(
			[]string{"COMPONENT_ADDED", "CYCLE_INTRODUCED"},
			[]string{"CYCLE_INTRODUCED", "SERVICE_ADDED"},
		))
	// Both graphs nil → structural empty → keyword fallback alone.
	assert.Equal(t, []string{"SERVICE_ADDED"},
		mergeArchEvents(deriveStructuralEvents(nil, nil), []string{"SERVICE_ADDED"}))
	assert.Empty(t, deriveStructuralEvents(nil, nil))
}

func TestDeriveStructuralEvents_NilHead(t *testing.T) {
	base := dossierTestGraph("base",
		dossierTestNode("internal/a/f.go::Serve", "internal/a/f.go", "Serve", "func Serve()"))
	assert.Empty(t, deriveStructuralEvents(base, nil))
	require.NotNil(t, base)
}
