package akg

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIncrementalTracker_DetectUnchangedFiles(t *testing.T) {
	tmpDir := t.TempDir()
	f1 := filepath.Join(tmpDir, "a.go")
	f2 := filepath.Join(tmpDir, "b.go")

	require.NoError(t, os.WriteFile(f1, []byte("package a"), 0644))
	require.NoError(t, os.WriteFile(f2, []byte("package b"), 0644))

	tracker := NewIncrementalTracker(tmpDir)
	info1, err := os.Stat(f1)
	require.NoError(t, err)
	tracker.LastModified[f1] = info1.ModTime()

	unchanged, modified := tracker.DetectUnchangedFiles([]string{f1, f2})
	assert.Contains(t, unchanged, f1)
	assert.Contains(t, modified, f2)
}

func TestSerializeGraphDiffToTurtle(t *testing.T) {
	diff := &GraphDiff{
		NodesAdded: []DiffNode{
			{ID: "pkg/a.go::A", Kind: "Struct", Name: "A"},
		},
		EdgesAdded: []DiffEdge{
			{SourceID: "pkg/a.go::A", TargetID: "pkg/b.go::B", Type: "calls", Line: 10},
		},
		NodesRemoved: []DiffNode{
			{ID: "pkg/c.go::C", Kind: "Struct", Name: "C"},
		},
	}

	ttl := SerializeGraphDiffToTurtle(diff, 2)
	assert.Contains(t, ttl, "Delta Append for Version 2")
	assert.Contains(t, ttl, "Deleted")
	assert.Contains(t, ttl, "Struct")
	assert.Contains(t, ttl, "<< <http://glassmarble.org/node/pkg/a.go::A>")
}
