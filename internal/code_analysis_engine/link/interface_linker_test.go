package link

import (
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/normalize"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/aggregate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func interfaceFixture(sigMethods map[string]string, modified map[string]bool) (*LinkOutput, *aggregate.AggregateOutput) {
	globalIndex := map[string][]*normalize.GASTNode{
		"Runner": {
			{
				Name:       "Runner",
				Kind:       "interface",
				Type:       normalize.GASTTypeDeclaration,
				Properties: map[string]string{"file_path": "pkg.go"},
				Children: func() []*normalize.GASTNode {
					var kids []*normalize.GASTNode
					for name, sig := range sigMethods {
						kids = append(kids, &normalize.GASTNode{Name: name, Kind: "method", Type: normalize.GASTFunction, Signature: sig})
					}
					return kids
				}(),
			},
		},
	}

	aggregateOut := &aggregate.AggregateOutput{GlobalDefinitionIndex: globalIndex}

	cpg := NewLinkOutput("HEAD")
	cpg.ModifiedFiles = modified
	cpg.GraphNodes["pkg.go::Runner"] = &ResolvedNode{ID: "pkg.go::Runner", Kind: "INTERFACE", Name: "Runner", FileSpec: LocationMeta{Path: "pkg.go"}}
	return cpg, aggregateOut
}

func addStruct(cpg *LinkOutput, aggregateOut *aggregate.AggregateOutput, name string, methods map[string]string) {
	children := make([]*normalize.GASTNode, 0, len(methods))
	for mName, sig := range methods {
		children = append(children, &normalize.GASTNode{Name: mName, Kind: "method", Type: normalize.GASTFunction, Signature: sig})
	}
	aggregateOut.GlobalDefinitionIndex[name] = []*normalize.GASTNode{
		{Name: name, Kind: "struct", Type: normalize.GASTTypeDeclaration, Properties: map[string]string{"file_path": "pkg.go"}, Children: children},
	}
	cpg.GraphNodes["pkg.go::"+name] = &ResolvedNode{ID: "pkg.go::" + name, Kind: "STRUCT", Name: name, FileSpec: LocationMeta{Path: "pkg.go", LineStart: 1}}
}

// TestInterfaceLinkerFullRebuildRuns (W1-13 / A-05): empty ModifiedFiles is
// a full rebuild — the pass must run for every pair, even though the
// ModifiedFiles lookups all miss.
func TestInterfaceLinkerFullRebuildRuns(t *testing.T) {
	cpg, aggregateOut := interfaceFixture(map[string]string{"Run": "Run() error"}, nil)
	addStruct(cpg, aggregateOut, "Robot", map[string]string{"Run": "Run() error"})

	LinkInterfacesAndRealizations(aggregateOut, cpg)

	found := false
	for _, e := range cpg.OutboundEdges["pkg.go::Robot"] {
		if e.Type == EdgeImplements && e.TargetID == "pkg.go::Runner" {
			found = true
		}
	}
	assert.True(t, found, "full rebuild must emit IMPLEMENTS even when ModifiedFiles is empty (A-05)")
}

// TestInterfaceLinkerIncrementalGate (W1-13 / A-05): in a delta, pairs where
// neither side is modified are skipped.
func TestInterfaceLinkerIncrementalGate(t *testing.T) {
	cpg, aggregateOut := interfaceFixture(map[string]string{"Run": "Run() error"}, map[string]bool{"other.go": true})
	addStruct(cpg, aggregateOut, "Robot", map[string]string{"Run": "Run() error"})

	LinkInterfacesAndRealizations(aggregateOut, cpg)

	assert.Empty(t, cpg.OutboundEdges["pkg.go::Robot"], "both-unmodified pair must be skipped in a delta")
}

// TestInterfaceLinkerSignatureMatchPrimary (W1-13 / A-17): same name but a
// different signature does NOT satisfy the interface.
func TestInterfaceLinkerSignatureMatchPrimary(t *testing.T) {
	cpg, aggregateOut := interfaceFixture(map[string]string{"Run": "Run() error"}, nil)
	addStruct(cpg, aggregateOut, "Robot", map[string]string{"Run": "Run(cfg string) error"})

	LinkInterfacesAndRealizations(aggregateOut, cpg)

	assert.Empty(t, cpg.OutboundEdges["pkg.go::Robot"], "signature mismatch must not IMPLEMENTS")
}

// TestInterfaceLinkerNameFallback (W1-13 / A-17): when either signature is
// empty, name-only matching still satisfies the interface.
func TestInterfaceLinkerNameFallback(t *testing.T) {
	cpg, aggregateOut := interfaceFixture(map[string]string{"Run": "Run() error"}, nil)
	addStruct(cpg, aggregateOut, "Robot", map[string]string{"Run": ""})

	LinkInterfacesAndRealizations(aggregateOut, cpg)

	found := false
	for _, e := range cpg.OutboundEdges["pkg.go::Robot"] {
		if e.Type == EdgeImplements && e.TargetID == "pkg.go::Runner" {
			found = true
		}
	}
	assert.True(t, found, "empty struct signature falls back to name match")
}

// TestInterfaceLinkerExactMembership (W1-13 / A-15): the GAST lookup is
// exact (name key + file path); a same-named type in another file must not
// leak methods in.
func TestInterfaceLinkerExactMembership(t *testing.T) {
	cpg, aggregateOut := interfaceFixture(map[string]string{"Run": "Run() error"}, nil)

	// Same-named struct in another file with a DIFFERENT method set.
	aggregateOut.GlobalDefinitionIndex["Robot"] = []*normalize.GASTNode{
		{
			Name:       "Robot",
			Kind:       "struct",
			Type:       normalize.GASTTypeDeclaration,
			Properties: map[string]string{"file_path": "other.go"},
			Children: []*normalize.GASTNode{
				{Name: "Run", Kind: "method", Type: normalize.GASTFunction, Signature: "Run(cfg string) error"},
				{Name: "Stop", Kind: "method", Type: normalize.GASTFunction, Signature: "Stop() error"},
			},
		},
	}
	cpg.GraphNodes["pkg.go::Robot"] = &ResolvedNode{ID: "pkg.go::Robot", Kind: "STRUCT", Name: "Robot", FileSpec: LocationMeta{Path: "pkg.go"}}

	LinkInterfacesAndRealizations(aggregateOut, cpg)

	// The in-scope Robot must not inherit other.go's Stop, and its Run
	// signature (from other.go!) must not be used either — exact
	// membership returns no methods for pkg.go's Robot, so no edge.
	assert.Empty(t, cpg.OutboundEdges["pkg.go::Robot"])
	require.Len(t, aggregateOut.GlobalDefinitionIndex["Robot"], 1)
}
