package stage3

import (
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGraftFileNodeCreatesTree(t *testing.T) {
	root := NewDirectoryNode("root", ".")
	gastRoot := &stage2.GASTNode{Type: stage2.GASTFileRoot, Name: "c.go"}

	GraftFileNode(root, "a/b/c.go", gastRoot, []string{"fmt", "os"}, "go")

	require.NotNil(t, root.SubFolders["a"])
	require.NotNil(t, root.SubFolders["a"].SubFolders["b"])

	assert.Equal(t, "a", root.SubFolders["a"].FolderName)
	assert.Equal(t, "a", root.SubFolders["a"].RelativePath)
	assert.Equal(t, "b", root.SubFolders["a"].SubFolders["b"].FolderName)
	assert.Equal(t, "a/b", root.SubFolders["a"].SubFolders["b"].RelativePath)

	file := root.SubFolders["a"].SubFolders["b"].Files["c.go"]
	require.NotNil(t, file)
	assert.Equal(t, "c.go", file.FileName)
	assert.Equal(t, "a/b/c.go", file.RelativePath)
	assert.Equal(t, "go", file.Language)
	assert.Same(t, gastRoot, file.GASTRoot)
	assert.Equal(t, []string{"fmt", "os"}, file.LocalImports)
}

func TestGraftFileNodeRootFile(t *testing.T) {
	root := NewDirectoryNode("root", ".")
	gastRoot := &stage2.GASTNode{Type: stage2.GASTFileRoot, Name: "main.go"}

	GraftFileNode(root, "main.go", gastRoot, nil, "go")

	require.NotNil(t, root.Files["main.go"])
	assert.Equal(t, "main.go", root.Files["main.go"].RelativePath)
	assert.Equal(t, "main.go", root.Files["main.go"].FileName)
	assert.Same(t, gastRoot, root.Files["main.go"].GASTRoot)
	assert.Empty(t, root.SubFolders)
}

func TestGraftFileNodeOverwritesExistingFile(t *testing.T) {
	root := NewDirectoryNode("root", ".")
	GraftFileNode(root, "a/b/c.go", &stage2.GASTNode{Type: stage2.GASTFileRoot, Name: "c.go"}, []string{"fmt"}, "go")

	second := &stage2.GASTNode{Type: stage2.GASTFileRoot, Name: "c2.go"}
	GraftFileNode(root, "a/b/c.go", second, []string{"os"}, "python")

	file := root.SubFolders["a"].SubFolders["b"].Files["c.go"]
	require.NotNil(t, file)
	assert.Same(t, second, file.GASTRoot)
	assert.Equal(t, "python", file.Language)
	assert.Equal(t, []string{"os"}, file.LocalImports)
}

func TestGraftFileNodeInvalidInputs(t *testing.T) {
	root := NewDirectoryNode("root", ".")
	assert.NotPanics(t, func() {
		GraftFileNode(nil, "a/b/c.go", &stage2.GASTNode{}, nil, "go")
		GraftFileNode(root, "", &stage2.GASTNode{}, nil, "go")
	})
	assert.Empty(t, root.SubFolders)
	assert.Empty(t, root.Files)
}

func TestPruneFileNode(t *testing.T) {
	root := NewDirectoryNode("root", ".")
	GraftFileNode(root, "a/b/c.go", &stage2.GASTNode{Type: stage2.GASTFileRoot, Name: "c.go"}, nil, "go")

	removed := PruneFileNode(root, "a/b/c.go")
	assert.True(t, removed)
	assert.Nil(t, root.SubFolders["a"])
	assert.Empty(t, root.SubFolders)
	assert.Empty(t, root.Files)
}

func TestPruneFileNodeNonexistent(t *testing.T) {
	root := NewDirectoryNode("root", ".")
	GraftFileNode(root, "a/b/c.go", &stage2.GASTNode{Type: stage2.GASTFileRoot, Name: "c.go"}, nil, "go")

	assert.False(t, PruneFileNode(root, "a/b/nope.go"))
	assert.False(t, PruneFileNode(root, "x/y/z.go"))
	require.NotNil(t, root.SubFolders["a"].SubFolders["b"].Files["c.go"])
}

func TestPruneFileNodeInvalidInputs(t *testing.T) {
	root := NewDirectoryNode("root", ".")
	assert.False(t, PruneFileNode(nil, "a/b/c.go"))
	assert.False(t, PruneFileNode(root, ""))
}

func TestPruneLeavesSibling(t *testing.T) {
	root := NewDirectoryNode("root", ".")
	GraftFileNode(root, "a/b/c.go", &stage2.GASTNode{Type: stage2.GASTFileRoot, Name: "c.go"}, nil, "go")
	GraftFileNode(root, "a/b/d.go", &stage2.GASTNode{Type: stage2.GASTFileRoot, Name: "d.go"}, nil, "go")

	removed := PruneFileNode(root, "a/b/c.go")
	assert.True(t, removed)

	b := root.SubFolders["a"].SubFolders["b"]
	require.NotNil(t, b)
	assert.Nil(t, b.Files["c.go"])
	require.NotNil(t, b.Files["d.go"])
}
