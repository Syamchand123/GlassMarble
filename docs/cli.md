# GlassMarble CLI Master Reference (`gmb`)

`gmb` is the command-line interface for the GlassMarble Architectural Knowledge Graph (AKG) and Diagram Visualization Engine.

---

## Commands

### `gmb analyze`
Parses, normalizes, aggregates, and semantic-links source code files into the AKG graph database (`.glassmarble/akg.json`).

```bash
gmb analyze [--dir <path>] [--commit <hash>] [--full] [--workers <N>] [--link-level <level>] [--json] [--bench] [--intelligence]
```

**Flags:**
- `--dir`: Target repository directory to analyze (default: `.`).
- `--commit`: Git commit hash for delta diffing (default: working tree vs `HEAD`).
- `--full`: Force a full clean scan of all files at full linker detail (bypasses delta incremental fast path).
- `--workers`: Parallel worker count (default: CPUs, capped at 8).
- `--link-level`: Linker detail level (`architecture`, `standard`, `full`).
- `--store-code`: Opt-in source snippet storage in AKG nodes.
- `--json`: Emit machine-readable JSON summary.
- `--bench`: Run analysis benchmark suite and verify timings/file sizes against performance budget gates.
- `--intelligence`: Run architecture intelligence + developer memory after committing the graph (default `true`, human output only; both phases are non-fatal).

When `--intelligence` is enabled, `gmb analyze` also writes:
- `.glassmarble/intelligence/latest.json` — current Architecture Intelligence state,
- `.glassmarble/snapshots/` — point-in-time architecture snapshots (skip-written when the topology is unchanged),
- `.glassmarble/memory/` — the developer memory WALs (`events.jsonl`) and derived aggregates (`memory.json`, `timeline.json`).

Event ingestion is idempotent: re-analyzing the same commit never duplicates memory.

---

### `gmb memory`
Answers questions about the project's architectural history from the developer memory (`.glassmarble/memory/`). Deterministic retrieval — no LLM. Since convention learning, a learning overlay lets you correct wrong derived facts and replays them on every view.

```bash
gmb memory [--dir <path>] [--ask "<question>"] [--component <name>] [--json]
          [--correct <target> --kind STATE|INTENT|REASON --value <value>] [--corrections]
```

- Default: project overview — event count and current components with their temporal states.
- `--ask "what do we know about Redis?"`: ranked components, claims (labelled FACT / EXPLICIT_REASON / INFERENCE / SPECULATION), events and related timeline.
- `--component payment`: full history of the matching component (case-insensitive substring) plus its timeline.
- `--json`: machine-readable document.
- `--correct <target> --kind <k> --value <v>`: record a correction (target = component name, event ID, or claim ID). Original value is captured automatically and the audit entry is appended to `.glassmarble/memory/corrections.jsonl`.
- `--corrections`: show the correction audit trail.

Reasons are never invented: without evidence of a reason, no reason claim exists. Corrections are replayed in order on every view — deterministic, idempotent, and independent of aggregate rebuilds.

---

### `gmb visualize <diagram-type>`
Generates structural, behavioral, or specialized architecture diagrams for any of the 31 supported diagram types.

```bash
gmb visualize <type> [--dir <path>] [--format mermaid|plantuml|dot] [--scope global|package|file] [--entry <symbol>] [--depth <N>] [--unused] [--max-nodes <N>] [--save]
```

**Supported Diagram Types (31 Total):**
- **UML Family:** `class`, `sequence`, `component`, `object`, `package`, `activity`, `state`, `usecase`, `deployment`, `er`
- **C4 Family:** `c4-context`, `c4-container`, `c4-component`, `c4-code`
- **Specialized Family:** `architecture`, `callgraph`, `cfg`, `dfg`, `concurrency`, `dataflow`, `dependency`, `filedeps`, `boundary`, `security`, `memory`, `eventsourcing`, `rpc`, `constraints`, `di`, `type`

**Flags:**
- `--format`: Output markup format (`mermaid` [default], `plantuml`, `dot`).
- `--scope`: Graph projection boundary (`global` [default], `package`, `file`).
- `--entry`: Entrypoint function/class for sequence and execution-rooted diagrams.
- `--depth`: Call-graph / traversal depth (default: `3`).
- `--unused`: Include unreferenced / standalone nodes in the diagram.
- `--max-nodes`: Node count limit before truncation with `[+N external]` boundary ports.
- `--save`: Save generated markup to `.glassmarble/marbles/<type>.<format>`.

---

### `gmb inspect`
Queries, lists, or details nodes, edges, and symbol metadata inside the AKG.

```bash
gmb inspect --node <id> | --list | --languages | --edges <id>
```

---

### `gmb stats`
Displays pipeline execution telemetry spans and benchmark budget status.

```bash
gmb stats [--last] [--bench]
```

---

### `gmb dev rebase-goldens`
Developer utility command to update golden diagram fixtures across all 31 diagram types and supported output formats.

```bash
gmb dev rebase-goldens [--dir <path>] [--golden-dir <path>]
```

---

## Exit Codes

| Exit Code | Constant | Meaning |
|---|---|---|
| `0` | `Success` | Operation completed cleanly |
| `1` | `ErrValidation` | Invalid flag, path, or missing required parameter |
| `2` | `ErrParseFailed` | Syntax tree or parser error |
| `3` | `ErrNotFound` | Target node, symbol, or entrypoint not found |
| `4` | `ErrRenderLimit` | `--max-nodes` truncation limit reached or benchmark budget exceeded |
| `5` | `ErrInternal` | Internal AKG or engine error |
