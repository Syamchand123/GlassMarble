# Visualization Engine — Master Improvement Plan

**Current rating: 1.0/10 — Target: 9.0/10**

---

## Table of Contents

1. [Overview & Guiding Principles](#1-overview--guiding-principles)
2. [Phase 0: TTL Parser Overhaul (P0)](#2-phase-0-ttl-parser-overhaul-p0)
3. [Phase 1: Tier A — Semantic Subgraph Extraction (P1)](#3-phase-1-tier-a--semantic-subgraph-extraction-p1)
4. [Phase 2: Tier B — Layout & Aggregation (P2)](#4-phase-2-tier-b--layout--aggregation-p2)
5. [Phase 3: Tier C — Multi-Format Rendering (P3)](#5-phase-3-tier-c--multi-format-rendering-p3)
6. [Phase 4: Scope System (P4)](#6-phase-4-scope-system-p4)
7. [Phase 5: Graph Algorithms (P5)](#7-phase-5-graph-algorithms-p5)
8. [Phase 6: C4 Stencil Replacement (P6)](#8-phase-6-c4-stencil-replacement-p6)
9. [Phase 7: Pipeline Architecture Overhaul (P7)](#9-phase-7-pipeline-architecture-overhaul-p7)
10. [Phase 8: Testing (P8)](#10-phase-8-testing-p8)
11. [Phase 9: Concurrent Safety & Caching (P9)](#11-phase-9-concurrent-safety--caching-p9)
12. [Phase 10: Dead Code & Technical Debt (P10)](#12-phase-10-dead-code--technical-debt-p10)
13. [Phase 11: cmd/visualize integration (P11)](#13-phase-11-cmdvisualize-integration-p11)
14. [File Change Summary](#14-file-change-summary)
15. [Success Criteria](#15-success-criteria)

---

## 1. Overview & Guiding Principles

### Principles

1. **Data-driven, not stenciled** — Every diagram element must derive from actual `.ttl` graph data, not hardcoded strings.
2. **Every diagram is a view** — A diagram type defines which nodes, edges, and layout rules apply. No data is invented.
3. **3 scope levels** — Every diagram type supports `global` (entire graph), `folder` (single directory subtree), `file` (single file).
4. **TTL is the source of truth** — The parser must correctly handle every predicate and class the AKG serializer emits.
5. **Testable at every layer** — Each stage must be independently unit-testable with synthetic graph data.
6. **Concurrency-safe** — Cache reads/writes, parser invocations, and pipeline stages must be goroutine-safe.
7. **Graph completeness** — The engine must understand all 40+ predicates, all 20+ node kinds, and all topological patterns the AKG emits.

### The 3-Tier Pipeline (Revised)

```
[.ttl file]
    |
    v
Tier A: TTL Parser + Semantic Subgraph Extractors
    |  - ParseTTLFile → NativeGraph (in-memory adjacency)
    |  - Scope filter applied here
    |  - 21 + 1 extractors (one per diagram type + shared base)
    v
Tier B: Layout Aggregator + Graph Algorithms
    |  - Prune dead components (scope-aware)
    |  - Build tree (hierarchical clustering)
    |  - Run algorithms: SCC, PageRank, Betweenness, Community Detection
    |  - Topological sort
    v
Tier C: Multi-Format Renderer
    |  - Mermaid (primary)
    |  - PlantUML (secondary)
    |  - DOT/Graphviz (optional tertiary)
    v
[Markdown + Mermaid code block]
```

---

## 2. Phase 0: TTL Parser Overhaul (P0)

**Files:** `stage1/extractor.go` (rewrite), `types/types.go` (extend)

### Problem

The current `parseNodeBlock` uses `strings.Split(parts[1], ";")` and line-joining logic that does not correctly parse the actual AKG `.ttl` format. The serializer writes each predicate-value pair on a separate line terminated with `;` and the final line terminates with `.`.

### Solution: Replace ParseTTLFile with a proper RDF Turtle parser

**Must handle:**

```turtle
<http://glassmarble.org/node/src/db.go::DBStore> a gm:TypeDecl ;
    gm:name "DBStore" ;
    gm:primitiveType "DATABASE" ;
    gm:belongsToFile <http://glassmarble.org/file/src/db.go> ;
    gm:lineStart 10 ;
    gm:lineEnd 45 ;
    gm:isEntrypoint true ;
    gm:primitiveZone "DATABASE_ZONE" ;
    .
```

And edge triples:

```turtle
<http://glassmarble.org/node/a> gm:calls <http://glassmarble.org/node/b> .
<< <http://glassmarble.org/node/a> gm:calls <http://glassmarble.org/node/b> >> gm:lineNumber 42 .
```

**New parser architecture:**

```go
// ParseTTLFile returns a NativeGraph (pure in-memory representation, no AKG dependency)
type NativeGraph struct {
    Nodes map[string]*NativeNode
    Edges []NativeEdge
}

type NativeNode struct {
    ID            string
    Kind          string   // "gm:TypeDecl", "gm:Executable", etc.
    Name          string
    PrimitiveType string
    FileURI       string
    LineStart     int
    LineEnd       int
    Code          string
    IsEntrypoint  bool
    PrimitiveZone string
    Properties    map[string]string // all extra predicates
}

type NativeEdge struct {
    SourceID   string
    Predicate  string   // "gm:calls", "gm:dependsOn", etc.
    TargetID   string
    LineNumber int
}
```

**Parser algorithm:**

1. Read `@prefix` lines (store prefix map: `gm` → `http://glassmarble.org/schema#`)
2. For each non-blank, non-prefix line:
   - If it starts with `<<` — store as RDF-star edge property, process after all base triples
   - If it matches `<...> <...> <...> .` (3 URIs/refs, ends with `.`, no `;`) — parse as base edge triple
   - Otherwise — collect lines until `.` terminator, parse as node block
3. Node block parsing: extract subject URI, then for each `predicate value ;` pair:
   - Map `a` to `Kind`
   - Map known predicates to struct fields
   - Store unknown predicates in `Properties` map
4. Edge line: `<src> <pred> <tgt> .` → `NativeEdge{SourceID, Predicate, TargetID}`
5. RDF-star: `<< <src> <pred> <tgt> >> gm:lineNumber N .` → bind `LineNumber` to matching edge
6. URI parsing: strip angle brackets, resolve against base, strip known prefixes (`http://glassmarble.org/node/`, `file/`, `namespace/`)

### Files changed

- `internal/visualization_engine/stage1/extractor.go` — rewrite `ParseTTLFile` and `parseNodeBlock`, remove `reconstructEdges`, add `NativeGraph` usage
- `internal/visualization_engine/types/types.go` — add `NativeGraph`, `NativeNode` (alias `TTLNode` → deprecate)

---

## 3. Phase 1: Tier A — Semantic Subgraph Extraction (P1)

**Files:** `stage1/extractor.go` (rewrite nearly all extract functions)

### Problems

1. Edge filters miss 30+ predicates emitted by AKG
2. Every extractor discovers entry points differently (inconsistent)
3. C4 extractors produce identical stencil output

### Solution: Predicate-aware BFS + Categorized Extractors

**Step 1: Define predicate groups**

```go
type PredicateGroup int
const (
    GroupCallGraph    PredicateGroup = iota // gm:calls, gm:spawnsConcurrent, gm:dispatchesEvent, gm:contextualCall, gm:ffiCall
    GroupTypeHierarchy                      // gm:inheritsFrom, gm:extends, gm:implements, gm:mixes
    GroupComposition                        // gm:composes, gm:hasMember, gm:hasField, gm:aggregates, gm:contains
    GroupDataFlow                           // gm:dataFlowTo, gm:pointsTo, gm:heapAlias, gm:aliasesPointer, gm:vulnerableTaint
    GroupControlFlow                         // gm:controlFlowTo, gm:controlFlowToTrue, gm:controlFlowToFalse, gm:catchesException, gm:defersExecution
    GroupStructural                          // gm:belongsToFile, gm:belongsToNamespace, gm:belongsTo, gm:dependsOn, gm:references, gm:imports
    GroupMessaging                           // gm:sendsMessage, gm:receivesMessage, gm:publishesEvent, gm:subscribesEvent, gm:dispatchesEvent
    GroupInfrastructure                      // gm:networkCall, gm:queriesDatabase, gm:callsCloudAPI, gm:consumesResource, gm:mutatesGlobal, gm:securitySink, gm:exposesEndpoint
    GroupSecurity                            // gm:vulnerableTaint, gm:securitySink, gm:consumesResource
    GroupBinding                             // gm:instantiatesGeneric, gm:diInjects, gm:escapesToHeap, gm:branchConstraint, gm:aliasesType
    GroupAny                                 // all predicates
)
```

Each extractor selects a combination of groups and node kind filters. This eliminates the need to list individual predicates in each function.

**Step 2: Rewrite all 21 extractors using predicate groups + kind filters**

Each extractor becomes a declarative configuration:

```go
type ExtractionConfig struct {
    Name           string
    NodeKindFilter []string   // e.g., {"gm:TypeDecl", "gm:Executable"}
    PredicateGroup []PredicateGroup
    EntryStrategy  EntryStrategy // auto, entrypointID, all, changedFiles
    MaxDepth       int
    Direction      EdgeDirection // forward, reverse, both
    ScopeBehavior  ScopeBehavior // includeChildren (folder), exact (file), all (global)
}
```

Example mapping for all 21 diagrams:

| Diagram | Node Kinds | Predicate Groups | Entry Strategy | MaxDepth | Direction |
|---------|-----------|-----------------|----------------|----------|-----------|
| UMLClass | TypeDecl, Member, Executable | TypeHierarchy, Composition, Binding | EntryPoint or All | 7 | forward |
| UMLObject | TypeDecl | TypeHierarchy | EntryPoint or All | 3 | forward |
| UMLComponent | TypeDecl, Executable, Namespace | CallGraph, Composition, Structural | EntryPoint or All | 7 | forward |
| UMLDeployment | Namespace, File, Database, Executable | Infrastructure, CallGraph, Structural | All | 7 | forward |
| UMLPackage | Namespace, File, Module | Structural | All | unlimited | both |
| UMLComposite | TypeDecl, Interface, Port | Composition, TypeHierarchy | EntryPoint or All | 5 | forward |
| UMLProfile | TypeDecl, Annotation | TypeHierarchy, Composition | All | 5 | forward |
| UMLUsecase | Annotation, Executable, Function, Method | CallGraph, Structural | Auto-discover | 5 | forward |
| UMLActivity | ControlStructure, Block, Executable, Function, Method | ControlFlow | EntryPoint | 10 | forward |
| UMLState | Variable, ControlStructure, TypeDecl, Executable | DataFlow, ControlFlow | All | 5 | forward |
| UMLSequence | Executable, Function | CallGraph | **EntryPoint required** | depth | forward |
| UMLCommunication | Executable, Function | CallGraph | EntryPoint | depth | forward |
| UMLInteractionOverview | Executable, Function | CallGraph | EntryPoint | depth | forward |
| UMLTiming | Executable, Function, Variable | CallGraph | All | 5 | forward |
| C4Context | User, ExternalSystem, Namespace, Module | CallGraph, Structural, Infrastructure | Auto-discover | 3 | forward |
| C4Container | Module, Namespace, Database, Executable | CallGraph, Structural, Infrastructure, Messaging | Auto-discover | 3 | forward |
| C4Component | TypeDecl, Executable, Database, ExternalSystem, Function, Method | CallGraph, Composition, Structural, Infrastructure | Auto-discover | 5 | forward |
| C4Code | TypeDecl, Member, Executable | TypeHierarchy, Composition | EntryPoint or All | 3 | forward |
| C4Landscape | Namespace, File, Module, ExternalSystem | Structural, Infrastructure | All | unlimited | both |
| C4Dynamic | Executable, Function | CallGraph | EntryPoint | depth | forward |
| C4Deployment | Namespace, File, Executable, Database | Structural, Infrastructure, CallGraph | All | 3 | forward |
| ERDiagram | TypeDecl, Struct, Class, Member | Composition, Binding | All | 3 | both |
| DataFlow | Variable, Parameter, Executable | DataFlow, Security, Binding | Auto-discover | 10 | forward+reverse |
| Mindmap | Namespace, File, Module | Structural | All | unlimited | both |
| Flowchart | All nodes | ControlFlow, DataFlow, CallGraph | EntryPoint | 10 | forward |
| DependencyGraph | TypeDecl, File, Namespace | Structural | All | 3 | both |
| HotspotComplexity | Executable, Function, Method | CallGraph | All | 3 | forward |
| CallGraph | Executable, Function, Method | CallGraph, Messaging, Infrastructure | EntryPoint or All | unlimited | forward |
| LayeredArchitecture | TypeDecl, Executable | CallGraph, Composition, TypeHierarchy | All | unlimited | both |
| ChangeImpact | All nodes | All groups | ChangedFiles | 5 | reverse |
| Infrastructure | ExternalSystem, Database, Module, Namespace | Infrastructure, Structural, Messaging, Security | All | 3 | reverse |

**Step 3: Remove dead `extractComponentSubgraph`**

The unused function at `stage1/extractor.go:283` is removed entirely.

### Edge direction semantics

- **forward**: standard BFS from start nodes following outgoing edges
- **reverse**: BFS following incoming edges (impact analysis, infrastructure discovery)
- **both**: collect all nodes matching the kind filter within scope, then add edges between them

### Files changed

- `internal/visualization_engine/stage1/extractor.go` — full rewrite
- `internal/visualization_engine/types/types.go` — add `ExtractionConfig`, `PredicateGroup`, `EdgeDirection`, `EntryStrategy`, `ScopeBehavior`

---

## 4. Phase 2: Tier B — Layout & Aggregation (P2)

**Files:** `stage2/aggregator.go` (rewrite), `types/types.go` (extend)

### Problems

1. `getDirectoryPath` logic is convoluted and uses the wrong heuristic
2. No community/layer clustering
3. No ranking/importance computation
4. Dead component pruning is too aggressive (removes isolated but important nodes)

### Solution: Hierarchical Clustering + Graph Metrics

**Step 1: Rewrite `BuildLayoutTree`**

```go
func BuildLayoutTree(sub *NativeGraph, opts QueryOptions, diagramType DiagramType) *LayoutTree {
    // 1. Metadata computation
    //   a. Compute PageRank scores for all nodes
    //   b. Compute Betweenness Centrality for bottleneck detection
    //   c. Detect communities (Louvain-like modularity on edge types)
    //   d. Rank nodes by importance score

    // 2. Scope-boundary assignment
    //   a. Assign each node to its file boundary
    //   b. Assign each file to its directory boundary
    //   c. Assign each directory to its module boundary
    //   d. Diagram-specific: override boundaries (e.g., UMLPackage uses directory, UMLClass uses type hierarchy)

    // 3. Dead component pruning (scope-aware)
    //   a. Global scope: keep all nodes
    //   b. Folder scope: prune nodes outside the folder subtree, keep those inside
    //   c. File scope: prune to single file's nodes + their immediate neighbors

    // 4. Build hierarchical LayoutTree
    //   a. Create node → boundary mapping
    //   b. Create boundary tree
    //   c. For specific diagram types: structure boundaries semantically
    //      - UMLComposite: parts/ports structure
    //      - UMLDeployment: device/execution-environment
    //      - C4Component: container boundaries
    //      - LayeredArchitecture: layer boundaries

    // 5. Edge collapsing + cycle detection
    //   a. Collapse duplicate edges (same src/pred/tgt)
    //   b. Run Tarjan's SCC
    //   c. Mark cyclic edges

    // 6. Topological sort with sink-awareness
    //   a. Nodes sorted by: sink status (DB/NETWORK sinks last), then rank, then line number
    //   b. Boundaries sorted hierarchically

    // 7. Return LayoutTree
}
```

**Step 2: Compute graph metrics**

Add `stage2/metrics.go`:

```go
func ComputePageRank(g *NativeGraph, damping float64, iterations int) map[string]float64
func ComputeBetweenness(g *NativeGraph) map[string]float64
func DetectCommunities(g *NativeGraph, opts QueryOptions) map[string]string  // nodeID → community label
func ComputeInDegree(g *NativeGraph) map[string]int
func DetectGodObjects(g *NativeGraph) []GodObjectReport
```

These metrics feed into:
- HotspotComplexity: in-degree = coupling hotspot
- ChangeImpact: PageRank = risk score
- C4Context/Landscape: community = system boundary
- LayeredArchitecture: layer = community after topological analysis
- Edge visibility: high-betweenness nodes rendered as bottlenecks

**Step 3: Community detection algorithm**

Since the graph has typed edges, use edge-type-weighted modularity:

```
1. Start: each node in its own community
2. For each node, evaluate gain from moving to neighbor's community
3. Move to community with max positive gain
4. Repeat until stable
5. Output community assignments
```

Use structural edges (`gm:belongsToFile`, `gm:belongsToNamespace`, `gm:belongsTo`, `gm:dependsOn`) with higher weight for community structure, call graph edges with medium weight, and data/control flow edges with low weight.

**Step 4: Enhance `LayoutNode` with computed metrics**

```go
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
    // New computed fields
    PageRank        float64
    Betweenness     float64
    Community       string
    InDegree        int
    OutDegree       int
    IsHotspot       bool
    IsBottleneck    bool
    IsGodObject     bool
}
```

**Step 5: Community-aware boundary assignment**

Instead of only using directory paths, detect semantic boundaries:

```go
// For UMLPackage / DependencyGraph / Mindmap: directory boundaries
// For C4Context / C4Landscape: community boundaries
// For LayeredArchitecture: layer boundaries (by PrimitiveType)
// For UMLComponent: combined (community + directory)
// For C4Container: directory boundaries + community hints
```

### Files changed

- `internal/visualization_engine/stage2/aggregator.go` — rewrite
- `internal/visualization_engine/stage2/metrics.go` — new file
- `internal/visualization_engine/stage2/community.go` — new file (community detection)
- `internal/visualization_engine/types/types.go` — extend `LayoutNode`

---

## 5. Phase 3: Tier C — Multi-Format Rendering (P3)

**Files:** `stage3/formatter.go` (rewrite), `stage3/mermaid.go` (new), `stage3/plantuml.go` (new)

### Problems

1. All C4 renderers are stenciled with hardcoded labels
2. PlantUML renderer is incomplete (3 predicates only)
3. Fallback to flowchart is ugly for type-specific diagrams
4. No DOT/Graphviz output option

### Solution: Data-driven renderers per format

**Step 1: Split into format-specific files**

```
stage3/
    formatter.go        — dispatcher (RenderDiagram → mermaid/plantuml)
    mermaid.go          — all Mermaid diagram renderers
    plantuml.go         — all PlantUML diagram renderers
    dot.go              — Graphviz DOT renderer (optional)
    helpers.go          — shared sanitization, label formatting, arrow constants
```

**Step 2: Fix C4 renderers to be data-driven**

Example for C4Context:

```go
func renderC4ContextMermaid(tree *LayoutTree, sb *strings.Builder) {
    sb.WriteString("C4Context\n")
    title := getTitle(tree, "System Context Diagram")
    sb.WriteString(fmt.Sprintf("    title %s\n", title))

    // Collect person nodes (gm:User kind)
    for _, node := range collectNodesByKind(tree, "gm:User") {
        sb.WriteString(fmt.Sprintf("    Person(%s, \"%s\", \"External Actor\")\n",
            sanitizeName(node.ID), sanitizeMermaidLabel(node.Name)))
    }

    // Collect system nodes (gm:Namespace, gm:Module with structural edges)
    for _, boundary := range tree.Children {
        if isSystemBoundary(boundary) {
            alias := sanitizeName(boundary.BoundaryName)
            sb.WriteString(fmt.Sprintf("    System(%s, \"%s\", \"System\")\n",
                alias, sanitizeMermaidLabel(boundary.BoundaryName)))
        }
    }

    // Collect external systems (gm:ExternalSystem kind)
    for _, node := range collectNodesByKind(tree, "gm:ExternalSystem") {
        sb.WriteString(fmt.Sprintf("    SystemExt(%s, \"%s\", \"External System\")\n",
            sanitizeName(node.ID), sanitizeMermaidLabel(node.Name)))
    }

    // Collect databases
    for _, node := range collectNodesByPrimitive(tree, "DATABASE") {
        sb.WriteString(fmt.Sprintf("    SystemDb(%s, \"%s\", \"Database\")\n",
            sanitizeName(node.ID), sanitizeMermaidLabel(node.Name)))
    }

    // Render edges as Rel() calls
    drawn := make(map[string]bool)
    for _, edge := range tree.Edges {
        src := sanitizeName(edge.SourceID)
        tgt := sanitizeName(edge.TargetID)
        key := src + "->" + tgt
        if drawn[key] { continue }
        drawn[key] = true
        label := sanitizeMermaidLabel(shortPredicate(edge.Predicate))
        sb.WriteString(fmt.Sprintf("    Rel(%s, %s, \"%s\")\n", src, tgt, label))
    }
}
```

Every C4 diagram follows this pattern — no hardcoded strings.

**Step 3: Complete PlantUML renderer**

```go
func renderPlantUMLClassDiagram(tree *LayoutTree, sb *strings.Builder) {
    sb.WriteString("@startuml\n")
    sb.WriteString("skinparam style strictuml\n")

    // Collect classes
    for _, node := range collectAllNodes(tree) {
        alias := sanitizeName(node.ID)
        switch node.Kind {
        case "gm:TypeDecl":
            if node.PrimitiveType == "INTERFACE" || strings.Contains(node.Name, "Iface") {
                sb.WriteString(fmt.Sprintf("interface \"%s\" as %s\n", node.Name, alias))
            } else {
                sb.WriteString(fmt.Sprintf("class \"%s\" as %s\n", node.Name, alias))
            }
        case "gm:Executable":
            sb.WriteString(fmt.Sprintf("class \"%s\" as %s <<method>>\n", node.Name, alias))
        default:
            sb.WriteString(fmt.Sprintf("rectangle \"%s\" as %s\n", node.Name, alias))
        }
    }

    // PlantUML arrow semantics
    arrowMap := map[string]string{
        "gm:inheritsFrom":  " --|> ",
        "gm:extends":       " --|> ",
        "gm:implements":    " ..|> ",
        "gm:composes":      " --* ",
        "gm:aggregates":    " --o ",
        "gm:references":    " ..> ",
        "gm:calls":         " -> ",
        "gm:dependsOn":     " ..> ",
        "gm:hasMember":     " -- ",
        "gm:hasField":      " -- ",
        "gm:contains":      " --> ",
    }

    for _, edge := range tree.Edges {
        src := sanitizeName(edge.SourceID)
        tgt := sanitizeName(edge.TargetID)
        arrow, ok := arrowMap[edge.Predicate]
        if !ok {
            arrow = " ..> "
        }
        label := shortPredicate(edge.Predicate)
        sb.WriteString(fmt.Sprintf("%s %s %s : %s\n", src, arrow, tgt, label))
    }

    sb.WriteString("@enduml\n")
}
```

**Step 4: Add DOT/Graphviz renderer (optional, P3.5)**

Use the `"gonum/graph"` or raw DOT format as a third output option for integration with Graphviz-based tools.

### Files changed

- `internal/visualization_engine/stage3/formatter.go` — rewrite as dispatcher
- `internal/visualization_engine/stage3/mermaid.go` — new (all Mermaid renderers, extracted from formatter.go)
- `internal/visualization_engine/stage3/plantuml.go` — new (all PlantUML renderers)
- `internal/visualization_engine/stage3/dot.go` — new (optional DOT renderer)
- `internal/visualization_engine/stage3/helpers.go` — new (shared utilities)

---

## 6. Phase 4: Scope System (P4)

**Files:** `types/types.go` (extend), `visualizer.go` (rewrite), `cmd/visualize.go` (extend)

### Problem

`ScopePrefix` exists but is effectively dead code. There is no concept of hierarchical scope levels.

### Solution: 3-level scope system

**Step 1: Define scope types**

```go
type ScopeLevel int

const (
    ScopeGlobal ScopeLevel = iota // entire graph — everything in the .ttl file
    ScopeFolder                   // single directory/subtree (e.g., "internal/db")
    ScopeFile                     // single file (e.g., "src/db.go")
)
```

**Step 2: Define scope in QueryOptions**

```go
type QueryOptions struct {
    // ... existing fields ...
    Scope       ScopeLevel // new
    ScopePath   string     // path for folder/file scope (e.g., "internal/db" or "src/main.go")
}
```

**Step 3: Implement scope filtering in the extractor**

```go
func applyScope(graph *NativeGraph, opts QueryOptions) {
    switch opts.Scope {
    case ScopeGlobal:
        // no filtering — use entire graph
    case ScopeFolder:
        // Keep nodes whose FileURI starts with ScopePath
        // Keep all edges where both endpoints are kept
        // Keep namespace/module boundaries that contain kept files
    case ScopeFile:
        // Keep only nodes in the single file
        // Keep edges where both endpoints are in the file
        // Add edges from external references as dotted external nodes
    }
}
```

**Step 4: Wire scope flag through cmd/visualize**

```go
// cmd/visualize.go
var scopeFlag string // "global" (default), "folder:internal/db", "file:src/main.go"

func parseScope(scopeStr string) (ScopeLevel, string, error) {
    switch {
    case scopeStr == "" || scopeStr == "global":
        return ScopeGlobal, "", nil
    case strings.HasPrefix(scopeStr, "folder:"):
        return ScopeFolder, strings.TrimPrefix(scopeStr, "folder:"), nil
    case strings.HasPrefix(scopeStr, "file:"):
        return ScopeFile, strings.TrimPrefix(scopeStr, "file:"), nil
    default:
        return ScopeGlobal, "", fmt.Errorf("invalid scope: %s", scopeStr)
    }
}
```

### Files changed

- `internal/visualization_engine/types/types.go` — add `ScopeLevel`, extend `QueryOptions`
- `internal/visualization_engine/stage1/extractor.go` — add `applyScope`
- `internal/visualization_engine/visualizer.go` — pass scope through pipeline
- `cmd/visualize.go` — parse and pass scope flag

---

## 7. Phase 5: Graph Algorithms (P5)

**Files:** `stage2/metrics.go` (new), `stage2/community.go` (new), `stage2/path.go` (new), `stage2/clustering.go` (new)

### Purpose

Add an algorithmic layer that computes graph-theoretic properties of the subgraph, enriching the `LayoutTree` with metadata used by renderers.

### Algorithms to implement

#### 5.1 PageRank (`metrics.go`)

```go
func ComputePageRank(g *NativeGraph, damping float64, iterations int) map[string]float64
```

- Damping factor: 0.85 (standard)
- Used by: HotspotComplexity, ChangeImpact, all Track G diagrams
- Visual mapping: high-PageRank nodes highlighted as "Important", shown with larger radius in C4Context

#### 5.2 Betweenness Centrality (`metrics.go`)

```go
func ComputeBetweenness(g *NativeGraph) map[string]float64
```

- Brandes' algorithm: O(VE) for unweighted
- Used by: Highlight architectural bottlenecks, key connectors between subsystems
- Visual mapping: high-betweenness nodes shown as bridges, critical path markers

#### 5.3 In-Degree/Out-Degree Distribution (`metrics.go`)

```go
func ComputeDegreeDistribution(g *NativeGraph) (map[string]int, map[string]int)
```

- Used by: HotspotComplexity (high in-degree = coupling hotspot)

#### 5.4 Community Detection (`community.go`)

```go
func DetectCommunities(g *NativeGraph) map[string]string
```

- Weighted Louvain-style modularity optimization
- Edge type weights: structural=3, call=2, dataflow=1, control-flow=0.5
- Used by: C4Context, C4Landscape, LayeredArchitecture, PackageDiagram
- Visual mapping: each community gets a distinct boundary color/shape

#### 5.5 God-Object Detection (`metrics.go`)

```go
func DetectGodObjects(g *NativeGraph) []string
```

- Node with in-degree + out-degree > 3σ from mean
- Used by: HotspotComplexity, ChangeImpact, UMLComponent
- Visual mapping: god-objects shown with warning styling

#### 5.6 K-core Decomposition (`clustering.go`)

```go
func ComputeKCores(g *NativeGraph) map[string]int
```

- Identify the core-periphery structure
- Used by: DependencyGraph, C4Landscape — core shown as central, periphery as satellite

#### 5.7 Path Finding (`path.go`)

```go
func FindShortestPath(g *NativeGraph, src, tgt string) []string
func FindAllPaths(g *NativeGraph, src, tgt string, maxDepth int) [][]string
func FindCriticalPath(g *NativeGraph) []string  // longest path in DAG
```

- Used by: ChangeImpact (trace affected paths), DataFlow (trace reaching definitions), UMLSequence (render interaction order)

#### 5.8 Minimum Spanning Forest (`clustering.go`)

```go
func ComputeMST(g *NativeGraph) []NativeEdge
```

- Used by: Mindmap (tree layout), UMLPackage (remove cycles for cleaner layout)

#### 5.9 Graph Summary Statistics

```go
type GraphSummary struct {
    NodeCount       int
    EdgeCount       int
    Density         float64
    Diameter        int
    AvgPathLength   float64
    ClusterCount    int
    LargestSCCSize  int
    GodObjectCount  int
    BipartiteScore  float64
}
```

- Computed automatically and attached to `LayoutTree` as metadata
- Used by: C4Landscape title/annotation, all diagram footers

### Files changed (new)

- `internal/visualization_engine/stage2/metrics.go`
- `internal/visualization_engine/stage2/community.go`
- `internal/visualization_engine/stage2/path.go`
- `internal/visualization_engine/stage2/clustering.go`

---

## 8. Phase 6: C4 Stencil Replacement (P6)

**Files:** `stage3/mermaid.go`, `stage3/plantuml.go`

### Problem

All 7 C4 diagrams plus `renderTrackGDiagram` for Infrastructure use hardcoded labels like `"GlassMarble System"` and `"Application Container"`.

### Solution

Every C4 diagram traverses the actual `LayoutTree` and renders real node data. Implementation documented in [Phase 3](#5-phase-3-tier-c--multi-format-rendering-p3).

**Example: C4Container uses LayoutTree boundaries as containers**

```go
func renderC4ContainerMermaid(tree *LayoutTree, sb *strings.Builder) {
    sb.WriteString("C4Container\n")
    sb.WriteString(fmt.Sprintf("    title %s\n", getDiagramTitle(tree, "Container Diagram")))

    // Top-level boundaries become system boundaries
    for _, boundary := range tree.Children {
        alias := sanitizeName(boundary.BoundaryName)
        name := sanitizeMermaidLabel(boundary.BoundaryName)
        sb.WriteString(fmt.Sprintf("    System_Boundary(%s_sys, \"%s System\") {\n", alias, name))

        // Inner boundaries become containers
        for _, subBoundary := range boundary.Children {
            subAlias := sanitizeName(subBoundary.BoundaryName)
            subName := sanitizeMermaidLabel(subBoundary.BoundaryName)
            tech := detectContainerTechnology(subBoundary)
            sb.WriteString(fmt.Sprintf("        Container(%s, \"%s\", \"%s\", \"%s\")\n",
                subAlias, subName, tech, getContainerDescription(subBoundary)))
        }

        // Direct nodes in this boundary
        for _, node := range boundary.Nodes {
            nodeAlias := sanitizeName(node.ID)
            nodeName := sanitizeMermaidLabel(node.Name)
            tech := detectNodeTechnology(node)
            desc := getNodeDescription(node)
            if isDatabase(node) {
                sb.WriteString(fmt.Sprintf("        ContainerDb(%s, \"%s\", \"%s\", \"%s\")\n",
                    nodeAlias, nodeName, tech, desc))
            } else {
                sb.WriteString(fmt.Sprintf("        Container(%s, \"%s\", \"%s\", \"%s\")\n",
                    nodeAlias, nodeName, tech, desc))
            }
        }

        sb.WriteString("    }\n")
    }

    // Edge relationships
    renderC4Edges(tree, sb)
}
```

### Files changed

- `internal/visualization_engine/stage3/mermaid.go` — all C4 renderers replaced with data-driven implementations
- `internal/visualization_engine/stage3/plantuml.go` — PlantUML C4 renderers

---

## 9. Phase 7: Pipeline Architecture Overhaul (P7)

**Files:** `visualizer.go` (rewrite), `stage1/pipeline.go` (new), `stage2/pipeline.go` (new)

### Problem

The current pipeline is:

```
ProjectDiagram → getOrExtractSubgraph → stage1.ExtractSubgraph → stage2.BuildLayoutTree → stage3.RenderDiagramFormat
```

This does not support:
- Metrics computation
- Pre-processing steps
- Post-processing steps
- Intermediate validation
- Configurable pipeline stages

### Solution: Configurable Pipeline

```go
type PipelineStage int

const (
    StageParse     PipelineStage = iota // parse TTL → NativeGraph
    StageScope                          // apply scope filtering
    StageExtract                        // extract diagram-specific subgraph
    StageMetrics                        // compute graph metrics
    StageCluster                        // community detection + hierarchical clustering
    StageLayout                         // build LayoutTree
    StageRender                         // render to output format
)

type PipelineConfig struct {
    DiagramType   DiagramType
    Scope         ScopeLevel
    ScopePath     string
    Format        string
    // Algorithm toggles
    EnableMetrics bool
    EnableCommunities bool
    EnableSCC     bool
    MaxNodes      int
    MaxDepth      int
}
```

**New `EngineCoordinator.ProjectDiagram`:**

```go
func (ec *EngineCoordinator) ProjectDiagram(t DiagramType, opts QueryOptions) (string, error) {
    // 1. Parse TTL → NativeGraph (with caching)
    native, err := ec.parseGraph(opts)

    // 2. Apply scope filter
    native = applyScope(native, opts)

    // 3. Extract diagram subgraph
    extractionConfig := getExtractionConfig(t, opts)
    subgraph := extractSubgraph(native, extractionConfig)

    // 4. Compute metrics
    metrics := ComputeAllMetrics(subgraph)

    // 5. Cluster (communities / layers / hierarchy)
    clustering := DetectCommunities(subgraph)

    // 6. Build LayoutTree
    layout := BuildLayoutTree(subgraph, metrics, clustering, opts, t)

    // 7. Render
    markup := RenderDiagram(layout, t, opts.Format)

    return markup, nil
}
```

This makes the pipeline explicit, testable at each stage, and configurable.

**Add `stage1/types.go` and `stage2/types.go` for stage-specific types:**

```go
// stage1/types.go
type ExtractedSubgraph struct {
    Graph      *NativeGraph
    EntryNodes []string
    Config     ExtractionConfig
}

// stage2/types.go
type DiagramMetrics struct {
    PageRank       map[string]float64
    Betweenness    map[string]float64
    DegreeIn       map[string]int
    DegreeOut      map[string]int
    Communities    map[string]string
    GodObjects     []string
    KCore          map[string]int
    SCCs           [][]string
    Summary        GraphSummary
}

type LayoutInput struct {
    Subgraph    *ExtractedSubgraph
    Metrics     *DiagramMetrics
    Clustering  map[string]string
    Options     QueryOptions
    DiagramType DiagramType
}
```

### Files changed

- `internal/visualization_engine/visualizer.go` — rewrite with explicit pipeline
- `internal/visualization_engine/stage1/pipeline.go` — new file, extraction config lookup
- `internal/visualization_engine/stage1/types.go` — new file
- `internal/visualization_engine/stage2/types.go` — new file
- `internal/visualization_engine/types/types.go` — minor extensions

---

## 10. Phase 8: Testing (P8)

**Files:** `*_test.go` files throughout the engine

### Target: 85%+ coverage with 150+ tests

#### Test Plan

| Package | Tests | Focus |
|---------|-------|-------|
| `types` | 5 | Scope parsing, config validation, extraction config lookup |
| `stage1` (parser) | 30 | Parse all predicate types, all node kinds, RDF-star edges, prefix resolution, URI parsing, edge cases (empty file, malformed) |
| `stage1` (extractors) | 42 | One happy-path test per diagram type (21) + scope filter (3) + edge cases (18): empty graphs, missing entrypoints, all edge types, max depth limits |
| `stage2` (metrics) | 20 | PageRank convergence, betweenness star/disconnected, degree distribution, god-object detection, K-core, MST |
| `stage2` (community) | 10 | Modularity on simple graphs, disconnected graphs, edge-type weights |
| `stage2` (layout) | 15 | Boundary assignment (dir/community/layer), SCC detection, edge collapsing, topological sort, sink ordering, dead component pruning |
| `stage3` (mermaid) | 21 | One per diagram type + Format detection |
| `stage3` (plantuml) | 21 | One per diagram type (subset if some are identical) |
| `visualizer` | 8 | Pipeline end-to-end, caching, concurrency, error propagation, scope wiring |

### Test Data

Create a `testdata/` directory under `visualization_engine/`:

```
testdata/
    full_graph.ttl           — complete realistic AKG output (100+ nodes, 40 predicate types)
    minimal.ttl              — 1 node + 1 edge
    delta_append.ttl         — deleted nodes, delta format
    scope_internal.ttl       — nodes in "internal/" and "cmd/" for scope tests
    all_predicates.ttl       — one edge per predicate type (41+ edges)
    all_kinds.ttl            — one node per kind (20+ nodes)
    empty.ttl                — prefixes only, no content
```

### Files changed (new)

- `internal/visualization_engine/testdata/*.ttl` — test fixtures
- `internal/visualization_engine/stage1/extractor_test.go`
- `internal/visualization_engine/stage1/parser_test.go`
- `internal/visualization_engine/stage2/aggregator_test.go`
- `internal/visualization_engine/stage2/metrics_test.go`
- `internal/visualization_engine/stage2/community_test.go`
- `internal/visualization_engine/stage3/mermaid_test.go`
- `internal/visualization_engine/stage3/plantuml_test.go`
- `internal/visualization_engine/visualizer_test.go`

---

## 11. Phase 9: Concurrent Safety & Caching (P9)

**Files:** `visualizer.go`

### Problem

`subgraphCache` uses a global `sync.Mutex` but no cleanup, no eviction, and no goroutine safety for the parser.

### Solution: Thread-safe LRU cache with TTL

```go
type SubgraphCache struct {
    mu       sync.RWMutex
    entries  map[string]*cacheEntry
    maxSize  int
    evictCh  chan string
}

type cacheEntry struct {
    mtime     time.Time
    subgraph  *NativeGraph
    lastAccess time.Time
}
```

- `maxSize` = 128 entries (LRU eviction)
- `Get(key, mtime)` → if exists and mtime matches, return; else miss
- `Set(key, mtime, subgraph)` → insert/update
- `Evict()` → remove oldest accessed entries when full

Make all pipeline stages stateless where possible, and only cache the parsed `NativeGraph` (not the subgraph, since subgraphs depend on scope + diagram type).

### Thread-safety for the full pipeline

```go
func (ec *EngineCoordinator) ProjectDiagram(t DiagramType, opts QueryOptions) (string, error) {
    ec.mu.RLock()
    // ... concurrent-safe reads
    ec.mu.RUnlock()
    // Delegate to stateless pipeline functions
}
```

### Files changed

- `internal/visualization_engine/visualizer.go` — rewrite caching, add RWMutex, LRU eviction

---

## 12. Phase 10: Dead Code & Technical Debt (P10)

### Cleanup tasks

1. **Remove `types.go` stub** — root package `types.go` is a 3-line comment-only file. After moving all types to `types/types.go`, delete this file.
2. **Remove `extractComponentSubgraph`** — unused function at `stage1/extractor.go:283`. The switch routes `UMLComponent` to `extractC4ComponentSubgraph`.
3. **Consolidate `renderTrackGDiagram`** — the Kitchen-sink function at `stage3/formatter.go:1251` handles 6 diagram types with 15+ predicate branches. Split into individual diagram renderers in `mermaid.go`.
4. **Remove `reconstructEdges`** — after parser rewrite, edge reconstruction is handled inline in `ParseTTLFile`.
5. **Unify `parseURI`** — `parseURI` in `stage1/extractor.go:597` duplicates logic from `formatNodeURI` in `turtle_serializer.go`. Ensure consistent handling.
6. **Remove `scopeFlag` dead assignment** — `cmd/visualize.go:185` assigns to `scopeFlag` but never reads it. Wire to `QueryOptions.Scope` after Phase 4.

### Files changed (delete)

- `internal/visualization_engine/types.go` — delete

---

## 13. Phase 11: cmd/visualize Integration (P11)

**Files:** `cmd/visualize.go`, `cmd/visualize_test.go`

### Extensions

1. **Wire `--scope` flag** with `global`/`folder:X`/`file:X` syntax
2. **Add `--format plantuml` / `--format mermaid` / `--format dot`**
3. **Add `--save`** — already works
4. **Add `--output`** for stdout (default) or file
5. **Add `--summary`** flag to print graph statistics table before diagram
6. **Add `--algorithm`** flags for specific algorithms: `--pagerank`, `--community`, `--scc`
7. **Fix `TestVisualizeCommand_*`** tests to use updated output format

---

## 14. File Change Summary

### New files (12)

| File | Purpose |
|------|---------|
| `internal/visualization_engine/testdata/full_graph.ttl` | Test fixture |
| `internal/visualization_engine/testdata/minimal.ttl` | Test fixture |
| `internal/visualization_engine/testdata/all_predicates.ttl` | Test fixture |
| `internal/visualization_engine/testdata/all_kinds.ttl` | Test fixture |
| `internal/visualization_engine/stage2/metrics.go` | PageRank, Betweenness, Degree, GodObject detection, Summary |
| `internal/visualization_engine/stage2/community.go` | Community detection (Louvain-like) |
| `internal/visualization_engine/stage2/path.go` | Shortest path, All paths, Critical path |
| `internal/visualization_engine/stage2/clustering.go` | K-core, MST |
| `internal/visualization_engine/stage3/mermaid.go` | All Mermaid diagram renderers |
| `internal/visualization_engine/stage3/plantuml.go` | All PlantUML diagram renderers |
| `internal/visualization_engine/stage3/helpers.go` | Shared label/arrow/sanitization helpers |
| `internal/visualization_engine/stage3/dot.go` | Graphviz DOT renderer (optional) |

### Rewrite files (6)

| File | Change |
|------|--------|
| `internal/visualization_engine/stage1/extractor.go` | Full rewrite: proper TTL parser, extraction config, scope filter, predicate-group BFS |
| `internal/visualization_engine/stage2/aggregator.go` | Full rewrite: metrics-aware layout, community boundaries, enhanced LayoutTree |
| `internal/visualization_engine/stage3/formatter.go` | Simplify to dispatcher only |
| `internal/visualization_engine/visualizer.go` | Rewrite: explicit pipeline, LRU cache, concurrent-safe |
| `internal/visualization_engine/types/types.go` | Extend: NativeGraph, LayoutNode(metrics), ExtractionConfig, ScopeLevel, PipelineConfig |
| `cmd/visualize.go` | Wire scope, add flags |

### Delete files (1)

| File | Reason |
|------|--------|
| `internal/visualization_engine/types.go` | Replaced by `types/types.go` |

### New test files (9)

| File | Tests |
|------|-------|
| `internal/visualization_engine/stage1/parser_test.go` | 15+ |
| `internal/visualization_engine/stage1/extractor_test.go` | 25+ |
| `internal/visualization_engine/stage2/aggregator_test.go` | 10+ |
| `internal/visualization_engine/stage2/metrics_test.go` | 15+ |
| `internal/visualization_engine/stage2/community_test.go` | 8+ |
| `internal/visualization_engine/stage3/mermaid_test.go` | 21 |
| `internal/visualization_engine/stage3/plantuml_test.go` | 10+ |
| `internal/visualization_engine/visualizer_test.go` | 8+ |
| `cmd/visualize_test.go` (update) | 4+ |

---

## 15. Success Criteria

| Criterion | Minimum | Target |
|-----------|---------|--------|
| TTL parser handles all AKG output | 40 predicates + 20 kinds | All RDF Turtle valid output |
| Each extractor finds the right nodes | Nodes present in TTL | Correct diagram-specific subgraph |
| C4 diagrams use real graph data | No hardcoded strings | All 7 C4 diagrams data-driven |
| Scope levels work | 3 levels functional | Verified with real .ttl files |
| All 21 diagrams render | 21 unique outputs | Every diagram produces valid Mermaid |
| PlantUML output works | 21 diagrams | Valid PlantUML syntax |
| Test coverage | 80% | 85%+ |
| Race detector | Clean | Clean |
| `go build ./...` | Clean | Clean |
| `go vet ./...` | Clean | Clean |
| Concurrency safety | RWMutex on cache | Full pipeline concurrent-safe |
| Graph algorithms | PageRank + Betweenness + SCC | All 9 algorithms implemented |
| Caching | LRU with mtime validation | 128-entry LRU, TTL eviction |
| Diagram match | 21 targeted core (14 UML + 7 C4) | All 21 + specialized projections (ER, DataFlow, Mindmap, Flowchart, 6 Track G) |
