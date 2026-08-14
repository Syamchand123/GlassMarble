# GlassMarble CLI Master Reference (`gmb`)

`gmb` is the command-line interface for the GlassMarble Architectural
Knowledge Graph (AKG) and Diagram Visualization Engine. The complete command,
flag, and exit-code reference is `docs/commands_master_reference.md`; this
file is the condensed quick reference.

---

## Commands (28 total)

### Build & maintain
- `gmb init [--dir <path>]` — create the `.glassmarble` workspace
  (`marbles/`, `config.yaml`, empty `akg.json`, `.gitignore` entry).
- `gmb analyze [flags]` — run the 4-phase ingestion pipeline
  (ingestion → normalization → aggregation → linking) and commit the
  resulting CPG to `.glassmarble/akg.json` (GraphJSON, schema v3).
- `gmb watch [flags]` — continuously watch the repo (fsnotify + git
  fingerprint) and re-analyze on change.
- `gmb hooks install|uninstall [--dir <path>]` — manage the git
  post-commit hook that runs `gmb analyze` after every commit.
- `gmb import <graph.json> [--dir <path>]` — replace the active AKG snapshot
  from a GraphJSON document (rejects dangling references).
- `gmb export [--format graphjson|neo4j] -o <file>` — export the snapshot to
  GraphJSON (default) or a Neo4j Cypher script.

### Query
- `gmb status [--json]` — AKG health, node statistics, graph versions
  (missing DB = exit 0, "uninitialized").
- `gmb doctor` — integrity diagnostics: parse-back, duplicate node IDs,
  dangling references.
- `gmb diff` — show the persisted AKG state and its committed transaction
  version (commit hash, schema version, graph version).
- `gmb tree [--depth <n>]` — architectural directory & symbol hierarchy.
- `gmb dependency [target] [--json]` — inbound/outbound dependency analysis.
- `gmb hotspot [--top <n>] [--json]` — in-degree ranking of hotspots.
- `gmb inspect [node_id] [--list|--search <q>|--file F --line L|--languages]`
  — node detail, symbol search, entry-point discovery, language matrix.
- `gmb patterns [--smells] [--json]` — component inference and pattern
  detection (PR-01..PR-07), optional smell detection.
- `gmb snapshot --create|--list|--at <ref>|--diff <base> <head>|--replay`
  — point-in-time architecture snapshots with graph replay.
- `gmb timeline [--component <n>] [--from <ref>] [--to <ref>] [--format
  text|json|mermaid] [--full]` — architecture evolution timeline.
- `gmb stats [--arch] [--bench] [--last]` — pipeline telemetry,
  architecture health (Ca/Ce/Instability), benchmark budget status.

### Govern
- `gmb drift [--json]` — architecture drift vs declared layering and cycle
  budgets (fails CI when violations exist).
- `gmb compare [base.json head.json] | --dir <path> [--json]` — diff two AKG
  snapshots; `--dir` compares committed AKG vs a fresh working-tree analysis.

### Visualize & reason
- `gmb visualize <diagram_type> [flags]` — generate architecture diagrams:
  **31 types** (14 UML + 7 C4 + 4 Specialized + 6 Analysis) in `mermaid`,
  `plantuml`, or `dot`. Subcommands: `list`, `check <type>`.
- `gmb why <question>` — grounded architecture Q&A via the AI engine.
- `gmb ai [question]` — the AI Architect agent; subcommands `chat`,
  `configure`, `models`, `doctor`, `sessions`.

### Utility
- `gmb housekeeping [--prune] [--older-than <days>]` — report and prune
  marbles/AI artifacts (never touches `akg.json`).
- `gmb completion bash|zsh|fish|powershell` — shell completion scripts.
- `gmb version` / `gmb --version` — version (v0.1.0).
- `gmb dev rebase-goldens` — developer utility to regenerate golden diagram
  fixtures.

---

## `gmb analyze`

```bash
gmb analyze [--dir <path>] [--commit <hash>] [--full] [--workers <N>]
            [--link-level <level>] [--macro-inference <mode>] [--max-nodes <N>]
            [--abort-on-limit] [--store-code] [--include-docs] [--intelligence]
            [--bench] [--json] [--verbose]
```

**Flags:**
- `--dir`: Target repository directory (default `.`).
- `--commit`: Git commit hash for delta diffing. Empty (default) diffs the
  working tree against `HEAD`; a hash diffs that commit against its parent.
- `--full`: Force a full clean scan at full linker detail.
- `--workers`: Parallel worker count (default: CPUs).
- `--link-level`: `architecture` (default), `standard`, `full`.
- `--macro-inference`: `disabled`, `structural`, `all` (default).
- `--max-nodes` / `--abort-on-limit`: node budget (warn or hard-stop).
- `--store-code`: store source snippets in AKG nodes (opt-in).
- `--include-docs`: run knowledge fusion (ADR/README/git-history claims into
  developer memory).
- `--intelligence`: run architecture intelligence + developer memory after
  committing (default `true`, human output only; non-fatal).
- `--bench`: enforce the performance budget gates (analyze ≤ 20 s, commit ≤
  8 s, state ≤ 12 MB) — exit 4 on failure.
- `--json`: machine-readable JSON summary.

Delta vs full: no state → full scan; empty graph from `init` → full scan;
clean tree → full scan; dirty tree → delta; `--full` → full; `--commit <h>`
→ delta of that commit.

---

## `gmb memory`

Query the developer memory — what the system was, what changed, and
(where evidence exists) why. Deterministic retrieval, no LLM.

```bash
gmb memory [--dir <path>] [--ask "<question>"] [--component <name>] [--json]
          [--correct <target> --kind <kind> --value <value>] [--corrections]
```

- Default: project overview — event count and current components with
  temporal states (CURRENT / REMOVED / DEPRECATED / EXPERIMENTAL /
  HISTORICAL / UNKNOWN).
- `--ask "what do we know about Redis?"`: ranked components, claims
  (labelled FACT / EXPLICIT_REASON / INFERENCE / SPECULATION), events and
  related timeline.
- `--component <name>`: full history of the matching component
  (case-insensitive substring) plus its timeline.
- `--correct <target> --kind <k> --value <v>`: record a learning correction
  (target = component name, event ID, or claim ID). Kinds: `INTENT`
  (default), `LABEL`, `STATE`, `CONFIDENCE`, `REJECT`, `ACCEPT`.
- `--corrections`: show the correction audit trail.

Reasons are never invented: without evidence of a reason, no reason claim
exists. Corrections are appended to `.glassmarble/memory/corrections.jsonl`
and replayed in order on every view — deterministic, idempotent, and
independent of aggregate rebuilds.

---

## `gmb visualize <diagram_type>`

Generates any of the **31 supported diagram types** (see
`docs/diagrams.md` for the full catalog).

```bash
gmb visualize <type> [--dir <path>] [--format mermaid|plantuml|dot]
  [--scope global|folder:<path>|file:<path>] [--entry <symbol>]
  [--depth <N>] [--unused] [--max-nodes <N>] [--link-level <level>]
  [--summary] [--pagerank] [--community] [--scc] [--save <name>]
  [--output <file>] [--render <x.svg|png>]
```

- `sequence` requires `--entry`.
- `--save <name>` writes a fenced markdown file to
  `.glassmarble/marbles/<name>.md`.
- `--render` uses Kroki, falling back to local mermaid-cli; markup saved as
  `.txt` if both fail.

---

## `gmb inspect`

```bash
gmb inspect --list | --search <q> | <node_id> | --file <f> --line <n> | --languages
```

- `--list`: candidate entry points (FUNCTION/METHOD) for sequence diagrams.
- `--languages`: the 14-language support matrix report.
- `--type`: filter by `FUNCTION`, `METHOD`, `STRUCT`, `CLASS`, `INTERFACE`.

---

## `gmb stats`

```bash
gmb stats [--arch] [--bench] [--last]
```

- `--arch`: architecture health — component coupling (Ca/Ce/Instability)
  with STABLE/UNSTABLE status from architecture intelligence.
- `--bench`: benchmark gates and budget status.
- `--last` (default true): telemetry spans for the last pipeline execution.

---

## Global Flags

| Flag | Type | Description |
|---|---|---|
| `--root-dir` | string | Root directory for analysis |
| `--debug` | bool | Debug logging |
| `-c, --config` | string | Config file (default `$HOME/.glassmarble.yaml`) |
| `-v, --verbose` | bool | Verbose output |
| `--max-json-mb` | int | Refuse to load/commit `akg.json` larger than N MiB (0 = unlimited) |

---

## Exit Codes

| Exit | Meaning |
|---|---|
| `0` | Success (incl. `status`/`doctor` with a missing database — a state, not an error) |
| `1` | Validation error or any other unclassified failure |
| `2` | Entry point missing or not found (e.g. `visualize sequence` without `--entry`) |
| `3` | Empty subgraph (diagram would contain no nodes) |
| `4` | Render/node limit exceeded, or benchmark budget exceeded |