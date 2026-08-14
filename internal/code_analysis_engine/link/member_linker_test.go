package link

import (
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/normalize"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/aggregate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func memberFixtureTree() *aggregate.AggregateOutput {
	root := aggregate.NewDirectoryNode(".", "")
	dir := aggregate.NewDirectoryNode("pkg", "pkg")
	root.SubFolders["pkg"] = dir

	// Options: fields TargetDir/Workers, embedded Base, methods Run/validate,
	// BaseTypes [Base], Implemented [Runnable].
	options := &normalize.GASTNode{
		Type:        normalize.GASTTypeDeclaration,
		Name:        "Options",
		Kind:        "struct",
		StartLine:   1,
		BaseTypes:   []string{"Base"},
		Implemented: []string{"Runnable"},
		Children: []*normalize.GASTNode{
			{Type: normalize.GASTField, Name: "TargetDir", StartLine: 2},
			{Type: normalize.GASTField, Name: "Workers", StartLine: 3},
			{Type: normalize.GASTField, Name: "Base", Kind: "embedding", FieldType: "Base", StartLine: 4},
			{Type: normalize.GASTFunction, Name: "Options.Run", Kind: "method", ReceiverType: "Options", StartLine: 5, Children: []*normalize.GASTNode{
				{Type: normalize.GASTParameter, Name: "cfg", StartLine: 5},
			}},
			{Type: normalize.GASTFunction, Name: "Options.validate", Kind: "method", ReceiverType: "Options", StartLine: 9},
		},
	}

	// Runnable interface with method_elem (empty receiver, §5.2.2).
	runnable := &normalize.GASTNode{
		Type:      normalize.GASTTypeDeclaration,
		Name:      "Runnable",
		Kind:      "interface",
		StartLine: 12,
		Children: []*normalize.GASTNode{
			{Type: normalize.GASTFunction, Name: "Run", Kind: "method", StartLine: 13},
		},
	}

	base := &normalize.GASTNode{
		Type:      normalize.GASTTypeDeclaration,
		Name:      "Base",
		Kind:      "struct",
		StartLine: 16,
	}

	// Free function with params and a resolvable return type (Base).
	fn := &normalize.GASTNode{
		Type:       normalize.GASTFunction,
		Name:       "NewBase",
		Kind:       "function",
		ReturnType: "Base",
		StartLine:  20,
		Children: []*normalize.GASTNode{
			{Type: normalize.GASTParameter, Name: "name", StartLine: 20},
			{Type: normalize.GASTParameter, Name: "size", StartLine: 20},
		},
	}

	dir.Files["types.go"] = &aggregate.FileBoundaryNode{
		FileName:     "types.go",
		RelativePath: "pkg/types.go",
		Language:     "go",
		GASTRoot: &normalize.GASTNode{Children: []*normalize.GASTNode{
			base, runnable, options, fn,
		}},
	}

	return &aggregate.AggregateOutput{
		RootNode:           root,
		CommitHash:         "HEAD",
		EntrypointRegistry: []string{},
	}
}

// TestMemberLinkerFieldEdges (W1-11, §5.4.1): type → field HAS_FIELD edges
// with registered FIELD nodes (structural spine).
func TestMemberLinkerFieldEdges(t *testing.T) {
	aggregateOut := memberFixtureTree()
	cpg := BuildInitialNodes(aggregateOut, nil)
	LinkMembersAndReturns(aggregateOut, cpg)

	typeID := "pkg/types.go::Options"
	fields := cpg.OutboundEdges[typeID]
	hasField := map[string]bool{}
	for _, e := range fields {
		if e.Type == EdgeHasField {
			hasField[e.TargetID] = true
		}
	}
	assert.True(t, hasField["pkg/types.go::Options::TargetDir"], "TargetDir field edge")
	assert.True(t, hasField["pkg/types.go::Options::Workers"], "Workers field edge")

	fieldNode, ok := cpg.GetNode("pkg/types.go::Options::TargetDir")
	require.True(t, ok, "FIELD node must exist")
	assert.Equal(t, "FIELD", fieldNode.Kind)
	assert.Equal(t, "pkg/types.go", fieldNode.FileSpec.Path)
}

// TestMemberLinkerHasReceiver (W1-11 / A-16): method → owner type, explicit
// receivers and interface method_elem alike.
func TestMemberLinkerHasReceiver(t *testing.T) {
	aggregateOut := memberFixtureTree()
	cpg := BuildInitialNodes(aggregateOut, nil)
	LinkMembersAndReturns(aggregateOut, cpg)

	optID := "pkg/types.go::Options"
	runID := "pkg/types.go::Options::Run"
	validateID := "pkg/types.go::Options::validate"

	hasReceiver := map[string]string{}
	for _, e := range cpg.OutboundEdges[runID] {
		if e.Type == EdgeHasReceiver {
			hasReceiver[runID] = e.TargetID
		}
	}
	for _, e := range cpg.OutboundEdges[validateID] {
		if e.Type == EdgeHasReceiver {
			hasReceiver[validateID] = e.TargetID
		}
	}
	assert.Equal(t, optID, hasReceiver[runID], "Run → Options")
	assert.Equal(t, optID, hasReceiver[validateID], "validate → Options")

	// Interface method_elem: empty receiver, owned by the interface.
	ifaceID := "pkg/types.go::Runnable"
	methodID := "pkg/types.go::Runnable::Run"
	found := false
	for _, e := range cpg.OutboundEdges[methodID] {
		if e.Type == EdgeHasReceiver && e.TargetID == ifaceID {
			found = true
		}
	}
	assert.True(t, found, "interface method_elem must have HAS_RECEIVER → interface")
}

// TestMemberLinkerEmbedding (W1-11, §5.4.1): Go embedding emits
// EdgeExtends @1.0 with gm:embedding "true".
func TestMemberLinkerEmbedding(t *testing.T) {
	aggregateOut := memberFixtureTree()
	cpg := BuildInitialNodes(aggregateOut, nil)
	LinkMembersAndReturns(aggregateOut, cpg)

	sourceID := "pkg/types.go::Options"
	targetID := "pkg/types.go::Base"

	var found *ResolvedEdge
	for _, e := range cpg.OutboundEdges[sourceID] {
		if e.Type == EdgeExtends && e.TargetID == targetID && e.Properties["gm:embedding"] == "true" {
			found = &e
		}
	}
	require.NotNil(t, found, "embedding must emit EXTENDS → Base")
	assert.Equal(t, float32(1.0), found.Confidence)
	assert.Equal(t, "true", found.Properties["gm:embedding"])
}

// TestMemberLinkerBaseTypesAndImplemented (W1-11, §5.4.1 / A-02):
// BaseTypes → EXTENDS for structs, IMPLEMENTS for interfaces;
// Implemented → IMPLEMENTS.
func TestMemberLinkerBaseTypesAndImplemented(t *testing.T) {
	aggregateOut := memberFixtureTree()
	cpg := BuildInitialNodes(aggregateOut, nil)
	LinkMembersAndReturns(aggregateOut, cpg)

	sourceID := "pkg/types.go::Options"
	baseID := "pkg/types.go::Base"
	runnableID := "pkg/types.go::Runnable"

	ext, impl := false, false
	for _, e := range cpg.OutboundEdges[sourceID] {
		if e.Type == EdgeExtends && e.TargetID == baseID {
			ext = true
		}
		if e.Type == EdgeImplements && e.TargetID == runnableID {
			impl = true
		}
	}
	assert.True(t, ext, "BaseTypes Base → EXTENDS (target is a struct)")
	assert.True(t, impl, "Implemented Runnable → IMPLEMENTS")
}

// TestMemberLinkerReturnsAndParams (W1-11, §5.4.1): function → return-type
// node (when resolvable) and function → param nodes.
func TestMemberLinkerReturnsAndParams(t *testing.T) {
	aggregateOut := memberFixtureTree()
	cpg := BuildInitialNodes(aggregateOut, nil)
	LinkMembersAndReturns(aggregateOut, cpg)

	fnID := "pkg/types.go::NewBase"
	baseID := "pkg/types.go::Base"

	returns := false
	for _, e := range cpg.OutboundEdges[fnID] {
		if e.Type == EdgeReturns && e.TargetID == baseID {
			returns = true
		}
	}
	assert.True(t, returns, "NewBase returns Base → RETURNS edge")

	hasParam := map[string]bool{}
	for _, e := range cpg.OutboundEdges[fnID] {
		if e.Type == EdgeHasParam {
			hasParam[e.TargetID] = true
		}
	}
	assert.True(t, hasParam[fnID+"::param:name"], "param name edge")
	assert.True(t, hasParam[fnID+"::param:size"], "param size edge")

	paramNode, ok := cpg.GetNode(fnID + "::param:name")
	require.True(t, ok, "PARAM node must exist")
	assert.Equal(t, "PARAM", paramNode.Kind)
	assert.Equal(t, "name", paramNode.Name)
}

// TestMemberLinkerT2NameMatchFallback verifies name-match fallback for T2 languages (W6-03 / §10.0).
func TestMemberLinkerT2NameMatchFallback(t *testing.T) {
	cpg := NewLinkOutput("HEAD")
	cpg.GraphNodes["src/handler.py::Processor"] = &ResolvedNode{
		ID:   "src/handler.py::Processor",
		Kind: "CLASS",
		Name: "Processor",
	}

	globalIndex := map[string][]*normalize.GASTNode{
		"Processor": {{Name: "Processor", Properties: map[string]string{"file_path": "src/handler.py"}}},
	}

	targetFQN := resolveTypeToFQN("Processor", "src/client.py", globalIndex, cpg)
	assert.Equal(t, "src/handler.py::Processor", targetFQN, "T2 name-match fallback must resolve Processor to its FQN")
}

// TestMemberLinkerNilGlobalDefinitionIndex (GAP-L-04): a first-run aggregate
// output may carry a nil GlobalDefinitionIndex (empty repo). The member
// linker must tolerate it: no panic, the structural spine (fields/params)
// still emits, and only unresolvable targets are skipped (no fabricated
// nodes or edges). Resolvable bases still link through GraphNodes, the
// step-1 fallback that does not require the index.
func TestMemberLinkerNilGlobalDefinitionIndex(t *testing.T) {
	aggregateOut := memberFixtureTree()
	aggregateOut.GlobalDefinitionIndex = nil

	phantom := &normalize.GASTNode{
		Type:      normalize.GASTTypeDeclaration,
		Name:      "Phantom",
		Kind:      "class",
		StartLine: 24,
		BaseTypes: []string{"NoSuchType"},
		Children: []*normalize.GASTNode{
			{Type: normalize.GASTField, Name: "Ghost", StartLine: 25},
		},
	}
	dir := aggregateOut.RootNode.SubFolders["pkg"]
	dir.Files["types.go"].GASTRoot.Children = append(dir.Files["types.go"].GASTRoot.Children, phantom)

	cpg := BuildInitialNodes(aggregateOut, nil)
	require.NotPanics(t, func() { LinkMembersAndReturns(aggregateOut, cpg) })

	// Structural spine still emitted without the index.
	typeID := "pkg/types.go::Options"
	fieldEdge := false
	for _, e := range cpg.OutboundEdges[typeID] {
		if e.Type == EdgeHasField {
			fieldEdge = true
		}
	}
	assert.True(t, fieldEdge, "HAS_FIELD edges must emit without GlobalDefinitionIndex")

	// Bases present in GraphNodes still resolve via step-1 fallback.
	ext := false
	for _, e := range cpg.OutboundEdges[typeID] {
		if e.Type == EdgeExtends && e.TargetID == "pkg/types.go::Base" {
			ext = true
		}
	}
	assert.True(t, ext, "EXTENDS edge must resolve through GraphNodes fallback")

	// A base that exists nowhere must NOT produce a fabricated edge/node.
	phantomID := "pkg/types.go::Phantom"
	assert.False(t, hasEdge(cpg, phantomID, "NoSuchType", EdgeExtends), "unresolvable base must be skipped")
	assert.True(t, hasEdge(cpg, phantomID, "pkg/types.go::Phantom::Ghost", EdgeHasField), "field spine still emits for Phantom")

	// Full pipeline entry point tolerates the nil index too.
	require.NotPanics(t, func() {
		Link(aggregateOut, nil, nil, LinkerConfig{LevelOfDetail: LevelFull})
	})
}
