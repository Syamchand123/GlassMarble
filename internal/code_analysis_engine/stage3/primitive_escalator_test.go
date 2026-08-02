package stage3

import (
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage2"
	"github.com/stretchr/testify/assert"
)

func gastRootWithPrimitive(prim string) *stage2.GASTNode {
	return &stage2.GASTNode{
		Type: stage2.GASTFileRoot,
		Children: []*stage2.GASTNode{
			{
				Type: stage2.GASTCallExpression,
				Name: "doThing",
				Properties: map[string]string{
					"primitive": prim,
				},
			},
		},
	}
}

func TestEscalatePrimitivesZones(t *testing.T) {
	root := NewDirectoryNode("root", ".")

	GraftFileNode(root, "security/auth.go", gastRootWithPrimitive("SECURITY_SINK"), nil, "go")
	GraftFileNode(root, "security/crypto.go", gastRootWithPrimitive("CRYPTO_OPS"), nil, "go")
	GraftFileNode(root, "db/store.go", gastRootWithPrimitive("DATABASE_IO"), nil, "go")
	GraftFileNode(root, "net/client.go", gastRootWithPrimitive("network_io"), nil, "go")
	GraftFileNode(root, "net/http.go", gastRootWithPrimitive("  NETWORK_IO  "), nil, "go")

	emptyDir := NewDirectoryNode("empty", "empty")
	root.SubFolders["empty"] = emptyDir

	counts := EscalatePrimitives(root)

	assert.Equal(t, "SECURITY_ZONE", root.SubFolders["security"].PrimitiveZone)
	assert.Equal(t, "DATABASE_ZONE", root.SubFolders["db"].PrimitiveZone)
	assert.Equal(t, "NETWORK_IO_ZONE", root.SubFolders["net"].PrimitiveZone)
	assert.Equal(t, "", emptyDir.PrimitiveZone)

	// Security is the most infectious, so the root inherits SECURITY_ZONE.
	assert.Equal(t, "SECURITY_ZONE", root.PrimitiveZone)

	// Counts aggregate bottom-up and primitive names are normalized (lowercase
	// and padded values become UPPERCASE).
	assert.Equal(t, 2, counts["NETWORK_IO"])
	assert.Equal(t, 1, counts["SECURITY_SINK"])
	assert.Equal(t, 1, counts["DATABASE_IO"])
}

func TestEscalatePrimitivesSecurityPriority(t *testing.T) {
	root := NewDirectoryNode("root", ".")
	GraftFileNode(root, "mixed/app.go", gastRootWithPrimitive("DATABASE_IO"), nil, "go")
	GraftFileNode(root, "mixed/handler.go", gastRootWithPrimitive("SECURITY_SINK"), nil, "go")

	EscalatePrimitives(root)

	assert.Equal(t, "SECURITY_ZONE", root.SubFolders["mixed"].PrimitiveZone)
}

func TestEscalatePrimitivesDatabasePriority(t *testing.T) {
	root := NewDirectoryNode("root", ".")
	GraftFileNode(root, "db/model.go", gastRootWithPrimitive("ORM_MODEL"), nil, "go")

	EscalatePrimitives(root)

	assert.Equal(t, "DATABASE_ZONE", root.SubFolders["db"].PrimitiveZone)
}

func TestEscalatePrimitivesNil(t *testing.T) {
	assert.Nil(t, EscalatePrimitives(nil))
}

func TestEscalatePrimitivesEmptyTree(t *testing.T) {
	root := NewDirectoryNode("root", ".")

	counts := EscalatePrimitives(root)

	assert.NotNil(t, counts)
	assert.Empty(t, counts)
	assert.Equal(t, "", root.PrimitiveZone)
}
