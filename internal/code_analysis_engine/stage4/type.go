package stage4

import (
	"fmt"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage3"
	"github.com/Syamchand123/GlassMarble/internal/product"
	"github.com/Syamchand123/GlassMarble/internal/product/ont"
)

// RelationshipType defines edge kinds in the Code Property Graph (CPG = AST + CFG + DFG + Call Graph).
// The constants are grouped into the four families of the edge taxonomy v2
// (master_overhaul_plan.md §4.2); a single producer pass per family emits
// them, and every family maps to a declared gm: view (structural, dynamic,
// security) enforced by internal/akg ontology conformance tests.
type RelationshipType string

const (
	// STRUCTURAL family — view: structural. Producer passes: builder
	// (ownership/containment), type_linker (hierarchy), member_linker
	// (membership). Emitted edges are type/member ownership and dependency
	// facts, not execution facts.
	EdgeContains   RelationshipType = "CONTAINS"
	EdgeBelongsTo  RelationshipType = "BELONGS_TO"
	EdgeDependsOn  RelationshipType = "DEPENDS_ON"
	EdgeImplements RelationshipType = "IMPLEMENTS"
	EdgeExtends    RelationshipType = "EXTENDS"
	EdgeMixes      RelationshipType = "MIXES"
	EdgeComposes   RelationshipType = "COMPOSES"
	EdgeHasField   RelationshipType = "HAS_FIELD"
	EdgeHasParam   RelationshipType = "HAS_PARAM"
	EdgeReturns    RelationshipType = "RETURNS"
	// EdgeHasReceiver: method -> owner type, the missing explicit ownership
	// edge (AUDIT A-16); serialized as gm:hasReceiver.
	EdgeHasReceiver RelationshipType = "HAS_RECEIVER"

	// BEHAVIORAL family — view: structural. Producer passes: call_linker,
	// concurrency_linker, event_linker, rpc_linker, ffi_linker, di_linker,
	// security_linker, semantic_linker. Emitted edges describe what a node
	// does at runtime (calls, messaging, I/O, lifecycle).
	EdgeCalls       RelationshipType = "CALLS"
	EdgeContextCall RelationshipType = "1CFA_CALL"
	// EdgeVirtualContext: VIRTUAL_CONTEXT specialization link
	// (contextNode → base function), serialized as gm:virtualContextLink
	// (W1-18/A-18 — previously mislabeled as INSTANTIATES_GENERIC;
	// gm:instantiatesGeneric is reserved for real generic instantiation).
	EdgeVirtualContext   RelationshipType = "VIRTUAL_CONTEXT_LINK"
	EdgeSpawnsConcurrent RelationshipType = "SPAWNS_CONCURRENT"
	EdgeDefers           RelationshipType = "CFG_DEFERS"
	EdgeCatches          RelationshipType = "CFG_CATCHES"
	EdgeThrows           RelationshipType = "THROWS"
	EdgeReferences       RelationshipType = "REFERENCES"
	EdgeInstantiates     RelationshipType = "INSTANTIATES_GENERIC"
	EdgeDispatchesEvent  RelationshipType = "DISPATCHES_EVENT"
	EdgePublishes        RelationshipType = "PUBLISHES_EVENT"
	EdgeSubscribes       RelationshipType = "SUBSCRIBES_EVENT"
	EdgeSendsTo          RelationshipType = "SENDS_MSG"
	EdgeReceivesFrom     RelationshipType = "RECEIVES_MSG"
	EdgeQueriesDB        RelationshipType = "QUERIES_DB"
	EdgeCallsCloudAPI    RelationshipType = "CALLS_CLOUD_API"
	EdgeExposesEndpoint  RelationshipType = "EXPOSES_ENDPOINT"
	EdgeFFICall          RelationshipType = "FFI_CALL"
	EdgeInjects          RelationshipType = "DI_INJECTS"
	EdgeConsumesResource RelationshipType = "CONSUMES_RESOURCE"
	EdgeMutatesGlobal    RelationshipType = "MUTATES_GLOBAL"
	// EdgeNetworkCall is a BEHAVIORAL family member (producer: rpc_linker)
	// that the §4.2 table does not enumerate; it is kept because rpc_linker
	// still emits it and Phase 0 must not change behavior.
	EdgeNetworkCall RelationshipType = "NETWORK_RPC_CALL"

	// DYNAMIC family — view: dynamic. Producer passes: cfg_linker,
	// dfg_linker, alias_linker, memory_linker, constraint_linker. Emitted
	// edges capture intra-function control and data movement. EdgeVulnerable
	// and EdgeQueriesDB also participate in the SECURITY family (§4.2).
	EdgeControlFlow       RelationshipType = "CFG_FLOW"
	EdgeConditionalBranch RelationshipType = "CFG_IF"
	EdgeLoopBranch        RelationshipType = "CFG_LOOP"
	EdgeSwitchBranch      RelationshipType = "CFG_SWITCH"
	EdgeConstraint        RelationshipType = "BRANCH_CONSTRAINT"
	EdgeDataFlow          RelationshipType = "DATA_FLOW"
	EdgePointsTo          RelationshipType = "POINTS_TO"
	EdgeHeapAlias         RelationshipType = "HEAP_ALIAS"
	EdgeAliases           RelationshipType = "ALIASES"
	EdgeAliasesType       RelationshipType = "ALIASES_TYPE"
	EdgeCyclic            RelationshipType = "CYCLIC_DEPENDENCY"
	EdgeVulnerable        RelationshipType = "VULNERABLE_TAINT"
	EdgeEscapesToHeap     RelationshipType = "ESCAPES_TO_HEAP"

	// SECURITY family — view: security. Producer pass: security_linker
	// (taint propagation into sinks). EdgeSecuritySink is the sink marker;
	// EdgeVulnerable (taint flow) and EdgeQueriesDB (when the query is a
	// sink) are shared with the DYNAMIC/BEHAVIORAL families.
	EdgeSecuritySink RelationshipType = "SECURITY_SINK"
)

// ViewOfEdgeType returns the gm: view tag an edge family belongs to
// (master_overhaul_plan.md §4.2/§4.3): one of "structural", "dynamic", or
// "security". Shared edges (EdgeVulnerable, EdgeQueriesDB) participate in
// two families; the serializer emits a single gm:view attribute per triple
// (K-01), so they keep their primary family view and security filtering is
// applied at extraction time. The returned value must match the gm:view
// vocabulary declared in the AKG ontology (internal/akg/ontology.ttl), the
// shared predicate vocabulary enforced by the ontology conformance tests.
func ViewOfEdgeType(et RelationshipType) string {
	switch et {
	case EdgeSecuritySink:
		return "security"
	case EdgeControlFlow, EdgeConditionalBranch, EdgeLoopBranch, EdgeSwitchBranch,
		EdgeConstraint, EdgeDataFlow, EdgePointsTo, EdgeHeapAlias, EdgeAliases,
		EdgeAliasesType, EdgeCyclic, EdgeVulnerable, EdgeEscapesToHeap:
		return "dynamic"
	default:
		return "structural"
	}
}

const (
	LevelArchitecture = "architecture"
	LevelStandard     = "standard"
	LevelFull         = "full"
)

// QueryFilter defines a flexible, composable filter for querying graph nodes.
// All non-zero/non-empty fields are AND-ed together.
type QueryFilter struct {
	Kind          string            // exact node kind match (empty = any)
	NameContains  string            // substring match on node Name
	NameRegex     string            // regex match on node Name (compiled internally)
	Primitive     string            // exact Primitive match
	Properties    map[string]string // all specified key=value pairs must match
	PropertyRegex map[string]string // property key -> regex pattern for value match
	MinEdges      int               // minimum total edge count (outbound + inbound)
	MaxEdges      int               // maximum total edge count (0 = no limit)
	Limit         int               // max results (0 = unlimited)
	Offset        int               // pagination offset
}

// LinkerConfig controls which linker passes execute and at what granularity.
type LinkerConfig struct {
	// DisabledPasses lists linker passes to skip entirely.
	// Pass names match the buffer indices used in linker.go:
	//   "type", "member", "interface", "cfg", "dfg", "callgraph", "concurrency",
	//   "filedeps", "semantics", "rpc", "constraints", "ffi",
	//   "eventsourcing", "di", "escape", "alias", "security"
	DisabledPasses []string `json:"disabled_passes,omitempty"`

	// LevelOfDetail controls CFG/DFG granularity:
	//   "architecture" — skip CFG and DFG entirely, only module/type/call/dep edges
	//   "standard"     — aggregate CFG per function (count branches, no per-branch nodes)
	//   "full"         — current behavior: per-branch CFG nodes, per-variable DFG nodes
	LevelOfDetail string `json:"level_of_detail,omitempty"`

	// MaxNodesPerFile limits CFG/DFG synthetic nodes per file (0 = unlimited)
	MaxNodesPerFile int `json:"max_nodes_per_file,omitempty"`

	// MacroInference controls AKG macro inference behavior:
	//   "disabled" — skip macro inference entirely
	//   "structural" — only run structurally-verified rules (require graph evidence)
	//   "all" — run all rules including name-based heuristics (default)
	MacroInference string `json:"macro_inference,omitempty"`

	// MaxTotalNodes limits total nodes in the CPG. If exceeded, analysis
	// prints a warning and proceeds with what was built (degraded mode).
	// 0 = unlimited.
	MaxTotalNodes int `json:"max_total_nodes,omitempty"`

	// AbortOnLimit when true causes Link() to return an error instead
	// of proceeding with a degraded graph.
	AbortOnLimit bool `json:"abort_on_limit,omitempty"`

	// DisabledRules lists macro-inference rule IDs to skip (e.g. ["rule_05", "rule_13"])
	DisabledRules []string `json:"disabled_rules,omitempty"`

	// OwnershipMap is a shared read-only ownership index built once per Link()
	// call and reused by all linkers. Previously each linker rebuilt it
	// independently (AUDIT Issue 1.7 / Phase 1C-8).
	OwnershipMap *stage3.OwnershipMap `json:"-"`
}

// Stage4Output is the final, completely bound code compilation payload (Complete Code Property Graph).
type Stage4Output struct {
	CommitHash string `json:"commit_hash"`

	// GraphNodes maps a system-wide distinct signature (Universal FQN) to its metadata block
	// e.g., "src/core/database.go::PostgresStore::Save" -> ResolvedNode
	GraphNodes map[string]*ResolvedNode `json:"graph_nodes"`

	// OutboundEdges acts as a direct adjacency map for fast forward crawling (Sequence/Call/CFG/DFG graph)
	// Key: Source Node ID -> Value: Array of connected relationships
	OutboundEdges map[string][]ResolvedEdge `json:"outbound_edges"`

	// InboundEdges acts as a reverse index lookup for instantly finding callers (Impact Analysis / Taint Lineage)
	// Key: Target Node ID -> Value: Array of incoming relationships
	InboundEdges map[string][]ResolvedEdge `json:"inbound_edges"`

	// Stage 3.8: Root Execution Nodes (Endpoints, Mains)
	EntrypointRegistry []string `json:"entrypoints"`

	// Stage 3.9: Architectural Folder Zones (e.g., src/database -> DATABASE_ZONE)
	FolderZones map[string]string `json:"folder_zones"`

	// DB is an un-serialized reference to the global AKG for incremental delta linking
	db GraphDB `json:"-"`

	// ModifiedFiles is an un-serialized set of files being processed in the current Delta.
	// Linkers must use this to guarantee O(1) scaling by skipping unmodified files.
	ModifiedFiles map[string]bool `json:"-"`

	// Config is the LinkerConfig passed to Link(). It is un-serialized and propagated
	// to each buffer so that individual linkers can adjust granularity.
	Config LinkerConfig `json:"-"`

	// baseNodes is a shared read-only reference to the initial node set.
	// Each linker pass has its own private GraphNodes map for writes; reads
	// fall through to baseNodes via GetNode/NodeExists. This eliminates the
	// N full map copies that the buffer architecture previously required.
	baseNodes map[string]*ResolvedNode `json:"-"`

	// edgeSet deduplicates edges by (source, target, type, line) so AddEdge
	// never scans the outbound slice linearly (AUDIT Issue 1.7 / Phase 1C-8).
	edgeSet map[string]struct{} `json:"-"`

	// typeNameIndex caches the name → node-ID map for STRUCT/CLASS/INTERFACE
	// nodes, built once per pass (W1-12 / A-15: no linear GraphNodes scans
	// in type resolution).
	typeNameIndex map[string]string `json:"-"`
}

// isFullMode reports whether the current linker configuration runs the
// per-statement/heuristic passes (constraints, alias, escape analysis,
// 1-CFA contexts). Empty config defaults to full for backwards
// compatibility with tests and existing callers.
func isFullMode(cpg *Stage4Output) bool {
	level := cpg.Config.LevelOfDetail
	return level == "" || level == LevelFull
}

// ownershipMap returns the shared ownership index built once by Link(),
// falling back to a lazy build for direct linker calls (tests).
func ownershipMap(cpg *Stage4Output, stage3Out *stage3.Stage3Output) *stage3.OwnershipMap {
	if cpg != nil && cpg.Config.OwnershipMap != nil {
		return cpg.Config.OwnershipMap
	}
	if stage3Out == nil {
		return nil
	}
	return stage3.BuildOwnershipMap(stage3Out.GlobalDefinitionIndex, stage3Out.WorkspaceCtx)
}

// SetDB attaches the read-only global graph database for incremental lookups.
func (s *Stage4Output) SetDB(db GraphDB) {
	s.db = db
}

// GetNode resolves a node by checking local delta, shared base, then global DB.
func (s *Stage4Output) GetNode(id string) (*ResolvedNode, bool) {
	if n, ok := s.GraphNodes[id]; ok {
		return n, true
	}
	if s.baseNodes != nil {
		if n, ok := s.baseNodes[id]; ok {
			return n, true
		}
	}
	if s.db != nil {
		return s.db.GetNode(id)
	}
	return nil, false
}

// NodeExists checks whether a node with the given ID exists in the local delta,
// shared base, or global DB. Use for read/lookup decisions only (e.g. whether
// an edge target may already exist in an unmodified file).
func (s *Stage4Output) NodeExists(id string) bool {
	if _, ok := s.GraphNodes[id]; ok {
		return true
	}
	if s.baseNodes != nil {
		if _, ok := s.baseNodes[id]; ok {
			return true
		}
	}
	if s.db != nil {
		_, ok := s.db.GetNode(id)
		return ok
	}
	return false
}

// HasNode checks whether a node ID exists in the current delta payload
// (GraphNodes plus the builder's shared base set). Unlike NodeExists it never
// consults the persisted graph: every node a delta emits belongs to a file
// that the commit will sweep and re-add, so skip decisions must be based on
// the delta alone — a base-graph hit would silently lose the node when the
// sweep removes all nodes of modified files.
func (s *Stage4Output) HasNode(id string) bool {
	if _, ok := s.GraphNodes[id]; ok {
		return true
	}
	if s.baseNodes != nil {
		_, ok := s.baseNodes[id]
		return ok
	}
	return false
}

// nameToNodeID returns the type-name → node-ID index for
// STRUCT/CLASS/INTERFACE nodes (W1-12 / A-15: exact-map resolution instead
// of linear GraphNodes scans). Built lazily once per pass; the delta map
// shadows the shared base on name collisions.
func (s *Stage4Output) nameToNodeID() map[string]string {
	if s.typeNameIndex != nil {
		return s.typeNameIndex
	}
	idx := make(map[string]string)
	add := func(nodes map[string]*ResolvedNode) {
		for id, n := range nodes {
			if n.Kind == "STRUCT" || n.Kind == "CLASS" || n.Kind == "INTERFACE" {
				if _, dup := idx[n.Name]; !dup {
					idx[n.Name] = id
				}
			}
		}
	}
	add(s.GraphNodes)
	if s.baseNodes != nil {
		add(s.baseNodes)
	}
	s.typeNameIndex = idx
	return idx
}

// GraphDB is a read-only interface providing access to the globally persisted Code Property Graph (AKG).
// It enables the Stage 4 Incremental Delta Linker to lookup existing symbols in unmodified files.
type GraphDB interface {
	GetNode(id string) (*ResolvedNode, bool)
	GetOutboundEdges(id string) []ResolvedEdge
	GetNodesByKind(kind string) []*ResolvedNode
	GetInboundEdges(id string) []ResolvedEdge
	Query(filter QueryFilter) []*ResolvedNode
	GetNodesByPattern(predicate RelationshipType, objectID string) []string
}

// ResolvedNode represents a vertex in the Code Property Graph (CPG).
type ResolvedNode struct {
	ID              string             `json:"id"`                         // Universal Signature ID (e.g. "path::Struct::Method")
	Kind            string             `json:"kind"`                       // "MODULE", "STRUCT", "INTERFACE", "FUNCTION", "METHOD", "CFG_BRANCH", "DFG_VAR"
	Name            string             `json:"name"`                       // Literal code name
	Primitive       string             `json:"primitive,omitempty"`        // "DISK_IO", "NETWORK_IO", "COMPUTE", "CONCURRENCY", "DATABASE"
	PrimitiveScores map[string]float64 `json:"primitive_scores,omitempty"` // Taint attenuation distance scores
	FileSpec        LocationMeta       `json:"file_spec"`                  // Exact file path and physical coordinates
	Properties      map[string]string  `json:"properties,omitempty"`       // Extra metadata
}

// LocationMeta defines physical file coordinates.
type LocationMeta struct {
	Path      string `json:"path"`
	LineStart int    `json:"line_start"`
	LineEnd   int    `json:"line_end"`
}

// ResolvedEdge represents a directional relationship between two nodes in the CPG.
type ResolvedEdge struct {
	SourceID   string           `json:"source_id"`            // Originating node signature
	TargetID   string           `json:"target_id"`            // Destination node signature
	Type       RelationshipType `json:"type"`                 // "CALLS", "IMPLEMENTS", "COMPOSES", "CFG_IF", "DATA_FLOW", etc.
	LineNumber int              `json:"line_number"`          // Precise file coordinate where the relation happens
	Confidence float32          `json:"confidence,omitempty"` // 1.0 = FQN match, 0.7 = import-resolved, 0.5 = same-package, 0.3 = heuristic
	IsCycle    bool             `json:"is_cycle,omitempty"`   // True if this edge creates an architectural cycle
	// Properties carries typed edge facts (v2, W1-11): gm:embedding,
	// gm:provenance (W1-14), etc. Serialized as edge attributes.
	Properties map[string]string `json:"properties,omitempty"`
}

// NewStage4Output instantiates a fresh Stage4Output structure.
func NewStage4Output(commitHash string) *Stage4Output {
	return &Stage4Output{
		CommitHash:         commitHash,
		GraphNodes:         make(map[string]*ResolvedNode),
		OutboundEdges:      make(map[string][]ResolvedEdge),
		InboundEdges:       make(map[string][]ResolvedEdge),
		EntrypointRegistry: make([]string, 0),
		FolderZones:        make(map[string]string),
	}
}

// AddEdge registers a directional edge in both OutboundEdges and InboundEdges maps.
func (s *Stage4Output) AddEdge(sourceID, targetID string, edgeType RelationshipType, lineNo int) {
	if sourceID == "" || targetID == "" || sourceID == targetID {
		return
	}

	edge := ResolvedEdge{
		SourceID:   sourceID,
		TargetID:   targetID,
		Type:       edgeType,
		LineNumber: lineNo,
	}

	if !s.registerEdge(edge) {
		return
	}

	s.OutboundEdges[sourceID] = append(s.OutboundEdges[sourceID], edge)
	s.InboundEdges[targetID] = append(s.InboundEdges[targetID], edge)
}

// AddEdgeProperties registers a directional edge with confidence and typed
// edge facts (v2, W1-11): gm:embedding "true" for Go embedding,
// gm:provenance (W1-14), etc.
func (s *Stage4Output) AddEdgeProperties(sourceID, targetID string, edgeType RelationshipType, lineNo int, confidence float32, props map[string]string) {
	if sourceID == "" || targetID == "" || sourceID == targetID {
		return
	}

	edge := ResolvedEdge{
		SourceID:   sourceID,
		TargetID:   targetID,
		Type:       edgeType,
		LineNumber: lineNo,
		Confidence: confidence,
		Properties: props,
	}

	if !s.registerEdge(edge) {
		return
	}

	s.OutboundEdges[sourceID] = append(s.OutboundEdges[sourceID], edge)
	s.InboundEdges[targetID] = append(s.InboundEdges[targetID], edge)
}

// registerEdge deduplicates identical (source, target, type, line) edges via a
// map — O(1) instead of the previous linear scan per insert.
func (s *Stage4Output) registerEdge(edge ResolvedEdge) bool {
	key := edge.SourceID + "\x00" + edge.TargetID + "\x00" + string(edge.Type) + "\x00" + fmt.Sprintf("%d", edge.LineNumber)
	if len(edge.Properties) > 0 {
		var sb strings.Builder
		for _, k := range []string{ont.PredEmbedding, ont.PredProvenance} {
			if v, ok := edge.Properties[k]; ok {
				sb.WriteString("\x00")
				sb.WriteString(k)
				sb.WriteString("=")
				sb.WriteString(v)
			}
		}
		key += sb.String()
	}
	if s.edgeSet == nil {
		s.edgeSet = make(map[string]struct{})
	}
	if _, exists := s.edgeSet[key]; exists {
		return false
	}
	s.edgeSet[key] = struct{}{}
	return true
}

// ensureVirtualNode creates a synthetic ResolvedNode if one does not already exist with the given ID.
// This is used by linkers that create heuristic/informational edges to logical categories (databases,
// queues, endpoints, sinks, etc.) rather than actual code symbols.
func ensureVirtualNode(id, kind, name string, cpg *Stage4Output) {
	if _, exists := cpg.GraphNodes[id]; !exists {
		cpg.GraphNodes[id] = &ResolvedNode{
			ID:   product.InternString(id),
			Kind: product.InternString(kind),
			Name: product.InternString(name),
		}
	}
}

// AddEdgeWithConfidence registers a directional edge with an explicit confidence rating (0.0 to 1.0).
func (s *Stage4Output) AddEdgeWithConfidence(sourceID, targetID string, edgeType RelationshipType, lineNo int, confidence float32) {
	if sourceID == "" || targetID == "" || sourceID == targetID {
		return
	}

	edge := ResolvedEdge{
		SourceID:   sourceID,
		TargetID:   targetID,
		Type:       edgeType,
		LineNumber: lineNo,
		Confidence: confidence,
	}

	if !s.registerEdge(edge) {
		return
	}

	s.OutboundEdges[sourceID] = append(s.OutboundEdges[sourceID], edge)
	s.InboundEdges[targetID] = append(s.InboundEdges[targetID], edge)
}

// touchesFile reports whether an edge's source or target node lives in the
// given file (master_overhaul_plan.md §9.1 file scoping, W1-10/A-12).
// Resolution order: node FileSpec.Path, then Properties["file_path"]
// (nodes built by heuristic linkers may only carry the property). File
// paths are compared normalized (slash-separated). The node lookup goes
// through GetNode so delta/base/db layers are all honored.
func touchesFile(edge ResolvedEdge, cpg *Stage4Output, filePath string) bool {
	norm := stage3.NormalizeRelativePath(filePath)

	check := func(id string) bool {
		if id == "" {
			return false
		}
		n, ok := cpg.GetNode(id)
		if !ok {
			return false
		}
		if n.FileSpec.Path != "" && stage3.NormalizeRelativePath(n.FileSpec.Path) == norm {
			return true
		}
		if fp := n.Properties["file_path"]; fp != "" && stage3.NormalizeRelativePath(fp) == norm {
			return true
		}
		return false
	}

	return check(edge.SourceID) || check(edge.TargetID)
}

// RemoveNode deletes a node and every incident edge from the adjacency
// maps (W1-17/A-14: orphan synthetic nodes are dropped when they cannot be
// scoped to any file).
func (s *Stage4Output) RemoveNode(id string) {
	if s == nil {
		return
	}
	delete(s.GraphNodes, id)

	dropEdge := func(edges []ResolvedEdge) []ResolvedEdge {
		out := edges[:0]
		for _, e := range edges {
			if e.SourceID == id || e.TargetID == id {
				continue
			}
			out = append(out, e)
		}
		return out
	}
	delete(s.OutboundEdges, id)
	for src, edges := range s.OutboundEdges {
		if cleaned := dropEdge(edges); len(cleaned) != len(edges) {
			s.OutboundEdges[src] = cleaned
		}
	}
	delete(s.InboundEdges, id)
	for dst, edges := range s.InboundEdges {
		if cleaned := dropEdge(edges); len(cleaned) != len(edges) {
			s.InboundEdges[dst] = cleaned
		}
	}
}
