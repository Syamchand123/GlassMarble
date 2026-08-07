package stage4

import (
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage2"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage3"
)

// dynamicNodeKinds are the node kinds produced only by dynamic-view passes
// (§5.4.3): CFG/DFG branches, summaries, constraints and control structures.
var dynamicNodeKinds = map[string]bool{
	"IF_BRANCH": true, "LOOP_BRANCH": true, "SWITCH_BRANCH": true,
	"EXCEPTIONAL_BRANCH": true, "CFG_FLOW": true, "CFG_SUMMARY": true,
	"DFG_VAR": true, "DFG_SUMMARY": true,
	"ControlStructure": true, "Constraint": true, "CONSTRAINT": true,
}

// TestLevelArchitecture_NoDynamicNodes is the §5.4.8 acceptance gate
// (W1-15/A-13): the architecture-level delta contains zero
// ControlStructure/CFGFlow/Constraint/DFG nodes and zero dynamic- or
// security-view edges.
func TestLevelArchitecture_NoDynamicNodes(t *testing.T) {
	stage3Out := buildTestStage3Out()

	out, err := Link(stage3Out, []string{"test.go"}, nil,
		LinkerConfig{LevelOfDetail: LevelArchitecture})
	if err != nil {
		t.Fatalf("Link(architecture) error: %v", err)
	}

	for id, n := range out.GraphNodes {
		if dynamicNodeKinds[n.Kind] {
			t.Errorf("architecture delta contains dynamic node %q (kind %q)", id, n.Kind)
		}
	}

	for src, edges := range out.OutboundEdges {
		for _, e := range edges {
			if view := ViewOfEdgeType(e.Type); view != "structural" {
				t.Errorf("architecture delta contains %s-view edge %s → %s (%s)",
					view, src, e.TargetID, e.Type)
			}
		}
	}
}

// TestLevelArchitectureKeepsStructuralSpine guards the A-13 intent: the
// architecture level must still emit the structural spine (hierarchy,
// membership, calls) — level filtering only removes dynamic/security.
func TestLevelArchitectureKeepsStructuralSpine(t *testing.T) {
	stage3Out := buildRichTestStage3Out()

	out, err := Link(stage3Out, []string{"service.go", "other.go"}, nil,
		LinkerConfig{LevelOfDetail: LevelArchitecture})
	if err != nil {
		t.Fatalf("Link(architecture) error: %v", err)
	}

	seen := map[RelationshipType]bool{}
	for _, edges := range out.OutboundEdges {
		for _, e := range edges {
			seen[e.Type] = true
		}
	}
	for _, want := range []RelationshipType{
		EdgeHasField, EdgeHasReceiver, EdgeHasParam, EdgeReturns,
		EdgeExtends, EdgeImplements, EdgeCalls, EdgeContains, EdgeDependsOn,
	} {
		if !seen[want] {
			t.Errorf("architecture delta missing structural edge %s", want)
		}
	}
}

// buildRichTestStage3Out creates a stage3 output with a struct, fields,
// methods, embedding, an interface implementation, a cross-file call and an
// import — everything the structural spine producers consume.
func buildRichTestStage3Out() *stage3.Stage3Output {
	mkField := func(name, kind string) *stage2.GASTNode {
		return &stage2.GASTNode{Type: stage2.GASTField, Name: name, Kind: kind, StartLine: 2}
	}
	base := &stage2.GASTNode{Type: stage2.GASTTypeDeclaration, Name: "Base", Kind: "class", StartLine: 1}
	dispatcher := &stage2.GASTNode{
		Type: stage2.GASTTypeDeclaration,
		Name: "Dispatcher",
		Kind: "struct",
		Properties: map[string]string{
			"file_path": "service.go",
		},
		BaseTypes:   []string{"Base"},
		Implemented: []string{"Handler"},
		Children: []*stage2.GASTNode{
			mkField("TargetDir", "field"),
			mkField("embedded", "embedding"),
			{Type: stage2.GASTFunction, Name: "Dispatcher.Dispatch", Kind: "method",
				ReceiverType: "Dispatcher", StartLine: 3,
				ReturnType: "Handler",
				Children: []*stage2.GASTNode{
					{Type: stage2.GASTParameter, Name: "job", Kind: "parameter", StartLine: 3},
				}},
		},
	}
	handler := &stage2.GASTNode{Type: stage2.GASTTypeDeclaration, Name: "Handler", Kind: "interface", StartLine: 1}

	main := &stage2.GASTNode{
		Type:      stage2.GASTFunction,
		Name:      "main",
		Kind:      "function",
		StartLine: 1,
		Children: []*stage2.GASTNode{
			{Type: stage2.GASTCallExpression, Name: "Dispatch", ReceiverType: "Dispatcher", StartLine: 4},
		},
	}

	return &stage3.Stage3Output{
		CommitHash: "rich",
		GlobalCallQueue: []stage3.LinkedCallSite{
			{
				SourceFileNodeID: "service.go::main",
				SourceFilePath:   "service.go",
				ReceiverName:     "Dispatcher",
				MethodName:       "Dispatch",
				LineNumber:       4,
			},
		},
		GlobalDefinitionIndex: map[string][]*stage2.GASTNode{
			"Dispatcher": {dispatcher},
			"Base":       {base},
			"Handler":    {handler},
			"main":       {main},
		},
		WorkspaceCtx: &stage3.WorkspaceContext{},
		RootNode: &stage3.DirectoryNode{
			RelativePath: ".",
			SubFolders:   map[string]*stage3.DirectoryNode{},
			Files: map[string]*stage3.FileBoundaryNode{
				"service.go": {
					FileName:     "service.go",
					RelativePath: "service.go",
					Language:     "go",
					LocalImports: []string{"github.com/acme/lib/queue"},
					GASTRoot: &stage2.GASTNode{
						Type: stage2.GASTFileRoot, Name: "service",
						Children: []*stage2.GASTNode{dispatcher, handler, main},
					},
				},
				"other.go": {
					FileName:     "other.go",
					RelativePath: "other.go",
					Language:     "go",
					LocalImports: []string{"service"},
					GASTRoot: &stage2.GASTNode{
						Type: stage2.GASTFileRoot, Name: "other",
						Children: []*stage2.GASTNode{
							{Type: stage2.GASTFunction, Name: "helper", Kind: "function", StartLine: 1},
						},
					},
				},
			},
		},
	}
}

// TestLevelStandardKeepsSummariesAndGatesHeuristics verifies §5.4.3: standard
// keeps aggregate CFG/DFG summaries (cfg/dfg passes run in summary mode) but
// gates the named full-only heuristics (constraints, alias, escape) and
// security.
func TestLevelStandardKeepsSummariesAndGatesHeuristics(t *testing.T) {
	stage3Out := buildTestStage3Out()

	out, err := Link(stage3Out, []string{"test.go"}, nil,
		LinkerConfig{LevelOfDetail: LevelStandard})
	if err != nil {
		t.Fatalf("Link(standard) error: %v", err)
	}

	hasSummary := false
	for _, n := range out.GraphNodes {
		if n.Kind == "CFG_SUMMARY" || n.Kind == "DFG_SUMMARY" {
			hasSummary = true
		}
	}
	if !hasSummary {
		t.Error("standard delta missing CFG/DFG summary nodes")
	}

	for src, edges := range out.OutboundEdges {
		for _, e := range edges {
			if view := ViewOfEdgeType(e.Type); view == "security" {
				t.Errorf("standard delta contains security-view edge %s → %s (%s)", src, e.TargetID, e.Type)
			}
			switch e.Type {
			case EdgeConstraint, EdgeAliases, EdgeAliasesType, EdgeEscapesToHeap:
				t.Errorf("standard delta contains full-only edge %s → %s (%s)", src, e.TargetID, e.Type)
			}
		}
	}
}
