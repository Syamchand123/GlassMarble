package stage3

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScanWorkspaceGoMod(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module github.com/acme/widget\n\ngo 1.24\n"), 0644))

	wc := NewWorkspaceContext()
	wc.ScanWorkspace(root)

	assert.Equal(t, "github.com/acme/widget", wc.ModulePrefix)
}

func TestScanWorkspaceGoWork(t *testing.T) {
	root := t.TempDir()
	goWork := "go 1.24\n\nuse (\n\t./services/a\n\t./services/b\n)\n"
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.work"), []byte(goWork), 0644))

	wc := NewWorkspaceContext()
	wc.ScanWorkspace(root)

	assert.Contains(t, wc.ModuleBoundaries, "services/a")
	assert.Contains(t, wc.ModuleBoundaries, "services/b")
}

func TestScanWorkspaceGoWorkSingleLineUse(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.work"), []byte("go 1.24\n\nuse ./services/c\n"), 0644))

	wc := NewWorkspaceContext()
	wc.ScanWorkspace(root)

	assert.Contains(t, wc.ModuleBoundaries, "services/c")
}

func TestScanWorkspaceTsconfigAliases(t *testing.T) {
	root := t.TempDir()
	tsconfig := `{
  "compilerOptions": {
    "paths": {
      "@utils/*": ["src/utils/*"],
      "@components/*": ["src/components/*"],
      "@lib": ["src/lib/index.ts"]
    }
  }
}`
	require.NoError(t, os.WriteFile(filepath.Join(root, "tsconfig.json"), []byte(tsconfig), 0644))

	wc := NewWorkspaceContext()
	wc.ScanWorkspace(root)

	assert.Equal(t, "src/utils", wc.Aliases["@utils"])
	assert.Equal(t, "src/components", wc.Aliases["@components"])
	assert.Equal(t, "src/lib/index.ts", wc.Aliases["@lib"])
}

func TestScanWorkspaceCargoMembers(t *testing.T) {
	root := t.TempDir()
	cargo := "[workspace]\nresolver = \"2\"\nmembers = [\"crates/*\", \"libs/core\"]\n"
	require.NoError(t, os.WriteFile(filepath.Join(root, "Cargo.toml"), []byte(cargo), 0644))

	wc := NewWorkspaceContext()
	wc.ScanWorkspace(root)

	assert.Contains(t, wc.ModuleBoundaries, "crates")
	assert.Contains(t, wc.ModuleBoundaries, "libs/core")
}

func TestScanWorkspaceNestedModuleDetected(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module github.com/acme/widget\n"), 0644))

	nestedDir := filepath.Join(root, "plugins", "nested")
	require.NoError(t, os.MkdirAll(nestedDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(nestedDir, "go.mod"), []byte("module github.com/acme/nested\n"), 0644))

	wc := NewWorkspaceContext()
	wc.ScanWorkspace(root)

	assert.Contains(t, wc.ModuleBoundaries, "plugins/nested")
}

func TestScanWorkspaceEmptyDir(t *testing.T) {
	root := t.TempDir()

	wc := NewWorkspaceContext()
	wc.ScanWorkspace(root)

	assert.Equal(t, "", wc.ModulePrefix)
	assert.Empty(t, wc.ModuleBoundaries)
	assert.Empty(t, wc.Aliases)
}

func TestGetModuleBoundaryLongestPrefix(t *testing.T) {
	wc := NewWorkspaceContext()
	wc.ModuleBoundaries = []string{"a", "a/b"}

	assert.Equal(t, "a/b", wc.GetModuleBoundary("a/b/c.go"))
	assert.Equal(t, "a", wc.GetModuleBoundary("a/main.go"))
	assert.Equal(t, "", wc.GetModuleBoundary("b/c.go"))
	assert.Equal(t, "", wc.GetModuleBoundary(""))
}
