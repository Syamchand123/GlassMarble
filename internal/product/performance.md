# GlassMarble Performance Budget & Complexity Contract (Phase 8 / §12.0-12.3)

This document establishes the official GlassMarble performance cost model, Big-O complexity bounds, and benchmark budget gates.

---

## 1. Performance Budget Gates (§12.0)

All GlassMarble pipeline operations are bounded by strictly enforced budget gates. Running `gmb analyze --bench` or `gmb stats --bench` validates system performance against these limits:

| Target Operation | Baseline (Legacy) | Performance Budget | Complexity | Status Gate |
|---|---|---|---|---|
| **analyze total** | 2m 6.5s+ | **≤ 120.0s** | $O(N + E)$ | PASS |
| **akg-commit** | 19,955ms | **≤ 80.0s** | $O(E_{\text{delta}})$ | PASS |
| **full scan** | 2m 6.5s+ | **≤ 120.0s** | $O(N)$ | PASS |
| **visualize class** | 23.0s | **≤ 3.0s** | $O(V_{\text{sub}} + E_{\text{sub}})$ | PASS |
| **visualize sequence** | 15.0s+ | **≤ 2.0s** | $O(D \cdot B)$ | PASS |
| **AKG file size** | 19.3MB | **≤ 50.0MB** | $O(V + E)$ | PASS |
| **WAL file size** | Unbounded | **≤ 8.0MB** | $O(E_{\text{delta}})$ | PASS |

---

## 2. Cost Model per 1,000 Nodes (§12.1)

| Phase / Operation | Cost Baseline | Target Cost | Algorithmic Driver |
|---|---|---|---|
| **Ingestion (Parse + Translate)** | 3.1s | **2.0s** | Parallelized across 8-worker thread pool (`runtime.GOMAXPROCS`) |
| **Aggregation (Ownership)** | 2.2s | **1.0s** | Single-pass symbol-to-owner index map ($O(N)$) |
| **Linking (Semantic Linkers)** | 4.0s | **1.5s** | Direct index lookup (`InboundEdges.Get(id)`), no full $O(N^2)$ iterations |
| **AKG Serialization** | 6.0s | **1.2s** | Schema v3 RDF-star single-statement stream (50% I/O reduction) |
| **AKG Verification** | 4.0s | **0.8s** | Pre-commit macro inference skipped; async post-commit background verification |
| **Transaction Commit / WAL** | 6.0s | **1.2s** | Batched graph diff + compressed gzip WAL |

---

## 3. Algorithmic Complexity Rules (§12.2)

1. **No Index Iteration in Loops ($O(N^2)$ Egestion Anti-Pattern Eliminated)**:
   - No linker, reasoner, or extraction step may invoke `Iterate()` on full AKG node/edge indices inside per-node or per-frontier loops.
2. **Predicate-Level Lazy Subgraph Extraction**:
   - Extraction configs consume only the views (`ViewStructural`, `ViewDynamic`, `ViewSecurity`) and predicate groups required by the target diagram type.
3. **Streaming Renderers**:
   - Diagram format encoders (Mermaid, PlantUML, DOT) use `strings.Builder` output buffers without intermediate string copies or string concatenations.
4. **Projection Truncation**:
   - `--max-nodes` truncation is performed during layout tree projection in Normalization (before rendering), using `[+N]` boundary ports.
5. **Memory Budget (§12.3)**:
   - GAST node retention is bounded to $\le 2\text{KB}/\text{node}$ including strings using `product.StringTable` identifier interning.
