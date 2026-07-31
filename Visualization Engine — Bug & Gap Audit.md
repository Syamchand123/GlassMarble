# Visualization Engine — Bug & Gap Audit

> Analysis based on live run of `gmb.exe visualize class --save class_diagram` against a 19 MB `akg_state.ttl` containing **197 TypeDecl nodes** and **6,809 edges**. Output: `0 nodes, 0 edges`.

---

## BUG 1 — Silent Empty Output for Class/Object/Component/Sequence Diagrams (CRITICAL)

**File:** `stage1/extractor.go` · `extractionConfigs` map (line 185–215) and `extractWithConfig` (line 57–124)

**Root Cause:** Every diagram type that uses `EntryStrategyEntryPoint` silently returns an empty subgraph when no `--entry` flag is provided. The strategy only populates `startIDs` if `opts.EntryPointID != ""`. If it's empty, BFS starts from nothing.

**Affected diagrams:** `class`, `object`, `component`, `composite`, `c4code`, `c4dynamic`, `sequence`, `communication`, `interaction`, `activity`, `flowchart`, `callgraph`.  That is ~12 of 21 diagram types.

**Evidence:**
```
% Graph Summary: 0 nodes, 0 edges, density=0.0000
```
The TTL has 197 `gm:TypeDecl` nodes but `visualize class` is using `EntryStrategyEntryPoint` without any user-provided `--entry`, so `startIDs = nil` → BFS returns empty.

**Fix needed:** `UMLClass` should fall back to `EntryStrategyAll` (or `EntryStrategyAuto`) when `opts.EntryPointID == ""`. The existing `IncludeUnused: true` flag already signals this intent — but it's never checked inside `extractWithConfig`.

---

## BUG 2 — `isBaseEdge` Misclassifies Multi-value `a` Declarations (HIGH)

**File:** `stage1/extractor.go` · `isBaseEdge` (line 330–340)

**Root Cause:** The TTL file uses `a rdfs:Resource` as an extra type annotation (e.g. `a gm:TypeDecl ; ... ; a rdfs:Resource .`). When the AKG writer emits this pattern on a node block, `isBaseEdge` can be fooled into returning `true` because after trimming the block has exactly 3 space-separated fields (`<uri> a rdfs:Resource`) — causing a **node to be parsed as an edge**.

Also, `isBaseEdge` uses `strings.Contains(block, ";")` as its "is a node block" guard. This is correct for the current TTL format, but is fragile — any future edge with an embedded semicolon in an annotated literal would be misrouted.

---

## BUG 3 — `bindEdgeProperty` Warns on Every RDF-star Annotation (HIGH)

**File:** `stage1/extractor.go` · `bindEdgeProperty` (line 363–402)

**Root Cause:** RDF-star annotations (`<< src pred tgt >> gm:lineNumber N .`) are only useful if the base edge was already parsed and stored in `edgeMap`. But many `gm:calls` edges point to `ext:...` node IDs — external function stubs. These external stub **nodes** are never stored (they have no known `gm:TypeDecl` kind), but their **edges** must be parsed before the annotation is processed.

**Evidence from terminal output (hundreds of lines):**
```
WARN: RDF-star annotation for non-existent edge internal/akg/... 
```
The base edge is correctly parsed into `edgeMap`. But the RDF-star annotation block is being processed **before** the base edge in some orderings, or the edge key doesn't match because `ParseNodeURI` is stripping `<>` from the outer node but keeping `%20` percent-encoding, causing a key mismatch.

**Investigation finding:** `parseBaseEdge` stores the key as `ParseNodeURI(parts[0])|pred|ParseNodeURI(parts[2])`. `bindEdgeProperty` reconstructs the key from the `<<src pred tgt>>` inner triple. If the URIs in the annotation and in the base edge are serialized slightly differently (e.g., one is `<http://...>` and the other is a bare `ext:...` without angle brackets), the keys won't match → permanent "non-existent edge" warning.

---

## BUG 4 — `parseNodeBlock` Drops Nodes with Unknown Predicates via Properties Map, but Never Warns on Known-Bad `a` Values (MEDIUM)

**File:** `stage1/extractor.go` · `parseNodeBlock` (line 404–484)

**Root Cause:** TTL nodes have predicates like `gm:betweenness_centrality`, `gm:blast_radius`, `gm:instability`, `gm:macro_rules`, `gm:pagerank` — none of these are handled in the `switch pred` block. They all fall into the `default` branch, stored in `node.Properties`. That's fine. However, these are rich analytical properties the visualization engine **ignores completely** — it never reads `node.Properties` after parsing.

**Impact:** PageRank values, blast radius, instability scores are all present in the TTL but the visualizer's layout phase recomputes them from scratch (via `stage2.ComputeAllMetrics`), throwing away the pre-computed values from stage4.

---

## BUG 5 — `ExtractFromSubgraph` vs `ParseTTLFileToNative` Duality (MEDIUM)

**File:** `visualizer.go` and `stage1/extractor.go`

There are **two separate entrypoints** for running extraction:
- `stage1.ExtractSubgraph(ttlPath, t, opts)` — parses file, extracts inline (old API, used by older test code)
- `stage1.ParseTTLFileToNative(ttlPath)` → `stage1.ExtractFromSubgraph(full, cfg, opts)` — parse once, extract separately (current pipeline in `visualizer.go`)

`GetExtractionConfig` is exported but `getExtractionConfig` is also internal. `ExtractSubgraph` calls `getExtractionConfig` (unexported). `visualizer.go` calls `GetExtractionConfig` (exported). They're identical wrappers — confirmed dead duplication.

---

## BUG 6 — `UMLClass` `IncludeUnused: true` is Ignored by the Extractor (MEDIUM)

**File:** `stage1/extractor.go` · `extractionConfigs` (line 185)

`UMLClass` sets `IncludeUnused: true` in its `ExtractionConfig`, but `extractWithConfig` **never reads `cfg.IncludeUnused`**. The `opts.IncludeUnused` from the CLI `--unused` flag is read by `pruneDeadComponents` in stage2, but the extraction-level flag from the config is completely ignored. This means the `IncludeUnused: true` in the class diagram config is dead code.

---

## BUG 7 — `bothPassSubgraph` Only Includes Edges Where Both Nodes Match the Filter (MEDIUM)

**File:** `stage1/extractor.go` · `bothPassSubgraph` (line 165–182)

For `EdgeDirectionBoth` strategies (e.g., `UMLPackage`, `Mindmap`, `ERDiagram`), `bothPassSubgraph` first collects all nodes matching the `NodeKindFilter`, then only keeps edges where **both** endpoints are in the collected nodes. This discards all edges that cross into/from external nodes (`ext:...` nodes), even when those connections are meaningful for the diagram.

---

## BUG 8 — `ext:...` Nodes Are Never Parsed as Real Nodes (HIGH)

**File:** `stage1/extractor.go` · `parseNodeBlock` (line 404–484)

Every external function call goes to a stub node with ID like:
```
ext:( "fmt" "log" "sync" ... )::{: defer tm.saveToDisk(...)
```
These node IDs:
1. Contain encoded characters (`%20`, `%22`, etc. in the TTL)
2. Are the **targets of nearly all `gm:calls` edges** (the internal→external call graph)
3. Have **no valid `gm:TypeDecl`/`gm:Executable` kind** — they're stored as bare `a rdfs:Class` or not stored at all

This means the entire call graph in the TTL is **not usable by the visualizer** because the destination of every edge is an unknown node. BFS from any internal node reaches only `ext:...` nodes which have no kind match and are discarded.

---

## BUG 9 — `parseLiteral` Doesn't Handle Multi-line String Literals (LOW)

**File:** `stage1/extractor.go` · `parseLiteral` (line 513–523)

The TTL scanner joins multi-line blocks by line. But if a `gm:code` or `gm:content` literal contains an escaped newline (`\\n`), `parseLiteral` converts `\\n` → `\n`, which is correct. However it doesn't handle:
- Triple-quoted literals (`"""..."""`) — not standard Turtle but produced by some AKG writers
- Unmatched quotes in literals where the closing `"` appears on a different scanner line

---

## BUG 10 — `parseGraph` Cache Key Doesn't Include Diagram Type (MEDIUM)

**File:** `visualizer.go` · `parseGraph` (line 209–227)

```go
cacheKey := fmt.Sprintf("parse:%s", ec.ttlPath)
```

The cache stores the full **NativeGraph** (all nodes and all edges). That's correct — parsing is type-agnostic. But the cache TTL is 10 minutes, and the cache key doesn't include the mtime precisely — it uses `info.ModTime()` which is compared via `Equal()`. If the filesystem timestamp resolution is coarse (e.g., 1-second on FAT32/Windows), two different writes within the same second will serve stale data.

---

## BUG 11 — Rendering Switch: `Flowchart` and `UMLActivity` Share the Same Renderer (LOW)

**File:** `stage3/mermaid.go` · `RenderDiagram` (line 32–33)

```go
case types.UMLActivity, types.Flowchart:
    renderActivityDiagram(tree, &sb)
```

A flowchart and an activity diagram are semantically different Mermaid diagram types (`flowchart LR` vs `stateDiagram-v2`). They are rendered identically, which means `visualize flowchart` and `visualize activity` produce the same output.

---

## BUG 12 — `renderClassDiagram` Only Handles `gm:TypeDecl` as Classes, Drops `gm:Member` (MEDIUM)

**File:** `stage3/mermaid.go` · `renderClassDiagram` (line 84–)

```go
if node.Kind == "gm:TypeDecl" {
    classes[node.ID] = node
} else if node.Kind == "gm:Executable" {
    // methods
}
```

The extraction config includes `"gm:Member"` as a valid node kind for UMLClass. But the renderer has no branch for `gm:Member` — they are silently dropped, losing all struct field information.

---

## BUG 13 — No Node Deduplication in `bfsSubgraph` Edge Collection (LOW)

**File:** `stage1/extractor.go` · `bfsSubgraph` (line 526–556)

Edges are appended inside the BFS loop without checking for duplicates. If the same edge is reachable via multiple BFS paths, it's added multiple times to `sub.Edges`. `collapseEdges` in stage2 handles some of this, but duplicate detection at the source would be cleaner and more efficient.

---

## STRUCTURAL GAP — The Visualizer Ignores Stage4 Pre-computed Graph Metrics

The AKG stage4 linker computes and writes `gm:pagerank`, `gm:betweenness_centrality`, `gm:blast_radius`, and `gm:instability` directly into the TTL for every node. The visualization engine **re-derives all these metrics** from scratch in `stage2.ComputeAllMetrics`. This is:
1. Wasteful (the metrics are right there in the TTL)
2. Inconsistent (the visualizer's PageRank may differ from the AKG's stored values)
3. A missed opportunity — the AKG has richer metrics (blast_radius, instability) the visualizer doesn't compute at all

---

## Summary Table

| # | Severity | File | Issue |
|---|----------|------|-------|
| 1 | **CRITICAL** | `stage1/extractor.go` | `EntryStrategyEntryPoint` → empty output when `--entry` not given (12 diagram types broken) |
| 2 | HIGH | `stage1/extractor.go` | `isBaseEdge` heuristic can misclassify multi-type node blocks |
| 3 | HIGH | `stage1/extractor.go` | RDF-star edge key mismatch → 100+ bogus WARNs, line numbers lost |
| 4 | MEDIUM | `stage1/extractor.go` | Pre-computed AKG metrics ignored; not read from `node.Properties` |
| 5 | MEDIUM | `stage1/extractor.go` | Duplicate `ExtractSubgraph`/`ParseTTLFileToNative` API duality |
| 6 | MEDIUM | `stage1/extractor.go` | `cfg.IncludeUnused` field is dead — never read by extractor |
| 7 | MEDIUM | `stage1/extractor.go` | `bothPassSubgraph` drops all cross-boundary edges |
| 8 | HIGH | `stage1/extractor.go` | `ext:...` external stub nodes have no kind → entire call graph is unreachable |
| 9 | LOW | `stage1/extractor.go` | `parseLiteral` doesn't handle triple-quoted or multi-segment literals |
| 10 | MEDIUM | `visualizer.go` | Cache key ignores mtime resolution; coarse timestamps can serve stale parse results |
| 11 | LOW | `stage3/mermaid.go` | `Flowchart` and `UMLActivity` render identically |
| 12 | MEDIUM | `stage3/mermaid.go` | `gm:Member` nodes silently dropped in class diagram renderer |
| 13 | LOW | `stage1/extractor.go` | No edge deduplication in BFS; `collapseEdges` must compensate |
| — | STRUCTURAL | All stages | Visualizer ignores pre-computed TTL metrics; re-derives everything from scratch |
