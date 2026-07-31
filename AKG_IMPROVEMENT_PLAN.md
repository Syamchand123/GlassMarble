# AKG Package Improvement Master Plan

**Goal:** Transform `internal/akg` from 6.5/10 → 9.5/10 by addressing all weak components: query capability, test coverage, scalability, macro inference caching, Turtle serializer correctness, PackageCohesion heuristics, graph algorithm completeness, concurrency safety, performance, and ontology completeness.

---

## Table of Contents

1. [P0: Query API](#p0-query-api)
2. [P0: Test Coverage](#p0-test-coverage)
3. [P1: Graph Algorithm Completeness](#p1-graph-algorithm-completeness)
4. [P1: Concurrent Safety](#p1-concurrent-safety)
5. [P2: Scalability — Lazy Clone](#p2-scalability--lazy-clone)
6. [P2: Performance — Bounded Clone Goroutines](#p2-performance--bounded-clone-goroutines)
7. [P2: Turtle Serializer Fixes](#p2-turtle-serializer-fixes)
8. [P2: PackageCohesion — Formal BELONGS_TO Edge](#p2-packagecohesion--formal-belongs_to-edge)
9. [P2: Macro Inference — Caching + Configurable Rules](#p2-macro-inference--caching--configurable-rules)
10. [P2: Ontology Completeness](#p2-ontology-completeness)
11. [Verification](#verification)

---

## P0: Query API

**Files:** `internal/akg/mvcc.go`, `internal/akg/query.go` (new)

### 1.1 Extend `GraphDB` interface in `internal/code_analysis_engine/stage4/type.go`

The `GraphDB` interface currently only has `GetNode`, `GetOutboundEdges`, `GetNodesByKind`. Add:

```go
type GraphDB interface {
    GetNode(id string) (*ResolvedNode, bool)
    GetOutboundEdges(id string) []ResolvedEdge
    GetNodesByKind(kind string) []*ResolvedNode

    // NEW METHODS:
    GetInboundEdges(id string) []ResolvedEdge
    Query(filter QueryFilter) []*ResolvedNode
    GetNodesByPattern(predicate string, object string) []string // returns source node IDs
}
```

### 1.2 Add `QueryFilter` struct to `internal/akg/query.go`

```go
// QueryFilter defines a flexible query for AKG graph nodes.
type QueryFilter struct {
    Kind          string            // exact kind match (empty = any)
    NameContains  string            // substring match on Name
    NameRegex     string            // regex match on Name
    Primitive     string            // exact primitive match
    Properties    map[string]string // all specified key=value pairs must match
    PropertyRegex map[string]string // property key → regex pattern
    MinEdges      int               // minimum total edge count (outbound + inbound)
    MaxEdges      int               // maximum total edge count (0 = no limit)
    Limit         int               // max results (0 = unlimited)
    Offset        int               // pagination offset
}
```

### 1.3 Add `Query` method to `CodePropertyGraph`

```go
// Query returns all nodes matching the given filter.
// Filters are AND-ed together. Empty/zero fields are ignored.
func (c *CodePropertyGraph) Query(filter QueryFilter) []*stage4.ResolvedNode
```

Implementation plan:

1. Start with the full node set, or pre-filter by `Kind` using `KindIndex` if set.
2. Apply each filter field in sequence:
   - `Kind`: use `KindIndex` for O(1) lookup
   - `NameContains`: `strings.Contains(node.Name, filter.NameContains)`
   - `NameRegex`: `regexp.MustCompile(filter.NameRegex).MatchString(node.Name)`
   - `Primitive`: exact match
   - `Properties`: all specified key=val pairs must be present in `node.Properties`
   - `PropertyRegex`: regex match on property values
   - `MinEdges/MaxEdges`: count `len(outbound[id]) + len(inbound[id])`
3. Apply `Offset` / `Limit` for pagination.

### 1.4 Add `GetNodesByPattern` method

```go
// GetNodesByPattern returns all source node IDs that have an edge of the given predicate type
// pointing to a node with the given object ID (or any object if object == "").
func (c *CodePropertyGraph) GetNodesByPattern(predicate stage4.RelationshipType, objectID string) []string
```

Implementation:

- If `objectID == ""`, scan all `OutboundEdges`, collect source IDs where at least one edge matches `predicate`.
- If `objectID != ""`, scan `OutboundEdges`, check edges with matching type and TargetID.

### 1.5 Add `Match` convenience method

```go
// Match returns all nodes matching a simple pattern string.
// Pattern syntax: "kind:STRUCT" or "name:Service" or "primitive:DATABASE" or "prop:key=val"
func (c *CodePropertyGraph) Match(pattern string) []*stage4.ResolvedNode
```

Parse `pattern` prefix (`kind:`, `name:`, `primitive:`, `prop:`) and delegate to `Query`.

### 1.6 Tests

- `TestQueryByKind` — filter nodes by kind
- `TestQueryByNameContains` — substring match
- `TestQueryByProperty` — key=val match
- `TestQueryByPrimitive` — exact primitive match
- `TestQueryComposite` — multiple filters AND-ed
- `TestQueryPagination` — offset + limit
- `TestQueryEmpty` — no filters returns all nodes
- `TestGetNodesByPattern` — predicate + object filtering
- `TestMatchPattern` — all pattern prefixes

---

## P0: Test Coverage

**Files:** `internal/akg/*_test.go` (create `mvcc_test.go`, `wal_test.go`, `macros_test.go`, `serializer_test.go`)

Goal: achieve >80% line coverage across the package.

### 2.1 Graph Algorithm Tests (`internal/akg/mvcc_test.go`)

#### 2.1.1 `DetectCycles`

| Test | Scenario |
|------|----------|
| `TestDetectCycles_NoCycle` | Simple DAG with 3 nodes (A→B→C) → empty cycle list |
| `TestDetectCycles_SingleCycle` | A→B→C→A → 1 cycle with [A,B,C] |
| `TestDetectCycles_MultipleCycles` | A→B→C→A, D→E→F→D → 2 cycles |
| `TestDetectCycles_SelfLoop` | A→A → single-node self-cycle (check if included as len(1) or excluded) |
| `TestDetectCycles_EmptyGraph` | No nodes → empty |
| `TestDetectCycles_Disconnected` | A→B, C→D (two independent components, no cycle) → empty |

#### 2.1.2 `FindArticulationPoints`

| Test | Scenario |
|------|----------|
| `TestArticulationPoints_SimpleBridge` | A-B-C-D where B-C is bridge → [B,C] |
| `TestArticulationPoints_StarGraph` | Center A with leaf B,C,D → [A] only |
| `TestArticulationPoints_Cycle` | A-B-C-D-A → no articulation points |
| `TestArticulationPoints_Tree` | Root with 2 children, each with leaf → [root] |
| `TestArticulationPoints_Empty` | No nodes → empty |
| `TestArticulationPoints_Disconnected` | Two separate cliques → empty |

#### 2.1.3 `CalculatePageRank`

| Test | Scenario |
|------|----------|
| `TestPageRank_OneNode` | Single node → rank = 1.0 (or 1.0/numNodes properly) |
| `TestPageRank_TwoNodesOneEdge` | A→B: B gets damping share from A |
| `TestPageRank_Convergence` | Cycle A↔B: ranks converge to ~0.5 each |
| `TestPageRank_NoEdges` | 3 nodes, no edges → all equal |
| `TestPageRank_CustomIterations` | 1 iteration gives different result than 10 |
| `TestPageRank_Empty` | No nodes → empty map |

#### 2.1.4 `CalculateBetweennessCentrality`

| Test | Scenario |
|------|----------|
| `TestBetweenness_LineGraph` | A-B-C-D: B and C have highest centrality |
| `TestBetweenness_StarGraph` | Center A with B,C,D → A has centrality, leaves have 0 |
| `TestBetweenness_Disconnected` | Two separate edges → all 0 |
| `TestBetweenness_AllKinds` | Include FUNCTION, METHOD, CFG, DFG nodes — verify they are included |
| `TestBetweenness_Empty` | No nodes → empty map |

#### 2.1.5 `DetectGodObjects`

| Test | Scenario |
|------|----------|
| `TestGodObjects_None` | 3 nodes with fan-in 1, fan-out 1 → none |
| `TestGodObjects_OneGod` | 1 node with fan-in 15, fan-out 15 → detected |
| `TestGodObjects_Boundaries` | Threshold floor (10) — verify node at threshold-1 is excluded |
| `TestGodObjects_OnlyRelevantKinds` | FUNCTION/METHOD with extreme degrees → not flagged (only STRUCT/CLASS/MODULE/FILE) |
| `TestGodObjects_Empty` | No nodes → empty |

#### 2.1.6 `FindIsolatedIslands`

| Test | Scenario |
|------|----------|
| `TestIslands_None` | Fully connected graph with entrypoint → no islands |
| `TestIslands_OneIsland` | Separate component with no entrypoint → 1 island |
| `TestIslands_IslandHasEntrypoint` | Separate component with entrypoint → not an island |
| `TestIslands_Singleton` | Single node with no edges, not an entrypoint → should it be an island? (size>1 rule) |
| `TestIslands_MultipleIslands` | 3 disconnected groups, 1 has entrypoint → 2 islands |
| `TestIslands_Empty` | No nodes → empty |

#### 2.1.7 `GetStructuralSimilarity`

| Test | Scenario |
|------|----------|
| `TestSimilarity_Identical` | Same outbound targets → 1.0 |
| `TestSimilarity_NoOverlap` | Different outbound → 0.0 |
| `TestSimilarity_Partial` | Some shared targets → intermediate value |
| `TestSimilarity_BothEmpty` | No edges for either → 1.0 |

#### 2.1.8 `GetTopologicalSort`

| Test | Scenario |
|------|----------|
| `TestTopoSort_DAG` | A→B→C → [A,B,C], true |
| `TestTopoSort_WithCycle` | A→B→C→A → partial sort, false |
| `TestTopoSort_Disconnected` | A→B, C→D → any valid order |
| `TestTopoSort_SingleNode` | A → [A], true |
| `TestTopoSort_Empty` | No nodes → empty, true |

#### 2.1.9 `FindPath`

| Test | Scenario |
|------|----------|
| `TestFindPath_Exists` | A→B→C: FindPath(A,C,10) → [A,B,C] |
| `TestFindPath_NoPath` | Disconnected → nil |
| `TestFindPath_MaxDepth` | A→B→C→D, maxDepth=2 → nil (path too long) |
| `TestFindPath_SameNode` | A→A or FindPath(A,A,10) → immediate path |
| `TestFindPath_MissingNodes` | start or target doesn't exist → nil |

#### 2.1.10 `GetOrphanNodes`

| Test | Scenario |
|------|----------|
| `TestOrphans_Mixed` | 2 with inbound, 1 without and not entrypoint → orphan |
| `TestOrphans_Entrypoint` | 1 without inbound but is entrypoint → excluded |
| `TestOrphans_None` | All have inbound or are entrypoints → empty |

### 2.2 MVCC Clone Tests (`internal/akg/mvcc_test.go`)

| Test | Scenario |
|------|----------|
| `TestClone_Nil` | Clone(nil) → fresh graph |
| `TestClone_Empty` | Clone(empty graph) → empty graph |
| `TestClone_DeepCopy` | Modify a node's Properties in clone → original unaffected |
| `TestClone_DeepEdges` | Append to OutboundEdges in clone → original length unchanged |
| `TestClone_DeepKindIndex` | Delete from KindIndex in clone → original index intact |
| `TestClone_DeepHashIndex` | Delete from HashIndex in clone → original index intact |
| `TestClone_DeepEntrypoints` | Append to Entrypoints in clone → original unchanged |
| `TestClone_DeepFileNodeIndex` | Delete from FileNodeIndex in clone → original unchanged |
| `TestClone_DeepErrors` | Clone errors slice → modifications are independent |
| `TestClone_LargeGraph_Parallel` | 1000 nodes, verify no data race (`-race`) |
| `TestMVCC_AllocateShadow` | AllocateShadowSnapshot → returns graph with incremented Version |
| `TestMVCC_Promote` | PromoteShadowSnapshot → GetSnapshot returns new graph |
| `TestMVCC_Isolation` | Allocate → modify shadow → snapshot still shows old state |
| `TestMVCC_ConcurrentAccess` | 10 goroutines reading, 1 writing → no race (`-race`) |

### 2.3 WAL Tests (`internal/akg/wal_test.go`)

| Test | Scenario |
|------|----------|
| `TestWAL_AppendAndRead` | Append entry, ReadAllEntries → entry returned |
| `TestWAL_MarkCommitted` | Append STARTED, MarkCommitted, ReadAllEntries → status=COMMITTED |
| `TestWAL_EmptyRead` | No file exists → nil, no error |
| `TestWAL_AppendAfterRotation` | Force rotation, append to new segment, read all entries from all segments |
| `TestWAL_ConcurrentAppend` | 10 goroutines appending → all entries readable, no corruption |
| `TestWAL_CheckpointTriggersRotation` | Fill file past 100MB → Checkpoint returns true |
| `TestWAL_RecoverReplay` | Create STARTED+COMMITTED entries, run Recover → graph state matches |
| `TestWAL_RecoverSkipsUncommitted` | Create STARTED without COMMITTED → Recover ignores entry |
| `TestWAL_EntryRoundTrip` | Full WALEntry with Payload → read back, verify all fields match |

### 2.4 Transaction Lifecycle CRUD Tests (`internal/akg/transaction_manager_test.go`)

| Test | Scenario |
|------|----------|
| `TestExecuteDelta_NilPayload` | nil payload → no error, no change |
| `TestExecuteDelta_EmptyPayload` | Empty Stage4Output → no error |
| `TestExecuteDelta_AddNodes` | Add 3 nodes → graph has them |
| `TestExecuteDelta_DeleteNodes` | Add node, then delete via modifiedFiles → node gone |
| `TestExecuteDelta_AddEdges` | Add 2 nodes + edge → edge resolvable |
| `TestExecuteDelta_MultipleDeltas` | Sequential deltas → cumulative state |
| `TestExecuteDelta_SweepRemovesOldEdges` | Node in modified file, edge to it → edge cleaned |
| `TestExecuteDelta_DanglingReferenceAudit` | Add edge to missing node → Errors populated |
| `TestExecuteDelta_MacroInferenceFires` | Add node with "Service" name → macro_rules populated |
| `TestGetActiveSnapshot_Immutable` | Get snapshot, modify original → snapshot unchanged |
| `TestSubscribe_ReceivesEvent` | Subscribe, execute delta → event received |
| `TestLock_AcquireRelease` | AcquireLock twice in same process → second succeeds |
| `TestLock_Contention` | Simulate concurrent lock attempt → fails with timeout |

### 2.5 Turtle Serializer Round-Trip Tests (`internal/akg/serializer_test.go`)

| Test | Scenario |
|------|----------|
| `TestSerializeRoundTrip` | Create graph → SerializeToTurtle → ParseTTLFile → verify nodes+edges match |
| `TestSerializeDelta` | SerializeDeltaToTurtle with deleted nodes → tombstone triples present |
| `TestSerializeNilGraph` | nil graph → error |
| `TestSerializeAllKinds` | Graph with every kind → each maps to correct class |
| `TestSerializeAllEdgeTypes` | Graph with every edge type → each maps to correct predicate |
| `TestSerializeProperties` | Node with Properties → each becomes gm:key "val" |
| `TestSerializeEntrypoints` | Entrypoint flag → gm:isEntrypoint true |
| `TestSerializeEmptyGraph` | Empty graph → no node triples, just prefixes and metadata |

### 2.6 Macro Inference Tests (`internal/akg/macros_test.go`)

Beyond existing `reasoner_test.go`:

| Test | Scenario |
|------|----------|
| `TestMacroRule_DeadCode` | Orphan node → Rule 29 fired |
| `TestMacroRule_CircularDependency` | Cycle A→B→C→A → Rule 30 fired on each |
| `TestMacroRule_ArticulationPoint` | Bridge node → Rule 31 fired |
| `TestMacroRule_HighBlastRadius` | Node with impact radius > 50 → Rule 32 |
| `TestMacroRule_HighPageRank` | Node with PR > 5/N → Rule 33 |
| `TestMacroRule_IsolatedIsland` | Disconnected group → Rule 34 |
| `TestMacroRule_GodObject` | Extreme fan-in/fan-out → Rule 35 |
| `TestMacroRule_ArchitecturalBridge` | High betweenness → Rule 36 |
| `TestMacroRule_LowCohesion` | Package with 0 internal edges → Rule 37 |
| `TestMacroRuler_ServiceLayer` | Node named "xxxService" → Rule 6 fired |
| `TestMacroRule_NetworkIO` | Node with primitive=NETWORK_IO → Rule 4 fired |

### 2.7 `CalculateInstability` / `CalculateImpactRadius` / `CalculatePackageCohesion` Tests

| Test | Scenario |
|------|----------|
| `TestInstability_Stable` | 5 inbound, 0 outbound → 0.0 |
| `TestInstability_Unstable` | 0 inbound, 5 outbound → 1.0 |
| `TestInstability_Balanced` | 3 inbound, 3 outbound → 0.5 |
| `TestInstability_Isolated` | 0 inbound, 0 outbound → 0.0 (default) |
| `TestImpactRadius_NoDependents` | No inbound edges → 0 |
| `TestImpactRadius_Transitive` | A depends on B, B depends on C → impact(C) = 2 |
| `TestImpactRadius_Self` | Impact on itself → 0 (visited set excludes self from count) |
| `TestPackageCohesion_High` | 3 components, 6 internal edges → 2.0 |
| `TestPackageCohesion_Low` | 3 components, 0 internal edges → 0.0 |
| `TestPackageCohesion_NoComponents` | No nodes point to package → 0.0 |
| `TestPackageCohesion_SingleComponent` | 1 component, 0 internal edges → 0.0 |

---

## P1: Graph Algorithm Completeness

**Files:** `internal/akg/mvcc.go`

### 3.1 Fix `CalculateBetweennessCentrality`

**Problem:** Only runs on "major nodes" (STRUCT/CLASS/MODULE/PACKAGE/FILE). Ignores FUNCTIONS, METHODS, CFG_BRANCH, DFG_VAR — this misses real architectural bridges at the function level.

**Fix:**

1. Change the initial filter from the restrictive set to include ALL node kinds:

```go
for id := range c.Nodes {
    majorNodes = append(majorNodes, id)
    bc[id] = 0.0
}
```

2. Re-measure performance impact. For large graphs (6M nodes), the original restriction was a performance optimization. **Instead of removing the restriction entirely, make it configurable:**

```go
func (c *CodePropertyGraph) CalculateBetweennessCentrality(includeAll bool) map[string]float64
```

- `includeAll=false`: current behavior (major nodes only)
- `includeAll=true`: run on ALL nodes

In `reasoner.go`, call with `includeAll=true` but add a warning if the graph exceeds 100K nodes:

```go
betweenness := graph.CalculateBetweennessCentrality(len(graph.Nodes) < 100000)
```

3. Add a test that verifies inclusion of FUNCTION/METHOD/CFG nodes.

### 3.2 Fix `DetectGodObjects`

**Problem:** Already fixed (only checks STRUCT/CLASS/MODULE/FILE). But the threshold floor of `thresholdIn < 10` and `thresholdOut < 10` is overly aggressive — in repos with <10 components of each kind, nearly everything gets flagged.

**Fix:** 

- Compute threshold in terms of total relevant nodes: `threshold = max(mean+2*stddev, sqrt(totalNodes))` instead of hardcoded 10.
- Add a test with <10 nodes showing no false positives.

### 3.3 Add `CalculateBetweennessCentrality` tests with mixed kinds

- Add a test graph that includes FUNCTION and METHOD nodes on shortest paths.
- Verify that `includeAll=true` includes them and the centrality values differ from `includeAll=false`.

---

## P1: Concurrent Safety

**Files:** `internal/akg/mvcc.go`

### 4.1 Add internal `sync.RWMutex` to `CodePropertyGraph`

**Problem:** `CodePropertyGraph` has no internal mutex. A caller that obtains a "read-only" snapshot via `GetSnapshot()` can still mutate slice internals, corrupting state for other readers.

**Fix:**

1. Add a `sync.RWMutex` field to `CodePropertyGraph`:

```go
type CodePropertyGraph struct {
    mu sync.RWMutex `json:"-"` // protects all mutable fields below
    // ... existing fields
}
```

2. Add read-locked wrapper methods for all public read accessors:

```go
func (c *CodePropertyGraph) SafeGetNode(id string) (*stage4.ResolvedNode, bool) {
    c.mu.RLock()
    defer c.mu.RUnlock()
    return c.GetNode(id)
}

func (c *CodePropertyGraph) SafeGetOutboundEdges(id string) []stage4.ResolvedEdge {
    c.mu.RLock()
    defer c.mu.RUnlock()
    return c.GetOutboundEdges(id)
}

func (c *CodePropertyGraph) SafeGetInboundEdges(id string) []stage4.ResolvedEdge {
    c.mu.RLock()
    defer c.mu.RUnlock()
    return c.GetInboundEdges(id)
}

func (c *CodePropertyGraph) SafeGetNodesByKind(kind string) []*stage4.ResolvedNode {
    c.mu.RLock()
    defer c.mu.RUnlock()
    return c.GetNodesByKind(kind)
}

func (c *CodePropertyGraph) SafeQuery(filter QueryFilter) []*stage4.ResolvedNode {
    c.mu.RLock()
    defer c.mu.RUnlock()
    return c.Query(filter)
}
```

3. **Crucially**: The `Clone()` method must also acquire the read lock to ensure a consistent snapshot of all maps/slices:

```go
func (cpg *CodePropertyGraph) Clone() *CodePropertyGraph {
    cpg.mu.RLock()
    defer cpg.mu.RUnlock()
    // ... existing clone logic
}
```

4. In `MVCCGraphContainer.GetSnapshot()`, return a **clone** instead of raw pointer (or document the threading contract clearly). The current design returns the raw pointer, which means external callers can mutate through the pointer. **Decision:** Keep returning raw pointer (for performance) but internal callers must use `Safe*` methods or hold the mutex. Document that `GetSnapshot()` returns a pointer that should be treated as read-only.

### 4.2 Add `-race` test suite

Create a dedicated test file `internal/akg/race_test.go` with:

```go
// +build race

package akg

// TestConcurrentReadWrite_NoRace verifies that concurrent reads and writes
// through the Safe* methods do not trigger the race detector.
func TestConcurrentReadWrite_NoRace(t *testing.T) {
    graph := buildGraph(100)
    var wg sync.WaitGroup
    for i := 0; i < 20; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            for j := 0; j < 100; j++ {
                graph.SafeGetNode("node_0")
                graph.SafeGetOutboundEdges("node_0")
                graph.SafeQuery(QueryFilter{Kind: "FUNCTION"})
            }
        }()
    }
    wg.Wait()
}
```

Run with: `go test -race ./internal/akg/`

---

## P2: Scalability — Lazy Clone

**Files:** `internal/akg/mvcc.go`, `internal/akg/mvcc_lazy.go` (new)

### 5.1 Problem Analysis

`Clone()` copies the **entire graph** every transaction — all maps, all slices, all values. For a 100K-file repo with ~6M nodes:
- `Nodes`: ~6M map entries → ~500MB heap
- `OutboundEdges`: ~6M keys → ~500MB more
- `InboundEdges`: ~6M keys → ~500MB more
- Indices: ~500MB more
- **Total per clone**: ~2GB. With a naive commit-per-file strategy, this is unsustainable.

### 5.2 Solution: Copy-on-Write (COW) using Btree

**Strategy:** Replace `map[string]*stage4.ResolvedNode` with an **immutable Btree-based map** that supports structural sharing.

1. **Use `github.com/google/btree`** (or a simple immutable map with a version chain). The Btree approach:
   - Every write creates a new leaf-to-root path (O(log N) copying)
   - Reads are lock-free on the old versions
   - `Clone()` becomes O(1) — just copy the root pointer + version counter

2. **Define a generic `CowMap[K, V]`**:

```go
// CowMap is a copy-on-write map with O(log N) writes and O(1) clones.
type CowMap[K comparable, V any] struct {
    // Implementation: immutable AVL tree or btree with versioned root.
    // Write: clone the search path (log N nodes), return new root.
    // Read: traverse current root (no lock needed for immutable nodes).
    root atomic.Pointer[cowNode[K, V]]
}

type cowNode[K comparable, V any] struct {
    key    K
    val    V
    left   *cowNode[K, V]
    right  *cowNode[K, V]
    height int
}
```

3. **Fields to convert** (from `map[K]V` to `CowMap`):

| Field | Current Type | New Type |
|-------|-------------|----------|
| `Nodes` | `map[string]*stage4.ResolvedNode` | `CowMap[string, *stage4.ResolvedNode]` |
| `OutboundEdges` | `map[string][]stage4.ResolvedEdge` | `CowMap[string, []stage4.ResolvedEdge]` |
| `InboundEdges` | `map[string][]stage4.ResolvedEdge` | `CowMap[string, []stage4.ResolvedEdge]` |
| `KindIndex` | `map[string]map[string]bool` | `CowMap[string, CowMap[string, bool]]` |
| `HashIndex` | `map[string][]string` | `CowMap[string, []string]` |
| `FileNodeIndex` | `map[string]map[string]bool` | Stays as `map[string]CowMap[string, bool]` (small cardinality) |
| `MacroRules` | `map[string][]string` | `CowMap[string, []string]` |
| `FolderZones` | `map[string]string` | `CowMap[string, string]` |

4. **`Clone()` becomes:**

```go
func (cpg *CodePropertyGraph) Clone() *CodePropertyGraph {
    cpg.mu.RLock()
    defer cpg.mu.RUnlock()
    // Atomic pointer copy — O(1) for each COW map
    clone := &CodePropertyGraph{
        Version:       cpg.Version,
        Nodes:         cpg.Nodes.Clone(),          // root pointer copy
        OutboundEdges: cpg.OutboundEdges.Clone(),  // root pointer copy
        InboundEdges:  cpg.InboundEdges.Clone(),   // root pointer copy
        // ... remaining fields
    }
    return clone
}
```

5. **No 8 parallel goroutines needed** — the entire clone is a handful of atomic pointer swaps.

### 5.3 Write path changes

All mutation sites must use COW write pattern:

```go
// Before:
s.Nodes[id] = node

// After:
s.Nodes = s.Nodes.Set(id, node)
```

### 5.4 Migration plan

1. Implement `CowMap` (immutable AVL tree) with `Get`, `Set`, `Delete`, `Len`, `Iterate`, `Clone`, `Snapshot` (returns map for serialization).
2. Write unit tests for `CowMap`: `TestCowMap_Set`, `TestCowMap_Get`, `TestCowMap_Delete`, `TestCowMap_CloneIsolation`, `TestCowMap_CloneIsO1`, `TestCowMap_Race`.
3. Convert `CodePropertyGraph` fields one at a time, testing after each conversion.
4. Update all usage sites in `mvcc.go`, `transaction_manager.go`, `reasoner.go`.
5. Remove `sync.WaitGroup` + 8 goroutine clone pattern.
6. Benchmark: `go test -bench=BenchmarkClone -benchmem ./internal/akg/`

### 5.5 Performance targets

| Metric | Before | After |
|--------|--------|-------|
| Clone(1000 nodes) | ~50µs, 500KB alloc | ~500ns, 0 alloc (root ptr copy) |
| Clone(1M nodes) | ~50ms, 500MB alloc | ~1µs, 0 alloc |
| Set 1 node in clone | ~50ms (full copy) | ~1µs (log N copy) |
| Concurrent reads | blocked during clone | unblocked |

---

## P2: Performance — Bounded Clone Goroutines

**Files:** `internal/akg/mvcc.go`

### 6.1 Problem

`Clone()` spawns 8 goroutines unconditionally. If `Clone()` is called concurrently (by multiple callers), this creates 8× goroutines, potentially unbounded. On a 6M-node graph, each goroutine iterates millions of entries.

### 6.2 Fix

**If COW is implemented (Section 5), this entire section is automatically solved** — Clone becomes O(1) with no goroutines.

**If COW is deferred:** Replace unconditional 8-goroutine spawn with a **worker pool** bounded by `runtime.GOMAXPROCS(0)`:

```go
func (cpg *CodePropertyGraph) Clone() *CodePropertyGraph {
    // ... initialize clone

    numWorkers := runtime.GOMAXPROCS(0)
    if numWorkers < 2 {
        numWorkers = 2
    }

    tasks := []struct{
        name string
        fn func()
    }{
        {"Nodes", func() { /* copy Nodes */ }},
        {"OutboundEdges", func() { /* copy OutboundEdges */ }},
        // ... more tasks
    }

    taskCh := make(chan func(), len(tasks))
    var wg sync.WaitGroup
    for i := 0; i < numWorkers; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            for task := range taskCh {
                task()
            }
        }()
    }
    for _, t := range tasks {
        taskCh <- t.fn
    }
    close(taskCh)
    wg.Wait()
    return clone
}
```

---

## P2: Turtle Serializer Fixes

**Files:** `internal/akg/turtle_serializer.go`

### 7.1 Fix `EdgeReferences` → `gm:references` (not `gm:dataFlowTo`)

**Problem:** Line 194: `case stage4.EdgeReferences: return "gm:dataFlowTo"`. This is a semantic collision — `REFERENCES` and `DATA_FLOW` both map to `gm:dataFlowTo`.

**Fix:**

1. Add `gm:references` to `ontology.ttl`:

```turtle
gm:references a rdf:Property ;
    rdfs:domain rdfs:Resource ;
    rdfs:range rdfs:Resource ;
    rdfs:label "references" ;
    rdfs:comment "Represents a generic reference from one code element to another (import, type reference, etc.)." .
```

2. Change `mapEdgeTypeToPredicate`:

```go
case stage4.EdgeReferences:
    return "gm:references"
```

### 7.2 Remove spurious `a rdfs:Resource` on every node

**Problem:** Line 118: `fmt.Fprintf(w, "    a rdfs:Resource .\n\n")` is appended to EVERY node, even after the type declaration `a gm:File ;`. This produces:

```turtle
<uri> a gm:File ;
    gm:name "x" ;
    a rdfs:Resource .
```

This is invalid Turtle — `a` (rdf:type) can only appear once per subject, and here it appears twice.

**Fix:**

Replace the trailing `a rdfs:Resource .` with a simple `.` to close the statement:

```go
// Close statement
fmt.Fprintf(w, "    .\n\n")
```

Or better, restructure serialization so that `a ClassType` is written as the last triple before the period:

```go
// Write all properties and the type statement
fmt.Fprintf(w, "%s\n", nodeURI)
fmt.Fprintf(w, "    a %s ;\n", classType)
fmt.Fprintf(w, "    gm:name \"%s\" ;\n", escapeLiteral(node.Name))
// ... other properties ...
if entrypointSet[nodeID] {
    fmt.Fprintf(w, "    gm:isEntrypoint true ;\n")
}
fmt.Fprintf(w, "    .\n\n") // Close with period, no extra a rdfs:Resource
```

### 7.3 Add mapping for `EdgeDependsOn`, `EdgeContains`, `EdgeMixes`, `EdgeHasField`, `EdgeHasParam`, `EdgeReturns`

**Problem:** These edge types are defined in `RelationshipType` but have no mapping in `mapEdgeTypeToPredicate` — they fall through to `rdfs:seeAlso`.

**Fix:** Add proper predicate entries:

```go
case stage4.EdgeContains:
    return "gm:contains"
case stage4.EdgeDependsOn:
    return "gm:dependsOn"
case stage4.EdgeMixes:
    return "gm:mixes"
case stage4.EdgeHasField:
    return "gm:hasField"
case stage4.EdgeHasParam:
    return "gm:hasParam"
case stage4.EdgeReturns:
    return "gm:returns"
```

And corresponding entries in `ontology.ttl`.

### 7.4 Add class mapping for missing kinds in `mapKindToClass`

**Problem:** Many kinds fall through to `rdfs:Class` generic:

Add mappings:
- `CFG_SUMMARY`, `DFG_SUMMARY` → `gm:ControlStructure` or `gm:Block`
- `EVENT_TOPIC` → `gm:Annotation`
- `VIRTUAL_DATABASE`, `VIRTUAL_ENDPOINT` → `gm:Annotation`

---

## P2: PackageCohesion — Formal BELONGS_TO Edge

**Files:** `internal/akg/mvcc.go`, `internal/code_analysis_engine/stage4/type.go`

### 8.1 Problem

`CalculatePackageCohesion` (line 555-577 of mvcc.go) uses a fragile heuristic: it assumes that edges **targeting** the package ID represent BELONGS_TO relationships. This is not a formal relationship — any edge to a package/module node is treated as membership, which is incorrect.

```go
for _, e := range c.GetOutboundEdges(id) {
    if e.TargetID == packageID { // Fragile heuristic
        components = append(components, id)
    }
}
```

### 8.2 Solution

1. **Define `EdgeBelongsTo` in `RelationshipType`:**

```go
// In internal/code_analysis_engine/stage4/type.go
EdgeBelongsTo RelationshipType = "BELONGS_TO"
```

2. **Update `mapEdgeTypeToPredicate`:**

```go
case stage4.EdgeBelongsTo:
    return "gm:belongsTo"
```

3. **Update `ontology.ttl`:**

```turtle
gm:belongsTo a rdf:Property ;
    rdfs:domain rdfs:Resource ;
    rdfs:range rdfs:Resource ;
    rdfs:label "belongsTo" ;
    rdfs:comment "Formal membership relationship: a node belongs to a containing package, module, or file." .
```

4. **Fix `CalculatePackageCohesion`:**

```go
func (c *CodePropertyGraph) CalculatePackageCohesion(packageID string) float64 {
    var components []string
    componentSet := make(map[string]bool)

    // Use formal BELONGS_TO edge instead of fragile heuristics
    for _, edge := range c.GetInboundEdges(packageID) {
        if edge.Type == stage4.EdgeBelongsTo {
            components = append(components, edge.SourceID)
            componentSet[edge.SourceID] = true
        }
    }
    // ... rest unchanged
}
```

5. **Fallback for legacy graphs:** If no `BELONGS_TO` edges exist, fall back to the old heuristic (scanning all edges targeting `packageID`), but emit a deprecation warning.

---

## P2: Macro Inference — Caching + Configurable Rules

**Files:** `internal/akg/reasoner.go`, `internal/akg/reasoner_cache.go` (new)

### 9.1 Result Caching

**Problem:** Every call to `RunTopologicalMacroInference` re-runs ALL 45 rules against ALL relevant nodes. For a 6M-node graph with frequent deltas, this is wasteful — only the delta nodes need fresh inference.

**Solution:**

1. **Add a `macroCache` field to `CodePropertyGraph`:**

```go
type CodePropertyGraph struct {
    // ...
    macroCache map[string][]string `json:"-"` // cached macro rules per node ID
    macroHash  string              `json:"-"` // hash of all inputs that influenced the cache
}
```

2. **Keyed cache invalidation:** Store a per-node content hash:

```go
func nodeMacroKey(node *stage4.ResolvedNode, graph *CodePropertyGraph) string {
    h := sha256.New()
    h.Write([]byte(node.ID))
    h.Write([]byte(node.Kind))
    h.Write([]byte(node.Name))
    h.Write([]byte(node.Primitive))
    // Include all edge targets
    for _, e := range graph.OutboundEdges[node.ID] {
        h.Write([]byte(e.TargetID))
        h.Write([]byte(e.Type))
    }
    return fmt.Sprintf("%x", h.Sum(nil))
}
```

3. **Caching logic in `RunTopologicalMacroInference`:**

```go
func RunTopologicalMacroInference(graph *CodePropertyGraph, config ...stage4.LinkerConfig) {
    // ... existing setup

    if graph.macroCache == nil {
        graph.macroCache = make(map[string][]string)
    }

    for nodeID, node := range graph.Nodes {
        if node == nil { continue }
        if node.Kind == "MODULE" || node.Kind == "FILE" || node.Kind == "STRUCT" || node.Kind == "CLASS" || node.Kind == "FUNCTION" {
            cacheKey := nodeMacroKey(node, graph)
            if cached, ok := graph.macroCache[cacheKey]; ok {
                // Restore cached result
                graph.MacroRules[nodeID] = cached
                if node.Properties == nil {
                    node.Properties = make(map[string]string)
                }
                node.Properties["macro_rules"] = strings.Join(cached, " | ")
                continue
            }

            // Run inference (existing logic)
            wg.Add(1)
            sem <- struct{}{}
            go func(id string, n *stage4.ResolvedNode, key string) {
                defer wg.Done()
                defer func() { <-sem }()
                inferMacroRulesForNode(id, n, graph, &mu, macroMode)

                // Cache the result
                mu.Lock()
                graph.macroCache[key] = graph.MacroRules[id]
                mu.Unlock()
            }(nodeID, node, cacheKey)
        }
    }
    // ...
}
```

4. **Cache invalidation during sweep phase:** In `applyDeltaToShadow` in `transaction_manager.go`, reset the macro cache:

```go
shadow.macroCache = nil // invalidate cache on any write
```

### 9.2 User-Configurable Rule Sets

**Problem:** Currently all 45 rules are hardcoded in `inferMacroRulesForNode`. There is no way to enable/disable individual rules or groups of rules.

**Solution:**

1. **Add `DisabledRules` field to `LinkerConfig`:**

```go
type LinkerConfig struct {
    // ... existing fields
    DisabledRules []string `json:"disabled_rules,omitempty"` // Rule IDs to skip, e.g. ["rule_5", "rule_13"]
}
```

2. **Assign every rule a stable ID:**

```go
const (
    RuleID_WebToStorage      = "rule_01"
    RuleID_SecurityGate      = "rule_02"
    RuleID_AsyncBackground   = "rule_03"
    // ... all 37
)
```

3. **Add a `RuleDefinition` registry:**

```go
type RuleDefinition struct {
    ID      string
    Name    string
    Tier    string // RuleTierHeuristic, RuleTierStructural, RuleTierArchitectural
    Enabled func(node *stage4.ResolvedNode, graph *CodePropertyGraph, disabledRules map[string]bool) bool
    Apply   func(node *stage4.ResolvedNode, graph *CodePropertyGraph) string
}

var RuleRegistry = []RuleDefinition{
    {ID: "rule_01", Name: "Web-to-Storage Traffic", Tier: RuleTierStructural,
        Enabled: func(n *stage4.ResolvedNode, g *CodePropertyGraph, dr map[string]bool) bool {
            if dr["rule_01"] { return false }
            return true // + tier check via shouldApplyRule
        },
        Apply: func(n *stage4.ResolvedNode, g *CodePropertyGraph) string {
            // ... existing rule logic
        },
    },
    // ... all 37 rules
}
```

4. **Replace hardcoded if-blocks in `inferMacroRulesForNode`** with a loop over `RuleRegistry`:

```go
func inferMacroRulesForNode(nodeID string, node *stage4.ResolvedNode, graph *CodePropertyGraph, mu *sync.Mutex, macroMode string) {
    // ... existing DFS walk to collect primitives, flags ...

    disabledSet := make(map[string]bool)
    // populate from config if available

    for _, rule := range RuleRegistry {
        if !shouldApplyRule(rule.Tier, macroMode) {
            continue
        }
        if rule.Enabled(node, graph, disabledSet) {
            result := rule.Apply(node, graph)
            if result != "" {
                inferredRules = append(inferredRules, result)
            }
        }
    }
    // ... rest unchanged
}
```

### 9.3 Tests

| Test | Scenario |
|------|----------|
| `TestMacroCache_Hit` | Same node twice → second call uses cache |
| `TestMacroCache_Invalidation` | Modify node → cache cleared |
| `TestMacroCache_DifferentNodes` | Different nodes = different cache keys |
| `TestDisabledRules_ExcludeRule` | Disable rule_06 → Service node not tagged |
| `TestDisabledRules_AllowOthers` | Disable rule_06, enable rule_07 → only rule_07 fires |
| `TestRuleRegistry_AllRules` | Every rule in registry fires under right conditions |

---

## P2: Ontology Completeness

**Files:** `internal/akg/ontology.ttl`

### 10.1 Missing Class Mappings

Add the following classes (kinds) that are produced by the analysis pipeline:

```turtle
gm:CFGSummary a rdfs:Class ;
    rdfs:label "CFGSummary" ;
    rdfs:comment "Aggregate control flow summary node for a function or method (standard level of detail)." .

gm:DFGSummary a rdfs:Class ;
    rdfs:label "DFGSummary" ;
    rdfs:comment "Aggregate data flow summary node for a function or method (standard level of detail)." .

gm:EventTopic a rdfs:Class ;
    rdfs:label "EventTopic" ;
    rdfs:comment "Virtual node representing a message queue topic or event channel." .

gm:VirtualDatabase a rdfs:Class ;
    rdfs:label "VirtualDatabase" ;
    rdfs:comment "Virtual node representing a logical database or data store." .

gm:VirtualEndpoint a rdfs:Class ;
    rdfs:label "VirtualEndpoint" ;
    rdfs:comment "Virtual node representing a logical API endpoint or service boundary." .
```

### 10.2 Missing Property Mappings

```turtle
gm:isEntrypoint a rdf:Property ;
    rdfs:domain rdfs:Resource ;
    rdfs:range xsd:boolean ;
    rdfs:label "isEntrypoint" ;
    rdfs:comment "Flag indicating this node is a root execution entrypoint (main, HTTP handler, etc.)." .

gm:primitiveZone a rdf:Property ;
    rdfs:domain rdfs:Resource ;
    rdfs:range xsd:string ;
    rdfs:label "primitiveZone" ;
    rdfs:comment "Architectural zone classification (e.g., DATABASE_ZONE, API_ZONE)." .
```

### 10.3 Missing Edge Predicates

Add all predicates that `mapEdgeTypeToPredicate` emits but `ontology.ttl` is missing:

```turtle
gm:extends a rdf:Property ;
    rdfs:domain gm:TypeDecl ;
    rdfs:range gm:TypeDecl ;
    rdfs:label "extends" .

gm:throws a rdf:Property ;
    rdfs:domain gm:Executable ;
    rdfs:range gm:TypeDecl ;
    rdfs:label "throws" .

gm:exposesEndpoint a rdf:Property ;
    rdfs:domain gm:Executable ;
    rdfs:range rdfs:Resource ;
    rdfs:label "exposesEndpoint" .

gm:securitySink a rdf:Property ;
    rdfs:domain gm:Executable ;
    rdfs:range rdfs:Resource ;
    rdfs:label "securitySink" .

gm:consumesResource a rdf:Property ;
    rdfs:domain gm:Executable ;
    rdfs:range rdfs:Resource ;
    rdfs:label "consumesResource" .

gm:mutatesGlobal a rdf:Property ;
    rdfs:domain gm:Executable ;
    rdfs:range rdfs:Resource ;
    rdfs:label "mutatesGlobal" .

gm:aliasesType a rdf:Property ;
    rdfs:domain rdfs:Resource ;
    rdfs:range rdfs:Resource ;
    rdfs:label "aliasesType" .

gm:aliasesPointer a rdf:Property ;
    rdfs:domain rdfs:Resource ;
    rdfs:range rdfs:Resource ;
    rdfs:label "aliasesPointer" .

gm:vulnerableTaint a rdf:Property ;
    rdfs:domain rdfs:Resource ;
    rdfs:range rdfs:Resource ;
    rdfs:label "vulnerableTaint" .

gm:instantiatesGeneric a rdf:Property ;
    rdfs:domain gm:Executable ;
    rdfs:range gm:TypeDecl ;
    rdfs:label "instantiatesGeneric" .

gm:sendsMessage a rdf:Property ;
    rdfs:domain gm:Executable ;
    rdfs:range rdfs:Resource ;
    rdfs:label "sendsMessage" .

gm:receivesMessage a rdf:Property ;
    rdfs:domain gm:Executable ;
    rdfs:range rdfs:Resource ;
    rdfs:label "receivesMessage" .

gm:cyclicDependency a rdf:Property ;
    rdfs:domain rdfs:Resource ;
    rdfs:range rdfs:Resource ;
    rdfs:label "cyclicDependency" .

gm:networkCall a rdf:Property ;
    rdfs:domain gm:Executable ;
    rdfs:range rdfs:Resource ;
    rdfs:label "networkCall" .

gm:queriesDatabase a rdf:Property ;
    rdfs:domain gm:Executable ;
    rdfs:range rdfs:Resource ;
    rdfs:label "queriesDatabase" .

gm:callsCloudAPI a rdf:Property ;
    rdfs:domain gm:Executable ;
    rdfs:range rdfs:Resource ;
    rdfs:label "callsCloudAPI" .

gm:catchesException a rdf:Property ;
    rdfs:domain gm:Executable ;
    rdfs:range gm:TypeDecl ;
    rdfs:label "catchesException" .

gm:defersExecution a rdf:Property ;
    rdfs:domain gm:Executable ;
    rdfs:range gm:Executable ;
    rdfs:label "defersExecution" .
```

### 10.4 Update `mapKindToClass` in `turtle_serializer.go`

Add missing kind mappings:

```go
case "CFG_SUMMARY":
    return "gm:CFGSummary"
case "DFG_SUMMARY":
    return "gm:DFGSummary"
case "EVENT_TOPIC":
    return "gm:EventTopic"
case "VIRTUAL_DATABASE":
    return "gm:VirtualDatabase"
case "VIRTUAL_ENDPOINT":
    return "gm:VirtualEndpoint"
case "BLOCK":
    return "gm:Block"
case "ANNOTATION", "DECORATOR":
    return "gm:Annotation"
```

---

## Verification

### Test Commands

```bash
# All AKG tests
go test ./internal/akg/... -v -count=1

# With race detector
go test -race ./internal/akg/... -v -count=1

# Benchmarks
go test -bench=. -benchmem ./internal/akg/...

# Coverage
go test -coverprofile=akg.cov ./internal/akg/...
go tool cover -html=akg.cov -o akg.cov.html

# Build
go build ./internal/akg/...

# Vet
go vet ./internal/akg/...
```

### Success Criteria

| Area | Minimum | Target |
|------|---------|--------|
| Test count | 100+ | 150+ |
| Line coverage | 80% | 90%+ |
| Race condition | 0 (`-race` clean) | 0 |
| Build | `go build` passes | `go vet` clean |
| Query | `Query(kind, prop)`, `GetNodesByPattern`, `Match` work | SPARQL-like expressiveness |
| Clone(1M nodes) | <1µs with COW | <500ns |
| Turtle round-trip | All 27 edge types round-trip clean | All 37 edge types |
| PackageCohesion | Uses `EdgeBelongsTo` | Backward-compatible fallback |
| Macro inference | Cache hit avoids re-inference | Configurable rule registry |
| Betweenness centrality | Includes all node kinds | Configurable filter |

### Implementation Order

| Phase | Items | Effort | Impact |
|-------|-------|--------|--------|
| **1** | P0 Query API + Test Coverage (all graph algos, MVCC, WAL) | 3-4 days | Highest (unblocks everything) |
| **2** | Turtle fixes + Ontology + PackageCohesion + EdgeBelongs | 1 day | High (correctness) |
| **3** | Concurrency safety (mutex + race tests) | 1 day | High (stability) |
| **4** | Macro caching + configurable rules | 2 days | Medium (perf + UX) |
| **5** | Betweenness inclusive + GodObject threshold fix | 0.5 day | Medium (correctness) |
| **6** | COW Map + lazy Clone + bounded goroutines | 3-4 days | Medium (scalability) |
