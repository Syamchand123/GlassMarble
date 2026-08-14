package aggregate

import (
	"net/url"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/normalize"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIndexExternalDependencies(t *testing.T) {
	output := &AggregateOutput{
		LocalTables: map[string]*normalize.FileSymbolTable{
			"main.go": {
				RelPath: "main.go",
				Imports: []string{
					"fmt",
					"github.com/acme/thirdparty/awesome",
				},
			},
			"app/service.go": {
				RelPath: "app/service.go",
				Imports: []string{
					"net/http",
					"github.com/acme/widget/internal/pkg",
				},
			},
		},
		WorkspaceCtx: &WorkspaceContext{ModulePrefix: "github.com/acme/widget"},
	}

	IndexExternalDependencies(output)

	// v2 (W1-09): keys are ext:<url-escaped path>, not raw import strings.
	key := ExternalKey("github.com/acme/thirdparty/awesome")
	require.Contains(t, output.ExternalDependencies, key)
	node := output.ExternalDependencies[key]
	assert.Equal(t, normalize.GASTTypeDeclaration, node.Type)
	assert.Equal(t, "External", node.Visibility)
	assert.Equal(t, "true", node.Properties["is_external"])
	assert.Equal(t, "EXTERNAL_SDK", node.Properties["primitive"])
	assert.Equal(t, "github.com/acme/thirdparty/awesome", node.Properties["import_path"])
	assert.Equal(t, key, node.Properties["ext_id"])

	assert.Len(t, output.ExternalDependencies, 1)
	assert.NotContains(t, output.ExternalDependencies, "fmt")
	assert.NotContains(t, output.ExternalDependencies, "net/http")
	assert.NotContains(t, output.ExternalDependencies, "github.com/acme/widget/internal/pkg")
	assert.NotContains(t, output.ExternalDependencies, "github.com/acme/thirdparty/awesome")
}

func TestIndexExternalDependenciesDeduplicates(t *testing.T) {
	output := &AggregateOutput{
		LocalTables: map[string]*normalize.FileSymbolTable{
			"a/main.go": {Imports: []string{"github.com/acme/lib"}},
			"b/main.go": {Imports: []string{"github.com/acme/lib"}},
		},
	}

	IndexExternalDependencies(output)

	require.Len(t, output.ExternalDependencies, 1)
	assert.Contains(t, output.ExternalDependencies, ExternalKey("github.com/acme/lib"))
}

func TestResolveExternalKeySelfHealsV1ToV2(t *testing.T) {
	// W1-09: v1 raw import paths read old caches/deps.json; lookups must
	// self-heal to the v2 ext:<escaped> spelling without mutating input.
	imp := "github.com/acme/lib"
	want := "ext:" + url.PathEscape(imp)
	assert.Equal(t, want, ResolveExternalKey(imp))
	assert.Equal(t, want, ResolveExternalKey(want))
	assert.Equal(t, "ext:github.com%2Facme%2Flib", want)
}

func TestIndexExternalDependenciesSelfHealsV1Keys(t *testing.T) {
	// Simulate an old cache keyed by raw import path: v2 reader must
	// still find the node via the raw spelling (self-healing read).
	output := &AggregateOutput{
		LocalTables: map[string]*normalize.FileSymbolTable{
			"a/main.go": {Imports: []string{"github.com/acme/lib"}},
		},
		ExternalDependencies: map[string]*normalize.GASTNode{
			"github.com/acme/lib": {
				Name:       "github.com/acme/lib",
				Visibility: "External",
				Properties: map[string]string{"primitive": "EXTERNAL_SDK"},
			},
		},
	}

	// The indexer dedupes on the v2 key: legacy nodes are preserved but
	// a v2-spelled node is created for new writes.
	IndexExternalDependencies(output)

	node, ok := output.ExternalDependencies[ExternalKey("github.com/acme/lib")]
	require.True(t, ok)
	assert.Equal(t, "true", node.Properties["is_external"])
	// v1 raw spelling remains readable for old consumers.
	assert.NotNil(t, output.ExternalDependencies["github.com/acme/lib"])
}

func TestIndexExternalDependenciesEmpty(t *testing.T) {
	output := &AggregateOutput{}

	IndexExternalDependencies(output)

	assert.NotNil(t, output.ExternalDependencies)
	assert.Empty(t, output.ExternalDependencies)
}

func TestIndexExternalDependenciesNilSymbolTable(t *testing.T) {
	output := &AggregateOutput{
		LocalTables: map[string]*normalize.FileSymbolTable{
			"main.go": nil,
		},
	}

	IndexExternalDependencies(output)

	assert.Empty(t, output.ExternalDependencies)
}

func TestIsStdlibImport(t *testing.T) {
	tests := []struct {
		name    string
		imp     string
		relPath string
		want    bool
	}{
		// Go
		{"go stdlib single", "fmt", "main.go", true},
		{"go stdlib nested", "net/http", "main.go", true},
		{"go stdlib nested json", "encoding/json", "main.go", true},
		{"go third party", "github.com/x/y", "main.go", false},
		{"go golang.org x", "golang.org/x/tools", "main.go", false},

		// Python
		{"py os", "os", "main.py", true},
		{"py sys", "sys", "main.py", true},
		{"py json", "json", "main.py", true},
		{"py no-dot third party tolerated", "requests", "main.py", true},
		{"py dotted third party", "flask.ext", "main.py", false},

		// Java / Kotlin
		{"java util", "java.util.List", "Main.java", true},
		{"java javax", "javax.servlet.http", "Main.java", true},
		{"java third party", "com.google.gson.Gson", "Main.java", false},
		{"kotlin non-stdlib", "kotlin.text.Regex", "Main.kt", false},

		// JS/TS
		{"node builtin prefixed", "node:fs", "app.ts", true},
		{"node legacy builtin", "fs", "app.js", true},
		{"ts bare package tolerated", "react", "app.tsx", true},
		{"ts scoped package", "@mui/material", "app.tsx", false},
		{"ts package subpath", "lodash/fp", "app.ts", false},
		{"ts relative", "./local", "app.ts", false},
		{"ts relative parent", "../util", "app.ts", false},

		// Extension / edge cases
		{"extension mismatch (fmt treated as py stdlib)", "fmt", "main.py", true},
		{"unknown extension", "github.com/x/y", "lib.rb", false},
		{"empty import", "", "main.go", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsStdlibImport(tt.imp, tt.relPath)
			assert.Equal(t, tt.want, got, "IsStdlibImport(%q, %q)", tt.imp, tt.relPath)
		})
	}
}
