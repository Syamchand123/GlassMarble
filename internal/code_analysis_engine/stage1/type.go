package stage1

import (
	"context"
	"time"
)

type SupportedLang string

const (
	LangGo      SupportedLang = "go"
	LangJava    SupportedLang = "java"
	LangPython  SupportedLang = "python"
	LangJS      SupportedLang = "javascript"
	LangTS      SupportedLang = "typescript"
	LangCpp     SupportedLang = "cpp"
	LangC       SupportedLang = "c"
	LangCSharp  SupportedLang = "csharp"
	LangRust    SupportedLang = "rust"
	LangRuby    SupportedLang = "ruby"
	LangPHP     SupportedLang = "php"
	LangKotlin  SupportedLang = "kotlin"
	LangSwift   SupportedLang = "swift"
	LangScala   SupportedLang = "scala"
	LangCSS     SupportedLang = "css"
	LangHTML    SupportedLang = "html"
	LangJSON    SupportedLang = "json"
	LangUnknown SupportedLang = "unknown"
)

type ChangeKind string

const (
	ChangeAdded    ChangeKind = "added"
	ChangeModified ChangeKind = "modified"
	ChangeDeleted  ChangeKind = "deleted"
	ChangeRenamed  ChangeKind = "renamed"
)

type TokenKind string

const (
	TokenDeclaration TokenKind = "declaration"
	TokenCall        TokenKind = "call"
	TokenImport      TokenKind = "import"
)

// FileTask represents a single ingestion request dispatched to a worker.
type FileTask struct {
	FilePath string
	RelPath  string
	Language SupportedLang
	Change   ChangeKind
	Commit   string
	Author   string
	Time     time.Time
}

// RichToken represents a localized structural element found by Tree-sitter.
// It is a superset of the legacy RichToken (master_overhaul_plan.md §5.1.1):
// the flat token stream keeps the original fields, while declaration nodes
// additionally carry their tree-sitter field roles and declaration-relevant
// named children so stage 2 translators never need content-regex parsing
// (fixes A-07/A-08/A-18 at the source).
type RichToken struct {
	Kind       TokenKind
	Type       string
	Content    string
	Name       string
	DocComment string
	ParentIdx  int
	Depth      int
	StartLine  uint32
	EndLine    uint32
	StartByte  uint32
	EndByte    uint32
	HasError   bool

	// FieldRoles maps tree-sitter field name â†’ child token text
	// (e.g. "name"â†’"Options", "type"â†’"string", "base_type"â†’"Base",
	// "receiver"â†’"s", "result"â†’"bool", "interface"â†’"Provider").
	// For declaration nodes only. §5.1.1.
	FieldRoles map[string]string
	// NamedChildren holds only declaration-relevant named children of a
	// declaration node (e.g. field_declaration / method_spec children of a
	// type_spec). §5.1.1.
	NamedChildren []*RichToken
	// IsFieldDecl marks a field-bearing declaration node
	// (field_declaration, property_signature, ...). §5.1.1.
	IsFieldDecl bool
	// IsMethodSpec marks an interface method declaration (Go method_spec,
	// TS method_signature). §5.1.1.
	IsMethodSpec bool
	// IsEmbedded marks Go anonymous embedding / C++ base class nodes.
	// §5.1.1.
	IsEmbedded bool
}

// IngestionResult aggregates the parsed syntax fragments of a single file.
type IngestionResult struct {
	FilePath   string
	RelPath    string
	Language   SupportedLang
	Change     ChangeKind
	Commit     string
	Author     string
	Time       time.Time
	RichTokens []RichToken
	Bytes      int
	HasErrors  bool
	Error      error
}

// DeleteEvent is emitted for files removed from the repository. They bypass
// the parser and go straight to the AKG for node pruning.
type DeleteEvent struct {
	FilePath string
	RelPath  string
	Language SupportedLang
	Commit   string
	Author   string
	Time     time.Time
}

// StageOutput is the final Stage 1 payload consumed by Stage 2.
type StageOutput struct {
	Updated  []*IngestionResult
	Deleted  []*DeleteEvent
	Skipped  []string
	Warnings []string
}

// Config holds the knobs for a Stage 1 ingestion run.
type Config struct {
	RootDir       string
	WorkerCount   int
	MaxFileBytes  int64
	BufferSize    int
	IncludeHidden bool
	// GitTrackedOnly restricts discovery to files tracked by git
	// (git ls-files). When the directory is not a git repository the walker
	// falls back to scanning everything (AUDIT Issue 1.8 / Phase 1C-9).
	GitTrackedOnly bool
	Ctx            context.Context
	// OnProgress, when non-nil, is invoked as files are discovered and parsed
	// so a BubbleTea program can animate a live per-file counter. done is the
	// number of files emitted so far; total is the number of parse tasks
	// dispatched (0 when unknown, e.g. streaming discovery in flight).
	OnProgress func(done, total int)
}

const defaultMaxFileBytes = 2 << 20

func DefaultConfig(root string) Config {
	return Config{
		RootDir:       root,
		WorkerCount:   0,
		MaxFileBytes:  defaultMaxFileBytes,
		BufferSize:    128,
		IncludeHidden: false,
		Ctx:           context.Background(),
	}
}
