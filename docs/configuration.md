# GlassMarble Configuration Reference

This document is the canonical reference for every configuration surface of GlassMarble: the core `config.yaml`, the AI engine's `ai.yaml`, environment variables, and the sub-module sections (`intelligence:`, `fusion:`, `learning:`, `aging:`, `drift:`).

---

## 1. Precedence

Every setting resolves with the same rule — **highest wins**:

```
CLI flags
  > GLASSMARBLE_* environment variables
  > project config   (.glassmarble/config.yaml  |  .glassmarble/ai.yaml)
  > global config    (~/.glassmarble/config.yaml | ~/.glassmarble/ai.yaml)
  > built-in defaults
```

Notes:

* Project files live in the repository's `.glassmarble/` directory; global files live in `~/.glassmarble/`.
* The global config acts as a base layer: any key absent from the project file falls back to it.
* For the AI engine there are two generic keys — `GLASSMARBLE_AI_API_KEY` and `GLASSMARBLE_OLLAMA_BASE_URL` — plus one provider-specific key per provider (`GLASSMARBLE_OPENAI_API_KEY`, `GLASSMARBLE_ANTHROPIC_API_KEY`, ...).
* Quirk: the root command accepts a `--config` flag, but it is **not wired** to `config.Load`; per-command flags (`--dir`, `--workers`, `--output-format`, ...) are the intended override path.

---

## 2. Core Configuration — `.glassmarble/config.yaml`

`gmb init` creates a minimal file:

```yaml
root_dir: .
debug: false
output_format: mermaid
max_file_bytes: 2097152
```

### 2.1 Keys & defaults

| Key | Default | Description |
|---|---|---|
| `root_dir` | `.` | Repository root that analysis operates on. |
| `worker_count` | `4` | Parallel parser worker goroutines. `0` on the CLI flag means "auto (CPUs)". |
| `max_file_bytes` | `10485760` (10 MiB) | Files larger than this are skipped. Note: `gmb init` writes `2097152` (2 MiB). |
| `debug` | `false` | Verbose debug logging. |
| `storage_dir` | `.glassmarble` | Storage directory name created at the repo root. |
| `output_format` | `mermaid` | Default diagram output: `mermaid`, `plantuml`, or `dot`. |
| `include_hidden` | `false` | Include dotfiles/dot-directories in scans. |
| `drift` | (empty) | Drift invariants — see §3. |
| `intelligence` | (defaults) | Architecture intelligence thresholds — see §4. |
| `fusion` | (defaults) | Knowledge fusion settings — see §5. |
| `learning` | (defaults) | Convention learning settings — see §6. |
| `aging` | (defaults) | Knowledge aging settings — see §7. |

### 2.2 Environment variables (core)

| Variable | Overrides |
|---|---|
| `GLASSMARBLE_ROOT_DIR` | `root_dir` |
| `GLASSMARBLE_WORKER_COUNT` | `worker_count` |
| `GLASSMARBLE_MAX_FILE_BYTES` | `max_file_bytes` |
| `GLASSMARBLE_DEBUG` | `debug` |
| `GLASSMARBLE_STORAGE_DIR` | `storage_dir` |
| `GLASSMARBLE_OUTPUT_FORMAT` | `output_format` |
| `GLASSMARBLE_INCLUDE_HIDDEN` | `include_hidden` |

---

## 3. `drift:` — Architecture Invariants

Checked by `gmb drift`. All keys optional; when absent, drift reports only cycles.

```yaml
drift:
  layers:
    - name: api
      paths: ["cmd/**", "internal/app/**"]
    - name: domain
      paths: ["internal/**"]
    - name: data
      paths: ["internal/db/**"]
  forbidden_deps:
    - source: data
      target: api
      reason: "data layer must not depend on the API layer"
  cycle_budget: 0        # max tolerated inter-layer cycles (0 = any cycle fails)
```

| Key | Description |
|---|---|
| `layers` | Named path-glob partitions; each node is assigned to the first layer whose pattern matches its file path. |
| `forbidden_deps` | Directed `source -> target` layer dependencies that are not allowed. |
| `cycle_budget` | Maximum tolerated cycles between layers. Non-positive means any cycle fails. |

---

## 4. `intelligence:` — Architecture Intelligence Thresholds

All keys optional; zeros fall back to `DefaultIntelligenceConfig()`.

| Key | Default | Description |
|---|---|---|
| `god_object_fan_in_threshold` | `15` | Fan-in above which a node is a candidate god object. |
| `god_object_method_threshold` | `30` | Method count above which a class is a candidate god object. |
| `small_cycle_threshold` | `3` | Cycle-size boundary between small and large cycles. |
| `large_cycle_threshold` | `5` | Size above which a cycle is "large". |
| `god_package_traffic_pct` | `40.0` | % of package traffic routed through one symbol to flag a god package. |
| `layered_consistency_threshold` | `0.80` | Minimum layering-consistency score for PR-07 compliance. |
| `event_edge_pct` | `15.0` | Edge-share threshold for event-driven detection. |
| `coupling_change_pct` | `0.20` | Coupling delta (20%) that triggers a `COUPLING_*` event. |
| `llm_intent_enabled` | `false` | Enable LLM-based intent labeling of events. |
| `snapshot_no_graph` | `false` | Write snapshots without embedding the full graph (see storage contract — auto-enables when `snapshot_auto_threshold_*` is exceeded). |
| `snapshot_max_count` | `30` | Max snapshots retained; oldest pruned on `gmb analyze` / `gmb snapshot --create` (RCA-1 retention). |
| `snapshot_auto_threshold_nodes` | `15000` | Auto-enable `snapshot_no_graph` when node count ≥ this (RCA-1). |
| `snapshot_auto_threshold_mb` | `8` | Auto-enable `snapshot_no_graph` when estimated state ≥ this MB (RCA-1). |
| `page_rank_iterations` | `100` | PageRank iterations. |
| `page_rank_damping` | `0.85` | PageRank damping factor. |
| `arch_layers` | (empty) | Optional layering for PR-07 / `gmb stats --arch`; empty degrades to root grouping. |
| `arch_excluded_dirs` | (empty) | Directory prefixes skipped by component inference (vendored/generated code). |
| `node_count_threshold` | `2000` | Graph size above which analytics switch to iterative algorithms. |
| `unstable_threshold` | `0.8` | Component is unstable when Instability > this. |
| `stable_components_threshold` | `0.9` | Share of component weight required for a snapshot to be "stable". |
| `snapshot_ttl_seconds` | `3600` | Cache validity of snapshots. |
| `snapshot_num_pages` | `10` | Recent graph pages retained per snapshot source. |
| `run_rules` | (empty) | Subset of `patterns`, `smells`, `events` to run; empty = all. |

---

## 5. `fusion:` — Knowledge Fusion (`gmb analyze --include-docs`)

Whether fusion runs at all is decided by the `--include-docs` flag (opt-in), not by this section. All keys optional; zeros fall back to `DefaultFusionConfig()`.

| Key | Default | Description |
|---|---|---|
| `adr_globs` | `docs/adr/**/*.md`, `docs/adr/*.md`, `docs/decisions/**/*.md`, `docs/decisions/*.md`, `docs/**/adr/**/*.md`, `**/adr-*.md` | File globs (repo-relative, `**` allowed) matched for ADR parsing. |
| `readme_files` | `README.md`, `README.MD`, `docs/README.md` | README files parsed for technology mentions. |
| `tech_lexicon` | (built-in) | Extra technology names beyond the built-in lexicon (Redis, PostgreSQL, Kafka, gRPC, Docker, Kubernetes, ...). Matching is case-insensitive, word-boundary based. |
| `include_git_sources` | `true` | Extract PR/issue claims from git history. Tri-state: explicit `false` disables. |
| `max_commits` | `500` | Most-recent commits the git adapter scans per run (bounds cost). |
| `doc_max_size_bytes` | `1048576` (1 MiB) | Doc files larger than this are skipped. |
| `exclusive_predicates` | `state`, `status`, `version`, `deployed_on` | Single-valued predicates per subject; conflicting objects are contradictions resolved by source reliability. All other predicates are multi-valued. |

---

## 6. `learning:` — Convention Learning

All keys optional; zeros fall back to `DefaultLearningConfig()`.

| Key | Default | Description |
|---|---|---|
| `apply_on_query` | `true` | Overlay recorded corrections onto memory query results (corrections are always persisted regardless). Tri-state. |
| `conventions_enabled` | `true` | Run deterministic convention extraction during `gmb analyze` (naming patterns, layer dirs, ADR locations → `.glassmarble/memory/conventions.json`). Tri-state. |
| `min_convention_evidence` | `2` | Minimum occurrences before a convention is reported. |

---

## 7. `aging:` — Knowledge Aging

All keys optional; zeros fall back to `DefaultAgingConfig()`.

| Key | Default | Description |
|---|---|---|
| `enabled` | `true` | Turn the aging pass on/off. Tri-state. |
| `code_half_life_days` | `365` | Freshness half-life for claims evidenced by code or user corrections. |
| `docs_half_life_days` | `270` | Half-life for documentation-sourced claims. |
| `git_half_life_days` | `180` | Half-life for git-history / heuristic-derived claims. |
| `llm_half_life_days` | `90` | Half-life for LLM-inferred claims (fastest decay). |
| `default_half_life_days` | `180` | Fallback half-life for unclassified evidence sources. |
| `deprecation_to_historical_days` | `180` | Days a component must stay DEPRECATED before becoming HISTORICAL. |
| `experimental_promotion_events` | `3` | Minimum confirming events before EXPERIMENTAL is promoted to CURRENT. |
| `stale_grace_days` | `7` | Days a component may be absent from snapshots before aging may transition it away from CURRENT/EXPERIMENTAL. Explicit `0` disables the grace period. Tri-state. |

---

## 8. AI Engine — `.glassmarble/ai.yaml` / `~/.glassmarble/ai.yaml`

Created and edited with `gmb ai configure` (`--scope global|project`). Written with 0600 permissions (may contain an API key).

| Key | Default | Description |
|---|---|---|
| `provider` | `openai` | Provider: `openai`, `anthropic`, `gemini`, `deepseek`, `mistral`, `glm`, `nvidia`, `openrouter`, `groq`, `ollama`, `custom`. |
| `model` | `gpt-4o` | Model identifier. |
| `api_key` | (empty) | Prefer environment variables. |
| `base_url` | (provider default) | Custom endpoint override. |
| `temperature` | `0.2` | Sampling temperature (`0` = provider default on CLI). |
| `max_turns` | `15` | Tool-call rounds per run. |
| `max_tool_result_bytes` | `8192` | Per-tool result truncation. |
| `max_output_tokens` | `8192` | Completion token cap. |
| `timeout_sec` | `180` | HTTP timeout. |
| `stream` | `true` | Token streaming. |
| `max_total_tokens` | `0` | Per-run token budget (`0` = unlimited). |
| `max_cost_usd` | `0` | Per-run cost cap (`0` = unlimited). |
| `max_session_messages` | `40` | Chat history rolling window. |

Environment variables mirror these fields: `GLASSMARBLE_AI_PROVIDER`, `GLASSMARBLE_AI_MODEL`, `GLASSMARBLE_AI_API_KEY`, `GLASSMARBLE_AI_BASE_URL`, `GLASSMARBLE_AI_TEMPERATURE`, `GLASSMARBLE_AI_MAX_TURNS`, `GLASSMARBLE_AI_MAX_TOOL_RESULT_BYTES`, `GLASSMARBLE_AI_MAX_OUTPUT_TOKENS`, `GLASSMARBLE_AI_TIMEOUT_SEC`, `GLASSMARBLE_AI_STREAM` (`0`/`false` disables), `GLASSMARBLE_AI_MAX_TOTAL_TOKENS`, `GLASSMARBLE_AI_MAX_COST`, `GLASSMARBLE_AI_MAX_SESSION_MESSAGES`. Provider-specific keys use `GLASSMARBLE_<PROVIDER>_API_KEY`; Ollama's endpoint is `GLASSMARBLE_OLLAMA_BASE_URL`.

API key resolution for a provider: explicit `api_key` in config > `GLASSMARBLE_<PROVIDER>_API_KEY` > `GLASSMARBLE_AI_API_KEY`.

---

## 9. Complete Example

```yaml
root_dir: .
worker_count: 4
max_file_bytes: 10485760
debug: false
storage_dir: .glassmarble
output_format: mermaid
include_hidden: false

drift:
  layers:
    - name: api
      paths: ["cmd/**"]
    - name: internal
      paths: ["internal/**"]
  forbidden_deps:
    - source: internal
      target: api
  cycle_budget: 0

intelligence:
  god_object_fan_in_threshold: 15
  god_object_method_threshold: 30
  layered_consistency_threshold: 0.80
  snapshot_ttl_seconds: 3600
  snapshot_num_pages: 10
  snapshot_no_graph: false
  snapshot_max_count: 30
  snapshot_auto_threshold_nodes: 15000
  snapshot_auto_threshold_mb: 8
  run_rules: ["patterns", "smells", "events"]

fusion:
  adr_globs: ["docs/adr/**/*.md", "**/adr-*.md"]
  readme_files: ["README.md", "docs/README.md"]
  tech_lexicon: ["ClickHouse", "Temporal"]
  include_git_sources: true
  max_commits: 500
  doc_max_size_bytes: 1048576

learning:
  apply_on_query: true
  conventions_enabled: true
  min_convention_evidence: 2

aging:
  enabled: true
  code_half_life_days: 365
  docs_half_life_days: 270
  git_half_life_days: 180
  llm_half_life_days: 90
  default_half_life_days: 180
  deprecation_to_historical_days: 180
  experimental_promotion_events: 3
  stale_grace_days: 7
```