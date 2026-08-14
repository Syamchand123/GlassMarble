<p align="center">
  <img src="./assets/GMB_LOGO.png" width="180" alt="GlassMarble">
</p>

<h1 align="center">GlassMarble</h1>

<p align="center">
  <strong>AI Architecture Intelligence Platform</strong><br>
  A self-evolving Architecture Knowledge Graph compiler, visualizer, and AI architect for your codebase.
</p>

<p align="center">
<a href="docs/architecture.md">Architecture Manual</a> ·
  <a href="docs/commands_master_reference.md">CLI Master Reference</a> ·
  <a href="docs/ai.md">AI Architect Guide</a> ·
  <a href="docs/akg_format.md">AKG Format</a> ·
  <a href="docs/configuration.md">Configuration</a> ·
  <a href="#quick-start">Quick Start</a> ·
  <a href="#cli-reference">CLI Reference</a> ·
  <a href="#diagram-types">Diagram Types</a>
</p>

---

## What is GlassMarble?

Modern codebases suffer from **documentation drift**: architecture diagrams and dependency maps are created during design and become obsolete the moment code changes. GlassMarble eliminates this permanently.

GlassMarble compiles your source code — across **17 languages** (14 with full tree-sitter grammars, plus Kotlin/Swift/Scala declaration support) — into a semantic **Architecture Knowledge Graph (AKG)** stored as a portable GraphJSON database (`.glassmarble/akg.json`). From this living graph it can:

- **Generate 31 architecture diagrams** (UML + C4 + specialized + analysis) as Mermaid, PlantUML, or DOT markup — always synchronized with your latest code.
- **Detect architectural hotspots**, circular dependencies, and high-coupling bottlenecks automatically.
- **Answer any question about your codebase** through a grounded AI Architect agent that reads real graph data, not LLM hallucinations.
- **Track structural changes** between commits using Git-aware incremental analysis.

> The repository is the single source of truth. The graph follows it.

---

## How It Works

```
┌──────────────────────────────────────────────────────────────────────┐
│            Multi-Language Source Code                                │
│  Go · Python · JS/TS · Java · C/C++ · C# · Ruby · PHP · Rust · ...  │
└─────────────────────┬────────────────────────────────────────────────┘
                      │  Ingestion: Tree-sitter Ingestion & GAST Normalization
                      ▼
┌──────────────────────────────────────────┐
│         Code Property Graph (CPG)        │  AST + CFG + DFG + Call Graph
└─────────────────────┬────────────────────┘
                      │  Aggregation: Topology Mapping & Semantic Linking
                      ▼
┌──────────────────────────────────────────┐
│    Architecture Knowledge Graph (AKG)    │  GraphJSON (schema v3) · MVCC · atomic commits
└─────────────────────┬────────────────────┘
                      │  Query-based Virtual Subgraph Extraction
                      ▼
┌──────────────────────────────────────────┐
│         Visualization Engine             │  Subgraph → Layout → Mermaid
└─────────────────────┬────────────────────┘
                      │
                      ▼
┌──────────────────────────────────────────┐
│         AI Architect Agent               │  BYOK · Tool-calling · Grounded answers
└──────────────────────────────────────────┘
```

### The Four-Phase Analysis Pipeline

| Phase | Name | What it does |
|---|---|---|
| **Ingestion** | Tree-sitter Ingestion | Parses source files into Concrete Syntax Trees using native Tree-sitter grammars for each language. Supports incremental (git-diff) and full-scan modes. |
| **Normalization** | GAST Normalization | Coerces language-specific CST nodes into a unified **Generic AST (GAST)** — declarations, calls, types, fields — consistent across all 14 languages. Detects I/O primitives (`DATABASE`, `NETWORK_IO`, `DISK_IO`). |
| **Aggregation** | Topology Aggregation | Clusters files into package/directory boundaries, resolves public/private namespace visibility, builds the call-queue and definition index. |
| **Linking** | Semantic Graph Linking | Links calls to target functions (heuristic receiver matching + selector deconstruction), resolves interface implementations, builds CFG/DFG sub-graphs, propagates resource traits, detects concurrency forks. Commits the graph to the AKG via an MVCC transaction. |

### The AKG Database

The graph lives in `.glassmarble/akg.json` — a deterministic, diff-friendly **GraphJSON** document. Nodes and edges are sorted, and edge metadata is preserved directly (e.g., the exact source line of a call):

```json
{
  "nodes": [
    { "id": "auth/login.go::Authenticator::Authenticate", "kind": "FUNCTION",
      "file_spec": { "path": "auth/login.go", "line_start": 18 } }
  ],
  "edges": [
    { "source_id": "auth/login.go::Authenticator::Authenticate",
      "target_id": "db/database.go::DBStore::GetUser",
      "type": "calls", "line_number": 18 }
  ]
}
```

The database layer provides:
- **MVCC** — atomic snapshot swaps for concurrent read safety.
- **Transaction manager** — `db.lock` serializes writers; writes go to `.tmp`, then `os.Rename` (no corrupt half-writes), followed by post-write verification; `--max-json-mb` guards the state-size budget.
- **Self-healing** — repositories created before the v3 store are migrated in place: a legacy `akg_state.ttl` is parsed once, written as `akg.json`, and archived as `akg_state.ttl.bak`.
- **File lock** — `db.lock` prevents concurrent write collisions.

---

## Quick Start

### Prerequisites

- Go 1.22+
- A Git repository to analyze (recommended, not required)

### Build

```bash
git clone https://github.com/Syamchand123/GlassMarble
cd GlassMarble
go build -o gmb.exe main.go       # Windows
go build -o gmb main.go           # Linux / macOS
```

### Initialize & Analyze

```bash
# 1. Initialize the .glassmarble workspace in your repo
gmb init --dir /path/to/your/repo

# 2. Run full analysis — builds the AKG from scratch
gmb analyze --dir /path/to/your/repo --full

# 3. Check AKG health
gmb status --dir /path/to/your/repo
```

### Generate a Diagram

```bash
# Class diagram — saved to .glassmarble/marbles/class.md
gmb visualize class --save class

# C4 Container diagram
gmb visualize c4container --save c4_containers

# Call graph from a specific entry point
gmb visualize callgraph --entry "mypackage/service.go::Service::Handle" --save calls

# Print to stdout without saving
gmb visualize er
```

### Ask the AI Architect

```bash
# One-shot question (configure first with: gmb ai configure)
gmb ai "which services depend on the payment module?"
gmb ai "explain how authentication works"
gmb ai "generate a sequence diagram for the checkout flow" --save checkout_seq.md

# Interactive REPL with persistent session memory
gmb ai chat
```

---

## CLI Reference

### `gmb init`

Initialize the `.glassmarble` workspace directory and default configuration.

```bash
gmb init [--dir <path>]
```

Creates `.glassmarble/`, `.glassmarble/marbles/`, `.glassmarble/intelligence/`, `.glassmarble/snapshots/`, `.glassmarble/memory/`, `.glassmarble/config.yaml`, and an empty GraphJSON v3 `akg.json` state file. Appends `.glassmarble` to `.gitignore` automatically.

---

### `gmb analyze`

Run source-code ingestion and build (or update) the Architecture Knowledge Graph.

```bash
gmb analyze [--dir <path>] [--full] [--workers <n>] [--commit <hash>] [--verbose]
```

| Flag | Default | Description |
|---|---|---|
| `--dir` | `.` | Repository root to analyze |
| `--full` | `false` | Force full re-scan of every file. Default is incremental (git diff). |
| `--workers` | auto | Number of parallel parser worker goroutines |
| `--commit` | (working tree) | Git commit hash to associate with this analysis run. Empty (default) diffs the working tree against HEAD; a hash diffs that commit against its parent |
| `--include-docs` | `false` | Run knowledge fusion (ADR/README/PR claims into developer memory) |
| `--verbose` | `false` | Print phase-by-phase progress |

**Incremental mode**: On git repositories with an existing `akg.json`, GlassMarble runs `git diff HEAD` and only re-parses changed files, merging the delta into the persisted graph. On the first run (no state file) or when `--full` is passed, every file is scanned.

With `--intelligence` (default `true`, human output only), analysis also runs architectural intelligence, persists the result to `.glassmarble/intelligence/latest.json`, stores snapshots in `.glassmarble/snapshots/`, and folds architectural change events into developer memory (`.glassmarble/memory/`). Re-analyzing the same tree is idempotent — events are never duplicated. These phases are non-fatal: failures warn and the graph commit still succeeds.

With `--include-docs` (default `false`, opt-in because doc scanning and git-history walks are not free on large repositories), analysis also runs knowledge fusion: ADR files and READMEs are parsed into knowledge claims, PR/issue references in recent git history become file-level claims, and everything is appended to developer memory — queryable through `gmb memory --ask`. The `fusion:` section of `.glassmarble/config.yaml` tunes the doc globs, technology lexicon and git scan depth; re-analyzing the same tree appends nothing (idempotent).

After analysis, the convention-learning layer refreshes the **project conventions store** (`.glassmarble/memory/conventions.json`): it replays all recorded corrections against the new memory state so every view stays self-corrected, and learns project-wide naming and intent conventions (configurable under `learning:` in `.glassmarble/config.yaml`). Corrections recorded with `gmb memory --correct` are the human feedback loop — see the `gmb memory` section below.

---

### `gmb memory`

Query the developer memory: what do we know about the architecture, when did it change, and why.

```bash
gmb memory [--dir <path>] [--ask "<question>"] [--component <name>] [--json]
          [--correct <target> --kind INTENT|LABEL|STATE|CONFIDENCE|REJECT|ACCEPT
           --value <value> [--reason <text>] [--author <name>]] [--corrections]
```

| Mode | Description |
|---|---|
| (default) | Project overview: event count, component list with temporal states |
| `--ask` | Deterministic ranked retrieval over components, claims, events and timeline (no LLM) |
| `--component` | Longitudinal history of one component (substring match) plus its timeline |
| `--json` | Emit the machine-readable document instead of the human report |
| `--correct` | Record a convention learning correction (component state, event intent, or claim reason). The original value is captured automatically and appended to the `.glassmarble/memory/corrections.jsonl` audit trail |
| `--corrections` | Show the correction audit trail |

Reasons are never invented: every claim is labelled by how it was established — `FACT` (observed from the graph diff), `EXPLICIT_REASON` (stated by a human in a commit/PR/issue/docs), `INFERENCE` (derived by GlassMarble), or `SPECULATION` (low-confidence guess).

Corrections are replayed as an **overlay** on every view (overview, `--ask`, `--component`, `--json`) in recording order — deterministic, idempotent, and untouched by aggregate rebuilds. Corrected entries are flagged in the report so the human view of memory is always the corrected one.

---

### `gmb visualize <type>`

Generate an architecture diagram from the AKG and render it as Mermaid.js markup.

```bash
gmb visualize <diagram_type> [flags]
```

| Flag | Description |
|---|---|
| `--entry <id>` | Entry point node ID for BFS/DFS traversal (required for `sequence`) |
| `--depth <n>` | Maximum traversal depth (default 7) |
| `--unused` | Include nodes with no inbound edges |
| `--scope <value>` | Scope the layout: `global` (default), `folder:<path>`, or `file:<path>` |
| `--link-level <value>` | Graph linkage detail: `architecture` (default), `standard`, or `full` |
| `--format <value>` | Output format: `mermaid` (default), `plantuml`, or `dot` |
| `--save <name>` | Save output to `.glassmarble/marbles/<name>.md` |
| `--output <file>` | Write the diagram to a file instead of stdout |
| `--summary` | Print graph statistics before the diagram |
| `--pagerank` | Enable PageRank computation |
| `--community` | Enable Louvain community detection |
| `--scc` | Enable Tarjan SCC cycle analysis |
| `--max-nodes <n>` | Abort with exit code 4 if the node count exceeds this limit |
| `--changed-files <list>` | Comma-separated changed files for impact analysis |
| `--render <file>` | Render to an image (`.svg`/`.png`) via Kroki or mermaid-cli |
| `--relative` | Render paths relative to the folder root under folder scope |

`gmb visualize list` prints the full 31-type catalog; `gmb visualize check <type>` validates a type name.

See [Diagram Types](#diagram-types) for the full list of `<type>` values.

---

### `gmb status`

Display AKG health and graph statistics.

```bash
gmb status [--dir <path>]
```

```
=== GlassMarble Architecture Knowledge Graph Status ===
  Storage Dir:    .glassmarble
  Schema Version: 3
  Graph Version:  14
  Commit Hash:    a3f9c12e8b7d
  Last Analysis:  2026-08-01T14:00:00+05:30
  Nodes Count:    3847
  Outbound Edges: 12091
  Indexed Files:  214
  Entrypoints:    18
  Virtual Nodes:  421 (10.9%)
  Health Errors:  0 dangling reference(s)
  Storage:        akg.json 2.4 MB
  Verification:   verified (no dangling edges)
  Freshness:      ok
```

---

### `gmb inspect`

Query and explore individual nodes in the AKG without loading the full graph.

```bash
# List all nodes
gmb inspect --list [--dir <path>]

# Search by name or file
gmb inspect --search "UserService" [--dir <path>]

# Show full metadata for a specific node ID
gmb inspect "mypackage/service.go::Service::Handle" [--dir <path>]

# Resolve the node at a specific file and line number
gmb inspect --file service.go --line 42 [--dir <path>]
```

---

### `gmb hotspot`

Rank architectural hotspots by in-degree centrality (most-depended-upon symbols).

```bash
gmb hotspot [--top <n>] [--dir <path>]
```

```
=== Top 10 Architectural Hotspots (Ranked by In-Degree Centrality) ===

Rank  Symbol ID                                      Kind      In-Degree  Out-Degree  Primitive
1     auth/service.go::AuthService::Validate         FUNCTION  47         8           NETWORK_IO
2     db/store.go::PostgresStore::GetUser            FUNCTION  39         3           DATABASE
```

---

### `gmb dependency`

Analyze inbound and outbound dependencies for a file or symbol.

```bash
# Whole-repository dependency summary
gmb dependency [--dir <path>]

# Dependencies of a specific symbol or file path
gmb dependency "auth/service.go" [--dir <path>]
gmb dependency "PostgresStore"   [--dir <path>]
```

Outputs both **direct outbound** (`->`) and **direct inbound** (`<-`) callers with edge type and source line number.

---

### `gmb diff`

Show the architectural delta between the last two committed GraphJSON states.

```bash
gmb diff [--dir <path>]
```

Displays the current and previous commit hash, schema/graph versions, and node/edge delta counts. There is no WAL to replay — the diff is computed directly from the previous committed state.

---

### `gmb doctor`

Run integrity diagnostics on the AKG state database.

```bash
gmb doctor [--dir <path>]
```

Checks:
- **Parse-back integrity** — parses `akg.json` through the canonical parser.
- **Duplicate node IDs**.
- **Dangling edges** — edges pointing to non-existent nodes.
- **Version reporting** — schema/graph version, commit hash, and node/edge counts.

Exits non-zero if any check fails. Run this after a crash or to validate a fresh analysis.

---

### `gmb hooks`

Install a Git `post-commit` hook to automatically re-analyze after every commit.

```bash
gmb hooks install    # installs .git/hooks/post-commit
gmb hooks uninstall  # removes the hook
```

---

### `gmb watch`

Continuously poll the repository for changes and trigger incremental analysis.

```bash
gmb watch [--dir <path>] [--interval 5s]
```

---

### `gmb housekeeping`

Report and prune the `.glassmarble` working set (saved diagrams, AI sessions, snapshots).

```bash
gmb housekeeping                          # report disk usage only
gmb housekeeping --prune                  # delete files older than 30 days
gmb housekeeping --prune --older-than 7   # 7-day retention window
```

The AKG state file (`akg.json`) is **never** pruned by this command.

---

### `gmb ai` — AI Architect Agent

A grounded, Bring-Your-Own-Key AI agent that answers questions by querying the live AKG — not guessing.

```bash
# One-time setup
gmb ai configure
gmb ai configure --provider openai --model gpt-4o --key sk-...
gmb ai configure --provider anthropic --model claude-sonnet-4-5 --key sk-ant-...

# One-shot questions
gmb ai "how does authentication work?"
gmb ai "which services depend on UserService?"
gmb ai "generate a C4 container diagram" --save c4.md

# Interactive REPL with persistent session memory
gmb ai chat
gmb ai chat --new              # fresh session
gmb ai sessions                # list all saved sessions

# Tool filtering and spending guardrails
gmb ai --tools akg,code "question"
gmb ai --no-tools "opinion question"
gmb ai --max-total-tokens 20000 "question"
gmb ai --max-cost 0.50 "question"

# Diagnostics
gmb ai doctor
gmb ai models
```

#### Supported Providers

| Provider | Adapter | Key Environment Variable |
|---|---|---|
| `openai` | OpenAI-compatible | `GLASSMARBLE_OPENAI_API_KEY` |
| `anthropic` | Native Claude | `GLASSMARBLE_ANTHROPIC_API_KEY` |
| `gemini` | Native Gemini | `GLASSMARBLE_GEMINI_API_KEY` |
| `deepseek` | OpenAI-compatible | `GLASSMARBLE_DEEPSEEK_API_KEY` |
| `mistral` | OpenAI-compatible | `GLASSMARBLE_MISTRAL_API_KEY` |
| `groq` | OpenAI-compatible | `GLASSMARBLE_GROQ_API_KEY` |
| `openrouter` | OpenAI-compatible | `GLASSMARBLE_OPENROUTER_API_KEY` |
| `nvidia` | OpenAI-compatible | `GLASSMARBLE_NVIDIA_API_KEY` |
| `glm` | OpenAI-compatible | `GLASSMARBLE_GLM_API_KEY` |
| `ollama` | OpenAI-compatible | *(none — local)* |
| `custom` | OpenAI-compatible | `GLASSMARBLE_AI_API_KEY` |

The OpenAI-compatible adapter works with any endpoint speaking the chat-completions wire format — point `--base-url` at it.

#### AI Tool Catalog

| Group | Tools |
|---|---|
| **System** | `system_status`, `system_diagram_types`, `save_artifact` |
| **AKG Queries** | `akg_status`, `akg_summary`, `akg_search`, `akg_get_node`, `akg_edges`, `akg_traverse`, `akg_path`, `akg_cycles`, `akg_orphans`, `akg_god_objects`, `akg_hotspots`, `akg_page_rank`, `akg_impact_radius`, `akg_communities`, `akg_articulation_points`, `akg_topological_order`, `akg_entrypoints`, `akg_similarity` |
| **AKG Intelligence** | `query_architecture_memory`, `get_architecture_timeline`, `get_architecture_patterns` |
| **Code** | `code_read_file`, `code_list_dir`, `code_search_symbol`, `code_definition`, `code_diff` |
| **Diagrams** | `diagram_generate` (all 31 types), `diagram_summary`, `diagram_types` |

---

## Diagram Types

Generated diagrams are saved as Mermaid.js markdown to `.glassmarble/marbles/`.

### UML Diagrams (14)

| Command | Description |
|---|---|
| `class` | Type hierarchies, fields, methods, and inheritance |
| `object` | Instance relationships and compositions |
| `component` | Module and component boundaries |
| `deployment` | Infrastructure and deployment topology |
| `package` | Package and namespace dependencies |
| `composite` | Internal structure of components |
| `profile` | Stereotype and constraint extensions |
| `usecase` | Actor and feature interaction |
| `activity` | Control flow and business process flows |
| `state` | State machines and transitions |
| `sequence` | Ordered inter-component message flows *(requires `--entry`)* |
| `communication` | Collaboration and message links |
| `interaction` | High-level interaction overview fragments |
| `timing` | Time-constrained state changes |

### C4 Model Diagrams (7)

| Command | Description |
|---|---|
| `c4context` | System and external actor relationships |
| `c4container` | Services, databases, and runtime containers |
| `c4component` | Internal component decomposition |
| `c4code` | Class-level implementation detail |
| `c4landscape` | Multi-system landscape overview |
| `c4dynamic` | Dynamic interaction flows |
| `c4deployment` | Infrastructure deployment environment |

### Specialized Diagrams (4)

| Command | Description |
|---|---|
| `er` | Entity-relationship model |
| `dataflow` | Data movement across system boundaries |
| `mindmap` | Hierarchical concept structure |
| `flowchart` | General-purpose process flow |

### Analysis Diagrams (6)

| Command | Description |
|---|---|
| `dependency` | Import and package dependency tree |
| `hotspot` | High-coupling and complexity heatmap |
| `callgraph` | Function-level call chain traversal |
| `layered` | Architectural tier separation |
| `impact` | Blast radius of a code change |
| `infrastructure` | External systems, databases, and messaging |

---

## Supported Languages

GlassMarble uses native [Tree-sitter](https://tree-sitter.github.io/) grammars for each language:

| Language | Extensions |
|---|---|
| Go | `.go` |
| Python | `.py` |
| JavaScript | `.js`, `.mjs` |
| TypeScript | `.ts`, `.tsx` |
| Java | `.java` |
| C | `.c`, `.h` |
| C++ | `.cpp`, `.cc`, `.cxx`, `.hpp` |
| C# | `.cs` |
| Rust | `.rs` |
| Ruby | `.rb` |
| PHP | `.php` |
| HTML | `.html`, `.htm` |
| CSS | `.css` |
| JSON | `.json` |

Kotlin, Swift, and Scala are additionally registered with **declaration-only** support (types, functions, and fields are indexed; no intra-method CFG/DFG). See `docs/supported_languages.txt` for details.

---

## Configuration

See [docs/configuration.md](docs/configuration.md) for the complete reference — core keys, sub-module configs (`intelligence:`, `fusion:`, `learning:`, `aging:`), and every environment variable.

### `.glassmarble/config.yaml`

```yaml
root_dir: .
debug: false
output_format: mermaid
max_file_bytes: 2097152   # 2 MB — files larger than this are skipped
worker_count: 0           # 0 = auto (GOMAXPROCS)
storage_dir: .glassmarble # storage directory name
include_hidden: false     # include dotfiles/dot-directories in scans
```

### AI Configuration (`.glassmarble/ai.yaml` or `~/.glassmarble/ai.yaml`)

```yaml
provider: openai
model: gpt-4o
api_key: ""                   # prefer environment variables
base_url: ""                  # optional: custom endpoint override
temperature: 0.2
max_turns: 15                 # tool-call rounds per run
max_tool_result_bytes: 8192   # per-tool result truncation
max_output_tokens: 8192
timeout_sec: 180
stream: true                  # token streaming (default on)
max_total_tokens: 0           # per-run token budget (0 = unlimited)
max_cost_usd: 0               # per-run cost cap in USD (0 = unlimited)
max_session_messages: 40      # chat history rolling window
```

Config precedence: **CLI flag > `GLASSMARBLE_*` environment variable > project config (`.glassmarble/config.yaml`, `.glassmarble/ai.yaml`) > global config (`~/.glassmarble/`) > defaults**

Environment variables mirror all fields: `GLASSMARBLE_AI_PROVIDER`, `GLASSMARBLE_AI_MODEL`, `GLASSMARBLE_AI_API_KEY`, `GLASSMARBLE_AI_BASE_URL`, `GLASSMARBLE_AI_TEMPERATURE`, `GLASSMARBLE_AI_MAX_TURNS`, `GLASSMARBLE_AI_STREAM` (`0`/`false` to disable), `GLASSMARBLE_AI_MAX_TOTAL_TOKENS`, `GLASSMARBLE_AI_MAX_COST`, `GLASSMARBLE_AI_MAX_SESSION_MESSAGES`. Provider-specific keys use `GLASSMARBLE_<PROVIDER>_API_KEY`.

---

## Workspace Layout

After `gmb init` and `gmb analyze`:

```
your-repo/
├── .glassmarble/
│   ├── akg.json             # Primary AKG — GraphJSON schema v3 (source of truth)
│   ├── akg.json.v<ver>.bak  # Pre-migration backups (v1/v2 → v3)
│   ├── db.lock              # Ephemeral write-lock token (deleted after commit)
│   ├── config.yaml          # GlassMarble project configuration
│   ├── ai.yaml              # AI engine BYOK configuration
│   ├── marbles/             # Saved diagram markup (.md files)
│   ├── intelligence/        # latest.json — last architecture-intelligence run
│   ├── snapshots/           # index.json + snap_<hash8>.json full-state snapshots
│   ├── memory/              # Developer memory: events/claims/corrections.jsonl,
│   │                        # conventions.json, memory.json, timeline.json
│   └── ai/
│       └── sessions/        # Persistent AI chat sessions (.json, 0600 permissions)
```

---

## Internal Architecture

```
internal/
├── akg/                          # Database & transaction layer (GraphJSON schema v3)
│   ├── mvcc.go                   # Multi-Version Concurrency Control
│   ├── transaction_manager.go    # Atomic commits, db.lock, post-write verification
│   ├── graph_json.go             # Canonical GraphJSON serialization
│   ├── schema_v3.go              # Schema v3 shape & stale-kind folding
│   ├── migrate.go                # AutoMigrateOnLoad: v1/v2 → v3 (+ legacy TTL self-heal)
│   ├── vocabulary.go             # RelationshipType → gm: predicate mapping
│   ├── reasoner.go               # Topological macro-inference rules
│   ├── incremental.go            # Delta-transaction merging
│   ├── doctor.go                 # RunDoctor integrity diagnostics
│   └── neo4j_export.go           # Deterministic Cypher export
│
├── code_analysis_engine/
│   ├── ingest/                   # Tree-sitter ingestion; git-delta support
│   ├── normalize/                # GAST normalization; 17-language translators
│   ├── aggregate/                # Topology mapping; namespace clustering
│   └── link/                     # Semantic linking; CPG construction; graph commit
│       ├── call_linker.go        # Nested selector call resolution
│       ├── interface_linker.go   # Duck-type interface matching (Go, TS, etc.)
│       ├── cfg_linker.go         # Control-flow graph construction
│       ├── dfg_linker.go         # Data-flow graph construction
│       └── primitive_reasoner.go # DATABASE / NETWORK_IO trait propagation
│
├── visualization_engine/
│   ├── core.go                   # Engine coordinator; 31-type registry
│   ├── diagrams.go               # Canonical diagram catalog & entry rules
│   ├── projection/               # Virtual subgraph extraction (projector, entrypoint)
│   ├── view/                     # ViewSpec → DiagramSpec builders + extractors
│   └── adapters/                 # Mermaid / PlantUML / DOT renderers
│
├── arch_intelligence/            # Metrics, pattern detection (PR-01..PR-07)
├── arch_timeline/                # Architecture event timeline
├── archmodel/                    # ArchEvent kinds, snapshots, stats
├── developer_memory/             # Memory store (claims, corrections, JSONL)
├── knowledge_aging/              # Aging pipeline (internal phase)
├── knowledge_fusion/             # Doc fusion (--include-docs)
├── learning/                     # Convention learning & correction overlay
├── commit_reasoning/             # Post-commit reasoning (internal phase)
├── config/                       # Layered config: flags > env > yaml > defaults
├── drift/                        # Working-tree vs committed-graph drift
├── evidence/                     # Confidence & provenance metadata
├── terminal/  logger/  errors/   # Terminal UX, logging, typed exit codes
│
└── ai_engine/
    ├── engine.go                 # Provider-agnostic message loop
    ├── provider/                 # LLM adapters: OpenAI-compat, Anthropic, Gemini, ...
    └── tools/                    # system_tools, akg_tools, code_tools,
                                  # diagram_tools, memory/pattern/timeline tools

cmd/
├── analyze.go      visualize.go   memory.go      ai.go
├── inspect.go      dependency.go  hotspot.go     patterns.go
├── timeline.go     why.go         stats.go       snapshot.go
├── diff.go         status.go      doctor.go      drift.go
├── housekeeping.go export.go      import.go      compare.go
├── tree.go         hooks.go       watch.go       dev.go
├── version.go      completion.go  init.go        root.go
└── (aging.go, fusion.go, learning.go, evolution.go, code.go,
     commit_reasoning_llm.go, memory_pipeline.go — internal pipeline phases)
```

---

## Global Flags

Available on every command:

| Flag | Description |
|---|---|
| `--root-dir <path>` | Override the repository root directory |
| `--debug` | Enable verbose debug logging |
| `--verbose`, `-v` | Print detailed phase-by-phase output |
| `--max-json-mb <n>` | Refuse to load or commit a GraphJSON AKG file larger than N MiB (0 = unlimited) |

---

## License

See [LICENSE](LICENSE).
