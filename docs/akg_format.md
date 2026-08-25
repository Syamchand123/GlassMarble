# The AKG GraphJSON Format (`.glassmarble/akg.json`)

The canonical GlassMarble state file is `akg.json` — a **GraphJSON** document (schema v3) stored inside the workspace's `.glassmarble/` directory. It is the single source of truth: deterministic, diff-friendly, and human-auditable. There is no WAL and no separate RDF/Turtle store — this one file holds the entire Architecture Knowledge Graph.

## 1. Document Shape

```json
{
  "schema_version": 3,
  "commit_hash": "f438841a7ac27c9c910881070f65fd9fd2c90a72",
  "version": 5,
  "entrypoints": ["cmd.analyze.init", "cmd.visualize.init", "main", "main.main"],
  "nodes":   [ /* NodeJSON ... */ ],
  "edges":   [ /* EdgeJSON ... */ ],
  "summary": { "primary_patterns": ["Component X enforces Security Validation before Storage Persistence [structural]", "..."] },
  "errors":  [ /* dropped-edge records ... */ ],
  "verified": true
}
```

| Field | Meaning |
|---|---|
| `schema_version` | Always `3` for current stores. |
| `commit_hash` | Git commit (or `"migrated_v3"` after a migration) the analysis ran against. |
| `version` | Monotonic graph revision — incremented on every commit. |
| `entrypoints` | FQNs of resolved entry points (used for entry-driven subgraph extraction). |
| `nodes` | Sorted node array. |
| `edges` | Sorted edge array. |
| `summary` | Intelligence summary (e.g. detected `primary_patterns` from the last run). |
| `errors` | Non-fatal records of edges dropped during merge sweeps (dangling references) — kept so `gmb doctor` and drift can report them. |
| `verified` | `true` after a successful post-write verification. |

Nodes and edges are emitted in deterministic sorted order so that `git diff` on the state file is meaningful and drift detection is cheap.

## 2. Nodes

```json
{
  "id": "auth/login.go::Authenticator::Authenticate",
  "kind": "FUNCTION",
  "name": "Authenticate",
  "primitive": "NETWORK_IO",
  "primitive_scores": { "NETWORK_IO": 1 },
  "file_spec": { "path": "auth/login.go", "line_start": 18, "line_end": 45 },
  "properties": {
    "fully_qualified_name": "auth.Authenticator.Authenticate",
    "module_name": "auth.login",
    "architecture_tier": "DomainLayer",
    "return_type": "string",
    "content": "func (a *Authenticator) Authenticate(...) ..."
  }
}
```

### 2.1 Node IDs

IDs follow `<path>::<type>::<member>` with `::` separators; parameters and fields append `::param:<name>` / `::field:<name>`. Example: `cmd/aging.go::agingPinsFromCorrections::param:repoDir`.

### 2.2 Node kinds (uppercase)

Observed kinds in a real repository: `PARAM`, `FUNCTION`, `FIELD`, `EXTERNAL_API`, `METHOD`, `FILE`, `STRUCT`, `FUNCTION_TYPE`, `MODULE`, `ALIAS`, `EXTERNAL_SDK`, `INTERFACE`.

Schema v3 migration consolidates legacy kinds (`TYPE_DECL` → `STRUCT`, `EXECUTABLE` → `FUNCTION`, `TYPE` → `STRUCT`) and folds the legacy `code` property key into `content`.

### 2.3 Properties

`properties` is a string-keyed, string-valued map: FQN, module/package, namespace scope, architecture tier, return type, primitive risk (`primitive_risk_level`, `primitive_risk_score`), behavioral-primitive flags, and — when `--store-code` is enabled — the source `content` snippet.

## 3. Edges

```json
{
  "source_id": "auth/login.go::Authenticator::Authenticate",
  "target_id": "db/database.go::DBStore::GetUser",
  "type": "CALLS",
  "line_number": 22,
  "confidence": 1.0,
  "properties": { "gm:provenance": "ast" }
}
```

| Field | Meaning |
|---|---|
| `source_id` / `target_id` | Node IDs — the references must resolve (see `gmb doctor`). |
| `type` | Uppercase predicate, e.g. `CALLS` (see §4). |
| `line_number` | Exact source line of the relationship. |
| `confidence` | `1.0` for AST-observed, lower for heuristic inference. |
| `properties` | Optional metadata; `gm:provenance` records how the edge was derived (`ast`, heuristic, etc.). |

## 4. Edge Predicate Vocabulary

| Predicate | Meaning |
|---|---|
| `CALLS` | Call from source to target function/method |
| `BELONGS_TO` | Member → containing declaration/module |
| `HAS_PARAM` | Function/method → parameter |
| `CONTAINS` | Container → nested declaration/scope |
| `HAS_FIELD` | Type → field |
| `COMPOSES` | Type composition (embedded types, structural ownership) |
| `RETURNS` | Function/method → return type reference |
| `DEPENDS_ON` | Module/package-level dependency |
| `HAS_RECEIVER` | Method → receiver type |
| `IMPLEMENTS` | Structural interface implementation |
| `EXTENDS` | Type inheritance |

The full `RelationshipType` taxonomy (STRUCTURAL / BEHAVIORAL / DYNAMIC / SECURITY groups) is documented in `docs/relationship_types.md`. Predicate names are stable: the `EventKind` and node-kind string values must never be renamed after first release.

## 5. Size Guard & Storage Contract

`--max-json-mb <n>` (global flag, 0 = unlimited) refuses to load or commit a state file larger than N MiB. `gmb status` reports the current file size; `gmb stats --bench` verifies the budget gates (state ≤ 12 MB, json state ≤ 8 MB).

### 5.1 Workspace Storage Layout (post-v1.0.1)

```
.glassmarble/
├── akg.json                 # canonical graph — pretty-printed, diff-friendly, atomic commit (NEVER pruned)
├── snapshots/
│   ├── snap_<id>.json       # metadata only (~KB, compact JSON) when large-repo threshold hit
│   ├── snap_<id>.graph.json.gz  # gzipped compact GraphJSON sidecar (only when graph embedded)
│   └── index.json
├── memory/
│   ├── events.jsonl         # WAL source of truth (fsynced per line)
│   ├── memory.json          # derived aggregate, compact JSON (rebuildable from WAL)
│   └── timeline.json        # derived timeline, compact JSON
├── intelligence/latest.json # compact JSON
├── marbles/                 # diagrams
└── ai/sessions/             # chat sessions
```

* `akg.json` is the only non-optional artifact (≈1.2 KB/node pretty, ≈0.9 KB/node compact). No compromise on fidelity.
* Snapshots no longer duplicate the graph as an escaped JSON string. When `intelligence.snapshot_no_graph=false` and the graph is embedded, it is stored as a gzipped sidecar (`~5×` smaller) and omitted from `snap_<id>.json`. When the repo exceeds `snapshot_auto_threshold_nodes` (15k) or `snapshot_auto_threshold_mb` (8 MB), snapshots automatically switch to `no-graph` (≈KB) and `gmb snapshot --replay` / structural diffs are disabled for those snapshots (use `git` history via `akg.json` instead).
* `gmb housekeeping` now reports `snapshots/` + `memory/` + `intelligence/` and enforces `snapshot_max_count` (default 30) on every `gmb analyze` / `gmb snapshot --create`.
* Use `gmb analyze --snapshot-no-graph` to force metadata-only snapshots, or `gmb housekeeping --prune-snapshots --keep 10` to reclaim disk immediately.

### 5.2 Scaling to 500k LOC (>60k nodes)

| Nodes | akg.json (pretty) | akg.json (compact) | 30 snapshots (no-graph) | memory.json (compact) | Total est. |
|---|---|---|---|---|---|
| 13k (this repo) | 17 MB | ~12 MB | ~0.3 MB | ~13 MB (was 19) | ~30 MB |
| 40k | ~52 MB | ~36 MB | ~0.9 MB | ~35 MB | ~75 MB |
| 67k (500k LOC) | ~88 MB | ~60 MB | ~1.5 MB | ~55 MB | ~120 MB → set `snapshot_no_graph=true` already, prune memory via `gmb memory` GC keeps under 100 MB for the other artifacts; `akg.json` gzipped backup ~15 MB if needed |

> Keep `.glassmarble` under 100 MB for large repos: `akg.json` compact (~60 MB) dominates; the remaining budget is achieved by `snapshot_no_graph` (auto) + `snapshot_max_count=30` + compact `memory.json`/`intelligence.json`. Use `--max-json-mb 100` to gate CI.

### 5.3 Old Snapshot Migration

Pre-v1.0.1 `snap_<id>.json` files with inline `akg_json` (escaped string, 50 MB) are still readable by `Replay` and `store.loadSnapshotLocked`. New snapshots use the sidecar; run `gmb housekeeping --prune-snapshots --keep 30` to drop legacy weight.

## 6. Migration & Self-Healing

`AutoMigrateOnLoad` upgrades older stores in place:

| From | Action |
|---|---|
| schema v1 | Baseline TTL-era store → migrated to v3 with a `akg.json.v1.bak` (or `akg_state.v1.ttl.bak`) backup. |
| schema v2 | TTL-era store with commit-hash metadata and tombstone blocks → migrated to v3 with a `akg.json.v2.bak` backup. |
| legacy `akg_state.ttl` (pre-v1) | Parsed once on load (self-heal behind a fallback flag), written as `akg.json`, then retired. |

Migration reclassifies stale node kinds and ensures `content`/`code` property symmetry. A migrated graph is tagged with `"commit_hash": "migrated_v3"` when no commit is available.

## 7. Tooling

| Command | Purpose |
|---|---|
| `gmb status` | Schema/graph version, commit, node/edge counts, verification status. |
| `gmb doctor` | Parse-back integrity, duplicate node IDs, dangling edges. |
| `gmb diff` | Node/edge deltas between the last two committed states. |
| `gmb export --format graphjson` | Write a normalized copy of the graph (default format). |
| `gmb import [graph.json]` | Load an external GraphJSON file into the workspace. |
| `gmb export --format neo4j` | Emit Cypher: nodes as `GMNode:<Kind>` labels, `MATCH (a:GMNode {id: ...}), (b:GMNode {id: ...}) CREATE (a)-[:<PRED>]->(b)`. |
| `gmb visualize` | Any diagram type reads the same file through the projection engine. |
| `gmb compare <base> <head>` | Structural diff of two GraphJSON files. |

## 8. Worked Example

The simplest complete, valid document (as produced by `gmb init`):

```json
{
  "schema_version": 3,
  "commit_hash": "",
  "version": 0,
  "entrypoints": [],
  "nodes": [],
  "edges": [],
  "summary": {},
  "errors": [],
  "verified": true
}
```

After a first `gmb analyze`, `entrypoints` lists resolved init/main symbols (`cmd.analyze.init`, `main.main`, ...), `nodes` carries every declaration with file spans, and `edges` carries calls and structural relations with source lines. Adding `"content"` properties requires `gmb analyze --store-code`; intelligence `summary` is written after the post-commit intelligence pass.
