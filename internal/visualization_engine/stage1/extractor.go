package stage1

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/types"
)

// ParseIssue describes a single malformed statement found while parsing a TTL
// file. ParseTTLFile aggregates all issues and returns them as the error so a
// partially-readable graph is never silently served (AUDIT Issue 2 Phase 2B-8).
type ParseIssue struct {
	Line int
	Msg  string
}

func (p ParseIssue) Error() string {
	if p.Line > 0 {
		return fmt.Sprintf("line %d: %s", p.Line, p.Msg)
	}
	return p.Msg
}

// ExtractSubgraph parses a TTL file and extracts a subgraph matching the given diagram type and options.
func ExtractSubgraph(ttlPath string, t types.DiagramType, opts types.QueryOptions) (*types.VirtualSubgraph, error) {
	nodes, edges, err := ParseTTLFile(ttlPath)
	if err != nil {
		return nil, fmt.Errorf("failed to parse Turtle file: %w", err)
	}

	cfg := GetExtractionConfig(t, opts)
	return extractWithConfig(nodes, edges, cfg, opts)
}

func predicatesForGroup(group types.PredicateGroup) []string {
	switch group {
	case types.GroupCallGraph:
		return []string{"gm:calls", "gm:spawnsConcurrent", "gm:dispatchesEvent", "gm:contextualCall", "gm:ffiCall"}
	case types.GroupTypeHierarchy:
		return []string{"gm:inheritsFrom", "gm:extends", "gm:implements", "gm:mixes"}
	case types.GroupComposition:
		return []string{"gm:composes", "gm:hasMember", "gm:hasField", "gm:aggregates", "gm:contains"}
	case types.GroupDataFlow:
		return []string{"gm:dataFlowTo", "gm:pointsTo", "gm:heapAlias", "gm:aliasesPointer", "gm:vulnerableTaint"}
	case types.GroupControlFlow:
		return []string{"gm:controlFlowTo", "gm:controlFlowToTrue", "gm:controlFlowToFalse", "gm:catchesException", "gm:defersExecution"}
	case types.GroupStructural:
		return []string{"gm:belongsToFile", "gm:belongsToNamespace", "gm:belongsTo", "gm:dependsOn", "gm:references", "gm:imports"}
	case types.GroupMessaging:
		return []string{"gm:sendsMessage", "gm:receivesMessage", "gm:publishesEvent", "gm:subscribesEvent", "gm:dispatchesEvent"}
	case types.GroupInfrastructure:
		return []string{"gm:networkCall", "gm:queriesDatabase", "gm:callsCloudAPI", "gm:consumesResource", "gm:mutatesGlobal", "gm:securitySink", "gm:exposesEndpoint"}
	case types.GroupSecurity:
		return []string{"gm:vulnerableTaint", "gm:securitySink", "gm:consumesResource"}
	case types.GroupBinding:
		return []string{"gm:instantiatesGeneric", "gm:diInjects", "gm:escapesToHeap", "gm:branchConstraint", "gm:aliasesType"}
	case types.GroupAny:
		return nil
	default:
		return nil
	}
}

func extractWithConfig(nodes map[string]*types.TTLNode, edges []types.TTLEdge, cfg types.ExtractionConfig, opts types.QueryOptions) (*types.VirtualSubgraph, error) {
	var startIDs []string
	switch cfg.EntryStrategy {
	case types.EntryStrategyEntryPoint:
		if opts.EntryPointID != "" {
			startIDs = []string{opts.EntryPointID}
		} else if cfg.IncludeUnused {
			startIDs = filterNodes(nodes, opts, func(n *types.TTLNode) bool {
				return matchesKind(n.Kind, cfg.NodeKindFilter)
			})
		} else {
			startIDs = getEntryPoints(nodes, opts, func(n *types.TTLNode) bool {
				return matchesKind(n.Kind, cfg.NodeKindFilter)
			})
		}
	case types.EntryStrategyAuto:
		startIDs = getEntryPoints(nodes, opts, func(n *types.TTLNode) bool {
			return matchesKind(n.Kind, cfg.NodeKindFilter)
		})
	case types.EntryStrategyChangedFiles:
		for id, n := range nodes {
			if n.FileURI != "" {
				for _, cf := range opts.ChangedFiles {
					if strings.Contains(n.FileURI, cf) {
						startIDs = append(startIDs, id)
						break
					}
				}
			}
		}
		if len(startIDs) == 0 && len(opts.ChangedFiles) > 0 {
			return nil, fmt.Errorf("no graph nodes match changed files: %v", opts.ChangedFiles)
		}
	default:
		startIDs = filterNodes(nodes, opts, func(n *types.TTLNode) bool {
			return matchesKind(n.Kind, cfg.NodeKindFilter)
		})
	}

	if opts.EntryPointID != "" && cfg.EntryStrategy == types.EntryStrategyAll {
		startIDs = append(startIDs, opts.EntryPointID)
	}

	// Validate user-supplied entry points: a missing entry must be a hard
	// error, never a silent empty diagram (AUDIT Issue 2 Phase 2A-3).
	if opts.EntryPointID != "" && !resolveEntryID(opts.EntryPointID, nodes) {
		return nil, fmt.Errorf("entry point not found: %q", opts.EntryPointID)
	}

	// Depth semantics: 0 means unlimited (matches the AI tool contract).
	maxDepth := cfg.MaxDepth
	if opts.MaxDepth > 0 {
		maxDepth = opts.MaxDepth
	}
	if maxDepth <= 0 {
		maxDepth = int(^uint(0) >> 1)
	}

	var allowedPreds map[string]bool
	for _, g := range cfg.PredicateGroup {
		preds := predicatesForGroup(g)
		if preds == nil {
			allowedPreds = nil
			break
		}
		if allowedPreds == nil {
			allowedPreds = make(map[string]bool)
		}
		for _, p := range preds {
			allowedPreds[p] = true
		}
	}

	edgeFilter := func(e types.TTLEdge) bool {
		return allowedPreds == nil || allowedPreds[e.Predicate]
	}
	idx := buildAdjacencyIndex(nodes, edges)

	switch cfg.Direction {
	case types.EdgeDirectionForward:
		return bfsSubgraph(startIDs, nodes, edges, idx, maxDepth, edgeFilter), nil
	case types.EdgeDirectionReverse:
		return reverseBFS(startIDs, nodes, edges, idx, maxDepth, edgeFilter), nil
	default:
		return bothPassSubgraph(startIDs, nodes, edges, idx, maxDepth, func(n *types.TTLNode) bool {
			return matchesKind(n.Kind, cfg.NodeKindFilter)
		}, edgeFilter), nil
	}
}

// resolveEntryID reports whether the given user-supplied entry ID refers to an
// existing node, accepting the bare ID, the formatted IRI, and any
// percent-encoded variant of either.
func resolveEntryID(entry string, nodes map[string]*types.TTLNode) bool {
	if _, ok := nodes[entry]; ok {
		return true
	}
	parsed := types.ParseNodeURI(entry)
	if _, ok := nodes[parsed]; ok {
		return true
	}
	if strings.HasPrefix(parsed, "file:") || strings.HasPrefix(parsed, "module:") {
		if _, ok := nodes[parsed]; ok {
			return true
		}
	}
	return false
}

// adjacencyIndex maps node IDs to the edge slices leaving/entering them so the
// BFS traversals are O(depth x (V+E)) instead of O(depth x V x E)
// (AUDIT Issue 2 Phase 2C-10).
type adjacencyIndex struct {
	out map[string][]types.TTLEdge
	in  map[string][]types.TTLEdge
}

func buildAdjacencyIndex(nodes map[string]*types.TTLNode, edges []types.TTLEdge) *adjacencyIndex {
	idx := &adjacencyIndex{
		out: make(map[string][]types.TTLEdge, len(nodes)),
		in:  make(map[string][]types.TTLEdge, len(nodes)),
	}
	for _, e := range edges {
		idx.out[e.SourceID] = append(idx.out[e.SourceID], e)
		idx.in[e.TargetID] = append(idx.in[e.TargetID], e)
	}
	return idx
}

func matchesKind(kind string, filters []string) bool {
	if len(filters) == 0 {
		return true
	}
	for _, f := range filters {
		if kind == f {
			return true
		}
	}
	return false
}

func reverseBFS(startIDs []string, allNodes map[string]*types.TTLNode, allEdges []types.TTLEdge, idx *adjacencyIndex, maxDepth int, edgeFilter func(types.TTLEdge) bool) *types.VirtualSubgraph {
	sub := &types.VirtualSubgraph{Nodes: make(map[string]*types.TTLNode)}
	queue := startIDs
	visited := make(map[string]bool)
	edgeSeen := make(map[string]bool)
	for depth := 0; len(queue) > 0 && depth <= maxDepth; depth++ {
		var next []string
		for _, id := range queue {
			if visited[id] {
				continue
			}
			visited[id] = true
			if n, ok := allNodes[id]; ok {
				sub.Nodes[id] = n
			}
			for _, e := range idx.in[id] {
				if e.TargetID == id && edgeFilter(e) {
					edgeKey := fmt.Sprintf("%s|%s|%s", e.SourceID, e.Predicate, e.TargetID)
					if !edgeSeen[edgeKey] {
						edgeSeen[edgeKey] = true
						sub.Edges = append(sub.Edges, e)
					}
					next = append(next, e.SourceID)
				}
			}
		}
		queue = next
	}
	return sub
}

func bothPassSubgraph(startIDs []string, allNodes map[string]*types.TTLNode, allEdges []types.TTLEdge, idx *adjacencyIndex, maxDepth int, nodeFilter func(*types.TTLNode) bool, edgeFilter func(types.TTLEdge) bool) *types.VirtualSubgraph {
	sub := &types.VirtualSubgraph{Nodes: make(map[string]*types.TTLNode)}

	if len(startIDs) == 0 {
		for id, n := range allNodes {
			if nodeFilter(n) {
				sub.Nodes[id] = n
			}
		}
		for _, e := range allEdges {
			if edgeFilter(e) {
				_, srcOK := sub.Nodes[e.SourceID]
				_, tgtOK := sub.Nodes[e.TargetID]
				if srcOK && tgtOK {
					sub.Edges = append(sub.Edges, e)
				}
			}
		}
		return sub
	}

	queue := startIDs
	visited := make(map[string]bool)
	edgeSeen := make(map[string]bool)

	for depth := 0; len(queue) > 0 && depth <= maxDepth; depth++ {
		var next []string
		for _, id := range queue {
			if visited[id] {
				continue
			}
			visited[id] = true
			if n, ok := allNodes[id]; ok {
				sub.Nodes[id] = n
			}
			for _, e := range idx.out[id] {
				if e.SourceID == id && edgeFilter(e) {
					edgeKey := fmt.Sprintf("%s|%s|%s", e.SourceID, e.Predicate, e.TargetID)
					if !edgeSeen[edgeKey] {
						edgeSeen[edgeKey] = true
						sub.Edges = append(sub.Edges, e)
					}
					next = append(next, e.TargetID)
				}
			}
			for _, e := range idx.in[id] {
				if e.TargetID == id && edgeFilter(e) {
					edgeKey := fmt.Sprintf("%s|%s|%s", e.SourceID, e.Predicate, e.TargetID)
					if !edgeSeen[edgeKey] {
						edgeSeen[edgeKey] = true
						sub.Edges = append(sub.Edges, e)
					}
					next = append(next, e.SourceID)
				}
			}
		}
		queue = next
	}
	return sub
}

// extractionConfigs defines node-kind filters and predicate groups per diagram
// type. Kinds listed here are exactly the classes the serializer can emit
// (gm:Struct/Class/Interface/Function/Method/Member/Module/File/Namespace/
// Parameter/Variable/Package plus the legacy fallbacks gm:TypeDecl,
// gm:Executable, gm:ControlStructure, gm:CFGSummary, gm:DFGSummary,
// gm:EventTopic, gm:VirtualDatabase, gm:VirtualEndpoint, gm:Block,
// gm:Annotation and the synthetic gm:ExternalSDK/API/FFI, gm:Virtual* classes)
// — see internal/akg/turtle_serializer.go mapKindToClass.
var extractionConfigs = map[types.DiagramType]types.ExtractionConfig{
	types.UMLClass:               {Name: "UMLClass", NodeKindFilter: []string{"gm:TypeDecl", "gm:Struct", "gm:Class", "gm:Interface", "gm:Member", "gm:Function", "gm:Method", "gm:Executable", "gm:External"}, PredicateGroup: []types.PredicateGroup{types.GroupTypeHierarchy, types.GroupComposition, types.GroupBinding}, EntryStrategy: types.EntryStrategyEntryPoint, MaxDepth: 7, Direction: types.EdgeDirectionForward, IncludeUnused: true},
	types.UMLObject:              {Name: "UMLObject", NodeKindFilter: []string{"gm:TypeDecl", "gm:Struct", "gm:Class", "gm:Interface", "gm:External"}, PredicateGroup: []types.PredicateGroup{types.GroupTypeHierarchy}, EntryStrategy: types.EntryStrategyEntryPoint, MaxDepth: 3, Direction: types.EdgeDirectionForward},
	types.UMLComponent:           {Name: "UMLComponent", NodeKindFilter: []string{"gm:TypeDecl", "gm:Struct", "gm:Class", "gm:Interface", "gm:Executable", "gm:Function", "gm:Method", "gm:Namespace", "gm:Module", "gm:External"}, PredicateGroup: []types.PredicateGroup{types.GroupCallGraph, types.GroupComposition, types.GroupStructural}, EntryStrategy: types.EntryStrategyEntryPoint, MaxDepth: 7, Direction: types.EdgeDirectionForward},
	types.UMLDeployment:          {Name: "UMLDeployment", NodeKindFilter: []string{"gm:Namespace", "gm:Module", "gm:File", "gm:VirtualDatabase", "gm:Executable", "gm:Function", "gm:Method", "gm:External"}, PredicateGroup: []types.PredicateGroup{types.GroupInfrastructure, types.GroupCallGraph, types.GroupStructural}, EntryStrategy: types.EntryStrategyAll, MaxDepth: 7, Direction: types.EdgeDirectionForward},
	types.UMLPackage:             {Name: "UMLPackage", NodeKindFilter: []string{"gm:Namespace", "gm:File", "gm:Module", "gm:External"}, PredicateGroup: []types.PredicateGroup{types.GroupStructural}, EntryStrategy: types.EntryStrategyAll, MaxDepth: 99, Direction: types.EdgeDirectionBoth},
	types.UMLComposite:           {Name: "UMLComposite", NodeKindFilter: []string{"gm:TypeDecl", "gm:Struct", "gm:Class", "gm:Interface", "gm:External"}, PredicateGroup: []types.PredicateGroup{types.GroupComposition, types.GroupTypeHierarchy}, EntryStrategy: types.EntryStrategyEntryPoint, MaxDepth: 5, Direction: types.EdgeDirectionForward},
	types.UMLProfile:             {Name: "UMLProfile", NodeKindFilter: []string{"gm:TypeDecl", "gm:Struct", "gm:Class", "gm:Interface", "gm:Annotation"}, PredicateGroup: []types.PredicateGroup{types.GroupTypeHierarchy, types.GroupComposition}, EntryStrategy: types.EntryStrategyAll, MaxDepth: 5, Direction: types.EdgeDirectionForward},
	types.UMLUsecase:             {Name: "UMLUsecase", NodeKindFilter: []string{"gm:Annotation", "gm:Executable", "gm:Function", "gm:Method", "gm:External"}, PredicateGroup: []types.PredicateGroup{types.GroupCallGraph, types.GroupStructural}, EntryStrategy: types.EntryStrategyAuto, MaxDepth: 5, Direction: types.EdgeDirectionForward},
	types.UMLActivity:            {Name: "UMLActivity", NodeKindFilter: []string{"gm:ControlStructure", "gm:Block", "gm:Executable", "gm:Function", "gm:Method", "gm:External"}, PredicateGroup: []types.PredicateGroup{types.GroupControlFlow}, EntryStrategy: types.EntryStrategyEntryPoint, MaxDepth: 10, Direction: types.EdgeDirectionForward},
	types.Flowchart:              {Name: "Flowchart", PredicateGroup: []types.PredicateGroup{types.GroupControlFlow, types.GroupDataFlow, types.GroupCallGraph}, EntryStrategy: types.EntryStrategyEntryPoint, MaxDepth: 10, Direction: types.EdgeDirectionForward},
	types.UMLState:               {Name: "UMLState", NodeKindFilter: []string{"gm:Variable", "gm:Parameter", "gm:ControlStructure", "gm:TypeDecl", "gm:Struct", "gm:Class", "gm:Executable", "gm:Function", "gm:Method"}, PredicateGroup: []types.PredicateGroup{types.GroupDataFlow, types.GroupControlFlow}, EntryStrategy: types.EntryStrategyAll, MaxDepth: 5, Direction: types.EdgeDirectionForward},
	types.UMLSequence:            {Name: "UMLSequence", NodeKindFilter: []string{"gm:Executable", "gm:Function", "gm:Method", "gm:External"}, PredicateGroup: []types.PredicateGroup{types.GroupCallGraph}, EntryStrategy: types.EntryStrategyEntryPoint, Direction: types.EdgeDirectionForward},
	types.UMLCommunication:       {Name: "UMLCommunication", NodeKindFilter: []string{"gm:Executable", "gm:Function", "gm:Method", "gm:External"}, PredicateGroup: []types.PredicateGroup{types.GroupCallGraph}, EntryStrategy: types.EntryStrategyEntryPoint, Direction: types.EdgeDirectionForward},
	types.UMLInteractionOverview: {Name: "UMLInteractionOverview", NodeKindFilter: []string{"gm:Executable", "gm:Function", "gm:Method", "gm:External"}, PredicateGroup: []types.PredicateGroup{types.GroupCallGraph}, EntryStrategy: types.EntryStrategyEntryPoint, Direction: types.EdgeDirectionForward},
	types.UMLTiming:              {Name: "UMLTiming", NodeKindFilter: []string{"gm:Executable", "gm:Function", "gm:Method", "gm:Variable", "gm:External"}, PredicateGroup: []types.PredicateGroup{types.GroupCallGraph}, EntryStrategy: types.EntryStrategyAll, MaxDepth: 5, Direction: types.EdgeDirectionForward},
	types.C4Context:              {Name: "C4Context", NodeKindFilter: []string{"gm:ExternalSDK", "gm:ExternalAPI", "gm:ExternalFFI", "gm:External", "gm:Namespace", "gm:Module"}, PredicateGroup: []types.PredicateGroup{types.GroupCallGraph, types.GroupStructural, types.GroupInfrastructure}, EntryStrategy: types.EntryStrategyAuto, MaxDepth: 3, Direction: types.EdgeDirectionForward},
	types.C4Container:            {Name: "C4Container", NodeKindFilter: []string{"gm:Module", "gm:Namespace", "gm:VirtualDatabase", "gm:Executable", "gm:Function", "gm:Method", "gm:External"}, PredicateGroup: []types.PredicateGroup{types.GroupCallGraph, types.GroupStructural, types.GroupInfrastructure, types.GroupMessaging}, EntryStrategy: types.EntryStrategyAuto, MaxDepth: 3, Direction: types.EdgeDirectionForward},
	types.C4Component:            {Name: "C4Component", NodeKindFilter: []string{"gm:TypeDecl", "gm:Struct", "gm:Class", "gm:Interface", "gm:Executable", "gm:Function", "gm:Method", "gm:VirtualDatabase", "gm:ExternalSDK", "gm:ExternalAPI", "gm:ExternalFFI", "gm:External"}, PredicateGroup: []types.PredicateGroup{types.GroupCallGraph, types.GroupComposition, types.GroupStructural, types.GroupInfrastructure}, EntryStrategy: types.EntryStrategyAuto, MaxDepth: 5, Direction: types.EdgeDirectionForward},
	types.C4Code:                 {Name: "C4Code", NodeKindFilter: []string{"gm:TypeDecl", "gm:Struct", "gm:Class", "gm:Interface", "gm:Member", "gm:Function", "gm:Method", "gm:Executable", "gm:External"}, PredicateGroup: []types.PredicateGroup{types.GroupTypeHierarchy, types.GroupComposition}, EntryStrategy: types.EntryStrategyEntryPoint, MaxDepth: 3, Direction: types.EdgeDirectionForward},
	types.C4Landscape:            {Name: "C4Landscape", NodeKindFilter: []string{"gm:Namespace", "gm:Module", "gm:File", "gm:ExternalSDK", "gm:ExternalAPI", "gm:ExternalFFI", "gm:External"}, PredicateGroup: []types.PredicateGroup{types.GroupStructural, types.GroupInfrastructure}, EntryStrategy: types.EntryStrategyAll, MaxDepth: 99, Direction: types.EdgeDirectionBoth},
	types.C4Dynamic:              {Name: "C4Dynamic", NodeKindFilter: []string{"gm:Executable", "gm:Function", "gm:Method", "gm:External"}, PredicateGroup: []types.PredicateGroup{types.GroupCallGraph}, EntryStrategy: types.EntryStrategyEntryPoint, Direction: types.EdgeDirectionForward},
	types.C4Deployment:           {Name: "C4Deployment", NodeKindFilter: []string{"gm:Namespace", "gm:Module", "gm:File", "gm:Executable", "gm:Function", "gm:Method", "gm:VirtualDatabase", "gm:External"}, PredicateGroup: []types.PredicateGroup{types.GroupStructural, types.GroupInfrastructure, types.GroupCallGraph}, EntryStrategy: types.EntryStrategyAll, MaxDepth: 3, Direction: types.EdgeDirectionForward},
	types.ERDiagram:              {Name: "ERDiagram", NodeKindFilter: []string{"gm:TypeDecl", "gm:Struct", "gm:Class", "gm:Member", "gm:Executable", "gm:Function", "gm:Method"}, PredicateGroup: []types.PredicateGroup{types.GroupComposition, types.GroupBinding}, EntryStrategy: types.EntryStrategyAll, MaxDepth: 3, Direction: types.EdgeDirectionBoth},
	types.DataFlow:               {Name: "DataFlow", NodeKindFilter: []string{"gm:Variable", "gm:Parameter", "gm:Executable", "gm:Function", "gm:Method", "gm:External"}, PredicateGroup: []types.PredicateGroup{types.GroupDataFlow, types.GroupSecurity, types.GroupBinding}, EntryStrategy: types.EntryStrategyAuto, MaxDepth: 10, Direction: types.EdgeDirectionBoth},
	types.Mindmap:                {Name: "Mindmap", NodeKindFilter: []string{"gm:Namespace", "gm:Module", "gm:File"}, PredicateGroup: []types.PredicateGroup{types.GroupStructural}, EntryStrategy: types.EntryStrategyAll, MaxDepth: 99, Direction: types.EdgeDirectionBoth},
	types.DependencyGraph:        {Name: "DependencyGraph", NodeKindFilter: []string{"gm:TypeDecl", "gm:Struct", "gm:Class", "gm:Interface", "gm:File", "gm:Namespace", "gm:Module", "gm:External"}, PredicateGroup: []types.PredicateGroup{types.GroupStructural}, EntryStrategy: types.EntryStrategyAll, MaxDepth: 3, Direction: types.EdgeDirectionBoth},
	types.HotspotComplexity:      {Name: "HotspotComplexity", NodeKindFilter: []string{"gm:Executable", "gm:Function", "gm:Method", "gm:External"}, PredicateGroup: []types.PredicateGroup{types.GroupCallGraph}, EntryStrategy: types.EntryStrategyAll, MaxDepth: 3, Direction: types.EdgeDirectionForward},
	types.CallGraph:              {Name: "CallGraph", NodeKindFilter: []string{"gm:Executable", "gm:Function", "gm:Method", "gm:External"}, PredicateGroup: []types.PredicateGroup{types.GroupCallGraph, types.GroupMessaging, types.GroupInfrastructure}, EntryStrategy: types.EntryStrategyEntryPoint, MaxDepth: 99, Direction: types.EdgeDirectionForward},
	types.LayeredArchitecture:    {Name: "LayeredArchitecture", NodeKindFilter: []string{"gm:TypeDecl", "gm:Struct", "gm:Class", "gm:Interface", "gm:Executable", "gm:Function", "gm:Method", "gm:External"}, PredicateGroup: []types.PredicateGroup{types.GroupCallGraph, types.GroupComposition, types.GroupTypeHierarchy}, EntryStrategy: types.EntryStrategyAll, MaxDepth: 99, Direction: types.EdgeDirectionBoth},
	types.ChangeImpact:           {Name: "ChangeImpact", PredicateGroup: []types.PredicateGroup{types.GroupAny}, EntryStrategy: types.EntryStrategyChangedFiles, MaxDepth: 5, Direction: types.EdgeDirectionReverse},
	types.Infrastructure:         {Name: "Infrastructure", NodeKindFilter: []string{"gm:ExternalSDK", "gm:ExternalAPI", "gm:ExternalFFI", "gm:VirtualDatabase", "gm:Module", "gm:Namespace", "gm:External"}, PredicateGroup: []types.PredicateGroup{types.GroupInfrastructure, types.GroupStructural, types.GroupMessaging, types.GroupSecurity}, EntryStrategy: types.EntryStrategyAll, MaxDepth: 3, Direction: types.EdgeDirectionReverse},
}

func GetExtractionConfig(t types.DiagramType, opts types.QueryOptions) types.ExtractionConfig {
	cfg, ok := extractionConfigs[t]
	if !ok {
		return types.ExtractionConfig{Name: "Default", PredicateGroup: []types.PredicateGroup{types.GroupAny}, EntryStrategy: types.EntryStrategyAll, MaxDepth: 7, Direction: types.EdgeDirectionForward}
	}
	switch t {
	case types.UMLSequence, types.UMLCommunication, types.UMLInteractionOverview, types.C4Dynamic:
		cfg.MaxDepth = opts.MaxDepth
	}
	return cfg
}

// getEntryPoints finds the entry points based on QueryOptions.
func getEntryPoints(nodes map[string]*types.TTLNode, opts types.QueryOptions, defaultFilter func(*types.TTLNode) bool) []string {
	var startIDs []string
	if opts.EntryPointID != "" {
		startIDs = append(startIDs, opts.EntryPointID)
		return startIDs
	}

	// F.5 Auto-Discover Sequence Diagram Entry Points
	// First pass: try to find main, handler, controller, api
	for id, n := range nodes {
		if defaultFilter(n) {
			lower := strings.ToLower(id + " " + n.Name)
			if strings.Contains(lower, "main") || strings.Contains(lower, "handler") || strings.Contains(lower, "controller") || strings.Contains(lower, "api") {
				if opts.ScopePrefix == "" || strings.HasPrefix(id, opts.ScopePrefix) {
					startIDs = append(startIDs, id)
				}
			}
		}
	}

	// Second pass: fallback if no entry points found
	if len(startIDs) == 0 {
		for id, n := range nodes {
			if defaultFilter(n) {
				if opts.ScopePrefix == "" || strings.HasPrefix(id, opts.ScopePrefix) {
					startIDs = append(startIDs, id)
				}
			}
		}
	}

	return startIDs
}

// filterNodes applies ScopePrefix and MaxNodes filters.
func filterNodes(nodes map[string]*types.TTLNode, opts types.QueryOptions, filter func(*types.TTLNode) bool) []string {
	var ids []string
	for id, n := range nodes {
		if filter(n) {
			if opts.ScopePrefix == "" || strings.HasPrefix(id, opts.ScopePrefix) {
				ids = append(ids, id)
				if opts.MaxNodes > 0 && len(ids) >= opts.MaxNodes {
					break
				}
			}
		}
	}
	return ids
}

// ParseTTLFile parses a Turtle (.ttl) file at ttlPath and returns extracted TTLNodes,
// TTLEdges, and any parse error. Use ParseTTLFileToNative for a NativeGraph result.
// Malformed statements are reported as a structured error (with line numbers)
// instead of being silently dropped (AUDIT Issue 2 Phase 2B-8).
func ParseTTLFile(ttlPath string) (map[string]*types.TTLNode, []types.TTLEdge, error) {
	file, err := os.Open(ttlPath)
	if err != nil {
		return nil, nil, err
	}
	defer file.Close()

	nodes := make(map[string]*types.TTLNode)
	edgeMap := make(map[string]*types.TTLEdge)
	var issues []ParseIssue

	scanner := bufio.NewScanner(file)
	// Lines larger than 64KB would previously abort the scan silently and
	// truncate the graph. Raise the cap and treat overflow as a hard error.
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	var block []string
	blockStartLine := 1
	lineNo := 0

	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "@prefix") || strings.HasPrefix(line, "#") {
			continue
		}

		if len(block) == 0 {
			blockStartLine = lineNo
		}
		block = append(block, line)
		if strings.HasSuffix(line, ".") {
			blockStr := strings.Join(block, " ")
			block = nil

			if strings.HasPrefix(blockStr, "<<") {
				if err := bindEdgeProperty(blockStr, edgeMap); err != nil {
					issues = append(issues, ParseIssue{Line: blockStartLine, Msg: err.Error()})
				}
			} else if isBaseEdge(blockStr) {
				if err := parseBaseEdge(blockStr, edgeMap); err != nil {
					issues = append(issues, ParseIssue{Line: blockStartLine, Msg: err.Error()})
				}
			} else {
				parseNodeBlock(blockStr, nodes)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, nil, fmt.Errorf("scanning %s: %w", ttlPath, err)
	}

	// A trailing statement that never reached its terminator is a truncated
	// file (AUDIT Issue 5 finding 6): surface it instead of silently serving
	// a partial graph.
	if len(block) > 0 {
		issues = append(issues, ParseIssue{Line: blockStartLine, Msg: "unterminated statement block (truncated file?)"})
	}

	if len(issues) > 0 {
		return nil, nil, fmt.Errorf("%s: %d malformed statement(s): %w", ttlPath, len(issues), issues[0])
	}

	parsedEdges := make([]types.TTLEdge, 0, len(edgeMap))
	for _, e := range edgeMap {
		parsedEdges = append(parsedEdges, *e)
	}
	return nodes, parsedEdges, nil
}

func isBaseEdge(block string) bool {
	if strings.HasPrefix(strings.TrimSpace(block), "<<") {
		return false
	}
	trimmed := strings.TrimSuffix(strings.TrimSpace(block), ".")
	parts := strings.Fields(trimmed)
	if len(parts) == 3 {
		if parts[1] == "a" {
			return false
		}
		return true
	}
	return false
}

func parseBaseEdge(block string, edgeMap map[string]*types.TTLEdge) error {
	block = strings.TrimSuffix(strings.TrimSpace(block), ".")
	block = strings.TrimSpace(block)
	parts := strings.Fields(block)
	if len(parts) < 3 {
		return fmt.Errorf("malformed edge block dropped (expected 3+ fields): %.50s", block)
	}

	src := types.ParseNodeURI(parts[0])
	pred := parts[1]
	tgt := types.ParseNodeURI(parts[2])

	// Tombstones (AUDIT Issue 2 Phase 2A-4): the legacy serializer form
	// `<uri> gm:status "DELETED" .` is a deletion marker, not an edge.
	if pred == "gm:status" {
		if parseLiteral(tgt) == "DELETED" {
			return nil
		}
		return fmt.Errorf("unexpected gm:status value in edge block: %.50s", block)
	}

	key := fmt.Sprintf("%s|%s|%s", src, pred, tgt)
	edgeMap[key] = &types.TTLEdge{
		SourceID:  src,
		Predicate: pred,
		TargetID:  tgt,
	}
	return nil
}

func bindEdgeProperty(block string, edgeMap map[string]*types.TTLEdge) error {
	block = strings.TrimSuffix(strings.TrimSpace(block), ".")
	block = strings.TrimSpace(block)

	startIdx := strings.Index(block, "<<")
	endIdx := strings.LastIndex(block, ">>")
	if startIdx == -1 || endIdx == -1 || startIdx >= endIdx {
		return fmt.Errorf("malformed reified property block (no <<...>>): %.50s", block)
	}

	triplePart := strings.TrimSpace(block[startIdx+2 : endIdx])
	propsPart := strings.TrimSpace(block[endIdx+2:])

	tripleFields := strings.Fields(triplePart)
	if len(tripleFields) < 3 {
		return fmt.Errorf("malformed triple in reified prop (expected 3+ fields): %.50s", triplePart)
	}

	src := types.ParseNodeURI(tripleFields[0])
	pred := tripleFields[1]
	tgt := types.ParseNodeURI(tripleFields[2])

	key := fmt.Sprintf("%s|%s|%s", src, pred, tgt)
	edge, exists := edgeMap[key]
	if !exists {
		// Edge might be declared after the annotation, so create it if it doesn't exist
		edge = &types.TTLEdge{
			SourceID:  src,
			Predicate: pred,
			TargetID:  tgt,
		}
		edgeMap[key] = edge
	}

	if strings.HasPrefix(propsPart, "gm:lineNumber") {
		numStr := strings.TrimSpace(strings.TrimPrefix(propsPart, "gm:lineNumber"))
		if n, err := strconv.Atoi(numStr); err == nil {
			edge.LineNumber = n
		} else {
			return fmt.Errorf("bad gm:lineNumber value %q for edge %s: %v", numStr, key, err)
		}
	}
	return nil
}

func parseNodeBlock(block string, nodes map[string]*types.TTLNode) {
	parts := strings.SplitN(block, " ", 2)
	if len(parts) < 2 {
		return
	}

	nodeID := types.ParseNodeURI(parts[0])
	if nodeID == "metadata" {
		return
	}

	node := &types.TTLNode{ID: nodeID}
	rest := strings.TrimSpace(parts[1])

	rest = strings.TrimSuffix(rest, ".")
	rest = strings.TrimSpace(rest)

	attributes := splitTTLAttributes(rest)
	for _, attr := range attributes {
		attr = strings.TrimSpace(attr)
		if attr == "" {
			continue
		}

		kv := strings.SplitN(attr, " ", 2)
		if len(kv) < 2 {
			continue
		}

		pred := strings.TrimSpace(kv[0])
		val := strings.TrimSpace(kv[1])

		switch pred {
		case "a":
			if val != "rdfs:Resource" {
				node.Kind = val
			}
		case "gm:name":
			node.Name = parseLiteral(val)
		case "gm:primitiveType":
			node.PrimitiveType = parseLiteral(val)
		case "gm:belongsToFile":
			node.FileURI = types.ParseNodeURI(val)
		case "gm:lineStart":
			if n, err := strconv.Atoi(val); err == nil {
				node.LineStart = n
			}
		case "gm:lineEnd":
			if n, err := strconv.Atoi(val); err == nil {
				node.LineEnd = n
			}
		case "gm:code":
			node.Code = parseLiteral(val)
		case "gm:isEntrypoint":
			if val == "true" {
				node.IsEntrypoint = true
			}
		case "gm:primitiveZone":
			node.PrimitiveZone = parseLiteral(val)
		case "gm:status":
			if parseLiteral(val) == "DELETED" {
				return
			}
		default:
			if node.Properties == nil {
				node.Properties = make(map[string]string)
			}
			node.Properties[strings.TrimPrefix(pred, "gm:")] = parseLiteral(val)
		}
	}

	// A block typed gm:Deleted is a tombstone even without gm:status.
	if node.Kind == "gm:Deleted" {
		return
	}

	if node.ID != "" && node.Kind != "" {
		if strings.HasPrefix(node.ID, "ext:") && node.Kind == "rdfs:Class" {
			node.Kind = "gm:External"
		}
		nodes[node.ID] = node
	}
}

// splitTTLAttributes splits a TTL node block's attributes by semicolon,
// but correctly ignores semicolons inside double-quoted string literals.
func splitTTLAttributes(s string) []string {
	var result []string
	var current strings.Builder
	inQuotes := false

	for i := 0; i < len(s); i++ {
		ch := s[i]
		if ch == '"' {
			if i == 0 || s[i-1] != '\\' {
				inQuotes = !inQuotes
			}
			current.WriteByte(ch)
		} else if ch == ';' && !inQuotes {
			result = append(result, current.String())
			current.Reset()
		} else {
			current.WriteByte(ch)
		}
	}
	if current.Len() > 0 {
		result = append(result, current.String())
	}
	return result
}

// parseLiteral unescapes a TTL literal in a single pass (longest-match
// semantics): a backslash starts an escape and consumes exactly one following
// character. This is the exact inverse of the serializer's escapeLiteral and
// preserves sequences like \\n (literal backslash + n) instead of corrupting
// them into real newlines (AUDIT Issue 2 Phase 2B-7).
func parseLiteral(val string) string {
	val = strings.TrimSpace(val)
	if strings.HasPrefix(val, "\"\"\"") && strings.HasSuffix(val, "\"\"\"") {
		val = val[3 : len(val)-3]
	} else if strings.HasPrefix(val, "\"") && strings.HasSuffix(val, "\"") {
		val = val[1 : len(val)-1]
	}

	if !strings.Contains(val, "\\") {
		return val
	}

	var b strings.Builder
	b.Grow(len(val))
	for i := 0; i < len(val); i++ {
		if val[i] == '\\' && i+1 < len(val) {
			switch val[i+1] {
			case '\\':
				b.WriteByte('\\')
				i++
			case '"':
				b.WriteByte('"')
				i++
			case 'n':
				b.WriteByte('\n')
				i++
			case 'r':
				b.WriteByte('\r')
				i++
			case 't':
				b.WriteByte('\t')
				i++
			default:
				b.WriteByte(val[i])
			}
			continue
		}
		b.WriteByte(val[i])
	}
	return b.String()
}

// bfsSubgraph performs a depth-limited breadth-first search to extract a focused subgraph.
func bfsSubgraph(
	startIDs []string,
	allNodes map[string]*types.TTLNode,
	allEdges []types.TTLEdge,
	idx *adjacencyIndex,
	maxDepth int,
	edgeFilter func(types.TTLEdge) bool,
) *types.VirtualSubgraph {
	sub := &types.VirtualSubgraph{Nodes: make(map[string]*types.TTLNode)}
	queue := startIDs
	visited := make(map[string]bool)
	edgeSeen := make(map[string]bool)
	for depth := 0; len(queue) > 0 && depth <= maxDepth; depth++ {
		var next []string
		for _, id := range queue {
			if visited[id] {
				continue
			}
			visited[id] = true
			if n, ok := allNodes[id]; ok {
				sub.Nodes[id] = n
			}
			for _, e := range idx.out[id] {
				if e.SourceID == id && edgeFilter(e) {
					edgeKey := fmt.Sprintf("%s|%s|%s", e.SourceID, e.Predicate, e.TargetID)
					if !edgeSeen[edgeKey] {
						edgeSeen[edgeKey] = true
						sub.Edges = append(sub.Edges, e)
					}
					next = append(next, e.TargetID)
				}
			}
		}
		queue = next
	}
	return sub
}

// ParseTTLFileToNative parses a TTL file and returns a NativeGraph directly (types are aliases, no copy needed).
func ParseTTLFileToNative(ttlPath string) (*types.NativeGraph, error) {
	nodes, edges, err := ParseTTLFile(ttlPath)
	if err != nil {
		return nil, err
	}
	return &types.NativeGraph{Nodes: nodes, Edges: edges}, nil
}

// StreamTTLBlocks streams a TTL file into logical statement blocks in file
// order; it is the exported generic form of the streaming primitive behind
// the lazy query entry points (AUDIT Issue 4 Phase 4A-2).
func StreamTTLBlocks(ttlPath string, handle func(block string) error) error {
	return scanTTLStream(ttlPath, handle)
}

// scanTTLStream streams a TTL file into logical statement blocks (node
// blocks, base edge triples, reified edge-property triples) and invokes
// handle for every complete block in file order. A trailing unterminated
// block is a hard error (truncated file). It is the shared streaming
// primitive behind the lazy query entry points so the whole database is
// never materialized for a small read (AUDIT Issue 4 Phase 4A-2).
func scanTTLStream(ttlPath string, handle func(block string) error) error {
	file, err := os.Open(ttlPath)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	var block []string
	blockStartLine := 1
	lineNo := 0

	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "@prefix") || strings.HasPrefix(line, "#") {
			continue
		}

		if len(block) == 0 {
			blockStartLine = lineNo
		}
		block = append(block, line)
		if strings.HasSuffix(line, ".") {
			blockStr := strings.Join(block, " ")
			block = nil
			if err := handle(blockStr); err != nil {
				return err
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scanning %s: %w", ttlPath, err)
	}
	if len(block) > 0 {
		return fmt.Errorf("%s: unterminated statement block at line %d (truncated file?)", ttlPath, blockStartLine)
	}
	return nil
}

// ParseTTLFileToNativeScoped streams the TTL and materializes ONLY the
// nodes belonging to scopePath plus the edges whose endpoints are both in
// that set: file-scope diagrams must parse only the file's triples instead
// of loading the whole database (AUDIT Issue 4 Phase 4A-2). Matching rules
// mirror ApplyScope(ScopeFile) exactly, so the result equals a full parse
// followed by ApplyScope. In the serialized layout all node blocks precede
// all edge blocks, so the endpoint set is complete when edges are streamed.
func ParseTTLFileToNativeScoped(ttlPath, scopePath string) (*types.NativeGraph, error) {
	if scopePath == "" {
		return nil, fmt.Errorf("file scope requires a non-empty path")
	}
	graph := &types.NativeGraph{
		Nodes: make(map[string]*types.NativeNode),
		Edges: make([]types.NativeEdge, 0),
	}
	matched := make(map[string]bool)
	edgeMap := make(map[string]*types.NativeEdge)
	scopedPath := "/" + strings.TrimPrefix(scopePath, "/")

	err := scanTTLStream(ttlPath, func(block string) error {
		trimmed := strings.TrimSpace(block)
		if strings.HasPrefix(trimmed, "<<") {
			if err := bindEdgePropertyScoped(block, edgeMap, matched); err != nil {
				return err
			}
			return nil
		}
		if isBaseEdge(trimmed) {
			return parseBaseEdgeScoped(block, edgeMap, matched)
		}
		// Node block: parse into a throwaway node and keep it only when it
		// matches the scope (skipped blocks are dropped without any
		// persistent allocation).
		scratch := make(map[string]*types.TTLNode, 1)
		parseNodeBlock(block, scratch)
		for id, n := range scratch {
			if matchesFileScope(n, id, scopePath, scopedPath) {
				graph.Nodes[id] = n
				matched[id] = true
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	for _, e := range edgeMap {
		graph.Edges = append(graph.Edges, *e)
	}
	return graph, nil
}

// matchesFileScope reproduces ApplyScope(ScopeFile)'s node predicate.
func matchesFileScope(n *types.NativeNode, id, scopePath, scopedPath string) bool {
	return n.FileURI == scopedPath || strings.HasSuffix(n.FileURI, scopedPath) ||
		strings.HasPrefix(id, scopedPath) || strings.HasPrefix(id, scopePath)
}

// parseBaseEdgeScoped parses a base edge triple, keeping it only when both
// endpoints belong to the scoped node set.
func parseBaseEdgeScoped(block string, edgeMap map[string]*types.NativeEdge, matched map[string]bool) error {
	parts := strings.Fields(strings.TrimSuffix(strings.TrimSpace(block), "."))
	if len(parts) < 3 {
		return fmt.Errorf("malformed edge block dropped (expected 3+ fields): %.50s", block)
	}
	if parts[1] == "gm:status" {
		return nil // tombstone marker, not an edge
	}
	src := types.ParseNodeURI(parts[0])
	tgt := types.ParseNodeURI(parts[2])
	if !matched[src] || !matched[tgt] {
		return nil
	}
	key := fmt.Sprintf("%s|%s|%s", src, parts[1], tgt)
	edgeMap[key] = &types.NativeEdge{SourceID: src, Predicate: parts[1], TargetID: tgt}
	return nil
}

// bindEdgePropertyScoped binds a reified gm:lineNumber to a kept scoped edge.
func bindEdgePropertyScoped(block string, edgeMap map[string]*types.NativeEdge, matched map[string]bool) error {
	block = strings.TrimSuffix(strings.TrimSpace(block), ".")
	startIdx := strings.Index(block, "<<")
	endIdx := strings.LastIndex(block, ">>")
	if startIdx == -1 || endIdx == -1 || startIdx >= endIdx {
		return fmt.Errorf("malformed reified property block (no <<...>>): %.50s", block)
	}
	triplePart := strings.TrimSpace(block[startIdx+2 : endIdx])
	propsPart := strings.TrimSpace(block[endIdx+2:])

	tripleFields := strings.Fields(triplePart)
	if len(tripleFields) < 3 {
		return fmt.Errorf("malformed triple in reified prop (expected 3+ fields): %.50s", triplePart)
	}
	src := types.ParseNodeURI(tripleFields[0])
	pred := tripleFields[1]
	tgt := types.ParseNodeURI(tripleFields[2])
	key := fmt.Sprintf("%s|%s|%s", src, pred, tgt)
	edge, exists := edgeMap[key]
	if !exists {
		if !matched[src] || !matched[tgt] {
			return nil
		}
		edge = &types.NativeEdge{SourceID: src, Predicate: pred, TargetID: tgt}
		edgeMap[key] = edge
	}
	if strings.HasPrefix(propsPart, "gm:lineNumber") {
		if n, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(propsPart, "gm:lineNumber"))); err == nil {
			edge.LineNumber = n
		} else {
			return fmt.Errorf("bad gm:lineNumber value %q for edge %s: %v", propsPart, key, err)
		}
	}
	return nil
}

// ParseTTLNodeByID streams the TTL and returns the LAST node block whose ID
// matches nodeID (last-wins append semantics, tombstones excluded) plus every
// edge touching that node (outbound and inbound). Memory is bounded by the
// node's incident degree — the rest of the database is never materialized
// (AUDIT Issue 4 Phase 4A-2). If the node is absent, (nil, nil, nil) is
// returned with no error.
func ParseTTLNodeByID(ttlPath, nodeID string) (*types.TTLNode, []types.TTLEdge, error) {
	var found *types.TTLNode
	edgeMap := make(map[string]*types.TTLEdge)

	err := scanTTLStream(ttlPath, func(block string) error {
		trimmed := strings.TrimSpace(block)
		if strings.HasPrefix(trimmed, "<<") {
			// Bind a line number to a kept incident edge.
			block = strings.TrimSuffix(trimmed, ".")
			startIdx := strings.Index(block, "<<")
			endIdx := strings.LastIndex(block, ">>")
			if startIdx == -1 || endIdx == -1 || startIdx >= endIdx {
				return nil
			}
			triplePart := strings.TrimSpace(block[startIdx+2 : endIdx])
			fields := strings.Fields(triplePart)
			if len(fields) < 3 {
				return nil
			}
			src := types.ParseNodeURI(fields[0])
			tgt := types.ParseNodeURI(fields[2])
			key := fmt.Sprintf("%s|%s|%s", src, fields[1], tgt)
			if e, ok := edgeMap[key]; ok {
				if n, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(block[endIdx+2:]), "gm:lineNumber"))); err == nil {
					e.LineNumber = n
				}
			}
			return nil
		}
		if isBaseEdge(trimmed) {
			parts := strings.Fields(strings.TrimSuffix(trimmed, "."))
			if len(parts) < 3 {
				return nil
			}
			src := types.ParseNodeURI(parts[0])
			tgt := types.ParseNodeURI(parts[2])
			if src != nodeID && tgt != nodeID {
				return nil
			}
			if parts[1] == "gm:status" {
				return nil
			}
			key := fmt.Sprintf("%s|%s|%s", src, parts[1], tgt)
			edgeMap[key] = &types.TTLEdge{SourceID: src, Predicate: parts[1], TargetID: tgt}
			return nil
		}
		scratch := make(map[string]*types.TTLNode, 1)
		parseNodeBlock(block, scratch)
		if n, ok := scratch[nodeID]; ok {
			found = n // last block wins
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}

	edges := make([]types.TTLEdge, 0, len(edgeMap))
	for _, e := range edgeMap {
		edges = append(edges, *e)
	}
	return found, edges, nil
}

// StreamTTLNodes streams every node block in the TTL and invokes fn for
// each parsed node; iteration stops when fn returns false. Edge blocks and
// the metadata node are skipped, so --list/--search style reads never
// materialize the graph (AUDIT Issue 4 Phase 4A-2).
func StreamTTLNodes(ttlPath string, fn func(*types.TTLNode) bool) error {
	return scanTTLStream(ttlPath, func(block string) error {
		trimmed := strings.TrimSpace(block)
		if strings.HasPrefix(trimmed, "<<") || isBaseEdge(trimmed) {
			return nil
		}
		scratch := make(map[string]*types.TTLNode, 1)
		parseNodeBlock(block, scratch)
		for _, n := range scratch {
			if !fn(n) {
				return errStopStreaming
			}
		}
		return nil
	})
}

// errStopStreaming signals an early, successful termination of a stream.
var errStopStreaming = fmt.Errorf("streaming stopped by callback")

// StopStreaming reports whether an error returned by a StreamTTLNodes
// callback is the early-stop sentinel (which is not a failure).
func StopStreaming(err error) bool {
	return errors.Is(err, errStopStreaming)
}

// ApplyScope filters a VirtualSubgraph in-place to include only nodes matching the scope level and path.
func ApplyScope(sub *types.VirtualSubgraph, opts types.QueryOptions) {
	switch opts.Scope {
	case types.ScopeGlobal:
	case types.ScopeFolder:
		if opts.ScopePath == "" {
			return
		}
		filteredNodes := make(map[string]*types.TTLNode)
		for id, n := range sub.Nodes {
			if strings.HasPrefix(n.FileURI, opts.ScopePath) || strings.HasPrefix(id, opts.ScopePath) {
				filteredNodes[id] = n
			}
		}
		var filteredEdges []types.TTLEdge
		for _, e := range sub.Edges {
			_, srcOK := filteredNodes[e.SourceID]
			_, tgtOK := filteredNodes[e.TargetID]
			if srcOK && tgtOK {
				filteredEdges = append(filteredEdges, e)
			}
		}
		sub.Nodes = filteredNodes
		sub.Edges = filteredEdges
	case types.ScopeFile:
		if opts.ScopePath == "" {
			return
		}
		filteredNodes := make(map[string]*types.TTLNode)
		scopePath := opts.ScopePath
		if !strings.HasPrefix(scopePath, "/") {
			scopePath = "/" + scopePath
		}
		for id, n := range sub.Nodes {
			if n.FileURI == scopePath || strings.HasSuffix(n.FileURI, scopePath) || strings.HasPrefix(id, scopePath) || strings.HasPrefix(id, opts.ScopePath) {
				filteredNodes[id] = n
			}
		}
		var filteredEdges []types.TTLEdge
		for _, e := range sub.Edges {
			_, srcOK := filteredNodes[e.SourceID]
			_, tgtOK := filteredNodes[e.TargetID]
			if srcOK && tgtOK {
				filteredEdges = append(filteredEdges, e)
			}
		}
		sub.Nodes = filteredNodes
		sub.Edges = filteredEdges
	}
}
