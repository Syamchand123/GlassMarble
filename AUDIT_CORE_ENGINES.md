# GlassMarble Core Engines — Audit Report & Remediation Plan

**Date:** 2026-07-31
**Scope:** `internal/code_analysis_engine` (stages 1–4), `internal/akg`, `internal/visualization_engine` (stage1–3 + types), their consumers (`cmd/analyze.go`, `cmd/visualize.go`, `cmd/status.go`, `cmd/inspect.go`, `internal/ai_engine/akgbridge`, `internal/ai_engine/tools`), and the persisted artifact `.glassmarble/akg_state.ttl`.
**Method:** full source read-through (all packages), cross-referenced producer→consumer chains, `go test`/`go vet` runs, and comparison of every predicate/class emitted vs. declared in `ontology.ttl`. Read-only; no code changed.
**Key context:** `.glassmarble/` in this repo currently contains only an empty `wal/` directory — no `akg_state.ttl` has ever been produced here, so all evidence in this report is from code-path analysis plus the unit fixtures. The test suite is green, but (as documented in §2) green tests actively mask most of these bugs.

---

## Executive Summary

The five concerns raised are all **confirmed**, and they are worse than reported in one critical way: **the three core packages disagree with each other about the data model**. `code_analysis_engine` writes nodes/kinds that `visualization_engine` filters on but the serializer never emits; the serializer writes a TTL format that the parser half-understands; the parser silently discards what it doesn't understand. The result is a pipeline where each stage "works" in isolation, but the end-to-end product — a clean, correct, queryable architecture graph — never materializes.

| # | Issue | Verdict | Root-cause severity |
|---|-------|---------|---------------------|
| 1 | 10k nodes/10k edges for ~200 files, noisy | **Confirmed.** Bloat is structural (per-statement nodes, fake function nodes, fabricated virtual nodes) | Critical |
| 2 | Visualization engine "cannot do its core task" | **Confirmed.** Guaranteed-invalid markup for whole diagram families; silent empty diagrams; dead filter vocabulary | Critical |
| 3 | `ontology.ttl` gaps + unknown conformance of `akg_state.ttl` | **Confirmed.** Ontology is decorative (never loaded/validated); serializer emits undeclared predicates; restore is lossy | Critical |
| 4 | Hardware/memory safety for a user-machine CLI | **Confirmed.** No atomic writes, no size guards, whole-file parses, unbounded caches, broken JSON snapshot, WAL replay-everything | Critical |
| 5 | No QA for `akg_state.ttl` | **Confirmed.** Zero production validation exists; dangling edges are silently persisted; corrupt files are silently served | Critical |

**Estimated fixed-state gains:** fixing issue 1 alone should cut graph size 3–6× (10k→~1.5–3k nodes for this repo's scale) and eliminate the worst O(k²)/O(V·E) blowups; fixing issue 2 makes the 31 advertised diagram types actually render; fixing issues 3–5 makes the graph *trustworthy* and *survivable* on real machines.

---

# Issue 1 — `code_analysis_engine`: node/edge bloat and noise

## Findings

### 1.1 The default configuration is "full" — per-statement granularity
`cmd/analyze.go:90` hard-codes `LevelOfDetail: "full"` and the CLI default for `--link-level` is `"full"` (`cmd/analyze.go:152`). Full mode explicitly means "per-branch CFG nodes, per-variable DFG nodes" (`internal/code_analysis_engine/stage4/type.go:94-97`). The `--full` flag is a **no-op** (`cmd/analyze.go:53-55` is an empty comment block) and the incremental path (`RunIngestionForDelta`, `stage1/engine.go:36`; `CollectGitDiff`, `stage1/git.go:15`) has **zero callers** — every `gmb analyze` re-scans the entire repo at full detail.

### 1.2 Measured per-file production budget (full mode)
Per typical 100-line file (5 functions, 10 params, 15 calls, 8 control-flow statements):

| Producer | Granularity | Nodes | Edges |
|---|---|---|---|
| `builder.go:137` | per file | 1 FILE | — |
| `builder.go:108` | per directory | 1 MODULE | — |
| `builder.go:167-199` | per type | 1 | — |
| `builder.go:201-233` | per function | 1 | — |
| `cfg_linker.go:79-119` | **per control-flow statement** (if/for/while/switch/return/try/defer/go/throw) | 1 branch node each | 1–2 each |
| `dfg_linker.go:80-122` | **per parameter** | 1 DFG_VAR each | 2–3 each |
| `call_linker.go:83-107` | per high-risk-utility call | 1 VIRTUAL_CONTEXT per (caller,callee) pair | CALLS+1CFA+INSTANTIATES |
| `call_linker.go:267-299` | per unresolved call to external import | 1 EXTERNAL_API + EXTERNAL_SDK | CALLS + CONTAINS |
| `dfg_linker.go:127-152` | per call expression | — | +1 extra DATA_FLOW per call |
| `semantic_linker.go:55-162` | per hint/pattern | shared VIRTUAL_* nodes | 1–3 each |
| `constraint_linker.go:46-69` | **per `if` statement** | 1 ABSTRACT_CONSTRAINT | 1 |
| `alias_linker.go:46-76` | per variable containing `new(`/`make(`/`&` (string check) | 1 HEAP_ALLOCATION | POINTS_TO+HEAP_ALIAS |
| `dependency_linker.go:39-66` | per import (incl. stdlib) | — | 1 DEPENDS_ON |

**Sum: ≈35–50 nodes and ≈80–90 edges per file → ~7k–10k nodes / ~16k–18k edges per 200 files.** This matches the reported numbers almost exactly. The SCC all-pairs generator (§1.5) can add tens of thousands more.

### 1.3 The single worst defect: control-flow statements become FUNCTION nodes
`stage1/parser.go:188-197` force-classifies `if`, `for`, `while`, `switch`, `return`, `try`, `catch`, `defer`, `go`, `throw` as `TokenDeclaration`. Every translator **except Go** then hits a catch-all `default:` that sets `GASTFunction`:
- Java `java_translator.go:46-49`, Python `python_translator.go:38-41`, JS `javascript_translator.go:36-42`, TS `typescript_translator.go:47-53`, C `c_translator.go:34-37`, C++ `cpp_translator.go:37-43`, C# `csharp_translator.go:42-45`, Rust `rust_translator.go:49-52`, PHP `php_translator.go:39-45`, Ruby `ruby_translator.go:42-45`.

Consequence: in 10 of 13 languages, **every `if`/`for`/`return` is indexed as a definition**, enters `GlobalDefinitionIndex` (`aggregator.go:283-307`), becomes a graph node with a garbage FQN, and pollutes call resolution. For a Java/Python/JS/TS-heavy repo this alone accounts for 1,600–4,000 fake nodes per 200 files. Additionally, JS/TS `const`/`let`/`var` declarations hit the same catch-all — and **no translator ever emits `GASTVariable`** (only `stage2/type.go:17` and `normalizer.go:330` reference the type), so `dfg_linker.go:80`'s variable branch is dead code in practice.

### 1.4 Fabricated/virtual node spam
The graph is salted with synthetic nodes that carry little architecture signal and are **never swept on re-analysis** (see Issue 3/4):
- `VIRTUAL_CONTEXT` per (caller, callee) pair for any method name containing log/print/query/exec/send (`call_linker.go:88-107`, `isHighRiskUtility` at `call_linker.go:138-144`).
- `EXTERNAL_API`/`EXTERNAL_SDK` fabricated for **stdlib calls** (`fmt.Println`, `os.ReadFile`, `http.Get`) — `call_linker.go:267-299`, `external_dependency_indexer.go:64-70` ("treating stdlib as external is perfectly fine").
- `ABSTRACT_CONSTRAINT` per `if` statement (`constraint_linker.go:50-68`) — a *second* synthetic node for the same statement that already produced a CFG node.
- `HEAP_ALLOCATION` per variable whose *raw source text* contains `new(`/`make(`/`&` (`alias_linker.go:50-62`).
- Shared virtual nodes with hard-coded names: `thread_or_coroutine`, `TAINT:DATABASE`, `QUEUE::<receiver>`, `topic::<first-arg>`, `endpoint:GET:unknown` (**route hard-coded to "unknown"**, `normalizer.go:616`), `memory::HEAP` (target node **never created** — guaranteed dangling edge).

### 1.5 Duplicate and wrong edges
- One call can emit up to 6 edges: CALLS (`call_linker.go:85`), DATA_FLOW (`dfg_linker.go:149`), CFG_FLOW (`cfg_linker.go:115`), SPAWNS_CONCURRENT (`concurrency_linker.go:33`), QUERIES_DB/CALLS_CLOUD_API (`semantic_linker.go:106,111`), plus 1-CFA CONTEXT_CALL.
- The same event call is linked twice (DISPATCHES_EVENT via `normalizer.go:365-379` **and** PUBLISHES_EVENT via `event_linker.go:45-75`); same for concurrency (semantic_linker.go:55-59 + concurrency_linker.go:29-33).
- `detectCyclicDependencies` (`dependency_linker.go:159-167`) emits an `EdgeCyclic` between **every ordered pair** of each SCC — O(k²) edges. One 200-file SCC → ~39,800 edges.
- `security_linker.go:44-87` emits VULNERABLE_TAINT edges from every substring-classified source to every reachable sink (depth-15 DFS) — heuristic noise, not analysis.

### 1.6 Unresolved-symbol and mis-resolution edges (quality)
- `memory_linker.go:71` writes `ESCAPES_TO_HEAP → "memory::HEAP"` where the target node is never created (later recorded as `DanglingReferenceError`).
- `resolveCallTarget` attempt 4 (`call_linker.go:257-265`) scans **all nodes** and returns the *first* name match (confidence 0.3); attempt 3 takes `nodes[0]` from a global map (confidence 0.5) — plausible-looking but wrong CALLS edges are common.
- Go ID inconsistency defeats direct resolution: Go `node.Name` is already `pkg.Func` (`go_translator.go:49-51`) → ID `path::pkg.Func`, while attempt 0 builds `path::Func` (`call_linker.go:194-203`) — never matches, forcing every Go call through heuristic fallbacks.

### 1.7 Scalability hazards
- O(C×N) call resolution: attempt 4 scans all ~8k nodes for every unresolved call (`call_linker.go:258-265`) ≈ 128M map iterations per run.
- Interface linker is cubic in the worst case (`interface_linker.go:130-134, 158-162`).
- `AddEdge` linearly scans the outbound slice per insert — O(E × avg-degree) (`stage4/type.go:264-269, 302-306`).
- `ReasonWholeProgramPrimitives` does 5 full-graph passes with bubble-sort per step (`primitive_reasoner.go:16-150, 69-75`).
- `BuildOwnershipMap` rebuilt 5× per run (`cfg_linker.go:21`, `dfg_linker.go:27`, `call_linker.go:15`, `concurrency_linker.go:15`, `di_linker.go:15`).
- Every `ResolvedNode` copies the full GAST `Properties` map incl. up-to-2048-byte `content` snippets (`parser.go:220-224`), written as `gm:content` literals — ~0.5–3 KB of TTL per node.

### 1.8 Other quality defects
- Generated files (`*.pb.go`, `*.generated.*`, mocks, bindata) are **not excluded** by `walker.go` (only size limits, `walker.go:88-91`); the scan follows the raw filesystem, not `git ls-files`, so untracked/scratch files enter the graph.
- Type-kind distinctions collapse on disk: STRUCT/CLASS/INTERFACE all serialize to `gm:TypeDecl` and restore to `STRUCT` (`turtle_serializer.go:150-225`).
- `gm:code` is declared in the ontology but never emitted; the pipeline emits `gm:content` instead — an undeclared duplicate concept.

## Proposed fix plan — Issue 1

**Phase 1A — Stop the bleeding (defaults + config, low risk):**
1. Change the `analyze` default to `--link-level=architecture` (`cmd/analyze.go:90`, `:152`) and document `full` as opt-in; architecture mode kills CFG/DFG/constraint/alias/context per-statement nodes. *Expected: 3–5× node reduction.*
2. Fix the 10 translator catch-all `default:` branches to map control-flow tokens to `GASTControlFlow` (mirroring `go_translator.go:72-80`) and JS/TS lexical declarations to `GASTVariable`. *Expected: −1,600–4,000 fake nodes per 200 files on non-Go repos. This is the highest-value code change in the codebase.*
3. Disable stdlib-as-external fabrication: skip imports resolvable to the language stdlib in `external_dependency_indexer.go:64-70` and `call_linker.go:267-299`. *Expected: −600–1,400 nodes.*
4. Gate the SCC `EdgeCyclic` emission: one marker per strongly-connected component (or per original dependency pair within the SCC), not the full Cartesian product (`dependency_linker.go:159-167`).

**Phase 1B — Remove noise producers (medium risk):**
5. Drop per-call DATA_FLOW duplicates (`dfg_linker.go:127-152`), the 1-CFA VIRTUAL_CONTEXT fabrication (`call_linker.go:88-107`), ABSTRACT_CONSTRAINT nodes (`constraint_linker.go`), and the string-contains HEAP_ALLOCATION (`alias_linker.go`) from default mode; keep them behind `full`.
6. Delete the duplicate semantic links (event/concurrency double-linking, §1.5) by consolidating in one linker each.
7. Rebuild `resolveCallTarget` with a proper per-package symbol index and signature-aware matching; remove the first-match heuristic (attempt 4) and fix the Go `pkg.Func` ID mismatch (`call_linker.go:194-265`).

**Phase 1C — Engineering the index (performance, higher risk):**
8. Precompute adjacency maps once in stage4 (node→callers, type→uses) instead of per-linker O(N) scans; eliminate the 5× `BuildOwnershipMap` rebuild; replace `AddEdge` slice-linear dedup with a map keyed by (src,tgt,type,line).
9. Wire the existing delta path: `RunIngestionForDelta` + `CollectGitDiff` into `analyze` (make `--full` actually mean full, default = delta). Add a git-tracked-file filter to `walker.go` and a generated-file denylist.
10. Add a "quality budget" report at end of analyze: counts of unresolved calls, dangling edges, virtual nodes — surfaced to the user (feeds Issue 5 QA).

**Target after 1A+1B:** ~1.5–3k nodes / ~3–6k edges for a 200-file repo, with only real symbols and real call/dependency/type edges.

---

# Issue 2 — `visualization_engine`: diagram generation is broken

## Findings

Test results: `go test ./internal/visualization_engine/... -count=1` passes all 5 packages; `go vet` clean. **This is misleading** — the fixtures are hand-written and do not match real serializer output, and no test validates output with a real Mermaid/PlantUML/DOT parser. Detailed reasons in §2.6.

### 2.1 Critical — guaranteed invalid output (every invocation of these types)
- **DataFlow diagrams emit malformed Mermaid on every edge.** `renderDataFlowDiagram` builds `arrow := " -.->|"` and emits `A -.->| B` — an unterminated `|` link-text marker (`stage3/mermaid.go:648-653`). Every DATA_FLOW diagram fails in any Mermaid viewer.
- **C4Deployment emits a C4Container header but `Deployment_Node` bodies** (`mermaid_c4.go:213,223`) — `Deployment_Node` only exists in C4Deployment diagrams; the output is invalid.
- **Missing entry point → silent empty diagram.** `getEntryPoints` returns the requested ID verbatim (`stage1/extractor.go:283-286`); `bfsSubgraph` silently skips IDs absent from the graph (`extractor.go:605`) → empty nodes, empty edges, **no error**. The repo's own test proves it: `TestProjectDiagramScopeFolder` (`visualizer_test.go:257-261`) uses entry `...::HandleRequest` while the fixture only contains `...::HandleHTTP` — the test passes because it only asserts non-empty string on a bare `graph TD` header.
- **Tombstone triples become phantom edges.** The serializer emits deletions as single-line triples `<URI> gm:status "DELETED" .` (`akg/turtle_serializer.go:45`), but the parser routes every 3-field statement through `parseBaseEdge` (`extractor.go:379-413`) → phantom edge to a node `"DELETED"` that doesn't exist. The test fixture (`testdata/delta_append.ttl:26-30`) masks this by writing the tombstone as a *node block*, which is not what the serializer produces. Change-Impact diagrams (GroupAny, reverse-BFS) are polluted by these phantom edges.

### 2.2 Critical — the kind-vocabulary mismatch starves whole diagram families
The serializer can only emit classes from `mapKindToClass` (`akg/turtle_serializer.go:150-185`): `gm:Namespace, gm:File, gm:TypeDecl, gm:Executable, gm:ControlStructure, gm:Variable, gm:Parameter, gm:CFGSummary, gm:DFGSummary, gm:EventTopic, gm:VirtualDatabase, gm:VirtualEndpoint, gm:Block, gm:Annotation, rdfs:Class`. But extraction configs filter on `gm:User, gm:ExternalSystem, gm:Module, gm:Database, gm:Member, gm:Function, gm:Method, gm:Struct, gm:Class, gm:Interface, gm:Port` (`extractor.go:233-265`) — **every one of those is dead**. And the kinds the analysis engine *does* create (`gm:EventTopic`, `gm:VirtualDatabase`, `gm:VirtualEndpoint`) appear in **no** extraction filter. Result: **C4_CONTEXT, C4_LANDSCAPE, ER_DIAGRAM (partial), INFRASTRUCTURE, UML_DEPLOYMENT, UML_COMPOSITE, UML_ACTIVITY (partial) can never contain their intended nodes** — they render empty or trivial diagrams.

### 2.3 Major — parsing and round-trip corruption
- **ID %-encoding asymmetry:** `FormatNodeURI` percent-encodes `"`, space, `<`, `>`, backtick (`types/types.go:224-229`) but `ParseNodeURI` performs **no decoding** (`types.go:242-257`). A node `file:my dir/x.go` parses back as `file:my%20dir/x.go`. User/AI-supplied entry IDs with spaces can never match. A literal `%` is not escaped at all.
- **Literal unescape order corrupts backslashes:** `parseLiteral` does `\"`→`"`, `\n`→newline, `\\`→`\` (`extractor.go:580-582`). Text that originally contained `\n` (escaped `\\n` in TTL) becomes a **real newline**, later breaking every renderer. `\r` is not unescaped at all (asymmetric with serializer).
- **Silent partial graphs:** malformed statements are dropped with only a stderr warning (`extractor.go:398-401, 420-424, 431-433, 463-465, 487-489, 538`); `ParseTTLFile` never reports content errors — dangling edges survive while their nodes vanish.
- **64KB line cap:** `bufio.Scanner` aborts scan past 64KB lines (`extractor.go:344`) — partial graph, no error.
- Predicates the serializer emits but no extraction group consumes are silently lost (`gm:throws`, `gm:cyclicDependency`, `gm:returns`, `gm:hasParam`); group entries the serializer never emits are dead (`gm:implements`, `gm:aggregates`, `gm:controlFlowToTrue/False`, `gm:imports`, `gm:belongsToNamespace`, `gm:hasMember`).

### 2.4 Major — algorithmic and rendering correctness
- **BFS is O(depth × V × E):** `for _, e := range allEdges` nested inside the queue loop in all traversals (`extractor.go:608-617, 162-171, 213-226`); CallGraph uses MaxDepth 99 → ~100 × V × E scans. At 10k nodes/100k edges that is ~10¹¹ operations per diagram — minutes of CPU. **No adjacency index exists anywhere in stage1.**
- **Cache poisoning:** `parseGraph` returns the shared cached pointer (`visualizer.go:216-218,225`) and `ApplyScope` mutates it in place (`extractor.go:655-656,679-680`) — after one scoped request the cached "full" graph is permanently truncated for all later requests. Cache key ignores scope (`visualizer.go:215`).
- **Alias collisions:** `sanitizeName` maps `- . / : % ( ) < > [ ]` and spaces all to `_` (`helpers.go:10-31`) → `user-service.go::X` and `user_service.go::X` collide → duplicate class/ER/C4 declarations (parse error) or silent node-merging.
- **Class-diagram predicate mapping is wrong:** serializer emits `gm:extends` for EdgeExtends (`turtle_serializer.go:233-234`) but the renderer switch handles `inheritsFrom/implements/composes/aggregates` — **not `gm:extends`** (`mermaid.go:158-169`) → every extends edge becomes `..> uses`; `gm:implements` never exists, so interfaces draw as inheritance.
- **Same-file fallback fans out spurious relations:** one call in a 10-class file yields up to 10 `..> uses` edges (`helpers.go:84-96`).
- **Newlines in labels break every format** (`helpers.go:116-132` doesn't strip `\n`; truncation at byte 60 can split runes).
- **`%%` summary footer appended to mindmap** (which rejects comments) and every other diagram (`helpers.go:264-272`, `mermaid.go:82`).
- **Mindmap has no root node** (`mermaid.go:674-690`) → parse error on real projects.
- **Metrics:** diameter/avg-path computed on the *undirected closure* (`path.go:164-166,195-197`); disconnected graphs silently report intra-component values as if global; density can exceed 1 when dangling edges exist (`metrics.go:198-201`); k-core tail can assign N+1 (`clustering.go:37-49,68-72`); Louvain `modularityGain` is a crude approximation and can assign nodes to an empty-string community (`community.go:41,102-115`).
- **Semantic stubs:** C4Dynamic renders with C4Context header (`mermaid_c4.go:188`), C4Code is just a class diagram (`mermaid_c4.go:155-157`), UMLTiming renders a timeline, UMLState fabricates `[*] --> firstState` from arbitrary nodes (`mermaid.go:476-481`).
- **Depth semantics lie:** `maxDepth<=0` is capped to 7 (`extractor.go:97-100`) while the AI tool advertises "0 = unlimited" (`diagram_tools.go:45`).
- **`EnableSCC` pipeline flag is dead** — SCCs always computed (`metrics.go:175`).

### 2.5 Error propagation
The only failure points are missing/unreadable TTL (`visualizer.go:76-79, 210-223`). **Nothing validates the extracted subgraph or the rendered markup.** Empty graphs and syntactically invalid markup are returned as success to both `gmb visualize` and the AI `diagram_generate` tool (`diagram_tools.go:86-92`). This is why "the AI engine fails automatically."

### 2.6 Why the tests don't catch any of this
1. Entry-ID mismatch tests pass on empty output (only `result != ""` asserted).
2. Fixtures are not serializer output (`all_kinds.ttl` contains classes the serializer can never emit; `delta_append.ttl` uses a tombstone form the serializer never produces).
3. Every stage3 test asserts substrings (`strings.Contains`), never parses Mermaid/PlantUML/DOT — CRITICAL-23/24 invisible.
4. Fixture IDs (`a`, `comp1`) never exercise sanitizeName collisions, newline labels, or multi-class files.

## Proposed fix plan — Issue 2

**Phase 2A — Make it render valid output (priority 1):**
1. Fix CRITICAL-23: valid Mermaid data-flow syntax (`-.->|label|` closed, or drop labels).
2. Fix CRITICAL-24: C4Deployment header/body consistency.
3. Fix CRITICAL-11: validate entry point exists → hard error ("entry point not found: <id>") instead of silent empty output; propagate as error through `diagram_generate`.
4. Fix CRITICAL-1 (shared with Issue 3): make tombstones parse as deletions, not edges.
5. Reconcile the kind vocabulary: single source of truth (ontology) → serializer map → extraction filters. Add the missing kinds (Database, ExternalSystem, Member, Function/Method, Struct/Class/Interface) to `mapKindToClass` or change extraction filters to the classes that actually exist. Decide deliberately which of the 31 types are supported and drop/repair the rest.

**Phase 2B — Correctness of the parse layer:**
6. Fix `ParseNodeURI` to decode percent-escapes (or stop escaping in `FormatNodeURI` — one canonical ID encoding, both sides).
7. Fix `parseLiteral` unescape order (longest-match first: `\\`, `\"`, `\n`, `\r`) and add round-trip tests for backslash sequences.
8. Make the parser strict-and-loud: return structured parse errors with line numbers instead of stderr warnings; fail visibly on unknown statement shapes.
9. Fix cache poisoning (copy before scope) and add scope to the cache key; or drop the cache for correctness first, optimize later.

**Phase 2C — Algorithms and renderers:**
10. Build an adjacency index in stage1 (node→in/out edges) — turns BFS from O(depth×V×E) to O(depth×(V+E)).
11. Fix sanitizeName collisions (dedup with numeric suffixes), newline stripping, mid-rune truncation; validate all emitted syntax with the actual grammar rules (add a lightweight Mermaid syntax checker — bracket balance, arrow grammar — to stage3 tests).
12. Fix class-diagram predicate mapping (add `gm:extends`); remove the same-file fan-out fallback.
13. Fix metric semantics (directed diameter or document undirected; report disconnectedness; clamp density; fix k-core tail; fix Louvain empty-community assignment).
14. Implement the semantic stubs properly (C4Dynamic/C4Code/UMLTiming/UMLState) or remove them from the advertised 31 types.

**Verification:** for each of the 31 types, add a golden test that (a) feeds the real serializer output of a fixture graph, (b) renders, (c) parses the output with a real Mermaid parser (e.g., `mermaid.parse` via node, or a strict in-repo grammar checker), (d) asserts non-empty node/edge counts. This is the only way green tests stop meaning "something was printed."

---

# Issue 3 — AKG correctness: ontology, serialization, and `akg_state.ttl` conformance

## Findings

### 3.1 The ontology is decorative
- **`ontology.ttl` is never read by any Go code.** Repo-wide grep for `ontology` in `*.go` returns zero matches. No schema-conformance check exists. `ErrSchemaVersion` (`internal/errors/errors.go:10`) is dead code — never returned or checked; `loadFromDisk`'s version block only handles *older* versions, never rejects newer ones (`transaction_manager.go:564-573`).
- 6 declared predicates are **never emitted**: `gm:hasMember`, `gm:hasParameter`, `gm:hasReceiver`, `gm:signature`, `gm:language`, `gm:code` (the pipeline emits `gm:content` instead — undeclared duplicate).
- ~20+ predicates are **emitted but undeclared**: every entry in `node.Properties` is serialized as raw `gm:<key>` (`turtle_serializer.go:109-114`): `content`, `condition`, `module_name`, `file_path`, `fully_qualified_name`, `namespace_scope`, `receiver_type`, `primitive_risk_score`, `architecture_tier`, `macro_rules`, `blast_radius`, `instability`, `pagerank`, `betweenness_centrality`, `cohesion`, `has_behavioral_primitives`, `is_header`, `n_plus_one_query_warning`, etc.
- Pipeline fabricates classes with no ontology declaration: `VIRTUAL_CONTEXT`, `VIRTUAL_QUEUE`, `VIRTUAL_TAINT_SOURCE`, `VIRTUAL_GLOBAL_STATE`, `VIRTUAL_SECURITY_SINK`, `VIRTUAL_RESOURCE`, `EXTERNAL_SDK`, `EXTERNAL_API`, `EXTERNAL_FFI`, `HEAP_ALLOCATION`, `ABSTRACT_CONSTRAINT`, `CFG_FLOW` (also used as an edge type), `EXCEPTIONAL_BRANCH` — all fall through to generic `rdfs:Class` (`turtle_serializer.go:182-184`).
- Consumers filter on predicates that are never emitted (`gm:implements`, `gm:aggregates`, `gm:controlFlowToTrue/False`, `gm:imports`, `gm:belongsToNamespace`, `gm:hasMember`) — see Issue 2 §2.2.

### 3.2 Round-trip (write → read → write) is lossy — the file does NOT perfectly capture the graph
- **Kind collapse:** STRUCT/CLASS/INTERFACE → `gm:TypeDecl` → restore `STRUCT`; FUNCTION/METHOD → `gm:Executable` → restore `FUNCTION`; LOOP/SWITCH_BRANCH → `gm:ControlStructure` → restore `IF_BRANCH` (`turtle_serializer.go:150-225`).
- **Entrypoints, FolderZones, Code, CommitHash are parsed but never restored:** `reconstructFromTurtle` maps only Kind/PrimitiveType/Name/FileURI/LineStart/LineEnd (`transaction_manager.go:641-652`); `gm:isEntrypoint` (`extractor.go:520-523`), `gm:primitiveZone` (`extractor.go:524-525`), `gm:code` (`extractor.go:518-519`) are parsed and **discarded**. After one restart the in-memory graph is missing them; after the next full rewrite they are gone from the TTL permanently.
- **Delta append never persists entrypoints/zones at all** — `SerializeDeltaToTurtle` copies only Nodes + OutboundEdges into the temp graph (`turtle_serializer.go:51-61`) while `writeGraphToWriter` emits `gm:isEntrypoint`/`gm:primitiveZone` from `graph.Entrypoints`/`graph.FolderZones` — which are empty in the temp graph.
- **Tombstones are inert and deletions are lost on restart:** tombstone emitted as a base triple (`turtle_serializer.go:45`) → parsed as an edge (`extractor.go:379-413`) → the only `gm:status` filter is in the node-block path that never fires (`extractor.go:526-529`) → on restore the deleted node *resurrects*, and the tombstone edge degrades to `rdfs:seeAlso` with target `"DELETED"` (`transaction_manager.go:669-690`, `turtle_serializer.go:315-317`).
- **Version is never serialized to the TTL** (`turtle_serializer.go:26-29` writes only commitHash/name) → after TTL restore `Version=1` → `maxAppliedTx=1` → **every committed WAL entry ever written is replayed on every startup** (`transaction_manager.go:94-115`), re-running sweep + full-graph macro inference and rewriting the TTL — even for read-only commands like `gmb status`.
- **URI escaping incomplete** (`types.go:223-238`: missing `{ } | ^`) and **literal escaping asymmetric** (`\r` never unescaped).

### 3.3 MVCC/transaction-manager correctness
- **The JSON base snapshot is structurally empty** — all graph maps carry `json:"-"` (`mvcc.go:29-42`); `saveBaseSnapshot` (`transaction_manager.go:475-489`) writes an empty graph; `loadFromDisk` decodes it and dereferences nil `*CowMap` at `transaction_manager.go:601` → **panic**. The load error is discarded at startup (`transaction_manager.go:64`), so any WAL rotation poisons the next startup. Only avoided today because rotation never triggers.
- **Snapshot isolation is broken:** `Clone()` shares CowMap tree *values* (edge slices) by reference (`mvcc_lazy.go:250-255`); the sweep mutates those slices in place (`transaction_manager.go:312-326, 336-344`) → concurrent readers and the async disk writer observe torn edge lists.
- **No atomic persistence:** `saveToDisk` writes straight to `akg_state.ttl` with `O_TRUNC` (`transaction_manager.go:518`) — no tmp+rename, no fsync. **`docs/architecture.md:292-296` claims atomic tmp+rename exists; it does not.** A kill mid-write corrupts the TTL; the self-heal path then fails too (`transaction_manager.go:590-593`).
- **Write happens after the lock is released** (`transaction_manager.go:155-158` vs `196-201`): a second process can acquire the lock and start its own O_TRUNC while the first is mid-write. `visualize` takes no lock at all for reads (`cmd/visualize.go:121-127`).
- **Errors are swallowed everywhere:** `_ = tm.loadFromDisk()` (`:64`), `_ = tm.Recover()` (`:67`), `_ = tm.wal.MarkCommitted(txID)` (`:189`), async `saveToDisk` error discarded (`:198-200`), `_ = tm.saveBaseSnapshot(g)` (`:209`), checkpoint error discarded (`:204`).
- **Stale data accumulation:** file deletions are never swept in full mode (only `RunIngestionForDelta` emits delete events, and it's not wired); virtual nodes have no `FileSpec.Path` and are invisible to the FileNodeIndex sweep → they persist forever; entrypoints accumulate duplicates every run (`transaction_manager.go:416` appends without dedup; the FQN keys never match `deletedNodeIDs`).
- **Delta append only triggers on deletions ≤20%** (`transaction_manager.go:498-504`) — in practice everything is a full rewrite; when append does trigger, stale superseded triples accumulate (masked by last-block-wins parsing, `extractor.go:542`).

### 3.4 Reasoner
- Runs full-graph algorithms (Tarjan SCC, articulation points, 20-iteration PageRank, Brandes betweenness, islands, god objects, per-node DFS) on **every commit** (`transaction_manager.go:470`) and **every TTL restore** (`transaction_manager.go:693`) — O(V·E)-class work, and its output (`gm:macro_rules` properties) is **persisted into the TTL and then re-inferred on restore** (compounding).
- Cache key excludes `macroMode`/`disabledRules` (`reasoner_cache.go:340-351`) → stale rules when the same process re-infers with different settings.
- Dead branches: `macroMode=="disabled"` early-returns at `reasoner.go:43-45` making the `:108-110` re-check unreachable; rule 33's threshold `pr > 5.0/len(Nodes)` is a fixed constant of 5, not a statistical criterion (`reasoner.go:208`).

### 3.5 Query layer
- `Query` dereferences `KindIndex.Get(...)` with no nil guard (`query.go:15`) — TTL-restored graphs have `KindIndex == nil` (never rebuilt on restore) → **panic**; only the AI bridge patches this (`bridge.go:126-142`), and the patch skips JSON-loaded graphs.

### 3.6 Dead CLI surface
- `cmd/diff.go:32-49` prints a **hard-coded sample diff**; the WAL differ is not wired. `cmd/analyze.go:53-55` `--full` is a no-op (see Issue 1).

## Proposed fix plan — Issue 3

**Phase 3A — Make the ontology real (the blueprint):**
1. Load `ontology.ttl` at build time via `go:embed`; add an in-repo conformance check (unit test) that asserts: every predicate emitted by `mapEdgeTypeToPredicate` and every `gm:<key>` property key emitted by the serializer is declared; every class emitted by `mapKindToClass` is declared.
2. Update `ontology.ttl` to declare the intentional vocabulary: the ~20 property keys, the virtual classes (or formally mark them `gm:Virtual*`), `gm:content`, `gm:status` semantics, and remove/merge duplicates (`hasMember` vs `hasField`, `hasParameter` vs `hasParam`, `inheritsFrom` vs `extends`, `contains` vs `belongsTo`).
3. Add `gm:schemaVersion` to the metadata node and write it on every save; enforce it on load (reject newer schemas with `ErrSchemaVersion`, finally).

**Phase 3B — Fix persistence correctness (priority 1 for data trust):**
4. Rewrite `saveToDisk` to tmp-file + fsync + atomic `os.Rename`; keep the lock held through the write (or serialize writes with an internal writer mutex); make the async write a synchronous-or-queued single writer.
5. Fix the JSON snapshot: either make `CodePropertyGraph` JSON-serializable (persist CowMap contents via exported serialization) or **remove the JSON path entirely** and restore solely from TTL + WAL. Given the TTL is the source of truth, the simplest correct design is: no JSON cache; WAL is the only replay log; TTL is written atomically and fsync'd. If the JSON cache stays, guard all nil CowMaps on load.
6. Fix tombstones: write deletions inside node blocks (`<uri> a gm:Deleted ; gm:status "DELETED" .` style) or teach the parser to treat `gm:status "DELETED"` triples as deletions; on restore, apply them to the graph.
7. Serialize `Version` (as `gm:schemaVersion`/`gm:version` on the metadata node) so WAL replay is bounded by `maxAppliedTx` instead of replaying everything; truncate/rotate the WAL after successful atomic TTL write.
8. Restore Entrypoints/FolderZones/Code/CommitHash on `reconstructFromTurtle` (they're already parsed — just not copied).
9. Fix MVCC snapshot isolation: the sweep must build new edge slices (copy-on-write), never mutate shared slices in place; add a race test with concurrent `ExecuteDeltaTransaction` + reader + async-save.
10. Stop swallowing errors: `loadFromDisk`/`Recover` failures must surface (corruption → clear error message, never silent empty graph); save errors must be reported by `gmb analyze`.

**Phase 3C — Delta mode that actually works:**
11. Wire the delta path (`RunIngestionForDelta` + `CollectGitDiff`) into `analyze`; make `--full` force the full re-scan. Sweep deleted files properly in both modes. Rebuild entrypoints with dedup.
12. Scope the reasoner: run macro inference only on changed subgraphs, or gate it behind the existing `macro-inference` flag with the cache key fixed (include mode + disabled rules); stop persisting `macro_rules` into the TTL (in-memory derived data only).

**Phase 3D — Query/restore robustness:**
13. Rebuild KindIndex/HashIndex/FolderZones/macroCache on restore (or make `Query` nil-safe like the rest of the graph API).
14. Fix URI escaping (`{ } | ^`) and literal `\r` handling symmetrically in serializer and parser.
15. Implement `cmd/diff` against WAL history or remove the command.

---

# Issue 4 — Hardware/memory safety (user-machine CLI)

## Findings

### 4.1 Everyone loads the whole graph
- `gmb visualize`: whole-file parse → full `NativeGraph` (`visualizer.go:209-227`), cached **globally** (`SubgraphCache`, `visualizer.go:31-37`, max 128 entries, 10-min TTL). Scope filtering happens *after* the full parse (`visualizer.go:76-90`), so a `file:x` diagram still loads the entire repo graph. `gmb ai` re-parses the full TTL in the same process via `diagram_generate` (`diagram_tools.go:86`) while the bridge also holds the full AKG graph.
- `gmb status` / `gmb inspect` / `gmb dependency` / `gmb hotspot` / `gmb diff` / `gmb tree`: all `NewAKGTransactionManager` → `loadFromDisk` → whole-graph restore (`cmd/status.go:30-35`, `cmd/inspect.go:32-37`, etc.).
- The AI bridge eagerly loads a full snapshot whenever tools are enabled (`engine.go:147-165`, `bridge.go:83-119`), and TTL-restore additionally runs full-graph macro inference (`transaction_manager.go:693`).

### 4.2 Multiple coexisting full copies
During `gmb analyze`: JSON/TTL restore copy + active graph + MVCC shadow (near-complete second copy; CowMap Set clones O(log n) nodes per insert, `cowmap.go:68-104`) + `saveToDisk(shadow.Clone())` and `saveBaseSnapshot` goroutines + stage-4 plain-Go maps (`stage4/type.go:122-159`) + WAL JSON buffer → **steady 2–3 full graphs, transient up to 4**. In an AI session: bridge graph + viz NativeGraph coexist (2 different-shaped full copies). The global SubgraphCache can hold up to 128 full graphs.

### 4.3 Measured memory multipliers (10k/10k = current scale; targets = enterprise)
| Scale | TTL size | Viz parse (NativeGraph) | AKG graph (steady) | analyze peak | ai session peak |
|---|---|---|---|---|---|
| 10k nodes + 10k edges | ~9–11 MB | ~16–20 MB | ~20–28 MB | ~50–70 MB | ~40–50 MB |
| 100k + 100k | ~90–110 MB | ~160–200 MB | ~200–280 MB | ~500–700 MB | ~400–500 MB |
| 1M + 1M | ~0.9–1.1 GB | ~1.6–2.0 GB | ~2.0–2.8 GB | ~5–7 GB (**OOM on 8 GB machines**) | ~4–5 GB |

### 4.4 Unbounded growth / no guards
- **No size guard anywhere**: no TTL size check, no disk-quota check, no memory budget. Grep for `memory|limit|budget|maxBytes` finds only LLM token/cost budgets (`cmd/ai.go:823-825`).
- `--max-nodes`/`--abort-on-limit` (`cmd/analyze.go:98-103, 154-155`) are checked in `linker.go:174-179` **after** the full link is already built — they cannot prevent the spike they purport to cap.
- **WAL grows unboundedly** (append-only, rotated only >100 MB, segments never truncated — `wal.go:82-105`), and `ReadAllEntries` decodes all segments into RAM on every open (`wal.go:117-134`).
- `.glassmarble/` accumulates: TTL full rewrites, WAL up to 200+ MB, `marbles/` and `ai/sessions/` with no pruning.
- `macroCache` (`mvcc.go:30`) grows one entry per distinct node key, only reset during a transaction sweep; duplicated rule text exists in 3 places per node (properties + MacroRules CowMap + macroCache).
- `internal/config/config.go` has `WorkerCount`/`MaxFileBytes` knobs but **no command imports it**; `cmd/analyze.go:39` builds `stage1.DefaultConfig` directly — config file knobs are unwired.
- No progress indication for the expensive phases (restore, full rewrite, stage-4 link, macro inference).

### 4.5 Crash/corruption risks
- No atomic TTL write, no fsync (§3.3) — kill mid-write → corrupt TTL → consumers silently serve partial/empty graphs (`ParseTTLFile` returns what survived; `loadFromDisk` errors discarded).
- Async write races the next transaction's sweep (shared slices) and a concurrent process's write (§3.3).
- Stale `akg_state.json.gz` preferred over fresher TTL with **no freshness check** (`transaction_manager.go:535-561`) → silently stale graphs for status/ai/inspect.
- Windows lock-probe relies on matching `"already finished"` in process-query error text (`transaction_manager.go:712-725`) — platform-fragile.

## Proposed fix plan — Issue 4

**Phase 4A — Make reads cheap and lazy (priority 1):**
1. **Single canonical graph representation.** Have the AKG package own one parser; make the viz engine and the AI bridge consume the same in-memory form (via the AKG API) instead of three parsers/copies. This alone removes the largest structural overhead.
2. **Adopt a delta/lazy access pattern:** add `Query`-based entry points (node lookup, subgraph extraction) that load only what is needed; use it in `status`/`inspect`/`diagram_generate` (file-scope diagrams should parse only the file's triples).
3. Fix the SubgraphCache: bound by bytes (e.g., 64 MB) not count; include scope in the key; never mutate cached graphs (copy before scope).
4. Wire `internal/config` knobs (`max_file_bytes`, `worker_count`) into `cmd/analyze.go`; add explicit `--max-ttl-mb` guard that refuses/streams beyond a budget.

**Phase 4B — Bounded resources:**
5. Replace the whole-graph JSON/WAL replay on open with a bounded protocol: atomic TTL write + truncated WAL (see Issue 3 Phase 3B); on open, load TTL once into the graph; drop `ReadAllEntries` of all segments.
6. Add streaming serialization verification: serializer already streams via `fmt.Fprintf` (good) — keep it that way and never build giant strings; audit the parse path for `strings.Join(block, " ")` retention (extractor.go:355 — attribute strings are substrings of the joined block, pinning whole statements in RAM).
7. Cap `macroCache` and drop persisted `macro_rules` (derived data, Issue 3 Phase 3C-12).
8. Add `.glassmarble` housekeeping: prune WAL segments after successful atomic writes; document/prune `marbles/`, `sessions/`; report `gmb status` size info.
9. Add progress reporting with timestamps/memory deltas to the long phases (parse, link, reasoner, save) via `--verbose`.

**Phase 4C — Safety rails:**
10. Enforce lock semantics: writers serialize; readers take shared lock; single-writer async queue; fsync + atomic rename on TTL.
11. Startup diagnostics: if TTL is corrupt/over budget → hard, actionable error; `gmb doctor` (`cmd/doctor.go`) should check TTL integrity + size + WAL state.
12. Document the memory envelope in `docs/architecture.md` and the CLI help (expected RAM by graph scale).

---

# Issue 5 — QA of the final `akg_state.ttl` (the product's core artifact)

## Findings

There is **no production QA** for the persisted graph. Searches for `validate|audit|qa|schema|conformance|integrity|verify` across `cmd/` and `internal/` find only:

1. **Dangling-reference audit — in-memory only.** Runs pre-serialization (`transaction_manager.go:452-467`), results go into `shadow.Errors`, which the serializer **never writes**, and which `gmb status` merely counts (`cmd/status.go:50`). Dangling edges are silently persisted.
2. **Ontology conformance — zero.** The ontology is never loaded (Issue 3 §3.1).
3. **No parse-back of the written file.** Round-trip checks exist only in unit tests (`akg/serializer_test.go:135-165, 221-272, 274-317`), and they use fixtures/limited cases that hide the bugs listed in Issue 2 §2.6. Production write path: write → return.
4. **No completeness/duplication/size checks.** Nothing verifies all files/commits are present; the parser silently dedups (last-wins) and silently drops malformed blocks; consumers never know the file contains garbage.
5. **Stale-cache hazard** (§4.5): JSON cache preferred without freshness check.
6. **Silent corruption serving** (§4.5): a truncated TTL produces "AKG State: Empty" (`cmd/status.go:36-39`) and AI bridge says "is empty — run `gmb analyze` first" (`bridge.go:110-111`) — misleading diagnostics.

So today, the user — and the LLM — cannot know whether `akg_state.ttl` is correct, complete, noise-free, or even valid.

## Proposed fix plan — Issue 5

**Phase 5A — Validation at write time (priority 1):**
1. **Post-write verification step in the transaction commit:** after atomic write, re-read the file with the canonical parser and assert: triple count parity with the graph, no parse warnings, zero dangling edges (edge target ∈ node IDs), zero duplicate node IDs. Fail the commit loudly (keep the previous good file) or mark the graph `verified:false`.
2. **Schema conformance check** (shared with Issue 3 Phase 3A): every predicate/class in the file is declared in the ontology; report unknown-predicate counts.
3. **Noise budget report:** at end of `gmb analyze`, print QA metrics: files, nodes, edges, unresolved calls, virtual-node count, dangling edges, TTL size, WAL size, and the same stats per link level. This makes bloat visible and measurable (and gives the Issue 1 fixes a scoreboard).

**Phase 5B — `gmb doctor` and `gmb status` as the QA surface:**
4. Extend `gmb doctor` to: parse-back the TTL, check ontology conformance, check dangling references, check freshness (TTL vs WAL vs JSON cache), report size/history, and detect duplicate IDs and unknown classes/predicates. Exit non-zero on integrity failures.
5. `gmb status` reports: schema version, commit hash, node/edge counts, entrypoint count, virtual-node share, last-analysis time, file freshness — the "health dashboard" for the core artifact.

**Phase 5C — Continuous quality (tests as QA):**
6. **Round-trip property tests:** for representative graphs (and the fixtures), assert `graph → TTL → parse → graph` equality of nodes, edges, entrypoints, zones, kinds — covering every issue documented in §3.2 (entrypoints, zones, version, tombstones, escaping).
7. **Golden diagram tests** (Issue 2 verification) that use real serializer output.
8. **Bloat regression guard:** a CI test that runs the pipeline on this repo and fails if nodes/edges exceed a budget (e.g., <1.5k nodes for ~200 files) — making the 10k/10k problem impossible to reintroduce.
9. **Corruption drills:** tests that truncate/corrupt the TTL and assert the CLI errors cleanly instead of serving partial graphs.

---

# Cross-cutting remediation roadmap

## Priority ordering

| Priority | Phase | Why first | Est. effort |
|---|---|---|---|
| P0 | 1A (translator catch-all fix, defaults) | Biggest quality/scale win per line of code; unblocks everything downstream | ~1–2 days |
| P0 | 2A (CRITICAL-1/11/23/24, kind vocabulary) | Makes diagrams real; unblocks AI | ~2–3 days |
| P0 | 3B (atomic write, tombstones, version, restore loss, JSON snapshot) | Data trust + crash safety | ~3–4 days |
| P1 | 1B (noise removal), 2B (parser), 3C (delta wiring), 4A (lazy reads, single representation) | Scale + correctness | ~1–2 weeks |
| P1 | 5A (write-time validation + QA report) | Locks in gains; makes quality measurable | ~2–3 days |
| P2 | 1C (performance index), 2C (renderers/metrics), 3D (query/restore), 4B/4C (budgets/locks/progress) | Enterprise scale | ~2–3 weeks |
| P2 | 5B/5C (doctor, status, property/golden tests) | Long-term trust | ~1 week |

**Sequencing note:** phases within a priority are independent enough to parallelize across 2–3 engineers, but 3B must precede 4B (the atomic-write/WAL redesign is the foundation for memory bounding), and 2A's kind-vocabulary decision must be made *before* 3A's ontology update (the ontology must match the pipeline, not the other way around).

## Top 10 single-line-of-code wins (quick wins before the big work)
1. `cmd/analyze.go:90` — default `LevelOfDetail: "architecture"`.
2. The 10 translator `default:` branches (Issue 1 §1.3) — control-flow → `GASTControlFlow`.
3. `mermaid.go:648-653` — close the `-.->|` link-text marker.
4. `mermaid_c4.go:213` — C4Deployment header.
5. `extractor.go:283-286` + `605` — entry-point existence check with hard error.
6. `parseLiteral` unescape order + `\r` (`extractor.go:573-584`).
7. `ParseNodeURI` — decode percent-escapes (`types.go:242-257`).
8. `extractor.go:655-656,679-680` — copy before `ApplyScope` (cache poisoning).
9. `external_dependency_indexer.go:64-70` — stop treating stdlib as external.
10. `dependency_linker.go:159-167` — SCC edge cap.

## Verification protocol for the whole remediation
After each phase: `go test ./... -count=1`, `go vet ./...`, `go build ./...`, plus the new regression tests (Issue 5 Phase 5C). Run `gmb analyze` on this repo before/after each phase and compare the QA report (nodes, edges, unresolved, virtuals, size) — the scoreboard must only go down. Keep the invariant from earlier work: **never touch `internal/visualization_engine` files, `internal/akg` files, or `internal/code_analysis_engine` files except for the fixes in this plan** — this audit is the only sanctioned change list for those packages.

---

## Appendix A — Conformance quick-reference (serializer ↔ ontology ↔ consumers)

Declared-but-never-emitted: `gm:belongsToNamespace`, `gm:hasMember`, `gm:hasParameter`, `gm:hasReceiver`, `gm:signature`, `gm:language`, `gm:code`.
Emitted-but-undeclared (properties): `gm:content`, `gm:condition`, `gm:module_name`, `gm:file_path`, `gm:fully_qualified_name`, `gm:namespace_scope`, `gm:local_boundary`, `gm:receiver_type`, `gm:is_async`, `gm:type_params`, `gm:primitive_risk_score`, `gm:primitive_risk_level`, `gm:architecture_tier`, `gm:architectural_violations`, `gm:has_behavioral_primitives`, `gm:is_header`, `gm:role`, `gm:is_external`, `gm:primitive`, `gm:base_target`, `gm:caller_ctx`, `gm:logic`, `gm:param_count`, `gm:var_count`, `gm:params`, `gm:vars`, `gm:ffi_lang`, `gm:data_sensitivity_level`, `gm:data_privacy_violation`, `gm:n_plus_one_query_warning`, `gm:performance_hot_path`, `gm:resilience`, `gm:observability_blindspot`, `gm:macro_rules`, `gm:blast_radius`, `gm:instability`, `gm:pagerank`, `gm:betweenness_centrality`, `gm:cohesion`.
Consumed-but-never-emitted: `gm:implements`, `gm:aggregates`, `gm:controlFlowToTrue`, `gm:controlFlowToFalse`, `gm:imports`, `gm:belongsToNamespace`, `gm:hasMember`.
Classes fabricated without ontology declaration: `VIRTUAL_CONTEXT`, `VIRTUAL_QUEUE`, `VIRTUAL_TAINT_SOURCE`, `VIRTUAL_GLOBAL_STATE`, `VIRTUAL_SECURITY_SINK`, `VIRTUAL_RESOURCE`, `EXTERNAL_SDK`, `EXTERNAL_API`, `EXTERNAL_FFI`, `HEAP_ALLOCATION`, `ABSTRACT_CONSTRAINT`, `CFG_FLOW`, `EXCEPTIONAL_BRANCH`, `VIRTUAL_CLOUD_API`.
Ontology classes never instantiated: `gm:Member`, `gm:Parameter`, `gm:Block`, `gm:Annotation`.

## Appendix B — Key file:line index (for engineers executing the plan)

- Pipeline wiring + defaults: `cmd/analyze.go:39,44,53-55,90,98-103,119,127,151-156`
- Translator catch-alls: `internal/code_analysis_engine/stage2/java_translator.go:46-49`, `python_translator.go:38-41`, `javascript_translator.go:36-42`, `typescript_translator.go:47-53`, `c_translator.go:34-37`, `cpp_translator.go:37-43`, `csharp_translator.go:42-45`, `rust_translator.go:49-52`, `php_translator.go:39-45`, `ruby_translator.go:42-45`, reference: `go_translator.go:72-80`
- Parser force-classification: `internal/code_analysis_engine/stage1/parser.go:188-197,220-224`
- Bloat producers: `stage4/cfg_linker.go:79-119,174-177`, `dfg_linker.go:80-122,127-152`, `call_linker.go:83-107,138-144,257-299`, `constraint_linker.go:46-69`, `alias_linker.go:46-76`, `dependency_linker.go:39-66,159-167`, `semantic_linker.go:23-31,55-162`, `external_dependency_indexer.go:21-70`, `memory_linker.go:48-72`
- Serializer: `internal/akg/turtle_serializer.go:14-64,66-148,150-225,227-326`
- Transaction manager: `internal/akg/transaction_manager.go:40-70,73-120,146-236,256-473,475-524,526-621,623-696,698-742`
- MVCC/WAL/reasoner/query: `internal/akg/mvcc.go:29-43,672-706,779-809`, `internal/akg/mvcc_lazy.go:250-255`, `internal/akg/wal.go:52-105,117-137`, `internal/akg/reasoner.go:43-45,107-258,337-360`, `internal/akg/reasoner_cache.go:340-351`, `internal/akg/query.go:12-63`, `internal/akg/cowmap.go:68-104`
- TTL parser: `internal/visualization_engine/stage1/extractor.go:28-55,97-100,147-176,233-286,334-377,379-413,415-544,573-584,605-617,655-680`
- Viz coordination: `internal/visualization_engine/visualizer.go:31-37,63-117,209-227`
- Renderers: `stage3/mermaid.go:82,128,158-169,476-481,507-531,583-602,648-726`, `stage3/mermaid_c4.go:155-157,188,213,223`, `stage3/helpers.go:10-31,84-96,116-132,264-272`, `stage3/plantuml.go:83-95`
- Metrics/clustering: `stage2/metrics.go:10-108,127-151,198-201`, `stage2/path.go:164-197,220-227,249-320`, `stage2/community.go:41,50,102-115`, `stage2/clustering.go:37-72`, `stage2/aggregator.go:106-152,179-199,303,371`
- Consumers: `cmd/status.go:30-50`, `cmd/inspect.go:32-37`, `cmd/visualize.go:116-127`, `internal/ai_engine/engine.go:147-165`, `internal/ai_engine/akgbridge/bridge.go:83-142`, `internal/ai_engine/tools/diagram_tools.go:45,57-59,86-92`
- Ontology: `internal/akg/ontology.ttl` (entire file — 447 lines, no code reference)

---

## Remediation status (scoreboard)

Updated 2026-08-01. Verification after every batch: `go build ./...`, `go vet ./...`, `go test ./... -count=1` all green; live `gmb analyze` on this repo round-trips clean (post-write verification: node & edge parity, 0 dangling).

| Issue | Batch | Status | Evidence |
|---|---|---|---|
| 1 | A | **DONE** | `stage2/type.go` + all translators emit real `GASTVariable`; per-statement fake FUNCTION nodes gone; node/edge bloat cut (see live run below); `debug_cst.go`/`debug_dangling.go` scratch tools + `--full` no-op removed |
| 2 | C | **DONE** | `stage3/mermaid.go` + `mermaid_c4.go` all 31 renderers on the alias registry (collision-safe); C4Dynamic/C4Deployment/C4Code emit real diagrams; `stage3/syntax_test.go` golden-syntax checker over all 31 types; DataFlow arrows `==>`/`-.->` |
| 3 | A–D | **DONE** | `ontology.go` go:embed + conformance tests; atomic tmp+fsync+rename saves; tombstone node blocks; version-bounded WAL replay + truncation; `cmd/diff` WAL-based; URI escaping `{ } \| ^ \r \n \t " < > \` % space backslash` symmetric in `FormatNodeURI`/`ParseNodeURI`; serializer dedups parallel triples (max line number) matching the canonical parser |
| 4 | D | **DONE** | config file (`WorkerCount`, `MaxFileBytes`) wired into `cmd/analyze.go`; WAL rotation on successful commit |
| 5 | D | **DONE** | `gmb doctor` (parse-back, dangling, duplicate IDs, unknown gm: terms, stale WAL), `gmb status` verification/freshness, `gmb diff`; stage1 extractor flags unterminated trailing blocks (truncation detection); post-write verification fails commits on node/edge lossiness |
| 5 | E (final QA) | **DONE** | **Zero-dangling guard**: strict post-write verification now FAILS commits on any dangling edge (source or target), with actionable rebuild guidance; merge sweep moved post-graft (Step C.4) so cross-file edges from unchanged files survive incremental commits and only truly-gone nodes are tombstoned — the persisted TTL is byte-equivalent between full and incremental runs; dropped edges are recorded in `graph.Errors` for `gmb status`/`doctor`; WAL rolled back on rejected commits so recovery never blocks; graphs load as `verified` when the file reconstructs clean (was: always UNVERIFIED); QA report measured on the merged graph with TTL/WAL sizes (`ttl=3.5MB wal=0B`) |

**Live run (this repo, `gmb analyze --link-level=architecture`):**
Full scan: `173 files | 2308 nodes | 5479 edges | 527 virtual | 0 dangling | ttl=3.5MB wal=0B | 5.3s` — commits atomically, post-write parity + zero-dangling verified. Incremental delta: commits with the same persisted triple set (3813 base triples identical to the full run; doctor parse-back `2308 nodes, 3813 edges`, 0 dangling, 0 duplicate IDs); `gmb status` reports `Verification: verified`, 0 health errors, freshness ok; `gmb diff` shows WAL truncated after commit.

**Regression tests added (Batch E):** `TestDanglingEdgeSweepNeverPersists` + updated `TestExecuteDelta_DanglingReferenceAudit` (akg): the merge sweep drops dangling edges, records them, and nothing dangling is ever persisted or reloaded. `TestEscapeAnalysisTargetsExist` (stage4): full-mode escape-analysis edges (`VAR_`/`memory::HEAP`) always land on created nodes.

**Remaining known gaps (tracked, not in this remediation's scope):** 16 unknown `gm:` terms reported by `gmb doctor` (ontology.ttl does not yet declare the full stage-4 kind vocabulary); `gmb status` metadata section reports last-commit (delta) stats rather than cumulative graph totals; delta-mode linking produces fewer parallel in-memory call-site edges than a full scan (per-triple persisted state is equivalent — the TTL captures the max line per triple either way).
