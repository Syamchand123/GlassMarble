package layout

import (
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/product/ont"
	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/types"
	"github.com/stretchr/testify/assert"
)

func TestClassifyEdges_SameFileClassesPreserved(t *testing.T) {
	sub := &types.VirtualSubgraph{
		Nodes: map[string]*types.TTLNode{
			"pkg/foo.go::ClassA": {ID: "pkg/foo.go::ClassA", Kind: "STRUCT", Name: "ClassA"},
			"pkg/foo.go::ClassB": {ID: "pkg/foo.go::ClassB", Kind: "STRUCT", Name: "ClassB"},
		},
		Edges: []types.NativeEdge{
			{SourceID: "pkg/foo.go::ClassA", TargetID: "pkg/foo.go::ClassB", Predicate: ont.PredCalls},
			{SourceID: "pkg/foo.go::ClassA", TargetID: "pkg/foo.go::ClassB", Predicate: ont.PredCalls},
		},
	}

	proj := ClassifyEdges(sub)
	assert.Equal(t, 2, proj.EdgeCount)
	assert.Len(t, proj.ClassRelations, 1)

	rel := proj.ClassRelations[0]
	assert.Equal(t, "pkg/foo.go::ClassA", rel.SourceClassID)
	assert.Equal(t, "pkg/foo.go::ClassB", rel.TargetClassID)
	assert.Equal(t, 2, rel.Count)
	assert.Equal(t, "usage", rel.Kind)
}

func TestClassifyEdges_SelfLoopsDropped(t *testing.T) {
	sub := &types.VirtualSubgraph{
		Nodes: map[string]*types.TTLNode{
			"pkg/foo.go::ClassA": {ID: "pkg/foo.go::ClassA", Kind: "STRUCT", Name: "ClassA"},
		},
		Edges: []types.NativeEdge{
			{SourceID: "pkg/foo.go::ClassA", TargetID: "pkg/foo.go::ClassA", Predicate: ont.PredCalls},
		},
	}

	proj := ClassifyEdges(sub)
	assert.Empty(t, proj.ClassRelations)
}
