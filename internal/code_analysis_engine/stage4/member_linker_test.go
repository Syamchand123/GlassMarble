package stage4

import (
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage2"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func memberFixtureTree() *stage3.Stage3Output {
	root := stage3.NewDirectoryNode(".", "")
	dir := stage3.NewDirectoryNode("pkg", "pkg")
	root.SubFolders["pkg"] = dir

	// Options: fields TargetDir/Workers, embedded Base, methods Run/validate,
	// BaseTypes [Base], Implemented [Runnable].
	options := &stage2.GASTNode{
		Type:        stage2.GASTTypeDeclaration,
		Name:        "Options",
		Kind:        "struct",
		StartLine:   1,
		BaseTypes:   []string{"Base"},
		Implemented: []string{"Runnable"},
		Children: []*stage2.GASTNode{
			{Type: stage2.GASTField, Name: "TargetDir", StartLine: 2},
			{Type: stage2.GASTField, Name: "Workers", StartLine: 3},
			{Type: stage2.GASTField, Name: "Base", Kind: "embedding", FieldType: "Base", StartLine: 4},
			{Type: stage2.GASTFunction, Name: "Options.Run", Kind: "method", ReceiverType: "Options", StartLine: 5, Children: []*stage2.GASTNode{
				{Type: stage2.GASTParameter, Name: "cfg", StartLine: 5},
			}},
			{Type: stage2.GASTFunction, Name: "Options.validate", Kind: "method", ReceiverType: "Options", StartLine: 9},
		},
	}

	// Runnable interface with method_elem (empty receiver, §5.2.2).
	runnable := &stage2.GASTNode{
		Type:      stage2.GASTTypeDeclaration,
		Name:      "Runnable",
		Kind:      "interface",
		StartLine: 12,
		Children: []*stage2.GASTNode{
			{Type: stage2.GASTFunction, Name: "Run", Kind: "method", StartLine: 13},
		},
	}

	base := &stage2.GASTNode{
		Type:      stage2.GASTTypeDeclaration,
		Name:      "Base",
		Kind:      "struct",
		StartLine: 16,
	}

	// Free function with params and a resolvable return type (Base).
	fn := &stage2.GASTNode{
		Type:       stage2.GASTFunction,
		Name:       "NewBase",
		Kind:       "function",
		ReturnType: "Base",
		StartLine:  20,
		Children: []*stage2.GASTNode{
			{Type: stage2.GASTParameter, Name: "name", StartLine: 20},
			{Type: stage2.GASTParameter, Name: "size", StartLine: 20},
		},
	}

	dir.Files["types.go"] = &stage3.FileBoundaryNode{
		FileName:     "types.go",
		RelativePath: "pkg/types.go",
		Language:     "go",
		GASTRoot: &stage2.GASTNode{Children: []*stage2.GASTNode{
			base, runnable, options, fn,
		}},
	}

	return &stage3.Stage3Output{
		RootNode:           root,
		CommitHash:         "HEAD",
		EntrypointRegistry: []string{},
	}
}

// TestMemberLinkerFieldEdges (W1-11, §5.4.1): type → field HAS_FIELD edges
// with registered FIELD nodes (structural spine).
func TestMemberLinkerFieldEdges(t *testing.T) {
	stage3Out := memberFixtureTree()
	cpg := BuildInitialNodes(stage3Out, nil)
	LinkMembersAndReturns(stage3Out, cpg)

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
	stage3Out := memberFixtureTree()
	cpg := BuildInitialNodes(stage3Out, nil)
	LinkMembersAndReturns(stage3Out, cpg)

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
	stage3Out := memberFixtureTree()
	cpg := BuildInitialNodes(stage3Out, nil)
	LinkMembersAndReturns(stage3Out, cpg)

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
	stage3Out := memberFixtureTree()
	cpg := BuildInitialNodes(stage3Out, nil)
	LinkMembersAndReturns(stage3Out, cpg)

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
	stage3Out := memberFixtureTree()
	cpg := BuildInitialNodes(stage3Out, nil)
	LinkMembersAndReturns(stage3Out, cpg)

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
	cpg := NewStage4Output("HEAD")
	cpg.GraphNodes["src/handler.py::Processor"] = &ResolvedNode{
		ID:   "src/handler.py::Processor",
		Kind: "CLASS",
		Name: "Processor",
	}

	globalIndex := map[string][]*stage2.GASTNode{
		"Processor": {{Name: "Processor", Properties: map[string]string{"file_path": "src/handler.py"}}},
	}

	targetFQN := resolveTypeToFQN("Processor", "src/client.py", globalIndex, cpg)
	assert.Equal(t, "src/handler.py::Processor", targetFQN, "T2 name-match fallback must resolve Processor to its FQN")
}
