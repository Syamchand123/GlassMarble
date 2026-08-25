# Changelog

All notable changes to the GlassMarble Architectural Intelligence Platform are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

---

## [Unreleased]

### Planned
- LSP (Language Server Protocol) server interface for VS Code and JetBrains IDE plugins.
- Real-time architectural drift linter for GitHub Pull Request status checks.

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
