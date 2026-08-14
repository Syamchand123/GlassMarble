# GlassMarble Architecture Intelligence

Architecture Intelligence (AI) is the post-commit layer that turns the raw Architecture Knowledge Graph (AKG) into **evidence**: detected patterns, smells, components, architectural change events, timelines, and snapshots — all persisted under `.glassmarble/` and queryable through dedicated commands and the AI agent's AKG tools.

## 1. When It Runs

`gmb analyze` runs the intelligence pipeline after the GraphJSON commit, controlled by `--intelligence` (default `true`, human output only). The pipeline:

1. Computes global metrics (coupling, cohesion, layering compliance, cycles).
2. Infers logical components (Louvain community detection + directory-prefix analysis — no LLM).
3. Detects architecture patterns (PR-01..PR-07) and smells.
4. Generates typed architecture events from the graph diff vs the previous state.
5. Persists `.glassmarble/intelligence/latest.json`, writes snapshots to `.glassmarble/snapshots/`, and folds events into developer memory (`.glassmarble/memory/`).

These phases are **non-fatal**: failures warn, and the graph commit still succeeds. Re-analyzing the same tree is idempotent — events are never duplicated.

`--include-docs` additionally runs knowledge fusion (ADR/README/PR claims into memory), and the convention-learning layer refreshes `.glassmarble/memory/conventions.json`.

## 2. Architecture Events

Every structural change between commits becomes an `ArchEvent` (deterministic sha256 `id`, commit hash, timestamp, affected node IDs, components, evidence bundle, optional intent). The event kind vocabulary (`internal/archmodel/model.go`):

| Kind | Meaning |
|---|---|
| `SERVICE_ADDED` / `SERVICE_REMOVED` | A logical service appeared / disappeared |
| `SERVICE_SPLIT` / `SERVICE_MERGED` | Service boundaries reorganized |
| `DEPENDENCY_ADDED` / `DEPENDENCY_REMOVED` | Component-level dependency changed |
| `PATTERN_DETECTED` / `PATTERN_REMOVED` | Architecture pattern recognized / lost |
| `SMELL_DETECTED` / `SMELL_RESOLVED` | Anti-pattern appeared / resolved |
| `BOUNDARY_CREATED` | New component boundary inferred |
| `ASYNC_PATTERN_INTRODUCED` | Concurrency fork detected |
| `CACHING_ADDED` | Caching layer introduced |
| `SECURITY_LAYER_ADDED` | Auth/security gate detected |
| `API_ENDPOINT_ADDED` | New external-facing entrypoint |
| `DATA_STORE_ADDED` | New storage dependency |
| `COUPLING_INCREASED` / `COUPLING_DECREASED` | Coupling delta beyond `coupling_change_pct` (20%) |
| `DEAD_CODE_DETECTED` | Unreachable symbols found |
| `CYCLE_INTRODUCED` / `CYCLE_RESOLVED` | Cyclic dependency appeared / resolved |
| `LAYER_VIOLATION` | Cross-layer dependency against configured layering |
| `STATE_CHANGE` | Knowledge-state transition performed by aging (e.g. `CURRENT → DEPRECATED`), carried machine-readably in a `state=<STATE>` tag |

Events are appended to `.glassmarble/memory/events.jsonl` so memory rebuilds reproduce aging states exactly.

## 3. Knowledge States & Claims

### 3.1 Component knowledge states

Every component carries a temporal knowledge state: `CURRENT`, `EXPERIMENTAL`, `DEPRECATED`, `REMOVED`, `HISTORICAL`, `UNKNOWN`. Aging transitions these states with configurable half-lives and a stale-grace period (see `aging:` in `docs/configuration.md`).

### 3.2 Claim labels

Every memory claim is labelled by how it was established — reasons are never invented:

| Label | Meaning |
|---|---|
| `FACT` | Observed directly from the graph diff |
| `EXPLICIT_REASON` | Stated by a human in a commit/PR/issue/documentation |
| `INFERENCE` | Derived by GlassMarble's heuristics |
| `SPECULATION` | Low-confidence guess |

### 3.3 Corrections (human feedback loop)

`gmb memory --correct <target> --kind <kind> --value <value> [--reason <text>] [--author <name>]` records a correction; the original value is captured automatically and appended to `.glassmarble/memory/corrections.jsonl`.

| Kind | Effect |
|---|---|
| `INTENT` | Overrides the WHY explanation of an event/claim |
| `LABEL` | Overrides a displayed name |
| `STATE` | Overrides a knowledge state (must be a valid state value) |
| `CONFIDENCE` | Overrides a displayed confidence score |
| `REJECT` | Rejects an inference (stays visible, flagged; no value) |
| `ACCEPT` | Confirms an inference (no value) |

Corrections are replayed as a deterministic overlay on every memory view (overview, `--ask`, `--component`, `--json`) in recording order; corrected entries are flagged in reports.

## 4. Patterns, Smells & Components

### 4.1 Pattern detectors (PR-01..PR-07)

`internal/arch_intelligence/pattern_detector.go` runs heuristic detectors over the AKG; each emits a name + confidence:

| ID | Pattern |
|---|---|
| PR-01 | Layered Architecture |
| PR-02 | Clean Architecture |
| PR-03 | Microservices |
| PR-04 | Bounded Context (DDD) |
| PR-05 | CQRS |
| PR-06 | Event-Driven |
| PR-07 | Repository Pattern |

### 4.2 Component inference

Logical components (`comp_<name>`) are inferred from Louvain community detection + directory prefixes, with kinds `SERVICE`, `MODULE`, `BOUNDED_CONTEXT`, `LAYER`, `FEATURE`, `EXTERNAL_DEPENDENCY`. Each carries afferent (`Ca`) and efferent (`Ce`) coupling; `Instability = Ce/(Ca+Ce)`. A component with `Instability > unstable_threshold` (0.8) is reported unstable.

### 4.3 Smells

`gmb patterns --smells` also runs smell detection (god objects, cycles, god packages, unstable components) using the `intelligence:` thresholds in `docs/configuration.md` §4.

## 5. CLI Surface

### `gmb patterns`

```
gmb patterns [--smells] [--json] [--dir <path>]
```

Human report:

```
=== Architecture Intelligence ===
Patterns:
  DDD          Bounded Context     confidence=0.80
Components: 62
  comp_orders   internal/orders 12 nodes
  ...
```

### `gmb timeline`

```
gmb timeline [--component <name>] [--from <iso|ref>] [--to <iso|ref>]
             [--format text|json|mermaid] [--full] [--dir <path>]
```

Default window: the last six months. Text rows look like:

```
[2026-08-13 12:32] Coupling Decreased: <component>
```

`--full` adds commit, kind, components, and tags; `--format mermaid` renders a mermaid timeline.

### `gmb snapshot`

```
gmb snapshot [--create] [--list] [--at <ref>] [--diff '<base> <head>']
             [--replay <ref>] [--diagram <type>] [--format mermaid|plantuml|dot]
             [--no-graph] [--json]
```

- `--create` runs intelligence at HEAD and stores a new snapshot (`snap_<hash8>.json` + `index.json`).
- `--at` shows the state at the nearest snapshot at or before a commit/ref.
- `--diff` shows the architectural diff between two refs.
- `--replay` restores the embedded graph at a ref and renders a diagram (default `dependency`).
- `--no-graph` skips embedding the full graph (smaller files; disables `--replay` and structural diffs).

### `gmb stats`

```
gmb stats [--last] [--bench] [--arch] [--dir <path>]
```

- `--last` (default `true`): telemetry spans for the last pipeline execution.
- `--bench`: pipeline benchmark gates and budget status (analyze ≤ 20s, commit ≤ 8s, full scan ≤ 12s, visualize ≤ 3s/2s, state ≤ 12MB, json state ≤ 8MB).
- `--arch`: architecture health — component coupling (Ca/Ce/Instability) from intelligence.

### `gmb why`

```
gmb why "<question>"
```

Grounded question answering: retrieves top-10 architectural evidence (claims, events, timeline) and asks the configured LLM to answer from that evidence only — no hallucination.

### `gmb memory`

Queries the developer memory (see `docs/commands_master_reference.md` §8.8 and §8.27): overview, `--ask`, `--component`, `--correct`, `--corrections`.

## 6. Output Artifacts

| Artifact | Path |
|---|---|
| Latest intelligence run | `.glassmarble/intelligence/latest.json` |
| Snapshots | `.glassmarble/snapshots/index.json`, `.glassmarble/snapshots/snap_<hash8>.json` |
| Memory events | `.glassmarble/memory/events.jsonl` |
| Memory claims | `.glassmarble/memory/claims.jsonl` |
| Correction audit trail | `.glassmarble/memory/corrections.jsonl` |
| Learned conventions | `.glassmarble/memory/conventions.json` |

## 7. AI Agent Integration

The intelligence layer is exposed to the AI architect through three AKG tools: `query_architecture_memory`, `get_architecture_timeline`, `get_architecture_patterns` — alongside 18 `akg_*` graph tools and the metrics/pattern tools `akg_run_architecture_analysis`, `akg_detect_architecture_patterns`, `akg_get_commit_reasoning`, `akg_run_timeline_analysis`.

## 8. Configuration

All thresholds are configurable under `intelligence:`, `fusion:`, `learning:`, and `aging:` in `.glassmarble/config.yaml` — see `docs/configuration.md` for the complete key/default reference.