# GlassMarble CLI — Master Command Reference

> **Source of truth** for the `gmb` command-line interface.
> Covers every command, subcommand, flag, execution flow, use case, and best practice in the GlassMarble product (v0.1.0).

---

## Table of Contents

1. [Product Overview](#1-product-overview)
2. [Installation & Building](#2-installation--building)
3. [Command Hierarchy](#3-command-hierarchy)
4. [Global Behavior & Persistent Flags](#4-global-behavior--persistent-flags)
5. [The `.glassmarble` Workspace](#5-the-glassmarble-workspace)
6. [Configuration System](#6-configuration-system)
7. [Interactive TUI vs Plain Output](#7-interactive-tui-vs-plain-output)
8. [Command Reference](#8-command-reference)
   - [init](#81-gmb-init)
   - [analyze](#82-gmb-analyze)
   - [watch](#83-gmb-watch)
   - [status](#84-gmb-status)
   - [doctor](#85-gmb-doctor)
   - [diff](#86-gmb-diff)
   - [tree](#87-gmb-tree)
   - [dependency](#88-gmb-dependency)
   - [hotspot](#89-gmb-hotspot)
   - [inspect](#810-gmb-inspect)
   - [drift](#811-gmb-drift)
   - [visualize](#812-gmb-visualize)
   - [compare](#813-gmb-compare)
   - [export](#814-gmb-export)
   - [import](#815-gmb-import)
   - [hooks](#816-gmb-hooks)
   - [housekeeping](#817-gmb-housekeeping)
   - [memory](#821-gmb-memory)
   - [completion](#818-gmb-completion)
   - [version](#819-gmb-version)
   - [ai](#820-gmb-ai-and-subcommands)
9. [Analysis Pipeline Execution Flow](#9-analysis-pipeline-execution-flow)
10. [Use Cases & Command Workflows](#10-use-cases--command-workflows)
11. [Best Practices](#11-best-practices)
12. [Exit Codes & Error Semantics](#12-exit-codes--error-semantics)
13. [Troubleshooting](#13-troubleshooting)

---

## 1. Product Overview

GlassMarble builds and maintains a self-evolving **Architecture Knowledge Graph (AKG)** of a software repository. The CLI (`gmb`) ingests source code with tree-sitter grammars, normalizes it into a graph of symbols and dependencies, and persists the result as RDF Turtle (`.ttl`) under `.glassmarble/`.

The CLI is organized around four capability areas:

| Area | Commands |
|---|---|
| **Build** (ingest & maintain the graph) | `init`, `analyze`, `watch`, `hooks`, `import`, `export` |
| **Query** (read the graph) | `status`, `tree`, `dependency`, `hotspot`, `inspect`, `diff`, `doctor` |
| **Govern** (enforce architecture) | `drift`, `compare` |
| **Visualize & Reason** | `visualize`, `ai` |
| **Utility** | `housekeeping`, `completion`, `version` |

Supported languages (tree-sitter grammars): Go, C, C++, C#, Python, Java, JavaScript, TypeScript, HTML, CSS, JSON, Ruby, PHP, Rust. See `docs/supported_languages.txt`.

---

## 2. Installation & Building

GlassMarble is a Go application. Build the binary from the repository root:

```bash
go build -o gmb.exe main.go        # Windows
go build -o gmb main.go            # macOS / Linux
```

For shell completion and `hooks install` to work smoothly, put the binary on `PATH` (optional; `hooks` embeds the absolute binary path, so it works even off `PATH`).

Verify installation:

```bash
gmb version
gmb --version          # Fang-styled version output
gmb --help             # root help
```

---

## 3. Command Hierarchy

```
gmb
├── init                          Initialize a repository workspace
├── analyze                       Run the 4-stage ingestion pipeline (full or delta)
├── watch                         Continuously watch the repo and re-analyze on change
├── status                        AKG database status, stats, and health
├── doctor                        Integrity diagnostics on the AKG database
├── diff                          Architectural diff across WAL transactions
├── tree                          Architectural directory & symbol hierarchy tree
├── dependency [target]           Inbound/outbound dependency analysis
├── hotspot                       High-coupling hotspots (in-degree ranking)
├── inspect [node_id]             Node detail, symbol search, entry-point discovery
├── drift                         Architecture drift vs declared layers/budgets
├── visualize <diagram_type>      Generate Mermaid/PlantUML/DOT diagrams (28 types)
├── compare [base.json head.json] Diff two AKG snapshots (CI-friendly)
├── export                        Export snapshot to GraphJSON or Turtle
├── import <graph.json>           Replace snapshot from a GraphJSON document
├── hooks <install|uninstall>     Manage the git post-commit auto-analyze hook
├── housekeeping                  Report & prune .glassmarble working-set storage
├── memory                        Query the developer memory (what changed, and why)
├── completion <shell>            Generate shell completion scripts
├── version                       Print version
└── ai [question]                 Ask the AI Architect about the codebase
    ├── chat                      Interactive multi-turn conversation
    ├── configure                 Configure AI provider/model/key (BYOK)
    ├── models                    List supported providers & models
    ├── doctor                    Diagnose the AI engine setup
    └── sessions                  List / delete saved chat sessions
```

Cobra also provides a built-in `help [command]` command (styled by Fang):
`gmb help analyze`, `gmb help ai chat`, `gmb analyze --help`.

---

## 4. Global Behavior & Persistent Flags

The root command `gmb` wraps all output with **Fang** (styled help, errors, and version). Command errors are reported once, cleanly; `SilenceUsage`/`SilenceErrors` are enabled at the root.

### Persistent root flags (valid on every command)

| Flag | Type | Default | Description |
|---|---|---|---|
| `--root-dir` | string | `""` | Root directory for analysis (used by AI commands and config plumbing) |
| `--debug` | bool | `false` | Enable debug logging |
| `-c, --config` | string | `""` | Config file (default is `$HOME/.glassmarble.yaml`) |
| `-v, --verbose` | bool | `false` | Verbose output |
| `--max-ttl-mb` | int | `0` | Refuse to load or commit an AKG state file larger than this many MiB (`0` = unlimited). A hard bloat guard for the graph database. |

### Common flags shared by most commands

| Flag | Type | Default | Meaning |
|---|---|---|---|
| `--dir` | string | `.` | Directory containing the `.glassmarble/` folder (the repo root). All commands resolve `--dir/.glassmarble` for the database. |
| `--json` | bool | `false` | Emit machine-readable JSON instead of the human report. When set, the command **bypasses the interactive TUI layer entirely** — safe for CI/scripts. |

> **Note on `--dir`:** it is the **repo root** that contains `.glassmarble/`, not the `.glassmarble` folder itself. Example: `gmb status --dir C:\projects\myrepo`.

---

## 5. The `.glassmarble` Workspace

Created by `gmb init` (or on demand). Lives at `<repo>/.glassmarble/` and is auto-added to `.gitignore`.

```
.glassmarble/
├── akg.json                     # The AKG graph — canonical GraphJSON (source of truth)
├── config.yaml                  # Project config (root_dir, debug, output_format, max_file_bytes, drift:)
├── wal/
│   └── akg_transactions.wal     # Write-ahead log (transaction replay; truncated after each successful commit)
├── marbles/                     # Saved diagram markdown files
├── intelligence/
│   └── latest.json              # Stage 5 current architectural state (written by `gmb analyze --stage5`)
├── snapshots/
│   ├── index.json               # Snapshot index (commit → file)
│   └── snap_<hash8>.json        # Point-in-time ArchSnapshot (skip-written when topology unchanged)
├── memory/
│   ├── events.jsonl             # Append-only event WAL (source of truth for Stage 6)
│   ├── claims.jsonl             # Append-only claim WAL (source of truth for Stage 6)
│   ├── memory.json              # Derived DeveloperMemory aggregate (rebuildable from the WALs)
│   └── timeline.json            # Derived timeline (fast path for queries)
├── ai/
│   ├── ai.yaml                  # Project-scoped AI configuration
│   ├── sessions/                # Saved chat sessions
│   └── *.md                     # Saved AI answers
└── db.lock                      # Transient transaction lock (auto-reclaimed after 30s staleness)
```

| Artifact | Purpose | Guarded by |
|---|---|---|
| `akg.json` | The graph itself. Committed atomically with a `.tmp-*` rename. | post-write verification (byte parity + zero-dangling guard), `--max-ttl-mb` budget |
| `wal/akg_transactions.wal` | Crash-safety: every transaction is appended, then truncated after a successful commit | `gmb doctor` staleness check, `housekeeping --prune` |
| `db.lock` | Mutual exclusion for transactions (30s staleness timeout) | `AcquireLock`/`ReleaseLock` |
| `marbles/` | Saved diagrams from `visualize --save` and AI artifacts | `housekeeping --prune` |
| `intelligence/latest.json` | Stage 5 output (current architecture state) | atomic temp+rename write |
| `snapshots/` | Point-in-time architecture snapshots for event diffing | unchanged-topology skip-write |
| `memory/` | Stage 6 developer memory: WALs (source of truth) + derived aggregates | corrupt-line tolerance, atomic rebuild |
| `ai/` | AI answers, project AI config, chat sessions | `housekeeping --prune`, `ai sessions --delete` |

---

## 6. Configuration System

### 6.1 Core pipeline config (non-AI)

Precedence (**highest wins**):

```
CLI flags > GLASSMARBLE_* environment variables > .glassmarble/config.yaml > ~/.glassmarble/config.yaml > defaults
```

Defaults:

| Key | Default | Env var | Notes |
|---|---|---|---|
| `root_dir` | `.` | `GLASSMARBLE_ROOT_DIR` | |
| `worker_count` | `4` | `GLASSMARBLE_WORKER_COUNT` | Parallel parser workers (flags win) |
| `max_file_bytes` | `10MB` (10485760) | `GLASSMARBLE_MAX_FILE_BYTES` | Files larger than this are skipped at ingestion |
| `debug` | `false` | `GLASSMARBLE_DEBUG` | |
| `storage_dir` | `.glassmarble` | `GLASSMARBLE_STORAGE_DIR` | |
| `output_format` | `mermaid` | `GLASSMARBLE_OUTPUT_FORMAT` | `mermaid`, `plantuml`, `dot` |
| `include_hidden` | `false` | `GLASSMARBLE_INCLUDE_HIDDEN` | |
| `drift` | (see below) | — | Read by `gmb drift` |

Example `.glassmarble/config.yaml` (as written by `init`):

```yaml
root_dir: .
debug: false
output_format: mermaid
max_file_bytes: 2097152
```

### 6.2 Drift configuration (read by `gmb drift`)

```yaml
drift:
  layers:
    - name: presentation
      paths: ["cmd/web/**"]
    - name: domain
      paths: ["internal/domain/**"]
    - name: infrastructure
      paths: ["internal/infra/**"]
  forbidden_deps:
    - source: presentation
      target: domain
      reason: presentation must not depend on domain internals
  cycle_budget: 3
```

Rules:
- **Layers** bucket nodes by path glob. A node is assigned to the **first** matching layer. `**` also matches the prefix directory and everything below it.
- **Forbidden deps** are directed `source → target` layer pairs; every graph edge crossing them is a violation.
- **Cycle budget** is the maximum tolerated number of cycles between layers; `0` or negative means "any cycle fails".
- When no `drift:` section exists, drift checks **only cycles** (unbounded layers = no forbidden-dep checks).

### 6.3 AI configuration (BYOK)

Precedence (**highest wins**):

```
CLI flags > GLASSMARBLE_AI_* environment variables > .glassmarble/ai.yaml (project) > ~/.glassmarble/ai.yaml (global) > defaults
```

Defaults: provider `openai`, model `gpt-4o`, temperature `0.2`, max turns `15`, max tool result bytes `8192`, max output tokens `8192`, timeout `180s`, streaming on, max session messages `40`.

Config file keys (`ai.yaml`): `provider`, `model`, `api_key`, `base_url`, `temperature`, `max_turns`, `max_tool_result_bytes`, `max_output_tokens`, `timeout_sec`, `stream`, `max_total_tokens`, `max_cost_usd`, `max_session_messages`.

Environment variables:

| Variable | Meaning |
|---|---|
| `GLASSMARBLE_AI_PROVIDER` | Provider name |
| `GLASSMARBLE_AI_MODEL` | Model identifier |
| `GLASSMARBLE_AI_API_KEY` | Generic API key (fallback for all providers) |
| `GLASSMARBLE_<PROVIDER>_API_KEY` | Provider-specific key (e.g. `GLASSMARBLE_OPENAI_API_KEY`, `GLASSMARBLE_GEMINI_API_KEY`) — wins over generic |
| `GLASSMARBLE_AI_BASE_URL` | Override provider endpoint |
| `GLASSMARBLE_AI_TEMPERATURE` | Sampling temperature |
| `GLASSMARBLE_AI_MAX_TURNS` | Tool-call turn cap |
| `GLASSMARBLE_AI_MAX_TOOL_RESULT_BYTES` | Tool result truncation |
| `GLASSMARBLE_AI_MAX_OUTPUT_TOKENS` | Max completion tokens |
| `GLASSMARBLE_AI_TIMEOUT_SEC` | HTTP timeout |
| `GLASSMARBLE_AI_STREAM` | `1`/`true` enable streaming, `0`/`false` disable |
| `GLASSMARBLE_AI_MAX_TOTAL_TOKENS` | Per-run prompt+completion token cap |
| `GLASSMARBLE_AI_MAX_COST` | Per-run cost cap (USD) |
| `GLASSMARBLE_AI_MAX_SESSION_MESSAGES` | Chat history budget |

Files are written with `0600` permissions; keys are never logged (masked as `sk-...****`).

---

## 7. Interactive TUI vs Plain Output

GlassMarble has a Bubble Tea interactive layer. Commands decide between TUI and plain mode by calling `tui.IsInteractive(stdin, stdout)`:

| Condition | Behavior |
|---|---|
| stdin **and** stdout are terminals | Interactive TUI where available (`analyze`, `watch`, `tree`, `inspect`, `visualize`, `ai`, `ai chat`, `ai sessions`, `housekeeping --prune` confirm) |
| stdout is piped / non-TTY (CI, scripts, `| less`) | Plain text output — the command prints the report and exits |
| `--json` is set | Always machine-readable JSON, **never** interactive |

Design intent: TUI for humans, plain/JSON for automation. The plain output is byte-stable for tests and CI.

---

## 8. Command Reference

---

### 8.1 `gmb init`

**Purpose:** Create the `.glassmarble` workspace and configuration for a repository.

**Syntax:** `gmb init [--dir <path>]`

**Flags:**

| Flag | Type | Default | Description |
|---|---|---|---|
| `--dir` | string | `.` | Target repository directory |

**What it creates (idempotent — existing files are never overwritten):**

1. `.glassmarble/` directory
2. `.glassmarble/marbles/` directory
3. `.glassmarble/config.yaml` (default values) — only if missing
4. `.glassmarble/akg.json` (empty but valid GraphJSON) — only if missing
5. Appends `.glassmarble` to `.gitignore` (creates `.gitignore` if needed)

**Execution flow:**
```
resolve abs path
  → mkdir .glassmarble, .glassmarble/marbles
  → write config.yaml (if absent)
  → write empty akg.json state file (if absent)
  → update/create .gitignore
  → print styled success card (badge)
```

**Use cases:**
- First run in a new repository.
- CI setup before `analyze`.
- *Note:* `init` alone produces an **empty** graph — run `gmb analyze` afterwards. The delta path is skipped until the state file is non-empty, so the first `analyze` always does a full scan.

**Example:**
```bash
gmb init
gmb init --dir /path/to/repo
```

---

### 8.2 `gmb analyze`

**Purpose:** Run the full 4-stage ingestion pipeline and commit the result to the AKG. The workhorse of GlassMarble.

**Syntax:** `gmb analyze [--dir <path>] [--full] [--workers <n>] [--commit <hash>] [--link-level <level>] [--macro-inference <mode>] [--max-nodes <n>] [--abort-on-limit] [--verbose] [--json]`

**Flags:**

| Flag | Type | Default | Description |
|---|---|---|---|
| `--dir` | string | `.` | Repository root to analyze |
| `--commit` | string | `""` | Git commit hash to tag the analysis. **Empty (default)** diffs the **working tree** against HEAD (incremental delta). A hash diffs **that commit against its parent**. |
| `--full` | bool | `false` | Force a clean full scan of every file **at full linker detail**. Default is incremental delta. |
| `--workers` | int | `0` (CPUs) | Parallel parser workers |
| `--link-level` | string | `architecture` | Linker detail: `architecture` (module/type/call/dep edges; CFG/DFG disabled), `standard` (aggregate CFG), `full` (per-branch CFG+DFG + heuristic passes) |
| `--macro-inference` | string | `all` | `disabled` (no macro inference), `structural` (rules with evidence only), `all` (full heuristic + structural) |
| `--max-nodes` | int | `0` | Max total CPG nodes before warning/abort (`0` = unlimited) |
| `--abort-on-limit` | bool | `false` | Abort analysis if `--max-nodes` is exceeded (otherwise warn) |
| `--verbose` | bool | `false` | Stage-by-stage progress on stdout |
| `--json` | bool | `false` | Machine-readable JSON result |

**Execution flow (incremental delta, default):**

```
resolve abs dir
  → config load (workers, max_file_bytes merged)
  → GitTrackedOnly = true when .git exists
  → if NOT --full:
        hasBaseState = (akg.json exists AND contains nodes)   # empty graph from init does NOT count
        diff = CollectGitDiff(dir, commitHash)                    # ""  → git diff HEAD (working tree)
                                                                  # hash → git diff-tree <hash> (commit vs parent)
        if hasBaseState && diffErr == nil && len(diff) > 0:
            Stage 1 delta: parse only changed files, emit deletes
        else:
            Stage 1 full scan (all files)
  → Stage 2: GAST normalization of every parsed tree
  → Stage 3: topology aggregation → global definition index
  → open AKG transaction manager (.glassmarble)
  → Stage 4: CPG linking (delta mode against base graph)
  → ExecuteDeltaTransaction(cpg, modifiedFiles)   # atomic GraphJSON commit, WAL truncation
  → quality report on the MERGED graph (nodes/edges/virtual/dangling)
  → report skipped files & ingestion warnings
```

**Output (plain):**
```
Starting GlassMarble Analysis on C:\repo...
Stage 1 (delta): parsed 3 changed files, 0 deleted.
Stage 2: Normalized 3 syntax trees.
Stage 3: Built topology with 42 global definition symbols.
Linker level: architecture (CFG/DFG disabled)
Stage 4: Bound Delta CPG with 90 new/modified nodes.
Analyzed 3 files | 120 nodes | 240 edges | 18 virtual | 0 dangling | state=180.0KB wal=0B | 0.4s
WARNING: 1 file(s) skipped during ingestion (oversized or unsupported language):
  - assets/logo.png (exceeds MaxFileBytes)
Note: 2 ingestion warning(s):
  - internal/x.go (untracked by git)
```

**JSON output fields** (`--json`): `target_dir`, `commit_hash`, `files_analyzed`, `nodes`, `edges`, `virtual_nodes`, `dangling_edges`, `state_bytes`, `wal_bytes`, `duration_ms`, `skipped[]`, `warnings[]`, `storage_dir`.

**Delta vs full — decision table:**

| Situation | Path taken |
|---|---|
| No `.glassmarble/akg.json` | **Full scan** (nothing to diff against) |
| `init` created empty graph | **Full scan** (empty document is not a base) |
| TTL non-empty, working tree clean | **Full scan** (empty diff) |
| TTL non-empty, working tree dirty | **Delta** — only changed files re-parsed, merged into base |
| `--full` | **Full scan** of every file at `full` linker detail |
| `--commit <hash>` | **Delta of that commit** against its parent |

> ⚠️ **Historical bug (fixed):** `--commit` used to default to `"HEAD"`, causing `git diff-tree HEAD` (files changed *in the last commit*) instead of a working-tree diff. A bare `gmb analyze` could ingest only the last commit's files. The default is now `""`. Regression tests cover this (`cmd/analyze_delta_regression_test.go`).

**Use cases:**
- First full build: `gmb analyze` (no flags needed — full scan happens automatically).
- Daily incremental: `gmb analyze` after editing code.
- Complete rebuild at deep linker detail: `gmb analyze --full` (slow; forces per-branch CFG+DFG).
- CI smoke: `gmb analyze --json` and parse `files_analyzed`/`dangling_edges`.

**Examples:**
```bash
gmb analyze
gmb analyze --dir . --verbose
gmb analyze --full
gmb analyze --link-level standard --workers 8
gmb analyze --max-nodes 20000 --abort-on-limit
gmb analyze --json > analysis.json
gmb analyze --commit 3f2a1b4      # analyze only what that commit changed
```

---

### 8.3 `gmb watch`

**Purpose:** Continuously watch the repository and automatically re-analyze when source files change.

**Syntax:** `gmb watch [--dir <path>] [--interval <dur>] [--commit <hash>] [--workers <n>] [--link-level <level>] [--macro-inference <mode>] [--max-nodes <n>] [--abort-on-limit] [--verbose]`

**Flags:**

| Flag | Type | Default | Description |
|---|---|---|---|
| `--dir` | string | `.` | Target repository directory |
| `--commit` | string | `""` | Tag hash (same semantics as `analyze`) |
| `--workers` | int | `0` | Parser workers |
| `--link-level` | string | `architecture` | Linker detail level |
| `--macro-inference` | string | `all` | Macro inference mode |
| `--max-nodes` | int | `0` | Node budget |
| `--abort-on-limit` | bool | `false` | Abort on budget exceed |
| `--verbose` | bool | `false` | Verbose stage output |
| `--interval` | duration | `500ms` | Debounce interval for file-system events |

**Requirements:** must be a git repository (`watch requires a git repository`).

**Execution flow:**

```
verify .git exists
  → initial analysis (full scan on first run, delta afterwards)
  → register recursive fsnotify watches (skips .git, .glassmarble, node_modules, vendor, dist, build, target, bin, obj, out, coverage, hidden dirs)
  → loop:
        fs event → filter relevance (source-like paths; ignores .ttl/.wal/.lock + ignored segments)
                  → new dirs get registered on the fly
        debounce window → git working-tree fingerprint check (HEAD hash + git status --porcelain)
                  → if fingerprint changed → runAnalysis (delta)
        Ctrl+C / context cancel → stop
```

Two layers of change detection: **fsnotify** (fast, local events) and the **git fingerprint** (catches branch switches, rebases, external edits).

**Plain output:**
```
GlassMarble Watcher active on 'C:\repo' (fsnotify, debounce: 500ms)
Press Ctrl+C to stop.

[14:02:11] Initial analysis...
[14:03:47] Repository changes detected, running analysis...
```

**Use cases:**
- Live graph while refactoring.
- Keeping the AKG warm for AI queries and diagram generation.

---

### 8.4 `gmb status`

**Purpose:** Display AKG database status, node statistics, and graph health.

**Syntax:** `gmb status [--dir <path>] [--json]`

**Flags:**

| Flag | Type | Default | Description |
|---|---|---|---|
| `--dir` | string | `.` | Directory containing `.glassmarble/` |
| `--json` | bool | `false` | Machine-readable JSON |

**Behavior:**
- No database → styled "uninitialized" card (or `{"initialized": false, "error": ...}` with `--json`). **Exit 0** — missing DB is a state, not an error.
- Database → streams the TTL once (bounded memory, no full graph restore) and reports: node/edge counts, indexed files, entry points, virtual nodes & share, dangling references, TTL/WAL sizes, schema & graph versions, commit hash, last analysis time, freshness (WAL), unpersisted transactions.

**JSON fields:** `initialized`, `storage_dir`, `schema_version`, `graph_version`, `commit_hash`, `last_analysis`, `nodes`, `edges`, `indexed_files`, `entrypoints`, `virtual_nodes`, `virtual_share_pct`, `dangling_references`, `ttl_bytes`, `wal_bytes`, `verified` (true when 0 dangling), `freshness_ok`, `unpersisted_transactions`, `error`, `generated_at`.

**Use cases:**
- "Is my graph up to date?" health check.
- CI gate: `verified` / `dangling_references`.
- Pre-AI sanity check (the AI's `akg_status` tool does the same).

**Examples:**
```bash
gmb status
gmb status --json | jq '.verified, .nodes, .dangling_references'
```

---

### 8.5 `gmb doctor`

**Purpose:** Run integrity diagnostics on the AKG state database.

**Syntax:** `gmb doctor [--dir <path>]`

**Flags:** `--dir` (string, default `.`).

**Checks performed:**
1. Parse-back integrity (TTL re-parses cleanly through the canonical parser)
2. Ontology conformance of every `gm:` term
3. Dangling references
4. Duplicate node IDs
5. WAL state (stale/leftover transactions)
6. File freshness

**Exit semantics:** exits **non-zero** when the database fails any integrity check (or when the WAL is stale). A missing database renders an uninitialized report with exit 0.

**Use cases:**
- After crashes or manual `.glassmarble` edits.
- Pre-upgrade verification.
- CI health gate alongside `status`.

---

### 8.6 `gmb diff`

**Purpose:** Show architectural diff across committed transactions.

**Syntax:** `gmb diff [--dir <path>]`

**Flags:** `--dir` (string, default `.`).

**Behavior:** Replays the write-ahead log and prints the structural mutations of every recorded transaction: TxID, commit hash, timestamp, status, nodes added, edges added, modified file count. Because the WAL is truncated after every successful atomic GraphJSON write, a clean database shows **no pending transactions** — the latest state lives in `akg.json`.

**Use cases:**
- Verify that the last analysis committed cleanly.
- Recover context after a crash (WAL entries that never made it to the state file).

---

### 8.7 `gmb tree`

**Purpose:** Display the architectural directory and symbol hierarchy tree.

**Syntax:** `gmb tree [--dir <path>] [--depth <n>]`

**Flags:**

| Flag | Type | Default | Description |
|---|---|---|---|
| `--dir` | string | `.` | Directory containing `.glassmarble/` |
| `--depth` | int | `4` | Maximum directory depth to expand |

**Behavior:**
- Loads the active snapshot; errors with `AKG database is empty` if none.
- Groups nodes by file path, prints `path` then indented symbols as `Name [Kind] <Primitive>`.
- Sorted paths; capped at **200 files** printed with `└── ... (showing 200 of N files)`.
- Footer: `N file(s) indexed · M symbol(s)`.
- Interactive TTY → scrollable Bubble Tea program; non-TTY → plain lines.

**Use cases:**
- Orientation in an unfamiliar codebase.
- Package/class/method overview before deeper queries.

**Example:**
```bash
gmb tree --depth 3
```

---

### 8.8 `gmb dependency [target_file_or_symbol]`

**Purpose:** Analyze inbound and outbound dependencies for a file or symbol.

**Syntax:** `gmb dependency [target] [--dir <path>] [--json]` — at most one argument.

**Flags:**

| Flag | Type | Default | Description |
|---|---|---|---|
| `--dir` | string | `.` | Database folder |
| `--json` | bool | `false` | JSON report |

**Behavior:**
- **No target** → repository summary: total nodes, outbound/inbound edge mappings, and top-20 most-depended-upon nodes (by outbound edge count).
- **With target** → case-insensitive matching on node ID, file path, and exact name. Every match prints its outbound (`-> target [type] (L#line)`) and inbound (`<- source [type] (L#line)`) edges with line numbers.
- No match → error `no matching node or file found for 'X'` (exit non-zero).

**JSON output (target mode):** `{"target": ..., "nodes": [{"id", "outbound": [{"type","id","line"}], "inbound": [...]}]}`.

**Use cases:**
- "Who calls `Connect`?" before a refactor.
- "What does this file import?" impact scoping.
- CI documentation of coupling.

**Examples:**
```bash
gmb dependency
gmb dependency Connect
gmb dependency internal/db/db.go
gmb dependency Connect --json
```

---

### 8.9 `gmb hotspot`

**Purpose:** Identify high-coupling architectural hotspots and most-depended-upon symbols.

**Syntax:** `gmb hotspot [--dir <path>] [--top <n>] [--json]`

**Flags:**

| Flag | Type | Default | Description |
|---|---|---|---|
| `--dir` | string | `.` | Database folder |
| `--top` | int | `10` | Number of top hotspot symbols to display |
| `--json` | bool | `false` | JSON output |

**Behavior:** Ranks all nodes with any degree by **in-degree** (descending) — the symbols everyone depends on — and prints rank, ID, kind, in/out degree, primitive.

**Use cases:**
- Refactoring priorities (high in-degree = high blast radius).
- Architecture review: god-object candidates.
- Risk reporting for PRs.

---

### 8.10 `gmb inspect [node_id]`

**Purpose:** Inspect AKG graph nodes, search symbols, and discover entry points.

**Syntax:** `gmb inspect [node_id] [--list] [--search <query>] [--type <kind>] [--file <path> --line <n>] [--dir <path>]`

**Flags:**

| Flag | Type | Default | Description |
|---|---|---|---|
| `--list` | bool | `false` | List candidate entry points (FUNCTION/METHOD) for sequence diagrams |
| `--search` | string | `""` | Search nodes by symbol name or path fragment |
| `--type` | string | `""` | Filter by node kind: `FUNCTION`, `METHOD`, `STRUCT`, `CLASS`, `INTERFACE` |
| `--file` | string | `""` | File path to look up (used with `--line`) |
| `--line` | int | `0` | Line number to look up (used with `--file`) |
| `--dir` | string | `.` | Database folder |

**Modes (mutually exclusive):**

| Mode | Plain output | Interactive TTY |
|---|---|---|
| `--list` | Streams first 30 entry points `[FUNCTION] id (path:Ln)` | Filterable table program |
| `--search q` | Streams up to 20 matches with ID/kind/file/line/primitive | Filterable table program |
| `node_id` arg | Full node detail: name, kind, primitive, file span, properties, outbound/inbound edges with line numbers | Detail view |
| `--file F --line L` | Resolves the node containing line L in file F, then shows its details | Detail view |

**Line lookup algorithm:** streams nodes, filters to the file, picks the node with the largest `LineStart <= line` (binary-search equivalent without loading the index).

**Use cases:**
- Find an entry point for a sequence diagram (`inspect --list` → pick ID → `visualize sequence --entry <id>`).
- "What symbol is at line 42 of x.go?"
- Deep-dive a single node's dependencies with line-level precision.

**Examples:**
```bash
gmb inspect --list
gmb inspect --search Store
gmb inspect --type INTERFACE
gmb inspect "src/db.go::DBStore::Save"
gmb inspect --file src/db.go --line 42
```

---

### 8.11 `gmb drift`

**Purpose:** Detect architecture drift against declared layering and cycle budgets.

**Syntax:** `gmb drift [--dir <path>] [--json]`

**Flags:**

| Flag | Type | Default | Description |
|---|---|---|---|
| `--dir` | string | `.` | Database folder |
| `--json` | bool | `false` | JSON report |

**Behavior:**
1. Loads the active snapshot (errors if empty).
2. Reads the `drift:` section from `.glassmarble/config.yaml` (merged onto defaults; bare config without `drift` still parses).
3. Assigns every node to the first matching layer glob.
4. Counts forbidden cross-layer edges and layer cycles.

**Exit semantics:** returns an error (exit non-zero) when `forbidden_dependencies > 0` **or** `cycle_count > cycle_budget`. This makes `gmb drift` a natural CI gate.

**JSON fields:** `layers_defined`, `violations[]` (`source_id`, `target_id`, `source_layer`, `target_layer`, `edge_type`, `message`), `cycle_count`, `cycle_budget`, `forbidden_dependencies`.

**Use cases:**
- Enforce "UI must not call persistence directly."
- Cycle budgets on layered architectures.
- CI: `gmb drift` must pass on every PR.

**Example with config:**
```yaml
# .glassmarble/config.yaml
drift:
  layers:
    - name: api
      paths: ["cmd/api/**"]
    - name: core
      paths: ["internal/core/**"]
  forbidden_deps:
    - source: core
      target: api
  cycle_budget: 0
```

```bash
gmb drift
gmb drift --json | jq '.violations'
```

---

### 8.12 `gmb visualize <diagram_type>`

**Purpose:** Generate visual architecture diagrams ("marbles") from the AKG — 28 diagram types.

**Syntax:** `gmb visualize <diagram_type> [flags]`

**Diagram types:**

| Family | Types |
|---|---|
| **UML (14)** | `class`, `object`, `component`, `deployment`, `package`, `composite`, `profile`, `usecase`, `activity`, `state`, `sequence`, `communication`, `interaction`, `timing` |
| **C4 (7)** | `c4context`, `c4container`, `c4component`, `c4code`, `c4landscape`, `c4dynamic`, `c4deployment` |
| **Specialized (4)** | `er`, `dataflow`, `mindmap`, `flowchart` |
| **Track G (6)** | `dependency`, `hotspot`, `callgraph`, `layered`, `impact`, `infrastructure` |

**Flags:**

| Flag | Type | Default | Description |
|---|---|---|---|
| `--entry` | string | `""` | Execution entry point symbol ID (**mandatory for `sequence`**) |
| `--depth` | int | `7` | Max search depth for reachability path walk |
| `--unused` | bool | `false` | Include unreferenced dead components |
| `--dir` | string | `.` | Directory containing `.glassmarble/` |
| `--save` | string | `""` | Save diagram to a `.md` file inside `.glassmarble/marbles/` |
| `--format` | string | `mermaid` | Markup language: `mermaid`, `plantuml`, `dot` |
| `--scope` | string | `""` | `global` (default), `folder:<path>`, `file:<path>` |
| `--output` | string | `""` | Write raw markup to a file instead of stdout |
| `--summary` | bool | `false` | Print graph summary before the diagram |
| `--pagerank` | bool | `false` | Enable PageRank computation |
| `--community` | bool | `false` | Enable community detection |
| `--scc` | bool | `false` | Enable strongly connected components analysis |
| `--render` | string | `""` | Render to an image file — `.svg` or `.png` — via Kroki (network) then mermaid-cli (local) |

**Behavior / execution flow:**

```
validate diagram type
  → sequence requires --entry (else error)
  → locate .glassmarble/akg.json (else: "active AKG database not found")
  → parse scope (global | folder:path | file:path)
  → optional analytics pipeline (pagerank/community/scc)
  → optional --summary callback
  → 3-tier visualizer engine projects the state into markup
  → output routing:
        --render <x.svg|png>  → Kroki POST (30s timeout) → mermaid-cli fallback → raw markup saved as <target>.txt on total failure
        --save <name>         → .glassmarble/marbles/<name>.md with language fence (mermaid/plantuml/dot)
        --output <file>       → raw markup to file
        (none)                → markup to stdout
```

**Render pipeline (--render):** Kroki at `https://kroki.io/<type>/<svg|png>` first; on failure falls back to local `mmdc` (mermaid-cli); if both fail, markup is persisted to `<target>.txt` and an error returned.

**Use cases:**
- Documentation: `visualize class --save class-diagram`.
- Sequence diagrams from real entry points: `inspect --list` → `visualize sequence --entry <id>`.
- Architecture reviews: `visualize layered`, `visualize callgraph`, `visualize hotspot`.
- Impact analysis: `visualize impact --entry <id> --depth 3`.
- Subsystem zoom: `visualize c4component --scope folder:internal/core`.
- Slide material: `visualize component --render diagram.svg`.

**Examples:**
```bash
gmb visualize class
gmb visualize class --format plantuml
gmb visualize c4container --scope folder:internal --save c4-container
gmb visualize sequence --entry "src/app.go::main" --depth 5
gmb visualize dependency --pagerank --community --summary
gmb visualize callgraph --render callgraph.png
```

---

### 8.13 `gmb compare [base_graph.json] [head_graph.json]`

**Purpose:** Diff two AKG snapshots and report the architectural changes. This is the command CI runs to produce a PR architecture comment.

**Syntax:** `gmb compare [base.json head.json] | gmb compare --dir <path> [--json]`

**Flags:**

| Flag | Type | Default | Description |
|---|---|---|---|
| `--dir` | string | `.` | Database folder (**used only with no file args**) |
| `--json` | bool | `false` | JSON diff output |

**Two operating modes:**

1. **Two-file mode** — both graphs supplied as GraphJSON files:
   ```
   gmb compare main-branch.json pr-branch.json
   ```
2. **Working-tree mode** (`--dir`, no args) — base is the **committed AKG** (`.glassmarble/akg.json`); head is produced by a **fresh full analysis of the current working tree**. The base is cloned first so the analysis doesn't mutate it.

**Behavior:** `akg.DiffGraphs(base, head)` → added/removed nodes, added/removed edges, touched files. Rendered as a two-column styled compare (or JSON).

**Use cases:**
- PR review: "what architecture changed in this branch?"
- Release notes: architecture delta between tags.
- CI comment generation.

**Examples:**
```bash
gmb export --output base.json && git checkout feature && gmb export --output head.json
gmb compare base.json head.json
gmb compare --dir . --json
```

---

### 8.14 `gmb export`

**Purpose:** Export the AKG snapshot to a portable, diff-friendly document.

**Syntax:** `gmb export --output <graph.json|graph.ttl> [--dir <path>]`

**Flags:**

| Flag | Type | Default | Description |
|---|---|---|---|
| `--output`, `-o` | string | `""` | **Required.** Output file path |
| `--dir` | string | `.` | Database folder |

**Behavior:** extension decides format:
- `.json` → **GraphJSON** (recommended interchange: lossless — edge confidence, parallel edges — deterministic, reviewable in PRs)
- `.ttl` / `.turtle` → canonical RDF Turtle (matches on-disk format)

Errors if the database is empty or `--output` is missing; unsupported extension → error.

**Use cases:**
- Backing up the graph.
- Feeding `compare` / `import`.
- Committing an architecture snapshot to the repo for PR diffs.

**Examples:**
```bash
gmb export -o graph.json
gmb export -o snapshot.ttl
```

---

### 8.15 `gmb import <graph.json>`

**Purpose:** Import a portable graph document, **replacing** the active AKG snapshot.

**Syntax:** `gmb import <graph.json> [--dir <path>]`

**Flags:** `--dir` (string, default `.`).

**Behavior:**
1. Parses GraphJSON.
2. Opens the AKG transaction manager.
3. `ReplaceGraph` — **rejects dangling references** so the persisted state always stays verified.
4. WAL is truncated after import.
5. Prints a success card with node/edge counts.

**Use cases:**
- Restoring a backup.
- Applying a reviewed architecture change produced elsewhere.
- CI artifact promotion.

**Example:**
```bash
gmb import graph.json
```

---

### 8.16 `gmb hooks <install|uninstall>`

**Purpose:** Install or uninstall a Git `post-commit` hook that auto-runs `gmb analyze` after every commit.

**Syntax:** `gmb hooks install [--dir <path>]` / `gmb hooks uninstall [--dir <path>]`

**Flags:** `--dir` (string, default `.`).

**Behavior:**
- `install`: writes `.git/hooks/post-commit` with the **absolute binary path** and the **absolute target dir** (works regardless of CWD and whether `gmb` is on PATH):
  ```sh
  #!/bin/sh
  # GlassMarble auto-analysis post-commit hook
  "C:\path\to\gmb.exe" analyze --dir "C:\abs\repo"
  ```
- `uninstall`: removes the hook (idempotent; reports if none existed).
- Errors if `.git/hooks` doesn't exist (not a git repo).

**Use cases:**
- Keep the AKG fresh with zero effort after each commit.
- Team workflows: every developer keeps the graph in sync automatically.

**Examples:**
```bash
gmb hooks install
gmb hooks uninstall
```

---

### 8.17 `gmb housekeeping`

**Purpose:** Report and prune `.glassmarble` working-set storage (marbles, AI sessions, WAL). **Never touches `akg.json`** (guarded by verification and `--max-ttl-mb`).

**Syntax:** `gmb housekeeping [--prune] [--older-than <days>] [--dir <path>]`

**Flags:**

| Flag | Type | Default | Description |
|---|---|---|---|
| `--dir` | string | `.` | Database folder |
| `--prune` | bool | `false` | Delete marbles/sessions older than the retention window and truncate the WAL |
| `--older-than` | int | `30` | Retention window in days |

**Behavior:**
- **Report only** (no `--prune`): per-area sizes for `state (akg.json)`, `wal/`, `marbles/`, `ai/` + totals, plus a hint to run `--prune`.
- **With `--prune`:** interactive terminals show a preview and ask for confirmation; non-TTY runs prune directly. Deletes marbles/ai files older than the cutoff, then truncates the WAL **if a healthy non-empty state file exists**.

**Use cases:**
- Scheduled cleanup: `gmb housekeeping --prune --older-than 7` (cron/CI).
- Storage reporting before deciding on retention.

---

### 8.18 `gmb completion <shell>`

**Purpose:** Generate shell completion scripts.

**Syntax:** `gmb completion bash|zsh|fish|powershell`

**Shells:** `bash`, `zsh`, `fish`, `powershell`.

**Important:** this command **bypasses Fang's styled output** — help and scripts are ANSI-free so they can be safely piped into a shell session.

**Installation:**
```bash
source <(gmb completion bash)                      # bash
gmb completion zsh > "${fpath[1]}/_glassmarble"    # zsh
gmb completion fish > ~/.config/fish/completions/gmb.fish
```

**Use cases:** tab-completion of commands, flags, and diagram types.

---

### 8.19 `gmb version`

**Purpose:** Print the GlassMarble version.

**Syntax:** `gmb version`

**Behavior:** prints a styled version badge (e.g. `GlassMarble v0.1.0`). Equivalent: `gmb --version`.

---

### 8.20 `gmb ai` (and subcommands)

**Purpose:** Ask the GlassMarble AI Architect anything about the codebase. Bring-Your-Own-Key engine with agentic tool calling over the AKG and the source tree.

**Syntax:** `gmb ai [question]` — plus subcommands `chat`, `configure`, `models`, `doctor`, `sessions`.

**Persistent AI flags (all `gmb ai` forms):**

| Flag | Type | Default | Description |
|---|---|---|---|
| `--provider` | string | `""` | Provider name (openai, anthropic, gemini, deepseek, mistral, glm, nvidia, openrouter, groq, ollama, custom) |
| `--model` | string | `""` | Model identifier |
| `--key` | string | `""` | API key (prefer env vars or `gmb ai configure`) |
| `--base-url` | string | `""` | Override provider API base URL |
| `--temperature` | float | `0` | Sampling temperature (`0` = provider default) |
| `--max-turns` | int | `0` | Max agent tool-call turns |
| `--timeout` | int | `0` | HTTP timeout (seconds) |
| `--tools` | string | `""` | Restrict tools to categories (`system`, `akg`, `code`, `diagram`) or individual tool names, comma-separated |
| `--no-tools` | bool | `false` | Plain chat mode, no tool calling |
| `--no-stream` | bool | `false` | Buffered output instead of streaming |
| `--max-total-tokens` | int | `0` | Stop when prompt+completion tokens exceed this |
| `--max-cost` | float | `0` | Stop when estimated spend exceeds this USD amount |
| `--max-session-messages` | int | `0` | Chat history budget (messages kept) |
| `--save` | string | `""` | Save the final answer instead of printing: diagram markup → `.glassmarble/marbles/`, everything else → `.glassmarble/ai/` (single-query mode) |

**Agent tools (by category):**

| Category | Tools |
|---|---|
| `system` | `system_status`, `system_diagram_types` |
| `akg` | `akg_status`, `akg_summary`, `akg_search`, `akg_get_node`, `akg_edges`, `akg_traverse`, `akg_path`, `akg_cycles`, `akg_orphans`, `akg_god_objects`, `akg_hotspots`, `akg_page_rank`, `akg_impact_radius`, `akg_communities`, `akg_articulation_points`, `akg_topological_order`, `akg_entrypoints`, `akg_similarity` |
| `code` | `code_read_file`, `code_list_dir`, `code_search_symbol`, `code_definition`, `code_diff` |
| `diagram` | `diagram_generate`, `diagram_summary`, `diagram_types` |

**Behavior (single query):**
- With a question → streams an answer; tool calls/returns are traced to stderr (`→ tool(args)`, `← tool: ok (N bytes)`).
- `--save <name>` → writes the answer to `.glassmarble/marbles/` (if it looks like diagram markup) or `.glassmarble/ai/`; prints a path receipt instead of the answer.
- Stop reasons are surfaced: turn limit, token budget, cost budget.
- `--verbose` prints token/cost accounting.

**Examples (from the command help):**
```bash
gmb ai "explain the architecture of this repository"
gmb ai "which services depend on the payment module"
gmb ai "generate a C4 container diagram"
gmb ai "generate a C4 container diagram" --save c4.md
gmb ai "write architecture notes" --save notes.md
gmb ai --no-tools "opinion question"
gmb ai --tools akg,code "question"
gmb ai --max-cost 0.5 "question"
gmb ai --no-stream "question"
```

---

#### 8.20.1 `gmb ai chat`

**Purpose:** Interactive multi-turn conversation with session memory.

**Syntax:** `gmb ai chat [--session <id>] [--new]`

**Flags:**

| Flag | Type | Default | Description |
|---|---|---|---|
| `--session` | string | `""` | Resume a specific saved session by id |
| `--new` | bool | `false` | Start a fresh session instead of resuming the latest |

**Behavior:**
- Resumes the latest session by default (or starts one).
- `--session` + `--new` together → error (mutually exclusive).
- Transcripts saved under `.glassmarble/ai/sessions/`; `exit`/`quit`/`bye` or Ctrl+D leaves.
- Session summary printed at exit: turns, messages, tokens, cost, resume hint.
- In the interactive TUI, Ctrl+S saves the last turn to `.glassmarble/ai/chat-<id>.md`.

**Use cases:** long-running architecture conversations with context that persists across CLI sessions.

---

#### 8.20.2 `gmb ai configure`

**Purpose:** Configure the AI provider, model, and API key (BYOK).

**Syntax:**
```
gmb ai configure                    # interactive wizard (TTY required)
gmb ai configure --provider NAME --model MODEL --key KEY [--scope global|project]
```

**Flags:**

| Flag | Type | Default | Description |
|---|---|---|---|
| `--scope` | string | `global` | `global` → `~/.glassmarble/ai.yaml`; `project` → `<dir>/.glassmarble/ai.yaml` |
| `--dir` | string | `.` | Workspace directory for `--scope project` |
| `--provider` | string | `""` | Provider name |
| `--model` | string | `""` | Model identifier |
| `--key` | string | `""` | API key |
| `--base-url` | string | `""` | Base URL override |
| `--temperature` | float | `0` | Temperature |
| `--max-turns` | int | `0` | Turn cap |
| `--timeout` | int | `0` | HTTP timeout (s) |
| `--max-total-tokens` | int | `0` | Token budget |
| `--max-cost` | float | `0` | Cost budget (USD) |
| `--max-session-messages` | int | `0` | Session message budget |

**Behavior:** without flags requires a TTY (interactive Huh form); with flags updates the target file. Validates the provider against the registry; saves with `0600` permissions; prints masked key confirmation. Unknown provider → error listing `gmb ai models`.

**Use cases:** one-time setup; team project-scoped config committed to the repo (`.glassmarble/ai.yaml`).

---

#### 8.20.3 `gmb ai models`

**Purpose:** List supported AI providers and their models.

**Syntax:** `gmb ai models`

**Behavior:** prints the registry with current provider highlighted and per-provider key status (configured via config, env var, or neither).

**Providers & models (as registered):**

| Provider | Key env var | Models |
|---|---|---|
| openai | `GLASSMARBLE_OPENAI_API_KEY` | gpt-5, gpt-5-mini, gpt-4o, gpt-4o-mini, o3, o3-mini |
| anthropic | `GLASSMARBLE_ANTHROPIC_API_KEY` | claude-opus-4-1, claude-sonnet-4-5, claude-haiku-4-5 |
| gemini | `GLASSMARBLE_GEMINI_API_KEY` | gemini-2.5-pro, gemini-2.5-flash, gemini-2.5-flash-lite, gemini-2.0-flash |
| deepseek | `GLASSMARBLE_DEEPSEEK_API_KEY` | deepseek-chat, deepseek-reasoner |
| mistral | `GLASSMARBLE_MISTRAL_API_KEY` | mistral-large-latest, mistral-small-latest, codestral-latest |
| glm | `GLASSMARBLE_GLM_API_KEY` | glm-4.6, glm-4.5, glm-4.5-air |
| nvidia | `GLASSMARBLE_NVIDIA_API_KEY` | deepseek-ai/deepseek-r1, nvidia/llama-3.1-nemotron-70b-instruct, meta/llama-3.3-70b-instruct |
| openrouter | `GLASSMARBLE_OPENROUTER_API_KEY` | openai/gpt-5, anthropic/claude-sonnet-4-5, google/gemini-2.5-pro, deepseek/deepseek-chat, qwen/qwen3-235b-a22b |
| groq | `GLASSMARBLE_GROQ_API_KEY` | llama-3.3-70b-versatile, llama-3.1-8b-instant |
| ollama | `GLASSMARBLE_OLLAMA_BASE_URL` | llama3.3, qwen3, deepseek-r1, mistral (local, default `http://localhost:11434/v1`) |
| custom | `GLASSMARBLE_AI_API_KEY` | user-defined (OpenAI-compatible, requires `--base-url`) |

---

#### 8.20.4 `gmb ai doctor`

**Purpose:** Diagnose the AI engine setup.

**Syntax:** `gmb ai doctor`

**Behavior:** validates configuration, pings provider connectivity, checks AKG presence; prints a styled report (problems flagged). Exit non-zero when problems are found.

**Use cases:** first-run troubleshooting ("why won't the AI answer?"), config migration checks.

---

#### 8.20.5 `gmb ai sessions`

**Purpose:** List / delete saved chat sessions.

**Syntax:** `gmb ai sessions [--delete <id>]`

**Flags:**

| Flag | Type | Default | Description |
|---|---|---|---|
| `--delete` | string | `""` | Delete a saved session by id |

**Behavior:** interactive TTY → session browser with delete; non-TTY → plain list (newest first). `--delete <id>` removes one session and prints confirmation.

---

### 8.21 `gmb memory`

**Purpose:** Query the Stage 6 developer memory — what the system was, what changed, and (where evidence exists) why. Deterministic retrieval, no LLM (master plan §4.4).

**Syntax:** `gmb memory [--dir <path>] [--ask "<question>"] [--component <name>] [--json]`

**Flags:**

| Flag | Type | Default | Description |
|---|---|---|---|
| `--dir` | string | `.` | Directory containing the `.glassmarble/` database |
| `--ask` | string | `""` | Natural-language-style question answered by ranked retrieval |
| `--component` | string | `""` | Show the full history of one component (case-insensitive substring match) |
| `--json` | bool | `false` | Emit the machine-readable document instead of the human report |

**Data source:** `.glassmarble/memory/` — `events.jsonl`/`claims.jsonl` (append-only WALs, source of truth) + derived `memory.json`/`timeline.json`. Populated by `gmb analyze --stage5`; re-runs are idempotent.

**Output (default):** project id, event count, component count, last update, and each component with its temporal state (CURRENT / REMOVED / DEPRECATED / EXPERIMENTAL / HISTORICAL / UNKNOWN).

**Output (`--ask`):** ranked components, knowledge claims (each labelled by how it was established — `FACT`, `EXPLICIT_REASON`, `INFERENCE`, `SPECULATION` — with aggregate confidence), matching events, and the related timeline. Reasons are never invented: an event without a stated reason produces no reason claim.

**Exit codes:** `0` on success; non-zero on load/path failures. An empty memory or a no-match query is a graceful message with exit `0`.

**Use cases:** "what do we know about Redis?", "why was PaymentService added?", onboarding context, pre-refactor impact review.

---

## 9. Analysis Pipeline Execution Flow

The `analyze`/`watch` pipeline is the heart of the product. Four stages, then an atomic commit:

```
┌──────────────────────────────────────────────────────────────────────┐
│ STAGE 1 — Tree-sitter Ingestion                                      │
│   discover source files (git-tracked when .git exists)               │
│   FULL: walk all files → concurrent parse through worker pool        │
│   DELTA: parse only git-diff files, emit deletes for removed files   │
│   skips: oversized (> max_file_bytes), unknown grammar               │
│   live progress → TUI counter                                        │
├──────────────────────────────────────────────────────────────────────┤
│ STAGE 2 — GAST Normalization                                         │
│   normalize every parsed tree → canonical GAST nodes                 │
│   produces UpsertedTrees + DeletedPaths                              │
├──────────────────────────────────────────────────────────────────────┤
│ STAGE 3 — Topology Aggregation                                       │
│   build global definition index (symbol → definitions)               │
│   workspace context for cross-file resolution                        │
├──────────────────────────────────────────────────────────────────────┤
│ STAGE 4 — CPG Linking                                                │
│   resolve references → nodes + outbound edges                        │
│   detail: architecture (default) | standard CFG | full CFG+DFG       │
│   macro inference: disabled | structural | all                       │
│   budgets: --max-nodes / --abort-on-limit                            │
├──────────────────────────────────────────────────────────────────────┤
│ COMMIT — ExecuteDeltaTransaction                                     │
│   append to WAL → merge delta into base graph → atomic TTL write     │
│   (temp file + rename) → verify → truncate WAL                       │
└──────────────────────────────────────────────────────────────────────┘
   Quality report on the MERGED graph: files, nodes, edges, virtual,
   dangling, ttl/wal sizes, duration
```

**Guard rails:**
- Delta only against a **non-empty** base state (fixed bug — see §8.2 warning).
- `db.lock` prevents concurrent transactions; stale locks reclaimed after 30s.
- `--max-ttl-mb` refuses oversized state load/commit.
- Post-write verification + `doctor` for integrity.

---

## 10. Use Cases & Command Workflows

### 10.1 First-time onboarding (new repo)

```bash
gmb init                          # 1. create workspace
gmb analyze --verbose             # 2. first full scan (automatic)
gmb status                        # 3. verify health (0 dangling, verified)
gmb tree --depth 3                # 4. get oriented
gmb dependency                    # 5. see the coupling landscape
```

### 10.2 Daily development loop

```bash
# Option A — manual:
# ... edit code ...
gmb analyze                       # incremental delta, fast

# Option B — automatic after every commit:
gmb hooks install                 # post-commit hook runs analyze
# ... commit ... (graph updates automatically)

# Option C — continuous:
gmb watch                         # re-analyzes on every save
```

### 10.3 Architecture governance (CI gate)

```yaml
# .glassmarble/config.yaml
drift:
  layers:
    - name: api
      paths: ["cmd/api/**"]
    - name: core
      paths: ["internal/core/**"]
  forbidden_deps:
    - source: core
      target: api
  cycle_budget: 2
```

```bash
gmb analyze --json          # in CI: build/refresh the graph
gmb drift --json            # gate: fails on forbidden deps / budget breach
gmb doctor                  # gate: fails on corrupt state
gmb status --json           # gate: assert verified == true
```

### 10.4 Code review / PR architecture comment

```bash
# On the base branch:
gmb export -o base.json
# On the feature branch:
gmb export -o head.json
gmb compare base.json head.json          # human-readable
gmb compare base.json head.json --json   # machine-readable for comment bot
```

### 10.5 Impact analysis before a refactor

```bash
gmb hotspot --top 20              # find the most-coupled symbols
gmb inspect "src/db.go::DBStore::Save"   # line-level in/out dependencies
gmb visualize impact --entry "src/db.go::DBStore::Save" --depth 4
gmb dependency Connect            # who calls it?
```

### 10.6 Documentation & diagrams

```bash
gmb inspect --list                # find entry points for sequence diagrams
gmb visualize sequence --entry "src/app.go::main" --save main-sequence
gmb visualize class --save class-diagram --format plantuml
gmb visualize c4container --scope folder:internal --save c4-internal
gmb visualize callgraph --render callgraph.png   # PNG for slides/docs
```

### 10.7 AI assistant workflows

```bash
gmb ai configure --provider gemini --model gemini-2.5-flash --key <KEY>   # once
gmb ai "explain the architecture of this repository"
gmb ai "which services depend on the payment module"
gmb ai "generate a C4 container diagram" --save c4.md
gmb ai --max-cost 0.5 "what is the worst coupling here?"
gmb ai chat                        # multi-turn with memory
gmb ai chat --new                  # fresh conversation
gmb ai sessions                    # manage saved sessions
gmb ai doctor                      # diagnose setup
```

### 10.8 Backup & restore

```bash
gmb export -o backup.json          # snapshot the graph
gmb import backup.json             # restore (rejects dangling refs)
```

### 10.9 Storage hygiene

```bash
gmb housekeeping                          # report sizes
gmb housekeeping --prune --older-than 7   # prune + truncate WAL (cron-friendly)
```

---

## 11. Best Practices

1. **Let the first analysis be a full scan.** Don't `--full` after `init`; just run `gmb analyze` — the empty-base guard already forces a full scan.
2. **Keep the default `--commit ""`.** That's the working-tree delta. Only pass a hash when you intentionally want a specific commit's diff.
3. **Use `--full` sparingly.** It forces per-branch CFG+DFG at full linker detail and can be dramatically slower. Prefer `gmb analyze --link-level standard` for a deeper-but-faster pass, or `--full` only for complete rebuilds.
4. **Never run two analyses concurrently.** `db.lock` exists for a reason; `watch` + manual `analyze` + `hooks` can collide. If you get `ErrLockTimeout`, wait and retry.
5. **Use `--json` in automation.** Every report command (`status`, `dependency`, `hotspot`, `drift`, `compare`, `analyze`) has a JSON mode that is TUI-free and stable.
6. **Treat `gmb doctor` as a gate.** Run it after any crash, manual TTL edit, or before upgrades. Non-zero exit = broken DB.
7. **Make `gmb drift` a mandatory PR check.** Declare layers + forbidden deps + cycle budget in `config.yaml`; drift exits non-zero on violations.
8. **Install the post-commit hook or run `watch` in dev** so the AKG never goes stale — stale graphs produce misleading AI answers and diagrams.
9. **Use `--scope folder:...` on big repos.** `visualize` on a global scope can be heavy; scope to `folder:` or `file:` for focused diagrams.
10. **`--max-ttl-mb` on shared/CI machines** prevents accidental graph bloat from leaking into every command.
11. **BYOK keys: prefer env vars or `gmb ai configure` over `--key`** on the command line (shell history exposure). Keys are masked in all output.
12. **Schedule `housekeeping --prune`** (cron/CI) so marbles and AI sessions don't accumulate unbounded.
13. **Shell completions:** `source <(gmb completion bash)` (or zsh/fish/powershell) — the scripts are ANSI-clean by design.
14. **Verify after big changes:** `gmb status` → `verified` should be `true` and `dangling_references` zero; otherwise `gmb analyze --full` rebuilds.

---

## 12. Exit Codes & Error Semantics

| Command | Exit 0 | Exit non-zero when |
|---|---|---|
| `init` | workspace created/verified | path resolution fails |
| `analyze` | committed, healthy | any stage fails, commit rejected |
| `watch` | Ctrl+C stopped | not a git repo; watcher failure |
| `status` | **always** (missing DB = uninitialized state, not error) | TTL unreadable/corrupt |
| `doctor` | all integrity checks pass (missing DB also 0) | parse-back failure, dangling, stale WAL, duplicate IDs |
| `diff` | printed | missing DB |
| `tree` | printed | empty DB |
| `dependency` | printed | empty DB; no matching target |
| `hotspot` | printed | empty DB |
| `inspect` | printed | empty DB; node/line not found |
| `drift` | compliant | forbidden deps > 0 OR cycle_count > cycle_budget |
| `visualize` | markup rendered/saved | bad type, missing DB, sequence without `--entry`, render failure |
| `compare` | diff printed | missing files, empty DB |
| `export` | file written | missing `--output`, empty DB, bad extension |
| `import` | replaced | parse failure, dangling references rejected |
| `hooks` | installed/uninstalled | not a git repo; unknown subcommand |
| `housekeeping` | report/prune done | (prune is best-effort per area) |
| `completion` | script emitted | unknown shell |
| `version` | printed | — |
| `ai` | answered | no question; config/connection failure; doctor problems; save failures |
| `ai chat` | conversation ended | engine/connection failure per turn (continues); invalid session |
| `ai sessions` | listed/deleted | invalid session id |
| `ai configure` | saved | invalid provider; non-TTY without flags |
| `ai models` | printed | — |
| `ai doctor` | healthy | problems found |

Common errors:
- `AKG database is empty -- run 'glassmarble analyze' first` → run `gmb analyze` (or `gmb init` first).
- `active AKG database not found at ...` → missing `.glassmarble/akg.json`.
- `failed to commit AKG transaction` / lock timeouts → concurrent analysis or stale lock (auto-reclaimed after 30s).
- `Refuse to load ... larger than N MiB` → `--max-ttl-mb` budget hit.

---

## 13. Troubleshooting

| Symptom | Likely cause | Fix |
|---|---|---|
| `Files Analyzed` looks too small after `init` | `--commit` used to default to `HEAD` (fixed in this version) | Update binary; plain `gmb analyze` (working-tree delta / full scan) |
| Analysis picks up only the last commit's files | Passing `--commit <hash>` intentionally diffs that commit | Omit `--commit` for working-tree delta |
| `dangling_edges > 0` | Base graph missing/incomplete | `gmb analyze --full` rebuild |
| `db.lock` timeouts | concurrent analyses | Wait 30s (stale lock auto-reclaim) or delete `db.lock` |
| Files skipped with "exceeds MaxFileBytes" | `max_file_bytes` too small (default 10MB, init writes 2MB) | Raise in `.glassmarble/config.yaml` or `GLASSMARBLE_MAX_FILE_BYTES` |
| "untracked by git" warnings | GitTrackedOnly mode (auto when `.git` exists) | Files not in git are excluded by design; commit them or remove warning expectation |
| `sequence` diagram refuses without entry | entry point mandatory | `gmb inspect --list` to find an entry ID, then `--entry <id>` |
| `--render` writes `.txt` instead of image | Kroki unreachable + no `mmdc` | Install mermaid-cli or check network |
| AI says "doctor found N problem(s)" | missing key/provider/AKG | `gmb ai doctor`, `gmb ai configure`, `gmb analyze` |
| `ai configure` non-TTY error | interactive wizard needs terminal | Use flag form or env vars |
| WAL never shrinks | stale transactions | `gmb housekeeping --prune` or run any successful analysis (truncates) |
| Large graph slow to load | big TTL | `gmb status` uses streaming (fast); use `--scope` on `visualize`; raise `--max-ttl-mb` only if intended |

---

*Generated from the GlassMarble v0.1.0 CLI source. Command files: `cmd/*.go`. For product/architecture details see `docs/architecture.md`; for AI engine details see `docs/ai.md`.*
