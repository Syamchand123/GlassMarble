package aggregate

import (
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/normalize"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFindEntryPoints(t *testing.T) {
	output := &AggregateOutput{
		GlobalDefinitionIndex: map[string][]*normalize.GASTNode{
			"cmd.main": {
				{
					Name: "main",
					Kind: "func",
					Properties: map[string]string{
						"file_path": "cmd/main.go",
					},
				},
			},
			"app.Server": {
				{
					Name: "Server",
					Kind: "func",
					Properties: map[string]string{
						"primitive": "EXPOSES_ENDPOINT",
						"file_path": "app/server.go",
					},
				},
			},
			"app.Helper": {
				{
					Name: "Helper",
					Kind: "func",
					Properties: map[string]string{
						"file_path": "app/helper.go",
					},
				},
			},
		},
	}

	eps := FindEntryPoints(output)
	require.Len(t, eps, 2)

	byFQN := map[string]EntryPoint{}
	for _, ep := range eps {
		byFQN[ep.FQN] = ep
	}

	assert.Equal(t, EntryPointMain, byFQN["cmd.main"].Kind)
	assert.Equal(t, "cmd/main.go", byFQN["cmd.main"].FilePath)
	assert.Equal(t, EntryPointHandler, byFQN["app.Server"].Kind)
	assert.NotContains(t, byFQN, "app.Helper")
}

func TestFindEntryPointsNilOutput(t *testing.T) {
	assert.Empty(t, FindEntryPoints(nil))
	assert.Empty(t, FindEntryPoints(&AggregateOutput{}))
}

func TestFindEntryPointsStableOrder(t *testing.T) {
	output := &AggregateOutput{
		GlobalDefinitionIndex: map[string][]*normalize.GASTNode{
			"z.last":    {{Name: "last", Kind: "func"}},
			"a.first":   {{Name: "first", Kind: "func", Properties: map[string]string{"primitive": "ROUTER"}}},
			"m.main":    {{Name: "main", Kind: "func"}},
			"b.handler": {{Name: "handler", Kind: "func", Properties: map[string]string{"primitive": "NETWORK_IO"}}},
		},
	}

	eps := FindEntryPoints(output)
	require.Len(t, eps, 3) // "z.last" is neither main/init nor a handler primitive
	for i := 1; i < len(eps); i++ {
		assert.True(t, eps[i-1].FQN < eps[i].FQN, "must be sorted: %v", eps)
	}
}

func TestIndexEntrypointsStampsGmMarkers(t *testing.T) {
	output := &AggregateOutput{
		GlobalDefinitionIndex: map[string][]*normalize.GASTNode{
			"cmd.main": {
				{
					Name: "main",
					Kind: "func",
					Properties: map[string]string{
						"file_path": "cmd/main.go",
					},
				},
			},
			"app.Server": {
				{
					Name: "Server",
					Kind: "func",
					Properties: map[string]string{
						"primitive": "EXPOSES_ENDPOINT",
					},
				},
			},
		},
		EntrypointRegistry: make([]string, 0),
	}

	IndexEntrypoints(output)

	require.Len(t, output.EntrypointRegistry, 2)
	main := output.GlobalDefinitionIndex["cmd.main"][0]
	server := output.GlobalDefinitionIndex["app.Server"][0]
	assert.Equal(t, "true", main.Properties["is_entrypoint"])
	assert.Equal(t, "true", main.Properties["gm:isMain"]) // W1-09 marker
	assert.Equal(t, "true", server.Properties["is_entrypoint"])
	assert.NotContains(t, server.Properties, "gm:isMain")
}
