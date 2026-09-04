# GlassMarble Model Context Protocol (MCP) Server

> Connect your AI coding assistants, IDEs, and autonomous agents directly to the live Architecture Knowledge Graph (AKG).

GlassMarble includes a native **Model Context Protocol (MCP)** server (`gmb mcp`) compliant with the current MCP specification (negotiating versions from `2024-11-05` to `2025-11-25`). 

Instead of relying on LLMs hallucinating repository structure or grepping thousands of files, the MCP server provides agents with deterministic, compiler-grounded queries, blast radius simulations, cycle detections, and architectural memory.

---

## 1. Quick Start

### Start in Stdio Mode (Default for CLI tools)
```bash
gmb mcp
```

### Start in Streamable HTTP Mode (for Remote or IDE connections)
```bash
gmb mcp --transport http --port 8765 --host 127.0.0.1 --auth-token "my-secret-token"
```

### Export Client Configuration
GlassMarble can automatically generate ready-to-paste configuration blocks:
```bash
gmb mcp --print-config claude-code     # Claude Code CLI config
gmb mcp --print-config claude-desktop  # Claude Desktop JSON
gmb mcp --print-config cursor          # Cursor IDE mcp.json
gmb mcp --print-config windsurf        # Windsurf Cascade config
```

---

## 2. Transports & Protocols

| Transport | Flag | Endpoint | Best For |
|---|---|---|---|
| **stdio** | `--transport stdio` | stdin / stdout | Claude Code CLI, local sub-processes, command-line agents |
| **Streamable HTTP** | `--transport http` | `http://<host>:<port>/mcp` | Modern web agents, Cursor, Windsurf, multi-client daemons |
| **SSE (Legacy)** | `--transport sse` | `http://<host>:<port>/sse` | Legacy SSE clients requiring persistent event streams |

### Lifecycle & Deadlines
- **Context-Aware Stdio Shutdown:** Cleanly unblocks I/O when stdin terminates or when SIGINT/SIGTERM is caught.
- **Per-Tool Execution Timeouts:** Prevent runaway queries with `--tool-timeout` (default: `60s`).
- **Loopback Default:** HTTP/SSE transports bind to `127.0.0.1` by default to prevent accidental local network exposure.

---

## 3. Security & Sandboxing

- **Bearer Token Authentication:** When `--auth-token <secret>` is provided, all HTTP/SSE requests must provide `Authorization: Bearer <secret>`. Verification is performed using `subtle.ConstantTimeCompare` to eliminate timing attacks.
- **Roots-Aware Path Sandboxing:** File and directory arguments accessed via tools are strictly validated against the repository root directory, forbidding directory traversal attacks (`../`).
- **Read-Only Enforcement:** Operating with `--read-only` (default: `true`) guarantees that tool executions inspect and query the AKG without mutating project files.

---

## 4. Tools Catalog

The MCP server dynamically registers 30+ architectural intelligence tools:

### AKG Inspection & Topology
| Tool Name | Parameters | Description |
|---|---|---|
| `akg_query` | `query`, `limit` | Execute semantic and structural queries across all symbols and dependencies |
| `akg_get_node` | `id` | Retrieve detailed metadata, properties, and call sites for a specific symbol |
| `akg_find_dependents` | `id`, `depth` | Find all incoming callers and upstream dependent nodes |
| `akg_find_dependencies`| `id`, `depth` | Find all outbound dependencies called by a node |
| `akg_get_neighbors` | `id` | Get 1-hop closed neighborhood (inbound and outbound) |

### Impact & Refactoring Analysis
| Tool Name | Parameters | Description |
|---|---|---|
| `analyze_impact` | `id`, `changed_files` | Calculate blast radius, affected downstream files, and impacted test suites |
| `trace_call_path` | `source`, `target` | Find shortest direct or transitive call paths between two symbols |
| `find_dead_code` | — | Identify unreachable code units based on resolved entrypoints |
| `detect_cycles` | — | Detect dependency cycles and strongly connected components (SCC) |
| `find_cut_vertices` | — | Find architectural articulation points whose failure partitions the system |
| `get_hotspots` | `limit` | Rank symbols by PageRank centrality and in-degree coupling |

### Architecture Governance & Drift
| Tool Name | Parameters | Description |
|---|---|---|
| `check_architecture_drift` | `since` | Compare current architecture against layer rules or historical baselines |
| `verify_invariants` | — | Check rule compliance (e.g. `domain` cannot depend on `infrastructure`) |
| `list_architectural_smells`| — | Retrieve structural debt findings (God components, circular dependencies) |

### Memory & Timeline
| Tool Name | Parameters | Description |
|---|---|---|
| `query_developer_memory` | `query`, `component` | Search knowledge claims, ADR decisions, and rationale records |
| `get_architecture_timeline`| `since`, `limit` | Inspect chronological evolution events (`SERVICE_ADDED`, `CYCLE_RESOLVED`) |

---

## 5. Resources Catalog

The MCP server supports dual URI schemes: `gmb://` and `glassmarble://`:

| URI | Content Type | Content |
|---|---|---|
| `gmb://status` | `application/json` | Graph health, node/edge counts, storage breakdown, verification state |
| `gmb://intelligence` | `application/json` | Detected design patterns, structural smells, and top hotspots |
| `gmb://timeline` | `application/json` | Architecture evolution events and commit diff annotations |
| `gmb://memory` | `application/json` | Active knowledge claims, developer rationale, and recorded conventions |
| `gmb://rules` | `application/yaml` | Layering invariants, forbidden dependency constraints, and cycle budgets |
| `gmb://config` | `application/yaml` | Active `.glassmarble/config.yaml` settings |

---

## 6. Prompt Templates

GlassMarble provides built-in prompt templates to guide AI models through architectural workflows:

* **`explain_architecture`**: Guides the AI to synthesize high-level patterns, core bounded contexts, and entrypoints.
* **`analyze_impact`**: Prompts the AI to perform risk assessment before refactoring or removing a component.
* **`find_technical_debt`**: Surfaces structural smells, cyclic dependencies, and articulation bottlenecks.
* **`ci_gate_check`**: Formats an automated architectural compliance review for pull requests.
* **`onboard_developer`**: Generates a developer onboarding walkthrough tailored to the repository's real topology.

---

## 7. Client Integration Examples

### Claude Code (`~/.claude/config.json` or project config)
```json
{
  "mcpServers": {
    "glassmarble": {
      "command": "gmb",
      "args": ["mcp", "--read-only"]
    }
  }
}
```

### Cursor (`.cursor/mcp.json`)
```json
{
  "mcpServers": {
    "glassmarble": {
      "command": "gmb",
      "args": ["mcp", "--read-only"]
    }
  }
}
```

### Windsurf (`~/.codeium/windsurf/mcp_config.json`)
```json
{
  "mcpServers": {
    "glassmarble": {
      "command": "gmb",
      "args": ["mcp"]
    }
  }
}
```

### Streamable HTTP Setup (Remote / Containerized)
```json
{
  "mcpServers": {
    "glassmarble": {
      "url": "http://127.0.0.1:8765/mcp",
      "headers": {
        "Authorization": "Bearer YOUR_SECRET_TOKEN"
      }
    }
  }
}
```
