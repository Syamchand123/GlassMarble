# GlassMarble CLI Master Reference (`gmb`)

`gmb` is the unified command-line interface for the GlassMarble Architectural Knowledge Graph (AKG) and Diagram Visualization Engine.

---

## Global Flags & Environment Variables

These flags apply across all commands:

| Flag | Type | Default | Description |
|---|---|---|---|
| `--dir` | string | `.` | Target repository directory to inspect or analyze (alias: `--root-dir`) |
| `--color` | string | `auto` | Color output control: `auto`, `always`, or `never` (respects `NO_COLOR`) |
| `--quiet` | bool | `false` | Suppress non-error console output |
| `--debug` | bool | `false` | Enable detailed debug logging |
| `-c, --config` | string | `""` | Path to custom configuration YAML file |

---

## Exit Codes Reference

GlassMarble uses deterministic semantic exit codes for reliable CI/CD automation:

| Exit Code | Classification | Meaning | Typical Trigger |
|---|---|---|---|
| `0` | **Success** | Command finished successfully | Normal execution; uninitialized status check |
| `1` | **General Error** | Runtime exception or unclassified filesystem failure | Missing file; permission error |
| `2` | **Usage Error** | Invalid flags or syntax error | Unrecognized flag; missing required argument |
| `3` | **Scope / Target Error** | Scoped query or entrypoint symbol not found | `gmb visualize sequence --entry nonExistent` |
| `4` | **Integrity Failure** | Doctor check failed or benchmark budget exceeded | Dangling reference detected; benchmark overrun |

---

## Command Groups Overview (28 Commands)

### 1. Analyze & Index Commands
- `gmb init [--dir <path>] [--json]` — Initialize the `.glassmarble` workspace.
- `gmb analyze [--dir <path>] [--full] [--commit <hash>] [--intelligence] [--include-docs] [--json]` — Run the 4-phase parsing pipeline and commit the AKG.
- `gmb watch [--dir <path>]` — Live file watcher with automatic incremental re-analysis.
- `gmb hooks install|uninstall [--dir <path>]` — Manage git post-commit hook for automated re-analysis.

### 2. Inspect & Query Commands
- `gmb status [--dir <path>] [--json]` — AKG database metrics, graph version, and health status.
- `gmb doctor [--dir <path>] [--json]` — Deep integrity diagnostics: parse-back, duplicate IDs, dangling edges.
- `gmb diff [--dir <path>] [--json]` — Inspect committed transaction log and pending graph mutations.
- `gmb tree [--dir <path>] [--depth <n>] [--json]` — Render architectural directory & symbol hierarchy.
- `gmb dependency [symbol] [--dir <path>] [--json]` — Analyze inbound callers and outbound dependencies.
- `gmb hotspot [--dir <path>] [--top <n>] [--json]` — In-degree centrality ranking of architectural hubs.
- `gmb inspect [node_id] [--dir <path>] [--search <q>] [--list] [--languages] [--json]` — Search symbols, list entry points, or view node details.
- `gmb stats [--dir <path>] [--arch] [--bench] [--json]` — Pipeline telemetry, component coupling (Ca/Ce), and benchmark metrics.

### 3. Architecture Governance Commands
- `gmb drift [--dir <path>] [--json]` — Verify architecture drift against defined layering and cycle budgets.
- `gmb compare [base.json head.json] | --dir <path> [--json]` — Structural delta diff between two graph snapshots.
- `gmb snapshot --create|--list|--at <ref>|--diff <base> <head>|--replay` — Point-in-time architecture snapshots.
- `gmb timeline [--component <name>] [--from <ref>] [--to <ref>] [--format text|json|mermaid]` — Longitudinal architecture timeline.

### 4. Visualization Commands
- `gmb visualize <diagram_type> [flags]` — Generate 31 architecture diagram types in Mermaid, PlantUML, or DOT.
- `gmb visualize list` — List all 31 supported diagram types across UML, C4, and Specialized families.
- `gmb visualize check <type>` — Validate if graph contains sufficient data for a specific diagram type.

### 5. AI & Memory Commands
- `gmb ai "<question>" [--dir <path>] [--save <file>] [--streaming] [--verbose]` — One-shot grounded architecture Q&A.
- `gmb ai chat [--dir <path>] [--session <id>]` — Interactive full-screen terminal chat REPL with persistent session memory.
- `gmb ai configure` — Interactive configuration wizard for LLM provider and API keys.
- `gmb ai models` — List available AI providers (Claude, OpenAI, Ollama, Gemini, DeepSeek, OpenRouter).
- `gmb ai doctor` — Validate AI engine configuration, network connectivity, and AKG presence.
- `gmb ai sessions [--list|--prune]` — Manage persistent chat conversation sessions.
- `gmb memory [--ask "<q>"] [--component <name>] [--correct <target>] [--corrections] [--json]` — Query developer memory and record convention corrections.
- `gmb why "<question>"` — Quick architectural reasoning query.

### 6. Utility & Configuration Commands
- `gmb import <graph.json> [--dir <path>]` — Import and activate an external AKG GraphJSON snapshot.
- `gmb export [--format graphjson|neo4j] -o <file>` — Export graph to GraphJSON or Neo4j Cypher script.
- `gmb housekeeping [--prune] [--older-than <days>] [--json]` — Inspect and clean diagram artifacts and chat sessions.
- `gmb completion bash|zsh|fish|powershell` — Generate shell autocompletion scripts.
- `gmb version` — Display branded version, commit hash, build date, and toolchain info.