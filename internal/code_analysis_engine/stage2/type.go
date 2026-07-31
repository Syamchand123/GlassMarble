package stage2

import (
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage1"
)

type GASTNodeType string

const (
	GASTFileRoot        GASTNodeType = "FILE_ROOT"
	GASTTypeDeclaration GASTNodeType = "TYPE_DECLARATION" // Struct, Class, Interface, Enum, Record, Union, Trait
	GASTFunction        GASTNodeType = "FUNCTION"         // Function, Method, Constructor
	GASTParameter       GASTNodeType = "PARAMETER"        // Method/Function Parameter
	GASTField           GASTNodeType = "FIELD"            // Class/Struct Field
	GASTImport          GASTNodeType = "IMPORT"           // Import Statement / Module Include
	GASTCallExpression  GASTNodeType = "CALL_EXPRESSION"  // Function / Method Invocation
	GASTVariable        GASTNodeType = "VARIABLE"         // Variable Declaration
	GASTNamespace       GASTNodeType = "NAMESPACE"        // Package / Namespace Declaration
	GASTControlFlow     GASTNodeType = "CONTROL_FLOW"     // If, For, Switch, Return, etc.
)

type BehavioralPrimitive string

const (
	// Category A: Core System I/O & Storage
	PrimDiskIO        BehavioralPrimitive = "DISK_IO"
	PrimNetworkIO     BehavioralPrimitive = "NETWORK_IO"
	PrimDatabaseSQL   BehavioralPrimitive = "DATABASE_SQL"
	PrimDatabaseNoSQL BehavioralPrimitive = "DATABASE_NOSQL"

	// Backward-compatibility alias
	PrimDatabase BehavioralPrimitive = "DATABASE_SQL"

	// Category B: Distributed Infrastructure & Messaging
	PrimCache           BehavioralPrimitive = "CACHE"
	PrimMessageQueue    BehavioralPrimitive = "MESSAGE_QUEUE"
	PrimCloudSDK        BehavioralPrimitive = "CLOUD_SDK"
	PrimContainerDevOps BehavioralPrimitive = "CONTAINER_DEVOPS"

	// Category C: Processing, Concurrency & Memory
	PrimConcurrency     BehavioralPrimitive = "CONCURRENCY"
	PrimSynchronization BehavioralPrimitive = "SYNCHRONIZATION"
	PrimAllocation      BehavioralPrimitive = "ALLOCATION"
	PrimComputeMath     BehavioralPrimitive = "COMPUTE_MATH"

	// Category D: Security, Telemetry & Observability
	PrimSecurityAuth BehavioralPrimitive = "SECURITY_AUTH"
	PrimCrypto       BehavioralPrimitive = "CRYPTO"
	PrimLogging      BehavioralPrimitive = "LOGGING"
	PrimTelemetry    BehavioralPrimitive = "TELEMETRY"

	// Category E: Advanced Enterprise Integration
	PrimAI      BehavioralPrimitive = "AI_LLM"
	PrimIPC     BehavioralPrimitive = "IPC"
	PrimRPC     BehavioralPrimitive = "RPC"
	PrimUIEvent BehavioralPrimitive = "UI_EVENT"
	
	// Category F: Architectural Violations
	PrimCycleViolation BehavioralPrimitive = "CYCLE_VIOLATION"
)

// GASTNode is the language-agnostic internal representation of source code structures.
type GASTNode struct {
	ID           string                `json:"id"`
	Type         GASTNodeType          `json:"type"`
	Name         string                `json:"name"`
	Kind         string                `json:"kind"`
	DataType     string                `json:"data_type"`
	Namespace    string                `json:"namespace,omitempty"`
	ReceiverType string                `json:"receiver_type,omitempty"`
	DocComment   string                `json:"doc_comment,omitempty"`
	Visibility   string                `json:"visibility,omitempty"`
	Annotations  []string              `json:"annotations,omitempty"`
	Primitives   []BehavioralPrimitive `json:"primitives,omitempty"`
	Properties   map[string]string     `json:"properties,omitempty"`
	StartLine    uint32                `json:"start_line"`
	EndLine      uint32                `json:"end_line"`
	StartByte    uint32                `json:"start_byte"`
	EndByte      uint32                `json:"end_byte"`
	Children     []*GASTNode           `json:"children,omitempty"`
}

// SymbolMeta tracks exported or local declarations for namespace resolution.
type SymbolMeta struct {
	Name         string   `json:"name"`
	Kind         string   `json:"kind"`
	Namespace    string   `json:"namespace"`
	ReceiverType string   `json:"receiver_type,omitempty"`
	GASTNodeID   string   `json:"gast_node_id"`
	Visibility   string   `json:"visibility"`
	DataType     string   `json:"data_type,omitempty"`
	IsAsync      bool     `json:"is_async,omitempty"`
	Annotations  []string `json:"annotations,omitempty"`
}

// CallSite tracks unresolved method invocations within a file.
type CallSite struct {
	CallerNodeID string                `json:"caller_node_id"`
	ReceiverName string                `json:"receiver_name"`
	MethodName   string                `json:"method_name"`
	LineNumber   int                   `json:"line_number"`
	HasPrimitive bool                  `json:"has_primitive"`
	Primitives   []BehavioralPrimitive `json:"primitives,omitempty"`
	IsAwait      bool                  `json:"is_await,omitempty"`
}

type TypeAliasMeta struct {
	AliasName  string `json:"alias_name"`
	TargetType string `json:"target_type"`
	LineNumber int    `json:"line_number"`
}

type InheritanceMeta struct {
	ChildName   string `json:"child_name"`
	ParentName  string `json:"parent_name"`
	IsInterface bool   `json:"is_interface"`
	LineNumber  int    `json:"line_number"`
}

type InstantiationMeta struct {
	ObjectName string `json:"object_name"`
	LineNumber int    `json:"line_number"`
}

type ResourceMeta struct {
	ResourceType string `json:"resource_type"`
	ResourcePath string `json:"resource_path"`
	LineNumber   int    `json:"line_number"`
}

type ExceptionMeta struct {
	ExceptionType string `json:"exception_type"`
	Action        string `json:"action"` // "THROW", "CATCH"
	LineNumber    int    `json:"line_number"`
}

type SpawnMeta struct {
	ConcurrencyModel string `json:"concurrency_model"`
	TargetNodeID     string `json:"target_node_id"`
	LineNumber       int    `json:"line_number"`
}

type EventMeta struct {
	EventName  string `json:"event_name"`
	Action     string `json:"action"` // "EMIT", "LISTEN"
	LineNumber int    `json:"line_number"`
}

type EndpointMeta struct {
	Route      string `json:"route"`
	Method     string `json:"method"`
	LineNumber int    `json:"line_number"`
}

type SecuritySinkMeta struct {
	SinkType   string `json:"sink_type"`
	Severity   string `json:"severity"`
	LineNumber int    `json:"line_number"`
}

// FileSymbolTable tracks the full semantic extraction map within a single file scope.
type FileSymbolTable struct {
	FilePath    string               `json:"file_path"`
	RelPath     string               `json:"rel_path"`
	PackageName string               `json:"package_name,omitempty"`
	Language    stage1.SupportedLang `json:"language"`

	// Level 1: Core Definitions
	Imports     []string        `json:"imports"`
	Definitions []SymbolMeta    `json:"definitions"`
	LocalCalls  []CallSite      `json:"local_calls"`
	TypeAliases []TypeAliasMeta `json:"type_aliases,omitempty"`

	// Level 2: Architecture & Data Flow
	Inheritances   []InheritanceMeta   `json:"inheritances,omitempty"`
	Instantiations []InstantiationMeta `json:"instantiations,omitempty"`
	GlobalState    []SymbolMeta        `json:"global_state,omitempty"`
	ResourceLinks  []ResourceMeta      `json:"resource_links,omitempty"`

	// Level 3: Execution & Resilience
	Exceptions        []ExceptionMeta `json:"exceptions,omitempty"`
	ConcurrencySpawns []SpawnMeta     `json:"concurrency_spawns,omitempty"`
	EventHooks        []EventMeta     `json:"event_hooks,omitempty"`

	// Level 4: Boundaries & Security
	Endpoints     []EndpointMeta     `json:"endpoints,omitempty"`
	SecuritySinks []SecuritySinkMeta `json:"security_sinks,omitempty"`
}

// Stage2Payload is the complete atomic output of Stage 2 for a Git commit / ingestion run.
type Stage2Payload struct {
	CommitHash        string                      `json:"commit_hash"`
	UpsertedTrees     map[string]*GASTNode        `json:"upserted_trees"`      // Key: RelPath
	LocalSymbolTables map[string]*FileSymbolTable `json:"local_symbol_tables"` // Key: RelPath
	DeletedPaths      []string                    `json:"deleted_paths"`
}
