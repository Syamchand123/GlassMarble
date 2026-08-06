package stage3

import (
	"sync"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage2"
)

// Stage3Output represents the Aggregated Physical System Topology emitted by Stage 3.
type Stage3Output struct {
	// CommitHash is the Git commit identifier associated with this state mutation.
	CommitHash string `json:"commit_hash"`

	// RootNode represents the top-level repository workspace directory (root ".").
	RootNode *DirectoryNode `json:"root_node"`

	// GlobalDefinitionIndex is a flat lookup table of every structural entity visible across files.
	// Key: Fully Qualified Path Symbol (e.g., "src/core/database.PostgresStore")
	// Value: Slice of pointers to GASTNodes to gracefully handle overloading or shadowing.
	GlobalDefinitionIndex map[string][]*stage2.GASTNode `json:"global_definition_index"`

	// GlobalCallQueue is a unified bucket containing every unresolved external call site in the system.
	// This acts as a targeted checklist for Stage 4 Interprocedural Linker.
	GlobalCallQueue []LinkedCallSite `json:"global_call_queue"`

	// FileToSymbols maps a file path to its exported Fully Qualified Names (FQNs).
	// Crucial for true O(1) incremental pruning.
	FileToSymbols map[string][]string `json:"file_to_symbols"`

	// FileToMembers (v2, W1-10 / §5.3.4 / A-12) maps a file path to the
	// resolution keys (canonical IDs when present, else FQNs) of every
	// structural member defined in that file. Stage 4 consumes this to emit
	// real file→symbol CONTAINS edges so File nodes are never dead ends.
	FileToMembers map[string][]string `json:"file_to_members,omitempty"`

	// FileToCalls maps a file path to its external call sites.
	// Crucial for true O(1) incremental compilation.
	FileToCalls map[string][]LinkedCallSite `json:"file_to_calls"`

	// LocalTables preserves the file-level semantic mappings (Step 2.3 output) for Stage 4 linkage.
	LocalTables map[string]*stage2.FileSymbolTable `json:"local_tables"`

	// WorkspaceCtx holds the parsed monorepo module boundaries and path aliases.
	WorkspaceCtx *WorkspaceContext `json:"workspace_ctx"`

	// Step 3.6: External Dependencies map (Virtual nodes for 3rd party SDKs/libs).
	ExternalDependencies map[string]*stage2.GASTNode `json:"external_dependencies"`

	// Step 3.7: Generics Registry maps base names to their canonical generic signatures.
	GenericsRegistry map[string]string `json:"generics_registry"`

	// Step 3.8: Entrypoint Registry tracks FQNs of root execution nodes (mains, API endpoints).
	EntrypointRegistry []string `json:"entrypoint_registry"`
}

// DirectoryNode represents a physical folder in the workspace hierarchy.
type DirectoryNode struct {
	mu            sync.RWMutex
	FolderName    string                       `json:"folder_name"`    // e.g., "database"
	RelativePath  string                       `json:"relative_path"`  // e.g., "src/core/database"
	PrimitiveZone string                       `json:"primitive_zone"` // e.g., "DATABASE", "SECURITY" (Step 3.9)
	SubFolders    map[string]*DirectoryNode    `json:"sub_folders"`    // Key: Child folder name
	Files         map[string]*FileBoundaryNode `json:"files"`          // Key: File name (e.g., "postgres.go")
}

// FileBoundaryNode represents a physical file boundary inside a directory node.
type FileBoundaryNode struct {
	FileName     string           `json:"file_name"`     // e.g., "postgres.go"
	RelativePath string           `json:"relative_path"` // e.g., "src/core/database/postgres.go"
	Language     string           `json:"language"`      // e.g., "go"
	GASTRoot     *stage2.GASTNode `json:"gast_root"`     // Normalized AST tree from Stage 2
	LocalImports []string         `json:"local_imports"` // List of imports declared in this file
}

// LinkedCallSite represents an unresolved call site waiting for Stage 4 graph linking.
type LinkedCallSite struct {
	SourceFileNodeID string                       `json:"source_file_node_id"` // GAST Node ID of caller function
	SourceFilePath   string                       `json:"source_file_path"`    // Originating file relative path
	SourceFolderPath string                       `json:"source_folder_path"`  // Local folder path context
	ReceiverName     string                       `json:"receiver_name"`       // Variable/struct being invoked
	MethodName       string                       `json:"method_name"`         // Method/function token called
	LineNumber       int                          `json:"line_number"`         // Source code line number
	HasPrimitive     bool                         `json:"has_primitive"`       // Indicates boundary interaction
	Primitives       []stage2.BehavioralPrimitive `json:"primitives"`          // Behavioral flags
	LocalImports     []string                     `json:"local_imports"`
}

// NewDirectoryNode instantiates a fresh directory node.
func NewDirectoryNode(folderName, relPath string) *DirectoryNode {
	return &DirectoryNode{
		FolderName:   folderName,
		RelativePath: relPath,
		SubFolders:   make(map[string]*DirectoryNode),
		Files:        make(map[string]*FileBoundaryNode),
	}
}
