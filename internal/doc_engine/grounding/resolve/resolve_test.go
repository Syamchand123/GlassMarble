package resolve

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/link"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// buildResolveTestGraph mirrors grounding/collector_test.go buildTestGraph:
// a fake AKG graph with known node IDs, files, and line ranges.
func buildResolveTestGraph() *akg.CodePropertyGraph {
	g := akg.NewCodePropertyGraph("commit-resolve-test")
	put := func(id, name, path string, start, end int) {
		g.Nodes = g.Nodes.Set(id, &link.ResolvedNode{
			ID:   id,
			Name: name,
			Kind: "FUNCTION",
			FileSpec: link.LocationMeta{
				Path:      path,
				LineStart: start,
				LineEnd:   end,
			},
		})
	}
	put("internal/auth/jwt.go::ValidateToken", "ValidateToken", "internal/auth/jwt.go", 25, 50)
	put("internal/auth/jwt.go::parseRaw", "parseRaw", "internal/auth/jwt.go", 55, 65)
	return g
}

// writeSCIPSidecar writes a JSON sidecar under root/.glassmarble/scip/.
func writeSCIPSidecar(t *testing.T, root, content string) {
	t.Helper()
	dir := filepath.Join(root, ".glassmarble", "scip")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "index.json"), []byte(content), 0o644))
}

func TestResolveBatch_ASTFallback(t *testing.T) {
	g := buildResolveTestGraph()
	// Empty temp root: no SCIP sidecar, and the graph's files do not exist
	// on disk, so the LSP stage always skips — assertions are stable
	// whether or not gopls is installed.
	root := t.TempDir()

	out := ResolveBatch([]string{
		"internal/auth/jwt.go::ValidateToken",
		"internal/auth/jwt.go::parseRaw",
		"internal/auth/jwt.go::NoSuchFunc",
	}, g, root)
	require.Len(t, out, 3)

	got := out["internal/auth/jwt.go::ValidateToken"]
	assert.Equal(t, "internal/auth/jwt.go::ValidateToken", got.FQN)
	assert.Equal(t, "internal/auth/jwt.go", got.File)
	assert.Equal(t, 25, got.Line)
	assert.Equal(t, 50, got.EndLine)
	assert.Equal(t, "ast", got.Provenance)

	helper := out["internal/auth/jwt.go::parseRaw"]
	assert.Equal(t, "ast", helper.Provenance)
	assert.Equal(t, 55, helper.Line)

	// Misses are reported as "unresolved", never omitted.
	miss := out["internal/auth/jwt.go::NoSuchFunc"]
	assert.Equal(t, "internal/auth/jwt.go::NoSuchFunc", miss.FQN)
	assert.Equal(t, "unresolved", miss.Provenance)
}

func TestResolveBatch_ASTSuffixMatch(t *testing.T) {
	g := buildResolveTestGraph()
	root := t.TempDir()

	// Bare short name: no "::" file prefix, so LSP skips (graph node "ValidateToken"
	// does not exist for lspTarget) and AST answers via short-name match.
	out := ResolveBatch([]string{"ValidateToken"}, g, root)
	require.Len(t, out, 1)
	got := out["ValidateToken"]
	assert.Equal(t, "ast", got.Provenance)
	assert.Equal(t, "internal/auth/jwt.go", got.File)
	assert.Equal(t, 25, got.Line)
	assert.Equal(t, 50, got.EndLine)
}

func TestSCIP_JSONSidecar(t *testing.T) {
	root := t.TempDir()
	writeSCIPSidecar(t, root, `[
		{"fqn":"pkg/a.go::Alpha","file":"pkg/a.go","line":10,"end_line":20},
		{"fqn":"other/b.go::Beta","file":"other/b.go","line":3,"endLine":5}
	]`)

	// Nil graph: anything the sidecar misses must come back "unresolved".
	out := ResolveBatch([]string{"pkg/a.go::Alpha", "Beta", "Missing::Nope"}, nil, root)
	require.Len(t, out, 3)

	exact := out["pkg/a.go::Alpha"]
	assert.Equal(t, "scip", exact.Provenance)
	assert.Equal(t, "pkg/a.go", exact.File)
	assert.Equal(t, 10, exact.Line)
	assert.Equal(t, 20, exact.EndLine)

	// Short-name suffix match, with the "endLine" alias honored.
	suffix := out["Beta"]
	assert.Equal(t, "scip", suffix.Provenance)
	assert.Equal(t, "other/b.go", suffix.File)
	assert.Equal(t, 3, suffix.Line)
	assert.Equal(t, 5, suffix.EndLine)

	miss := out["Missing::Nope"]
	assert.Equal(t, "unresolved", miss.Provenance)
}

func TestSCIP_BeatsAST(t *testing.T) {
	root := t.TempDir()
	writeSCIPSidecar(t, root, `[
		{"fqn":"internal/auth/jwt.go::ValidateToken","file":"internal/auth/jwt.go","line":10,"end_line":20}
	]`)
	g := buildResolveTestGraph() // same FQN lives at line 25 in the graph

	out := ResolveBatch([]string{"internal/auth/jwt.go::ValidateToken"}, g, root)
	require.Len(t, out, 1)
	got := out["internal/auth/jwt.go::ValidateToken"]
	assert.Equal(t, "scip", got.Provenance)
	assert.Equal(t, 10, got.Line)
	assert.Equal(t, 20, got.EndLine)
}

func TestFallbackOrdering(t *testing.T) {
	root := t.TempDir()
	writeSCIPSidecar(t, root, `[
		{"fqn":"pkg/a.go::Alpha","file":"pkg/a.go","line":10,"end_line":20}
	]`)
	g := buildResolveTestGraph()
	// Gamma's file is not on disk, so LSP skips it and AST answers.
	g.Nodes = g.Nodes.Set("pkg/b.go::Gamma", &link.ResolvedNode{
		ID:       "pkg/b.go::Gamma",
		Name:     "Gamma",
		Kind:     "FUNCTION",
		FileSpec: link.LocationMeta{Path: "pkg/b.go", LineStart: 7, LineEnd: 9},
	})

	out := ResolveBatch([]string{"pkg/a.go::Alpha", "pkg/b.go::Gamma", "nope::Missing"}, g, root)
	require.Len(t, out, 3)
	assert.Equal(t, "scip", out["pkg/a.go::Alpha"].Provenance)
	assert.Equal(t, "ast", out["pkg/b.go::Gamma"].Provenance)
	assert.Equal(t, 7, out["pkg/b.go::Gamma"].Line)
	assert.Equal(t, "unresolved", out["nope::Missing"].Provenance)
}

func TestLSP_GoplsDefinition(t *testing.T) {
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls not on PATH; LSP stage untestable here")
	}
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/lsptest\n\ngo 1.23\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "hello.go"), []byte(`package main

import "fmt"

// Hello greets the world.
func Hello() string {
	return "hi " + fmt.Sprint("there")
}

func main() {
	fmt.Println(Hello())
}
`), 0o644))

	assert.Contains(t, AvailableSources(root), "lsp")

	// LSP is best-effort (cold gopls caches may exceed the 10s budget), so
	// accept either a live "lsp" answer or a clean "unresolved" fallback —
	// but never a missing key, a wrong file, or a panic.
	out := ResolveBatch([]string{"hello.go::Hello"}, nil, root)
	require.Contains(t, out, "hello.go::Hello")
	got := out["hello.go::Hello"]
	t.Logf("LSP resolution: %+v", got)
	assert.Contains(t, []string{"lsp", "unresolved"}, got.Provenance)
	if got.Provenance == "lsp" {
		assert.Equal(t, "hello.go", got.File)
		assert.Greater(t, got.Line, 0)
		assert.GreaterOrEqual(t, got.EndLine, got.Line)
	}
}

func TestAvailableSources(t *testing.T) {
	root := t.TempDir()

	src := AvailableSources(root)
	assert.Contains(t, src, "ast")
	assert.NotContains(t, src, "scip")

	// SCIP becomes usable when the binary index or the JSON sidecar exists.
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".glassmarble", "scip"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".glassmarble", "scip", "index.scip"), []byte("x"), 0o644))
	src = AvailableSources(root)
	assert.Contains(t, src, "scip")

	// LSP mirrors gopls presence on PATH.
	_, lookErr := exec.LookPath("gopls")
	assert.Equal(t, lookErr == nil, slices.Contains(src, "lsp"))
}

func TestResolveBatch_EmptyAndDuplicates(t *testing.T) {
	out := ResolveBatch(nil, nil, "")
	assert.Empty(t, out)

	out = ResolveBatch([]string{"", "a::B", "a::B"}, nil, t.TempDir())
	require.Len(t, out, 1)
	assert.Equal(t, "unresolved", out["a::B"].Provenance)
	assert.Equal(t, "a::B", out["a::B"].FQN)
}

func TestCodeOccurrences_SkipsCommentsAndStrings(t *testing.T) {
	content := "package main\n\n// Hello greets the world.\n// Hello again.\nvar label = \"Hello\"\n/* Hello block */\nfunc Hello() string {\n\treturn label\n}\n"
	got := codeOccurrences(content, "Hello", 5)
	// Doc comments (lines 2-3), the string literal (line 4) and the block
	// comment (line 5) must not yield candidates; the declaration (line 6)
	// is the first code occurrence.
	require.NotEmpty(t, got)
	assert.Equal(t, [2]int{6, 5}, got[0])
	for _, c := range got {
		assert.NotContains(t, []int{2, 3, 4, 5}, c[0])
	}
	assert.Empty(t, codeOccurrences(content, "Missing", 5))
	assert.Empty(t, codeOccurrences(content, "", 5))
}

func TestAvailableSources_JSONOnly(t *testing.T) {
	root := t.TempDir()

	// JSON-only repo (no binary index.scip): resolution reads the JSON
	// sidecar, so scip must still report available (gap B1 quirk).
	writeSCIPSidecar(t, root, `[
		{"fqn":"pkg/a.go::Alpha","file":"pkg/a.go","line":10,"end_line":20}
	]`)
	assert.Contains(t, AvailableSources(root), "scip")

	// Sanity: sidecar answers through the normal batch path too.
	out := ResolveBatch([]string{"pkg/a.go::Alpha"}, nil, root)
	assert.Equal(t, "scip", out["pkg/a.go::Alpha"].Provenance)
}

func TestResolveCrossRepo_HitWithXRepoProvenance(t *testing.T) {
	primary := t.TempDir() // no sidecar: primary cannot resolve
	second := t.TempDir()
	writeSCIPSidecar(t, second, `[
		{"fqn":"otherrepo/pkg/a.go::Alpha","file":"pkg/a.go","line":42,"end_line":50}
	]`)

	// Miss in the primary repo stays unresolved through the normal path.
	out := ResolveBatch([]string{"otherrepo/pkg/a.go::Alpha"}, nil, primary)
	assert.Equal(t, "unresolved", out["otherrepo/pkg/a.go::Alpha"].Provenance)

	// Cross-repo lookup hits with xrepo provenance.
	got := ResolveCrossRepo("otherrepo/pkg/a.go::Alpha", []string{second}, nil)
	assert.Equal(t, "scip:xrepo", got.Provenance)
	assert.Equal(t, "pkg/a.go", got.File)
	assert.Equal(t, 42, got.Line)
	assert.Equal(t, 50, got.EndLine)
}

func TestResolveCrossRepo_FirstHitWins(t *testing.T) {
	first := t.TempDir()
	writeSCIPSidecar(t, first, `[
		{"fqn":"otherrepo/pkg/a.go::Alpha","file":"pkg/a.go","line":1,"end_line":2}
	]`)
	second := t.TempDir()
	writeSCIPSidecar(t, second, `[
		{"fqn":"otherrepo/pkg/a.go::Alpha","file":"pkg/a.go","line":99,"end_line":100}
	]`)

	got := ResolveCrossRepo("otherrepo/pkg/a.go::Alpha", []string{first, second}, nil)
	assert.Equal(t, "scip:xrepo", got.Provenance)
	assert.Equal(t, 1, got.Line)
}

func TestResolveCrossRepo_NoRootsNoOp(t *testing.T) {
	// Empty env → ExtraRepoRoots is nil; ResolveCrossRepo with no roots is
	// a no-op returning unresolved (never an error, never a hit).
	t.Setenv("GMB_EXTRA_REPOS", "")
	assert.Empty(t, ExtraRepoRoots())

	got := ResolveCrossRepo("otherrepo/pkg/a.go::Alpha", nil, nil)
	assert.Equal(t, "unresolved", got.Provenance)

	got = ResolveCrossRepo("otherrepo/pkg/a.go::Alpha", []string{}, nil)
	assert.Equal(t, "unresolved", got.Provenance)

	// Unknown symbol with real roots still misses cleanly.
	second := t.TempDir()
	writeSCIPSidecar(t, second, `[
		{"fqn":"otherrepo/pkg/a.go::Alpha","file":"pkg/a.go","line":42,"end_line":50}
	]`)
	got = ResolveCrossRepo("otherrepo/pkg/z.go::Missing", []string{second}, nil)
	assert.Equal(t, "unresolved", got.Provenance)
}

func TestExtraRepoRoots_ParsesPathList(t *testing.T) {
	sep := string(filepath.ListSeparator)
	t.Setenv("GMB_EXTRA_REPOS", strings.Join([]string{"  /repo/a  ", "", "/repo/b"}, sep))
	assert.Equal(t, []string{"/repo/a", "/repo/b"}, ExtraRepoRoots())
}
