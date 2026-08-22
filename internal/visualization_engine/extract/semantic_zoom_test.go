package ingest

import (
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/types"
)

// TestGetPackageIDForNode verifies that package IDs are only derived from
// plausible package paths. AKG nodes with code-snippet names (multiline
// variable initializers, error strings) must never become package boundaries
// (GAP-W-02).
func TestGetPackageIDForNode(t *testing.T) {
	tests := []struct {
		name string
		id   string
		want string
	}{
		{
			name: "canonical module id becomes pkg id",
			id:   "module:internal/ai_engine",
			want: "pkg:internal",
		},
		{
			name: "canonical type id becomes pkg id",
			id:   "type:internal/visualization_engine/render/mermaid.go:renderer:mermaidRenderer",
			want: "pkg:internal/visualization_engine/render",
		},
		{
			name: "top-level module keeps its name",
			id:   "module:internal",
			want: "pkg:internal",
		},
		{
			name: "legacy file id becomes pkg id",
			id:   "file:internal/logger/logger.go::NewLogger",
			want: "pkg:internal/logger",
		},
		{
			name: "root-level go file is kept",
			id:   "main.go::main",
			want: "pkg:main.go",
		},
		{
			name: "code snippet id is rejected",
			id:   "os.Stdout = oldStdout }",
			want: "",
		},
		{
			name: "multiline code id is rejected",
			id:   "wg.Len()\n\t\t}",
			want: "",
		},
		{
			name: "error string id is rejected",
			id:   "concurrent delta failed",
			want: "",
		},
		{
			name: "numeric literal id is rejected",
			id:   "15",
			want: "",
		},
		{
			name: "url-encoded fragment id is rejected",
			id:   "pkg::=%20m.Send%28analyzeResultMsg%7Berr:%20err%2C%20initial:%20initial%7D%29",
			want: "",
		},
		{
			name: "keyword-only id is rejected",
			id:   "FUNCTION",
			want: "",
		},
		{
			name: "alias id with slash is accepted",
			id:   "gopkg.in/yaml.v3/yaml.go::YAMLToJSON",
			want: "pkg:gopkg.in/yaml.v3",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := getPackageIDForNode(tc.id, &types.NativeNode{Kind: "gm:Executable"})
			if got != tc.want {
				t.Errorf("expected %q, got %q", tc.want, got)
			}
		})
	}
}

// TestAggregateToPackageLevelSkipsGarbage verifies that nodes with unusable
// IDs are dropped from the zoomed graph together with their edges.
func TestAggregateToPackageLevelSkipsGarbage(t *testing.T) {
	graph := &types.NativeGraph{
		Nodes: map[string]*types.NativeNode{
			"module:internal/ai_engine":       {ID: "module:internal/ai_engine", Name: "ai_engine", Kind: "gm:Module"},
			"file:internal/ai_engine/agent.go::Agent": {ID: "file:internal/ai_engine/agent.go::Agent", Name: "Agent", Kind: "gm:Executable"},
			"os.Stdout = oldStdout }":         {ID: "os.Stdout = oldStdout }", Name: "os.Stdout = oldStdout }", Kind: "gm:Variable"},
		},
		Edges: []types.NativeEdge{
			{SourceID: "file:internal/ai_engine/agent.go::Agent", TargetID: "module:internal/ai_engine", Predicate: "gm:calls"},
			{SourceID: "os.Stdout = oldStdout }", TargetID: "file:internal/ai_engine/agent.go::Agent", Predicate: "gm:calls"},
		},
	}

	got := aggregateToPackageLevel(graph)
	if _, ok := got.Nodes["pkg:os.Stdout = oldStdout }"]; ok {
		t.Error("expected garbage package node to be dropped")
	}
	if _, ok := got.Nodes["pkg:internal/ai_engine"]; !ok {
		t.Error("expected pkg:internal/ai_engine to be present")
	}
	for _, e := range got.Edges {
		if e.SourceID == "" || e.TargetID == "" {
			t.Errorf("edge with empty endpoint survived aggregation: %+v", e)
		}
	}
}