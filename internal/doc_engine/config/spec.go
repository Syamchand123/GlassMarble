// Package config defines all data contracts for the GlassMarble
// Documentation Intelligence Engine (v1.2.0).
//
// Design mandate: 95% deterministic Go core, 5% LLM prose actuator.
// All types here are the schema for:
//   - .glassmarble/docs.yaml  (document configuration)
//   - .glassmarble/docs_state.json  (persistence / hash cache)
//   - FactSheet JSON  (the only data that crosses to the LLM)
package config

import "time"

// ────────────────────────────────────────────────────────────────────────────
// DocMode — how the engine manages a target file.
// ────────────────────────────────────────────────────────────────────────────

// DocMode defines the ownership model for a managed document.
type DocMode string

const (
	// ModeFullOwned means GlassMarble owns the entire file.
	// Every section is machine-managed. Human prose should live
	// exclusively in sections whose Managed == false.
	ModeFullOwned DocMode = "full_owned"

	// ModeManagedSections means GlassMarble only touches zones
	// delimited by <!-- gmb:begin:id --> ... <!-- gmb:end:id -->.
	// All other content is human territory and is never modified.
	ModeManagedSections DocMode = "managed_sections"
)

// ────────────────────────────────────────────────────────────────────────────
// ScopeRule — what paths and symbols a document "owns"
// ────────────────────────────────────────────────────────────────────────────

// ScopeRule defines the code boundary a document tracks.
// The invalidation engine uses ScopeRule to determine whether a commit
// makes a document's sections dirty.
type ScopeRule struct {
	// Paths are glob patterns relative to the repository root.
	// Example: ["internal/auth/**", "cmd/login.go"]
	Paths []string `yaml:"paths" json:"paths"`

	// EntryPoints are fully-qualified symbol names used as call-graph
	// anchors for sequence-diagram and callgraph grounding.
	// Example: ["internal/auth/service.go::Authenticate"]
	EntryPoints []string `yaml:"entry_points,omitempty" json:"entry_points,omitempty"`

	// ExcludePaths are glob patterns that are subtracted from Paths.
	// Example: ["internal/auth/testutil/**"]
	ExcludePaths []string `yaml:"exclude_paths,omitempty" json:"exclude_paths,omitempty"`
}

// ────────────────────────────────────────────────────────────────────────────
// DiagramRef — a living diagram to embed in the document
// ────────────────────────────────────────────────────────────────────────────

// DiagramRef links a living diagram (rendered by internal/visualization_engine)
// to a specific scope or entry point.
type DiagramRef struct {
	// Type is the diagram kind: c4container | layered | dependency |
	// callgraph | sequence | er | hotspot | ...
	Type string `yaml:"type" json:"type"`

	// Entry is an FQN symbol used as the root for callgraph/sequence diagrams.
	// Optional; only required for callgraph and sequence types.
	Entry string `yaml:"entry,omitempty" json:"entry,omitempty"`

	// Scope narrows the diagram to a folder or symbol set.
	// Example: "folder:internal/auth" or "global".
	Scope string `yaml:"scope,omitempty" json:"scope,omitempty"`
}

// ────────────────────────────────────────────────────────────────────────────
// StyleSpec — voice, tone, and jargon controls for the LLM actuator
// ────────────────────────────────────────────────────────────────────────────

// StyleSpec configures the writing style injected into every LLM prompt for
// a document. It ensures consistent voice across all managed docs regardless
// of which commit triggered the update.
type StyleSpec struct {
	// Standard is a named style preset: developer_guide | api_reference |
	// runbook | academic
	Standard string `yaml:"standard,omitempty" json:"standard,omitempty"`

	// Voice is a free-form instruction for tone: e.g.,
	// "active, second-person, present tense"
	Voice string `yaml:"voice,omitempty" json:"voice,omitempty"`

	// JargonBlacklist lists words/phrases the LLM must never use.
	// Example: ["simply", "obviously", "leverage", "utilize"]
	JargonBlacklist []string `yaml:"jargon_blacklist,omitempty" json:"jargon_blacklist,omitempty"`
}

// ────────────────────────────────────────────────────────────────────────────
// SectionSpec — one managed section within a document
// ────────────────────────────────────────────────────────────────────────────

// SectionSpec defines a single managed section within a DocSpec.
// It maps to <!-- gmb:begin:ID --> ... <!-- gmb:end:ID --> anchor blocks.
type SectionSpec struct {
	// ID is the stable anchor identifier used in HTML comments:
	//   <!-- gmb:begin:ID --> ... <!-- gmb:end:ID -->
	// Must be URL-safe (lowercase letters, digits, hyphens).
	ID string `yaml:"id" json:"id"`

	// Title is the human-readable section heading. Used as the H2/H3
	// in full_owned documents and as a label in status output.
	Title string `yaml:"title" json:"title"`

	// Instruction is the natural-language directive sent to the LLM.
	// The LLM reads this alongside the FactSheet to understand what to write.
	// Example: "Table of exported types, methods, and sentinel errors"
	Instruction string `yaml:"instruction" json:"instruction"`

	// GroundWith lists which AKG data sources to pull for this section.
	// Valid values: signatures | exported_symbols | callgraph | comments |
	// sentinels | error_returns | concurrency_primitives | config_vars |
	// arch_intelligence | arch_events | http_handlers
	GroundWith []string `yaml:"ground_with,omitempty" json:"ground_with,omitempty"`

	// Managed controls whether the engine may update this section.
	// false = human-only; the engine never overwrites it.
	// Defaults to true.
	Managed bool `yaml:"managed" json:"managed"`

	// Freeze locks the section. Equivalent to <!-- gmb:freeze -->.
	// Overrides Managed. Even if Managed is true, a frozen section is
	// never touched.
	Freeze bool `yaml:"freeze,omitempty" json:"freeze,omitempty"`

	// MaxWords enforces an upper bound on LLM output for this section.
	// 0 means no limit.
	MaxWords int `yaml:"max_words,omitempty" json:"max_words,omitempty"`
}

// ────────────────────────────────────────────────────────────────────────────
// GlobalConstraints — budget caps applied to all documents
// ────────────────────────────────────────────────────────────────────────────

// GlobalConstraints enforces resource budgets per run.
type GlobalConstraints struct {
	// MaxDocUpdatesPerCommit is the maximum number of document sections
	// the engine will update in a single run. Overflow is queued for the
	// next run. 0 = unlimited.
	MaxDocUpdatesPerCommit int `yaml:"max_doc_updates_per_commit,omitempty" json:"max_doc_updates_per_commit,omitempty"`

	// MaxTokensPerRun is a hard ceiling on total LLM tokens for one run.
	// The engine stops issuing LLM calls when this is reached and falls
	// back to deterministic rendering for remaining sections. 0 = unlimited.
	MaxTokensPerRun int `yaml:"max_tokens_per_run,omitempty" json:"max_tokens_per_run,omitempty"`

	// MinFreshnessThreshold triggers a CI warning when any document's
	// freshness score drops below this value (0-100). 0 = disabled.
	MinFreshnessThreshold int `yaml:"min_freshness_threshold,omitempty" json:"min_freshness_threshold,omitempty"`

	// MinFreshnessFail triggers a CI hard fail when any document's
	// freshness score drops below this value (0-100). 0 = disabled.
	MinFreshnessFail int `yaml:"min_freshness_fail,omitempty" json:"min_freshness_fail,omitempty"`
}

// ────────────────────────────────────────────────────────────────────────────
// DocSpec — the contract for one living document
// ────────────────────────────────────────────────────────────────────────────

// DocSpec defines the complete specification for a single living document.
// It lives either in .glassmarble/docs.yaml (under the `documents:` array)
// or in the YAML frontmatter of the document itself (self-describing mode).
type DocSpec struct {
	// ID is a unique, stable identifier for this document.
	// Used as the key in docs_state.json and for --doc filtering.
	// Must be URL-safe (lowercase letters, digits, hyphens).
	ID string `yaml:"id" json:"id"`

	// TargetPath is the path to the .md file, relative to the repository root.
	// Example: "docs/architecture.md"
	TargetPath string `yaml:"target" json:"target"`

	// Title is the document's human-readable title.
	Title string `yaml:"title" json:"title"`

	// Purpose is a high-signal description of what the document is for.
	// Injected into every LLM prompt to set context. One sentence.
	Purpose string `yaml:"purpose" json:"purpose"`

	// Audience is the target reader. Shapes LLM tone and depth.
	// Example: "On-call SRE engineers at 2 AM"
	Audience string `yaml:"audience" json:"audience"`

	// Mode controls how the engine manages the file.
	// Defaults to ModeManagedSections for safety.
	Mode DocMode `yaml:"mode,omitempty" json:"mode,omitempty"`

	// Scope defines which code paths and symbols this document tracks.
	Scope ScopeRule `yaml:"scope" json:"scope"`

	// Sections is the ordered list of sections this document contains.
	Sections []SectionSpec `yaml:"sections,omitempty" json:"sections,omitempty"`

	// Diagrams is the list of living diagrams to embed in this document.
	Diagrams []DiagramRef `yaml:"diagrams,omitempty" json:"diagrams,omitempty"`

	// Style overrides the global style for this document.
	Style *StyleSpec `yaml:"style,omitempty" json:"style,omitempty"`

	// Archetype names a built-in document template.
	// Valid: architecture | module | runbook | onboarding | migration |
	//        adr | api | security | database | config
	// When set, the archetype's default sections are used as a starting
	// point, and Sections entries override/extend them.
	Archetype string `yaml:"archetype,omitempty" json:"archetype,omitempty"`

	// MinFreshness is a per-document override for the CI freshness threshold.
	// 0 = use the global threshold from GlobalConstraints.
	MinFreshness int `yaml:"min_freshness,omitempty" json:"min_freshness,omitempty"`

	// Tags are arbitrary labels for grouping and filtering.
	// Example: ["security", "api-surface", "oncall"]
	Tags []string `yaml:"tags,omitempty" json:"tags,omitempty"`
}

// ────────────────────────────────────────────────────────────────────────────
// DocsConfig — the top-level .glassmarble/docs.yaml schema
// ────────────────────────────────────────────────────────────────────────────

// DocsConfig is the schema for .glassmarble/docs.yaml.
// It is the single authoritative configuration for all managed documents.
type DocsConfig struct {
	// Version is the schema version. Currently must be 1.
	Version int `yaml:"version" json:"version"`

	// DocsDir is the default directory for generated documents.
	// Documents without an explicit TargetPath prefix will be placed here.
	// Example: "docs"
	DocsDir string `yaml:"docs_dir,omitempty" json:"docs_dir,omitempty"`

	// Style is the global style applied to all documents unless overridden
	// by a per-document Style block.
	Style StyleSpec `yaml:"style,omitempty" json:"style,omitempty"`

	// Constraints applies budget and freshness caps globally.
	Constraints GlobalConstraints `yaml:"constraints,omitempty" json:"constraints,omitempty"`

	// TargetPlatform controls markdown output formatting for a specific
	// documentation platform.
	// Valid: github_flat (default) | vitepress | docusaurus | mkdocs
	TargetPlatform string `yaml:"target_platform,omitempty" json:"target_platform,omitempty"`

	// Documents is the ordered list of all managed documents.
	Documents []DocSpec `yaml:"documents" json:"documents"`
}

// ────────────────────────────────────────────────────────────────────────────
// FactSheet — the airtight JSON contract sent to the LLM
// ────────────────────────────────────────────────────────────────────────────

// FactSheet is the sole payload sent to the LLM prose actuator.
// It contains only symbol metadata, doc comments, and diagram code —
// never raw source files, environment variables, or secrets.
// The grounding/sanitizer.go component scrubs all content before assembly.
type FactSheet struct {
	SchemaVersion int    `json:"schema_version"`
	DocID         string `json:"doc_id"`
	SectionID     string `json:"section_id"`
	CommitHash    string `json:"commit_hash"`

	// CommitIntent is the classified commit intent (ADD_FEATURE, FIX_BUG, etc.)
	CommitIntent string `json:"commit_intent"`

	// CommitReason is the human-readable commit message subject.
	CommitReason string `json:"commit_reason"`

	// DocPurpose is the DocSpec.Purpose field; sets context for the LLM.
	DocPurpose string `json:"doc_purpose"`

	// DocAudience is the DocSpec.Audience field; shapes LLM tone.
	DocAudience string `json:"doc_audience"`

	// SectionInstruction is the SectionSpec.Instruction field.
	SectionInstruction string `json:"section_instruction"`

	// MaxWords caps LLM output length for this section (from SectionSpec.MaxWords).
	// 0 means no limit.
	MaxWords int `json:"max_words,omitempty"`

	// PriorSectionMarkdown is the current content of the managed zone.
	// The LLM updates this rather than writing from scratch.
	PriorSectionMarkdown string `json:"prior_section_markdown"`

	// GroundTruth is the deterministic data extracted from the AKG.
	GroundTruth GroundTruthPayload `json:"ground_truth"`

	// RenderMode records whether this fact sheet will go to the LLM
	// actuator ("llm") or the deterministic renderer ("deterministic").
	RenderMode string `json:"render_mode"`

	// Style is the effective style spec for this section.
	Style StyleSpec `json:"style"`
}

// GroundTruthPayload groups all AKG-derived facts for a section update.
// Every field is populated deterministically from the AKG before
// the LLM is called.
type GroundTruthPayload struct {
	// AddedSymbols lists new symbols introduced in this commit.
	AddedSymbols []SymbolFact `json:"added_symbols,omitempty"`

	// ModifiedSymbols lists symbols whose signatures or doc comments changed.
	ModifiedSymbols []SymbolDelta `json:"modified_symbols,omitempty"`

	// RemovedSymbols lists FQNs of symbols deleted in this commit.
	RemovedSymbols []string `json:"removed_symbols,omitempty"`

	// AllSymbols is the complete current symbol set for the scope.
	// Used for full_owned sections that need the current state.
	AllSymbols []SymbolFact `json:"all_symbols,omitempty"`

	// CallFlow is an ordered list of FQNs representing the call chain
	// from the document's entry point.
	CallFlow []string `json:"call_flow,omitempty"`

	// Callers lists the direct callers of changed symbols.
	Callers []string `json:"callers,omitempty"`

	// DiagramMermaid is the freshly rendered Mermaid/PlantUML source
	// for this section's diagram (if configured). Injected verbatim.
	DiagramMermaid string `json:"diagram_mermaid,omitempty"`

	// DocComments maps symbol FQN to its extracted doc comment.
	DocComments map[string]string `json:"doc_comments,omitempty"`

	// ConfigVars lists environment variable / config knobs in scope.
	ConfigVars []ConfigVarFact `json:"config_vars,omitempty"`

	// AddedConfigVars lists newly added config vars.
	AddedConfigVars []ConfigVarFact `json:"added_config_vars,omitempty"`

	// RemovedConfigVars lists config var names removed in this commit.
	RemovedConfigVars []string `json:"removed_config_vars,omitempty"`

	// Sentinels lists sentinel error variables in scope.
	Sentinels []SentinelFact `json:"sentinels,omitempty"`

	// AddedSentinels lists new sentinel errors introduced in this commit.
	AddedSentinels []SentinelFact `json:"added_sentinels,omitempty"`

	// ModifiedSentinels lists sentinel errors whose value or doc comment changed.
	ModifiedSentinels []SentinelDelta `json:"modified_sentinels,omitempty"`

	// Symbols is the active set of symbols in scope for this section.
	Symbols []SymbolFact `json:"symbols,omitempty"`

	// ArchEvents lists architectural milestone events and ontology facts.
	ArchEvents []string `json:"arch_events,omitempty"`

	// Diagrams lists all rendered living diagrams for this document.
	Diagrams []DiagramFact `json:"diagrams,omitempty"`
}

// DiagramFact captures a rendered living diagram (Mermaid block) for a document.
type DiagramFact struct {
	Type    string `json:"type"`
	Content string `json:"content"`
}

// SymbolFact is the AKG-extracted description of a single code symbol.
// All fields are populated deterministically from Tree-sitter GAST output.
type SymbolFact struct {
	// FQN is the fully-qualified name: "package/path::Type.Method"
	FQN string `json:"fqn"`

	// Kind is the symbol type: func | method | struct | interface | var | const
	Kind string `json:"kind"`

	// Signature is the exact typed signature as it appears in source.
	// Example: "func (tm *AKGTransactionManager) ExecuteDeltaTransaction(...) error"
	Signature string `json:"signature"`

	// Doc is the verbatim doc comment extracted from source.
	Doc string `json:"doc,omitempty"`

	// File is the repo-relative path to the source file.
	File string `json:"file"`

	// Line is the 1-based line number where the symbol is declared.
	Line int `json:"line"`

	// Permalink is the file:line range anchor for embedded links.
	// Example: "internal/auth/pkce.go#L47-L89"
	Permalink string `json:"permalink,omitempty"`
}

// SymbolDelta captures a change to an existing symbol between two commits.
type SymbolDelta struct {
	// FQN is the fully-qualified name of the changed symbol.
	FQN string `json:"fqn"`

	// Before is the signature/value before the commit.
	Before string `json:"before"`

	// After is the signature/value after the commit.
	After string `json:"after"`

	// DocBefore is the doc comment before the commit (empty if unchanged).
	DocBefore string `json:"doc_before,omitempty"`

	// DocAfter is the doc comment after the commit (empty if unchanged).
	DocAfter string `json:"doc_after,omitempty"`
}

// ConfigVarFact captures an environment variable or config key read by the code.
type ConfigVarFact struct {
	// Name is the environment variable name or config key.
	Name string `json:"name"`

	// Source is how it is read: os.Getenv | viper | flag | yaml
	Source string `json:"source"`

	// Required indicates whether the code panics/errors if the value is missing.
	Required bool `json:"required"`

	// Default is the fallback value if not set (empty string if none).
	Default string `json:"default,omitempty"`

	// DefaultValue is an alias for Default.
	DefaultValue string `json:"default_value,omitempty"`

	// Doc is the doc comment associated with the read site.
	Doc string `json:"doc,omitempty"`

	// File is the repo-relative source file.
	File string `json:"file"`

	// Line is the line number of the Getenv/flag/viper call.
	Line int `json:"line"`
}

// SentinelFact captures a sentinel error variable (var ErrFoo = errors.New(...)).
type SentinelFact struct {
	// FQN is the fully-qualified identifier: "internal/auth.ErrTokenExpired"
	FQN string `json:"fqn"`

	// Value is the error message string.
	Value string `json:"value"`

	// Doc is the doc comment for this sentinel.
	Doc string `json:"doc,omitempty"`

	// File is the repo-relative source file.
	File string `json:"file"`

	// Line is the line number of the declaration.
	Line int `json:"line"`

	// Callers lists the FQNs of functions that return this sentinel.
	Callers []string `json:"callers,omitempty"`
}

// SentinelDelta captures a change to a sentinel error value.
type SentinelDelta struct {
	FQN    string `json:"fqn"`
	Before string `json:"before"`
	After  string `json:"after"`
}

// ────────────────────────────────────────────────────────────────────────────
// GlobalCommitDossier — the single unified analysis built once per commit
// ────────────────────────────────────────────────────────────────────────────

// GlobalCommitDossier is built once per commit from the AKG diff,
// commit_reasoning, and arch_timeline. All document updates in a single
// run draw from this shared dossier to ensure cross-document consistency.
type GlobalCommitDossier struct {
	CommitHash    string `json:"commit_hash"`
	CommitIntent  string `json:"commit_intent"`
	CommitReason  string `json:"commit_reason"`
	PRDescription string `json:"pr_description,omitempty"`
	IssueRefs     []string `json:"issue_refs,omitempty"`

	// Symbol changes (from AKG GraphDiff)
	AddedSymbols    []SymbolFact   `json:"added_symbols,omitempty"`
	ModifiedSymbols []SymbolDelta  `json:"modified_symbols,omitempty"`
	RemovedSymbols  []string       `json:"removed_symbols,omitempty"`

	// Config var changes
	AddedConfigVars   []ConfigVarFact `json:"added_config_vars,omitempty"`
	RemovedConfigVars []string        `json:"removed_config_vars,omitempty"`

	// Sentinel error changes
	AddedSentinels    []SentinelFact  `json:"added_sentinels,omitempty"`
	ModifiedSentinels []SentinelDelta `json:"modified_sentinels,omitempty"`

	// ArchEvents is the list of architectural milestone events
	// (from internal/arch_timeline).
	ArchEvents []string `json:"arch_events,omitempty"`

	// DirtySections is the prioritized list of (doc, section) pairs
	// that need updating. Populated by the invalidation engine.
	DirtySections []DirtySectionRef `json:"dirty_sections,omitempty"`
}

// DirtySectionRef identifies one section within one document that has been
// invalidated and needs updating.
type DirtySectionRef struct {
	// DocID is the DocSpec.ID.
	DocID string `json:"doc_id"`

	// DocPath is the DocSpec.TargetPath.
	DocPath string `json:"doc_path"`

	// SectionID is the SectionSpec.ID. Empty string means the entire document.
	SectionID string `json:"section_id"`

	// Priority is the ordering weight (lower number = higher priority).
	// Security sections get priority 1; config sections get priority 5.
	Priority int `json:"priority"`

	// Reason is a human-readable explanation of why this section is dirty.
	Reason string `json:"reason"`
}

// ────────────────────────────────────────────────────────────────────────────
// DocumentResult — the outcome of processing one document section
// ────────────────────────────────────────────────────────────────────────────

// DocumentResult is returned per-section by the rendering engine.
type DocumentResult struct {
	DocID      string        `json:"doc_id"`
	SectionID  string        `json:"section_id"`
	DocPath    string        `json:"doc_path"`
	Changed    bool          `json:"changed"`
	RenderMode string        `json:"render_mode"` // "llm" | "deterministic" | "skipped"
	TokensUsed int           `json:"tokens_used"`
	Duration   time.Duration `json:"duration"`
	Err        error         `json:"-"`
}
