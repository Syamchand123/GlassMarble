package types

import (
	"strings"
	"testing"
)

func TestDiagramTypeConstants(t *testing.T) {
	if UMLClass != "UML_CLASS" {
		t.Errorf("expected UML_CLASS, got %s", UMLClass)
	}
	if C4Context != "C4_CONTEXT" {
		t.Errorf("expected C4_CONTEXT, got %s", C4Context)
	}
	if DependencyGraph != "DEPENDENCY_GRAPH" {
		t.Errorf("expected DEPENDENCY_GRAPH, got %s", DependencyGraph)
	}
}

func TestScopeLevelValues(t *testing.T) {
	if ScopeGlobal != 0 {
		t.Errorf("expected ScopeGlobal=0, got %d", ScopeGlobal)
	}
	if ScopeFolder != 1 {
		t.Errorf("expected ScopeFolder=1, got %d", ScopeFolder)
	}
	if ScopeFile != 2 {
		t.Errorf("expected ScopeFile=2, got %d", ScopeFile)
	}
}

func TestQueryOptionsDefault(t *testing.T) {
	opts := QueryOptions{}
	if opts.Scope != ScopeGlobal {
		t.Errorf("expected default Scope to be ScopeGlobal")
	}
	if opts.Format != "" {
		t.Errorf("expected default Format to be empty")
	}
	if opts.MaxDepth != 0 {
		t.Errorf("expected default MaxDepth to be 0")
	}
}

func TestPipelineConfigDefaults(t *testing.T) {
	cfg := PipelineConfig{}
	if cfg.EnableMetrics {
		t.Error("expected EnableMetrics to be false by default")
	}
	if cfg.EnableCommunities {
		t.Error("expected EnableCommunities to be false by default")
	}
}

func TestPipelineStageOrder(t *testing.T) {
	if StageParse != 0 {
		t.Errorf("expected StageParse=0, got %d", StageParse)
	}
	if StageScope != 1 {
		t.Errorf("expected StageScope=1, got %d", StageScope)
	}
	if StageRender != 6 {
		t.Errorf("expected StageRender=6, got %d", StageRender)
	}
}

func TestGraphSummaryDefault(t *testing.T) {
	s := GraphSummary{}
	if s.NodeCount != 0 {
		t.Errorf("expected NodeCount=0, got %d", s.NodeCount)
	}
}

func TestLayoutNodeDefault(t *testing.T) {
	n := LayoutNode{}
	if n.PageRank != 0 {
		t.Errorf("expected PageRank=0, got %f", n.PageRank)
	}
	if n.IsHotspot {
		t.Error("expected IsHotspot to be false")
	}
	if n.IsBottleneck {
		t.Error("expected IsBottleneck to be false")
	}
	if n.IsGodObject {
		t.Error("expected IsGodObject to be false")
	}
}

func TestExtractionConfigDefault(t *testing.T) {
	cfg := ExtractionConfig{}
	if cfg.MaxDepth != 0 {
		t.Errorf("expected MaxDepth=0, got %d", cfg.MaxDepth)
	}
	if cfg.Name != "" {
		t.Errorf("expected Name empty, got %s", cfg.Name)
	}
}

// ============================================================================
// URI encoding/decoding round-trip tests (AUDIT Issue 2 Phase 2B-6 and Issue 3
// Phase 3D-14). FormatNodeURI and ParseNodeURI must be exact inverses for every
// character the serializer escapes, including the later additions { } | ^ and
// control characters.
// ============================================================================

func TestFormatNodeURINamespaces(t *testing.T) {
	cases := []struct {
		id, want string
	}{
		{"path::Func", "<http://glassmarble.org/node/path::Func>"},
		{"file:src/main.go", "<http://glassmarble.org/file/src/main.go>"},
		{"module:internal/db", "<http://glassmarble.org/namespace/internal/db>"},
	}
	for _, c := range cases {
		if got := FormatNodeURI(c.id); got != c.want {
			t.Errorf("FormatNodeURI(%q) = %q, want %q", c.id, got, c.want)
		}
	}
}

func TestFormatNodeURIEscapes(t *testing.T) {
	// Every special character the serializer escapes (AUDIT Issue 3 Phase
	// 3D-14 / ontology_test.go's "special-chars" node). The angle brackets are
	// the IRI delimiters themselves, so they are intentionally present.
	input := "my dir/x.go::Foo\"<>&|^`{}"
	got := FormatNodeURI(input)
	for _, forbidden := range []string{" ", "\"", "{", "}", "|", "^", "`"} {
		if strings.Contains(got, forbidden) {
			t.Errorf("FormatNodeURI(%q) still contains %q in %q", input, forbidden, got)
		}
	}
}

func TestParseNodeURINamespaces(t *testing.T) {
	cases := []struct {
		uri, want string
	}{
		{"<http://glassmarble.org/node/path::Func>", "path::Func"},
		{"<http://glassmarble.org/file/src/main.go>", "file:src/main.go"},
		{"<http://glassmarble.org/namespace/internal/db>", "module:internal/db"},
		{"<ext:foo>", "ext:foo"},
		{"bare", "bare"},
	}
	for _, c := range cases {
		if got := ParseNodeURI(c.uri); got != c.want {
			t.Errorf("ParseNodeURI(%q) = %q, want %q", c.uri, got, c.want)
		}
	}
}

func TestRoundTripFormatParseNodeURI(t *testing.T) {
	// Any node ID with special characters must survive
	// FormatNodeURI -> ParseNodeURI unchanged (AUDIT Issue 2 Phase 2B-6).
	inputs := []string{
		"file:my dir/x.go",
		"src/db.go::DBStore::Fetch(id)",
		"ext:( \"fmt\" )::{: defer tm.saveToDisk(...)}",
		"path::Func{param|with|pipes}",
		"node::with%20percent::x",
		"module:internal/db",
		"file:x.go::Name\"with\"quotes",
		"::{}|^`<>\\",
	}
	for _, in := range inputs {
		got := ParseNodeURI(FormatNodeURI(in))
		if got != in {
			t.Errorf("round-trip %q -> %q -> %q", in, FormatNodeURI(in), got)
		}
	}
}

func TestParseNodeURIInvalidSequences(t *testing.T) {
	// Invalid %XX escapes must be left verbatim, not dropped.
	if got := ParseNodeURI("<http://glassmarble.org/node/a%2>b>"); got != "a%2>b" {
		t.Errorf("invalid escape not preserved: %q", got)
	}
	if got := ParseNodeURI("<http://glassmarble.org/node/a%GGb>"); got != "a%GGb" {
		t.Errorf("non-hex escape not preserved: %q", got)
	}
	if got := ParseNodeURI("<http://glassmarble.org/node/a%b>"); got != "a%b" {
		t.Errorf("truncated escape not preserved: %q", got)
	}
}

func TestNativeGraphCloneDeepCopy(t *testing.T) {
	g := &NativeGraph{
		Nodes: map[string]*NativeNode{
			"a": {ID: "a", Kind: "gm:TypeDecl", Properties: map[string]string{"k": "v"}},
		},
		Edges: []NativeEdge{{SourceID: "a", Predicate: "gm:calls", TargetID: "b"}},
	}

	clone := g.Clone()
	if clone == nil {
		t.Fatal("Clone returned nil")
	}
	if len(clone.Nodes) != 1 || len(clone.Edges) != 1 {
		t.Fatalf("clone size = %d/%d, want 1/1", len(clone.Nodes), len(clone.Edges))
	}

	// Mutating the clone's node properties must not affect the original.
	clone.Nodes["a"].Properties["k"] = "changed"
	if g.Nodes["a"].Properties["k"] != "v" {
		t.Error("properties map was shared, not deep-copied")
	}
	clone.Nodes["a"].Name = "renamed"
	if g.Nodes["a"].Name != "" {
		t.Error("node pointer was shared, not deep-copied")
	}

	// Mutating the clone's edge slice must not affect the original.
	clone.Edges[0].LineNumber = 99
	if g.Edges[0].LineNumber != 0 {
		t.Error("edge slice was shared, not deep-copied")
	}
}

func TestNativeGraphCloneNilAndEmpty(t *testing.T) {
	var g *NativeGraph
	if got := g.Clone(); got != nil {
		t.Errorf("Clone of nil graph = %v, want nil", got)
	}
	empty := &NativeGraph{Nodes: map[string]*NativeNode{}, Edges: []NativeEdge{}}
	if got := empty.Clone(); got == nil || len(got.Nodes) != 0 || len(got.Edges) != 0 {
		t.Errorf("Clone of empty graph = %v, want empty non-nil graph", got)
	}
}
