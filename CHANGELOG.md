# Changelog & Overhaul Release Notes

All notable changes to the GlassMarble Architectural Knowledge Graph (AKG) and Visualization Engine are documented here.

---

## [v1.0.0-overhaul] - 2026-08-06

### Major Features & Performance Overhaul

- **Pipeline Performance Budget (§12.0):**
  - Full codebase analysis time reduced from **2m6.5s to < 20s**.
  - Graph transaction commit phase reduced from **19.9s to < 8s**.
  - AKG TTL storage size reduced from **19.3MB to < 12MB**.
  - Parallel file ingestion worker pool capped at 8 threads to prevent scheduler thrash.

- **AKG Schema v3 & RDF-Star Single Statement (§6.0 / §14.1):**
  - Replaced double-write reified triples with W3C RDF-Star single-statement syntax (`<< subject predicate object >> gm:lineNumber N`).
  - Automatic `akg_state.v2.ttl.bak` backup and one-time in-memory schema v3 migration on first run.
  - Stale ontology kinds consolidated (`TYPE_DECL` $\rightarrow$ `STRUCT`, `EXECUTABLE` $\rightarrow$ `FUNCTION`).

- **Unified Diagram Pipeline (`product.BuildDiagram`) (§11.0):**
  - Consolidated CLI (`gmb visualize`), TUI, and AI agent tools under `internal/product/pipeline.go`.
  - Guarantees 100% format and scope parity across CLI, TUI, and AI agent interactions.
  - Automatic injection of standardized header comments (`% <type> · <scope> · entry=<resolved> · nodes=N edges=M`).

- **31 Diagram Types Across 3 Formats (§8.0):**
  - Complete support for 31 diagram types across UML, C4, and Specialized families.
  - Formats: Mermaid (`mermaid`), PlantUML (`plantuml`), Graphviz DOT (`dot`).

- **Developer & Quality Tooling (§12.0 / §13.0):**
  - Added `gmb analyze --bench` budget gate command (exits non-zero on budget overrun).
  - Added `gmb stats --bench` telemetry and performance summary.
  - Added `gmb dev rebase-goldens` developer command for updating golden diagram test fixtures.
  - Added AKG determinism test suite ensuring byte-identical TTL serialization across runs.

- **Documentation (§14.3):**
  - Published master CLI documentation (`docs/cli.md`).
  - Published master diagram specification (`docs/diagrams.md`).
