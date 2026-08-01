package stage4

import (
	"fmt"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage3"
)

// RelationshipType defines edge kinds in the Code Property Graph (CPG = AST + CFG + DFG + Call Graph).
type RelationshipType string

const (
	// Call Graph Edges
	EdgeCalls            RelationshipType = "CALLS"
	EdgeImplements       RelationshipType = "IMPLEMENTS"
	EdgeExtends          RelationshipType = "EXTENDS"
	EdgeMixes            RelationshipType = "MIXES"
	EdgeHasField         RelationshipType = "HAS_FIELD"
	EdgeHasParam         RelationshipType = "HAS_PARAM"
	EdgeReturns          RelationshipType = "RETURNS"
	EdgeThrows           RelationshipType = "THROWS"
	EdgeDependsOn        RelationshipType = "DEPENDS_ON"
	EdgeComposes         RelationshipType = "COMPOSES"
	EdgeReferences       RelationshipType = "REFERENCES"
	EdgeSpawnsConcurrent RelationshipType = "SPAWNS_CONCURRENT"
	EdgeDispatchesEvent  RelationshipType = "DISPATCHES_EVENT"
	EdgeExposesEndpoint  RelationshipType = "EXPOSES_ENDPOINT"
	EdgeSecuritySink     RelationshipType = "SECURITY_SINK"
	EdgeConsumesResource RelationshipType = "CONSUMES_RESOURCE"
	EdgeMutatesGlobal    RelationshipType = "MUTATES_GLOBAL"
	EdgeAliasesType      RelationshipType = "ALIASES_TYPE"
	EdgeContains         RelationshipType = "CONTAINS"

	// Control Flow Graph (CFG) Edges
	EdgeControlFlow       RelationshipType = "CFG_FLOW"
	EdgeConditionalBranch RelationshipType = "CFG_IF"
	EdgeLoopBranch        RelationshipType = "CFG_LOOP"
	EdgeSwitchBranch      RelationshipType = "CFG_SWITCH"
	EdgeCatches           RelationshipType = "CFG_CATCHES"
	EdgeDefers            RelationshipType = "CFG_DEFERS"

	// Data Flow Graph (DFG) Edges
	EdgeDataFlow   RelationshipType = "DATA_FLOW"
	EdgeAliases    RelationshipType = "ALIASES"
	EdgeVulnerable RelationshipType = "VULNERABLE_TAINT"

	// Architecture & IPC
	EdgeInstantiates  RelationshipType = "INSTANTIATES_GENERIC"
	EdgeSendsTo       RelationshipType = "SENDS_MSG"
	EdgeReceivesFrom  RelationshipType = "RECEIVES_MSG"
	EdgeCyclic        RelationshipType = "CYCLIC_DEPENDENCY"
	EdgeNetworkCall   RelationshipType = "NETWORK_RPC_CALL"
	EdgeQueriesDB     RelationshipType = "QUERIES_DB"
	EdgeCallsCloudAPI RelationshipType = "CALLS_CLOUD_API"

	// Phase 2 Enterprise Edges
	EdgeContextCall   RelationshipType = "1CFA_CALL"
	EdgePointsTo      RelationshipType = "POINTS_TO"
	EdgeHeapAlias     RelationshipType = "HEAP_ALIAS"
	EdgeConstraint    RelationshipType = "BRANCH_CONSTRAINT"
	EdgeFFICall       RelationshipType = "FFI_CALL"
	EdgePublishes     RelationshipType = "PUBLISHES_EVENT"
	EdgeSubscribes    RelationshipType = "SUBSCRIBES_EVENT"
	EdgeInjects       RelationshipType = "DI_INJECTS"
	EdgeEscapesToHeap RelationshipType = "ESCAPES_TO_HEAP"
	EdgeBelongsTo     RelationshipType = "BELONGS_TO"
)

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
	//   "type", "interface", "cfg", "dfg", "callgraph", "concurrency",
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
// shared base, or global DB. Used by linkers to avoid recreating summary nodes.
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

// registerEdge deduplicates identical (source, target, type, line) edges via a
// map — O(1) instead of the previous linear scan per insert.
func (s *Stage4Output) registerEdge(edge ResolvedEdge) bool {
	key := edge.SourceID + "\x00" + edge.TargetID + "\x00" + string(edge.Type) + "\x00" + fmt.Sprintf("%d", edge.LineNumber)
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
			ID:   id,
			Kind: kind,
			Name: name,
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
