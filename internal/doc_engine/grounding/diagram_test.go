package grounding

import (
	"strings"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/link"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateDiagram_Callgraph(t *testing.T) {
	graph := akg.NewCodePropertyGraph("c1")
	entry := "internal/auth/jwt.go::ValidateToken"
	callee := "internal/auth/db.go::LookupKey"

	graph.Nodes = graph.Nodes.Set(entry, &link.ResolvedNode{ID: entry, Name: "ValidateToken"})
	graph.Nodes = graph.Nodes.Set(callee, &link.ResolvedNode{ID: callee, Name: "LookupKey"})
	graph.OutboundEdges = graph.OutboundEdges.Set(entry, []link.ResolvedEdge{
		{SourceID: entry, TargetID: callee, Type: link.EdgeCalls},
	})

	ref := config.DiagramRef{
		Type:  "callgraph",
		Entry: entry,
	}

	diag, err := GenerateDiagram(ref, graph)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(diag, "```mermaid"))
	assert.True(t, strings.HasSuffix(strings.TrimSpace(diag), "```"))
	assert.Contains(t, diag, "-->")
	assert.Contains(t, diag, "ValidateToken")
	assert.Contains(t, diag, "LookupKey")
}

// TestGenerateDiagram_Callgraph_NodeLabelsAreDeterministic guards against a
// regression where the callgraph's node-label lines (the "id[\"name\"]"
// declarations after the edge list) were emitted by iterating a
// map[string]string directly. Go randomizes map iteration order per run, so
// the exact same graph could render its label lines in a different order on
// every regeneration even though nothing about the call graph changed —
// causing `gmb doc diff` (and real re-generation) to report a spurious
// change, and generated docs to churn in git diffs for no reason. Node IDs
// must be sorted before the labels are written, so this asserts the exact
// expected byte-for-byte output rather than just calling it twice.
func TestGenerateDiagram_Callgraph_NodeLabelsAreDeterministic(t *testing.T) {
	graph := akg.NewCodePropertyGraph("c3")
	entry := "b.go::Bravo"
	callee1 := "a.go::Alpha"
	callee2 := "c.go::Charlie"

	for _, id := range []string{entry, callee1, callee2} {
		graph.Nodes = graph.Nodes.Set(id, &link.ResolvedNode{ID: id, Name: id})
	}
	graph.OutboundEdges = graph.OutboundEdges.Set(entry, []link.ResolvedEdge{
		{SourceID: entry, TargetID: callee1, Type: link.EdgeCalls},
		{SourceID: entry, TargetID: callee2, Type: link.EdgeCalls},
	})

	ref := config.DiagramRef{Type: "callgraph", Entry: entry}

	want := "```mermaid\ngraph TD\n" +
		"  n_b_go__Bravo --> n_a_go__Alpha\n" +
		"  n_b_go__Bravo --> n_c_go__Charlie\n" +
		"  n_a_go__Alpha[\"Alpha\"]\n" +
		"  n_b_go__Bravo[\"Bravo\"]\n" +
		"  n_c_go__Charlie[\"Charlie\"]\n" +
		"```"

	for i := 0; i < 5; i++ {
		diag, err := GenerateDiagram(ref, graph)
		require.NoError(t, err)
		assert.Equal(t, want, diag)
	}
}

func TestGenerateDiagram_DependencyAndLayered(t *testing.T) {
	graph := akg.NewCodePropertyGraph("c2")
	graph.OutboundEdges = graph.OutboundEdges.Set("cmd/root.go::Main", []link.ResolvedEdge{
		{SourceID: "cmd/root.go::Main", TargetID: "internal/doc_engine/doc.go::Run", Type: link.EdgeCalls},
	})

	depRef := config.DiagramRef{Type: "dependency"}
	depDiag, err := GenerateDiagram(depRef, graph)
	require.NoError(t, err)
	assert.Contains(t, depDiag, "graph LR")
	assert.Contains(t, depDiag, "cmd")

	layerRef := config.DiagramRef{Type: "layered"}
	layerDiag, err := GenerateDiagram(layerRef, graph)
	require.NoError(t, err)
	assert.Contains(t, layerDiag, "subgraph")
	assert.Contains(t, layerDiag, "doc_engine")
}
