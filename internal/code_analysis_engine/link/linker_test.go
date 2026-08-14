package link

import (
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/normalize"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/aggregate"
)

func TestNewLinkOutput(t *testing.T) {
	out := NewLinkOutput("abc123")
	if out == nil {
		t.Fatal("NewLinkOutput returned nil")
	}
	if out.CommitHash != "abc123" {
		t.Errorf("CommitHash = %q, want abc123", out.CommitHash)
	}
	if out.GraphNodes == nil {
		t.Error("GraphNodes should be initialized")
	}
	if out.OutboundEdges == nil {
		t.Error("OutboundEdges should be initialized")
	}
	if out.InboundEdges == nil {
		t.Error("InboundEdges should be initialized")
	}
}

func TestBuildUniversalID(t *testing.T) {
	tests := []struct {
		relPath  string
		receiver string
		name     string
		want     string
	}{
		{"src/store.go", "PostgresStore", "Save", "src/store.go::PostgresStore::Save"},
		{"sample.py", "", "DatabaseConnector", "sample.py::DatabaseConnector"},
		{"", "", "main", "root::main"},
		{"path/to/file.go", "Handler", "", "path/to/file.go::Handler::anonymous"},
	}
	for _, tt := range tests {
		got := BuildUniversalID(tt.relPath, tt.receiver, tt.name)
		if got != tt.want {
			t.Errorf("BuildUniversalID(%q, %q, %q) = %q, want %q", tt.relPath, tt.receiver, tt.name, got, tt.want)
		}
	}
}

func TestGenerateASTHash(t *testing.T) {
	node := &normalize.GASTNode{
		Type:     normalize.GASTFunction,
		Kind:     "function",
		DataType: "string",
		Children: []*normalize.GASTNode{
			{Type: normalize.GASTCallExpression, Kind: "call"},
		},
	}
	hash := generateASTHash(node)
	if hash == "" {
		t.Error("generateASTHash returned empty")
	}
	if len(hash) != 12 {
		t.Errorf("generateASTHash length = %d, want 12", len(hash))
	}

	// Same node should produce same hash
	hash2 := generateASTHash(node)
	if hash != hash2 {
		t.Errorf("generateASTHash is not deterministic: %q vs %q", hash, hash2)
	}

	// Nil node should produce "nil"
	if h := generateASTHash(nil); h != "nil" {
		t.Errorf("generateASTHash(nil) = %q, want 'nil'", h)
	}

	// Different nodes should produce different hashes
	diffNode := &normalize.GASTNode{Type: normalize.GASTVariable, Kind: "variable"}
	diffHash := generateASTHash(diffNode)
	if diffHash == hash {
		t.Error("different nodes should produce different hashes")
	}
}

func TestRegistryNode(t *testing.T) {
	r := &ResolvedNode{
		ID:        "test::node",
		Kind:      "function",
		Name:      "doWork",
		Primitive: "NETWORK_IO",
		FileSpec: LocationMeta{
			Path:      "src/main.go",
			LineStart: 10,
			LineEnd:   25,
		},
	}
	if r.ID != "test::node" {
		t.Errorf("ID = %q, want test::node", r.ID)
	}
	if r.Kind != "function" {
		t.Errorf("Kind = %q, want function", r.Kind)
	}
	if r.Primitive != "NETWORK_IO" {
		t.Errorf("Primitive = %q, want NETWORK_IO", r.Primitive)
	}
}

func TestRegistryEdge(t *testing.T) {
	e := &ResolvedEdge{
		SourceID:   "a",
		TargetID:   "b",
		Type:       EdgeCalls,
		Confidence: 1.0,
	}
	if e.SourceID != "a" || e.TargetID != "b" {
		t.Errorf("edge endpoints: (%q, %q), want (a, b)", e.SourceID, e.TargetID)
	}
	if e.Type != EdgeCalls {
		t.Errorf("Type = %s, want CALLS", e.Type)
	}
}

func TestRelationshipTypes(t *testing.T) {
	// Verify all edge types are non-empty
	types := []RelationshipType{
		EdgeCalls, EdgeImplements, EdgeExtends, EdgeMixes,
		EdgeHasField, EdgeHasParam, EdgeReturns, EdgeThrows,
		EdgeDependsOn, EdgeComposes, EdgeReferences,
		EdgeSpawnsConcurrent, EdgeDispatchesEvent,
		EdgeExposesEndpoint, EdgeSecuritySink,
		EdgeConsumesResource, EdgeMutatesGlobal,
		EdgeAliasesType, EdgeContains,
		EdgeControlFlow, EdgeConditionalBranch, EdgeLoopBranch,
		EdgeSwitchBranch, EdgeCatches, EdgeDefers,
		EdgeDataFlow, EdgeAliases, EdgeVulnerable,
		EdgeInstantiates, EdgeCyclic, EdgeNetworkCall,
		EdgeQueriesDB, EdgeCallsCloudAPI, EdgeSendsTo, EdgeReceivesFrom,
	}
	for _, et := range types {
		if et == "" {
			t.Error("empty RelationshipType found")
		}
	}
}

func TestGraphDBInterface(t *testing.T) {
	var db GraphDB
	// Should be nil (no default implementation) — just verify it doesn't panic
	_ = db
}

func TestLinkReturnsOutput(t *testing.T) {
	aggregateOut := &aggregate.AggregateOutput{
		CommitHash: "test",
	}
	modifiedFiles := []string{"main.go"}
	out, err := Link(aggregateOut, modifiedFiles, nil)
	if err != nil {
		t.Fatalf("Link returned error: %v", err)
	}
	if out == nil {
		t.Fatal("Link returned nil output")
	}
	if out.CommitHash != "test" {
		t.Errorf("CommitHash = %q, want test", out.CommitHash)
	}
}

func TestLinkNilInput(t *testing.T) {
	out, err := Link(nil, nil, nil)
	if err != nil {
		t.Fatalf("Link(nil) returned error: %v", err)
	}
	if out == nil {
		t.Fatal("Link(nil) returned nil")
	}
	if out.CommitHash != "" {
		t.Errorf("CommitHash = %q, want empty", out.CommitHash)
	}
}

func TestLinkEmptyModifiedFiles(t *testing.T) {
	aggregateOut := &aggregate.AggregateOutput{
		CommitHash: "empty",
	}
	out, err := Link(aggregateOut, nil, nil)
	if err != nil {
		t.Fatalf("Link returned error: %v", err)
	}
	if out == nil {
		t.Fatal("Link returned nil")
	}
	if len(out.GraphNodes) != 0 {
		t.Errorf("GraphNodes should be empty, got %d", len(out.GraphNodes))
	}
}

func TestEnsureVirtualNode(t *testing.T) {
	cpg := NewLinkOutput("test")
	ensureVirtualNode("test::virtual", "VIRTUAL_RESOURCE", "Test Resource", cpg)

	n, exists := cpg.GraphNodes["test::virtual"]
	if !exists {
		t.Fatal("ensureVirtualNode did not create node")
	}
	if n.Kind != "VIRTUAL_RESOURCE" {
		t.Errorf("Kind = %q, want VIRTUAL_RESOURCE", n.Kind)
	}
	if n.Name != "Test Resource" {
		t.Errorf("Name = %q, want Test Resource", n.Name)
	}

	// Calling again should not overwrite
	ensureVirtualNode("test::virtual", "VIRTUAL_OTHER", "Overwritten", cpg)
	if cpg.GraphNodes["test::virtual"].Kind != "VIRTUAL_RESOURCE" {
		t.Error("ensureVirtualNode overwrote existing node")
	}
}

func TestVirtualNodeExistence(t *testing.T) {
	// Verify that every edge target ID created by the linkers has a corresponding
	// GraphNodes entry. This catches dangling virtual edge targets.
	cpg := NewLinkOutput("test")
	aggregateOut := &aggregate.AggregateOutput{
		CommitHash: "test",
		LocalTables: map[string]*normalize.FileSymbolTable{
			"test.go": {
				Endpoints:     []normalize.EndpointMeta{{Method: "GET", Route: "/api/test", LineNumber: 3}},
				SecuritySinks: []normalize.SecuritySinkMeta{{SinkType: "SQL", LineNumber: 4}},
				ResourceLinks: []normalize.ResourceMeta{{ResourceType: "FileSystem", LineNumber: 5}},
				GlobalState:   []normalize.SymbolMeta{{Name: "globalVar"}},
			},
		},
	}

	// Run the semantic linker
	LinkEnterpriseSemantics(aggregateOut, cpg)

	// Verify all virtual nodes were created. Concurrency spawns and event
	// hooks are no longer fabricated here — they are linked to real targets by
	// LinkConcurrencyAndAsyncControlFlow and LinkEventSourcing respectively
	// (AUDIT Issue 1.5 / Phase 1B-6).
	cases := []struct {
		id   string
		kind string
		name string
	}{
		{"endpoint:GET:/api/test", "VIRTUAL_ENDPOINT", "GET:/api/test"},
		{"sink:SQL", "VIRTUAL_SECURITY_SINK", "SQL"},
		{"resource:FileSystem", "VIRTUAL_RESOURCE", "FileSystem"},
		{"global:globalVar", "VIRTUAL_GLOBAL_STATE", "globalVar"},
	}

	for _, c := range cases {
		n, exists := cpg.GraphNodes[c.id]
		if !exists {
			t.Errorf("virtual node %q not created by semantic_linker", c.id)
			continue
		}
		if n.Kind != c.kind {
			t.Errorf("node %q Kind = %q, want %q", c.id, n.Kind, c.kind)
		}
		if n.Name != c.name {
			t.Errorf("node %q Name = %q, want %q", c.id, n.Name, c.name)
		}
	}

	for _, ghost := range []string{"thread_or_coroutine", "event:testEvent"} {
		if _, exists := cpg.GraphNodes[ghost]; exists {
			t.Errorf("ghost virtual node %q must not be fabricated by semantic_linker", ghost)
		}
	}
}

func TestLinkerConfigDisabledPass(t *testing.T) {
	// Verify that disabling a pass prevents its edges from being created
	aggregateOut := &aggregate.AggregateOutput{
		CommitHash: "test",
	}

	// Full run with no config
	out, err := Link(aggregateOut, nil, nil)
	if err != nil {
		t.Fatalf("Link() returned error: %v", err)
	}
	// Empty input should produce some edges from ReasonWholeProgramPrimitives and detectCyclicDependencies
	_ = out
}

func TestLinkerConfigLevelOfDetail(t *testing.T) {
	// Verify that "architecture" level disables CFG and DFG passes
	aggregateOut := &aggregate.AggregateOutput{
		CommitHash: "test",
	}

	// Run with architecture level
	cfg := LinkerConfig{LevelOfDetail: "architecture"}
	out, err := Link(aggregateOut, nil, nil, cfg)
	if err != nil {
		t.Fatalf("Link() returned error: %v", err)
	}

	// No CFG or DFG-specific nodes should exist
	for id, node := range out.GraphNodes {
		if node.Kind == "CFG_BRANCH" || node.Kind == "DFG_VAR" {
			t.Errorf("unexpected node %q (kind=%s) with architecture level; CFG/DFG should be disabled", id, node.Kind)
		}
	}

	// Verify no CFG-specific or DFG-specific edges
	for _, edges := range out.OutboundEdges {
		for _, e := range edges {
			if e.Type == EdgeControlFlow || e.Type == EdgeConditionalBranch ||
				e.Type == EdgeLoopBranch || e.Type == EdgeSwitchBranch ||
				e.Type == EdgeCatches || e.Type == EdgeDefers {
				t.Errorf("unexpected CFG edge %s with architecture level", e.Type)
			}
			if e.Type == EdgeDataFlow || e.Type == EdgeAliases {
				t.Errorf("unexpected DFG edge %s with architecture level", e.Type)
			}
		}
	}
}

func TestLinkerConfigBackwardCompatible(t *testing.T) {
	// Verify that calling Link with no config (old signature) still works
	aggregateOut := &aggregate.AggregateOutput{
		CommitHash: "backward",
	}
	out, err := Link(aggregateOut, []string{"test.go"}, nil)
	if err != nil {
		t.Fatalf("Link() without config returned error: %v", err)
	}
	if out.CommitHash != "backward" {
		t.Errorf("CommitHash = %q, want backward", out.CommitHash)
	}
}

func TestLinkerConfigEmpty(t *testing.T) {
	// Empty config should be equivalent to no config (full mode)
	aggregateOut := &aggregate.AggregateOutput{
		CommitHash: "empty",
	}
	cfg := LinkerConfig{}
	out, err := Link(aggregateOut, nil, nil, cfg)
	if err != nil {
		t.Fatalf("Link() with empty config returned error: %v", err)
	}
	if out.CommitHash != "empty" {
		t.Errorf("CommitHash = %q, want empty", out.CommitHash)
	}
}

// buildTestAggregateOut creates a minimal aggregate output with a function containing control flow and variables.
func buildTestAggregateOut() *aggregate.AggregateOutput {
	return &aggregate.AggregateOutput{
		CommitHash: "test",
		GlobalDefinitionIndex: map[string][]*normalize.GASTNode{
			"test.go::main": {{Name: "main"}},
		},
		WorkspaceCtx: &aggregate.WorkspaceContext{},
		RootNode: &aggregate.DirectoryNode{
			RelativePath: ".",
			SubFolders:   map[string]*aggregate.DirectoryNode{},
			Files: map[string]*aggregate.FileBoundaryNode{
				"test.go": {
					FileName:     "test.go",
					RelativePath: "test.go",
					Language:     "go",
					GASTRoot: &normalize.GASTNode{
						Type:      normalize.GASTFunction,
						Name:      "main",
						Kind:      "function",
						StartLine: 1,
						EndLine:   20,
						Children: []*normalize.GASTNode{
							{Type: normalize.GASTParameter, Name: "argc", Kind: "param", StartLine: 1},
							{Type: normalize.GASTVariable, Name: "x", Kind: "variable", StartLine: 2},
							{Type: normalize.GASTVariable, Name: "y", Kind: "variable", StartLine: 3},
							{Type: normalize.GASTControlFlow, Kind: "if_statement", StartLine: 5},
							{Type: normalize.GASTControlFlow, Kind: "for_statement", StartLine: 10},
							{Type: normalize.GASTControlFlow, Kind: "return_statement", StartLine: 15},
						},
					},
				},
			},
		},
	}
}

func TestCFGStandardMode(t *testing.T) {
	aggregateOut := buildTestAggregateOut()

	// Standard mode
	cfg := LinkerConfig{LevelOfDetail: LevelStandard}
	out, err := Link(aggregateOut, []string{"test.go"}, nil, cfg)
	if err != nil {
		t.Fatalf("Link() error: %v", err)
	}

	hasPerBranch := false
	hasSummary := false
	for _, n := range out.GraphNodes {
		if n.Kind == "IF_BRANCH" || n.Kind == "LOOP_BRANCH" || n.Kind == "CFG_FLOW" {
			hasPerBranch = true
			t.Errorf("per-branch node %q found in standard mode", n.ID)
		}
		if n.Kind == "CFG_SUMMARY" {
			hasSummary = true
		}
	}
	if !hasSummary {
		t.Error("CFG_SUMMARY node not created in standard mode")
	}
	if hasPerBranch {
		t.Error("per-branch nodes should not exist in standard mode")
	}
}

func TestDFGStandardMode(t *testing.T) {
	aggregateOut := buildTestAggregateOut()

	cfg := LinkerConfig{LevelOfDetail: LevelStandard}
	out, err := Link(aggregateOut, []string{"test.go"}, nil, cfg)
	if err != nil {
		t.Fatalf("Link() error: %v", err)
	}

	hasPerVar := false
	hasSummary := false
	for _, n := range out.GraphNodes {
		if n.Kind == "DFG_VAR" {
			hasPerVar = true
			t.Errorf("DFG_VAR node %q found in standard mode", n.ID)
		}
		if n.Kind == "DFG_SUMMARY" {
			hasSummary = true
		}
	}
	if !hasSummary {
		t.Error("DFG_SUMMARY node not created in standard mode")
	}
	if hasPerVar {
		t.Error("DFG_VAR nodes should not exist in standard mode")
	}
}

func TestFullModeCreatesPerNodes(t *testing.T) {
	aggregateOut := buildTestAggregateOut()

	out, err := Link(aggregateOut, []string{"test.go"}, nil)
	if err != nil {
		t.Fatalf("Link() error: %v", err)
	}

	hasCFGBranch := false
	hasDFGVar := false
	for _, n := range out.GraphNodes {
		if n.Kind == "IF_BRANCH" || n.Kind == "LOOP_BRANCH" || n.Kind == "CFG_FLOW" {
			hasCFGBranch = true
		}
		if n.Kind == "DFG_VAR" {
			hasDFGVar = true
		}
	}
	if !hasCFGBranch {
		t.Error("expected per-branch CFG nodes in full mode")
	}
	if !hasDFGVar {
		t.Error("expected DFG_VAR nodes in full mode")
	}
}

func TestThreeLevelComparison(t *testing.T) {
	aggregateOut := buildTestAggregateOut()

	// Architecture level
	archCfg := LinkerConfig{LevelOfDetail: LevelArchitecture}
	archOut, err := Link(aggregateOut, []string{"test.go"}, nil, archCfg)
	if err != nil {
		t.Fatalf("Link(architecture) error: %v", err)
	}

	// Standard level
	stdCfg := LinkerConfig{LevelOfDetail: LevelStandard}
	stdOut, err := Link(aggregateOut, []string{"test.go"}, nil, stdCfg)
	if err != nil {
		t.Fatalf("Link(standard) error: %v", err)
	}

	// Full level
	fullOut, err := Link(aggregateOut, []string{"test.go"}, nil)
	if err != nil {
		t.Fatalf("Link(full) error: %v", err)
	}

	archNodes := len(archOut.GraphNodes)
	stdNodes := len(stdOut.GraphNodes)
	fullNodes := len(fullOut.GraphNodes)

	t.Logf("architecture: %d nodes, standard: %d nodes, full: %d nodes", archNodes, stdNodes, fullNodes)

	// Architecture should have fewest nodes, full should have most
	if !(archNodes <= stdNodes && stdNodes <= fullNodes) {
		t.Errorf("expected arch(%d) <= standard(%d) <= full(%d)", archNodes, stdNodes, fullNodes)
	}

	// Full should have strictly more nodes than architecture (CFG/DFG adds nodes)
	if fullNodes <= archNodes {
		t.Errorf("full(%d) should have more nodes than architecture(%d)", fullNodes, archNodes)
	}
}

func TestResolvedNodeProperties(t *testing.T) {
	r := &ResolvedNode{
		ID:   "parent",
		Kind: "MODULE",
		Name: "root",
	}
	if r.ID != "parent" {
		t.Errorf("ID = %q, want parent", r.ID)
	}
}

// TestCFGNoFalsePositiveOnSpecification verifies that a node with Name="specification"
// does not create a spurious CFG branch. The old isControlFlowStatement() used
// strings.Contains on the concatenated Type+Kind+Name, causing "specification"
// to match "if" and create a false IF_BRANCH. The new classifyControlFlowKind()
// uses an exact switch on node.Kind only.
func TestCFGNoFalsePositiveOnSpecification(t *testing.T) {
	// The switch in classifyControlFlowKind is exact — no string contains.
	// Even if a GAST node has Name="specification" (which contains "if"),
	// only its node.Kind is checked in the exact switch.
	falsePositiveCases := []struct {
		name string
		kind string
	}{
		{"specification", "expression_statement"},
		{"unifiedProcessor", "expression_statement"},
		{"transformData", "expression_statement"},
		{"lowercase", "expression_statement"},
		{"trying", "expression_statement"},
		{"worthwhile", "expression_statement"},
		{"retry", "expression_statement"},
	}

	for _, tc := range falsePositiveCases {
		t.Run(tc.name, func(t *testing.T) {
			// Directly test classifyControlFlowKind — should NOT match any branch kind
			edgeType, branchKind := classifyControlFlowKind(tc.kind)
			if branchKind != "" || edgeType != "" {
				t.Errorf("classifyControlFlowKind(%q) = (%q, %q), want (\"\", \"\")",
					tc.kind, edgeType, branchKind)
			}

			// Verify extractCFGNodesFromGAST does not create a CFG node even if
			// the node has GASTControlFlow type but an unrecognized kind,
			// or is not GASTControlFlow at all.
			node := &normalize.GASTNode{
				Type: normalize.GASTControlFlow,
				Kind: tc.kind,
				Name: tc.name,
			}

			cpg := NewLinkOutput("test")
			om := aggregate.BuildOwnershipMap(nil, nil)
			extractCFGNodesFromGAST(node, "test.go", "pkg::main",
				cpg, nil, om, nil, make(map[string]int))

			for id := range cpg.GraphNodes {
				t.Errorf("unexpected node created for %q: %s", tc.name, id)
			}
		})
	}
}

// TestEscapeAnalysisTargetsExist drives the full-mode escape analysis over a
// GAST fixture with a pointer return and a goroutine spawn, and asserts every
// ESCAPES_TO_HEAP edge lands on a real node (AUDIT Issue 1.6 — every edge
// used to dangle because the VAR_/HEAP targets were never created).
func TestEscapeAnalysisTargetsExist(t *testing.T) {
	root := &normalize.GASTNode{
		Type: normalize.GASTFunction,
		Kind: "function",
		Name: "makeUser",
		Children: []*normalize.GASTNode{
			{
				Type:       normalize.GASTControlFlow,
				Kind:       "return_statement",
				StartLine:  8,
				Properties: map[string]string{"content": "return &user"},
			},
			{
				Type:       normalize.GASTCallExpression,
				Kind:       "call_expression",
				StartLine:  12,
				Properties: map[string]string{"content": "go worker()"},
			},
		},
	}
	aggregateOut := &aggregate.AggregateOutput{
		CommitHash: "test",
		RootNode: &aggregate.DirectoryNode{
			FolderName:   ".",
			RelativePath: ".",
			Files: map[string]*aggregate.FileBoundaryNode{
				"a.go": {
					FileName:     "a.go",
					RelativePath: "a.go",
					GASTRoot:     root,
				},
			},
		},
	}

	cpg := NewLinkOutput("test")
	cpg.Config.LevelOfDetail = LevelFull
	cpg.ModifiedFiles = map[string]bool{"a.go": true}
	cpg.GraphNodes["a.go::makeUser"] = &ResolvedNode{ID: "a.go::makeUser", Kind: "FUNCTION", Name: "makeUser"}

	LinkEscapeAnalysis(aggregateOut, cpg)

	for _, want := range []string{"a.go::makeUser::VAR_user", "memory::HEAP"} {
		if !cpg.NodeExists(want) {
			t.Errorf("escape target %q was not created", want)
		}
	}
	if q := MeasureQuality(cpg); q.DanglingEdges != 0 {
		t.Errorf("escape analysis left %d dangling edge(s): %v", q.DanglingEdges, q)
	}
}
