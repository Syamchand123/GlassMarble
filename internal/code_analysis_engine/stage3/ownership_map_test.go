package stage3

import (
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildOwnershipMap(t *testing.T) {
	globalIndex := map[string][]*stage2.GASTNode{
		"src/auth.Service": {
			{
				Name:      "Service",
				Kind:      "struct",
				Namespace: "auth",
				Properties: map[string]string{
					"file_path": "src/auth/service.go",
				},
			},
		},
		"src/db.PostgresStore": {
			{
				Name:         "PostgresStore",
				Kind:         "struct",
				Namespace:    "db",
				ReceiverType: "PostgresStore",
				Properties: map[string]string{
					"file_path": "src/db/postgres.go",
				},
			},
		},
		"cmd/main": {
			{
				Name: "main",
				Kind: "func",
				Properties: map[string]string{
					"file_path": "cmd/main.go",
				},
			},
		},
	}

	wc := NewWorkspaceContext()
	wc.ModuleBoundaries = []string{"src/auth"}

	om := BuildOwnershipMap(globalIndex, wc)
	require.NotNil(t, om)

	// ByName
	require.Len(t, om.ByName["Service"], 1)
	assert.Equal(t, "src/auth.Service", om.ByName["Service"][0].FQN)
	assert.Equal(t, "src/auth/service.go", om.ByName["Service"][0].FilePath)
	assert.Equal(t, "struct", om.ByName["Service"][0].Kind)

	require.Len(t, om.ByName["PostgresStore"], 1)
	assert.Equal(t, "src/db.PostgresStore", om.ByName["PostgresStore"][0].FQN)
	assert.Equal(t, "PostgresStore", om.ByName["PostgresStore"][0].ReceiverType)

	require.Len(t, om.ByName["main"], 1)
	assert.Equal(t, "cmd/main", om.ByName["main"][0].FQN)

	// ByImport is keyed by the node's namespace
	require.Len(t, om.ByImport["auth"], 1)
	require.Len(t, om.ByImport["db"], 1)
	assert.NotContains(t, om.ByImport, "cmd")

	// Module grouping: inside a boundary -> "src/auth", outside -> "root"
	require.Len(t, om.ByHierarchy["src/auth"]["auth"]["src/auth/service.go"], 1)
	require.Len(t, om.ByHierarchy["root"]["db"]["src/db/postgres.go"], 1)
	// Package is inferred from the file path when Namespace is empty
	require.Len(t, om.ByHierarchy["root"]["cmd"]["cmd/main.go"], 1)
}

func TestBuildOwnershipMapNilIndex(t *testing.T) {
	om := BuildOwnershipMap(nil, NewWorkspaceContext())
	require.NotNil(t, om)
	assert.Empty(t, om.ByHierarchy)
	assert.Empty(t, om.ByName)
	assert.Empty(t, om.ByImport)
}

func TestBuildOwnershipMapNilProperties(t *testing.T) {
	globalIndex := map[string][]*stage2.GASTNode{
		"pkg.Thing": {
			{Name: "Thing", Namespace: "pkg"},
		},
	}

	om := BuildOwnershipMap(globalIndex, NewWorkspaceContext())
	require.NotNil(t, om)
	require.Len(t, om.ByName["Thing"], 1)
	// Missing file_path falls back to the "root" module and empty file key
	require.Len(t, om.ByHierarchy["root"]["pkg"][""], 1)
	require.Len(t, om.ByImport["pkg"], 1)
}
