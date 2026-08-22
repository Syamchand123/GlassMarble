package link

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/aggregate"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/normalize"
)

// TestExternalCallQualifierMismatch (GAP-CALL-05): a receiver qualifier that
// matches neither the import alias nor its final path segment must not
// fabricate an ext: API node — the edge is dropped instead.
func TestExternalCallQualifierMismatch(t *testing.T) {
	cpg := NewLinkOutput("HEAD")
	out := &aggregate.AggregateOutput{
		ExternalDependencies: map[string]*normalize.GASTNode{
			aggregate.ExternalKey("github.com/acme/sdk"): {
				Type: normalize.GASTTypeDeclaration,
				Name: "github.com/acme/sdk",
			},
		},
	}

	// receiver "db" vs import base "sdk": no alias, no match → dropped.
	target, conf := resolveCallTarget("db", "Query", "svc.go", []string{"github.com/acme/sdk"}, nil, cpg, out)
	assert.Equal(t, "", target, "mismatched qualifier must not resolve")
	assert.Zero(t, conf)
	_, node := cpg.GetNode(aggregate.ExternalKey("github.com/acme/sdk") + "::db.Query")
	assert.False(t, node, "no fabricated EXTERNAL_API node for mismatched qualifier")
	_, sdk := cpg.GetNode(aggregate.ExternalKey("github.com/acme/sdk"))
	assert.False(t, sdk, "no fabricated EXTERNAL_SDK node for mismatched qualifier")
}

// TestExternalCallBareNeverFabricates (GAP-CALL-05): an unqualified call
// (receiver "") can never be an external package call; nothing is fabricated.
func TestExternalCallBareNeverFabricates(t *testing.T) {
	cpg := NewLinkOutput("HEAD")
	out := &aggregate.AggregateOutput{
		ExternalDependencies: map[string]*normalize.GASTNode{
			aggregate.ExternalKey("github.com/acme/sdk"): {
				Type: normalize.GASTTypeDeclaration,
				Name: "github.com/acme/sdk",
			},
		},
	}

	target, conf := resolveCallTarget("", "Bootstrap", "svc.go", []string{"github.com/acme/sdk"}, nil, cpg, out)
	assert.Equal(t, "", target, "bare calls never fabricate external API nodes")
	assert.Zero(t, conf)
	assert.Empty(t, cpg.GraphNodes)
}

// TestExternalCallAliasMatch (GAP-CALL-05): an explicit import alias that
// equals the receiver qualifier resolves and fabricates the API node.
func TestExternalCallAliasMatch(t *testing.T) {
	cpg := NewLinkOutput("HEAD")
	out := &aggregate.AggregateOutput{
		ExternalDependencies: map[string]*normalize.GASTNode{
			aggregate.ExternalKey("github.com/acme/sdk"): {
				Type: normalize.GASTTypeDeclaration,
				Name: "github.com/acme/sdk",
			},
		},
		LocalTables: map[string]*normalize.FileSymbolTable{
			"svc.go": {
				FilePath:      "svc.go",
				RelPath:       "svc.go",
				Imports:       []string{"github.com/acme/sdk"},
				ImportAliases: map[string]string{"github.com/acme/sdk": "sdk"},
			},
		},
	}

	target, conf := resolveCallTarget("sdk", "Connect", "svc.go", []string{"github.com/acme/sdk"}, nil, cpg, out)
	require.NotEmpty(t, target, "alias-matched qualifier resolves")
	assert.Equal(t, float32(0.9), conf)
	assert.Equal(t, aggregate.ExternalKey("github.com/acme/sdk")+"::sdk.Connect", target)
	_, api := cpg.GetNode(target)
	assert.True(t, api, "EXTERNAL_API node fabricated for alias match")
	_, sdk := cpg.GetNode(aggregate.ExternalKey("github.com/acme/sdk"))
	assert.True(t, sdk, "EXTERNAL_SDK node fabricated for alias match")
}

// TestExternalCallBaseNameMatch (GAP-CALL-05): a qualifier equal to the
// import's final path segment (gopkg.in-style version suffixes stripped)
// resolves.
func TestExternalCallBaseNameMatch(t *testing.T) {
	cpg := NewLinkOutput("HEAD")
	out := &aggregate.AggregateOutput{
		ExternalDependencies: map[string]*normalize.GASTNode{
			aggregate.ExternalKey("gopkg.in/yaml.v3"): {
				Type: normalize.GASTTypeDeclaration,
				Name: "gopkg.in/yaml.v3",
			},
		},
	}

	target, conf := resolveCallTarget("yaml", "Marshal", "svc.go", []string{"gopkg.in/yaml.v3"}, nil, cpg, out)
	require.NotEmpty(t, target, "base-name qualifier resolves for gopkg.in/yaml.v3")
	assert.Equal(t, float32(0.9), conf)
	assert.Equal(t, aggregate.ExternalKey("gopkg.in/yaml.v3")+"::yaml.Marshal", target)
}

// TestImportBaseName strips gopkg.in and major-version suffixes.
func TestImportBaseName(t *testing.T) {
	cases := map[string]string{
		"github.com/acme/sdk":               "sdk",
		"gopkg.in/yaml.v3":                  "yaml",
		"gopkg.in/yaml.v2":                  "yaml",
		"github.com/foo/bar/v2":             "bar",
		"net/http":                          "http",
		"github.com/go-redis/redis/v8":      "redis",
		"github.com/Syamchand123/GlassMarble/internal/errors": "errors",
	}
	for imp, want := range cases {
		assert.Equal(t, want, importBaseName(imp), "base name of %s", imp)
	}
}

// TestConversionCallNeverLinks (GAP-CALL-06): type conversions such as
// string(x) or float64(v) surface as call sites whose "method" is a
// predeclared type name. They must never produce a CALLS edge — no such
// symbol exists.
func TestConversionCallNeverLinks(t *testing.T) {
	cpg := NewLinkOutput("HEAD")
	out := &aggregate.AggregateOutput{
		GlobalCallQueue: []aggregate.LinkedCallSite{{
			SourceFilePath: "svc.go",
			SourceFileNodeID: "file:svc.go",
			ReceiverName:   "",
			MethodName:     "string",
			LineNumber:     10,
		}},
	}

	resolveCallSite(out.GlobalCallQueue[0], &aggregate.OwnershipMap{}, cpg, out)
	assert.Empty(t, cpg.GraphNodes, "conversion call sites must not create nodes or edges")
	assert.Empty(t, cpg.OutboundEdges["file:svc.go"], "conversion must not create CALLS edges")
}
