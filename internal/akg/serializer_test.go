package akg

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage4"
	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/stage1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSerializeNilGraph(t *testing.T) {
	var buf bytes.Buffer
	err := SerializeToTurtle(nil, &buf)
	if err == nil {
		t.Error("expected error for nil graph")
	}
}

func TestSerializeEmptyGraph(t *testing.T) {
	g := NewCodePropertyGraph("testhash")
	var buf bytes.Buffer
	err := SerializeToTurtle(g, &buf)
	if err != nil {
		t.Fatalf("serialize error: %v", err)
	}
	if buf.Len() == 0 {
		t.Error("expected non-empty output for empty graph (prefixes + metadata)")
	}
}

func TestSerializeAllKinds(t *testing.T) {
	g := NewCodePropertyGraph("test")
	kinds := []struct {
		kind  string
		class string
	}{
		{"MODULE", "gm:Module"},
		{"NAMESPACE", "gm:Namespace"},
		{"FILE", "gm:File"},
		{"STRUCT", "gm:Struct"},
		{"CLASS", "gm:Class"},
		{"INTERFACE", "gm:Interface"},
		{"FUNCTION", "gm:Function"},
		{"METHOD", "gm:Method"},
		{"FIELD", "gm:Member"},
		{"PARAMETER", "gm:Parameter"},
		{"IF_BRANCH", "gm:ControlStructure"},
		{"LOOP_BRANCH", "gm:ControlStructure"},
		{"DFG_VAR", "gm:Variable"},
	}

	for _, k := range kinds {
		id := "test::" + k.kind
		g.Nodes = g.Nodes.Set(id, &stage4.ResolvedNode{ID: id, Kind: k.kind, Name: k.kind})
	}

	var buf bytes.Buffer
	if err := SerializeToTurtle(g, &buf); err != nil {
		t.Fatalf("serialize error: %v", err)
	}

	output := buf.String()
	for _, k := range kinds {
		class := mapKindToClass(k.kind)
		if !contains(output, class) {
			t.Errorf("output missing class %s for kind %s", class, k.kind)
		}
	}
}

func TestSerializeAllEdgeTypes(t *testing.T) {
	g := NewCodePropertyGraph("test")
	g.Nodes = g.Nodes.Set("src", &stage4.ResolvedNode{ID: "src", Kind: "FUNCTION", Name: "src"})
	g.Nodes = g.Nodes.Set("dst", &stage4.ResolvedNode{ID: "dst", Kind: "FUNCTION", Name: "dst"})

	edgeTypes := []stage4.RelationshipType{
		stage4.EdgeCalls, stage4.EdgeComposes, stage4.EdgeDataFlow,
		stage4.EdgeControlFlow, stage4.EdgeImplements, stage4.EdgeSpawnsConcurrent,
		stage4.EdgeReferences, stage4.EdgeInjects, stage4.EdgeFFICall,
	}

	for i, et := range edgeTypes {
		existing, _ := g.OutboundEdges.Get("src")
		g.OutboundEdges = g.OutboundEdges.Set("src", append(existing,
			stage4.ResolvedEdge{SourceID: "src", TargetID: "dst", Type: et, LineNumber: i + 1}))
	}

	var buf bytes.Buffer
	if err := SerializeToTurtle(g, &buf); err != nil {
		t.Fatalf("serialize error: %v", err)
	}

	output := buf.String()
	if !contains(output, "gm:calls") || !contains(output, "gm:dataFlowTo") {
		t.Error("output missing expected edge predicates")
	}
}

func TestSerializeProperties(t *testing.T) {
	g := NewCodePropertyGraph("test")
	g.Nodes = g.Nodes.Set("n1", &stage4.ResolvedNode{
		ID: "n1", Kind: "CLASS", Name: "Test",
		Properties: map[string]string{"lang": "go", "framework": "gin"},
	})

	var buf bytes.Buffer
	if err := SerializeToTurtle(g, &buf); err != nil {
		t.Fatalf("serialize error: %v", err)
	}

	output := buf.String()
	if !contains(output, "gm:lang") || !contains(output, "gm:framework") {
		t.Error("output missing property predicates")
	}
}

func TestSerializeEntrypoints(t *testing.T) {
	g := NewCodePropertyGraph("test")
	g.Nodes = g.Nodes.Set("main", &stage4.ResolvedNode{ID: "main", Kind: "FUNCTION", Name: "main"})
	g.Entrypoints = []string{"main"}

	var buf bytes.Buffer
	if err := SerializeToTurtle(g, &buf); err != nil {
		t.Fatalf("serialize error: %v", err)
	}

	output := buf.String()
	if !contains(output, "gm:isEntrypoint") {
		t.Error("output missing isEntrypoint flag")
	}
}

func TestSerializeRoundTrip(t *testing.T) {
	g := NewCodePropertyGraph("test")
	g.Nodes = g.Nodes.Set("n1", &stage4.ResolvedNode{ID: "n1", Kind: "FUNCTION", Name: "foo", FileSpec: stage4.LocationMeta{Path: "test.go", LineStart: 1, LineEnd: 10}})
	g.Nodes = g.Nodes.Set("n2", &stage4.ResolvedNode{ID: "n2", Kind: "FUNCTION", Name: "bar", FileSpec: stage4.LocationMeta{Path: "test.go", LineStart: 20, LineEnd: 30}})
	g.OutboundEdges = g.OutboundEdges.Set("n1", []stage4.ResolvedEdge{{SourceID: "n1", TargetID: "n2", Type: stage4.EdgeCalls, LineNumber: 5}})

	var buf bytes.Buffer
	err := SerializeToTurtle(g, &buf)
	if err != nil {
		t.Fatalf("serialize error: %v", err)
	}

	tmpFile := filepath.Join(t.TempDir(), "roundtrip.ttl")
	if err := os.WriteFile(tmpFile, buf.Bytes(), 0644); err != nil {
		t.Fatalf("write temp file error: %v", err)
	}

	nodes, edges, err := stage1.ParseTTLFile(tmpFile)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if _, ok := nodes["n1"]; !ok {
		t.Error("expected n1 in parsed nodes")
	}
	if _, ok := nodes["n2"]; !ok {
		t.Error("expected n2 in parsed nodes")
	}
	if len(edges) != 1 {
		t.Errorf("expected 1 edge, got %d", len(edges))
	}
}

func TestSerializeDelta(t *testing.T) {
	payload := &stage4.Stage4Output{
		CommitHash: "delta",
		GraphNodes: map[string]*stage4.ResolvedNode{
			"n1": {ID: "n1", Kind: "FUNCTION", Name: "newfunc"},
		},
		OutboundEdges: map[string][]stage4.ResolvedEdge{},
	}
	deleted := map[string]bool{"old_n1": true}

	var buf bytes.Buffer
	err := SerializeDeltaToTurtle(payload, deleted, 7, &buf)
	if err != nil {
		t.Fatalf("serialize delta error: %v", err)
	}

	output := buf.String()
	// Tombstones must be written as node blocks (`<uri> a gm:Deleted ;
	// gm:status "DELETED" .`) so the parser treats them as deletions instead
	// of phantom edges (AUDIT Issue 3 Phase 3B-6).
	if !contains(output, "a gm:Deleted") {
		t.Error("expected gm:Deleted tombstone class in delta output")
	}
	if !contains(output, `gm:status "DELETED"`) {
		t.Error("expected DELETED status tombstone in delta output")
	}
	if !contains(output, "newfunc") {
		t.Error("expected newfunc node in delta output")
	}
	// The delta metadata block must carry the committing graph's version so
	// incremental appends never regress the WAL replay bound.
	if !contains(output, "gm:version 7") {
		t.Error("expected gm:version 7 in delta metadata block")
	}
}

func TestSerializeMetadataSchemaVersion(t *testing.T) {
	g := NewCodePropertyGraph("hash123")
	g.Version = 7
	g.Nodes = g.Nodes.Set("n1", &stage4.ResolvedNode{ID: "n1", Kind: "FUNCTION", Name: "f"})

	var buf bytes.Buffer
	if err := SerializeToTurtle(g, &buf); err != nil {
		t.Fatalf("serialize error: %v", err)
	}
	ttl := buf.String()

	// Metadata node must carry the schema version and the WAL replay bound so
	// recovery can skip already-applied transactions (AUDIT Issue 3 Phase 3B-7).
	if !contains(ttl, "gm:schemaVersion") {
		t.Error("output missing gm:schemaVersion in metadata block")
	}
	if !contains(ttl, "gm:version") {
		t.Error("output missing gm:version (WAL replay bound) in metadata block")
	}

	// Round-trip through the TTL reader: version and commit hash must be restored.
	roundTrip, err := reconstructFromTTLFile(writeTTLToTemp(t, ttl))
	if err != nil {
		t.Fatalf("reconstruct error: %v", err)
	}
	if roundTrip.Version != 7 {
		t.Errorf("expected restored Version=7, got %d", roundTrip.Version)
	}
	if roundTrip.CommitHash != "hash123" {
		t.Errorf("expected restored CommitHash hash123, got %q", roundTrip.CommitHash)
	}
}

func TestMapClassToKind(t *testing.T) {
	tests := []struct {
		class string
		want  string
	}{
		{"gm:Module", "MODULE"},
		{"gm:Namespace", "NAMESPACE"},
		{"gm:File", "FILE"},
		{"gm:Struct", "STRUCT"},
		{"gm:Class", "CLASS"},
		{"gm:Interface", "INTERFACE"},
		{"gm:Function", "FUNCTION"},
		{"gm:Method", "METHOD"},
		{"gm:Member", "FIELD"},
		{"gm:Variable", "VARIABLE"},
		{"gm:TypeDecl", "STRUCT"},
		{"gm:Executable", "FUNCTION"},
		{"gm:ControlStructure", "IF_BRANCH"},
		{"gm:Parameter", "PARAMETER"},
		{"gm:CFGSummary", "CFG_SUMMARY"},
		{"gm:DFGSummary", "DFG_SUMMARY"},
		{"gm:EventTopic", "EVENT_TOPIC"},
		{"gm:VirtualDatabase", "VIRTUAL_DATABASE"},
		{"gm:VirtualEndpoint", "VIRTUAL_ENDPOINT"},
		{"gm:Block", "BLOCK"},
		{"gm:Annotation", "ANNOTATION"},
		{"rdfs:Class", "rdfs:Class"},
		{"custom", "custom"},
	}
	for _, tc := range tests {
		if got := mapClassToKind(tc.class); got != tc.want {
			t.Errorf("mapClassToKind(%q) = %q, want %q", tc.class, got, tc.want)
		}
	}
}

func TestSerializeRoundTripKindStability(t *testing.T) {
	g := NewCodePropertyGraph("test")
	for _, kind := range []string{
		"MODULE", "NAMESPACE", "FILE", "STRUCT", "CLASS", "INTERFACE",
		"FUNCTION", "METHOD", "FIELD", "IF_BRANCH", "PARAMETER", "CFG_SUMMARY",
		"EVENT_TOPIC", "VIRTUAL_DATABASE", "BLOCK", "ANNOTATION", "UNKNOWN_KIND",
	} {
		id := "k::" + kind
		g.Nodes = g.Nodes.Set(id, &stage4.ResolvedNode{ID: id, Kind: kind, Name: kind})
	}

	serialize := func() string {
		var buf bytes.Buffer
		if err := SerializeToTurtle(g, &buf); err != nil {
			t.Fatalf("serialize error: %v", err)
		}
		return buf.String()
	}

	// Simulate repeated save -> Turtle restore -> save cycles (reconstructFromTurtle).
	for cycle := 0; cycle < 3; cycle++ {
		ttl := serialize()
		tmpFile := filepath.Join(t.TempDir(), "cycle.ttl")
		if err := os.WriteFile(tmpFile, []byte(ttl), 0644); err != nil {
			t.Fatalf("write error: %v", err)
		}
		nodes, _, err := stage1.ParseTTLFile(tmpFile)
		if err != nil {
			t.Fatalf("parse error: %v", err)
		}
		ng := NewCodePropertyGraph("cycle")
		for id, tn := range nodes {
			ng.Nodes = ng.Nodes.Set(id, &stage4.ResolvedNode{
				ID: id, Kind: mapClassToKind(tn.Kind), Name: tn.Name,
			})
		}
		g = ng
	}

	// Kinds must not degrade to rdfs:Class across cycles.
	ttl := serialize()
	for _, class := range []string{
		"gm:Module", "gm:Namespace", "gm:File", "gm:Struct", "gm:Class",
		"gm:Interface", "gm:Function", "gm:Method", "gm:ControlStructure",
	} {
		if !contains(ttl, class) {
			t.Errorf("class %s lost across serialization cycles", class)
		}
	}
	if !contains(ttl, "gm:Annotation") {
		t.Error("gm:Annotation lost across serialization cycles")
	}
}

func TestSerializePropertiesRoundTripStable(t *testing.T) {
	g := NewCodePropertyGraph("test")
	g.Nodes = g.Nodes.Set("n1", &stage4.ResolvedNode{
		ID: "n1", Kind: "CLASS", Name: "Test",
		// macro_rules is intentionally NOT persisted (derived data); use a
		// regular vocabulary property for the escaping round-trip check.
		Properties: map[string]string{"blast_radius": `Dead Sub-system: "quoted" \ back`},
	})

	serialize := func() string {
		var buf bytes.Buffer
		if err := SerializeToTurtle(g, &buf); err != nil {
			t.Fatalf("serialize error: %v", err)
		}
		return buf.String()
	}

	for cycle := 0; cycle < 3; cycle++ {
		ttl := serialize()
		tmpFile := filepath.Join(t.TempDir(), "props.ttl")
		if err := os.WriteFile(tmpFile, []byte(ttl), 0644); err != nil {
			t.Fatalf("write error: %v", err)
		}
		nodes, _, err := stage1.ParseTTLFile(tmpFile)
		if err != nil {
			t.Fatalf("parse error: %v", err)
		}
		n1, ok := nodes["n1"]
		if !ok {
			t.Fatal("expected n1 in parsed nodes")
		}
		ng := NewCodePropertyGraph("cycle")
		ng.Nodes = ng.Nodes.Set("n1", &stage4.ResolvedNode{
			ID: "n1", Kind: mapClassToKind(n1.Kind), Name: n1.Name, Properties: n1.Properties,
		})
		g = ng
	}

	ttl := serialize()
	if contains(ttl, "gm:gm:") {
		t.Error("property keys accumulate gm: prefixes across cycles")
	}
	if !contains(ttl, `gm:blast_radius "Dead Sub-system: \"quoted\" \\ back"`) {
		t.Errorf("property value not stable across cycles:\n%s", ttl)
	}
}

func TestSerialize_AllEdgeTypesCoverage(t *testing.T) {
	g := NewCodePropertyGraph("test")
	g.Nodes = g.Nodes.Set("src", &stage4.ResolvedNode{ID: "src", Kind: "FUNCTION", Name: "src"})
	g.Nodes = g.Nodes.Set("dst", &stage4.ResolvedNode{ID: "dst", Kind: "FUNCTION", Name: "dst"})

	for _, et := range []stage4.RelationshipType{
		stage4.EdgeCalls, stage4.EdgeImplements, stage4.EdgeExtends, stage4.EdgeComposes,
		stage4.EdgeReferences, stage4.EdgeThrows, stage4.EdgeSpawnsConcurrent,
		stage4.EdgeDispatchesEvent, stage4.EdgeExposesEndpoint, stage4.EdgeSecuritySink,
		stage4.EdgeConsumesResource, stage4.EdgeMutatesGlobal, stage4.EdgeAliasesType,
		stage4.EdgeControlFlow, stage4.EdgeDataFlow, stage4.EdgeAliases, stage4.EdgeVulnerable,
		stage4.EdgeInstantiates, stage4.EdgeSendsTo, stage4.EdgeReceivesFrom,
		stage4.EdgeCyclic, stage4.EdgeNetworkCall, stage4.EdgeQueriesDB, stage4.EdgeCallsCloudAPI,
		stage4.EdgeCatches, stage4.EdgeDefers, stage4.EdgeDependsOn, stage4.EdgeContains,
		stage4.EdgeMixes, stage4.EdgeHasField, stage4.EdgeHasParam, stage4.EdgeReturns,
		stage4.EdgeContextCall, stage4.EdgePointsTo, stage4.EdgeHeapAlias, stage4.EdgeConstraint,
		stage4.EdgeFFICall, stage4.EdgePublishes, stage4.EdgeSubscribes, stage4.EdgeInjects,
		stage4.EdgeEscapesToHeap, stage4.EdgeBelongsTo,
	} {
		g.OutboundEdges = g.OutboundEdges.Set("src", append(
			g.GetOutboundEdges("src"),
			stage4.ResolvedEdge{SourceID: "src", TargetID: "dst", Type: et, LineNumber: 1},
		))
	}

	var buf bytes.Buffer
	err := SerializeToTurtle(g, &buf)
	require.NoError(t, err)
	assert.NotEmpty(t, buf.String())
}

func TestSerialize_AllKindsCoverage(t *testing.T) {
	g := NewCodePropertyGraph("test")
	for _, kind := range []string{
		"MODULE", "FILE", "STRUCT", "CLASS", "INTERFACE", "FUNCTION", "METHOD",
		"IF_BRANCH", "LOOP_BRANCH", "SWITCH_BRANCH", "DFG_VAR", "PARAMETER",
		"CFG_SUMMARY", "DFG_SUMMARY", "EVENT_TOPIC", "VIRTUAL_DATABASE",
		"VIRTUAL_ENDPOINT", "BLOCK", "ANNOTATION", "DECORATOR", "UNKNOWN",
	} {
		g.Nodes = g.Nodes.Set(kind, &stage4.ResolvedNode{ID: kind, Kind: kind, Name: kind})
	}
	var buf bytes.Buffer
	err := SerializeToTurtle(g, &buf)
	require.NoError(t, err)
	assert.NotEmpty(t, buf.String())
}

func writeTTLToTemp(t *testing.T, ttl string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "roundtrip.ttl")
	if err := os.WriteFile(path, []byte(ttl), 0644); err != nil {
		t.Fatalf("write error: %v", err)
	}
	return path
}
