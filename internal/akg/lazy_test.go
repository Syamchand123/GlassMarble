package akg

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/link"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// commitTestGraph commits a small fixed graph and returns the storage dir.
func commitTestGraph(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	tm, err := NewAKGTransactionManager(dir)
	require.NoError(t, err)
	defer tm.Close()

	payload := link.NewLinkOutput("testhash")
	payload.GraphNodes["a"] = &link.ResolvedNode{ID: "a", Kind: "FUNCTION", Name: "alpha", FileSpec: link.LocationMeta{Path: "src/a.go", LineStart: 1, LineEnd: 10}}
	payload.GraphNodes["b"] = &link.ResolvedNode{ID: "b", Kind: "STRUCT", Name: "beta", FileSpec: link.LocationMeta{Path: "src/b.go", LineStart: 5, LineEnd: 20}}
	payload.GraphNodes["VIRTUAL_q1"] = &link.ResolvedNode{ID: "VIRTUAL_q1", Kind: "VIRTUAL_QUEUE", Name: "q1", FileSpec: link.LocationMeta{Path: "", LineStart: 0, LineEnd: 0}}
	payload.AddEdge("a", "b", link.EdgeCalls, 7)
	payload.EntrypointRegistry = []string{"a"}

	require.NoError(t, tm.ExecuteDeltaTransaction(payload, []string{"src/a.go", "src/b.go"}))
	return dir
}

// TestQueryNodeLazyMatchesRestored: a lazy node lookup returns exactly what
// the restored graph holds for the node and its incident edges
// (AUDIT Issue 4 Phase 4A-2).
func TestQueryNodeLazyMatchesRestored(t *testing.T) {
	dir := commitTestGraph(t)

	tm, err := NewAKGTransactionManager(dir)
	require.NoError(t, err)
	defer tm.Close()
	restored := tm.GetActiveSnapshot()
	rn, ok := restored.Nodes.Get("a")
	require.True(t, ok)

	node, out, in, err := QueryNode(dir, "a")
	require.NoError(t, err)
	require.NotNil(t, node)
	assert.Equal(t, rn.Name, node.Name)
	assert.Equal(t, rn.Kind, node.Kind)
	assert.Equal(t, rn.FileSpec.Path, node.FileSpec.Path)
	assert.Equal(t, rn.FileSpec.LineStart, node.FileSpec.LineStart)
	assert.Len(t, out, 1)
	assert.Empty(t, in)
	assert.Equal(t, "b", out[0].TargetID)
	assert.Equal(t, link.EdgeCalls, out[0].Type)
	assert.Equal(t, 7, out[0].LineNumber)

	// Inbound edge on the target side.
	_, _, inB, err := QueryNode(dir, "b")
	require.NoError(t, err)
	require.Len(t, inB, 1)
	assert.Equal(t, "a", inB[0].SourceID)

	// Missing node: nil, nil error.
	node, _, _, err = QueryNode(dir, "does-not-exist")
	require.NoError(t, err)
	assert.Nil(t, node)

	// Missing database: error.
	_, _, _, err = QueryNode(t.TempDir(), "a")
	require.Error(t, err)
}

// TestStreamNodesParity: streaming yields every node the restored graph
// holds, and early stop is clean (AUDIT Issue 4 Phase 4A-2).
func TestStreamNodesParity(t *testing.T) {
	dir := commitTestGraph(t)

	tm, err := NewAKGTransactionManager(dir)
	require.NoError(t, err)
	defer tm.Close()
	expected := tm.GetActiveSnapshot().Nodes.Len()

	seen := 0
	err = StreamNodes(dir, func(n *link.ResolvedNode) bool {
		seen++
		return true
	})
	require.NoError(t, err)
	assert.Equal(t, expected, seen)

	// Early stop must return a nil error.
	stopped := 0
	err = StreamNodes(dir, func(n *link.ResolvedNode) bool {
		stopped++
		return stopped < 1
	})
	require.NoError(t, err)
	assert.Equal(t, 1, stopped)
}

// TestStreamGraphStatsParity: the lazy stats equal the restored graph's
// figures (AUDIT Issue 4 Phase 4A-2 / Issue 5 Phase 5B-5).
func TestStreamGraphStatsParity(t *testing.T) {
	dir := commitTestGraph(t)

	tm, err := NewAKGTransactionManager(dir)
	require.NoError(t, err)
	defer tm.Close()
	snapshot := tm.GetActiveSnapshot()

	stats, err := StreamGraphStats(dir)
	require.NoError(t, err)

	assert.Equal(t, snapshot.Nodes.Len(), stats.NodeCount)
	assert.Equal(t, 1, stats.VirtualCount)
	assert.Equal(t, 1, stats.Entrypoints)
	assert.Equal(t, snapshot.FileNodeIndex.Len(), stats.IndexedFiles)

	expectedEdges := 0
	snapshot.OutboundEdges.Iterate(func(_ string, edges []link.ResolvedEdge) {
		expectedEdges += len(edges)
	})
	assert.Equal(t, expectedEdges, stats.Edges)
	assert.Equal(t, 0, stats.Dangling)
}

// TestStreamGraphStatsDangling: an edge referencing a missing node is
// counted as dangling.
func TestStreamGraphStatsDangling(t *testing.T) {
	dir := t.TempDir()
	g := &GraphJSON{
		SchemaVersion: CurrentSchemaVersion,
		Version:       1,
		Nodes: []GraphNodeJSON{
			{ID: "n1", Kind: "FUNCTION", Name: "f", FileSpec: LocationMetaJSON{Path: "src/a.go", LineStart: 1, LineEnd: 5}},
		},
		Edges: []GraphEdgeJSON{
			{SourceID: "n1", TargetID: "ghost", Type: "CALLS", LineNumber: 3},
		},
	}
	data, err := json.MarshalIndent(g, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "akg.json"), data, 0o644))

	stats, err := StreamGraphStats(dir)
	require.NoError(t, err)
	assert.Equal(t, 1, stats.NodeCount)
	assert.Equal(t, 1, stats.Edges)
	assert.Equal(t, 1, stats.Dangling)
}

// TestToNativeGraphParity: the in-memory conversion mirrors the persisted
// (deduplicated) form (AUDIT Issue 4 Phase 4A-1).
func TestToNativeGraphParity(t *testing.T) {
	dir := t.TempDir()
	tm, err := NewAKGTransactionManager(dir)
	require.NoError(t, err)
	defer tm.Close()

	payload := link.NewLinkOutput("testhash")
	payload.GraphNodes["a"] = &link.ResolvedNode{ID: "a", Kind: "FUNCTION", Name: "alpha", FileSpec: link.LocationMeta{Path: "src/a.go", LineStart: 1}}
	payload.GraphNodes["b"] = &link.ResolvedNode{ID: "b", Kind: "FUNCTION", Name: "beta", FileSpec: link.LocationMeta{Path: "src/b.go", LineStart: 1}}
	// Parallel edges between the same pair collapse to one canonical triple.
	payload.AddEdge("a", "b", link.EdgeCalls, 3)
	payload.AddEdge("a", "b", link.EdgeCalls, 9)
	require.NoError(t, tm.ExecuteDeltaTransaction(payload, []string{"src/a.go", "src/b.go"}))

	ng := tm.GetActiveSnapshot().ToNativeGraph()
	require.Len(t, ng.Nodes, 2)
	require.Len(t, ng.Edges, 1)
	assert.Equal(t, 9, ng.Edges[0].LineNumber)
	assert.Equal(t, "gm:calls", ng.Edges[0].Predicate)

	stats, err := StreamGraphStats(dir)
	require.NoError(t, err)
	assert.Equal(t, 2, stats.NodeCount)
	assert.Equal(t, 1, stats.Edges)
	assert.Equal(t, 0, stats.Dangling)
}
