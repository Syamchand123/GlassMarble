# Changelog

All notable changes to the GlassMarble Architectural Intelligence Platform are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

---

## [Unreleased] — v1.1.0

### Fixed — correctness & data integrity
- **MVCC snapshot corruption (`internal/akg`)**: `rotateLeft`/`rotateRight` mutated the nodes they were given. Because `Set`/`Delete` copy only the path to the touched key and alias every sibling subtree, a rebalance rewrote nodes still owned by earlier snapshots. Reproduced deterministically: a snapshot of `{a,b,c,d,e}` became `[a b b c d e]` — a duplicated key and a broken traversal — after a `Delete` on a *derived* map. Every incremental commit calls `Delete` on six indexes against a shadow that shares structure with the live graph, so any delta commit could corrupt the snapshot being served to readers. Both rotations are now pure; covered by new isolation, double-rotation and 200-key stress tests.
- **Silent data loss on linker panic (`internal/code_analysis_engine/link`)**: a panicking pass was recovered, logged, and ignored, after which analysis committed a graph missing that pass's output. Since the commit path sweeps every node of each modified file before grafting, the swept nodes and edges were permanently deleted behind one log line. Panics in passes and merge goroutines now abort the run with an error, leaving the stored graph untouched.
- **Untracked files invisible to incremental analysis (`ingest/git.go`)**: `git diff --name-status HEAD` never lists untracked files, so a newly created source file never entered the graph until some later full rescan. The working-tree delta now unions in `git ls-files --others --exclude-standard`, deduplicated and still respecting `.gitignore`. The root-commit probe also ran without a working directory, resolving against the process CWD instead of the analyzed repository.
- **Diagram canvas not updating (`gmb ui` → Diagrams)**: selecting a second diagram changed the title but left the previous SVG on screen. The placeholder element lived inside the container being overwritten, so the first render destroyed it and every later selection threw before fetching. Now renders into a dedicated canvas with a render token guarding out-of-order completions.
- **Untypeable characters in `gmb ai chat`**: vi-style viewport bindings matched before the composer saw the key, so `b`, `f`, `g`, `G` and space were swallowed — typing "before" produced "eore". Letter aliases now apply only when the composer is blurred; `esc` also quits.

### Changed — performance
- **Graph commit cost roughly halved.** Persistence was 69% of analysis wall-clock. The post-write verification re-read the state file, unmarshaled it into a second document, re-serialized the graph into a third, and byte-compared — paying for the write twice to prove determinism, which CI already gates. Verification now happens *while* writing (zero-dangling checked against the document being emitted) plus a streaming SHA-256 read-back, which additionally detects corruption between serialization and rename. Measured on an identical synthetic graph: 5k nodes 151ms → 88ms, 20k nodes 637ms → 338ms, with ~2.2× less memory and ~3× fewer allocations. End-to-end on this repository at ~15.3k nodes: commit 12,483ms → 9,845ms (−21%), total analysis −27%.
- `CowMap.Len` was an O(N) tree walk called casually on hot paths; the count is now maintained incrementally and read atomically.
- The state encoder writes through a 1 MiB buffer instead of issuing many small syscalls, and the storage directory is fsynced after the atomic rename (a rename is not durable until its directory is synced).
- Graph viewport rendering skips edges and uses a cached texture during pan/zoom, so interacting with a 5k-node / 14k-edge view no longer redraws everything each frame.

### Added
- `akg.json.sha256` sidecar so corruption at rest is detectable without re-deriving the graph.
- CI race-detector gate over `internal/akg` and `internal/code_analysis_engine` — the MVCC substrate is lock-free on the read path, so defects there are silent corruption rather than crashes, and the existing `*_NoRace` tests proved nothing without `-race`.
- Benchmarks comparing the previous and current commit paths on the same graph.

### Changed — CLI/TUI
- `--quiet` was declared and documented but read by nothing; it now suppresses non-error output.
- `--color=always` only unset `NO_COLOR`, which does not survive a non-TTY stdout; it now also sets `CLICOLOR_FORCE`, so piping to a pager keeps color.
- Non-interactive `analyze` printed one line at the start and one at the end, leaving CI runs looking hung. Phase-boundary callbacks that were previously discarded outside the TUI now render as plain log lines on stderr, keeping stdout clean for `--json`.

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
