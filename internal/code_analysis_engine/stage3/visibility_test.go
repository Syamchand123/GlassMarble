package stage3

import (
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestComputeVisibilityEnclaveScopes(t *testing.T) {
	wc := NewWorkspaceContext()
	wc.ModuleBoundaries = []string{"src/core"}

	tests := []struct {
		name         string
		visibility   string
		nodeName     string
		fileRelPath  string
		wantScope    string
		wantBoundary string
		wantSet      bool
	}{
		{name: "public", visibility: "public", nodeName: "Exported", fileRelPath: "src/core/auth/service.go", wantScope: "Public"},
		{name: "exported", visibility: "exported", nodeName: "Exported", fileRelPath: "src/core/auth/service.go", wantScope: "Public"},
		{name: "protected", visibility: "protected", nodeName: "Helper", fileRelPath: "src/core/auth/service.go", wantScope: "Protected", wantBoundary: "src/core/auth", wantSet: true},
		{name: "packageprivate", visibility: "packageprivate", nodeName: "packageScope", fileRelPath: "src/core/auth/service.go", wantScope: "PackagePrivate", wantBoundary: "src/core/auth", wantSet: true},
		{name: "internal", visibility: "internal", nodeName: "internalScope", fileRelPath: "src/core/auth/service.go", wantScope: "PackagePrivate", wantBoundary: "src/core/auth", wantSet: true},
		{name: "moduleinternal", visibility: "moduleinternal", nodeName: "ModuleScope", fileRelPath: "src/core/auth/service.go", wantScope: "ModuleInternal", wantBoundary: "src/core", wantSet: true},
		{name: "private", visibility: "private", nodeName: "secret", fileRelPath: "src/core/auth/service.go", wantScope: "StrictPrivate", wantBoundary: "src/core/auth/service.go", wantSet: true},
		{name: "strictprivate", visibility: "strictprivate", nodeName: "secret", fileRelPath: "src/core/auth/service.go", wantScope: "StrictPrivate", wantBoundary: "src/core/auth/service.go", wantSet: true},
		{name: "visibility case-insensitive", visibility: "PUBLIC", nodeName: "Exported", fileRelPath: "src/core/auth/service.go", wantScope: "Public"},
		{name: "fallback lowercase", visibility: "", nodeName: "myFunc", fileRelPath: "src/core/auth/service.go", wantScope: "PackagePrivate", wantBoundary: "src/core/auth", wantSet: true},
		{name: "fallback underscore prefix", visibility: "", nodeName: "_internalVar", fileRelPath: "src/core/auth/service.go", wantScope: "PackagePrivate", wantBoundary: "src/core/auth", wantSet: true},
		{name: "fallback uppercase", visibility: "", nodeName: "ExportedThing", fileRelPath: "src/core/auth/service.go", wantScope: "Public"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node := &stage2.GASTNode{
				Type:       stage2.GASTFunction,
				Name:       tt.nodeName,
				Visibility: tt.visibility,
			}

			ComputeVisibilityEnclave(node, tt.fileRelPath, wc)

			require.NotNil(t, node.Properties)
			assert.Equal(t, tt.wantScope, node.Properties["namespace_scope"], "namespace_scope")
			if tt.wantSet {
				assert.Equal(t, tt.wantBoundary, node.Properties["local_boundary"], "local_boundary")
			} else {
				_, ok := node.Properties["local_boundary"]
				assert.False(t, ok, "local_boundary should not be set")
			}
		})
	}
}

func TestComputeVisibilityEnclaveRecursive(t *testing.T) {
	wc := NewWorkspaceContext()
	root := &stage2.GASTNode{
		Type:       stage2.GASTTypeDeclaration,
		Name:       "Service",
		Visibility: "public",
		Children: []*stage2.GASTNode{
			{Type: stage2.GASTFunction, Name: "handle", Visibility: "protected"},
			{Type: stage2.GASTFunction, Name: "helper", Visibility: "private"},
		},
	}

	ComputeVisibilityEnclave(root, "src/core/service.go", wc)

	assert.Equal(t, "Public", root.Properties["namespace_scope"])
	assert.Equal(t, "Protected", root.Children[0].Properties["namespace_scope"])
	assert.Equal(t, "StrictPrivate", root.Children[1].Properties["namespace_scope"])
}

func TestComputeVisibilityEnclaveNilNode(t *testing.T) {
	assert.NotPanics(t, func() {
		ComputeVisibilityEnclave(nil, "src/core/service.go", NewWorkspaceContext())
	})
}
