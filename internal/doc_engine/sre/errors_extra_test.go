// Package sre extra tests: WithGraph vs nil-graph parity and divergence.
package sre

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/link"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func extraSentinelRepo(t *testing.T, callerBody string) string {
	t.Helper()
	dir := t.TempDir()
	pkgDir := filepath.Join(dir, "svc")
	require.NoError(t, os.MkdirAll(pkgDir, 0755))
	decl := "package svc\nimport \"errors\"\n\n// ErrUnavailable is returned when downstream is down.\nvar ErrUnavailable = errors.New(\"downstream unavailable\")\n"
	require.NoError(t, os.WriteFile(filepath.Join(pkgDir, "sentinels.go"), []byte(decl), 0644))
	if callerBody != "" {
		require.NoError(t, os.WriteFile(filepath.Join(pkgDir, "caller.go"), []byte(callerBody), 0644))
	}
	return dir
}

// TestExtraCatalogDivergenceGraphAuthoritative: the repo visibly references
// the sentinel (ident-scan finds a caller), but the graph knows the
// sentinel with zero inbound edges. The graph is authoritative, so the
// rendered WithGraph catalog must show no callers while the nil-graph
// catalog shows the parser-found caller.
func TestExtraCatalogDivergenceGraphAuthoritative(t *testing.T) {
	caller := "package svc\n\nfunc Poll() error {\n\treturn ErrUnavailable\n}\n"
	dir := extraSentinelRepo(t, caller)

	graph := akg.NewCodePropertyGraph("test")
	graph.Nodes = graph.Nodes.Set("svc/sentinels.go::ErrUnavailable", &link.ResolvedNode{
		ID:   "svc/sentinels.go::ErrUnavailable",
		Kind: "VAR",
		Name: "ErrUnavailable",
		FileSpec: link.LocationMeta{
			Path:      "svc/sentinels.go",
			LineStart: 5,
			LineEnd:   5,
		},
	})

	withGraph, err := GenerateErrorCatalogWithGraph(dir, graph)
	require.NoError(t, err)
	require.NoError(t, os.Remove(filepath.Join(dir, "docs", "errors.md")))
	withNil, err := GenerateErrorCatalogWithGraph(dir, nil)
	require.NoError(t, err)

	assert.NotEqual(t, withNil, withGraph,
		"graph-authoritative (empty) vs ident-scan (caller found) must diverge")
	assert.Contains(t, withNil, "caller.go#",
		"nil-graph catalog must carry the parser-found caller")
	assert.NotContains(t, withGraph, "caller.go#",
		"graph-authoritative catalog must not mix in ident-scan callers")
	assert.Contains(t, withGraph, "ErrUnavailable")
}

// TestExtraCatalogParityNoCallers: when nothing references the sentinel
// anywhere, both paths render identical markdown.
func TestExtraCatalogParityNoCallers(t *testing.T) {
	dir := extraSentinelRepo(t, "")
	withGraph, err := GenerateErrorCatalogWithGraph(dir, akg.NewCodePropertyGraph("empty"))
	require.NoError(t, err)
	require.NoError(t, os.Remove(filepath.Join(dir, "docs", "errors.md")))
	withNil, err := GenerateErrorCatalogWithGraph(dir, nil)
	require.NoError(t, err)
	assert.Equal(t, withNil, withGraph, "caller-free repo must render identical catalogs")
}

// TestExtraGraphCallerCapSortExclude pins the graph caller resolution
// contract: at most 5 callers, sorted by source ID, and the declaration
// file excluded.
func TestExtraGraphCallerCapSortExclude(t *testing.T) {
	dir := extraSentinelRepo(t, "")
	graph := akg.NewCodePropertyGraph("test")
	graph.Nodes = graph.Nodes.Set("svc/sentinels.go::ErrUnavailable", &link.ResolvedNode{
		ID:   "svc/sentinels.go::ErrUnavailable",
		Kind: "VAR",
		Name: "ErrUnavailable",
		FileSpec: link.LocationMeta{
			Path:      "svc/sentinels.go",
			LineStart: 5,
			LineEnd:   5,
		},
	})
	var edges []link.ResolvedEdge
	addCaller := func(name, path string, line int) {
		id := "svc/" + path + "::" + name
		graph.Nodes = graph.Nodes.Set(id, &link.ResolvedNode{
			ID:   id,
			Name: name,
			Kind: "FUNCTION",
			FileSpec: link.LocationMeta{
				Path:      "svc/" + path,
				LineStart: line,
				LineEnd:   line + 5,
			},
		})
		edges = append(edges, link.ResolvedEdge{
			SourceID: id, TargetID: "svc/sentinels.go::ErrUnavailable",
			Type: link.EdgeCalls, LineNumber: line + 100,
		})
	}
	for i, n := range []string{"Zeta", "Alpha", "Mid", "Beta", "Gamma", "Delta", "Epsilon"} {
		addCaller(n, fmt.Sprintf("c%d.go", i), 10+i)
	}
	graph.Nodes = graph.Nodes.Set("svc/sentinels.go::Helper", &link.ResolvedNode{
		ID:   "svc/sentinels.go::Helper",
		Name: "Helper",
		Kind: "FUNCTION",
		FileSpec: link.LocationMeta{
			Path:      "svc/sentinels.go",
			LineStart: 20,
			LineEnd:   25,
		},
	})
	edges = append(edges, link.ResolvedEdge{
		SourceID: "svc/sentinels.go::Helper", TargetID: "svc/sentinels.go::ErrUnavailable",
		Type: link.EdgeCalls, LineNumber: 22,
	})
	graph.InboundEdges = graph.InboundEdges.Set("svc/sentinels.go::ErrUnavailable", edges)

	facts, err := scanSentinelErrorsWithGraph(dir, graph)
	require.NoError(t, err)
	require.Len(t, facts, 1)
	callers := facts[0].Callers
	assert.Len(t, callers, 5, "graph callers must be capped at 5")
	assert.True(t, extraSortedStrings(callers), "graph callers must be sorted: %v", callers)
	for _, c := range callers {
		assert.NotContains(t, c, "sentinels.go", "declaration file must be excluded: %q", c)
	}
}

func TestExtraGraphCallerLineFallback(t *testing.T) {
	dir := extraSentinelRepo(t, "")
	graph := akg.NewCodePropertyGraph("test")
	graph.Nodes = graph.Nodes.Set("svc/sentinels.go::ErrUnavailable", &link.ResolvedNode{
		ID:   "svc/sentinels.go::ErrUnavailable",
		Kind: "VAR",
		Name: "ErrUnavailable",
		FileSpec: link.LocationMeta{
			Path:      "svc/sentinels.go",
			LineStart: 5,
			LineEnd:   5,
		},
	})
	// Ghost carries a path but no coordinates: the edge line number wins.
	graph.Nodes = graph.Nodes.Set("svc/noloc.go::Ghost", &link.ResolvedNode{
		ID:   "svc/noloc.go::Ghost",
		Name: "Ghost",
		Kind: "FUNCTION",
		FileSpec: link.LocationMeta{
			Path: "svc/noloc.go",
		},
	})
	graph.InboundEdges = graph.InboundEdges.Set("svc/sentinels.go::ErrUnavailable", []link.ResolvedEdge{
		{SourceID: "svc/noloc.go::Ghost", TargetID: "svc/sentinels.go::ErrUnavailable", Type: link.EdgeCalls, LineNumber: 42},
	})
	facts, err := scanSentinelErrorsWithGraph(dir, graph)
	require.NoError(t, err)
	require.Len(t, facts, 1)
	require.Len(t, facts[0].Callers, 1)
	assert.Equal(t, "svc/noloc.go#42", facts[0].Callers[0],
		"caller without node coordinates must fall back to the edge line number")
}

func TestExtraTriageTimeoutSteps(t *testing.T) {
	dir := t.TempDir()
	pkgDir := filepath.Join(dir, "svc")
	require.NoError(t, os.MkdirAll(pkgDir, 0755))
	code := "package svc\nimport \"errors\"\n\n// ErrOpTimeout is returned when slow.\nvar ErrOpTimeout = errors.New(\"slow\")\n"
	require.NoError(t, os.WriteFile(filepath.Join(pkgDir, "sentinels.go"), []byte(code), 0644))
	catalog, err := GenerateErrorCatalog(dir)
	require.NoError(t, err)
	assert.True(t, strings.Contains(catalog, "upstream service health") || strings.Contains(catalog, "timeout threshold"),
		"timeout sentinel must carry triage steps:\n%s", catalog)
}

func extraSortedStrings(in []string) bool {
	for i := 1; i < len(in); i++ {
		if in[i-1] > in[i] {
			return false
		}
	}
	return true
}
