<p align="center">
  <img src="./assets/GM_logo.png" width="180" alt="GlassMarble">
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
  <a href="#quick-start">Quick Start</a> ·
  <a href="#cli-reference">CLI Reference</a> ·
  <a href="#diagram-types">Diagram Types</a>
</p>

---

## What is GlassMarble?

Modern codebases suffer from **documentation drift**: architecture diagrams and dependency maps are created during design and become obsolete the moment code changes. GlassMarble eliminates this permanently.

GlassMarble compiles your source code — across **14 languages** — into a semantic **Architecture Knowledge Graph (AKG)** stored as a portable GraphJSON database (`.glassmarble/akg.json`). From this living graph it can:

- **Generate 31 architecture diagrams** (UML + C4 model) as Mermaid.js markup — always synchronized with your latest code.
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
                      │  Stage 1–2: Tree-sitter Ingestion & GAST Normalization
                      ▼
┌──────────────────────────────────────────┐
│         Code Property Graph (CPG)        │  AST + CFG + DFG + Call Graph
└─────────────────────┬────────────────────┘
                      │  Stage 3–4: Topology Mapping & Semantic Linking
                      ▼
┌──────────────────────────────────────────┐
│    Architecture Knowledge Graph (AKG)    │  W3C RDF-star Turtle · MVCC · WAL
└─────────────────────┬────────────────────┘
                      │  SPARQL-like Virtual Subgraph Extraction
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

### The Four-Stage Analysis Pipeline

| Stage | Name | What it does |
|---|---|---|
| **Stage 1** | Tree-sitter Ingestion | Parses source files into Concrete Syntax Trees using native Tree-sitter grammars for each language. Supports incremental (git-diff) and full-scan modes. |
| **Stage 2** | GAST Normalization | Coerces language-specific CST nodes into a unified **Generic AST (GAST)** — declarations, calls, types, fields — consistent across all 14 languages. Detects I/O primitives (`DATABASE`, `NETWORK_IO`, `DISK_IO`). |
| **Stage 3** | Topology Aggregation | Clusters files into package/directory boundaries, resolves public/private namespace visibility, builds the call-queue and definition index. |
| **Stage 4** | Semantic Graph Linking | Links calls to target functions (heuristic receiver matching + selector deconstruction), resolves interface implementations, builds CFG/DFG sub-graphs, propagates resource traits, detects concurrency forks. Commits the graph to the AKG via an MVCC transaction. |

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
- **WAL** — Write-Ahead Logging to `.glassmarble/wal/` for crash durability.
- **Atomic file swaps** — writes go to `.tmp`, then `os.Rename` — no corrupt half-writes.
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

Creates `.glassmarble/`, `.glassmarble/marbles/`, `.glassmarble/config.yaml`, and an empty `akg.json` state file. Appends `.glassmarble` to `.gitignore` automatically.

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
| `--verbose` | `false` | Print stage-by-stage progress |

**Incremental mode**: On git repositories with an existing `akg.json`, GlassMarble runs `git diff HEAD` and only re-parses changed files, merging the delta into the persisted graph. On the first run (no state file) or when `--full` is passed, every file is scanned.

With `--stage5` (default `true`, human output only), analysis also runs architectural intelligence, persists the result to `.glassmarble/intelligence/latest.json`, stores snapshots in `.glassmarble/snapshots/`, and folds architectural change events into developer memory (`.glassmarble/memory/`). Re-analyzing the same tree is idempotent — events are never duplicated. These stages are non-fatal: failures warn and the graph commit still succeeds.

---

### `gmb memory`

Query the developer memory: what do we know about the architecture, when did it change, and why.

```bash
gmb memory [--dir <path>] [--ask "<question>"] [--component <name>] [--json]
```

| Mode | Description |
|---|---|
| (default) | Project overview: event count, component list with temporal states |
| `--ask` | Deterministic ranked retrieval over components, claims, events and timeline (no LLM) |
| `--component` | Longitudinal history of one component (substring match) plus its timeline |
| `--json` | Emit the machine-readable document instead of the human report |

Reasons are never invented: every claim is labelled by how it was established — `FACT` (observed from the graph diff), `EXPLICIT_REASON` (stated by a human in a commit/PR/issue/docs), `INFERENCE` (derived by GlassMarble), or `SPECULATION` (low-confidence guess).

---

### `gmb visualize <type>`

Generate an architecture diagram from the AKG and render it as Mermaid.js markup.

```bash
gmb visualize <diagram_type> [flags]
```

| Flag | Description |
|---|---|
| `--entry <id>` | Entry point node ID for BFS/DFS traversal (required for `sequence`) |
| `--depth <n>` | Maximum traversal depth |
| `--unused` | Include nodes with no inbound edges |
| `--scope <value>` | Scope to a package path or folder boundary |
| `--save <name>` | Save output to `.glassmarble/marbles/<name>.md` |
| `--summary` | Print graph statistics before the diagram |
| `--pagerank` | Enable PageRank computation |
| `--community` | Enable Louvain community detection |
| `--scc` | Enable Tarjan SCC cycle analysis |

See [Diagram Types](#diagram-types) for the full list of `<type>` values.

---

### `gmb status`

Display AKG health, graph statistics, and WAL freshness.

```bash
gmb status [--dir <path>]
```

```
=== GlassMarble Architecture Knowledge Graph Status ===
  Storage Dir:    .glassmarble
  Schema Version: 2
  Graph Version:  14
  Commit Hash:    a3f9c12e8b7d
  Last Analysis:  2026-08-01T14:00:00+05:30
  Nodes Count:    3847
  Outbound Edges: 12091
  Indexed Files:  214
  Entrypoints:    18
  Virtual Nodes:  421 (10.9%)
  Health Errors:  0 dangling reference(s)
  Storage:        TTL 2.4 MB | WAL 0.0 KB
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

Show architectural mutations recorded in the Write-Ahead Log.

```bash
gmb diff [--dir <path>]
```

Replays WAL transactions to display commit hash, node/edge delta counts, and modified files per transaction. A clean database (WAL truncated after the last atomic write) reports no pending transactions.

---

### `gmb doctor`

Run integrity diagnostics on the AKG state database.

```bash
gmb doctor [--dir <path>]
```

Checks:
- **Parse-back integrity** — parses `akg.json` through the canonical parser.
- **Ontology conformance** — flags any undeclared `gm:` terms.
- **Dangling edges** — edges pointing to non-existent nodes.
- **Duplicate node IDs**.
- **WAL freshness** — uncommitted WAL entries newer than the TTL.

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

Report and prune the `.glassmarble` working set (saved diagrams, AI sessions, WAL segments).

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
gmb ai configure --provider anthropic --model claude-3-5-sonnet-20241022 --key sk-ant-...

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

### Specialized Diagrams (10)

| Command | Description |
|---|---|
| `er` | Entity-relationship model |
| `dataflow` | Data movement across system boundaries |
| `mindmap` | Hierarchical concept structure |
| `flowchart` | General-purpose process flow |
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

---

## Configuration

### `.glassmarble/config.yaml`

```yaml
root_dir: .
debug: false
output_format: mermaid
max_file_bytes: 2097152   # 2 MB — files larger than this are skipped
worker_count: 0           # 0 = auto (GOMAXPROCS)
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

Config precedence: **CLI flag > environment variable > project `ai.yaml` > global `ai.yaml` > defaults**

Environment variables mirror all fields: `GLASSMARBLE_AI_PROVIDER`, `GLASSMARBLE_AI_MODEL`, `GLASSMARBLE_AI_API_KEY`, `GLASSMARBLE_AI_BASE_URL`, `GLASSMARBLE_AI_TEMPERATURE`, `GLASSMARBLE_AI_MAX_TURNS`, `GLASSMARBLE_AI_STREAM` (`0`/`false` to disable), `GLASSMARBLE_AI_MAX_TOTAL_TOKENS`, `GLASSMARBLE_AI_MAX_COST`, `GLASSMARBLE_AI_MAX_SESSION_MESSAGES`. Provider-specific keys use `GLASSMARBLE_<PROVIDER>_API_KEY`.

---

## Workspace Layout

After `gmb init` and `gmb analyze`:

```
your-repo/
├── .glassmarble/
│   ├── akg.json             # Primary AKG — GraphJSON (source of truth)
│   ├── db.lock              # Ephemeral write-lock token (deleted after commit)
│   ├── config.yaml          # GlassMarble project configuration
│   ├── ai.yaml              # AI engine BYOK configuration
│   ├── wal/                 # Write-Ahead Log segments (durability)
│   ├── marbles/             # Saved diagram markup (.md files)
│   └── ai/
│       └── sessions/        # Persistent AI chat sessions (.json, 0600 permissions)
```

---

## Internal Architecture

```
internal/
├── akg/                          # Database & transaction layer
│   ├── mvcc.go                   # Multi-Version Concurrency Control
│   ├── wal.go                    # Write-Ahead Log
│   ├── transaction_manager.go    # Atomic commits, file locking, self-healing recovery
│   ├── turtle_serializer.go      # W3C RDF-star Turtle serialization
│   ├── reasoner.go               # Topological macro-inference rules
│   └── ontology.ttl              # RDF schema: classes, predicates, axioms
│
├── code_analysis_engine/
│   ├── stage1/                   # Tree-sitter ingestion; git-delta support
│   ├── stage2/                   # GAST normalization; 14 language translators
│   ├── stage3/                   # Topology mapping; namespace clustering
│   └── stage4/                   # Semantic linking; CPG construction; graph commit
│       ├── call_linker.go        # Nested selector call resolution
│       ├── interface_linker.go   # Duck-type interface matching (Go, TS, etc.)
│       ├── cfg_linker.go         # Control-flow graph construction
│       ├── dfg_linker.go         # Data-flow graph construction
│       └── primitive_reasoner.go # DATABASE / NETWORK_IO trait propagation
│
├── visualization_engine/
│   ├── visualizer.go             # Engine coordinator (3-stage pipeline)
│   ├── stage1/extractor.go       # SPARQL-like virtual subgraph extraction + BFS
│   ├── stage2/aggregator.go      # Layout tree; Tarjan SCC cycle detection
│   └── stage3/mermaid.go         # Mermaid.js markup renderer (31 diagram types)
│
└── ai_engine/
    ├── engine.go                 # Public facade: New / Ask / AskAgent
    ├── provider/                 # LLM adapters: OpenAI-compat, Anthropic, Gemini
    ├── agent/loop.go             # Tool-calling loop; token/cost budgets; streaming
    ├── tools/                    # akg_tools, code_tools, diagram_tools, system_tools
    ├── akgbridge/                # Lazy, cached AKG snapshot loader
    ├── session/                  # Persistent chat sessions
    └── aiconfig/                 # BYOK config: yaml + env + defaults

cmd/
├── analyze.go      init.go       status.go
├── visualize.go    inspect.go    hotspot.go
├── dependency.go   diff.go       doctor.go
├── hooks.go        watch.go      housekeeping.go
└── ai.go           root.go
```

---

## Global Flags

Available on every command:

| Flag | Description |
|---|---|
| `--root-dir <path>` | Override the repository root directory |
| `--debug` | Enable verbose debug logging |
| `--verbose`, `-v` | Print detailed stage-by-stage output |
| `--max-ttl-mb <n>` | Refuse to load or commit an AKG file larger than N MiB (0 = unlimited) |

---

## License

See [LICENSE](LICENSE).
