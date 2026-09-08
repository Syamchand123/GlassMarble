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
