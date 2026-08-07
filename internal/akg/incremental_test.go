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
