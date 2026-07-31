# Master Plan: Code Analysis Engine Improvement

## Overview

This plan addresses 6 weak areas identified in the 4-stage code analysis pipeline.
Each section below breaks down the problem, root cause, concrete fix, and verification steps.

---

## 1. Bug Fix: `isControlFlowStatement` False Positives

**File:** `internal/code_analysis_engine/stage4/cfg_linker.go:42-108`

### Problem

`isControlFlowStatement()` (line 110-124) uses `strings.Contains` on a concatenated string of
`node.Type + " " + node.Kind + " " + node.Name`. This causes false positives:

| Substring | False matches on node.Name | Result |
|-----------|---------------------------|--------|
| `"if"` | `unifiedProcessor`, `specification`, `signature`, `ifdef`, `iface`, `significant` | Spurious IF_BRANCH node |
| `"for"` | `transformData`, `formatString`, `beforeAll`, `performance` | Spurious LOOP_BRANCH node |
| `"else"` | `elsewhere`, `elapsed` | Spurious IF_BRANCH |
| `"case"` | `lowercase`, `uppercase`, `staircase`, `caseInsensitive`, `caseSensitive` | Spurious SWITCH_BRANCH node |
| `"try"` | `trying`, `retry`, `tryLock`, `tryBuild` | Spurious EXCEPTIONAL_BRANCH |
| `"while"` | `worthwhile` | Spurious LOOP_BRANCH |

### Root Cause

Stage 2's normalizer (`normalizer.go:146-157`) already classifies control-flow tokens by setting
`node.Type = GASTControlFlow`. The CFG linker ignores this and re-implements detection with
a brittle string-contains heuristic that operates on the full concatenated identity
(Type + Kind + Name) instead of checking only the `Type` field.

### Fix

Replace the whole `isControlFlowStatement()` function and the string-based detection
with a check on `node.Type`. The `extractCFGNodesFromGAST` function should:

```go
// After building enclosingFuncID (line 47-52), replace:
//   lowerType := strings.ToLower(string(node.Type) + " " + node.Kind + " " + node.Name)
//   if enclosingFuncID != "" && isControlFlowStatement(lowerType) {
//
// With:
if enclosingFuncID != "" && node.Type == stage2.GASTControlFlow {
```

Then remove `isControlFlowStatement()` entirely (lines 110-124).

For sub-classification (IF_BRANCH vs LOOP_BRANCH vs SWITCH_BRANCH etc.), use
`node.Kind` (which carries the original language-specific AST kind like `"if_statement"`,
`"for_statement"`, `"return_statement"`) with a **single-pass exact switch**:

```go
switch node.Kind {
case "if_statement", "if", "else_clause", "conditional":
    edgeType = EdgeConditionalBranch; branchKind = "IF_BRANCH"
case "for_statement", "for", "for_each", "foreach", "for_in_statement", "for_of_statement",
     "while_statement", "while", "do_statement", "do", "loop":
    edgeType = EdgeLoopBranch; branchKind = "LOOP_BRANCH"
case "switch_statement", "switch", "switch_case", "case":
    edgeType = EdgeSwitchBranch; branchKind = "SWITCH_BRANCH"
case "try_statement", "try", "catch_clause", "catch", "finally_clause", "finally":
    edgeType = EdgeCatches; branchKind = "EXCEPTIONAL_BRANCH"
case "defer", "defer_statement":
    edgeType = EdgeDefers; branchKind = "EXCEPTIONAL_BRANCH"
case "return_statement", "return",
     "throw_statement", "throw", "raise",
     "panic", "recover", "go_statement", "go":
    edgeType = EdgeControlFlow; branchKind = "CFG_FLOW"
default:
    // Not a recognized control flow kind — skip
    continue // (restructure to avoid creating a branch node)
}
```

### Verification

- Run `go test ./internal/code_analysis_engine/stage4/` — all tests must pass
- Run full analysis on GlassMarble itself: node count for CFG branches should drop
  (no more spurious branches on `unified`, `transform`, `specification`-named nodes)
- Add a test case in `linker_test.go` that creates a node with Name= `"specification"`
  and verifies no CFG branch is created

---

## 2. Bug Fix: Dangling Virtual Edge Targets

**Files:**
- `internal/code_analysis_engine/stage4/semantic_linker.go`
- `internal/code_analysis_engine/stage4/dfg_linker.go`
- `internal/code_analysis_engine/stage4/concurrency_linker.go`
- `internal/code_analysis_engine/stage4/rpc_linker.go`
- `internal/code_analysis_engine/stage4/di_linker.go`

### Problem

Several linkers create edges to target IDs that have no corresponding `ResolvedNode` entry
in `GraphNodes`. The AKG's own Dangling Reference Audit (`transaction_manager.go:419-433`)
flags every single one of these as errors.

All occurrences:

| File | Line | Edge type | Virtual target ID | New node needed |
|------|------|-----------|-------------------|-----------------|
| `semantic_linker.go` | 59 | `EdgeSpawnsConcurrent` | `"thread_or_coroutine"` | Virtual CONCURRENCY node |
| `semantic_linker.go` | 65 | `EdgeDispatchesEvent` | `"EVENT::"+evt.EventName` | Virtual EVENT_TOPIC node |
| `semantic_linker.go` | 103 | `EdgeQueriesDB` | `"DATABASE::"+call.ReceiverName` | Virtual DATABASE node |
| `semantic_linker.go` | 106 | `EdgeCallsCloudAPI` | `"CLOUD_API::"+call.ReceiverName` | Virtual CLOUD_API node |
| `semantic_linker.go` | 127 | `EdgeExposesEndpoint` | `"endpoint:"+ep.Method+":"+ep.Route` | Virtual ENDPOINT node |
| `semantic_linker.go` | 134 | `EdgeSecuritySink` | `"sink:"+sink.SinkType` | Virtual SECURITY_SINK node |
| `semantic_linker.go` | 140 | `EdgeConsumesResource` | `"resource:"+res.ResourceType` | Virtual RESOURCE node |
| `semantic_linker.go` | 148 | `EdgeMutatesGlobal` | `"global:"+gs.Name` | Virtual GLOBAL_STATE node |
| `dfg_linker.go` | 101 | `EdgeDataFlow` | `"TAINT:DATABASE"` | Virtual TAINT_SOURCE node |
| `concurrency_linker.go` | 47 | `EdgeSendsTo` | *likely similar pattern* | (check) |
| `concurrency_linker.go` | 49 | `EdgeReceivesFrom` | *likely similar pattern* | (check) |
| `rpc_linker.go` | 65 | `EdgeNetworkCall` | *likely similar pattern* | (check) |
| `di_linker.go` | 71 | `EdgeInjects` | *likely similar pattern* | (check) |

### Root Cause

The linkers create informational/heuristic edges to logical targets that represent
categories (databases, cloud APIs, endpoints, sinks) rather than actual code symbols.
They never register the target as a node.

### Fix

Add a helper function `ensureVirtualNode(id, kind, name, cpg)` that creates a synthetic
`ResolvedNode` if it doesn't exist. Then call it before every virtual edge:

```go
func ensureVirtualNode(id, kind, name string, cpg *Stage4Output) {
    if _, exists := cpg.GraphNodes[id]; !exists {
        cpg.GraphNodes[id] = &ResolvedNode{
            ID:   id,
            Kind: kind,
            Name: name,
        }
    }
}
```

Fix each dangling edge:

1. **semantic_linker.go:59** — concurrency spawn
   ```go
   ensureVirtualNode("thread_or_coroutine", "VIRTUAL_RESOURCE", "Concurrent Execution", cpg)
   cpg.AddEdge(callerFQN, "thread_or_coroutine", EdgeSpawnsConcurrent, spawn.LineNumber)
   ```

2. **semantic_linker.go:65** — event hooks. This should be merged with the event linker
   (buffers[12]) which already creates EVENT_TOPIC nodes. Either:
   - Delegate event hook edge creation to the event linker (remove from semantic linker)
   - Or create the same node format:
   ```go
   topicID := "event:" + evt.EventName
   ensureVirtualNode(topicID, "EVENT_TOPIC", evt.EventName, cpg)
   cpg.AddEdge(callerFQN, topicID, EdgeDispatchesEvent, evt.LineNumber)
   ```

3. **semantic_linker.go:103** — DB queries
   ```go
   dbID := "DATABASE::" + call.ReceiverName
   ensureVirtualNode(dbID, "VIRTUAL_DATABASE", call.ReceiverName, cpg)
   cpg.AddEdge(callerID, dbID, EdgeQueriesDB, call.LineNumber)
   ```

4. **semantic_linker.go:106** — Cloud API calls
   ```go
   cloudID := "CLOUD_API::" + call.ReceiverName
   ensureVirtualNode(cloudID, "VIRTUAL_CLOUD_API", call.ReceiverName, cpg)
   cpg.AddEdge(callerID, cloudID, EdgeCallsCloudAPI, call.LineNumber)
   ```

5. **semantic_linker.go:127** — Endpoints
   ```go
   epID := "endpoint:" + ep.Method + ":" + ep.Route
   ensureVirtualNode(epID, "VIRTUAL_ENDPOINT", ep.Method+":"+ep.Route, cpg)
   cpg.AddEdge(callerFQN, epID, EdgeExposesEndpoint, ep.LineNumber)
   ```

6. **semantic_linker.go:134** — Security sinks
   ```go
   sinkID := "sink:" + sink.SinkType
   ensureVirtualNode(sinkID, "VIRTUAL_SECURITY_SINK", sink.SinkType, cpg)
   cpg.AddEdge(callerFQN, sinkID, EdgeSecuritySink, sink.LineNumber)
   ```

7. **semantic_linker.go:140** — Resource links
   ```go
   resID := "resource:" + res.ResourceType
   ensureVirtualNode(resID, "VIRTUAL_RESOURCE", res.ResourceType, cpg)
   cpg.AddEdge(callerFQN, resID, EdgeConsumesResource, res.LineNumber)
   ```

8. **semantic_linker.go:148** — Global state
   ```go
   globalID := "global:" + gs.Name
   ensureVirtualNode(globalID, "VIRTUAL_GLOBAL_STATE", gs.Name, cpg)
   cpg.AddEdge(callerFQN, globalID, EdgeMutatesGlobal, 0)
   ```

9. **dfg_linker.go:101** — Taint database
   ```go
   taintID := "TAINT:DATABASE"
   ensureVirtualNode(taintID, "VIRTUAL_TAINT_SOURCE", "Database Taint Source", cpg)
   cpg.AddEdge(taintID, funcID, EdgeDataFlow, int(node.StartLine))
   ```

10. **concurrency_linker.go, rpc_linker.go, di_linker.go** — Audit for same pattern

### Verification

- Run `go test ./internal/code_analysis_engine/stage4/` — all tests pass
- Run full analysis on GlassMarble: `.\gmb.exe analyze --verbose`
- AKG should report **zero dangling reference errors** (previously the Errors field
  in `transaction_manager.go:419-433` would have been populated)
- Add a test in `linker_test.go` that inspects each virtual node's existence

---

## 3. No Selectivity — Linker Pass Configuration

**File:** `internal/code_analysis_engine/stage4/linker.go:12`

### Problem

All 15 linker passes always execute unconditionally. For an architecture-level analysis,
passes like CFG, DFG, FFI, Escape Analysis, Event Sourcing, DI Injection, and Constraint
linking produce irrelevant noise. For a 100K-file repo, this would be fatal.

### Root Cause

The `Link()` function signature has no configuration:
```go
func Link(stage3Out *stage3.Stage3Output, modifiedFiles []string, db GraphDB) (*Stage4Output, error)
```

### Fix

#### Step 1: Add `LinkerConfig` type to `type.go`

```go
// LinkerConfig controls which linker passes execute and at what granularity.
type LinkerConfig struct {
    // DisabledPasses lists linker passes to skip entirely.
    // Pass names match the buffer indices used in linker.go:
    //   "type", "interface", "cfg", "dfg", "callgraph", "concurrency",
    //   "filedeps", "semantics", "rpc", "constraints", "ffi",
    //   "eventsourcing", "di", "escape", "alias"
    DisabledPasses []string `json:"disabled_passes,omitempty"`

    // LevelOfDetail controls CFG/DFG granularity:
    //   "architecture" — skip CFG and DFG entirely, only module/type/call/dep edges
    //   "standard"     — aggregate CFG per function (count branches, no per-branch nodes)
    //   "full"         — current behavior: per-branch CFG nodes, per-variable DFG nodes
    LevelOfDetail string `json:"level_of_detail,omitempty"`

    // MaxNodesPerFile limits CFG/DFG synthetic nodes per file (0 = unlimited)
    MaxNodesPerFile int `json:"max_nodes_per_file,omitempty"`
}
```

#### Step 2: Update `Link()` signature

```go
func Link(stage3Out *stage3.Stage3Output, modifiedFiles []string, db GraphDB, config ...LinkerConfig) (*Stage4Output, error)
```

#### Step 3: Skip disabled passes in `Link()`

Replace hardcoded goroutine launches with a dispatch table:

```go
type passDef struct {
    name     string
    buffer   int
    fn       func(*stage3.Stage3Output, *Stage4Output)
}

passes := []passDef{
    {"type",         0, LinkTypesAndComposition},
    {"interface",    1, LinkInterfacesAndRealizations},
    {"cfg",          2, LinkIntraProceduralControlFlow},
    {"dfg",          3, LinkDataFlowGraph},
    {"callgraph",    4, LinkCallGraph},
    {"concurrency",  5, LinkConcurrencyAndAsyncControlFlow},
    {"filedeps",     6, LinkFileDependencies},
    {"semantics",    7, LinkEnterpriseSemantics},
    {"rpc",          9, LinkCrossLanguageRPC},
    {"constraints", 10, LinkConstraints},
    {"ffi",         11, LinkFFI},
    {"eventsourcing",12, LinkEventSourcing},
    {"di",          13, LinkDependencyInjection},
    {"escape",      14, LinkEscapeAnalysis},
    {"alias",       15, LinkAliasAnalysis},
}
```

Then filter by `config.DisabledPasses` and only launch enabled passes.

#### Step 4: Adjust worker count and buffer allocation

- `wg.Add()` = number of enabled passes instead of hardcoded 15
- Allocate only the needed number of buffers

#### Step 5: Wire `LinkerConfig` through `cmd/analyze.go`

Add a CLI flag `--linker-config` or `--link-level` with values `architecture`, `standard`, `full`.

### Verification

- Run with `--link-level architecture`: verify CFG_BRANCH and DFG_VAR nodes are absent
- Run with `--link-level full`: verify node count matches current baseline
- All `go test ./...` pass
- Test that disabled passes produce no edges of their type

---

## 4. CFG/DFG Granularity Reduction

**Files:**
- `internal/code_analysis_engine/stage4/cfg_linker.go`
- `internal/code_analysis_engine/stage4/dfg_linker.go`

### Problem

The CFG linker creates a separate `ResolvedNode` per control-flow construct:
every `if`, `for`, `switch`, `try`, `defer`, `throw`, `return`, `panic` in every
function gets its own node. For a Go function with 10 `if err != nil` checks,
that's 10 IF_BRANCH nodes. These constitute ~40-50% of the total 6340 nodes.

Similarly, the DFG linker creates a `DFG_VAR` node per variable/parameter per function.
A function with 5 parameters + 8 local variables = 13 DFG_VAR nodes.

### Root Cause

Both linkers operate at maximum granularity — one CPG node per source-level construct —
with no aggregation or summarization.

### Fix

Implement `LevelOfDetail` in both linkers:

#### CFG Linker Changes (`cfg_linker.go`)

- **`architecture` mode** (`LevelOfDetail == "architecture"`):
  Skip this pass entirely (already handled by the linker config in item 3)

- **`standard` mode** (`LevelOfDetail == "standard"`):
  Aggregate control flow per function. Instead of one node per branch, create a
  single `CFG_SUMMARY` node per function that records metadata:
  ```go
  // One summary node per function instead of N branch nodes:
  summaryID := enclosingFuncID + "::CFG_SUMMARY"
  // No per-branch nodes. Just edges for: function has_control_flow
  // Store branch counts in node Properties:
  properties: {
      "if_count":    "10",
      "loop_count":  "2",
      "switch_count":"0",
      "try_count":   "1",
      "throw_count": "0",
  }
  ```

- **`full` mode** (`LevelOfDetail == "full"`, or default):
  Current per-branch behavior (with the bug fix from item 1 applied)

#### DFG Linker Changes (`dfg_linker.go`)

- **`architecture` mode**: Skip this pass
- **`standard` mode**: Aggregate DFG per function. One `DFG_SUMMARY` node per function
  listing parameter and variable names in properties, without individual `DFG_VAR` nodes.
- **`full` mode**: Current per-variable behavior

#### New Type: Level constants

In `type.go`:
```go
const (
    LevelArchitecture = "architecture"
    LevelStandard     = "standard"
    LevelFull         = "full"
)
```

### Verification

- `Level=architecture`: CFG_BRANCH and DFG_VAR node count should be 0
- `Level=standard`: total nodes should drop by ~40-50% vs `Level=full`
- `Level=full`: node/edge counts should match current baseline
- Test all 3 levels in `linker_test.go`

---

## 5. Enterprise Macro Inference Improvements

**File:** `internal/akg/reasoner.go:260-428`

### Problem

33+ macro inference rules are entirely name-based heuristics with no structural
verification or confidence metadata:

```go
// Rule 7 (line 310): If name contains "controller" → "Inbound API Controller"
// Rule 5 (line 300): If name contains "repository" + has DATABASE primitive → "Repository Pattern"
// Rule 14 (line 345): If name contains "auth" → "Authentication Middleware"
```

These are speculative. A variable named `controller` or a folder named `repositories`
gets the same label as an actual web controller or data repository.

Additionally, applying all 33 rules to every MODULE/FILE/STRUCT/CLASS/FUNCTION node
(which in the current run means ~600 nodes × 33 rules = ~20K heuristic checks) is
wasteful.

### Root Cause

The inference rules were designed as broad heuristic sweeps. They lack:
- Confidence/evidence tiering
- Structural validation (e.g., does it actually implement an interface?)
- Per-rule enable/disable
- De-duplication (line 85: same rule added twice for articulation points)

### Fix

#### Step 1: Add confidence tiers to macro rules

Tag each rule with a tier:

```go
const (
    RuleTierHeuristic     = "heuristic"      // name-based, no structural evidence
    RuleTierStructural    = "structural"     // backed by graph edges/primitives
    RuleTierArchitectural = "architectural"  // derived from graph topology (PageRank, cycles, etc.)
)
```

- **Structural rules** (those requiring primitive evidence like `primitivesFound["DATABASE"]`
  or `hasSecurityGate`) → keep as-is, they already require structural evidence
- **Pure name-based rules** (rules 5-20 that only check `strings.Contains(lowerName, ...)`) →
  tag as `[heuristic]` prefix in the output string
- **Graph-topology rules** (rules 29-37: cycle detection, PageRank, betweenness, god objects) →
  tag as `[architectural]`

Example output:
```
Component UsersController serves as an Inbound API Controller [heuristic]
Component UserRepository implements the Repository Data Access Pattern [heuristic]
Component AuthMiddleware enforces Security Validation before Storage Persistence [structural]
Component db_handler is part of a Circular Dependency (Cycle Size: 3) [architectural]
```

#### Step 2: Add rule-level configuration to `LinkerConfig`

```go
type LinkerConfig struct {
    // ... existing fields ...
    
    // MacroInference controls AKG macro inference behavior:
    //   "disabled" — skip macro inference entirely
    //   "structural" — only run structurally-verified rules (require graph evidence)
    //   "all" — run all rules including name-based heuristics (default)
    MacroInference string `json:"macro_inference,omitempty"`
}
```

#### Step 3: Skip non-structural rules when `MacroInference == "structural"`

Wrap each pure-name-based rule:
```go
// Rule 5: Repository Pattern
if (strings.Contains(lowerName, "repository") || ...) && primitivesFound["DATABASE"] {
    rule := fmt.Sprintf("Component %s implements the Repository Data Access Pattern [heuristic]", node.Name)
    // Only add if inference mode includes heuristics
    if config.MacroInference != "structural" {
        inferredRules = append(inferredRules, rule)
    }
}
```

#### Step 4: Fix the duplicate rule insertion bug

Line 85 and 90 in `reasoner.go` both append the same rule:
```go
graph.MacroRules[id] = append(graph.MacroRules[id], rule)  // line 85
// ...
graph.MacroRules[id] = append(graph.MacroRules[id], rule)  // line 90
```
Remove line 90.

### Verification

- `MacroInference=disabled`: no nodes get `macro_rules` property
- `MacroInference=structural`: only rules backed by structural evidence fire
- `MacroInference=all`: all rules fire (current behavior, but with confidence tags)
- No duplicate rules in `MacroRules` (fix line 90)
- All `go test ./internal/akg/...` pass

---

## 6. Scaling Readiness

### Problem

The engine produces ~63 nodes and ~107 edges per file on average. For a 100K-file
enterprise repo, this projects to ~6.3M nodes and ~10.7M edges. The current design
has no partitioning, sampling, or aggregation strategies.

### Fixes (incremental, not a full redesign)

#### Step 1: Add `LevelOfDetail` thresholds (already covered in items 3-4)

- Architecture mode: ~20 nodes/file (module + file + type/function + call + dep edges)
- Standard mode: ~35 nodes/file (above + CFG summary + DFG summary)
- Full mode: ~63 nodes/file (current behavior)

For large repos, architecture mode is the only viable option.

#### Step 2: Add `MaxGraphSize` to `LinkerConfig`

```go
type LinkerConfig struct {
    // ... existing fields ...
    
    // MaxTotalNodes limits total nodes in the CPG. If exceeded, analysis
    // prints a warning and proceeds with what was built (degraded mode).
    // 0 = unlimited.
    MaxTotalNodes int `json:"max_total_nodes,omitempty"` // e.g., 500000 for 500K nodes
    
    // AbortOnLimit when true causes Link() to return an error instead
    // of proceeding with a degraded graph.
    AbortOnLimit bool `json:"abort_on_limit,omitempty"`
}
```

In `Link()`, after buffer merge, check the count:
```go
if cfg.MaxTotalNodes > 0 && len(cpg.GraphNodes) > cfg.MaxTotalNodes {
    if cfg.AbortOnLimit {
        return nil, fmt.Errorf("CPG exceeded max nodes: %d > %d", len(cpg.GraphNodes), cfg.MaxTotalNodes)
    }
    log.Printf("WARNING: CPG exceeded max nodes: %d > %d. Consider --link-level=architecture", len(cpg.GraphNodes), cfg.MaxTotalNodes)
}
```

#### Step 3: Add per-file node budget to CFG/DFG linkers

```go
// In cfg_linker.go and dfg_linker.go, before creating synthetic nodes:
fileNodeCount[relPath]++
if cfg.MaxNodesPerFile > 0 && fileNodeCount[relPath] > cfg.MaxNodesPerFile {
    continue  // skip creating more synthetic nodes for this file
}
```

#### Step 4: Optimize memory

- The 16 buffer copies multiply GraphNodes usage by 16× (line 27-30 of linker.go).
  For 6340 base nodes, that's 6340×16 = ~101K map entries per analysis.
- Fix: Use `sync.Map` or a copy-on-write pattern instead of N full map copies.
  Actually, the current design copies only map references (values are shared pointers),
  so each copy is just a map with ~6340 entries and shared values. This is OK for memory
  (~500KB per copy × 16 = ~8MB), but for large repos this grows.
- For large repos, consider eliminating buffers entirely and using per-pass mutexes
  or a node-creation lock. With the new wg.Wait() before merge, this is feasible:
  all linkers can share a single GraphNodes map with a sync.Mutex.

### Verification

- Test with `--link-level architecture` on a large directory: should complete quickly
- Test `MaxTotalNodes=100` with `AbortOnLimit=true`: should return error
- Test `MaxNodesPerFile=5`: each file should have at most 5 CFG+DFG synthetic nodes
- Memory profiling: verify buffer copies don't dominate

---

## Implementation Priority Order

| Priority | Item | Effort | Impact | Risk |
|----------|------|--------|--------|------|
| **P0** | Bug 1: `isControlFlowStatement` | 1 hour | High (false positives) | Low |
| **P0** | Bug 2: Dangling virtual nodes | 2 hours | High (AKG errors) | Low |
| **P1** | Item 3: Linker config/selectivity | 4 hours | High (enables large repos) | Medium |
| **P1** | Item 4: CFG/DFG granularity | 3 hours | High (reduces noise 40-50%) | Medium |
| **P2** | Item 5: Macro inference tiers | 3 hours | Medium (label quality) | Low |
| **P3** | Item 6: Scaling thresholds | 2 hours | Medium (safety net) | Low |

---

## Summary of All Files That Need Changes

| File | Change | Priority |
|------|--------|----------|
| `stage4/cfg_linker.go` | Replace `isControlFlowStatement` with `GASTControlFlow` check; add LevelOfDetail | P0 |
| `stage4/semantic_linker.go` | Add `ensureVirtualNode()` before all virtual edge targets | P0 |
| `stage4/dfg_linker.go` | Add `ensureVirtualNode("TAINT:DATABASE")` | P0 |
| `stage4/concurrency_linker.go` | Audit for dangling edge targets | P0 |
| `stage4/rpc_linker.go` | Audit for dangling edge targets | P0 |
| `stage4/di_linker.go` | Audit for dangling edge targets | P0 |
| `stage4/type.go` | Add `LinkerConfig` struct, `ensureVirtualNode()` helper | P1 |
| `stage4/linker.go` | Use `LinkerConfig` to skip passes; dynamic wg.Add; per-file node budget | P1 |
| `cmd/analyze.go` | Wire `--link-level` flag | P1 |
| `internal/akg/reasoner.go` | Add confidence tiers, rule-level config, fix duplicate rule | P2 |
| Any stage4 linker test files | Add test cases for each fix | P0/P1/P2 |
