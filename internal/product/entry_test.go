package product

import (
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveEntryPoint_ExplicitSymbol(t *testing.T) {
	graph := &types.VirtualSubgraph{
		Nodes: map[string]*types.TTLNode{
			"cmd/main.go::main":    {Name: "main", IsEntrypoint: true},
			"internal/foo.go::Foo": {Name: "Foo"},
		},
	}

	entry, err := ResolveEntryPoint(graph, types.QueryOptions{EntryPointID: "internal/foo.go::Foo"})
	require.NoError(t, err)
	assert.Equal(t, "internal/foo.go::Foo", entry)
}

func TestResolveEntryPoint_AutoEntry(t *testing.T) {
	graph := &types.VirtualSubgraph{
		Nodes: map[string]*types.TTLNode{
			"cmd/main.go::main":    {Name: "main", IsEntrypoint: true},
			"internal/foo.go::Foo": {Name: "Foo"},
		},
	}

	entry, err := ResolveEntryPoint(graph, types.QueryOptions{})
	require.NoError(t, err)
	assert.Equal(t, "cmd/main.go::main", entry)
}
