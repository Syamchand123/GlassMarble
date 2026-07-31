package types

import (
	"fmt"
	"strings"
)

type DiagramType string

const (
	UMLClass      DiagramType = "UML_CLASS"
	UMLObject     DiagramType = "UML_OBJECT"
	UMLComponent  DiagramType = "UML_COMPONENT"
	UMLDeployment DiagramType = "UML_DEPLOYMENT"
	UMLPackage    DiagramType = "UML_PACKAGE"
	UMLComposite  DiagramType = "UML_COMPOSITE"
	UMLProfile    DiagramType = "UML_PROFILE"
	UMLUsecase             DiagramType = "UML_USECASE"
	UMLActivity            DiagramType = "UML_ACTIVITY"
	UMLState               DiagramType = "UML_STATE"
	UMLSequence            DiagramType = "UML_SEQUENCE"
	UMLCommunication       DiagramType = "UML_COMMUNICATION"
	UMLInteractionOverview DiagramType = "UML_INTERACTION_OVERVIEW"
	UMLTiming              DiagramType = "UML_TIMING"
	C4Context    DiagramType = "C4_CONTEXT"
	C4Container  DiagramType = "C4_CONTAINER"
	C4Component  DiagramType = "C4_COMPONENT"
	C4Code       DiagramType = "C4_CODE"
	C4Landscape  DiagramType = "C4_LANDSCAPE"
	C4Dynamic    DiagramType = "C4_DYNAMIC"
	C4Deployment DiagramType = "C4_DEPLOYMENT"
	DataFlow  DiagramType = "DATA_FLOW"
	ERDiagram DiagramType = "ER_DIAGRAM"
	Mindmap   DiagramType = "MINDMAP"
	Flowchart DiagramType = "FLOWCHART"
	DependencyGraph     DiagramType = "DEPENDENCY_GRAPH"
	HotspotComplexity   DiagramType = "HOTSPOT_COMPLEXITY"
	CallGraph           DiagramType = "CALL_GRAPH"
	LayeredArchitecture DiagramType = "LAYERED_ARCHITECTURE"
	ChangeImpact        DiagramType = "CHANGE_IMPACT"
	Infrastructure      DiagramType = "INFRASTRUCTURE"
)

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
	PipelineCfg   *PipelineConfig
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

type NativeEdge struct {
	SourceID   string
	Predicate  string
	TargetID   string
	LineNumber int
}

type PredicateGroup int

const (
	GroupCallGraph       PredicateGroup = iota
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
	PageRank        float64
	Betweenness     float64
	Community       string
	InDegree        int
	OutDegree       int
	IsHotspot       bool
	IsBottleneck    bool
	IsGodObject     bool
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
	StageParse   PipelineStage = iota
	StageScope
	StageExtract
	StageMetrics
	StageCluster
	StageLayout
	StageRender
)

type GraphSummary struct {
	NodeCount      int
	EdgeCount      int
	Density        float64
	Diameter       int
	AvgPathLength  float64
	ClusterCount   int
	LargestSCCSize int
	GodObjectCount int
	BipartiteScore float64
}

// FormatNodeURI converts a bare node ID into a fully-qualified GlassMarble IRI string,
// encoding special characters and choosing the correct namespace (file, module, or node).
func FormatNodeURI(id string) string {
	clean := strings.ReplaceAll(id, "\\", "/")
	clean = strings.ReplaceAll(clean, "\"", "%22")
	clean = strings.ReplaceAll(clean, " ", "%20")
	clean = strings.ReplaceAll(clean, "<", "%3C")
	clean = strings.ReplaceAll(clean, ">", "%3E")
	clean = strings.ReplaceAll(clean, "`", "%60")

	if strings.HasPrefix(clean, "file:") {
		return fmt.Sprintf("<http://glassmarble.org/file/%s>", clean[5:])
	}
	if strings.HasPrefix(clean, "module:") {
		return fmt.Sprintf("<http://glassmarble.org/namespace/%s>", clean[7:])
	}
	return fmt.Sprintf("<http://glassmarble.org/node/%s>", clean)
}

// ParseNodeURI strips the GlassMarble IRI prefix from a URI and returns the bare node ID.
// Supports glassmarble.org/node/, /file/, and /namespace/ namespaces, as well as plain angle-bracket IRIs.
func ParseNodeURI(uri string) string {
	uri = strings.TrimSpace(uri)
	if strings.HasPrefix(uri, "<http://glassmarble.org/node/") && strings.HasSuffix(uri, ">") {
		return uri[len("<http://glassmarble.org/node/") : len(uri)-1]
	}
	if strings.HasPrefix(uri, "<http://glassmarble.org/file/") && strings.HasSuffix(uri, ">") {
		return "file:" + uri[len("<http://glassmarble.org/file/"):len(uri)-1]
	}
	if strings.HasPrefix(uri, "<http://glassmarble.org/namespace/") && strings.HasSuffix(uri, ">") {
		return "module:" + uri[len("<http://glassmarble.org/namespace/"):len(uri)-1]
	}
	if strings.HasPrefix(uri, "<") && strings.HasSuffix(uri, ">") {
		return uri[1 : len(uri)-1]
	}
	return uri
}
