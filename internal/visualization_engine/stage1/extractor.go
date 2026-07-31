package stage1

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/types"
)

var logParseWarn = func(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "WARN: "+format+"\n", args...)
}

// ExtractSubgraph parses a TTL file and extracts a subgraph matching the given diagram type and options.
func ExtractSubgraph(ttlPath string, t types.DiagramType, opts types.QueryOptions) (*types.VirtualSubgraph, error) {
	nodes, edges, err := ParseTTLFile(ttlPath)
	if err != nil {
		return nil, fmt.Errorf("failed to parse Turtle file: %w", err)
	}

	cfg := GetExtractionConfig(t, opts)
	return extractWithConfig(nodes, edges, cfg, opts), nil
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

func extractWithConfig(nodes map[string]*types.TTLNode, edges []types.TTLEdge, cfg types.ExtractionConfig, opts types.QueryOptions) *types.VirtualSubgraph {
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
	default:
		startIDs = filterNodes(nodes, opts, func(n *types.TTLNode) bool {
			return matchesKind(n.Kind, cfg.NodeKindFilter)
		})
	}

	if opts.EntryPointID != "" && cfg.EntryStrategy == types.EntryStrategyAll {
		startIDs = append(startIDs, opts.EntryPointID)
	}

	maxDepth := cfg.MaxDepth
	if maxDepth <= 0 {
		maxDepth = 7
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

	switch cfg.Direction {
	case types.EdgeDirectionForward:
		return bfsSubgraph(startIDs, nodes, edges, maxDepth, func(e types.TTLEdge) bool {
			return allowedPreds == nil || allowedPreds[e.Predicate]
		})
	case types.EdgeDirectionReverse:
		return reverseBFS(startIDs, nodes, edges, maxDepth, func(e types.TTLEdge) bool {
			return allowedPreds == nil || allowedPreds[e.Predicate]
		})
	default:
		return bothPassSubgraph(startIDs, nodes, edges, maxDepth, func(n *types.TTLNode) bool {
			return matchesKind(n.Kind, cfg.NodeKindFilter)
		}, func(e types.TTLEdge) bool {
			return allowedPreds == nil || allowedPreds[e.Predicate]
		})
	}
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

func reverseBFS(startIDs []string, allNodes map[string]*types.TTLNode, allEdges []types.TTLEdge, maxDepth int, edgeFilter func(types.TTLEdge) bool) *types.VirtualSubgraph {
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
			for _, e := range allEdges {
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

func bothPassSubgraph(startIDs []string, allNodes map[string]*types.TTLNode, allEdges []types.TTLEdge, maxDepth int, nodeFilter func(*types.TTLNode) bool, edgeFilter func(types.TTLEdge) bool) *types.VirtualSubgraph {
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
			for _, e := range allEdges {
				if (e.SourceID == id || e.TargetID == id) && edgeFilter(e) {
					edgeKey := fmt.Sprintf("%s|%s|%s", e.SourceID, e.Predicate, e.TargetID)
					if !edgeSeen[edgeKey] {
						edgeSeen[edgeKey] = true
						sub.Edges = append(sub.Edges, e)
					}
					if e.SourceID == id {
						next = append(next, e.TargetID)
					} else {
						next = append(next, e.SourceID)
					}
				}
			}
		}
		queue = next
	}
	return sub
}

var extractionConfigs = map[types.DiagramType]types.ExtractionConfig{
	types.UMLClass:             {Name: "UMLClass", NodeKindFilter: []string{"gm:TypeDecl", "gm:Member", "gm:Executable", "gm:External"}, PredicateGroup: []types.PredicateGroup{types.GroupTypeHierarchy, types.GroupComposition, types.GroupBinding}, EntryStrategy: types.EntryStrategyEntryPoint, MaxDepth: 7, Direction: types.EdgeDirectionForward, IncludeUnused: true},
	types.UMLObject:            {Name: "UMLObject", NodeKindFilter: []string{"gm:TypeDecl", "gm:External"}, PredicateGroup: []types.PredicateGroup{types.GroupTypeHierarchy}, EntryStrategy: types.EntryStrategyEntryPoint, MaxDepth: 3, Direction: types.EdgeDirectionForward},
	types.UMLComponent:         {Name: "UMLComponent", NodeKindFilter: []string{"gm:TypeDecl", "gm:Executable", "gm:Namespace", "gm:External"}, PredicateGroup: []types.PredicateGroup{types.GroupCallGraph, types.GroupComposition, types.GroupStructural}, EntryStrategy: types.EntryStrategyEntryPoint, MaxDepth: 7, Direction: types.EdgeDirectionForward},
	types.UMLDeployment:        {Name: "UMLDeployment", NodeKindFilter: []string{"gm:Namespace", "gm:File", "gm:Database", "gm:Executable", "gm:External"}, PredicateGroup: []types.PredicateGroup{types.GroupInfrastructure, types.GroupCallGraph, types.GroupStructural}, EntryStrategy: types.EntryStrategyAll, MaxDepth: 7, Direction: types.EdgeDirectionForward},
	types.UMLPackage:           {Name: "UMLPackage", NodeKindFilter: []string{"gm:Namespace", "gm:File", "gm:Module", "gm:External"}, PredicateGroup: []types.PredicateGroup{types.GroupStructural}, EntryStrategy: types.EntryStrategyAll, MaxDepth: 99, Direction: types.EdgeDirectionBoth},
	types.UMLComposite:         {Name: "UMLComposite", NodeKindFilter: []string{"gm:TypeDecl", "gm:Interface", "gm:Port", "gm:External"}, PredicateGroup: []types.PredicateGroup{types.GroupComposition, types.GroupTypeHierarchy}, EntryStrategy: types.EntryStrategyEntryPoint, MaxDepth: 5, Direction: types.EdgeDirectionForward},
	types.UMLProfile:           {Name: "UMLProfile", NodeKindFilter: []string{"gm:TypeDecl", "gm:Annotation"}, PredicateGroup: []types.PredicateGroup{types.GroupTypeHierarchy, types.GroupComposition}, EntryStrategy: types.EntryStrategyAll, MaxDepth: 5, Direction: types.EdgeDirectionForward},
	types.UMLUsecase:           {Name: "UMLUsecase", NodeKindFilter: []string{"gm:Annotation", "gm:Executable", "gm:Function", "gm:Method", "gm:External"}, PredicateGroup: []types.PredicateGroup{types.GroupCallGraph, types.GroupStructural}, EntryStrategy: types.EntryStrategyAuto, MaxDepth: 5, Direction: types.EdgeDirectionForward},
	types.UMLActivity:          {Name: "UMLActivity", NodeKindFilter: []string{"gm:ControlStructure", "gm:Block", "gm:Executable", "gm:Function", "gm:Method", "gm:External"}, PredicateGroup: []types.PredicateGroup{types.GroupControlFlow}, EntryStrategy: types.EntryStrategyEntryPoint, MaxDepth: 10, Direction: types.EdgeDirectionForward},
	types.Flowchart:            {Name: "Flowchart", PredicateGroup: []types.PredicateGroup{types.GroupControlFlow, types.GroupDataFlow, types.GroupCallGraph}, EntryStrategy: types.EntryStrategyEntryPoint, MaxDepth: 10, Direction: types.EdgeDirectionForward},
	types.UMLState:             {Name: "UMLState", NodeKindFilter: []string{"gm:Variable", "gm:ControlStructure", "gm:TypeDecl", "gm:Executable"}, PredicateGroup: []types.PredicateGroup{types.GroupDataFlow, types.GroupControlFlow}, EntryStrategy: types.EntryStrategyAll, MaxDepth: 5, Direction: types.EdgeDirectionForward},
	types.UMLSequence:          {Name: "UMLSequence", NodeKindFilter: []string{"gm:Executable", "gm:Function", "gm:External"}, PredicateGroup: []types.PredicateGroup{types.GroupCallGraph}, EntryStrategy: types.EntryStrategyEntryPoint, Direction: types.EdgeDirectionForward},
	types.UMLCommunication:     {Name: "UMLCommunication", NodeKindFilter: []string{"gm:Executable", "gm:Function", "gm:External"}, PredicateGroup: []types.PredicateGroup{types.GroupCallGraph}, EntryStrategy: types.EntryStrategyEntryPoint, Direction: types.EdgeDirectionForward},
	types.UMLInteractionOverview: {Name: "UMLInteractionOverview", NodeKindFilter: []string{"gm:Executable", "gm:Function", "gm:External"}, PredicateGroup: []types.PredicateGroup{types.GroupCallGraph}, EntryStrategy: types.EntryStrategyEntryPoint, Direction: types.EdgeDirectionForward},
	types.UMLTiming:            {Name: "UMLTiming", NodeKindFilter: []string{"gm:Executable", "gm:Function", "gm:Variable", "gm:External"}, PredicateGroup: []types.PredicateGroup{types.GroupCallGraph}, EntryStrategy: types.EntryStrategyAll, MaxDepth: 5, Direction: types.EdgeDirectionForward},
	types.C4Context:            {Name: "C4Context", NodeKindFilter: []string{"gm:User", "gm:ExternalSystem", "gm:Namespace", "gm:Module", "gm:External"}, PredicateGroup: []types.PredicateGroup{types.GroupCallGraph, types.GroupStructural, types.GroupInfrastructure}, EntryStrategy: types.EntryStrategyAuto, MaxDepth: 3, Direction: types.EdgeDirectionForward},
	types.C4Container:          {Name: "C4Container", NodeKindFilter: []string{"gm:Module", "gm:Namespace", "gm:Database", "gm:Executable", "gm:External"}, PredicateGroup: []types.PredicateGroup{types.GroupCallGraph, types.GroupStructural, types.GroupInfrastructure, types.GroupMessaging}, EntryStrategy: types.EntryStrategyAuto, MaxDepth: 3, Direction: types.EdgeDirectionForward},
	types.C4Component:          {Name: "C4Component", NodeKindFilter: []string{"gm:TypeDecl", "gm:Executable", "gm:Database", "gm:ExternalSystem", "gm:Function", "gm:Method", "gm:External"}, PredicateGroup: []types.PredicateGroup{types.GroupCallGraph, types.GroupComposition, types.GroupStructural, types.GroupInfrastructure}, EntryStrategy: types.EntryStrategyAuto, MaxDepth: 5, Direction: types.EdgeDirectionForward},
	types.C4Code:               {Name: "C4Code", NodeKindFilter: []string{"gm:TypeDecl", "gm:Member", "gm:Executable", "gm:External"}, PredicateGroup: []types.PredicateGroup{types.GroupTypeHierarchy, types.GroupComposition}, EntryStrategy: types.EntryStrategyEntryPoint, MaxDepth: 3, Direction: types.EdgeDirectionForward},
	types.C4Landscape:          {Name: "C4Landscape", NodeKindFilter: []string{"gm:Namespace", "gm:File", "gm:Module", "gm:ExternalSystem", "gm:External"}, PredicateGroup: []types.PredicateGroup{types.GroupStructural, types.GroupInfrastructure}, EntryStrategy: types.EntryStrategyAll, MaxDepth: 99, Direction: types.EdgeDirectionBoth},
	types.C4Dynamic:            {Name: "C4Dynamic", NodeKindFilter: []string{"gm:Executable", "gm:Function", "gm:External"}, PredicateGroup: []types.PredicateGroup{types.GroupCallGraph}, EntryStrategy: types.EntryStrategyEntryPoint, Direction: types.EdgeDirectionForward},
	types.C4Deployment:         {Name: "C4Deployment", NodeKindFilter: []string{"gm:Namespace", "gm:File", "gm:Executable", "gm:Database", "gm:External"}, PredicateGroup: []types.PredicateGroup{types.GroupStructural, types.GroupInfrastructure, types.GroupCallGraph}, EntryStrategy: types.EntryStrategyAll, MaxDepth: 3, Direction: types.EdgeDirectionForward},
	types.ERDiagram:            {Name: "ERDiagram", NodeKindFilter: []string{"gm:TypeDecl", "gm:Member", "gm:Struct", "gm:Class"}, PredicateGroup: []types.PredicateGroup{types.GroupComposition, types.GroupBinding}, EntryStrategy: types.EntryStrategyAll, MaxDepth: 3, Direction: types.EdgeDirectionBoth},
	types.DataFlow:             {Name: "DataFlow", NodeKindFilter: []string{"gm:Variable", "gm:Parameter", "gm:Executable", "gm:External"}, PredicateGroup: []types.PredicateGroup{types.GroupDataFlow, types.GroupSecurity, types.GroupBinding}, EntryStrategy: types.EntryStrategyAuto, MaxDepth: 10, Direction: types.EdgeDirectionBoth},
	types.Mindmap:              {Name: "Mindmap", NodeKindFilter: []string{"gm:Namespace", "gm:File", "gm:Module"}, PredicateGroup: []types.PredicateGroup{types.GroupStructural}, EntryStrategy: types.EntryStrategyAll, MaxDepth: 99, Direction: types.EdgeDirectionBoth},
	types.DependencyGraph:      {Name: "DependencyGraph", NodeKindFilter: []string{"gm:TypeDecl", "gm:File", "gm:Namespace", "gm:External"}, PredicateGroup: []types.PredicateGroup{types.GroupStructural}, EntryStrategy: types.EntryStrategyAll, MaxDepth: 3, Direction: types.EdgeDirectionBoth},
	types.HotspotComplexity:    {Name: "HotspotComplexity", NodeKindFilter: []string{"gm:Executable", "gm:Function", "gm:Method", "gm:External"}, PredicateGroup: []types.PredicateGroup{types.GroupCallGraph}, EntryStrategy: types.EntryStrategyAll, MaxDepth: 3, Direction: types.EdgeDirectionForward},
	types.CallGraph:            {Name: "CallGraph", NodeKindFilter: []string{"gm:Executable", "gm:Function", "gm:Method", "gm:External"}, PredicateGroup: []types.PredicateGroup{types.GroupCallGraph, types.GroupMessaging, types.GroupInfrastructure}, EntryStrategy: types.EntryStrategyEntryPoint, MaxDepth: 99, Direction: types.EdgeDirectionForward},
	types.LayeredArchitecture:  {Name: "LayeredArchitecture", NodeKindFilter: []string{"gm:TypeDecl", "gm:Executable", "gm:External"}, PredicateGroup: []types.PredicateGroup{types.GroupCallGraph, types.GroupComposition, types.GroupTypeHierarchy}, EntryStrategy: types.EntryStrategyAll, MaxDepth: 99, Direction: types.EdgeDirectionBoth},
	types.ChangeImpact:         {Name: "ChangeImpact", PredicateGroup: []types.PredicateGroup{types.GroupAny}, EntryStrategy: types.EntryStrategyChangedFiles, MaxDepth: 5, Direction: types.EdgeDirectionReverse},
	types.Infrastructure:       {Name: "Infrastructure", NodeKindFilter: []string{"gm:ExternalSystem", "gm:Database", "gm:Module", "gm:Namespace", "gm:External"}, PredicateGroup: []types.PredicateGroup{types.GroupInfrastructure, types.GroupStructural, types.GroupMessaging, types.GroupSecurity}, EntryStrategy: types.EntryStrategyAll, MaxDepth: 3, Direction: types.EdgeDirectionReverse},
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
func ParseTTLFile(ttlPath string) (map[string]*types.TTLNode, []types.TTLEdge, error) {
	file, err := os.Open(ttlPath)
	if err != nil {
		return nil, nil, err
	}
	defer file.Close()

	nodes := make(map[string]*types.TTLNode)
	edgeMap := make(map[string]*types.TTLEdge)

	scanner := bufio.NewScanner(file)
	var block []string

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "@prefix") || strings.HasPrefix(line, "#") {
			continue
		}

		block = append(block, line)
		if strings.HasSuffix(line, ".") {
			blockStr := strings.Join(block, " ")
			block = nil

			if strings.HasPrefix(blockStr, "<<") {
				bindEdgeProperty(blockStr, edgeMap)
			} else if isBaseEdge(blockStr) {
				parseBaseEdge(blockStr, edgeMap)
			} else {
				parseNodeBlock(blockStr, nodes)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, nil, err
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

func parseBaseEdge(block string, edgeMap map[string]*types.TTLEdge) {
	block = strings.TrimSuffix(strings.TrimSpace(block), ".")
	block = strings.TrimSpace(block)
	parts := strings.Fields(block)
	if len(parts) < 3 {
		logParseWarn("malformed edge block dropped (expected 3+ fields): %.50s", block)
		return
	}

	src := types.ParseNodeURI(parts[0])
	pred := parts[1]
	tgt := types.ParseNodeURI(parts[2])

	key := fmt.Sprintf("%s|%s|%s", src, pred, tgt)
	edgeMap[key] = &types.TTLEdge{
		SourceID:  src,
		Predicate: pred,
		TargetID:  tgt,
	}
}

func bindEdgeProperty(block string, edgeMap map[string]*types.TTLEdge) {
	block = strings.TrimSuffix(strings.TrimSpace(block), ".")
	block = strings.TrimSpace(block)

	startIdx := strings.Index(block, "<<")
	endIdx := strings.LastIndex(block, ">>")
	if startIdx == -1 || endIdx == -1 || startIdx >= endIdx {
		logParseWarn("malformed reified property block (no <<...>>): %.50s", block)
		return
	}

	triplePart := strings.TrimSpace(block[startIdx+2 : endIdx])
	propsPart := strings.TrimSpace(block[endIdx+2:])

	tripleFields := strings.Fields(triplePart)
	if len(tripleFields) < 3 {
		logParseWarn("malformed triple in reified prop (expected 3+ fields): %.50s", triplePart)
		return
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
			logParseWarn("bad gm:lineNumber value %q for edge %s: %v", numStr, key, err)
		}
	}
}

func parseNodeBlock(block string, nodes map[string]*types.TTLNode) {
	parts := strings.SplitN(block, " ", 2)
	if len(parts) < 2 {
		logParseWarn("malformed node block dropped (expected at least 2 fields): %.50s", block)
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
			logParseWarn("bogus predicate in node block for %s: %.50s", nodeID, attr)
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
			} else {
				logParseWarn("bad gm:lineStart value %q for node %s: %v", val, nodeID, err)
			}
		case "gm:lineEnd":
			if n, err := strconv.Atoi(val); err == nil {
				node.LineEnd = n
			} else {
				logParseWarn("bad gm:lineEnd value %q for node %s: %v", val, nodeID, err)
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

func parseLiteral(val string) string {
	val = strings.TrimSpace(val)
	if strings.HasPrefix(val, "\"\"\"") && strings.HasSuffix(val, "\"\"\"") {
		val = val[3 : len(val)-3]
	} else if strings.HasPrefix(val, "\"") && strings.HasSuffix(val, "\"") {
		val = val[1 : len(val)-1]
	}
	val = strings.ReplaceAll(val, "\\\"", "\"")
	val = strings.ReplaceAll(val, "\\n", "\n")
	val = strings.ReplaceAll(val, "\\\\", "\\")
	return val
}

// bfsSubgraph performs a depth-limited breadth-first search to extract a focused subgraph.
func bfsSubgraph(
	startIDs []string,
	allNodes map[string]*types.TTLNode,
	allEdges []types.TTLEdge,
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
			for _, e := range allEdges {
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


