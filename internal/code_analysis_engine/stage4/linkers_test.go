package stage4

import (
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage2"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage3"
)

// ---------------------------------------------------------------------------
// AddEdge / edge symmetry helpers
// ---------------------------------------------------------------------------

func TestAddEdgeSymmetryAndDedupe(t *testing.T) {
	cpg := NewStage4Output("t")
	cpg.AddEdge("a", "b", EdgeCalls, 3)
	cpg.AddEdge("a", "b", EdgeCalls, 3) // duplicate: ignored

	if len(cpg.OutboundEdges["a"]) != 1 {
		t.Fatalf("outbound edges = %d, want 1", len(cpg.OutboundEdges["a"]))
	}
	if len(cpg.InboundEdges["b"]) != 1 {
		t.Fatalf("inbound edges = %d, want 1", len(cpg.InboundEdges["b"]))
	}
	// Same endpoints, different line → distinct edge
	cpg.AddEdge("a", "b", EdgeCalls, 4)
	if len(cpg.OutboundEdges["a"]) != 2 {
		t.Fatalf("outbound edges after distinct line = %d, want 2", len(cpg.OutboundEdges["a"]))
	}
}

func TestAddEdgeRejectsEmptyAndSelfLoop(t *testing.T) {
	cpg := NewStage4Output("t")
	cpg.AddEdge("", "b", EdgeCalls, 1)
	cpg.AddEdge("a", "", EdgeCalls, 1)
	cpg.AddEdge("a", "a", EdgeCalls, 1) // self loop

	if len(cpg.OutboundEdges) != 0 {
		t.Fatalf("expected no edges, got %v", cpg.OutboundEdges)
	}
}

func TestAddEdgeWithConfidence(t *testing.T) {
	cpg := NewStage4Output("t")
	cpg.AddEdgeWithConfidence("a", "b", EdgeCalls, 7, 0.7)
	edges := cpg.OutboundEdges["a"]
	if len(edges) != 1 {
		t.Fatalf("edges = %d, want 1", len(edges))
	}
	if edges[0].Confidence != 0.7 {
		t.Errorf("confidence = %v, want 0.7", edges[0].Confidence)
	}
	if edges[0].LineNumber != 7 {
		t.Errorf("line = %d, want 7", edges[0].LineNumber)
	}
}

// ---------------------------------------------------------------------------
// BuildInitialNodes
// ---------------------------------------------------------------------------

func TestBuildInitialNodesFull(t *testing.T) {
	root := &stage2.GASTNode{
		Type: stage2.GASTTypeDeclaration,
		Name: "User",
		Kind: "class",
		Children: []*stage2.GASTNode{
			{
				Type:      stage2.GASTFunction,
				Name:      "Save",
				Kind:      "method",
				StartLine: 4,
				EndLine:   9,
			},
		},
	}
	stage3Out := &stage3.Stage3Output{
		CommitHash: "h",
		RootNode: &stage3.DirectoryNode{
			FolderName:   ".",
			RelativePath: ".",
			SubFolders: map[string]*stage3.DirectoryNode{
				"src": {
					FolderName:   "src",
					RelativePath: "src",
					Files: map[string]*stage3.FileBoundaryNode{
						"user.go": {
							FileName:     "user.go",
							RelativePath: "src/user.go",
							Language:     "go",
							GASTRoot:     root,
						},
					},
				},
			},
		},
	}

	// Empty modifiedFiles → full mode (all files processed)
	out := BuildInitialNodes(stage3Out, nil)

	if _, ok := out.GraphNodes["module:src"]; !ok {
		t.Error("module:src node not created")
	}
	if _, ok := out.GraphNodes["file:src/user.go"]; !ok {
		t.Error("file:src/user.go node not created")
	}
	if _, ok := out.GraphNodes["src/user.go::User"]; !ok {
		t.Error("src/user.go::User node not created")
	}
	if _, ok := out.GraphNodes["src/user.go::User::Save"]; !ok {
		t.Error("src/user.go::User::Save method node not created")
	}
}

func TestBuildInitialNodesDelta(t *testing.T) {
	root := &stage2.GASTNode{
		Type: stage2.GASTFunction,
		Name: "main",
		Kind: "function",
	}
	stage3Out := &stage3.Stage3Output{
		CommitHash: "h",
		RootNode: &stage3.DirectoryNode{
			FolderName:   ".",
			RelativePath: ".",
			Files: map[string]*stage3.FileBoundaryNode{
				"a.go": {FileName: "a.go", RelativePath: "a.go", GASTRoot: root},
				"b.go": {
					FileName:     "b.go",
					RelativePath: "b.go",
					GASTRoot:     &stage2.GASTNode{Type: stage2.GASTFunction, Name: "other", Kind: "function"},
				},
			},
		},
	}

	out := BuildInitialNodes(stage3Out, []string{"a.go"})
	if _, ok := out.GraphNodes["a.go::main"]; !ok {
		t.Error("modified file node missing")
	}
	if _, ok := out.GraphNodes["b.go::other"]; ok {
		t.Error("unmodified file should be skipped in delta mode")
	}
}

// ---------------------------------------------------------------------------
// LinkTypesAndComposition
// ---------------------------------------------------------------------------

func TestLinkTypesExtendsAndComposes(t *testing.T) {
	base := &stage2.GASTNode{
		Type: stage2.GASTTypeDeclaration,
		Name: "Base",
		Kind: "class",
		Properties: map[string]string{
			"file_path": "a.go",
		},
	}
	child := &stage2.GASTNode{
		Type: stage2.GASTTypeDeclaration,
		Name: "Child",
		Kind: "class",
		Properties: map[string]string{
			"extends":   "Base",
			"file_path": "a.go",
		},
		Children: []*stage2.GASTNode{
			{
				Type:     stage2.GASTField,
				Name:     "svc",
				DataType: "Base",
				StartLine: 8,
			},
			{
				Type:     stage2.GASTField,
				Name:     "items",
				DataType: "List<Base>",
				StartLine: 9,
			},
		},
	}
	stage3Out := &stage3.Stage3Output{
		CommitHash: "t",
		GlobalDefinitionIndex: map[string][]*stage2.GASTNode{
			"a.go::Base": {base},
			"a.go::Child": {child},
		},
		RootNode: &stage3.DirectoryNode{
			FolderName:   ".",
			RelativePath: ".",
			Files: map[string]*stage3.FileBoundaryNode{
				"a.go": {FileName: "a.go", RelativePath: "a.go", GASTRoot: child},
			},
		},
	}

	cpg := NewStage4Output("t")
	cpg.ModifiedFiles = map[string]bool{"a.go": true}
	cpg.GraphNodes["a.go::Base"] = &ResolvedNode{ID: "a.go::Base", Kind: "CLASS", Name: "Base"}
	cpg.GraphNodes["a.go::Child"] = &ResolvedNode{ID: "a.go::Child", Kind: "CLASS", Name: "Child"}

	LinkTypesAndComposition(stage3Out, cpg)

	hasExtends := hasEdge(cpg, "a.go::Child", "a.go::Base", EdgeExtends)
	hasComposes := hasEdge(cpg, "a.go::Child", "a.go::Base", EdgeComposes)
	hasInstantiates := hasEdge(cpg, "a.go::Child", "a.go::Base", EdgeInstantiates)
	if !hasExtends {
		t.Error("expected EXTENDS edge from Child to Base")
	}
	if !hasComposes {
		t.Error("expected COMPOSES edge for field of type Base")
	}
	if !hasInstantiates {
		t.Error("expected INSTANTIATES edge for List<Base> generic")
	}
}

// ---------------------------------------------------------------------------
// LinkInterfacesAndRealizations
// ---------------------------------------------------------------------------

func TestLinkImplementsWhenMethodsSatisfied(t *testing.T) {
	iface := &ResolvedNode{ID: "a.go::Shape", Kind: "INTERFACE", Name: "Shape", FileSpec: LocationMeta{Path: "a.go"}}
	strct := &ResolvedNode{ID: "b.go::Circle", Kind: "STRUCT", Name: "Circle", FileSpec: LocationMeta{Path: "b.go"}}

	cpg := NewStage4Output("t")
	cpg.ModifiedFiles = map[string]bool{"a.go": true, "b.go": true}
	cpg.GraphNodes[iface.ID] = iface
	cpg.GraphNodes[strct.ID] = strct
	// interface methods
	for _, m := range []string{"Area", "Perimeter"} {
		cpg.GraphNodes[iface.ID+"::"+m] = &ResolvedNode{ID: iface.ID + "::" + m, Kind: "METHOD", Name: m}
	}
	// struct implements both
	for _, m := range []string{"Area", "Perimeter"} {
		cpg.GraphNodes[strct.ID+"::"+m] = &ResolvedNode{ID: strct.ID + "::" + m, Kind: "METHOD", Name: m}
	}

	stage3Out := &stage3.Stage3Output{CommitHash: "t", GlobalDefinitionIndex: map[string][]*stage2.GASTNode{}}
	LinkInterfacesAndRealizations(stage3Out, cpg)

	if !hasEdge(cpg, strct.ID, iface.ID, EdgeImplements) {
		t.Error("expected IMPLEMENTS edge from Circle to Shape")
	}
}

func TestLinkImplementsFalseNegative(t *testing.T) {
	iface := &ResolvedNode{ID: "a.go::Shape", Kind: "INTERFACE", Name: "Shape", FileSpec: LocationMeta{Path: "a.go"}}
	strct := &ResolvedNode{ID: "b.go::Square", Kind: "STRUCT", Name: "Square", FileSpec: LocationMeta{Path: "b.go"}}

	cpg := NewStage4Output("t")
	cpg.ModifiedFiles = map[string]bool{"a.go": true, "b.go": true}
	cpg.GraphNodes[iface.ID] = iface
	cpg.GraphNodes[strct.ID] = strct
	cpg.GraphNodes[iface.ID+"::Area"] = &ResolvedNode{ID: iface.ID + "::Area", Kind: "METHOD", Name: "Area"}
	cpg.GraphNodes[iface.ID+"::Perimeter"] = &ResolvedNode{ID: iface.ID + "::Perimeter", Kind: "METHOD", Name: "Perimeter"}
	// Square only implements Area → no IMPLEMENTS edge
	cpg.GraphNodes[strct.ID+"::Area"] = &ResolvedNode{ID: strct.ID + "::Area", Kind: "METHOD", Name: "Area"}

	stage3Out := &stage3.Stage3Output{CommitHash: "t", GlobalDefinitionIndex: map[string][]*stage2.GASTNode{}}
	LinkInterfacesAndRealizations(stage3Out, cpg)

	if hasEdge(cpg, strct.ID, iface.ID, EdgeImplements) {
		t.Error("should NOT have IMPLEMENTS edge when a method is missing")
	}
}

// ---------------------------------------------------------------------------
// LinkCallGraph
// ---------------------------------------------------------------------------

func TestResolveCallTargetStaticSameFile(t *testing.T) {
	cpg := NewStage4Output("t")
	cpg.GraphNodes["svc.go::Handler::Process"] = &ResolvedNode{ID: "svc.go::Handler::Process", Kind: "METHOD", Name: "Process"}
	stage3Out := &stage3.Stage3Output{CommitHash: "t", WorkspaceCtx: stage3.NewWorkspaceContext()}

	id, conf := resolveCallTarget("Handler", "Process", "svc.go", nil, nil, cpg, stage3Out)
	if id != "svc.go::Handler::Process" {
		t.Errorf("target = %q, want svc.go::Handler::Process", id)
	}
	if conf != 1.0 {
		t.Errorf("confidence = %v, want 1.0", conf)
	}
}

func TestResolveCallTargetUnresolved(t *testing.T) {
	cpg := NewStage4Output("t")
	stage3Out := &stage3.Stage3Output{CommitHash: "t", WorkspaceCtx: stage3.NewWorkspaceContext()}
	id, conf := resolveCallTarget("Missing", "Nope", "x.go", nil, nil, cpg, stage3Out)
	if id != "" || conf != 0.0 {
		t.Errorf("expected unresolved (\"\", 0), got (%q, %v)", id, conf)
	}
}

func TestLinkCallGraphBasic(t *testing.T) {
	// caller: main() calls Process() on a local receiver
	callSite := stage3.LinkedCallSite{
		SourceFilePath: "svc.go",
		SourceFileNodeID: "svc.go::main",
		ReceiverName:   "Handler",
		MethodName:     "Process",
		LineNumber:     12,
	}
	stage3Out := &stage3.Stage3Output{
		CommitHash: "t",
		GlobalCallQueue: []stage3.LinkedCallSite{callSite},
		WorkspaceCtx: stage3.NewWorkspaceContext(),
	}

	cpg := NewStage4Output("t")
	cpg.ModifiedFiles = map[string]bool{"svc.go": true}
	cpg.GraphNodes["svc.go::main"] = &ResolvedNode{ID: "svc.go::main", Kind: "FUNCTION", Name: "main"}
	cpg.GraphNodes["svc.go::Handler::Process"] = &ResolvedNode{ID: "svc.go::Handler::Process", Kind: "METHOD", Name: "Process"}

	LinkCallGraph(stage3Out, cpg)

	if !hasEdge(cpg, "svc.go::main", "svc.go::Handler::Process", EdgeCalls) {
		t.Error("expected CALLS edge from main to Handler.Process")
	}
}

func TestReceiverTypeMatches(t *testing.T) {
	cases := []struct {
		receiver, receiverType, prop string
		want                         bool
	}{
		{"store", "Store", "", true},
		{"a.store", "Store", "", true},
		{"my_store", "MyStore", "", true},
		{"store", "Unrelated", "", false},
		{"", "Store", "", false},
		{"store", "", "", false},
		{"store", "", "Different", false},
		{"store", "", "", false},
	}
	for _, tc := range cases {
		if got := receiverTypeMatches(tc.receiver, tc.receiverType, tc.prop); got != tc.want {
			t.Errorf("receiverTypeMatches(%q,%q,%q) = %v, want %v", tc.receiver, tc.receiverType, tc.prop, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// LinkFileDependencies + cycle detection
// ---------------------------------------------------------------------------

func TestLinkFileDependencies(t *testing.T) {
	stage3Out := &stage3.Stage3Output{
		CommitHash: "t",
		RootNode: &stage3.DirectoryNode{
			FolderName:   ".",
			RelativePath: ".",
			Files: map[string]*stage3.FileBoundaryNode{
				"a.go": {
					FileName:     "a.go",
					RelativePath: "a.go",
					LocalImports: []string{"b"},
				},
				"b.go": {
					FileName:     "b.go",
					RelativePath: "b.go",
					LocalImports: []string{},
				},
			},
		},
	}
	cpg := NewStage4Output("t")
	cpg.ModifiedFiles = map[string]bool{"a.go": true, "b.go": true}
	cpg.GraphNodes["file:a.go"] = &ResolvedNode{ID: "file:a.go", Kind: "FILE", Name: "a.go"}
	cpg.GraphNodes["file:b.go"] = &ResolvedNode{ID: "file:b.go", Kind: "FILE", Name: "b.go"}

	LinkFileDependencies(stage3Out, cpg)
	if !hasEdge(cpg, "file:a.go", "file:b.go", EdgeDependsOn) {
		t.Error("expected DEPENDS_ON edge from file:a.go to file:b.go")
	}
}

func TestDetectCyclicDependencies(t *testing.T) {
	cpg := NewStage4Output("t")
	cpg.GraphNodes["file:a.go"] = &ResolvedNode{ID: "file:a.go", Kind: "FILE", Name: "a.go"}
	cpg.GraphNodes["file:b.go"] = &ResolvedNode{ID: "file:b.go", Kind: "FILE", Name: "b.go"}
	cpg.GraphNodes["file:c.go"] = &ResolvedNode{ID: "file:c.go", Kind: "FILE", Name: "c.go"}
	cpg.AddEdge("file:a.go", "file:b.go", EdgeDependsOn, 0)
	cpg.AddEdge("file:b.go", "file:a.go", EdgeDependsOn, 0)
	cpg.AddEdge("file:c.go", "file:a.go", EdgeDependsOn, 0)

	detectCyclicDependencies(cpg)

	if !hasEdge(cpg, "file:a.go", "file:b.go", EdgeCyclic) {
		t.Error("expected CYCLIC_DEPENDENCY edge a→b")
	}
	if !hasEdge(cpg, "file:b.go", "file:a.go", EdgeCyclic) {
		t.Error("expected CYCLIC_DEPENDENCY edge b→a")
	}
	if hasEdge(cpg, "file:c.go", "file:a.go", EdgeCyclic) {
		t.Error("c→a is acyclic and must not be marked CYCLIC_DEPENDENCY")
	}
}

// ---------------------------------------------------------------------------
// Semantic + concurrency linkers
// ---------------------------------------------------------------------------

func TestLinkSemanticEndpointsAndSinks(t *testing.T) {
	stage3Out := &stage3.Stage3Output{
		CommitHash: "t",
		LocalTables: map[string]*stage2.FileSymbolTable{
			"api.go": {
				Endpoints:     []stage2.EndpointMeta{{Method: "GET", Route: "/api/users", LineNumber: 5}},
				SecuritySinks: []stage2.SecuritySinkMeta{{SinkType: "SQL", LineNumber: 6}},
				ResourceLinks: []stage2.ResourceMeta{{ResourceType: "FileSystem", LineNumber: 7}},
				GlobalState:   []stage2.SymbolMeta{{Name: "cache"}},
			},
		},
	}
	cpg := NewStage4Output("t")
	cpg.ModifiedFiles = map[string]bool{"api.go": true}

	LinkEnterpriseSemantics(stage3Out, cpg)

	if !cpg.NodeExists("endpoint:GET:/api/users") {
		t.Error("endpoint node not created")
	}
	if !hasEdge(cpg, "file:api.go", "endpoint:GET:/api/users", EdgeExposesEndpoint) {
		t.Error("expected EXPOSES_ENDPOINT edge")
	}
	if !hasEdge(cpg, "file:api.go", "sink:SQL", EdgeSecuritySink) {
		t.Error("expected SECURITY_SINK edge")
	}
	if !hasEdge(cpg, "file:api.go", "resource:FileSystem", EdgeConsumesResource) {
		t.Error("expected CONSUMES_RESOURCE edge")
	}
	if !hasEdge(cpg, "file:api.go", "global:cache", EdgeMutatesGlobal) {
		t.Error("expected MUTATES_GLOBAL edge")
	}
}

func TestLinkConcurrencyAndMessagePassing(t *testing.T) {
	stage3Out := &stage3.Stage3Output{
		CommitHash: "t",
		GlobalCallQueue: []stage3.LinkedCallSite{
			{
				SourceFilePath: "async.go",
				SourceFileNodeID: "async.go::run",
				SourceFolderPath: "async.go",
				ReceiverName:   "worker",
				MethodName:     "Process",
				LineNumber:     3,
				Primitives:     []stage2.BehavioralPrimitive{stage2.PrimConcurrency},
			},
			{
				SourceFilePath: "mq.go",
				SourceFileNodeID: "mq.go::pump",
				ReceiverName:   "events",
				MethodName:     "sendMessage",
				LineNumber:     9,
			},
		},
		WorkspaceCtx: stage3.NewWorkspaceContext(),
	}
	cpg := NewStage4Output("t")
	cpg.ModifiedFiles = map[string]bool{"async.go": true, "mq.go": true}
	cpg.GraphNodes["async.go::run"] = &ResolvedNode{ID: "async.go::run", Kind: "FUNCTION", Name: "run"}
	cpg.GraphNodes["mq.go::pump"] = &ResolvedNode{ID: "mq.go::pump", Kind: "FUNCTION", Name: "pump"}
	cpg.GraphNodes["async.go::worker::Process"] = &ResolvedNode{ID: "async.go::worker::Process", Kind: "METHOD", Name: "Process"}

	LinkConcurrencyAndAsyncControlFlow(stage3Out, cpg)

	if !hasEdge(cpg, "async.go::run", "async.go::worker::Process", EdgeSpawnsConcurrent) {
		t.Error("expected SPAWNS_CONCURRENT edge")
	}
	if !cpg.NodeExists("QUEUE::events") {
		t.Error("virtual queue node not created")
	}
	if !hasEdge(cpg, "mq.go::pump", "QUEUE::events", EdgeSendsTo) {
		t.Error("expected SENDS_MSG edge")
	}
}

// ---------------------------------------------------------------------------
// Event sourcing, FFI, DI, RPC
// ---------------------------------------------------------------------------

func TestLinkEventSourcing(t *testing.T) {
	root := &stage2.GASTNode{
		Type: stage2.GASTFunction,
		Name: "publisher",
		Kind: "function",
		Children: []*stage2.GASTNode{
			{
				Type:       stage2.GASTCallExpression,
				Name:       "kafka.publish",
				Kind:       "call",
				StartLine:  4,
				Properties: map[string]string{"content": `kafka.publish("orders.created")`},
			},
		},
	}
	stage3Out := &stage3.Stage3Output{
		CommitHash: "t",
		RootNode: &stage3.DirectoryNode{
			FolderName:   ".",
			RelativePath: ".",
			Files: map[string]*stage3.FileBoundaryNode{
				"events.go": {FileName: "events.go", RelativePath: "events.go", GASTRoot: root},
			},
		},
	}
	cpg := NewStage4Output("t")
	cpg.ModifiedFiles = map[string]bool{"events.go": true}
	cpg.GraphNodes["events.go::publisher"] = &ResolvedNode{ID: "events.go::publisher", Kind: "FUNCTION", Name: "publisher"}

	LinkEventSourcing(stage3Out, cpg)

	if !cpg.NodeExists("topic::orders.created") {
		t.Fatalf("topic node not created: %v", cpg.GraphNodes)
	}
	if !hasEdge(cpg, "events.go::publisher", "topic::orders.created", EdgePublishes) {
		t.Error("expected PUBLISHES_EVENT edge")
	}
}

func TestLinkFFI(t *testing.T) {
	root := &stage2.GASTNode{
		Type: stage2.GASTFunction,
		Name: "Wrap",
		Kind: "function",
		Children: []*stage2.GASTNode{
			{Type: stage2.GASTImport, Name: `"C"`, Kind: "import"},
			{
				Type:       stage2.GASTCallExpression,
				Name:       "C.malloc",
				Kind:       "call",
				StartLine:  12,
				Properties: map[string]string{"content": "C.malloc(128)"},
			},
		},
	}
	stage3Out := &stage3.Stage3Output{
		CommitHash: "t",
		RootNode: &stage3.DirectoryNode{
			FolderName:   ".",
			RelativePath: ".",
			Files: map[string]*stage3.FileBoundaryNode{
				"cgo.go": {FileName: "cgo.go", RelativePath: "cgo.go", GASTRoot: root},
			},
		},
	}
	cpg := NewStage4Output("t")
	cpg.ModifiedFiles = map[string]bool{"cgo.go": true}
	cpg.GraphNodes["cgo.go::Wrap"] = &ResolvedNode{ID: "cgo.go::Wrap", Kind: "FUNCTION", Name: "Wrap"}

	LinkFFI(stage3Out, cpg)

	if !cpg.NodeExists("ffi:C::malloc") {
		t.Error("FFI node not created")
	}
	if !hasEdge(cpg, "cgo.go::Wrap", "ffi:C::malloc", EdgeFFICall) {
		t.Error("expected FFI_CALL edge")
	}
}

func TestLinkDependencyInjection(t *testing.T) {
	root := &stage2.GASTNode{
		Type: stage2.GASTFunction,
		Name: "wireApp",
		Kind: "function",
		Children: []*stage2.GASTNode{
			{
				Type:       stage2.GASTCallExpression,
				Name:       "wire.Build",
				Kind:       "call",
				StartLine:  8,
				Properties: map[string]string{"content": "wire.Build(NewUserService)"},
			},
		},
	}
	stage3Out := &stage3.Stage3Output{
		CommitHash: "t",
		GlobalDefinitionIndex: map[string][]*stage2.GASTNode{
			"wire.go::NewUserService": {{Name: "NewUserService", Kind: "function", Properties: map[string]string{"file_path": "wire.go"}}},
		},
		RootNode: &stage3.DirectoryNode{
			FolderName:   ".",
			RelativePath: ".",
			Files: map[string]*stage3.FileBoundaryNode{
				"wire.go": {FileName: "wire.go", RelativePath: "wire.go", GASTRoot: root},
			},
		},
		WorkspaceCtx: stage3.NewWorkspaceContext(),
	}
	cpg := NewStage4Output("t")
	cpg.ModifiedFiles = map[string]bool{"wire.go": true}
	cpg.GraphNodes["wire.go::wireApp"] = &ResolvedNode{ID: "wire.go::wireApp", Kind: "FUNCTION", Name: "wireApp"}
	cpg.GraphNodes["wire.go::NewUserService"] = &ResolvedNode{ID: "wire.go::NewUserService", Kind: "FUNCTION", Name: "NewUserService"}

	LinkDependencyInjection(stage3Out, cpg)

	if !hasEdge(cpg, "wire.go::wireApp", "wire.go::NewUserService", EdgeInjects) {
		t.Error("expected DI_INJECTS edge to NewUserService")
	}
}

func TestLinkCrossLanguageRPC(t *testing.T) {
	stage3Out := &stage3.Stage3Output{
		CommitHash: "t",
		LocalTables: map[string]*stage2.FileSymbolTable{
			"server.go": {
				Endpoints: []stage2.EndpointMeta{{Method: "GET", Route: "/api/users", LineNumber: 3}},
			},
		},
		GlobalCallQueue: []stage3.LinkedCallSite{
			{
				SourceFilePath: "client.js",
				SourceFileNodeID: "client.js::load",
				MethodName:     "fetch",
				LineNumber:     7,
			},
		},
	}
	cpg := NewStage4Output("t")
	cpg.ModifiedFiles = map[string]bool{"client.js": true}
	cpg.GraphNodes["client.js::load"] = &ResolvedNode{
		ID:   "client.js::load",
		Kind: "FUNCTION",
		Name: "load",
		Properties: map[string]string{
			"content": `fetch('/api/users')`,
		},
	}

	LinkCrossLanguageRPC(stage3Out, cpg)

	if !cpg.NodeExists("endpoint:GET:/api/users") {
		t.Error("endpoint node not created by RPC linker")
	}
	if !hasEdge(cpg, "client.js::load", "endpoint:GET:/api/users", EdgeNetworkCall) {
		t.Error("expected NETWORK_RPC_CALL edge")
	}
}

// ---------------------------------------------------------------------------
// Constraints, alias, escape (full-mode linkers)
// ---------------------------------------------------------------------------

func TestLinkConstraintsFullMode(t *testing.T) {
	root := &stage2.GASTNode{
		Type: stage2.GASTFunction,
		Name: "check",
		Kind: "function",
		Children: []*stage2.GASTNode{
			{
				Type:       stage2.GASTControlFlow,
				Kind:       "if_statement",
				StartLine:  6,
				Properties: map[string]string{"condition": "x != 0"},
			},
		},
	}
	stage3Out := &stage3.Stage3Output{
		CommitHash: "t",
		RootNode: &stage3.DirectoryNode{
			FolderName:   ".",
			RelativePath: ".",
			Files: map[string]*stage3.FileBoundaryNode{
				"c.go": {FileName: "c.go", RelativePath: "c.go", GASTRoot: root},
			},
		},
	}
	cpg := NewStage4Output("t")
	cpg.ModifiedFiles = map[string]bool{"c.go": true}
	cpg.GraphNodes["c.go::check"] = &ResolvedNode{ID: "c.go::check", Kind: "FUNCTION", Name: "check"}

	LinkConstraints(stage3Out, cpg)

	condID := "c.go::check::CONSTRAINT_x != 0"
	n, ok := cpg.GraphNodes[condID]
	if !ok {
		t.Fatalf("constraint node not created; nodes=%v", cpg.GraphNodes)
	}
	if n.Kind != "ABSTRACT_CONSTRAINT" || n.Properties["logic"] != "NOT_EQUAL" {
		t.Errorf("constraint node = %+v", n)
	}
	if !hasEdge(cpg, "c.go::check", condID, EdgeConstraint) {
		t.Error("expected BRANCH_CONSTRAINT edge")
	}
}

func TestLinkAliasAnalysis(t *testing.T) {
	root := &stage2.GASTNode{
		Type: stage2.GASTFunction,
		Name: "alloc",
		Kind: "function",
		Children: []*stage2.GASTNode{
			{
				Type:       stage2.GASTVariable,
				Name:       "p",
				DataType:   "Widget",
				Kind:       "variable",
				StartLine:  3,
				Properties: map[string]string{"content": "p := new(Widget)"},
			},
		},
	}
	stage3Out := &stage3.Stage3Output{
		CommitHash: "t",
		GlobalDefinitionIndex: map[string][]*stage2.GASTNode{
			"w.go::Widget": {{Name: "Widget", Kind: "class", Properties: map[string]string{"file_path": "w.go"}}},
		},
		RootNode: &stage3.DirectoryNode{
			FolderName:   ".",
			RelativePath: ".",
			Files: map[string]*stage3.FileBoundaryNode{
				"w.go": {FileName: "w.go", RelativePath: "w.go", GASTRoot: root},
			},
		},
	}
	cpg := NewStage4Output("t")
	cpg.ModifiedFiles = map[string]bool{"w.go": true}
	cpg.GraphNodes["w.go::alloc"] = &ResolvedNode{ID: "w.go::alloc", Kind: "FUNCTION", Name: "alloc"}
	cpg.GraphNodes["w.go::Widget"] = &ResolvedNode{ID: "w.go::Widget", Kind: "CLASS", Name: "Widget"}

	LinkAliasAnalysis(stage3Out, cpg)

	allocID := "alloc::w.go::alloc::p"
	if !cpg.NodeExists(allocID) {
		t.Fatalf("alloc node missing; nodes=%v", cpg.GraphNodes)
	}
	if !hasEdge(cpg, "w.go::alloc::VAR_p", allocID, EdgePointsTo) {
		t.Error("expected POINTS_TO edge")
	}
	if !hasEdge(cpg, allocID, "w.go::Widget", EdgeHeapAlias) {
		t.Error("expected HEAP_ALIAS edge")
	}
}

// ---------------------------------------------------------------------------
// Security taint
// ---------------------------------------------------------------------------

func TestSecurityTaintDetection(t *testing.T) {
	cpg := NewStage4Output("t")
	cpg.GraphNodes["srv.go::handle"] = &ResolvedNode{
		ID:   "srv.go::handle",
		Kind: "FUNCTION",
		Name: "handle",
		PrimitiveScores: map[string]float64{
			"NETWORK_IO": 1.0,
		},
	}
	cpg.GraphNodes["db.go::write"] = &ResolvedNode{
		ID:   "db.go::write",
		Kind: "FUNCTION",
		Name: "write",
		PrimitiveScores: map[string]float64{
			"DATABASE_SQL": 1.0,
		},
	}
	cpg.AddEdge("srv.go::handle", "db.go::write", EdgeCalls, 5)

	LinkSecurityVulnerabilities(cpg)

	if !hasEdge(cpg, "srv.go::handle", "db.go::write", EdgeVulnerable) {
		t.Error("expected VULNERABLE_TAINT edge from source to sink")
	}
}

func TestSecurityTaintSkipsSanitizer(t *testing.T) {
	cpg := NewStage4Output("t")
	cpg.GraphNodes["srv.go::handle"] = &ResolvedNode{ID: "srv.go::handle", Kind: "FUNCTION", Name: "handle",
		PrimitiveScores: map[string]float64{"NETWORK_IO": 1.0}}
	cpg.GraphNodes["util.go::sanitize"] = &ResolvedNode{ID: "util.go::sanitize", Kind: "FUNCTION", Name: "sanitize",
		PrimitiveScores: map[string]float64{"SANITIZER": 1.0}}
	cpg.GraphNodes["db.go::write"] = &ResolvedNode{ID: "db.go::write", Kind: "FUNCTION", Name: "write",
		PrimitiveScores: map[string]float64{"DATABASE_SQL": 1.0}}
	cpg.AddEdge("srv.go::handle", "util.go::sanitize", EdgeCalls, 5)
	cpg.AddEdge("util.go::sanitize", "db.go::write", EdgeCalls, 6)

	LinkSecurityVulnerabilities(cpg)

	if hasEdge(cpg, "srv.go::handle", "db.go::write", EdgeVulnerable) {
		t.Error("taint must not flow through a SANITIZER node")
	}
}

// ---------------------------------------------------------------------------
// ReasonWholeProgramPrimitives
// ---------------------------------------------------------------------------

func TestReasonWholeProgramPrimitives(t *testing.T) {
	cpg := NewStage4Output("t")
	cpg.GraphNodes["a.go::caller"] = &ResolvedNode{ID: "a.go::caller", Kind: "FUNCTION", Name: "caller", FileSpec: LocationMeta{Path: "a.go"}}
	cpg.GraphNodes["b.go::callee"] = &ResolvedNode{ID: "b.go::callee", Kind: "FUNCTION", Name: "callee",
		FileSpec: LocationMeta{Path: "b.go"}, Primitive: "NETWORK_IO",
		PrimitiveScores: map[string]float64{"NETWORK_IO": 1.0}}
	cpg.AddEdge("a.go::caller", "b.go::callee", EdgeCalls, 2)

	ReasonWholeProgramPrimitives(cpg)

	caller := cpg.GraphNodes["a.go::caller"]
	if caller.PrimitiveScores["NETWORK_IO"] != 0.8 {
		t.Errorf("caller NETWORK_IO score = %v, want 0.8 (1.0 * 0.8 decay)", caller.PrimitiveScores["NETWORK_IO"])
	}
	if caller.Primitive != "NETWORK_IO" {
		t.Errorf("caller.Primitive = %q, want NETWORK_IO", caller.Primitive)
	}
}

// ---------------------------------------------------------------------------
// MeasureQuality / IsVirtualID
// ---------------------------------------------------------------------------

func TestMeasureQuality(t *testing.T) {
	cpg := NewStage4Output("t")
	cpg.GraphNodes["real.go::fn"] = &ResolvedNode{ID: "real.go::fn", Kind: "FUNCTION", Name: "fn"}
	cpg.GraphNodes["sink:SQL"] = &ResolvedNode{ID: "sink:SQL", Kind: "VIRTUAL_SECURITY_SINK", Name: "SQL"}
	cpg.AddEdge("real.go::fn", "sink:SQL", EdgeSecuritySink, 1) // resolved
	cpg.AddEdge("real.go::fn", "missing.go::ghost", EdgeCalls, 2) // dangling

	q := MeasureQuality(cpg)
	if q.TotalNodes != 2 {
		t.Errorf("TotalNodes = %d, want 2", q.TotalNodes)
	}
	if q.TotalEdges != 2 {
		t.Errorf("TotalEdges = %d, want 2", q.TotalEdges)
	}
	if q.DanglingEdges != 1 {
		t.Errorf("DanglingEdges = %d, want 1", q.DanglingEdges)
	}
	if q.VirtualNodes != 1 {
		t.Errorf("VirtualNodes = %d, want 1 (sink:SQL)", q.VirtualNodes)
	}
}

func TestIsVirtualID(t *testing.T) {
	virtual := []string{
		"VIRTUAL_QUEUE::x", "TAINT:DATABASE", "QUEUE::events", "topic::orders",
		"event:onClick", "endpoint:GET:/api", "sink:SQL", "resource:FileSystem",
		"global:cache", "memory::HEAP", "alloc::a::b", "ext:github.com/x", "DATABASE::db", "CLOUD_API::aws",
	}
	for _, id := range virtual {
		if !IsVirtualID(id) {
			t.Errorf("IsVirtualID(%q) = false, want true", id)
		}
	}
	for _, id := range []string{"a.go::fn", "file:src/a.go", "module:src", "b.go::User::Save"} {
		if IsVirtualID(id) {
			t.Errorf("IsVirtualID(%q) = true, want false", id)
		}
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func hasEdge(cpg *Stage4Output, src, tgt string, typ RelationshipType) bool {
	for _, e := range cpg.OutboundEdges[src] {
		if e.TargetID == tgt && e.Type == typ {
			return true
		}
	}
	return false
}
