# GlassMarble — AI Architecture Intelligence Platform

> **The repository is the single source of truth. The architecture graph follows it.**

**GlassMarble** continuously compiles any codebase into a living **Architecture Knowledge Graph (AKG)**, keeps it synchronized with every commit, and layers a grounded AI Architect, 31 synchronized diagram types, architecture intelligence, drift governance, and developer memory on top — so engineering teams never work from an outdated picture of their system again.

| | |
|---|---|
| **Version** | v0.1.0 |
| **Interface** | Single CLI binary: `gmb` |
| **Runtime** | Go 1.22+ · Windows / Linux / macOS |
| **Languages analyzed** | 17 (14 full tree-sitter grammars + 3 declaration-only) |
| **Diagram types** | 31 (UML · C4 · specialized · analysis) |
| **Graph store** | Portable GraphJSON (`.glassmarble/akg.json`, schema v3) |
| **AI** | Bring-Your-Own-Key · 11 providers · 32 agent tools |

---

## Table of Contents

1. [Product Overview](#1-product-overview)
2. [The Problem We Solve](#2-the-problem-we-solve)
3. [How It Works](#3-how-it-works)
4. [Platform Features](#4-platform-features)
5. [The GlassMarble CLI at a Glance](#5-the-glassmarble-cli-at-a-glance)
6. [The `.glassmarble` Workspace](#6-the-glassmarble-workspace)
7. [Configuration](#7-configuration)
8. [Use Cases](#8-use-cases)
9. [Benefits & Outcomes](#9-benefits--outcomes)
10. [Getting Started](#10-getting-started)
11. [Vision](#11-vision)
12. [Documentation](#12-documentation)

---

## 1. Product Overview

GlassMarble is a self-evolving **AI Architecture Intelligence Platform** for software engineering teams. It performs deep static analysis of source code across 17 programming languages, compiles the results into a canonical **Architecture Knowledge Graph (AKG)** stored as a portable, diff-friendly GraphJSON document, and continuously synchronizes that graph with the repository as it evolves.

Unlike traditional documentation tools — which generate diagrams or documents once and immediately begin to rot — GlassMarble treats the repository itself as the source of truth. Every analysis run (triggered manually, by a `post-commit` hook, or by watch mode) re-ingests only what changed, merges the delta into the persisted graph, and refreshes every derived artifact: diagrams, architecture intelligence, event timelines, snapshots, developer memory, and AI answers.

The result is a **living architectural layer** that sits alongside the codebase: an always-current, searchable, explainable, and trustworthy representation of the entire software system — the definitive architectural source of truth for every repository it manages.

---

## 2. The Problem We Solve

### 2.1 Documentation drift

Architecture diagrams, dependency maps, and design documents are created during design phases and become obsolete the moment code changes. Keeping them current by hand is impossible at scale — so teams stop trusting them, or stop writing them.

### 2.2 Lost architectural knowledge

When senior engineers leave, when teams onboard new members, or when a codebase is handed over, the *why* behind the structure — boundaries, intent, decisions, conventions — evaporates. New developers must reverse-engineer thousands of files to answer "how does authentication work?"

### 2.3 The AI-code churn problem

AI-generated code accelerates the pace of change. When a machine can commit more code in a day than a team wrote in a week, human-maintained architecture understanding falls even further behind. Understanding the *current* system becomes the bottleneck.

### 2.4 Static analysis vs. hallucination

Generic AI assistants answer from probabilities; they invent plausible-sounding architectures that do not exist. Teams need answers grounded in the actual implementation — retrieved from a structured graph of real facts, not from an LLM's imagination.

> **GlassMarble's answer:** a continuously synchronized architecture graph — the single foundation for documentation, diagrams, analysis, governance, and grounded AI reasoning.

---

## 3. How It Works

### 3.1 The four-phase analysis pipeline

```text
Source Code (17 languages)
        │
        ▼
┌──────────────────────────────────────────────────────────────────┐
│ 1. INGEST      Tree-sitter parsing → Concrete Syntax Trees       │
│                (parallel worker pool)                            │
├──────────────────────────────────────────────────────────────────┤
│ 2. NORMALIZE   Coercion into a unified Generic AST (GAST) —      │
│                declarations, calls, types, fields, imports       │
├──────────────────────────────────────────────────────────────────┤
│ 3. AGGREGATE   Topology mapping — packages, visibility,          │
│                definitions index                                 │
├──────────────────────────────────────────────────────────────────┤
│ 4. LINK        Semantic graph linking — calls, interfaces        │
│                (duck-typing), control flow (CFG), data flow      │
│                (DFG), concurrency forks, resource primitives     │
│                (DATABASE, NETWORK_IO, DISK_IO)                   │
└──────────────────────────────┬───────────────────────────────────┘
        │
        ▼
┌──────────────────────────────────────────────────────────────────┐
│ Architecture Knowledge Graph (AKG)                               │
│ Committed atomically as GraphJSON (schema v3)                    │
└──────────────────────────────┬───────────────────────────────────┘
        │
        ▼
Post-commit layers: Architecture Intelligence → Timeline & Memory → Snapshots
        │
        ▼
Consumers: 31 diagram types · AI Architect · drift gates · exports
```

### 3.2 Incremental by design

- **Delta analysis** — on git repositories, `gmb analyze` runs `git diff HEAD` and re-parses only changed files, merging the delta into the persisted graph. Lightweight and fast.
- **Full scans** — first runs and the `--full` flag rebuild the graph from scratch at the full linker detail.
- **Linker detail levels** — `architecture` (default), `standard` (aggregate CFG), and `full` (per-branch CFG + DFG) let teams trade depth for speed.
- **Continuous modes** — `gmb hooks install` adds a Git `post-commit` hook that re-analyzes after every commit; `gmb watch` monitors the filesystem (fsnotify, debounce) and re-analyzes on change, recovering from lock contention via the last good state.

### 3.3 Storage: a portable, durable graph

- **Canonical store** — `.glassmarble/akg.json`: deterministic, sorted GraphJSON (schema v3). Every node carries file/line provenance; every edge carries the exact source line and confidence.
- **MVCC + atomic commits** — multi-version concurrency control with snapshot swaps, a `db.lock` write lock, temp-file + atomic-rename writes, and post-write verification (byte parity + zero-dangling guard).
- **Size governance** — the `--max-json-mb` budget refuses to load or commit oversized state; benchmark gates validate analysis (≤ 20 s), commit (≤ 8 s), and state size (≤ 12 MB) budgets.
- **Self-healing** — repositories created on earlier storage formats are migrated in place (v1/v2 → v3) with automatic backups, including one-time conversion of the legacy `akg_state.ttl` store.

---

## 4. Platform Features

### 4.1 Multi-language code analysis (17 languages)

| Full tree-sitter grammars | Declaration-only support |
|---|---|
| Go, Python, JavaScript, TypeScript, Java, C, C++, C#, Rust, Ruby, PHP, HTML, CSS, JSON (config-format) | Kotlin, Swift, Scala |

Every language is normalized into the same Generic AST, producing identical node kinds, edge types, and query surfaces regardless of source language.

### 4.2 Architecture Knowledge Graph (AKG)

The AKG is the canonical representation of the repository: services, modules, packages, classes, interfaces, functions, fields, parameters, files, external APIs and SDKs, plus the relationships between them — calls, ownership, composition, inheritance, interfaces, data flow, control flow, concurrency, and security-relevant access. Every fact carries:

- **Provenance** — exact file path and line spans.
- **Confidence** — `1.0` for AST-observed facts, graded lower for heuristic inference.
- **Verification** — `gmb doctor` re-parses the state and checks for duplicate IDs and dangling references.

### 4.3 31 synchronized diagram types

Generated from the live graph — never hand-drawn, never stale — in **Mermaid, PlantUML, or DOT**, saved to `.glassmarble/marbles/`:

| Family | Diagram types |
|---|---|
| **UML (14)** | class, object, component, deployment, package, composite, profile, usecase, activity, state, sequence, communication, interaction, timing |
| **C4 model (7)** | c4context, c4container, c4component, c4code, c4landscape, c4dynamic, c4deployment |
| **Specialized (4)** | er, dataflow, mindmap, flowchart |
| **Analysis (6)** | dependency, hotspot, callgraph, layered, impact, infrastructure |

Diagrams support scoping (`global`, `folder:<path>`, `file:<path>`), link-level detail, entry-driven extraction, node limits, and advanced graph analytics (PageRank, Louvain communities, Tarjan SCC cycles). `gmb visualize list` catalogs all types; `--render` rasterizes to SVG/PNG.

### 4.4 Architecture Intelligence

After every commit, GlassMarble analyzes the graph and emits evidence-based insights:

- **Pattern detection (PR-01..PR-07)** — Layered Architecture, Clean Architecture, Microservices, Bounded Context (DDD), CQRS, Event-Driven, Repository Pattern — each with a confidence score.
- **Smell detection** — god objects, cyclic dependencies, god packages, tight coupling, unstable abstraction, dead code.
- **Component inference** — logical components via Louvain community detection + directory analysis, with afferent/efferent coupling (Ca/Ce) and Instability metrics.
- **Architectural change events (23 kinds)** — `SERVICE_ADDED/REMOVED/SPLIT/MERGED`, `DEPENDENCY_ADDED/REMOVED`, `PATTERN_DETECTED/REMOVED`, `SMELL_DETECTED/RESOLVED`, `CYCLE_INTRODUCED/RESOLVED`, `COUPLING_INCREASED/DECREASED`, `LAYER_VIOLATION`, `STATE_CHANGE`, and more — each with commit hash, evidence, and affected components.
- **Layer compliance** — layering consistency scoring against declared architecture tiers.

### 4.5 Developer memory, timeline & conventions

- **Knowledge claims** labelled by how they were established — `FACT`, `EXPLICIT_REASON` (human-stated), `INFERENCE`, `SPECULATION`. Reasons are never invented.
- **Human feedback loop** — corrections (`INTENT`, `LABEL`, `STATE`, `CONFIDENCE`, `REJECT`, `ACCEPT`) replay as a deterministic overlay on every memory view and feed convention learning.
- **Knowledge aging** — claims and components decay by evidence-source half-life (code, docs, git, LLM); components transition through states (`CURRENT → EXPERIMENTAL → DEPRECATED → REMOVED → HISTORICAL`) with stale-grace protection and state-change pinning.
- **Project conventions** — deterministic learning of naming patterns, layer directories, and ADR locations into `.glassmarble/memory/conventions.json`.
- **Timeline** — queryable, filterable event history with text, JSON, and Mermaid timeline output; snapshots (`--create/--list/--at/--diff/--replay`) capture point-in-time architecture state and can replay and render historical graphs.

### 4.6 Drift detection & architecture governance

`gmb drift` turns architecture rules into automated checks: declared layers (path-glob partitioned), forbidden cross-layer dependencies, and a cycle budget. Drift violations produce exit-code failures — making architecture review a mandatory, machine-checked PR gate instead of a human chore.

### 4.7 Diagnostics & observability

- **`gmb status`** — schema/graph version, commit, node/edge counts, verification state.
- **`gmb doctor`** — parse-back integrity, duplicate node IDs, dangling edges.
- **`gmb diff`** — commit/schema/graph version and node/edge deltas between states.
- **`gmb stats`** — pipeline telemetry, benchmark budget status, architecture health (Ca/Ce/Instability).
- **`gmb hotspot`** — top depended-upon symbols ranked by in-degree centrality.
- **`gmb dependency` / `gmb tree` / `gmb inspect`** — inbound/outbound dependencies, directory trees, node lookup by ID/search/file/line.

### 4.8 The AI Architect Agent (BYOK)

A grounded, agentic AI architect that answers questions by querying the live AKG, source tree, and developer memory — not by guessing:

- **11 providers** — OpenAI, Anthropic, Gemini, DeepSeek, Mistral, GLM, NVIDIA, OpenRouter, Groq, Ollama (local), and any OpenAI-compatible custom endpoint — with config precedence (flags > env > project > global > defaults) and cost/token guardrails.
- **32 tools across 4 categories** — 18 `akg_*` graph tools (status, search, paths, cycles, communities, impact radius, hotspots, topological order, …), AKG intelligence tools (memory, timeline, patterns), 5 code tools (read file, list dir, symbol search, definition, diff), 3 diagram tools (generate any of the 31 types, summaries, type catalog), plus system tools.
- **Grounding** — responses are built from retrieved graph facts and source context, with evidence — minimizing hallucination.
- **Conversational surface** — `gmb ai "<question>"` one-shot, `gmb ai chat` REPL with persistent sessions, `gmb ai configure` wizard, `gmb ai models` / `gmb ai doctor` diagnostics, `gmb why "<question>"` evidence-retrieval Q&A.
- **Artifacts on demand** — generate and save diagrams (to `marbles/`) and engineering notes (to `ai/`) directly from conversation.

### 4.9 Interoperability

- **Neo4j export** — deterministic Cypher scripts (`GMNode:<Kind>` labels) for enterprise graph platforms.
- **GraphJSON export / import / compare** — portable graph exchange for CI promotion and offline review.
- **Shell completion** — bash, zsh, fish, PowerShell.
- **Machine-readable JSON output** — across report commands for automation.

---

## 5. The GlassMarble CLI at a Glance

28 commands, one binary (`gmb`, v0.1.0), with a consistent global flag surface (`--dir`, `--root-dir`, `--debug`, `--verbose`, `--max-json-mb`):

| Area | Commands |
|---|---|
| **Lifecycle** | `init`, `analyze`, `watch`, `hooks`, `housekeeping`, `completion`, `version` |
| **Query** | `status`, `inspect`, `dependency`, `tree`, `hotspot`, `memory`, `timeline`, `patterns`, `stats`, `diff`, `doctor`, `why` |
| **Visualize** | `visualize` (31 types), `snapshot` (create/list/at/diff/replay) |
| **Govern** | `drift` |
| **Exchange** | `export` (graphjson/neo4j), `import`, `compare` |
| **AI** | `ai` (chat, configure, models, doctor, sessions) |
| **Engineering** | `dev` (golden-file rebasing) |

Exit codes are contractual and CI-friendly:

| Code | Meaning |
|---|---|
| `0` | Success |
| `1` | Validation / other error |
| `2` | Entry missing / not found |
| `3` | Empty subgraph |
| `4` | Render / node-limit or benchmark budget exceeded |

---

## 6. The `.glassmarble` Workspace

```text
<repo>/.glassmarble/
├── akg.json                  # Architecture Knowledge Graph (GraphJSON, schema v3) — source of truth
├── akg.json.v<ver>.bak       # Pre-migration backups
├── config.yaml               # Project configuration (core + intelligence/fusion/learning/aging/drift)
├── ai.yaml                   # AI engine BYOK configuration (0600)
├── db.lock                   # Transaction write lock
├── marbles/                  # Saved diagram markup
├── intelligence/latest.json # Latest architecture-intelligence run
├── snapshots/                # Point-in-time architecture snapshots + index
├── memory/                   # events/claims/corrections JSONL, conventions, memory & timeline aggregates
└── ai/sessions/              # Persistent AI chat sessions (0600)
```

---

## 7. Configuration

Everything is tunable with a strict precedence chain:

> **CLI flags > `GLASSMARBLE_*` environment variables > project config (`.glassmarble/config.yaml`, `.glassmarble/ai.yaml`) > global config (`~/.glassmarble/`) > defaults**

- **Core** — worker count, max file size, output format, storage dir, hidden-file handling.
- **Intelligence** — detection thresholds for god objects, cycles, god packages, layering consistency, PageRank parameters, snapshot retention.
- **Fusion** — ADR/README globs, technology lexicon, git-scan depth (controls `--include-docs` knowledge fusion).
- **Learning** — correction overlay behavior, convention extraction, minimum evidence.
- **Aging** — freshness half-lives per evidence source, state-transition windows, stale-grace period.
- **Drift** — layers, forbidden dependencies, cycle budget — the architecture governance contract.
- **AI** — provider, model, temperature, tool budgets, token/cost caps, session history.

---

## 8. Use Cases

### 8.1 Onboarding at scale

New team members get an always-current map of the system instead of months of file archaeology. "How does authentication work?" is answered from the live graph — in seconds, with evidence.

### 8.2 Impact analysis before change

Before touching an API or refactoring a module, engineers see the full blast radius: reverse dependencies, affected services, hotspot coupling, and impact diagrams (`visualize impact`, `--changed-files`).

### 8.3 Architecture governance in CI

`gmb drift` with declared layers and forbidden dependencies fails the build on violations; cycle budgets keep the architecture honest; `gmb doctor` gates state integrity after every crash or manual edit.

### 8.4 Technical debt management

Continuous smell detection (dead code, cycles, god objects, tight coupling) plus hotspot ranking give engineering managers a quantified, evidence-backed debt dashboard that updates with every commit.

### 8.5 Refactoring & migration planning

Pattern detection identifies the architecture actually in use (Microservices vs. modular monolith vs. Clean Architecture); timeline and snapshots show how the architecture has been evolving — enabling data-driven refactoring decisions.

### 8.6 AI-assisted engineering

The AI Architect answers architecture questions, explains subsystems, generates diagrams on demand, and produces grounded documentation — all bound by the live graph, so the answers match the code that exists today, not the code that used to exist.

### 8.7 Multi-language and monorepo visibility

One normalized graph across 17 languages means cross-language dependency questions ("what calls into this Java service from the Go gateways?") are answered uniformly.

### 8.8 Audit, handover & enterprise integration

Deterministic Neo4j exports, portable GraphJSON, and JSON machine output make GlassMarble usable as the architectural layer inside larger governance and analytics stacks.

---

## 9. Benefits & Outcomes

| Challenge | Outcome with GlassMarble |
|---|---|
| Documentation drift | Every diagram and document regenerates from the live graph — always current |
| Slow onboarding | Self-serve, grounded answers to "how does X work?" |
| Untrustworthy AI answers | Reasoning over real graph facts with provenance and confidence |
| Invisible technical debt | Automated, quantified smell and hotspot detection |
| Architecture erosion | Machine-checked drift gates with forbidden dependencies |
| Change risk | Evidence-based impact analysis before every significant change |
| Knowledge loss on turnover | Persistent developer memory with claims, reasons, and corrections |
| Scale of modern codebases | Incremental, git-aware analysis; bounded state; benchmark budgets |

---

## 10. Getting Started

**Prerequisites:** Go 1.22+ and a Git repository (recommended).

```bash
# Build
go build -o gmb main.go

# Initialize, analyze, verify
gmb init --dir <repo>
gmb analyze --dir <repo>
gmb status --dir <repo>

# Visualize
gmb visualize class --save class          # → .glassmarble/marbles/class.md
gmb visualize sequence --entry "main"     # entry-driven call flows

# Intelligence
gmb patterns --smells
gmb timeline --full
gmb snapshot --create

# Governance
gmb drift                                # exits non-zero on violations

# AI Architect (BYOK)
gmb ai configure --provider openai --model gpt-4o --key sk-...
gmb ai "which services depend on the payment module?"
gmb ai chat
```

---

## 11. Vision

GlassMarble's long-term objective is to establish a new category of developer tooling centered on **continuous architecture intelligence** rather than static documentation. As software grows and AI-generated code accelerates change, maintaining accurate architectural understanding becomes the critical bottleneck. GlassMarble is engineered as the continuously synchronized architectural layer that transforms raw source code into an always-current, searchable, explainable, and trustworthy representation of the entire software system — enabling developers to spend less time understanding existing systems and more time building new capabilities with confidence.

---

## 12. Documentation

| Document | Contents |
|---|---|
| [`architecture.md`](architecture.md) | Master architecture & implementation manual |
| [`commands_master_reference.md`](commands_master_reference.md) | Full CLI reference (all 28 commands) |
| [`ai.md`](ai.md) | AI Architect guide (providers, tools, guardrails) |
| [`diagrams.md`](diagrams.md) | The 31 diagram types in detail |
| [`configuration.md`](configuration.md) | Complete configuration reference |
| [`architecture_intelligence.md`](architecture_intelligence.md) | Intelligence, memory, timeline, snapshots |
| [`akg_format.md`](akg_format.md) | GraphJSON state format & migration |
| [`relationship_types.md`](relationship_types.md) | Edge predicate taxonomy |
| [`neo4j.md`](neo4j.md) | Neo4j export recipes |
| [`supported_languages.txt`](supported_languages.txt) | Language support matrix |