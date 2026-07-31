# GlassMarble — Industry-Standard Master Improvement Plan

> **Author Perspective:** Senior Go Engineer — expertise in code analysis tooling, AST/graph systems, Tree-sitter ecosystem, Language Server Protocol internals (Sourcegraph, CodeQL).
>
> **Date:** 2026-07-23 | **Version:** 1.0

---

## Table of Contents

1. [Executive Summary](#1-executive-summary)
2. [Current State Audit](#2-current-state-audit)
3. [Track A — Language Analysis Precision](#track-a)
4. [Track B — GAST Semantic Depth](#track-b)
5. [Track C — Cross-File Symbol Resolution](#track-c)
6. [Track D — CPG Linker Accuracy](#track-d)
7. [Track E — AKG Intelligence and Reasoning](#track-e)
8. [Track F — Visualization Engine Precision](#track-f)
9. [Track G — New Diagram Types](#track-g)
10. [Track H — CLI and UX Excellence](#track-h)
11. [Track I — Performance and Scalability](#track-i)
12. [Track J — Testing and Quality Gates](#track-j)
13. [Track K — Architecture Completion](#track-k)
14. [Implementation Roadmap — 4 Milestones](#implementation-roadmap)
15. [File-by-File Specifications](#file-by-file-specifications)

---

## 1. Executive Summary

GlassMarble is an AI Architecture Intelligence Platform implemented as a Go CLI. Its core capability: parse source code using Tree-sitter, build a Code Property Graph (CPG) persisted as a W3C RDF Turtle database (AKG), and project that graph into 21 architectural diagrams (14 UML + 7 C4) in Mermaid.js format.

The initial version works end-to-end. However, critical gaps prevent reliable use on real-world enterprise codebases. Problems concentrate in three areas:

1. **Analysis Quality** — Shallow token extraction, minimal semantic understanding, poor cross-file resolution, and primitive detection that only works on obvious keyword matches.
2. **Visualization Precision** — Diagrams are structurally correct but semantically shallow. Many produce near-identical output regardless of type. Several produce empty output for most codebases.
3. **Robustness** — No progress feedback, no caching, no CLI `analyze` command, and `code.go` is a 13-line stub with 7 commented-out commands.

This plan defines 11 independent improvement tracks with specific, file-level changes to reach industry-standard quality.

---

## 2. Current State Audit

### Priority Legend

| Priority | Description |
|----------|-------------|
| P0 Critical | Breaks core functionality for most real codebases |
| P1 High | Significantly limits accuracy or usability |
| P2 Medium | Improves quality and coverage |
| P3 Enhancement | New capabilities and polish |

---

### 2.1 Stage 1 — Tree-sitter Ingestion (`internal/code_analysis_engine/stage1/`)

| Gap | Severity | Root Cause |
|-----|----------|------------|
| No Rust grammar wired | P0 | `LangRust` declared in `type.go` but no `LanguageSpec` in `languages.go` |
| Ruby imports use wrong node type strings | P0 | `require`/`require_relative` are call expressions, not tree-sitter node types |
| HTML grammar fundamentally wrong | P1 | Declarations=`element`, Calls=`element` — meaningless GAST output |
| JSON grammar misused | P1 | JSON has no functions/imports/calls — mapping produces pure noise |
| `MaxFileBytes` never enforced | P2 | Field exists in `Config` but `Discover()` never checks file size |
| `IncludeHidden` flag ignored | P2 | Walker skips hidden files regardless of the flag |
| `nodeName` fallback dumps full body | P2 | Final fallback `n.Utf8Text(source)` returns entire node including braces |
| PHP namespace imports incomplete | P2 | `include`/`require` are call expressions — not captured |
| C++ header vs source not distinguished | P3 | `.h` interface declarations vs `.cpp` implementations treated identically |

### 2.2 Stage 2 — GAST Normalization (`internal/code_analysis_engine/stage2/`)

| Gap | Severity | Root Cause |
|-----|----------|------------|
| `findEnclosingFunctionID` always returns `defaultID` | P0 | `curr = -1` set on first iteration — parent chain never walked |
| `Properties["receiver_type"]` never set | P0 | Stage 4 call_linker.go reads this — NO translator writes it |
| `Properties["fully_qualified_name"]` never set | P0 | Stage 3 GlobalDefinitionIndex depends on this — never written |
| `Properties["namespace_scope"]` never set | P0 | Stage 3 export detection depends on this — never written |
| `PackageName` in FileSymbolTable never set | P0 | All 13 translators fail to extract package/module name |
| `GASTNamespace` nodes never emitted | P1 | Package/namespace structural nodes completely absent from GAST |
| `GASTField` nodes never emitted | P1 | Struct/class fields invisible — class diagrams show no member fields |
| `GASTParameter` nodes never emitted | P1 | Function parameters invisible — sequence diagrams cannot show arguments |
| Python visibility never detected | P1 | `_` and `__` prefix conventions not handled |
| Behavioral primitives match comments/strings | P1 | `DetectBehavioralPrimitives` includes docstrings in content to match |
| All 13 translators are minimal stubs | P1 | `go_translator.go` is 1542 bytes — declarations and imports only |
| Node ID separator `#` causes collisions | P2 | File paths containing `#` create malformed IDs |
| Generics completely ignored | P2 | TypeScript, Java, C# generic type params produce no information |

### 2.3 Stage 3 — Topology Aggregation (`internal/code_analysis_engine/stage3/`)

| Gap | Severity | Root Cause |
|-----|----------|------------|
| `GlobalDefinitionIndex` always empty | P0 | Depends on `Properties["fully_qualified_name"]` which is never written |
| `GlobalCallQueue` has malformed caller IDs | P0 | Caller IDs miss `::` separators — Stage 4 falls back to file-level attribution for ALL calls |
| `collectExportedGASTNodes` missing `fileRelPath` parameter | P1 | Cannot construct fallback FQN without the file path context |
| Empty directory nodes not pruned | P2 | Deleted files leave empty `DirectoryNode` entries → empty MODULE nodes in Stage 4 |

### 2.4 Stage 4 — CPG Linker (`internal/code_analysis_engine/stage4/`)

| Gap | Severity | Root Cause |
|-----|----------|------------|
| `receiverMatchesTarget` is heuristic-only | P0 | String contains matching — false positives on any node containing the substring |
| No `EXTENDS`/`INHERITS` edge type | P0 | Java extends, Python base classes, C# inheritance — not modeled at all |
| `LinkInterfacesAndRealizations` is O(n²) | P1 | GlobalDefinitionIndex empty → falls through to full prefix scan |
| CFG linker produces minimal output | P1 | `cfg_linker.go` (3082 bytes) — almost no CFG edges for real code |
| DFG linker is incomplete | P1 | `dfg_linker.go` (2449 bytes) — variable data flow not tracked |
| Concurrency linker is Go-only | P1 | Java/C#/Python/TypeScript/Rust concurrency patterns ignored |
| `BuildUniversalID` produces collisions | P1 | Two `Save()` in different files → same ID when receiver is empty |
| Generics in type relationships ignored | P2 | `type_linker.go` ignores generic parameters |

### 2.5 AKG — Architectural Knowledge Graph (`internal/akg/`)

| Gap | Severity | Root Cause |
|-----|----------|------------|
| Only 4 macro-inference rules | P1 | Insufficient for real enterprise pattern detection |
| No AKG schema version | P1 | Schema changes between versions cannot be migrated |
| `saveToDisk` is synchronous in transaction lock | P1 | Blocks all reads while writing large JSON + Turtle |
| Lock is fragile spin-wait | P1 | 3-second spin with 100ms sleep — stale lock files not cleaned on crash |
| WAL has no size limit | P2 | Crash-loop scenarios grow WAL unboundedly |
| No graph compression | P2 | 100k+ node graphs produce 500MB+ uncompressed files |

### 2.6 Visualization Engine (`internal/visualization_engine/`)

| Gap | Severity | Root Cause |
|-----|----------|------------|
| No depth-limited BFS in extractors | P0 | `QueryOptions.MaxDepth` declared but never implemented — extracts full graph |
| Mermaid output not sanitized | P0 | Names with `"`, `()`, `<>`, `[]` break Mermaid.js syntax |
| All C4 levels render identically | P0 | C4Context/C4Container/C4Component/C4Code call same layout function |
| `extractClassSubgraph` includes ALL executables | P1 | Large codebases → thousands of nodes → Mermaid.js cannot render |
| TTL file re-parsed on every command | P1 | No in-memory caching — slow for repeated `visualize` invocations |
| Dead component masking removes isolated classes | P1 | Utility classes with no edges pruned — appear missing from diagram |
| Sequence diagram entry point ID is opaque | P1 | Users cannot discover valid entry point IDs without an `inspect` command |
| ER Diagram type declared but renderer is stub | P2 | `ERDiagram` type exists but formatter has no ER-specific logic |
| `renderMindmapDiagram` produces flat list | P2 | Should reflect directory/module hierarchy |

### 2.7 CLI Layer (`cmd/`)

| Gap | Severity | Root Cause |
|-----|----------|------------|
| No `analyze` command | P0 | No way to run Stage 1→4 + AKG update from CLI |
| No `init` command | P0 | No way to initialize a repository for GlassMarble |
| `code.go` is a 13-line stub | P0 | 7 commands commented as placeholders |
| No progress feedback in `visualize` | P1 | 10+ second operations with no output — users assume CLI hung |
| No `inspect` command | P1 | Cannot discover valid node IDs or query AKG |
| No output format options | P2 | Only Mermaid.js — no PlantUML, DOT, or JSON |

### 2.8 Cross-Cutting Architecture Gaps

| Gap | Severity |
|-----|----------|
| `internal/logger/` does not exist | P0 |
| `internal/config/` does not exist | P0 |
| `internal/errors/` does not exist | P1 |
| `internal/terminal/` does not exist | P1 |
| `internal/app/` bootstrap package missing | P1 |
| No benchmark tests | P2 |
| `main.go` lacks signal handling | P2 |

---

## Track A — Language Analysis Precision

**Goal:** Stage 1 produces semantically correct, complete token extraction for all 13 supported languages plus Rust.

### A.1 [P0] Fix Ruby Import Detection

**File:** `internal/code_analysis_engine/stage1/languages.go`

Ruby's `require` and `require_relative` are **call expressions** in the tree-sitter grammar, not node type strings. Change:

```go
// WRONG (current):
Imports: []string{"require", "require_relative"},

// CORRECT:
Imports: []string{"call"},  // All calls — filter by content in RubyTranslator
```

`RubyTranslator.CoerceToken()` inspects `tok.Content` to classify require calls as imports.

### A.2 [P0] Wire Rust Grammar

**Files:** `internal/code_analysis_engine/stage1/languages.go`, `go.mod`

Add `github.com/tree-sitter/tree-sitter-rust/bindings/go` to `go.mod` and register:

```go
{
    Lang:       LangRust,
    Extensions: []string{".rs"},
    NewLanguage: asLang(tree_sitter_rust.Language),
    Declarations: []string{
        "function_item", "impl_item", "struct_item",
        "enum_item", "trait_item", "type_item",
        "mod_item", "static_item", "const_item",
    },
    Imports: []string{"use_declaration", "extern_crate_declaration"},
    Calls:   []string{"call_expression", "macro_invocation"},
},
```

Add corresponding `RustTranslator` in Stage 2.

### A.3 [P1] Fix HTML Grammar Mapping

**File:** `internal/code_analysis_engine/stage1/languages.go`

HTML is not a programming language. Extract structurally meaningful nodes only:

```go
{
    Lang:        LangHTML,
    Extensions:  []string{".html", ".htm", ".xhtml"},
    NewLanguage: asLang(tree_sitter_html.Language),
    Declarations: []string{"doctype", "script_element", "style_element", "element"},
    Imports: []string{"attribute"},  // src/href attributes — filtered in translator
    Calls:   []string{},
},
```

### A.4 [P1] Fix JSON — Config-Format-Only Extraction

Add `IsConfigFormat bool` to `LanguageSpec`. JSON should only extract: `"dependencies"` as package imports, `"scripts"` as operations, `"name"`/`"version"` as identity, OpenAPI `"paths"` as API endpoints.

### A.5 [P2] Enforce MaxFileBytes / IncludeHidden

**File:** `internal/code_analysis_engine/stage1/walker.go`

```go
// MaxFileBytes enforcement:
info, err := entry.Info()
if err != nil { continue }
if cfg.MaxFileBytes > 0 && info.Size() > cfg.MaxFileBytes {
    skipped = append(skipped, entry.Name()+" (exceeds size limit)")
    continue
}

// IncludeHidden enforcement:
if !cfg.IncludeHidden && strings.HasPrefix(entry.Name(), ".") {
    return filepath.SkipDir
}
```

### A.6 [P2] Fix nodeName Fallback

**File:** `internal/code_analysis_engine/stage1/parser.go`

```go
text := n.Utf8Text(source)
if len(text) > 64 || strings.ContainsAny(text, "\n\r{};()") {
    return ""
}
return text
```

### A.7 [P2] Fix PHP Import Detection

Add `namespace_use_clause` to PHP imports. Handle `include`/`require`/`include_once`/`require_once` in `PHPTranslator` by inspecting call expression content.

### A.8 [P2] C++ Header vs Source Distinction

Add `HeaderExtensions []string` to `LanguageSpec`. Tag `.h`/`.hpp` declarations as `interface` kind and `.cpp`/`.cc` as `implementation` kind.

### A.9 [P3] Add YAML, TOML, Kotlin, Swift Grammars

- YAML/TOML: Kubernetes manifests, Docker Compose, Cargo.toml
- Kotlin: Android and Spring Boot backends
- Swift: iOS/macOS codebases

---

## Track B — GAST Semantic Depth

**Goal:** All 13 translators produce rich GAST nodes with visibility, type info, parameters, and fields.

### B.1 [P0] Fix findEnclosingFunctionID — Broken Parent Walk

**File:** `internal/code_analysis_engine/stage2/normalizer.go`

Change signature to accept raw tokens and walk the actual parent index chain:

```go
func findEnclosingFunctionID(nodes []*GASTNode, rawTokens []stage1.RawToken, tokenIdx int, defaultID string) string {
    curr := tokenIdx
    for curr >= 0 && curr < len(rawTokens) {
        if nodes[curr] != nil && nodes[curr].Type == GASTFunction {
            return nodes[curr].ID
        }
        curr = rawTokens[curr].ParentIdx  // Walk actual parent chain
    }
    return defaultID
}
```

### B.2 [P0] Emit Properties["receiver_type"] in All Translators

Every method node must have `Properties["receiver_type"]` set. For Go:

```go
// In GoTranslator.CoerceToken for method_declaration:
node.Properties["receiver_type"] = extractReceiverType(tok.Content)
node.ReceiverType = extractReceiverType(tok.Content)
```

### B.3 [P0] Populate FileSymbolTable.PackageName

Every translator must detect and set package/namespace name:
- Go: `package main` → `"main"`
- Java: `package com.example` → `"com.example"`
- C#: `namespace Company.Module` → `"Company.Module"`
- Python: Derived from file path relative to package root

### B.4 [P0] Emit GASTNamespace Nodes

Every translator must emit a `GASTNamespace` node for the package/namespace declaration. Critical for component and container diagram boundaries.

### B.5 [P0] Set Properties["fully_qualified_name"]

Every `GASTTypeDeclaration` and `GASTFunction` node:

```go
fqn := packageName + "." + receiverType + "." + name
node.Properties["fully_qualified_name"] = fqn
```

### B.6 [P0] Set Properties["namespace_scope"]

```go
if node.Visibility == "public" || node.Visibility == "exported" {
    node.Properties["namespace_scope"] = "exported"
} else {
    node.Properties["namespace_scope"] = "internal"
}
```

### B.7 [P1] Emit GASTField and GASTParameter Nodes

Fields and parameters are completely invisible. Required for:
- Class diagrams (member fields with types)
- ER diagrams (entity attributes)
- Sequence diagrams (parameter passing)

Each translator must emit field and parameter child nodes.

### B.8 [P1] Add Python Visibility Detection

```go
func pythonVisibility(name string) string {
    if strings.HasPrefix(name, "__") && !strings.HasSuffix(name, "__") {
        return "private"
    }
    if strings.HasPrefix(name, "_") {
        return "protected"
    }
    return "public"
}
```

### B.9 [P1] Strip Comments Before Primitive Detection

**File:** `internal/code_analysis_engine/stage2/primitives.go`

```go
func DetectBehavioralPrimitives(content, name string) []BehavioralPrimitive {
    stripped := stripCommentsAndStrings(content)  // New
    lower := strings.ToLower(stripped + " " + name)
    // ... rest unchanged
}
```

### B.10 [P2] Generics Detection (TypeScript/Java/C#)

Capture generic type parameters in `DataType` and `Properties["type_params"]`.

### B.11 [P2] Fix Node ID Separator

**File:** `internal/code_analysis_engine/stage2/translator.go`

```go
// From:
id := fmt.Sprintf("%s#%s#%s#L%d", fileRelPath, tok.Kind, name, tok.StartLine)
// To:
id := fmt.Sprintf("%s::%s::%s::L%d", fileRelPath, tok.Kind, name, tok.StartLine)
```

---

## Track C — Cross-File Symbol Resolution

**Goal:** Stage 3 produces a populated `GlobalDefinitionIndex` and accurate `GlobalCallQueue`.

### C.1 [P0] Fix GlobalDefinitionIndex Population

**File:** `internal/code_analysis_engine/stage3/aggregator.go`

After B.5 is implemented, add fallback FQN construction:

```go
func collectExportedGASTNodes(node *stage2.GASTNode, fileRelPath string, index map[string]*stage2.GASTNode) {
    if node == nil { return }
    if node.Type == stage2.GASTTypeDeclaration || node.Type == stage2.GASTFunction {
        if fqn := node.Properties["fully_qualified_name"]; fqn != "" {
            index[fqn] = node
        }
        // Fallback: path-based FQN
        dir := strings.ReplaceAll(filepath.Dir(fileRelPath), "/", ".")
        if dir != "." {
            index[dir + "." + node.Name] = node
        }
    }
    for _, child := range node.Children {
        collectExportedGASTNodes(child, fileRelPath, index)
    }
}
```

### C.2 [P0] Fix GlobalCallQueue Caller IDs

After B.5, caller IDs must be FQNs:

```go
for _, call := range symTable.LocalCalls {
    callerFQN := call.CallerNodeID
    if !strings.Contains(callerFQN, "::") {
        callerFQN = "file:" + NormalizeRelativePath(relPath)
    }
    // ... append to queue
}
```

### C.3 [P1] Import Path Resolution System

**File:** New — `internal/code_analysis_engine/stage3/import_resolver.go`

```go
type ImportResolver interface {
    Resolve(importPath, fromFile, rootDir string) []string
}
// Implementations: GoImportResolver, PythonImportResolver,
// JavaImportResolver, TSImportResolver
```

### C.4 [P1] Symbol Ownership Mapping

**File:** New — `internal/code_analysis_engine/stage3/ownership_map.go`

Reverse index: symbol name -> defining file(s). Enables call linker to resolve `db.Query` without perfect FQN.

```go
type OwnershipMap struct {
    ByName   map[string][]SymbolEntry
    ByImport map[string][]SymbolEntry
}
```

### C.5 [P2] Prune Empty Directory Nodes

**File:** `internal/code_analysis_engine/stage3/mutator.go`

```go
func pruneEmptyDirectories(dir *DirectoryNode) bool {
    for name, sub := range dir.SubFolders {
        if pruneEmptyDirectories(sub) { delete(dir.SubFolders, name) }
    }
    return len(dir.Files) == 0 && len(dir.SubFolders) == 0
}
```

---

## Track D — CPG Linker Accuracy

**Goal:** Stage 4 produces accurate call graphs, type hierarchies, and behavioral edges for all 13 languages.

### D.1 [P0] Replace Heuristic Receiver Matching

**File:** `internal/code_analysis_engine/stage4/call_linker.go`

Resolution order using the ownership map (C.4):
1. Direct import path resolution via `OwnershipMap.ByImport`
2. Same-package match against nodes with matching package name
3. `GlobalDefinitionIndex` FQN lookup
4. Heuristic fallback (current approach, lowest confidence)

### D.2 [P0] Add EXTENDS and Inheritance Edge Types

**File:** `internal/code_analysis_engine/stage4/type.go`

```go
const (
    EdgeExtends   RelationshipType = "EXTENDS"     // Class/struct inheritance
    EdgeMixes     RelationshipType = "MIXES"       // Ruby include Module, Python mixin
    EdgeHasField  RelationshipType = "HAS_FIELD"   // Struct/class field
    EdgeHasParam  RelationshipType = "HAS_PARAM"   // Function parameter
    EdgeReturns   RelationshipType = "RETURNS"     // Function return type
    EdgeThrows    RelationshipType = "THROWS"      // Exception emission
    EdgeDependsOn RelationshipType = "DEPENDS_ON"  // Package import dependency
)
```

### D.3 [P1] Fix CFG Linker

Implement real intra-function control flow:
- Sequential statements: `CFG_FLOW` edges
- `if/else`: `CFG_IF` edges
- `for/while`: `CFG_LOOP` edges
- `switch/match`: `CFG_SWITCH` edges
- `return`/`throw`: terminal flow

### D.4 [P1] Fix DFG Linker

Track variable data flow:
- Variable assignments → `DATA_FLOW` source to destination
- Function parameter → return value flow
- Database read → handler function flow (taint analysis foundation)

### D.5 [P1] Language-Specific Concurrency Patterns

| Language | Patterns to Detect |
|----------|-------------------|
| Go | `go func()`, `chan<-`, `sync.WaitGroup` |
| Java | `new Thread()`, `ExecutorService.submit()`, `CompletableFuture` |
| Python | `threading.Thread()`, `asyncio.create_task()`, `concurrent.futures` |
| TypeScript | `Promise`, `async/await`, `setTimeout`, `Worker` |
| C# | `Task.Run()`, `async/await`, `Thread`, `Parallel.For` |
| C++ | `std::thread`, `std::async`, OpenMP `#pragma omp` |
| Rust | `thread::spawn()`, `tokio::spawn()`, `async fn` |

### D.6 [P1] Collision-Safe Node Registration

**File:** `internal/code_analysis_engine/stage4/builder.go`

```go
func (b *InitialGraphBuilder) registerNode(id string, node *ResolvedNode) string {
    finalID := id
    for counter := 1; ; counter++ {
        if _, exists := b.output.GraphNodes[finalID]; !exists { break }
        finalID = fmt.Sprintf("%s#%d", id, counter)
    }
    b.output.GraphNodes[finalID] = node
    return finalID
}
```

### D.7 [P2] Language-Specific Interface Matchers

Per-language `InterfaceMatcher` for duck typing:
- Go: Implicit structural interface satisfaction
- Python: Abstract base class `ABC` + `@abstractmethod`
- TypeScript: Structural type compatibility

### D.8 [P3] Call Confidence Scoring

Add `Confidence float32` to `ResolvedEdge`: `1.0` = direct FQN match, `0.7` = import-resolved, `0.5` = same-package heuristic, `0.3` = string heuristic fallback.

---

## Track E — AKG Intelligence and Reasoning

**Goal:** Richer architectural inference with production-grade persistence and durability.

### E.1 [P1] Expand Macro-Inference Rules to 20+

**File:** `internal/akg/reasoner.go`

**Layered Architecture:**
- Repository Pattern: `Repository`/`Repo` name + DATABASE primitive
- Service Layer: `Service` name calling Repository nodes
- Controller Layer: HTTP primitive nodes calling Service nodes
- Gateway Pattern: Nodes with multiple external NETWORK_IO calls

**Event-Driven:**
- Event Publisher: Nodes dispatching to a message queue
- Event Consumer: Nodes consuming from a queue
- CQRS Pattern: Separate read-model and write-model commands

**Infrastructure:**
- Circuit Breaker: Retry + timeout + fallback sequence
- Cache-Aside: DATABASE + cache reads in same function
- Saga Orchestrator: Multi-step distributed transaction coordination

**Security:**
- Authentication Middleware: Auth check before business logic
- Input Validation Gate: Sanitization before storage
- Secret Manager Access: Credential/config store interactions

**Observability:**
- Metrics Emitter: Prometheus/StatsD integration
- Distributed Tracer: OpenTelemetry span creation
- Structured Logger: Context-bearing log emission

### E.2 [P1] Add Schema Version

**File:** `internal/akg/mvcc.go`

```go
type CodePropertyGraph struct {
    SchemaVersion int `json:"schema_version"`
    // ... rest unchanged
}
const CurrentSchemaVersion = 2
```

Add migration logic in `loadFromDisk` for old schema versions.

### E.3 [P2] Async Disk Persistence

Move `saveToDisk` to background goroutine after atomic snapshot promotion:

```go
tm.container.PromoteShadowSnapshot(shadow)
go func(g *CodePropertyGraph) {
    if err := tm.saveToDisk(g); err != nil { /* log error */ }
}(shadow.Clone())
```

### E.4 [P2] WAL Size Limit and Rotation

100MB WAL limit with automatic checkpoint-on-overflow. Keep last 2 WAL segments for crash recovery.

### E.5 [P2] Graph Compression

Gzip compression for `akg_state.json.gz` for large graphs (>10k nodes).

### E.6 [P2] Replace Spin-Wait Lock with PID-Based Lock

Write PID to lock file. On acquisition failure, check if PID is alive (stale lock detection). Exponential backoff up to 500ms intervals with 10-second deadline.

### E.7 [P3] ArchitecturalSummary in CodePropertyGraph

```go
type ArchitecturalSummary struct {
    PrimaryPatterns   []string       `json:"primary_patterns"`
    LayerDistribution map[string]int `json:"layer_distribution"`
    HotspotNodes      []string       `json:"hotspot_nodes"`
    EntryPoints       []string       `json:"entry_points"`
    ExternalDeps      []string       `json:"external_deps"`
    GeneratedAt       time.Time      `json:"generated_at"`
}
```

### E.8 [P3] Incremental Re-Reasoning

Re-run macro-inference only on nodes reachable from changed files. Avoids full-graph re-inference on small commits.

---

## Track F — Visualization Engine Precision

**Goal:** All 21 diagrams produce semantically accurate, non-overlapping, renderable Mermaid.js output.

### F.1 [P0] Depth-Limited BFS in All Extractors

**File:** `internal/visualization_engine/stage1/extractor.go`

```go
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
    for depth := 0; len(queue) > 0 && depth <= maxDepth; depth++ {
        var next []string
        for _, id := range queue {
            if visited[id] { continue }
            visited[id] = true
            if n, ok := allNodes[id]; ok { sub.Nodes[id] = n }
            for _, e := range allEdges {
                if e.SourceID == id && edgeFilter(e) {
                    sub.Edges = append(sub.Edges, e)
                    next = append(next, e.TargetID)
                }
            }
        }
        queue = next
    }
    return sub
}
```

### F.2 [P0] Mermaid Output Sanitization

**File:** `internal/visualization_engine/stage3/formatter.go`

```go
func sanitizeMermaidLabel(s string) string {
    s = strings.ReplaceAll(s, `"`, "'")
    s = strings.ReplaceAll(s, "()", "")
    s = strings.ReplaceAll(s, "<", "~")
    s = strings.ReplaceAll(s, ">", "~")
    s = strings.ReplaceAll(s, "[", "(")
    s = strings.ReplaceAll(s, "]", ")")
    if len(s) > 60 { s = s[:57] + "..." }
    return s
}
```

Apply to every node label in every formatter function.

### F.3 [P0] Semantic C4 Level Differentiation

Each C4 level must filter different node kinds:

| C4 Level | Node Kinds | Mermaid Output |
|----------|------------|----------------|
| C4 Context (L1) | `gm:ExternalSystem`, `gm:User`, system boundary | `C4Context` diagram |
| C4 Container (L2) | `gm:Module` (directories/services) | `C4Container` diagram |
| C4 Component (L3) | `gm:TypeDecl`, `gm:Executable` | `C4Component` diagram |
| C4 Code (L4) | `gm:Member`, `gm:Field` | UML Class diagram |

### F.4 [P1] Scope Filter for Class Diagrams

Add to `QueryOptions`:

```go
type QueryOptions struct {
    EntryPointID  string
    MaxDepth      int
    IncludeUnused bool
    ScopePrefix   string  // Filter to specific package/directory prefix
    MaxNodes      int     // Hard cap on output nodes (default 50)
    DiagramFocus  string  // "types", "functions", "all"
}
```

### F.5 [P1] Auto-Discover Sequence Diagram Entry Points

When `EntryPointID` is empty, auto-detect zero in-degree callable nodes (HTTP handlers, `main` functions, exported public functions with no callers).

### F.6 [P1] Unique Rendering Logic Per Diagram Type

- **Sequence:** Actors from unique callers/receivers, messages ordered by LineNumber, activation boxes
- **Activity:** `([*])` start/end, `{condition}` diamonds from CFG_IF edges, fork/join from SPAWNS_CONCURRENT
- **State:** State nodes from handler-named functions, transitions from CFG edges
- **Composite:** Internal structure with ports and parts
- **Profile:** Architectural stereotypes from MacroRules: `<<Repository>>`, `<<Service>>`, `<<Controller>>`
- **ER:** Entity boxes with typed fields, FK relationships from REFERENCES edges

### F.7 [P1] Mindmap Directory Hierarchy

Build proper directory-based hierarchy instead of flat list:

```
mindmap
  root((Repository))
    (internal)
      (services)
        UserService
        AuthService
      (repository)
        PostgresStore
    (cmd)
      main
```

### F.8 [P2] ER Diagram Renderer

```
erDiagram
  USER { string id PK, string email, string name }
  ORDER { string id PK, string userId FK, datetime createdAt }
  USER ||--o{ ORDER : "places"
```

Extract from `gm:TypeDecl` nodes with `gm:Member` field children and `gm:REFERENCES` edges.

### F.9 [P2] TTL File Caching

Cache parsed TTL graph in memory keyed by `ttlPath + file mtime`. Multiple `visualize` commands re-use cached graph.

---

## Track G — New Diagram Types

**Goal:** High-value diagram types that deliver differentiated value beyond existing tools.

### G.1 [P1] Dependency Graph Diagram

```
glassmarble visualize dependency
```

Shows file/package import dependencies as a directed graph.

### G.2 [P1] Hotspot / Complexity Diagram

Shows most-depended-upon nodes with visual emphasis (size/color by in-degree).

### G.3 [P2] Call Graph Diagram

Focused execution tree from a specific entry point — shows the full call chain depth-first.

### G.4 [P2] Layered Architecture Diagram

Clusters nodes by inferred architectural layer:

```
graph TB
    subgraph Presentation
        Handler[HTTP Handler]
    end
    subgraph Business
        UserSvc[User Service]
    end
    subgraph Data
        UserRepo[User Repository]
    end
    Handler --> UserSvc --> UserRepo
```

### G.5 [P2] Change Impact Diagram

Given changed files (from `git diff`), show all transitively affected components.

### G.6 [P3] Infrastructure Diagram

Detect database, cache, queue, and external API integrations and produce a C4 Context-style infrastructure map.

---

## Track H — CLI and UX Excellence

**Goal:** Implement the full CLI command surface with progress feedback and shell integration.

### H.1 [P0] analyze Command

**File:** `cmd/analyze.go` (new)

```
glassmarble analyze [--dir .] [--commit HEAD] [--full] [--workers N] [--verbose]
```

Runs Stage 1 → 2 → 3 → 4 → AKG update. Outputs:
```
Analyzed 127 files | 1,832 nodes | 5,211 edges | 2.3s
```

### H.2 [P0] init Command

**File:** `cmd/init.go` (new)

```
glassmarble init [--dir .]
```

Creates `.glassmarble/` with `config.yaml` and empty AKG state.

### H.3 [P1] status Command

**File:** `cmd/status.go` (new)

```
$ glassmarble status
  AKG State:  Loaded (v42)
  Commit:     abc123f (2024-01-15 14:32)
  Nodes:      1,832 | Edges: 5,211 | Files: 127
  Languages:  go(85), typescript(31), python(11)
  Health:     3 dangling references
```

### H.4 [P1] Progress Feedback in visualize

**File:** `cmd/visualize.go`

Add spinner and progress messages using `internal/terminal/`:

```
Parsing AKG database... (14,892 nodes)
Extracting subgraph... (247 nodes)
Building layout tree...
Rendering UML Class Diagram...
Done in 1.2s
```

### H.5 [P1] inspect Command

**File:** `cmd/inspect.go` (new)

```
glassmarble inspect --list                  # List entry points
glassmarble inspect --search "UserService"  # Search by name
glassmarble inspect --type class            # Filter by type
glassmarble inspect node <node-id>          # Full node details
```

Solves the sequence diagram entry point discovery problem.

### H.6 [P1] diff Command

**File:** `cmd/diff.go` (new)

```
glassmarble diff HEAD~1 HEAD
  + Added:    UserService.CreateUser
  - Removed:  LegacyAuthHandler
  ~ Modified: DatabasePool (new NETWORK_IO primitive)
  + New edge: UserService -> EmailNotifier
```

### H.7 [P2] tree Command

**File:** `cmd/tree.go` (new)

```
glassmarble tree [--depth 3]
  src/
  +-- services/
  |   +-- UserService  [CLASS, NETWORK_IO, DATABASE]
  |   +-- AuthService  [CLASS, NETWORK_IO]
  +-- repository/
      +-- PostgresStore  [CLASS, DATABASE]
```

### H.8 [P2] dependency Command

**File:** `cmd/dependency.go` (new)

```
glassmarble dependency services/user.go
  Direct imports:
    -> repository/user_repo.go
    -> pkg/jwt/validator.go
  Transitive imports:
    -> models/user.go (via user_repo.go)
```

### H.9 [P2] hotspot Command

**File:** `cmd/hotspot.go` (new)

```
glassmarble hotspot [--top 10]
  Rank  Node                       In-Degree  Primitives
  1.    services/UserService.go    42         NETWORK_IO, DATABASE
  2.    middleware/auth.go         38         NETWORK_IO
```

### H.10 [P2] PlantUML Output Format

```
glassmarble visualize class --format plantuml
```

### H.11 [P3] Shell Completion, Git Hooks, watch Command

- `glassmarble completion bash/zsh/fish`
- `glassmarble hooks install/uninstall` — post-commit auto-analysis
- `glassmarble watch [--interval 5s]` — continuous monitoring

---

## Track I — Performance and Scalability

**Goal:** Analyze enterprise codebases (100k+ LOC) in under 60 seconds.

### I.1 [P1] Streaming File Discovery

**File:** `stage1/walker.go` — stream discovered paths to worker channel instead of collecting all first.

### I.2 [P1] Parallel Stage 4 Linker Execution

**File:** `stage4/linker.go` — run independent linkers concurrently with per-linker edge buffers merged after completion.

### I.3 [P1] AKG Clone Copy-on-Write Optimization

**File:** `akg/mvcc.go` — share read-only map data; copy only modified segments instead of full deep copy on every transaction.

### I.4 [P2] Stage 2 Parallel File Translation

**File:** `stage2/normalizer.go` — process files concurrently using a bounded goroutine pool.

### I.5 [P2] TTL Incremental Write

**File:** `akg/transaction_manager.go` — append only changed triples; compact when delta exceeds 20% of base.

### I.6 [P3] Source Map B-Tree Index

Pre-build a B-tree index over line numbers for sub-millisecond symbol lookup needed by `inspect` command.

---

## Track J — Testing and Quality Gates

**Goal:** High test coverage, benchmarks, and CI quality gates preventing regressions.

### J.1 [P1] Unit Tests for All 13 Translators

Each translator test verifies: type extraction, method extraction with `receiver_type`, import detection, visibility, FQN generation, behavioral primitive accuracy.

### J.2 [P1] Unit Tests for All Stage 4 Linkers

Test cases: direct call resolution, cross-file resolution via imports, interface dispatch, inheritance detection, circular dependency detection.

### J.3 [P1] Full Pipeline Integration Tests

Test the complete Stage 1→2→3→4 pipeline with synthetic repositories (Go, Python, TypeScript, Java) with known structure. Verify specific edges exist in output CPG.

### J.4 [P1] Visualization Snapshot Tests

For each of 21 diagram types: load fixture AKG from `testdata/`, generate diagram, compare against golden file snapshot.

### J.5 [P2] Benchmark Suite

```go
func BenchmarkStage1_Ingestion(b *testing.B)
func BenchmarkStage2_Normalize(b *testing.B)
func BenchmarkStage4_LinkCallGraph(b *testing.B)
func BenchmarkAKG_ExecuteDeltaTransaction(b *testing.B)
func BenchmarkVisualize_ClassDiagram(b *testing.B)
```

### J.6 [P2] Fuzz Testing for Parser

Parser must never panic regardless of input content.

### J.7 [P3] CI Coverage Gate

Fail CI if test coverage drops below 70%.

---

## Track K — Architecture Completion

**Goal:** Create all missing infrastructure packages referenced in the architecture document.

### K.1 [P0] Create internal/logger/ Package

Structured logging using Go standard `log/slog`. Methods: `Info`, `Debug`, `Error`, `Warn`. Debug level controlled by `--debug` flag.

### K.2 [P0] Create internal/config/ Package

```go
type Config struct {
    RootDir       string `yaml:"root_dir"`
    WorkerCount   int    `yaml:"worker_count"`
    MaxFileBytes  int64  `yaml:"max_file_bytes"`
    Debug         bool   `yaml:"debug"`
    StorageDir    string `yaml:"storage_dir"`
    OutputFormat  string `yaml:"output_format"`  // "mermaid", "plantuml", "dot"
    IncludeHidden bool   `yaml:"include_hidden"`
}
```

Merge precedence: `--flags > GLASSMARBLE_* env vars > .glassmarble/config.yaml > ~/.glassmarble/config.yaml > defaults`

### K.3 [P1] Create internal/errors/ Package

```go
var (
    ErrNoAKGDatabase     = errors.New("AKG not found -- run: glassmarble analyze")
    ErrUnsupportedLang   = errors.New("language not supported")
    ErrParseFailure      = errors.New("file parsing failed")
    ErrLockTimeout       = errors.New("database lock timeout")
    ErrSchemaVersion     = errors.New("incompatible AKG schema version -- re-run analyze")
    ErrInvalidEntryPoint = errors.New("entry point not found in graph")
    ErrEmptyGraph        = errors.New("AKG has no nodes -- run analyze first")
)
```

### K.4 [P1] Create internal/terminal/ Package

Non-TTY detection, `NO_COLOR` env var support, spinner animation, progress bars, color helpers.

### K.5 [P1] Create internal/app/ Bootstrap Package

```go
type App struct {
    Config *config.Config
    Logger *logger.Logger
    AKG    *akg.AKGTransactionManager
}

func New(rootDir string, debug bool) (*App, error)
```

### K.6 [P2] Wire App into All Commands via PersistentPreRunE

Each command's `RunE` retrieves the `App` via `cmd.Context()`.

### K.7 [P2] Signal Handling and Graceful Shutdown

```go
func main() {
    ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
    defer stop()
    if err := cmd.Execute(ctx); err != nil { os.Exit(1) }
}
```

---

## Implementation Roadmap

### Milestone 1 — Foundation Fixes (Weeks 1–3)
*Make the pipeline produce accurate output for any Go/Python/Java codebase.*

| # | Track | Item | Target Files |
|---|-------|------|-------------|
| 1 | B | Fix findEnclosingFunctionID parent walk | `stage2/normalizer.go` |
| 2 | B | Emit receiver_type in all translators | All `stage2/*_translator.go` |
| 3 | B | Populate PackageName in all translators | All `stage2/*_translator.go` |
| 4 | B | Emit GASTNamespace nodes | All `stage2/*_translator.go` |
| 5 | B | Set fully_qualified_name property | All `stage2/*_translator.go` |
| 6 | B | Set namespace_scope property | All `stage2/*_translator.go` |
| 7 | C | Fix GlobalDefinitionIndex population | `stage3/aggregator.go` |
| 8 | C | Fix GlobalCallQueue caller IDs | `stage3/aggregator.go` |
| 9 | A | Fix Ruby import node types | `stage1/languages.go` |
| 10 | A | Wire Rust grammar | `stage1/languages.go`, `go.mod` |
| 11 | F | Mermaid output sanitization | `stage3/formatter.go` |
| 12 | K | Create logger package | `internal/logger/` |
| 13 | K | Create config package | `internal/config/` |
| 14 | H | Implement analyze command | `cmd/analyze.go` |
| 15 | H | Implement init command | `cmd/init.go` |

### Milestone 2 — Semantic Accuracy (Weeks 4–6)
*Accurate multi-language diagrams with real cross-file linking.*

| # | Track | Item | Target Files |
|---|-------|------|-------------|
| 16 | B | Emit Field and Parameter nodes | All translators |
| 17 | B | Python visibility detection | `python_translator.go` |
| 18 | B | Strip comments from primitive detection | `primitives.go` |
| 19 | C | Import path resolver | `stage3/import_resolver.go` (new) |
| 20 | C | Ownership map | `stage3/ownership_map.go` (new) |
| 21 | D | Replace heuristic receiver matching | `stage4/call_linker.go` |
| 22 | D | Add EXTENDS/inheritance edges | `stage4/type.go`, `type_linker.go` |
| 23 | D | Language-specific concurrency patterns | `stage4/concurrency_linker.go` |
| 24 | E | Expand macro-inference to 20+ rules | `akg/reasoner.go` |
| 25 | F | BFS depth limiting in all extractors | `visualization_engine/stage1/extractor.go` |
| 26 | F | C4 semantic differentiation | `stage3/formatter.go` |
| 27 | F | Class diagram scope filter + MaxNodes | `extractor.go`, `types.go` |
| 28 | F | Sequence auto-discover entry points | `extractor.go` |
| 29 | H | Implement status command | `cmd/status.go` |
| 30 | H | Implement inspect command | `cmd/inspect.go` |
| 31 | H | Progress feedback in visualize | `cmd/visualize.go` |
| 32 | K | Create errors package | `internal/errors/` |
| 33 | K | Create terminal package | `internal/terminal/` |
| 34 | K | Create app bootstrap package | `internal/app/` |

### Milestone 3 — Full Diagram Coverage (Weeks 7–9)
*All 21 diagram types produce unique, semantically accurate output.*

| # | Track | Item |
|---|-------|------|
| 35–38 | A | Fix HTML/JSON/PHP/C++ grammar issues |
| 39–40 | B | Generics detection; fix node ID separator |
| 41 | C | Prune empty directories |
| 42–44 | D | Fix CFG/DFG linkers; collision-safe registerNode |
| 45–47 | E | Schema version + migration; async persistence; WAL rotation |
| 48–51 | F | Differentiated rendering; mindmap hierarchy; ER diagram; TTL cache |
| 52–53 | G | Dependency graph; hotspot diagram |
| 54–57 | H | diff, tree, dependency, hotspot commands |
| 58–59 | I | Streaming discovery; parallel Stage 4 linkers |
| 60–63 | J | Unit tests for translators, linkers; integration tests; snapshot tests |

### Milestone 4 — Enterprise and Polish (Weeks 10–12)
*Production-grade tooling, advanced features, comprehensive testing, CI/CD integration.*

| # | Track | Item |
|---|-------|------|
| 64–66 | A | YAML/Kotlin/Swift grammar support |
| 67–68 | D | Language-specific interface matchers; call confidence scoring |
| 69–72 | E | Graph compression; improved locking; ArchitecturalSummary; incremental re-reasoning |
| 73–76 | G | Call graph; layered architecture; change impact; infrastructure diagrams |
| 77–80 | H | PlantUML format; shell completion; git hooks; watch command |
| 81–83 | I | AKG clone COW optimization; parallel Stage 2; TTL incremental write |
| 84–86 | J | Benchmark suite; fuzz testing; CI coverage gate |
| 87–88 | K | Wire App into all commands; signal handling |

---

## File-by-File Specifications

### Stage 1

| File | Changes |
|------|---------|
| `stage1/languages.go` | A.1 Ruby, A.2 Rust, A.3 HTML, A.4 JSON, A.7 PHP, A.8 C++, A.9 YAML/Kotlin/Swift |
| `stage1/parser.go` | A.6 nodeName fallback fix; multi-error IngestionResult |
| `stage1/walker.go` | A.5 MaxFileBytes + IncludeHidden; I.1 streaming |
| `stage1/type.go` | Add Rust/Kotlin/Swift/YAML/TOML to SupportedLang |

### Stage 2

| File | Changes |
|------|---------|
| `stage2/normalizer.go` | B.1 fix parent walk; I.4 parallel translation |
| `stage2/translator.go` | B.11 fix separator # -> :: |
| `stage2/primitives.go` | B.9 comment stripping; expanded patterns |
| `stage2/go_translator.go` | B.2-B.6, B.10, B.11: all semantic properties + GASTNamespace + GASTField |
| `stage2/java_translator.go` | Same as Go; B.10 generics; abstract/final/static |
| `stage2/python_translator.go` | B.8 visibility; decorators; __init__; @dataclass |
| `stage2/typescript_translator.go` | B.10 generics; readonly/abstract; union types |
| `stage2/csharp_translator.go` | Namespaces; generics; nullable; record types |
| `stage2/ruby_translator.go` | A.1 require via content; include/extend/prepend |
| `stage2/cpp_translator.go` | A.8 header vs source distinction |
| `stage2/php_translator.go` | A.7 include/require detection |
| All other translators | B.2-B.6, B.10, B.11 for all 13 languages |

### Stage 3

| File | Changes |
|------|---------|
| `stage3/aggregator.go` | C.1 fix GlobalDefinitionIndex; C.2 fix caller IDs |
| `stage3/mutator.go` | C.5 pruneEmptyDirectories |
| `stage3/import_resolver.go` | C.3 new — per-language import path resolver |
| `stage3/ownership_map.go` | C.4 new — reverse symbol index |

### Stage 4

| File | Changes |
|------|---------|
| `stage4/type.go` | D.2 new edge types; D.8 Confidence field |
| `stage4/builder.go` | D.6 collision-safe registerNode |
| `stage4/call_linker.go` | D.1 replace heuristic with ownership map |
| `stage4/type_linker.go` | D.2 detect EXTENDS/MIXES per language |
| `stage4/interface_linker.go` | D.7 language-specific duck-typing matchers |
| `stage4/cfg_linker.go` | D.3 real CFG within function bodies |
| `stage4/dfg_linker.go` | D.4 variable data flow tracking |
| `stage4/concurrency_linker.go` | D.5 all-language concurrency patterns |
| `stage4/linker.go` | I.2 parallel linker execution |

### AKG

| File | Changes |
|------|---------|
| `akg/mvcc.go` | E.2 SchemaVersion; E.7 ArchitecturalSummary; I.3 COW clone |
| `akg/transaction_manager.go` | E.3 async persistence; E.5 gzip; E.6 PID lock; E.2 migration |
| `akg/reasoner.go` | E.1 expand to 20+ rules; E.8 incremental re-reasoning |
| `akg/wal.go` | E.4 size limit and rotation |

### Visualization Engine

| File | Changes |
|------|---------|
| `visualization_engine/types/types.go` | F.4 ScopePrefix/MaxNodes/DiagramFocus in QueryOptions |
| `visualization_engine/stage1/extractor.go` | F.1 bfsSubgraph helper; F.4 scope filter; F.5 entry point auto-discovery |
| `visualization_engine/stage2/aggregator.go` | F.4 MaxNodes enforcement |
| `visualization_engine/stage3/formatter.go` | F.2 sanitizeMermaidLabel; F.3 C4 differentiation; F.6 unique rendering; F.7 mindmap; F.8 ER |
| `visualization_engine/visualizer.go` | F.9 TTL parse cache |

### CLI

| File | Changes |
|------|---------|
| `cmd/visualize.go` | H.4 progress; H.10 --format; F.4 --scope |
| `cmd/root.go` | K.6 App injection via PersistentPreRunE |
| `cmd/analyze.go` | H.1 new — full pipeline runner |
| `cmd/init.go` | H.2 new — repository initialization |
| `cmd/status.go` | H.3 new — AKG status |
| `cmd/inspect.go` | H.5 new — graph inspection |
| `cmd/diff.go` | H.6 new — architectural diff |
| `cmd/tree.go` | H.7 new — architecture tree |
| `cmd/dependency.go` | H.8 new — import dependency view |
| `cmd/hotspot.go` | H.9 new — hotspot analysis |

### New Infrastructure Packages

| Package | Track | Purpose |
|---------|-------|---------|
| `internal/logger/` | K.1 | Structured logging with slog |
| `internal/config/` | K.2 | Configuration management and merging |
| `internal/errors/` | K.3 | Typed error definitions |
| `internal/terminal/` | K.4 | Spinner, progress bars, colors |
| `internal/app/` | K.5 | Dependency injection container |

---

## Closing Principle

GlassMarble has an exceptionally sound foundational architecture. The pipeline design (Stage 1 -> 2 -> 3 -> 4 -> AKG -> Visualization) is correct. The data model (CPG persisted as RDF Turtle with MVCC) is architecturally sophisticated and industry-worthy.

The gaps concentrate entirely in the **semantic extraction layer** — the 13 language translators that convert raw Tree-sitter tokens into meaningful architectural knowledge. Once those gaps are filled (Milestones 1 and 2), the visualization diagrams automatically become accurate, because the pipeline is already correctly wired end-to-end.

### The Path to Industry Standard

```
Step 1: Make the DATA right        -> Tracks A, B, C, D
Step 2: Make the REASONING right   -> Track E
Step 3: Make the OUTPUT right      -> Tracks F, G
Step 4: Make the UX right          -> Tracks H, K
Step 5: Make it FAST and RELIABLE  -> Tracks I, J
```

> "The quality of a code analysis tool is determined entirely by the accuracy of its semantic extraction layer. Everything else — the graph, visualizations, reasoning — is downstream of that."
