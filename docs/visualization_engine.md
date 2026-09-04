# GlassMarble Visualization Engine & Web UI

> Generate 31+ living architectural diagrams or explore your Architecture Knowledge Graph (AKG) through an interactive, offline-first Web UI.

GlassMarble includes a dual-mode visualization engine:
1. **CLI Diagram Generator (`gmb visualize`)**: Compiles virtual subgraphs into 31+ living diagram notations across **Mermaid**, **PlantUML**, and **DOT (Graphviz)**.
2. **Interactive Visualizer Server (`gmb ui`)**: A local dev-tool web application providing a high-performance, GPU-accelerated canvas for exploring nodes, dependencies, architectural smells, blast radius, and historical evolution.

---

## 1. CLI Diagram Generator (`gmb visualize`)

### Quick Syntax
```bash
# Generate and print a diagram
gmb visualize class
gmb visualize c4container

# Save to .glassmarble/marbles/<name>.md
gmb visualize c4container --save c4_container

# Scope to a folder or file
gmb visualize component --scope folder:internal/akg
gmb visualize callgraph --entry "cmd/root.go::Execute"
```

### Supported Formats & Notations (31 Types)
* **14 UML Diagrams**: `class`, `component`, `sequence`, `package`, `deployment`, `state`, `activity`, `usecase`, `object`, `timing`, `composite`, `interaction`, `profile`, `communication`.
* **7 C4 Model Diagrams**: `c4context`, `c4container`, `c4component`, `c4code`, `c4dynamic`, `c4deployment`, `c4landscape`.
* **4 Specialized Diagrams**: `er`, `flowchart`, `mindmap`, `dataflow`.
* **6 Analytical Diagrams**: `callgraph`, `dependency`, `layered`, `microservice`, `impact`, `layer-invariants`.

### C4 Layouter Engine Hardening
* **Relatable Target Redirection:** In Mermaid C4, relationships (`Rel`) cannot target boundary groups (`System_Boundary`, `Enterprise_Boundary`). GlassMarble's layouter recursively binds folded boundary subtrees to their enclosing `Container` or `System` elements, eliminating layouter crashes (`Cannot read properties of undefined (reading 'x')`).
* **Technology Compaction:** Multi-primitive labels are automatically compacted (e.g. `"ALLOCATION, CACHE +2"`) to avoid overflowing Mermaid's fixed-width element containers.

---

## 2. Interactive Web Visualizer (`gmb ui`)

Start the visualizer server in your repository:
```bash
gmb ui
```
* **Custom port and host:** `gmb ui --port 8080 --host 127.0.0.1`
* **Headless / daemon mode:** `gmb ui --no-open --json` (emits a single startup receipt containing URL, port, and process ID).

---

## 3. Web UI Architecture & Highlights

### 100% Offline & Resilient
* **No External CDNs:** The visualizer eliminates all external network calls and `@import` font dependencies. It uses a high-legibility system font stack (`ui-monospace`, `-apple-system`, `Segoe UI`, `Roboto`), ensuring full functionality in air-gapped or offline development environments.
* **Automatic Cache-Busting:** `index.html` is served with `Cache-Control: no-cache` and injects a per-process `__ASSET_V__` query token on asset tags. Embedded scripts and styles receive long-lived caches (`max-age=86400`) but bust immediately whenever the binary or server restarts.

### 5 Dedicated Workspaces
1. **Architecture Graph:** Cytoscape.js interactive canvas with 3 granularity levels (`components`, `packages`, `symbols`).
2. **Intelligence & Smells:** Pattern detection confidence cards, top PageRank hotspots, and structural smell tables.
3. **Memory & Evolution:** Chronological timeline events, commit diff annotations, and recorded developer rationales.
4. **Marbles Diagrams:** Live-rendered SVG viewer for all repository diagrams with syntax-highlighted source drawer.
5. **Repository Metrics:** Instability ($I$), afferent/efferent coupling ($C_a / C_e$), dead-code counts, and layer compliance.

---

## 4. Canvas Engine & Performance Safeguards

The Architecture Graph is optimized to handle large codebases (15,000+ nodes, 35,000+ edges) smoothly:

### Multi-Algorithm Layouts
Switch between 5 layout algorithms on the fly:
* **Force-Directed (CoSE):** Physics simulation balancing repulsion and edge springs.
* **Layered DAG (Breadthfirst):** Top-down architectural hierarchy.
* **Concentric:** Concentric rings ordered by afferent coupling and in-degree.
* **Circular:** Cyclic dependency inspection.
* **Grid:** Spatial distribution for disconnected subgraphs.

### Scale-Adaptive Fallback
* Force-directed layout complexity is $O(N^2)$. On graphs with **>700 nodes** (`COSE_NODE_LIMIT`), the UI automatically falls back to the fast concentric layout with a notification toast, preventing browser thread lockup.
* **Compound Parent Stripping:** On large fallback layouts, compound package bounding boxes are automatically stripped from the render tree so they do not stretch across the canvas into a solid colored wash.

### Viewport Frame-Rate Optimization
* **Texture Caching:** `textureOnViewport: true` renders a cached texture during rapid panning and zooming.
* **Edge Culling during Manipulation:** `hideEdgesOnViewport: true` skips redrawing 14,000+ edges on every animation frame during user pan/zoom.
* **Zoom-Dependent Labels:** Labels use `min-zoomed-font-size: 7`, materializing as the user zooms in rather than occluding the canvas at full extent.

### Atmospheric Depth System
* **Pseudo-3D Depth Cues:** Elements float at visual depths based on connectivity:
  * Leaf nodes fade softly toward the background and display smaller labels.
  * Architectural hubs stay near-opaque with larger monospace labels and an underlay accent halo (`depth > 0.72`).
  * Purely data-driven via Cytoscape stylesheet attributes (`vis`, `fsize`, `eop`) — **zero per-frame CPU cost**.
* **Parallax Grid:** The dotted canvas background grid pans at 0.22× velocity and scales with zoom, providing physical grounding.

---

## 5. Architectural Overlays & Analysis Tools

* **Cycles Overlay:** Runs Tarjan's Strongly Connected Components algorithm via `/api/algorithms/cycles`, highlighting circular dependencies in crimson.
* **Cut Vertices Overlay:** Identifies articulation points via `/api/algorithms/cutvertices` that act as structural single points of failure.
* **PageRank Centrality:** Dynamically rescales node radii proportionally to PageRank eigenvector scores.
* **Architectural Smells Radar:** Highlights all nodes referenced in active architectural debt findings.
* **Shortest Path Tracing:** Enter source and target symbol names to highlight the exact call path traversing the system.
* **Refactoring Blast Radius Simulator:** Select any symbol and click "Analyze Impact" to simulate cascading downstream breakage and list affected test suites with copyable `go test` commands.

---

## 6. High-Resolution Safe PNG Export

Clicking the camera icon exports the current canvas view:
* **Canvas Dimension Safeguards:** Browsers silently fail (producing 0-byte images) if canvas dimensions exceed ~16,384px or ~268M pixels. GlassMarble dynamically calculates the optimal scale factor bounded by `MAX_DIM = 6000` and `MAX_AREA = 12,000,000` pixels:
  $$\text{scale} = \min\left(2,\; \frac{6000}{w},\; \frac{6000}{h},\; \sqrt{\frac{12000000}{w \times h}}\right)$$
* **Blob Pipeline:** Emits a native binary `Blob` object URL rather than multi-hundred-megabyte base64 strings, validates non-empty payloads, and cleanly revokes object URLs after download.
