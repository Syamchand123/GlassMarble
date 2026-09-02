# Changelog

All notable changes to the GlassMarble Architectural Intelligence Platform are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

---

## [Unreleased] — v1.1.0

### Changed
- **Visualization Server (`gmb ui`) — web UI rebuilt from scratch**:
  - The shipped `app.js` had a top-level SyntaxError, so the previous UI never executed (no tabs, no graph, no data fetches). Rebuilt as a minimal, professional dev-tool front end: system fonts (fully offline — no Google Fonts import), light/dark themes, responsive layout (the old CSS had zero media queries), ARIA tab/combobox semantics, keyboard shortcuts, and empty states with actionable hints.
  - All server data is HTML-escaped and inline `onclick` handlers removed (the old templates interpolated unescaped symbol names into `innerHTML`).
  - Client now reads the real API schemas (smells: `kind`/`severity`/`affected_ids`/`evidence`/`suggestion`), populates the Metrics tab from `/api/intelligence` (previously hardcoded placeholders), and lazy-loads the 3.3 MB `mermaid.min.js` only when the Diagrams tab opens.
  - Graph tab: adaptive layout fallback so force layout never freezes on 5k+ node graphs; cycles/cut-vertices/PageRank/smells overlays now explain themselves when the current view has no matching nodes.
  - Server: fixed nil-pointer dereference in `/api/status` on nil graph; added `Cache-Control` for embedded assets (previously re-downloaded ~3.7 MB per reload) and `X-Content-Type-Options: nosniff`.
- **MCP Server (`gmb mcp`) — protocol and hardening overhaul**:
  - `--transport http` now serves the modern **Streamable HTTP** transport at `/mcp` (previously it silently aliased to deprecated SSE, which remains available via `--transport sse`). HTTP/SSE bind to `127.0.0.1` by default (new `--host` flag; previously bound to all interfaces).
  - **Bearer token auth is now actually enforced** for HTTP/SSE with constant-time comparison and 401 + `WWW-Authenticate` (previously it was logged as "ENABLED" but never checked).
  - Protocol version is negotiated per session against the SDK's supported list (2024-11-05 through 2025-11-25); the server previously claimed a pinned 2024-11-05 while negotiating newer revisions.
  - Capability honesty: `resources.subscribe` is no longer advertised (there are no update emitters); cursor pagination added for tools/resources/prompts lists; `gmb_status` and `gmb_server_info` now return `structuredContent` with a text fallback.
  - Per-tool execution timeout (default 60s, `--tool-timeout`); context-aware stdio shutdown via `StdioServer.Listen` (no more 5s race that closed the bridge under in-flight handlers); path sandboxing consolidated into a single roots-aware guard (paths were validated twice by near-identical middlewares).
  - Removed ~1,500 lines of dead parallel implementation (`protocol.go`, `transport.go`, `registry.go`, `handlers/`, `resources/`, `prompts/`) whose error-code tests asserted constants no live code used; JSON-RPC error-code tests now assert the live SDK path.
  - Fixed: `--storage-dir` config was computed then ignored; port default mismatch (8088 flag vs 8765 config); numeric tool arguments sent as strings were silently dropped; cancellation check in evidence scanning was a no-op (`break` inside `select`); `slog` no longer hijacks the global logger for every `gmb` subcommand via package `init()`.
- **Diagram engine — C4 diagrams render again**:
  - Mermaid's C4 layouter crashes on any `Rel()` targeting a boundary block (`System_Boundary`/`Enterprise_Boundary`/`Deployment_Node`), which the edge-aggregation fallback produced — C4 container diagrams of real repositories failed to render. Aggregated edges now bind to the folded `Container`/`System` element (still relatable) and `renderC4Edges` refuses boundary-block endpoints. Golden fixtures rebaselined (`c4_container.mmd`, `c4_deployment.mmd`).

---

## [v1.0.1] - 2026-08-29

### Added
- **New `gmb update` Self-Update CLI Command**:
  - Automatically checks GitHub Releases for newer versions, verifies SHA256 integrity against `checksums.txt`, and updates the local executable in place.
  - Native OS and architecture auto-detection across Linux (`amd64`, `arm64`), macOS (`amd64`, `arm64`), and Windows (`amd64`, `arm64`).
  - Cross-platform atomic replacement strategy: resolves Windows file-locking via temporary `.old` renaming, and executes atomic replacement with `0755` permissions on POSIX systems.
  - Comprehensive flags: `--check` (dry-run query), `--force` (reinstall latest), `--tag <tag>` (pin to specific release), and `--json` (structured machine output).
  - Added `man/man1/gmb-update.1` roff manual page and shell completion generators for Bash, Zsh, Fish, and PowerShell.
- **Lip Gloss UI/UX for Incremental & Commit Analysis**:
  - Styled terminal cards in `internal/tui/views/analyze_view.go` for `gmb analyze` and git commit hook executions.
  - Branded mode banners with `⚡ INCREMENTAL` vs `🔄 FULL RESCAN` status pills.
  - Formatted graph metrics grid displaying total files, nodes, edges, virtual dependencies, and delta annotations ($+\Delta$).
  - Architecture intelligence badges: pattern confidence percentage pills (e.g. `● DDD Bounded Context [80%]`) and color-coded smell severity badges (`[LOW]`, `[MEDIUM]`, `[HIGH]`).
  - Reasoning highlights for commit changes and recorded developer memory events.
- **Enhanced Polyglot Test Suites & Fixtures**:
  - Expanded all 14 language samples under `testdata/languages/*/*` from minimal stubs to 80+ lines of real-world idioms (generics, async/await, traits, interfaces, and enums).
  - Added reusable test harness fixtures: `PolyglotProject`, `VisualizationStressProject`, and `LargeProject` (80-file scale).
  - Added comprehensive test suites: `tests/e2e/polyglot_e2e_test.go`, `tests/qa/extended_qa_test.go`, `tests/stages/comprehensive_ingestion_test.go`, `tests/nonfunctional/comprehensive_scale_test.go`, and `tests/edgecases/comprehensive_edgecases_test.go`.

### Fixed & Improved
- **Grounded AI Architect (`gmb why`) Reasoning Overhaul**:
  - Removed rigid refusal constraints from `internal/ai_engine/reasoning_prompts.go` that previously caused generic "I don't have evidence" responses.
  - Instructed AI Architect to deeply synthesize real AKG graph nodes, developer memory, commit records, and design patterns.
  - Enriched `internal/ai_engine/evidence_retriever.go` to pass docstrings, signatures, and package roles directly to the LLM.
  - Upgraded `cmd/why.go` to use tool-assisted `AskAgent` (`read_source`, `query_akg`, `query_architecture_memory`, `get_architecture_timeline`).
  - Added `ErrEmptySubgraph` validation prompting users to run `gmb analyze` on empty/un-analyzed repositories.
- **Mermaid & PlantUML Diagram Sanitization**:
  - Hardened class diagram member sanitizer against Python docstring triple-quotes (`'''`), dictionary unpacking (`{**}`), and raw braces.
  - Normalized class members to strict `Name : Type` syntax and filtered corrupted tokens.
  - Restored Mermaid primitive `DATABASE` type declaration rendering for database nodes in class diagrams.
  - Hardened PlantUML member and general diagram label sanitizers.
- **Interactive TUI Viewport Scrolling & Token Usage**:
  - Added mouse wheel, PageUp/PageDown, Home/End, and `k`/`j` scroll navigation to interactive chat and query viewports.
  - Added responsive word-wrapping aligned to terminal viewport width.
  - Implemented token usage estimation fallback for LLM providers that omit usage headers.
  - Cleaned up obsolete Docker/GHCR references across `.gitignore` and documentation.
- **Golden Test Fixture Parity**:
  - Gated golden file updates behind `GMB_REBASE_GOLDENS=1` in `golden_generator_test.go`, preventing regular `go test ./...` runs from silently mutating golden fixtures.

### CI/CD & Build Hardening
- Set `CGO_ENABLED: 1` globally across Ubuntu, macOS, and Windows runners for robust tree-sitter C bindings.
- Added Preflight Quality Gate job in `.github/workflows/release.yml` running `go vet`, determinism verification, golden parity, and sanitizer regression tests before triggering matrix builds.
- Multi-platform native release matrix with Clang macOS Intel cross-compilation.
- Added worktree isolation and idempotent diff reporting in `.github/workflows/akg-pr-comment.yml`.
- Supply chain security: Sigstore Cosign cryptographic signatures, Syft SPDX SBOMs, and SLSA build provenance attestations with non-blocking error handling.

---

## [v1.0.0] - 2026-08-24

### Added
- **Cross-Platform Zero-Dependency Binary Packaging & Distribution**:
  - Direct one-line web installers for UNIX (`curl | sh`) and Windows (`irm | iex`).
  - Native standalone binary archives for Linux (`amd64`, `arm64`), macOS (`amd64`, `arm64`), and Windows (`amd64`, `arm64`).
  - Full software supply chain security: Sigstore Cosign cryptographic signatures, Syft SPDX Software Bill of Materials (SBOM), and SLSA build provenance attestations.
  - Automated CI/CD release pipeline powered by GoReleaser and GitHub Actions matrix runners across Ubuntu, macOS, and Windows.
- **Charm-Based Terminal Design System & TUI**:
  - Unified OKLCH color palette with WCAG AA compliance across light and dark terminals.
  - Canonical keybindings standard (`q`, `esc`, `ctrl+c`, `↑↓`/`jk`, `g`/`G`, `ctrl+u`/`ctrl+d`, `pgup`/`pgdn`, `?`).
  - Toggleable interactive `?` help overlay component across all interactive programs.
  - Branded Huh form theme aligned with GlassMarble primary violet and cyan design tokens.
  - Pure-view architecture and responsive auto-resizing across terminal viewports.
  - Screen-reader accessible hotspot rankings and structured status cards.
  - Brand ASCII art Logo Banner on workspace initialization (`gmb init`).
- **Comprehensive CLI Polish & UX Overhaul**:
  - Command categorization into 6 structured groups (`analyze`, `inspect`, `govern`, `visualize`, `ai`, `utility`).
  - Complete `--json` machine-readable output parity across all 28 commands.
  - Standardized `--color (auto|always|never)` and `--dir` persistent flags.
  - Actionable error messages with contextual next-step hints (`— try 'gmb <cmd>'`).
  - Full shell autocompletions for Bash, Zsh, Fish, and PowerShell.
  - 32 offline roff UNIX manual pages in `man/man1/`.
- **Multi-Language Architecture Knowledge Graph Engine**:
  - Sub-second incremental parsing and Concrete Syntax Tree extraction across 17 programming languages.
  - Generic AST (GAST) normalization with I/O primitive classification (`DATABASE`, `NETWORK_IO`, `DISK_IO`).
  - Topology mapping, semantic interface linking, and MVCC atomic graph transactions in `.glassmarble/akg.json`.
- **31 Architecture Diagram Types**:
  - Full synthesis across UML, C4 model (Context, Container, Component, Code), and specialized architectural views.
  - Multi-target rendering to Mermaid.js (`mermaid`), PlantUML (`plantuml`), and Graphviz DOT (`dot`).
- **Grounded AI Architect Agent**:
  - Tool-calling agent with streaming tokens, tool pills, persistent session memory, and cost tracking.
  - Multi-provider support: Anthropic Claude, OpenAI, Ollama (local), Google Gemini, DeepSeek, OpenRouter, and custom endpoints.

---

## [v1.0.0-overhaul] - 2026-08-06

### Major Features & Performance Overhaul
- Full codebase analysis time reduced from 2m6.5s to < 20s.
- Graph transaction commit phase reduced from 19.9s to < 8s.
- AKG storage size reduced from 19.3MB to < 12MB.
- Parallel file ingestion worker pool capped at 8 threads to prevent scheduler thrash.
- Replaced double-write reified triples with W3C RDF-Star single-statement syntax.
- Consolidated CLI, TUI, and AI agent tools under unified diagram pipeline.
