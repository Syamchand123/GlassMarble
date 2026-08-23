package extract

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/product/ids"
	"github.com/Syamchand123/GlassMarble/internal/product/ont"
	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/types"
)

// graphJSON and graphNodeJSON/graphEdgeJSON mirror the akg package's JSON structures
// for parsing GraphJSON files without importing akg (avoiding circular dependency).
type graphJSON struct {
	SchemaVersion   int                          `json:"schema_version"`
	CommitHash      string                       `json:"commit_hash"`
	Version         uint64                       `json:"version"`
	Entrypoints     []string                     `json:"entrypoints,omitempty"`
	Nodes           []graphNodeJSON              `json:"nodes"`
	Edges           []graphEdgeJSON              `json:"edges"`
	Summary         *types.GraphSummary          `json:"summary,omitempty"`
	Errors          []danglingReferenceErrorJSON `json:"errors,omitempty"`
	Verified        bool                         `json:"verified,omitempty"`
	VerificationMsg string                       `json:"verification_msg,omitempty"`
}

type danglingReferenceErrorJSON struct {
	SourceID   string `json:"source_id"`
	TargetID   string `json:"target_id"`
	EdgeType   string `json:"edge_type"`
	LineNumber int    `json:"line_number"`
	Message    string `json:"message"`
}

type graphNodeJSON struct {
	ID              string             `json:"id"`
	Kind            string             `json:"kind"`
	Name            string             `json:"name"`
	Primitive       string             `json:"primitive,omitempty"`
	PrimitiveScores map[string]float64 `json:"primitive_scores,omitempty"`
	FileSpec        fileSpecJSON       `json:"file_spec"`
	Properties      map[string]string  `json:"properties,omitempty"`
}

type graphEdgeJSON struct {
	SourceID   string            `json:"source_id"`
	TargetID   string            `json:"target_id"`
	Type       string            `json:"type"`
	LineNumber int               `json:"line_number,omitempty"`
	Confidence float32           `json:"confidence,omitempty"`
	IsCycle    bool              `json:"is_cycle,omitempty"`
	Properties map[string]string `json:"properties,omitempty"`
}

type fileSpecJSON struct {
	Path      string `json:"path"`
	LineStart int    `json:"line_start,omitempty"`
	LineEnd   int    `json:"line_end,omitempty"`
}

func parseGraphJSON(statePath string) (map[string]*types.TTLNode, []types.TTLEdge, error) {
	if strings.HasSuffix(statePath, ".ttl") {
		return ParseTTLFile(statePath)
	}

	file, err := os.Open(statePath)
	if err != nil {
		return nil, nil, err
	}
	defer file.Close()

	var g graphJSON
	if err := json.NewDecoder(file).Decode(&g); err != nil {
		if nodes, edges, ttlErr := ParseTTLFile(statePath); ttlErr == nil {
			return nodes, edges, nil
		}
		return nil, nil, err
	}

	nodes := make(map[string]*types.TTLNode, len(g.Nodes))
	for _, gn := range g.Nodes {
		kind := mapKindToClass(gn.Kind)
		nodes[gn.ID] = &types.TTLNode{
			ID:            gn.ID,
			Kind:          kind,
			Name:          gn.Name,
			PrimitiveType: gn.Primitive,
			FileURI:       gn.FileSpec.Path,
			LineStart:     gn.FileSpec.LineStart,
			LineEnd:       gn.FileSpec.LineEnd,
			Properties:    gn.Properties,
		}
	}

	edges := make([]types.TTLEdge, 0, len(g.Edges))
	for _, ge := range g.Edges {
		pred := mapEdgeTypeToPredicate(ge.Type)
		edges = append(edges, types.TTLEdge{
			SourceID:   ge.SourceID,
			Predicate:  pred,
			TargetID:   ge.TargetID,
			LineNumber: ge.LineNumber,
		})
	}

	return nodes, edges, nil
}

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

// ExtractSubgraph parses a GraphJSON file and extracts a subgraph matching the given diagram type and options.
func ExtractSubgraph(StatePath string, t types.DiagramType, opts types.QueryOptions) (*types.VirtualSubgraph, error) {
	nodes, edges, err := parseGraphJSON(StatePath)
	if err != nil {
		return nil, fmt.Errorf("failed to parse GraphJSON file: %w", err)
	}

	cfg := GetExtractionConfig(t, opts)
	return extractWithConfig(nodes, edges, cfg, opts)
}

func predicatesForGroup(group types.PredicateGroup) []string {
	switch group {
	case types.GroupCallGraph:
		return []string{ont.PredCalls, ont.PredSpawnsConcurrent, ont.PredDispatchesEvent, ont.PredContextualCall, ont.PredFfiCall, ont.PredDispatchesAsync}
	case types.GroupTypeHierarchy:
		return []string{ont.PredInheritsFrom, ont.PredExtends, ont.PredImplements, ont.PredMixes}
	case types.GroupComposition:
		return []string{ont.PredComposes, ont.PredHasMember, ont.PredHasField, ont.PredAggregates, ont.PredContains}
	case types.GroupDataFlow:
		return []string{ont.PredDataFlowTo, ont.PredPointsTo, ont.PredHeapAlias, ont.PredAliasesPointer, ont.PredVulnerableTaint}
	case types.GroupControlFlow:
		return []string{ont.PredControlFlowTo, ont.PredControlFlowToTrue, ont.PredControlFlowToFalse, ont.PredCatchesException, ont.PredDefersExecution, ont.PredBranchConstraint}
	case types.GroupStructural:
		return []string{ont.PredBelongsToFile, ont.PredBelongsToNamespace, ont.PredBelongsTo, ont.PredDependsOn, ont.PredReferences, ont.PredImports}
	case types.GroupMessaging:
		return []string{ont.PredSendsMessage, ont.PredReceivesMessage, ont.PredPublishesEvent, ont.PredSubscribesEvent, ont.PredDispatchesEvent}
	case types.GroupInfrastructure:
		return []string{ont.PredNetworkCall, ont.PredQueriesDatabase, ont.PredCallsCloudAPI, ont.PredConsumesResource, ont.PredMutatesGlobal, ont.PredSecuritySink, ont.PredExposesEndpoint}
	case types.GroupSecurity:
		return []string{ont.PredVulnerableTaint, ont.PredSecuritySink, ont.PredConsumesResource}
	case types.GroupBinding:
		return []string{ont.PredInstantiatesGeneric, ont.PredDiInjects, ont.PredEscapesToHeap, ont.PredAliasesType}
	case types.GroupAny:
		return nil
	default:
		return nil
	}
}

func extractWithConfig(nodes map[string]*types.TTLNode, edges []types.TTLEdge, cfg types.ExtractionConfig, opts types.QueryOptions) (*types.VirtualSubgraph, error) {
	entryCandidate := func(n *types.TTLNode) bool {
		return matchesKind(n.Kind, cfg.NodeKindFilter) || matchesKind(n.Kind, []string{ont.PredExecutable, ont.PredFunction, ont.PredMethod})
	}
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
			startIDs = getEntryPoints(nodes, opts, entryCandidate)
		}
	case types.EntryStrategyAuto:
		startIDs = getEntryPoints(nodes, opts, entryCandidate)
	case types.EntryStrategyChangedFiles:
		if len(opts.ChangedFiles) > 0 {
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
			if len(startIDs) == 0 {
				return nil, fmt.Errorf("no graph nodes match changed files: %v", opts.ChangedFiles)
			}
		} else if opts.EntryPointID != "" {
			startIDs = []string{opts.EntryPointID}
		} else {
			startIDs = getEntryPoints(nodes, opts, entryCandidate)
		}
	default:
		// EntryStrategyAll: start from an explicit entry point when given,
		// otherwise extract the flat subgraph of every matching node and the
		// edges between them (no traversal, no kind-pollution).
		if opts.EntryPointID != "" {
			startIDs = []string{opts.EntryPointID}
		}
	}

	if opts.EntryPointID != "" && cfg.EntryStrategy == types.EntryStrategyAll && len(startIDs) == 0 {
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

	kindFilter := func(n *types.TTLNode) bool {
		return matchesKind(n.Kind, cfg.NodeKindFilter)
	}

	// EntryStrategyAll with no explicit entry means a flat extraction: every
	// matching node plus the edges whose endpoints are both in that set. A
	// forward/reverse traversal with no seeds would otherwise produce an empty
	// diagram, so funnel those into the flat both-pass path too.
	if len(startIDs) == 0 {
		return bothPassSubgraph(nil, nodes, edges, idx, maxDepth, kindFilter, edgeFilter), nil
	}

	switch cfg.Direction {
	case types.EdgeDirectionForward:
		return bfsSubgraph(startIDs, nodes, edges, idx, maxDepth, edgeFilter), nil
	case types.EdgeDirectionReverse:
		return reverseBFS(startIDs, nodes, edges, idx, maxDepth, edgeFilter), nil
	default:
		return bothPassSubgraph(startIDs, nodes, edges, idx, maxDepth, kindFilter, edgeFilter), nil
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

func mapKindToClass(kind string) string {
	switch kind {
	case "MODULE":
		return ont.PredModule
	case "NAMESPACE":
		return ont.PredNamespace
	case "FILE":
		return ont.PredFile
	case "STRUCT":
		return ont.PredStruct
	case "CLASS":
		return ont.PredClass
	case "INTERFACE":
		return ont.PredInterface
	case "FUNCTION":
		return ont.PredFunction
	case "METHOD":
		return ont.PredMethod
	case "FIELD":
		return ont.PredMember
	case "PARAMETER":
		return ont.PredParameter
	case "VARIABLE", "DFG_VAR":
		return ont.PredVariable
	case "PACKAGE":
		return ont.PredPackage
	case "META_DATA":
		return ont.PredMetaData
	case "TYPE_DECL", "TYPE":
		return ont.PredStruct
	case "EXECUTABLE":
		return ont.PredFunction
	case "IF_BRANCH", "LOOP_BRANCH", "SWITCH_BRANCH":
		return ont.PredControlStructure
	case "CFG_SUMMARY":
		return ont.PredCFGSummary
	case "DFG_SUMMARY":
		return ont.PredDFGSummary
	case "EVENT_TOPIC":
		return ont.PredEventTopic
	case "VIRTUAL_DATABASE":
		return ont.PredVirtualDatabase
	case "VIRTUAL_ENDPOINT":
		return ont.PredVirtualEndpoint
	case "BLOCK":
		return ont.PredBlock
	case "ANNOTATION", "DECORATOR":
		return ont.PredAnnotation
	case "VIRTUAL_CONTEXT":
		return ont.PredVirtualContext
	case "VIRTUAL_QUEUE":
		return ont.PredVirtualQueue
	case "USER":
		return ont.PredUser
	case "EXTERNAL_API":
		return ont.PredExternalAPI
	case "EXTERNAL_SDK":
		return ont.PredExternalSDK
	case "EXTERNAL_FFI":
		return ont.PredExternalFFI
	case "EXTERNAL":
		return ont.PredExternal
	default:
		return kind
	}
}

func mapEdgeTypeToPredicate(edgeType string) string {
	switch edgeType {
	case "CALLS":
		return ont.PredCalls
	case "IMPLEMENTS":
		return ont.PredImplements
	case "EXTENDS":
		return ont.PredExtends
	case "COMPOSES":
		return ont.PredComposes
	case "REFERENCES":
		return ont.PredReferences
	case "THROWS":
		return ont.PredThrows
	case "SPAWNS_CONCURRENT":
		return ont.PredSpawnsConcurrent
	case "DISPATCHES_EVENT":
		return ont.PredDispatchesEvent
	case "EXPOSES_ENDPOINT":
		return ont.PredExposesEndpoint
	case "SECURITY_SINK":
		return ont.PredSecuritySink
	case "CONSUMES_RESOURCE":
		return ont.PredConsumesResource
	case "MUTATES_GLOBAL":
		return ont.PredMutatesGlobal
	case "ALIASES_TYPE":
		return ont.PredAliasesType
	case "CONTROL_FLOW", "CONDITIONAL_BRANCH", "LOOP_BRANCH", "SWITCH_BRANCH":
		return ont.PredControlFlowTo
	case "DATA_FLOW":
		return ont.PredDataFlowTo
	case "ALIASES":
		return ont.PredAliasesPointer
	case "VULNERABLE":
		return ont.PredVulnerableTaint
	case "INSTANTIATES":
		return ont.PredInstantiatesGeneric
	case "VIRTUAL_CONTEXT":
		return ont.PredVirtualContextLink
	case "SENDS_TO":
		return ont.PredSendsMessage
	case "RECEIVES_FROM":
		return ont.PredReceivesMessage
	case "CYCLIC":
		return ont.PredCyclicDependency
	case "NETWORK_CALL":
		return ont.PredNetworkCall
	case "QUERIES_DB":
		return ont.PredQueriesDatabase
	case "CALLS_CLOUD_API":
		return ont.PredCallsCloudAPI
	case "CATCHES":
		return ont.PredCatchesException
	case "DEFERS":
		return ont.PredDefersExecution
	case "BELONGS_TO":
		return ont.PredBelongsTo
	case "DEPENDS_ON":
		return ont.PredDependsOn
	case "CONTAINS":
		return ont.PredContains
	case "MIXES":
		return ont.PredMixes
	case "HAS_FIELD":
		return ont.PredHasField
	case "HAS_PARAM":
		return ont.PredHasParam
	case "RETURNS":
		return ont.PredReturns
	case "HAS_RECEIVER":
		return ont.PredHasReceiver
	default:
		return edgeType
	}
}

func matchesKind(kind string, filters []string) bool {
	if len(filters) == 0 {
		return true
	}
	normKind := mapKindToClass(kind)
	for _, f := range filters {
		normFilter := mapKindToClass(f)
		if normKind == normFilter || strings.EqualFold(kind, f) || strings.EqualFold(normKind, f) {
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
// gm:Executable, gm:ControlStructure, gm:CFGFlow, gm:ExceptionalBranch,
// gm:AbstractConstraint, gm:EventTopic, gm:VirtualDatabase, gm:VirtualEndpoint,
// gm:Block, gm:Annotation and the synthetic gm:ExternalSDK/API/FFI, gm:Virtual*
// classes) — see internal/akg/turtle_serializer.go mapKindToClass.
//
// Predicate groups mirror the edges the serializer actually emits
// (gm:calls, gm:contextualCall, gm:controlFlowTo, gm:defersExecution,
// gm:branchConstraint, gm:contains, gm:dependsOn, gm:dataFlowTo,
// gm:queriesDatabase, gm:callsCloudAPI, gm:instantiatesGeneric,
// gm:escapesToHeap, gm:exposesEndpoint, gm:sendsMessage, gm:receivesMessage,
// gm:spawnsConcurrent, gm:securitySink, ...). Predicates the AKG never emits
// (gm:inheritsFrom/extends/implements/composes/hasMember/hasField, ...) are
// kept on the groups so diagrams remain correct for databases that do
// serialize them, while diagrams that previously consumed only CFG noise
// (gm:branchConstraint) now use those groups only where they belong.
//
// EntryStrategy semantics:
//   - EntryStrategyAll extracts the flat subgraph of every matching node plus
//     edges whose endpoints are both in that set; an explicit --entry switches
//     it to a focused traversal from that node.
//   - EntryStrategyEntryPoint traverses from the given --entry, or from an
//     auto-discovered entry point (gm:isEntrypoint flag, main, handler/
//     controller/api naming).
var extractionConfigs = map[types.DiagramType]types.ExtractionConfig{
	types.UMLClass:               {Name: "UMLClass", NodeKindFilter: []string{ont.PredTypeDecl, ont.PredStruct, ont.PredClass, ont.PredInterface, ont.PredMember, ont.PredFunction, ont.PredMethod, ont.PredExecutable, ont.PredExternal}, PredicateGroup: []types.PredicateGroup{types.GroupTypeHierarchy, types.GroupComposition, types.GroupCallGraph}, EntryStrategy: types.EntryStrategyAll, MaxDepth: 7, Direction: types.EdgeDirectionBoth, IncludeUnused: true, Views: []types.ViewTag{types.ViewStructural}},
	types.UMLObject:              {Name: "UMLObject", NodeKindFilter: []string{ont.PredTypeDecl, ont.PredStruct, ont.PredClass, ont.PredInterface, ont.PredFunction, ont.PredMethod, ont.PredExternal}, PredicateGroup: []types.PredicateGroup{types.GroupTypeHierarchy, types.GroupComposition, types.GroupCallGraph}, EntryStrategy: types.EntryStrategyEntryPoint, MaxDepth: 3, Direction: types.EdgeDirectionBoth, Views: []types.ViewTag{types.ViewStructural}},
	types.UMLComponent:           {Name: "UMLComponent", NodeKindFilter: []string{ont.PredStruct, ont.PredClass, ont.PredInterface, ont.PredModule, ont.PredFunction, ont.PredMethod, ont.PredExternal}, PredicateGroup: []types.PredicateGroup{types.GroupCallGraph, types.GroupComposition, types.GroupStructural}, EntryStrategy: types.EntryStrategyAll, MaxDepth: 7, Direction: types.EdgeDirectionBoth, IncludeUnused: true, Views: []types.ViewTag{types.ViewStructural}},
	types.UMLDeployment:          {Name: "UMLDeployment", NodeKindFilter: []string{ont.PredNamespace, ont.PredModule, ont.PredFile, ont.PredVirtualDatabase, ont.PredExecutable, ont.PredFunction, ont.PredMethod, ont.PredExternal}, PredicateGroup: []types.PredicateGroup{types.GroupInfrastructure, types.GroupCallGraph, types.GroupStructural}, EntryStrategy: types.EntryStrategyAll, MaxDepth: 7, Direction: types.EdgeDirectionForward, IncludeUnused: true, Views: []types.ViewTag{types.ViewStructural}},
	types.UMLPackage:             {Name: "UMLPackage", NodeKindFilter: []string{ont.PredNamespace, ont.PredFile, ont.PredModule, ont.PredExternal}, PredicateGroup: []types.PredicateGroup{types.GroupStructural}, EntryStrategy: types.EntryStrategyAll, MaxDepth: 99, Direction: types.EdgeDirectionBoth, IncludeUnused: true, Views: []types.ViewTag{types.ViewStructural}},
	types.UMLComposite:           {Name: "UMLComposite", NodeKindFilter: []string{ont.PredTypeDecl, ont.PredStruct, ont.PredClass, ont.PredInterface, ont.PredMember, ont.PredFunction, ont.PredMethod, ont.PredExternal}, PredicateGroup: []types.PredicateGroup{types.GroupComposition, types.GroupTypeHierarchy, types.GroupCallGraph}, EntryStrategy: types.EntryStrategyEntryPoint, MaxDepth: 5, Direction: types.EdgeDirectionBoth, Views: []types.ViewTag{types.ViewStructural}},
	types.UMLProfile:             {Name: "UMLProfile", NodeKindFilter: []string{ont.PredTypeDecl, ont.PredStruct, ont.PredClass, ont.PredInterface, ont.PredAnnotation, ont.PredExternal}, PredicateGroup: []types.PredicateGroup{types.GroupTypeHierarchy, types.GroupComposition, types.GroupBinding}, EntryStrategy: types.EntryStrategyAll, MaxDepth: 5, Direction: types.EdgeDirectionBoth, IncludeUnused: true, Views: []types.ViewTag{types.ViewStructural}},
	types.UMLUsecase:             {Name: "UMLUsecase", NodeKindFilter: []string{ont.PredExecutable, ont.PredFunction, ont.PredMethod, ont.PredExternal}, PredicateGroup: []types.PredicateGroup{types.GroupCallGraph, types.GroupStructural}, EntryStrategy: types.EntryStrategyEntryPoint, MaxDepth: 5, Direction: types.EdgeDirectionForward, Views: []types.ViewTag{types.ViewStructural}},
	types.UMLActivity:            {Name: "UMLActivity", NodeKindFilter: []string{ont.PredControlStructure, ont.PredBlock, ont.PredCFGFlow, ont.PredExecutable, ont.PredFunction, ont.PredMethod, ont.PredExternal}, PredicateGroup: []types.PredicateGroup{types.GroupControlFlow}, EntryStrategy: types.EntryStrategyEntryPoint, MaxDepth: 10, Direction: types.EdgeDirectionForward, Views: []types.ViewTag{types.ViewDynamic}},
	types.Flowchart:              {Name: "Flowchart", PredicateGroup: []types.PredicateGroup{types.GroupControlFlow, types.GroupDataFlow, types.GroupCallGraph}, EntryStrategy: types.EntryStrategyEntryPoint, MaxDepth: 10, Direction: types.EdgeDirectionForward, Views: []types.ViewTag{types.ViewDynamic}},
	types.UMLState:               {Name: "UMLState", NodeKindFilter: []string{ont.PredControlStructure, ont.PredCFGFlow, ont.PredExceptionalBranch, ont.PredBlock}, PredicateGroup: []types.PredicateGroup{types.GroupControlFlow, types.GroupDataFlow}, EntryStrategy: types.EntryStrategyEntryPoint, MaxDepth: 99, Direction: types.EdgeDirectionForward, Views: []types.ViewTag{types.ViewDynamic}},
	types.UMLSequence:            {Name: "UMLSequence", NodeKindFilter: []string{ont.PredExecutable, ont.PredFunction, ont.PredMethod, ont.PredExternal}, PredicateGroup: []types.PredicateGroup{types.GroupCallGraph}, EntryStrategy: types.EntryStrategyEntryPoint, Direction: types.EdgeDirectionForward, Views: []types.ViewTag{types.ViewStructural, types.ViewDynamic}},
	types.UMLCommunication:       {Name: "UMLCommunication", NodeKindFilter: []string{ont.PredExecutable, ont.PredFunction, ont.PredMethod, ont.PredExternal}, PredicateGroup: []types.PredicateGroup{types.GroupCallGraph}, EntryStrategy: types.EntryStrategyEntryPoint, Direction: types.EdgeDirectionForward, Views: []types.ViewTag{types.ViewStructural, types.ViewDynamic}},
	types.UMLInteractionOverview: {Name: "UMLInteractionOverview", NodeKindFilter: []string{ont.PredExecutable, ont.PredFunction, ont.PredMethod, ont.PredExternal}, PredicateGroup: []types.PredicateGroup{types.GroupCallGraph}, EntryStrategy: types.EntryStrategyEntryPoint, Direction: types.EdgeDirectionForward, Views: []types.ViewTag{types.ViewStructural, types.ViewDynamic}},
	types.UMLTiming:              {Name: "UMLTiming", NodeKindFilter: []string{ont.PredExecutable, ont.PredFunction, ont.PredMethod, ont.PredVariable, ont.PredExternal}, PredicateGroup: []types.PredicateGroup{types.GroupCallGraph}, EntryStrategy: types.EntryStrategyAll, MaxDepth: 5, Direction: types.EdgeDirectionForward, IncludeUnused: true, Views: []types.ViewTag{types.ViewStructural, types.ViewDynamic}},
	types.C4Context:              {Name: "C4Context", NodeKindFilter: []string{ont.PredUser, ont.PredExternalSDK, ont.PredExternalAPI, ont.PredExternalFFI, ont.PredExternal, ont.PredVirtualDatabase, ont.PredVirtualContext, ont.PredVirtualEndpoint, ont.PredModule, ont.PredNamespace, ont.PredFile, ont.PredStruct, ont.PredClass, ont.PredFunction, ont.PredMethod}, PredicateGroup: []types.PredicateGroup{types.GroupCallGraph, types.GroupStructural, types.GroupInfrastructure}, EntryStrategy: types.EntryStrategyAll, MaxDepth: 3, Direction: types.EdgeDirectionBoth, IncludeUnused: true, Views: []types.ViewTag{types.ViewStructural}},
	types.C4Container:            {Name: "C4Container", NodeKindFilter: []string{ont.PredModule, ont.PredNamespace, ont.PredFile, ont.PredVirtualDatabase, ont.PredExecutable, ont.PredFunction, ont.PredMethod, ont.PredExternal}, PredicateGroup: []types.PredicateGroup{types.GroupCallGraph, types.GroupStructural, types.GroupInfrastructure, types.GroupMessaging}, EntryStrategy: types.EntryStrategyAll, MaxDepth: 3, Direction: types.EdgeDirectionBoth, IncludeUnused: true, Views: []types.ViewTag{types.ViewStructural}},
	types.C4Component:            {Name: "C4Component", NodeKindFilter: []string{ont.PredStruct, ont.PredClass, ont.PredInterface, ont.PredFunction, ont.PredMethod, ont.PredVirtualDatabase, ont.PredExternal}, PredicateGroup: []types.PredicateGroup{types.GroupCallGraph, types.GroupComposition, types.GroupStructural, types.GroupInfrastructure}, EntryStrategy: types.EntryStrategyAll, MaxDepth: 5, Direction: types.EdgeDirectionBoth, IncludeUnused: true, Views: []types.ViewTag{types.ViewStructural}},
	types.C4Code:                 {Name: "C4Code", NodeKindFilter: []string{ont.PredTypeDecl, ont.PredStruct, ont.PredClass, ont.PredInterface, ont.PredMember, ont.PredFunction, ont.PredMethod, ont.PredExecutable, ont.PredExternal}, PredicateGroup: []types.PredicateGroup{types.GroupTypeHierarchy, types.GroupComposition, types.GroupCallGraph}, EntryStrategy: types.EntryStrategyAll, MaxDepth: 3, Direction: types.EdgeDirectionBoth, IncludeUnused: true, Views: []types.ViewTag{types.ViewStructural}},
	types.C4Landscape:            {Name: "C4Landscape", NodeKindFilter: []string{ont.PredNamespace, ont.PredModule, ont.PredFile, ont.PredStruct, ont.PredClass, ont.PredFunction, ont.PredMethod, ont.PredVirtualDatabase, ont.PredExternalSDK, ont.PredExternalAPI, ont.PredExternalFFI, ont.PredExternal}, PredicateGroup: []types.PredicateGroup{types.GroupStructural, types.GroupInfrastructure}, EntryStrategy: types.EntryStrategyAll, MaxDepth: 99, Direction: types.EdgeDirectionBoth, IncludeUnused: true, Views: []types.ViewTag{types.ViewStructural}},
	types.C4Dynamic:              {Name: "C4Dynamic", NodeKindFilter: []string{ont.PredExecutable, ont.PredFunction, ont.PredMethod, ont.PredExternal}, PredicateGroup: []types.PredicateGroup{types.GroupCallGraph}, EntryStrategy: types.EntryStrategyEntryPoint, Direction: types.EdgeDirectionForward, Views: []types.ViewTag{types.ViewStructural, types.ViewDynamic}},
	types.C4Deployment:           {Name: "C4Deployment", NodeKindFilter: []string{ont.PredNamespace, ont.PredModule, ont.PredFile, ont.PredExecutable, ont.PredFunction, ont.PredMethod, ont.PredVirtualDatabase, ont.PredExternal}, PredicateGroup: []types.PredicateGroup{types.GroupStructural, types.GroupInfrastructure, types.GroupCallGraph}, EntryStrategy: types.EntryStrategyAll, MaxDepth: 3, Direction: types.EdgeDirectionForward, IncludeUnused: true, Views: []types.ViewTag{types.ViewStructural}},
	types.ERDiagram:              {Name: "ERDiagram", NodeKindFilter: []string{ont.PredTypeDecl, ont.PredStruct, ont.PredClass, ont.PredInterface, ont.PredMember}, PredicateGroup: []types.PredicateGroup{types.GroupComposition, types.GroupBinding, types.GroupTypeHierarchy}, EntryStrategy: types.EntryStrategyAll, MaxDepth: 3, Direction: types.EdgeDirectionBoth, IncludeUnused: true, Views: []types.ViewTag{types.ViewStructural}},
	types.DataFlow:               {Name: "DataFlow", NodeKindFilter: []string{ont.PredVariable, ont.PredParameter, ont.PredExecutable, ont.PredFunction, ont.PredMethod, ont.PredExternal, ont.PredVirtualTaintSource, ont.PredVirtualSecuritySink, ont.PredStruct, ont.PredClass, ont.PredInterface}, PredicateGroup: []types.PredicateGroup{types.GroupDataFlow, types.GroupSecurity, types.GroupBinding, types.GroupCallGraph, types.GroupStructural}, EntryStrategy: types.EntryStrategyAll, MaxDepth: 10, Direction: types.EdgeDirectionBoth, IncludeUnused: true, Views: []types.ViewTag{types.ViewDynamic, types.ViewSecurity, types.ViewStructural}},
	types.Mindmap:                {Name: "Mindmap", NodeKindFilter: []string{ont.PredNamespace, ont.PredModule, ont.PredFile, ont.PredPackage, ont.PredStruct, ont.PredClass, ont.PredInterface, ont.PredFunction, ont.PredMethod}, PredicateGroup: []types.PredicateGroup{types.GroupStructural}, EntryStrategy: types.EntryStrategyAll, MaxDepth: 99, Direction: types.EdgeDirectionBoth, IncludeUnused: true, Views: []types.ViewTag{types.ViewStructural}},
	types.DependencyGraph:        {Name: "DependencyGraph", NodeKindFilter: []string{ont.PredTypeDecl, ont.PredStruct, ont.PredClass, ont.PredInterface, ont.PredFile, ont.PredNamespace, ont.PredModule, ont.PredExternal, ont.PredFunction, ont.PredMethod}, PredicateGroup: []types.PredicateGroup{types.GroupStructural, types.GroupBinding, types.GroupCallGraph, types.GroupComposition, types.GroupTypeHierarchy}, EntryStrategy: types.EntryStrategyAll, MaxDepth: 7, Direction: types.EdgeDirectionBoth, IncludeUnused: true, Views: []types.ViewTag{types.ViewStructural}},
	types.HotspotComplexity:      {Name: "HotspotComplexity", NodeKindFilter: []string{ont.PredExecutable, ont.PredFunction, ont.PredMethod, ont.PredExternal}, PredicateGroup: []types.PredicateGroup{types.GroupCallGraph}, EntryStrategy: types.EntryStrategyAll, MaxDepth: 3, Direction: types.EdgeDirectionForward, IncludeUnused: true, Views: []types.ViewTag{types.ViewStructural}},
	types.CallGraph:              {Name: "CallGraph", NodeKindFilter: []string{ont.PredExecutable, ont.PredFunction, ont.PredMethod, ont.PredExternal}, PredicateGroup: []types.PredicateGroup{types.GroupCallGraph, types.GroupMessaging, types.GroupInfrastructure}, EntryStrategy: types.EntryStrategyAll, MaxDepth: 99, Direction: types.EdgeDirectionBoth, IncludeUnused: true, Views: []types.ViewTag{types.ViewStructural, types.ViewDynamic}},
	types.LayeredArchitecture:    {Name: "LayeredArchitecture", NodeKindFilter: []string{ont.PredTypeDecl, ont.PredStruct, ont.PredClass, ont.PredInterface, ont.PredExecutable, ont.PredFunction, ont.PredMethod, ont.PredExternal}, PredicateGroup: []types.PredicateGroup{types.GroupCallGraph, types.GroupComposition, types.GroupTypeHierarchy}, EntryStrategy: types.EntryStrategyAll, MaxDepth: 99, Direction: types.EdgeDirectionBoth, IncludeUnused: true, Views: []types.ViewTag{types.ViewStructural}},
	types.ChangeImpact:           {Name: "ChangeImpact", PredicateGroup: []types.PredicateGroup{types.GroupAny}, EntryStrategy: types.EntryStrategyChangedFiles, MaxDepth: 5, Direction: types.EdgeDirectionReverse, Views: []types.ViewTag{types.ViewStructural}},
	types.Infrastructure:         {Name: "Infrastructure", NodeKindFilter: []string{ont.PredExternalSDK, ont.PredExternalAPI, ont.PredExternalFFI, ont.PredVirtualDatabase, ont.PredVirtualContext, ont.PredVirtualEndpoint, ont.PredVirtualQueue, ont.PredVirtualSecuritySink, ont.PredVirtualCloudAPI, ont.PredModule, ont.PredNamespace, ont.PredFile, ont.PredFunction, ont.PredMethod, ont.PredExternal}, PredicateGroup: []types.PredicateGroup{types.GroupInfrastructure, types.GroupStructural, types.GroupMessaging, types.GroupSecurity, types.GroupCallGraph}, EntryStrategy: types.EntryStrategyAll, MaxDepth: 5, Direction: types.EdgeDirectionBoth, IncludeUnused: true, Views: []types.ViewTag{types.ViewStructural, types.ViewSecurity}},
}

// GetExtractionConfig returns the per-diagram extraction configuration,
// applying the default view set (all views) when a config does not declare
// one (master_overhaul_plan.md §4.5 / W0-05). View-based edge filtering is
// applied by later phases (W4-01 §8.4).
func GetExtractionConfig(t types.DiagramType, opts types.QueryOptions) types.ExtractionConfig {
	cfg, ok := extractionConfigs[t]
	if !ok {
		cfg = types.ExtractionConfig{Name: "Default", PredicateGroup: []types.PredicateGroup{types.GroupAny}, EntryStrategy: types.EntryStrategyAll, MaxDepth: 7, Direction: types.EdgeDirectionForward}
	}
	switch t {
	case types.UMLSequence, types.UMLCommunication, types.UMLInteractionOverview, types.C4Dynamic:
		cfg.MaxDepth = opts.MaxDepth
	}
	if len(cfg.Views) == 0 {
		cfg.Views = append([]types.ViewTag(nil), types.AllViews...)
	}
	return cfg
}

// getEntryPoints discovers entry points for diagrams that need a starting
// node. Priority: explicit --entry, then nodes flagged gm:isEntrypoint, then
// nodes whose ID/name marks them as an entry (main), then handler/controller/
// api naming, then every matching node as a last-resort fallback.
func getEntryPoints(nodes map[string]*types.TTLNode, opts types.QueryOptions, defaultFilter func(*types.TTLNode) bool) []string {
	var startIDs []string
	if opts.EntryPointID != "" {
		return []string{opts.EntryPointID}
	}

	inScope := func(id string) bool {
		return opts.ScopePrefix == "" || strings.HasPrefix(id, opts.ScopePrefix)
	}
	seen := make(map[string]bool)
	add := func(id string) {
		if !seen[id] {
			seen[id] = true
			startIDs = append(startIDs, id)
		}
	}
	// Deterministic: sorted node IDs for each pass
	sortedIDs := make([]string, 0, len(nodes))
	for id := range nodes {
		sortedIDs = append(sortedIDs, id)
	}
	sort.Strings(sortedIDs)

	// Pass 1: explicit entry-point markers from the analyzer.
	for _, id := range sortedIDs {
		n := nodes[id]
		if defaultFilter(n) && n.IsEntrypoint && inScope(id) {
			add(id)
		}
	}
	if len(startIDs) > 0 {
		sort.Strings(startIDs)
		return startIDs
	}

	// Pass 2: canonical "main" entry points.
	for _, id := range sortedIDs {
		n := nodes[id]
		if defaultFilter(n) && inScope(id) {
			lower := strings.ToLower(id + " " + n.Name)
			if strings.Contains(lower, ".main") || strings.Contains(lower, "::main") || strings.HasSuffix(strings.ToLower(n.Name), " main") || strings.EqualFold(n.Name, "main") {
				add(id)
			}
		}
	}
	if len(startIDs) > 0 {
		sort.Strings(startIDs)
		return startIDs
	}

	// Pass 3: handler / controller / api naming.
	for _, id := range sortedIDs {
		n := nodes[id]
		if defaultFilter(n) && inScope(id) {
			lower := strings.ToLower(id + " " + n.Name)
			if strings.Contains(lower, "main") || strings.Contains(lower, "handler") || strings.Contains(lower, "controller") || strings.Contains(lower, "api") {
				add(id)
			}
		}
	}
	if len(startIDs) > 0 {
		sort.Strings(startIDs)
		return startIDs
	}

	// Pass 4: last-resort fallback to every matching node.
	for _, id := range sortedIDs {
		n := nodes[id]
		if defaultFilter(n) && inScope(id) {
			add(id)
		}
	}
	sort.Strings(startIDs)
	return startIDs
}

// filterNodes applies ScopePrefix and MaxNodes filters deterministically.
func filterNodes(nodes map[string]*types.TTLNode, opts types.QueryOptions, filter func(*types.TTLNode) bool) []string {
	var ids []string
	for id, n := range nodes {
		if filter(n) {
			if opts.ScopePrefix == "" || strings.HasPrefix(id, opts.ScopePrefix) {
				ids = append(ids, id)
			}
		}
	}
	sort.Strings(ids)
	if opts.MaxNodes > 0 && len(ids) > opts.MaxNodes {
		ids = ids[:opts.MaxNodes]
	}
	return ids
}

// ParseTTLFile parses a Turtle (.ttl) file at StatePath and returns extracted TTLNodes,
// TTLEdges, and any parse error. Use ParseTTLFileToNative for a NativeGraph result.
// Malformed statements are reported as a structured error (with line numbers)
// instead of being silently dropped (AUDIT Issue 2 Phase 2B-8).
func ParseTTLFile(StatePath string) (map[string]*types.TTLNode, []types.TTLEdge, error) {
	file, err := os.Open(StatePath)
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
		return nil, nil, fmt.Errorf("scanning %s: %w", StatePath, err)
	}

	// A trailing statement that never reached its terminator is a truncated
	// file (AUDIT Issue 5 finding 6): surface it instead of silently serving
	// a partial graph.
	if len(block) > 0 {
		issues = append(issues, ParseIssue{Line: blockStartLine, Msg: "unterminated statement block (truncated file?)"})
	}

	if len(issues) > 0 {
		return nil, nil, fmt.Errorf("%s: %d malformed statement(s): %w", StatePath, len(issues), issues[0])
	}

	parsedEdges := make([]types.TTLEdge, 0, len(edgeMap))
	keys := make([]string, 0, len(edgeMap))
	for k := range edgeMap {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		parsedEdges = append(parsedEdges, *edgeMap[k])
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
	if pred == ont.PredStatus {
		if parseLiteral(tgt) == "DELETED" {
			return nil
		}
		return fmt.Errorf("unexpected %s value in edge block: %.50s", ont.PredStatus, block)
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

	if propsPart != "" {
		attrs := strings.Split(propsPart, ";")
		for _, attr := range attrs {
			attr = strings.TrimSpace(attr)
			if attr == "" {
				continue
			}
			fields := strings.Fields(attr)
			if len(fields) < 2 {
				continue
			}
			pName := fields[0]
			pValue := strings.TrimSpace(strings.Join(fields[1:], " "))
			if pName == ont.PredLineNumber {
				if n, err := strconv.Atoi(pValue); err == nil {
					edge.LineNumber = n
				}
			}
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
		case ont.PredName:
			node.Name = parseLiteral(val)
		case ont.PredPrimitiveType:
			node.PrimitiveType = parseLiteral(val)
		case ont.PredBelongsToFile:
			node.FileURI = types.ParseNodeURI(val)
		case ont.PredLineStart:
			if n, err := strconv.Atoi(val); err == nil {
				node.LineStart = n
			}
		case ont.PredLineEnd:
			if n, err := strconv.Atoi(val); err == nil {
				node.LineEnd = n
			}
		case ont.PredContent, ont.PredCode:
			node.Code = parseLiteral(val)
		case ont.PredIsEntrypoint:
			if val == "true" {
				node.IsEntrypoint = true
			}
		case ont.PredPrimitiveZone:
			node.PrimitiveZone = parseLiteral(val)
		case ont.PredStatus:
			if parseLiteral(val) == "DELETED" {
				return
			}
		default:
			if node.Properties == nil {
				node.Properties = make(map[string]string)
			}
			node.Properties[strings.TrimPrefix(pred, ont.PrefixGM)] = parseLiteral(val)
		}
	}

	// A block typed gm:Deleted is a tombstone even without gm:status.
	if node.Kind == ont.PredDeleted {
		return
	}

	if node.ID != "" && node.Kind != "" {
		if strings.HasPrefix(node.ID, ont.PrefixExt) && node.Kind == "rdfs:Class" {
			node.Kind = ont.PredExternal
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
func ParseTTLFileToNative(StatePath string) (*types.NativeGraph, error) {
	nodes, edges, err := ParseTTLFile(StatePath)
	if err != nil {
		return nil, err
	}
	return &types.NativeGraph{Nodes: nodes, Edges: edges}, nil
}

// StreamTTLBlocks streams a TTL file into logical statement blocks in file
// order; it is the exported generic form of the streaming primitive behind
// the lazy query entry points (AUDIT Issue 4 Phase 4A-2).
func StreamTTLBlocks(StatePath string, handle func(block string) error) error {
	return scanTTLStream(StatePath, handle)
}

// scanTTLStream streams a TTL file into logical statement blocks (node
// blocks, base edge triples, reified edge-property triples) and invokes
// handle for every complete block in file order. A trailing unterminated
// block is a hard error (truncated file). It is the shared streaming
// primitive behind the lazy query entry points so the whole database is
// never materialized for a small read (AUDIT Issue 4 Phase 4A-2).
func scanTTLStream(StatePath string, handle func(block string) error) error {
	file, err := os.Open(StatePath)
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
		return fmt.Errorf("scanning %s: %w", StatePath, err)
	}
	if len(block) > 0 {
		return fmt.Errorf("%s: unterminated statement block at line %d (truncated file?)", StatePath, blockStartLine)
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
func ParseTTLFileToNativeScoped(StatePath, scopePath string) (*types.NativeGraph, error) {
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

	err := scanTTLStream(StatePath, func(block string) error {
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

	edgeKeys := make([]string, 0, len(edgeMap))
	for k := range edgeMap {
		edgeKeys = append(edgeKeys, k)
	}
	sort.Strings(edgeKeys)
	for _, k := range edgeKeys {
		graph.Edges = append(graph.Edges, *edgeMap[k])
	}
	return graph, nil
}

// matchesFileScope reproduces ApplyScope(ScopeFile)'s node predicate.
func matchesFileScope(n *types.NativeNode, id, scopePath, _ string) bool {
	if n != nil && isWithinFile(n.FileURI, scopePath) {
		return true
	}
	return isWithinFile(id, scopePath)
}

// parseBaseEdgeScoped parses a base edge triple, keeping it only when both
// endpoints belong to the scoped node set.
func parseBaseEdgeScoped(block string, edgeMap map[string]*types.NativeEdge, matched map[string]bool) error {
	parts := strings.Fields(strings.TrimSuffix(strings.TrimSpace(block), "."))
	if len(parts) < 3 {
		return fmt.Errorf("malformed edge block dropped (expected 3+ fields): %.50s", block)
	}
	if parts[1] == ont.PredStatus {
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
	if strings.HasPrefix(propsPart, ont.PredLineNumber) {
		if n, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(propsPart, ont.PredLineNumber))); err == nil {
			edge.LineNumber = n
		} else {
			return fmt.Errorf("bad %s value %q for edge %s: %v", ont.PredLineNumber, propsPart, key, err)
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
func ParseTTLNodeByID(StatePath, nodeID string) (*types.TTLNode, []types.TTLEdge, error) {
	var found *types.TTLNode
	edgeMap := make(map[string]*types.TTLEdge)

	err := scanTTLStream(StatePath, func(block string) error {
		trimmed := strings.TrimSpace(block)
		if strings.HasPrefix(trimmed, "<<") {
			// Bind an incident edge and line number from RDF-star statement
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
			if src == nodeID || tgt == nodeID {
				key := fmt.Sprintf("%s|%s|%s", src, fields[1], tgt)
				edge, ok := edgeMap[key]
				if !ok {
					edge = &types.TTLEdge{SourceID: src, Predicate: fields[1], TargetID: tgt}
					edgeMap[key] = edge
				}
				propsPart := strings.TrimSpace(block[endIdx+2:])
				if propsPart != "" {
					attrs := strings.Split(propsPart, ";")
					for _, attr := range attrs {
						attr = strings.TrimSpace(attr)
						attrFields := strings.Fields(attr)
						if len(attrFields) >= 2 && attrFields[0] == ont.PredLineNumber {
							if n, err := strconv.Atoi(attrFields[1]); err == nil {
								edge.LineNumber = n
							}
						}
					}
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
			if parts[1] == ont.PredStatus {
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
	keys := make([]string, 0, len(edgeMap))
	for k := range edgeMap {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		edges = append(edges, *edgeMap[k])
	}
	return found, edges, nil
}

// StreamTTLNodes streams every node block in the TTL and invokes fn for
// each parsed node; iteration stops when fn returns false. Edge blocks and
// the metadata node are skipped, so --list/--search style reads never
// materialize the graph (AUDIT Issue 4 Phase 4A-2).
func StreamTTLNodes(StatePath string, fn func(*types.TTLNode) bool) error {
	return scanTTLStream(StatePath, func(block string) error {
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

// normalizeScopePath converts a file path, FileURI or node ID into a slash-normalized relative path.
func normalizeScopePath(p string) string {
	res := p
	res = strings.TrimPrefix(res, "http://glassmarble.org/node/")
	res = strings.TrimPrefix(res, "http://glassmarble.org/file/")
	res = strings.TrimPrefix(res, "http://glassmarble.org/namespace/")
	res = strings.TrimPrefix(res, "file:")
	res = strings.TrimPrefix(res, "module:")
	res = strings.TrimPrefix(res, "virt:")
	res = strings.TrimPrefix(res, "./")
	if norm := ids.NormalizeLegacyID(res); norm != "" {
		if c, err := ids.ParseCanonicalID(norm); err == nil && c.Path != "" {
			res = c.Path
		} else {
			parts := strings.Split(res, "::")
			if len(parts) >= 2 {
				res = parts[0]
			}
		}
	}
	res = strings.ReplaceAll(res, "\\", "/")
	return strings.Trim(res, "/")
}

// isWithinFolder reports whether candidate lives inside the folder scope.
func isWithinFolder(candidate, scopePath string) bool {
	if candidate == "" || scopePath == "" {
		return false
	}
	c := normalizeScopePath(candidate)
	s := normalizeScopePath(scopePath)
	return c == s || strings.HasPrefix(c, s+"/")
}

// isWithinFile reports whether candidate names the scoped file (or a node inside it).
func isWithinFile(candidate, scopePath string) bool {
	if candidate == "" || scopePath == "" {
		return false
	}
	c := normalizeScopePath(candidate)
	s := normalizeScopePath(scopePath)
	return c == s || strings.HasSuffix(c, "/"+s)
}

// ApplyScope filters a VirtualSubgraph in-place to include only nodes matching the scope level and path (W5-01 / §9.1).
func ApplyScope(sub *types.VirtualSubgraph, opts types.QueryOptions) {
	if sub == nil || opts.Scope == types.ScopeGlobal || opts.ScopePath == "" {
		return
	}
	filteredNodes := make(map[string]*types.TTLNode)
	for id, n := range sub.Nodes {
		switch opts.Scope {
		case types.ScopeFolder:
			if isWithinFolder(n.FileURI, opts.ScopePath) || isWithinFolder(id, opts.ScopePath) {
				filteredNodes[id] = n
			}
		case types.ScopeFile:
			if isWithinFile(n.FileURI, opts.ScopePath) || isWithinFile(id, opts.ScopePath) {
				filteredNodes[id] = n
			}
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
