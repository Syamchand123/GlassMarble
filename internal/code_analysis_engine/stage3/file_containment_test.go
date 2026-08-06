package stage3

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage1"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(wd, "go.mod")); err == nil {
			return wd
		}
		parent := filepath.Dir(wd)
		if parent == wd {
			t.Fatal("repo root (go.mod) not found")
		}
		wd = parent
	}
}

// loadRealFile copies a repo file into the temp corpus as a "fixture".
func loadRealFile(t *testing.T, files map[string]string, repoRel, corpusRel string) {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(repoRoot(t), filepath.FromSlash(repoRel)))
	if err != nil {
		t.Fatalf("read %s: %v", repoRel, err)
	}
	files[corpusRel] = string(content)
}

func fixtureFiles(t *testing.T) map[string]string {
	files := make(map[string]string)
	loadRealFile(t, files, "internal/tui/programs/analyze/program.go", "internal/tui/programs/analyze/program.go")
	loadRealFile(t, files, "internal/ai_engine/provider/types.go", "internal/ai_engine/provider/types.go")
	loadRealFile(t, files, "internal/ai_engine/agent/dispatcher.go", "internal/ai_engine/agent/dispatcher.go")
	return files
}

func runStage3(t *testing.T, files map[string]string) *Stage3Output {
	t.Helper()
	dir := t.TempDir()
	for rel, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	out, err := stage1.RunIngestion(stage1.Config{RootDir: dir})
	if err != nil {
		t.Fatalf("RunIngestion: %v", err)
	}
	payload, err := stage2.Normalize(out, "acceptance")
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	aggregated, err := Aggregate(payload, nil, dir)
	if err != nil {
		t.Fatalf("Aggregate: %v", err)
	}
	return aggregated
}

// TestFileContainment (§5.3.5 / W1-10 / A-12): every file's member list is
// non-empty for the fixture corpus, and each member is resolvable in the
// global definition index (real containment, no dead-end File nodes).
func TestFileContainment(t *testing.T) {
	out := runStage3(t, fixtureFiles(t))

	require.NotEmpty(t, out.FileToMembers)
	for rel, members := range out.FileToMembers {
		require.NotEmpty(t, members, "file %s must have members", rel)
		for _, m := range members {
			require.NotEmpty(t, out.GlobalDefinitionIndex[m],
				"member %q of %s must exist in the global definition index", m, rel)
		}
	}

	// Every processed file appears in the member map.
	require.Contains(t, out.FileToMembers, "internal/tui/programs/analyze/program.go")
	require.Contains(t, out.FileToMembers, "internal/ai_engine/provider/types.go")
	require.Contains(t, out.FileToMembers, "internal/ai_engine/agent/dispatcher.go")
}

// TestFileToMembersRoundTrip (§5.3.5): FileToSymbols and FileToMembers
// agree per file — every member key is also a known symbol key.
func TestFileToMembersRoundTrip(t *testing.T) {
	out := runStage3(t, fixtureFiles(t))

	for rel, members := range out.FileToMembers {
		symbols := out.FileToSymbols[rel]
		for _, m := range members {
			require.Contains(t, symbols, m, "member %q of %s not in FileToSymbols", m, rel)
		}
	}
}

// TestExternalIDs (§5.3.5 / W1-09): an aliased import of the workspace's
// own module path produces ext:<module-relative> with the alias property.
func TestExternalIDs(t *testing.T) {
	dir := t.TempDir()
	write := func(rel, content string) {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	write("go.mod", "module github.com/Syamchand123/GlassMarble\n\ngo 1.22\n")
	write("cmd/diff/main.go", `package main

import (
	"fmt"
	akgerrs "github.com/Syamchand123/GlassMarble/internal/errors"
)

func main() {
	fmt.Println(akgerrs.ErrMalformedDiff)
}
`)

	out, err := stage1.RunIngestion(stage1.Config{RootDir: dir})
	require.NoError(t, err)
	payload, err := stage2.Normalize(out, "acceptance")
	require.NoError(t, err)
	aggregated, err := Aggregate(payload, nil, dir)
	require.NoError(t, err)

	require.Equal(t, "github.com/Syamchand123/GlassMarble", aggregated.WorkspaceCtx.ModulePrefix)

	key := ExternalKey("internal/errors")
	node, ok := aggregated.ExternalDependencies[key]
	require.True(t, ok, "ext:internal/errors must be indexed for the aliased import")
	assert.Equal(t, "akgerrs", node.Properties["alias"])
	assert.Equal(t, "internal/errors", node.Name)
	assert.Equal(t, "true", node.Properties["gm:isExternal"])
	assert.Equal(t, "true", node.Properties["is_external"])
	// No un-aliased duplicate of the same path.
	assert.NotContains(t, aggregated.ExternalDependencies, "github.com/Syamchand123/GlassMarble/internal/errors")
	assert.Equal(t, "true", aggregated.ExternalDependencies[key].Properties["gm:isExternal"])
}

// TestOwnershipMap (§5.3.5 / W1-07): owner/members round-trip on a real
// multi-file package — every method of a fixture type resolves to its
// owning type and back.
func TestOwnershipMap(t *testing.T) {
	out := runStage3(t, fixtureFiles(t))

	om := BuildOwnershipMap(out.GlobalDefinitionIndex, out.WorkspaceCtx)

	// Dispatcher (dispatcher.go) owns its 5 methods: maxBytes, Dispatch,
	// dispatch, find, render — all re-parented by the stage2 normalizer.
	// Ownership keys use resolutionKey semantics (FQN when present), so the
	// type key is its fully_qualified_name index key.
	var dispatcherKey string
	for key, nodes := range out.GlobalDefinitionIndex {
		for _, n := range nodes {
			if n.Name == "Dispatcher" && n.Kind == "struct" && key == n.Properties["fully_qualified_name"] {
				dispatcherKey = key
			}
		}
	}
	require.NotEmpty(t, dispatcherKey, "Dispatcher type must be indexed")

	members := om.GetMembers(dispatcherKey)
	require.NotEmpty(t, members, "Dispatcher must own members")

	want := []string{"maxBytes", "Dispatch", "dispatch", "find", "render"}
	for _, m := range want {
		var memberKey string
		for _, k := range members {
			if strings.HasSuffix(k, "."+m) || strings.HasSuffix(k, "::"+m) || k == m {
				memberKey = k
				break
			}
		}
		require.NotEmpty(t, memberKey, "Dispatcher.%s must be a member", m)
		assert.Equal(t, dispatcherKey, om.GetOwner(memberKey), "owner round-trip for Dispatcher.%s", m)
	}

	// Options (program.go) owns its fields and Run method.
	var optionsKey string
	for key, nodes := range out.GlobalDefinitionIndex {
		for _, n := range nodes {
			if n.Name == "Options" && n.Kind == "struct" && key == n.Properties["fully_qualified_name"] {
				optionsKey = key
			}
		}
	}
	require.NotEmpty(t, optionsKey, "Options type must be indexed")
	optionsMembers := om.GetMembers(optionsKey)
	require.NotEmpty(t, optionsMembers)
	joined := strings.Join(optionsMembers, ",")
	for _, field := range []string{"TargetDir", "Full", "Workers"} {
		assert.Contains(t, joined, field, "Options must own field %s", field)
	}
}
