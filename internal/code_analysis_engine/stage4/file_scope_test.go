package stage4

import (
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage2"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFileToSymbolEdgesReal (W1-10 / A-12): after BuildInitialNodes, every
// FILE node has real CONTAINS edges to the symbols defined in it, and the
// target nodes exist.
func TestFileToSymbolEdgesReal(t *testing.T) {
	root := stage3.NewDirectoryNode(".", "")
	progDir := stage3.NewDirectoryNode("internal", "internal")
	root.SubFolders["internal"] = progDir

	analyzeDir := stage3.NewDirectoryNode("analyze", "internal/tui/programs/analyze")
	progDir.SubFolders["tui"] = stage3.NewDirectoryNode("tui", "internal/tui")
	progDir.SubFolders["tui"].SubFolders["programs"] = stage3.NewDirectoryNode("programs", "internal/tui/programs")
	progDir.SubFolders["tui"].SubFolders["programs"].SubFolders["analyze"] = analyzeDir

	analyzeDir.Files["program.go"] = &stage3.FileBoundaryNode{
		FileName:     "program.go",
		RelativePath: "internal/tui/programs/analyze/program.go",
		Language:     "go",
		GASTRoot: &stage2.GASTNode{
			Type: stage2.GASTTypeDeclaration,
			Name: "Options",
			Kind: "struct",
			Children: []*stage2.GASTNode{
				{Type: stage2.GASTField, Name: "TargetDir"},
				{Type: stage2.GASTFunction, Name: "Run", Kind: "method", ReceiverType: "Options"},
			},
		},
	}

	stage3Out := &stage3.Stage3Output{
		RootNode:           root,
		CommitHash:         "HEAD",
		EntrypointRegistry: []string{},
	}

	cpg := BuildInitialNodes(stage3Out, nil)
	require.NotNil(t, cpg)

	fileID := "file:internal/tui/programs/analyze/program.go"
	fileNode, ok := cpg.GraphNodes[fileID]
	require.True(t, ok, "FILE node must exist")
	assert.Equal(t, "FILE", fileNode.Kind)

	outEdges := cpg.OutboundEdges[fileID]
	require.NotEmpty(t, outEdges, "FILE node must have outbound CONTAINS edges (A-12)")

	contained := map[string]bool{}
	for _, e := range outEdges {
		assert.Equal(t, EdgeContains, e.Type)
		contained[e.TargetID] = true
		_, exists := cpg.GetNode(e.TargetID)
		require.True(t, exists, "CONTAINS target %s must exist", e.TargetID)
	}
	assert.True(t, contained["internal/tui/programs/analyze/program.go::Options"], "type symbol contained")
	assert.True(t, contained["internal/tui/programs/analyze/program.go::Options::Run"], "method symbol contained")

	// Reverse direction: symbol → file inbound edge present.
	assert.NotEmpty(t, cpg.InboundEdges["internal/tui/programs/analyze/program.go::Options"])
}

// TestTouchesFile (W1-10, §9.1 file scoping): edge ↔ file association
// resolves via FileSpec.Path, Properties["file_path"], and normalizes
// Windows-style separators.
func TestTouchesFile(t *testing.T) {
	cpg := NewStage4Output("HEAD")
	cpg.GraphNodes["a.go::Thing"] = &ResolvedNode{
		ID:       "a.go::Thing",
		FileSpec: LocationMeta{Path: "src/a.go"},
	}
	cpg.GraphNodes["b.go::Thing"] = &ResolvedNode{
		ID:         "b.go::Thing",
		Properties: map[string]string{"file_path": "src\\b.go"},
	}
	cpg.GraphNodes["ext:x"] = &ResolvedNode{ID: "ext:x", Kind: "EXTERNAL_SDK"}

	edges := []ResolvedEdge{
		{SourceID: "a.go::Thing", TargetID: "b.go::Thing"},
		{SourceID: "ext:x", TargetID: "a.go::Thing"},
		{SourceID: "ext:x", TargetID: "missing.go::Nope"},
	}

	assert.True(t, touchesFile(edges[0], cpg, "src/a.go"))
	assert.True(t, touchesFile(edges[0], cpg, "src/b.go"))
	assert.True(t, touchesFile(edges[1], cpg, "src/a.go"))
	assert.False(t, touchesFile(edges[1], cpg, "src/c.go"))
	assert.False(t, touchesFile(edges[2], cpg, "src/a.go"))
	assert.False(t, touchesFile(edges[0], cpg, ""))
}

// TestTouchesFileSlashNormalized (W1-10): backslash paths on Windows and
// slash paths must compare equal.
func TestTouchesFileSlashNormalized(t *testing.T) {
	cpg := NewStage4Output("HEAD")
	cpg.GraphNodes["a.go::T"] = &ResolvedNode{
		ID:       "a.go::T",
		FileSpec: LocationMeta{Path: "internal\\util\\a.go"},
	}

	edge := ResolvedEdge{SourceID: "a.go::T", TargetID: "x"}
	assert.True(t, touchesFile(edge, cpg, "internal/util/a.go"))
	assert.True(t, touchesFile(edge, cpg, "internal\\util\\a.go"))
}
