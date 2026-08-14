# GlassMarble Diagram Master Specification (`docs/diagrams.md`)

GlassMarble supports **31 architectural diagram types** across **4 families**
(UML, C4, Specialized, Analysis) and **3 output formats** (`mermaid`,
`plantuml`, `dot`). The authoritative catalog lives in
`cmd/visualize.go` (`all31DiagramCatalog`) and is printed by
`gmb visualize list`.

---

## Diagram Families & Types

### 1. UML Family (14 Types)

| Type | Description | Entry |
|---|---|---|
| `class` | Class hierarchy, fields, interfaces, and typed relationships | optional |
| `object` | Runtime instance state and object links | required |
| `component` | Module and package architectural components | optional |
| `deployment` | Cloud resources, databases, queues, and deployment targets | optional |
| `package` | Namespace/package containment and dependencies | optional |
| `composite` | Internal structure of a component | required |
| `profile` | Stereotype and constraint extensions | optional |
| `usecase` | Endpoints and boundary user/system interactions | optional |
| `activity` | Function execution flow and control structures | required |
| `state` | State transitions and state machine branches | required |
| `sequence` | Call-stack execution trace starting from an entrypoint | **required** |
| `communication` | Collaboration and message links | required |
| `interaction` | High-level interaction overview fragments | required |
| `timing` | Time-constrained state changes | required |

### 2. C4 Model Family (7 Types)

| Type | Description | Entry |
|---|---|---|
| `c4context` | System context and external actors/systems | optional |
| `c4container` | Microservices, web apps, databases, and message queues | optional |
| `c4component` | Internal components within a container/module | optional |
| `c4code` | Code-level class and method structures within a component | optional |
| `c4landscape` | Multi-system landscape overview | optional |
| `c4dynamic` | Dynamic interaction flows | required |
| `c4deployment` | Infrastructure deployment environment | optional |

### 3. Specialized Family (4 Types)

| Type | Description | Entry |
|---|---|---|
| `er` | Entity-relationship database schemas and key links | optional |
| `dataflow` | Data movement pipelines and memory flows | optional |
| `mindmap` | Hierarchical concept structure | optional |
| `flowchart` | General-purpose process flow | required |

### 4. Analysis Family (6 Types)

| Type | Description | Entry |
|---|---|---|
| `dependency` | Module and package dependency graph | optional |
| `hotspot` | High-coupling and complexity heatmap | optional |
| `callgraph` | Method and function invocation call graph | optional |
| `layered` | Architectural tier separation | optional |
| `impact` | Blast radius of a code change | changed-files |
| `infrastructure` | External systems, databases, and messaging | optional |

**Entry requirement notes:** the catalog marks the entry requirement per
type. Hard-enforced in code: `sequence` **refuses to render without
`--entry`**. `impact` drives its projection from `--changed-files` (or the
git working-tree diff when empty).

**Render approximations** (documented in the renderers):
- `timing` renders as a Mermaid `timeline` diagram (Mermaid has no native
  UML timing diagram).
- `c4code` renders as a class diagram; `c4dynamic` as a `sequenceDiagram`.
- PlantUML falls back to a generic rectangle/arrow stencil for
  usecase/activity/state/sequence/ER/flow/mindmap/timing; C4 types get real
  C4 stencils.

---

## Output Formats

1. **Mermaid (`mermaid`)** — default; native markdown rendering for GitHub,
   IDEs, and web viewers.
2. **PlantUML (`plantuml`)** — rich UML rendering for enterprise tooling.
3. **Graphviz DOT (`dot`)** — standard graph visualization language.

All 31 types render in all 3 formats (`gmb visualize <type> --format <f>`).

---

## Scope Levels (`--scope`)

- **`global`** (default): the entire workspace graph.
- **`folder:<path>`**: scopes to a directory subtree; external references
  render as boundary ports (`[+N external]`). `--relative` renders paths
  relative to the folder root.
- **`file:<path>`**: scopes to a single source file.

---

## Command Flags (`gmb visualize <type>`)

| Flag | Default | Description |
|---|---|---|
| `--format` | `mermaid` | `mermaid`, `plantuml`, or `dot` |
| `--scope` | `global` | `global`, `folder:<path>`, `file:<path>` |
| `--entry` | `""` | Entry point symbol ID (mandatory for `sequence`) |
| `--depth` | `7` | Max search depth for reachability path walk |
| `--unused` | `false` | Include unreferenced dead components |
| `--max-nodes` | `0` | Node limit before truncation with `[+N external]` ports (`0` = unlimited) |
| `--link-level` | `architecture` | Graph linkage detail: `architecture`, `standard`, `full` |
| `--changed-files` | `""` | Comma-separated list of changed files (impact analysis) |
| `--relative` | `false` | Paths relative to folder root under folder scope |
| `--summary` | `false` | Print graph statistics before the diagram |
| `--pagerank` | `false` | Enable PageRank computation |
| `--community` | `false` | Enable community detection |
| `--scc` | `false` | Enable strongly connected components analysis |
| `--save` | `""` | Save markup to `.glassmarble/marbles/<name>.md` |
| `--output` | `""` | Write raw markup to a file instead of stdout |
| `--render` | `""` | Render to `.svg`/`.png` via Kroki, falling back to mermaid-cli (`mmdc`); markup saved as `<target>.txt` if both fail |
| `--dir` | `.` | Directory containing `.glassmarble/` |

Subcommands: `gmb visualize list` prints the full 31-type catalog;
`gmb visualize check <type>` validates a type name against the live graph.

---

## Projection & Streaming Rules

1. **Determinism:** diagram nodes and relationships are emitted in sorted
   order to ensure byte-equal output across runs.
2. **Streaming:** renderers stream output using `strings.Builder` without
   intermediate string copies.
3. **Max Nodes:** `--max-nodes` truncation is enforced at the projection
   layer before rendering starts.
4. **Entry resolution:** unknown or missing entry points fail with exit
   code 2 (entry-point error) rather than rendering an empty diagram.