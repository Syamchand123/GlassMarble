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

func TestBuildOwnershipMapNilSafeGetters(t *testing.T) {
	var om *OwnershipMap
	assert.Equal(t, "", om.GetOwner("pkg.Thing"))
	assert.Nil(t, om.GetMembers("pkg.Thing"))
	assert.Equal(t, "", (&OwnershipMap{}).GetOwner("pkg.Thing"))
	assert.Nil(t, (&OwnershipMap{}).GetMembers("pkg.Thing"))
}

func TestBuildOwnershipMapBackbone(t *testing.T) {
	// v2 (§5.3.1, A-16): types own their field/method children.
	// Methods are re-parented under their owner type by the stage2
	// normalizer, so the ownership backbone is derived from the
	// GAST parent-child structure.
	globalIndex := map[string][]*stage2.GASTNode{
		"pkg.Service": {
			{
				Name: "Service",
				Kind: "struct",
				Type: stage2.GASTTypeDeclaration,
				Properties: map[string]string{
					"fully_qualified_name": "pkg.Service",
				},
				Children: []*stage2.GASTNode{
					{
						Name: "Port",
						Type: stage2.GASTField,
						Properties: map[string]string{
							"fully_qualified_name": "pkg.Service.Port",
						},
					},
					{
						Name: "Start",
						Type: stage2.GASTFunction,
						Kind: "method",
						Properties: map[string]string{
							"fully_qualified_name": "pkg.Service.Start",
						},
					},
				},
			},
		},
	}

	om := BuildOwnershipMap(globalIndex, NewWorkspaceContext())

	require.Contains(t, om.MembersOf["pkg.Service"], "pkg.Service.Port")
	require.Contains(t, om.MembersOf["pkg.Service"], "pkg.Service.Start")
	require.Len(t, om.MembersOf["pkg.Service"], 2)
	assert.Equal(t, "pkg.Service", om.GetOwner("pkg.Service.Port"))
	assert.Equal(t, "pkg.Service", om.GetOwner("pkg.Service.Start"))
	assert.Empty(t, om.GetOwner("pkg.Unrelated"))
	assert.Nil(t, om.GetMembers("pkg.Unrelated"))
}

func TestBuildOwnershipMapCanonicalKey(t *testing.T) {
	// §5.3.1: canonical IDs are the primary ownership key when present.
	globalIndex := map[string][]*stage2.GASTNode{
		"pkg.Service": {
			{
				Name: "Service",
				Kind: "struct",
				Type: stage2.GASTTypeDeclaration,
				Properties: map[string]string{
					"canonical_id":         "type:src%2Fpkg:service.go::Service",
					"fully_qualified_name": "pkg.Service",
				},
				Children: []*stage2.GASTNode{
					{
						Name: "Start",
						Type: stage2.GASTFunction,
						Kind: "method",
						Properties: map[string]string{
							"canonical_id": "func:src%2Fpkg:service.go:Service:Start",
						},
					},
				},
			},
		},
	}

	om := BuildOwnershipMap(globalIndex, NewWorkspaceContext())

	require.Contains(t, om.MembersOf["type:src%2Fpkg:service.go::Service"], "func:src%2Fpkg:service.go:Service:Start")
	assert.Equal(t, "type:src%2Fpkg:service.go::Service", om.GetOwner("func:src%2Fpkg:service.go:Service:Start"))
}
