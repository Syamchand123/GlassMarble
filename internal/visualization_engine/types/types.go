package types

import (
	"fmt"
	"strings"
)

type DiagramType string

const (
	UMLClass               DiagramType = "UML_CLASS"
	UMLObject              DiagramType = "UML_OBJECT"
	UMLComponent           DiagramType = "UML_COMPONENT"
	UMLDeployment          DiagramType = "UML_DEPLOYMENT"
	UMLPackage             DiagramType = "UML_PACKAGE"
	UMLComposite           DiagramType = "UML_COMPOSITE"
	UMLProfile             DiagramType = "UML_PROFILE"
	UMLUsecase             DiagramType = "UML_USECASE"
	UMLActivity            DiagramType = "UML_ACTIVITY"
	UMLState               DiagramType = "UML_STATE"
	UMLSequence            DiagramType = "UML_SEQUENCE"
	UMLCommunication       DiagramType = "UML_COMMUNICATION"
	UMLInteractionOverview DiagramType = "UML_INTERACTION_OVERVIEW"
	UMLTiming              DiagramType = "UML_TIMING"
	C4Context              DiagramType = "C4_CONTEXT"
	C4Container            DiagramType = "C4_CONTAINER"
	C4Component            DiagramType = "C4_COMPONENT"
	C4Code                 DiagramType = "C4_CODE"
	C4Landscape            DiagramType = "C4_LANDSCAPE"
	C4Dynamic              DiagramType = "C4_DYNAMIC"
	C4Deployment           DiagramType = "C4_DEPLOYMENT"
	DataFlow               DiagramType = "DATA_FLOW"
	ERDiagram              DiagramType = "ER_DIAGRAM"
	Mindmap                DiagramType = "MINDMAP"
	Flowchart              DiagramType = "FLOWCHART"
	DependencyGraph        DiagramType = "DEPENDENCY_GRAPH"
	HotspotComplexity      DiagramType = "HOTSPOT_COMPLEXITY"
	CallGraph              DiagramType = "CALL_GRAPH"
	LayeredArchitecture    DiagramType = "LAYERED_ARCHITECTURE"
	ChangeImpact           DiagramType = "CHANGE_IMPACT"
	Infrastructure         DiagramType = "INFRASTRUCTURE"
)

// AllDiagramTypes returns a slice of all 31 supported diagram types.
func AllDiagramTypes() []DiagramType {
	return []DiagramType{
		UMLClass, UMLObject, UMLComponent, UMLDeployment, UMLPackage,
		UMLComposite, UMLProfile, UMLUsecase, UMLActivity, UMLState,
		UMLSequence, UMLCommunication, UMLInteractionOverview, UMLTiming,
		C4Context, C4Container, C4Component, C4Code, C4Landscape,
		C4Dynamic, C4Deployment, DataFlow, ERDiagram, Mindmap,
		Flowchart, DependencyGraph, HotspotComplexity, CallGraph,
		LayeredArchitecture, ChangeImpact, Infrastructure,
	}
}

type QueryOptions struct {
	EntryPointID  string
	MaxDepth      int
	IncludeUnused bool
	ScopePrefix   string
	MaxNodes      int
	DiagramFocus  string
	DiagramType   DiagramType
	Format        string
	ChangedFiles  []string
	OnProgress    func(stage, detail string)
	OnSummary     func(summary *GraphSummary)
	Scope         ScopeLevel
	ScopePath     string
	RelativePath  bool
	PipelineCfg   *PipelineConfig
	// LinkLevel is the graph linkage detail level ("architecture" |
	// "standard" | "full") requested by the caller. The TUI path forwards
	// it into the unified pipeline so interactive diagrams honor the
	// --link-level flag (GAP-H-05).
	LinkLevel string
}

type ScopeLevel int

const (
	ScopeGlobal ScopeLevel = iota
	ScopeFolder
	ScopeFile
)

type NativeGraph struct {
	Nodes map[string]*NativeNode
	Edges []NativeEdge
}

// TTLNode is an alias for NativeNode — kept for backward compatibility.
type TTLNode = NativeNode

// TTLEdge is an alias for NativeEdge — kept for backward compatibility.
type TTLEdge = NativeEdge

// VirtualSubgraph is an alias for NativeGraph — kept for backward compatibility.
type VirtualSubgraph = NativeGraph

type NativeNode struct {
	ID            string
	Kind          string
	Name          string
	PrimitiveType string
	FileURI       string
	LineStart     int
	LineEnd       int
	Code          string
	IsEntrypoint  bool
	PrimitiveZone string
	Properties    map[string]string
}

// Clone returns a deep copy of the graph. Scoping and extraction must never
// mutate a graph that may be shared (e.g. a cached parse result).
func (g *NativeGraph) Clone() *NativeGraph {
	if g == nil {
		return nil
	}
	out := &NativeGraph{
		Nodes: make(map[string]*NativeNode, len(g.Nodes)),
		Edges: make([]NativeEdge, len(g.Edges)),
	}
	for id, n := range g.Nodes {
		cp := *n
		if n.Properties != nil {
			cp.Properties = make(map[string]string, len(n.Properties))
			for k, v := range n.Properties {
				cp.Properties[k] = v
			}
		}
		out.Nodes[id] = &cp
	}
	copy(out.Edges, g.Edges)
	return out
}

type NativeEdge struct {
	SourceID   string
	Predicate  string
	TargetID   string
	LineNumber int
}

type PredicateGroup int

const (
	GroupCallGraph PredicateGroup = iota
	GroupTypeHierarchy
	GroupComposition
	GroupDataFlow
	GroupControlFlow
	GroupStructural
	GroupMessaging
	GroupInfrastructure
	GroupSecurity
	GroupBinding
	GroupAny
)

type EdgeDirection int

const (
	EdgeDirectionForward EdgeDirection = iota
	EdgeDirectionReverse
	EdgeDirectionBoth
)

// ViewTag identifies a gm: view of the graph (master_overhaul_plan.md §4.3).
// Every serializer-emitted triple carries one view attribute, and extraction
// configs select predicates by view first, then predicate group.
type ViewTag string

const (
	// ViewStructural covers type/member ownership and runtime behavior
	// (STRUCTURAL + BEHAVIORAL edge families of §4.2).
	ViewStructural ViewTag = "structural"
	// ViewDynamic covers intra-function control and data movement
	// (DYNAMIC edge family).
	ViewDynamic ViewTag = "dynamic"
	// ViewSecurity covers taint propagation and sinks (SECURITY edge family).
	ViewSecurity ViewTag = "security"
)

// AllViews lists every declared view tag.
var AllViews = []ViewTag{ViewStructural, ViewDynamic, ViewSecurity}

// ContainsView reports whether views contains the given tag.
func ContainsView(views []ViewTag, v ViewTag) bool {
	for _, w := range views {
		if w == v {
			return true
		}
	}
	return false
}

type EntryStrategy int

const (
	EntryStrategyAuto EntryStrategy = iota
	EntryStrategyEntryPoint
	EntryStrategyAll
	EntryStrategyChangedFiles
)

// ExtractionConfig defines how nodes and edges are selected from the full graph for a given diagram type.
type ExtractionConfig struct {
	Name           string
	NodeKindFilter []string
	PredicateGroup []PredicateGroup
	EntryStrategy  EntryStrategy
	MaxDepth       int
	Direction      EdgeDirection
	IncludeUnused  bool
	// Views lists the gm: views the diagram consumes; edges are filtered by
	// view before predicate group (§4.3). Empty means all views.
	Views []ViewTag
}

type LayoutNode struct {
	ID            string
	Kind          string
	Name          string
	PrimitiveType string
	LineStart     int
	LineEnd       int
	Code          string
	IsEntrypoint  bool
	PrimitiveZone string
	PageRank      float64
	Betweenness   float64
	Community     string
	InDegree      int
	OutDegree     int
	IsHotspot     bool
	IsBottleneck  bool
	IsGodObject   bool
	// Visibility carries the explicit visibility token (public/private/...)
	// from GASTNode.Visibility when the source graph provides it, so
	// renderers can emit UML visibility markers instead of ASCII-case
	// heuristics (GAP-L-02).
	Visibility string
}

type LayoutEdge struct {
	SourceID   string
	TargetID   string
	Predicate  string
	LineNumber int
	Weight     int
	IsCycle    bool
}

type LayoutTree struct {
	BoundaryName string
	Children     []*LayoutTree
	Nodes        []*LayoutNode
	Edges        []LayoutEdge
	Summary      *GraphSummary
}

type PipelineConfig struct {
	DiagramType       DiagramType
	Scope             ScopeLevel
	ScopePath         string
	Format            string
	EnableMetrics     bool
	EnableCommunities bool
	EnableSCC         bool
	MaxNodes          int
	MaxDepth          int
}

type PipelineStage int

const (
	StageParse PipelineStage = iota
	StageScope
	StageExtract
	StageMetrics
	StageCluster
	StageLayout
	StageRender
)

type GraphSummary struct {
	NodeCount           int     `json:"node_count"`
	EdgeCount           int     `json:"edge_count"`
	Density             float64 `json:"density"`
	Diameter            int     `json:"diameter"`
	AvgPathLength       float64 `json:"avg_path_length"`
	ClusterCount        int     `json:"cluster_count"`
	SCCCount            int     `json:"scc_count,omitempty"`
	LargestSCCSize      int     `json:"largest_scc_size"`
	GodObjectCount      int     `json:"god_object_count"`
	BipartiteScore      float64 `json:"bipartite_score"`
	ConnectedComponents int     `json:"connected_components"`
	WeakComponents      int     `json:"weak_components,omitempty"`
	Truncated           bool    `json:"truncated,omitempty"`
}

// FormatNodeURI converts a bare node ID into a fully-qualified GlassMarble IRI string,
// encoding special characters and choosing the correct namespace (file, module, or node).
// The encoding is reversible: ParseNodeURI decodes every escape produced here,
// including a literal '%' (encoded as %25).
func FormatNodeURI(id string) string {
	clean := strings.ReplaceAll(id, "%", "%25")
	// Backslashes are encoded, not rewritten to '/', so the encoding is an
	// exact inverse of ParseNodeURI's percentDecode: a Windows path or a
	// source snippet containing '\' survives Format->Parse unchanged
	// (AUDIT Issue 2 Phase 2B-6 round-trip invariant).
	clean = strings.ReplaceAll(clean, "\\", "%5C")
	clean = strings.ReplaceAll(clean, "\"", "%22")
	clean = strings.ReplaceAll(clean, " ", "%20")
	clean = strings.ReplaceAll(clean, "<", "%3C")
	clean = strings.ReplaceAll(clean, ">", "%3E")
	clean = strings.ReplaceAll(clean, "`", "%60")
	// Issue 3 Phase 3D-14: `{ } | ^` and control characters break the
	// line-based TTL block parser when node IDs embed source snippets
	// (virtual nodes). Escape them symmetrically with ParseNodeURI.
	clean = strings.ReplaceAll(clean, "{", "%7B")
	clean = strings.ReplaceAll(clean, "}", "%7D")
	clean = strings.ReplaceAll(clean, "|", "%7C")
	clean = strings.ReplaceAll(clean, "^", "%5E")
	clean = strings.ReplaceAll(clean, "\r", "%0D")
	clean = strings.ReplaceAll(clean, "\n", "%0A")
	clean = strings.ReplaceAll(clean, "\t", "%09")

	if strings.HasPrefix(clean, "file:") {
		return fmt.Sprintf("<http://glassmarble.org/file/%s>", clean[5:])
	}
	if strings.HasPrefix(clean, "module:") {
		return fmt.Sprintf("<http://glassmarble.org/namespace/%s>", clean[7:])
	}
	return fmt.Sprintf("<http://glassmarble.org/node/%s>", clean)
}

// ParseNodeURI strips the GlassMarble IRI prefix from a URI and returns the bare node ID.
// Supports glassmarble.org/node/, /file/, and /namespace/ namespaces, as well as plain
// angle-bracket IRIs. Percent-escapes produced by FormatNodeURI are decoded (%25, %22,
// %20, %3C, %3E, %60 and any other %XX sequence).
func ParseNodeURI(uri string) string {
	uri = strings.TrimSpace(uri)
	var bare string
	switch {
	case strings.HasPrefix(uri, "<http://glassmarble.org/node/") && strings.HasSuffix(uri, ">"):
		bare = uri[len("<http://glassmarble.org/node/") : len(uri)-1]
	case strings.HasPrefix(uri, "<http://glassmarble.org/file/") && strings.HasSuffix(uri, ">"):
		bare = "file:" + uri[len("<http://glassmarble.org/file/"):len(uri)-1]
	case strings.HasPrefix(uri, "<http://glassmarble.org/namespace/") && strings.HasSuffix(uri, ">"):
		bare = "module:" + uri[len("<http://glassmarble.org/namespace/"):len(uri)-1]
	case strings.HasPrefix(uri, "<") && strings.HasSuffix(uri, ">"):
		bare = uri[1 : len(uri)-1]
	default:
		return uri
	}
	return percentDecode(bare)
}

// percentDecode decodes %XX escapes. Invalid sequences are left verbatim.
func percentDecode(s string) string {
	if !strings.Contains(s, "%") {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+2 < len(s) {
			if c, ok := decodeHexByte(s[i+1], s[i+2]); ok {
				b.WriteByte(c)
				i += 2
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func decodeHexByte(hi, lo byte) (byte, bool) {
	h, ok1 := hexVal(hi)
	l, ok2 := hexVal(lo)
	if !ok1 || !ok2 {
		return 0, false
	}
	return h<<4 | l, true
}

func hexVal(c byte) (byte, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	}
	return 0, false
}
