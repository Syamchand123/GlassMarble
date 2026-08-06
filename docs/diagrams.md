# GlassMarble Diagram Master Specification (`docs/diagrams.md`)

GlassMarble supports **31 architectural diagram types** across **3 diagram families** and **3 output formats** (`mermaid`, `plantuml`, `dot`).

---

## Diagram Families & Types

### 1. UML Family (10 Types)
- **`class`**: Class hierarchy, field membership, interfaces, and typed relationships.
- **`sequence`**: Call-stack execution trace starting from an entrypoint.
- **`component`**: Module and package architectural components.
- **`object`**: Runtime instance state and object links.
- **`package`**: Namespace/package containment and dependencies.
- **`activity`**: Function execution flow and control structures.
- **`state`**: State transitions and state machine branches.
- **`usecase`**: Endpoints and boundary user/system interactions.
- **`deployment`**: Cloud resources, databases, queues, and deployment targets.
- **`er`**: Entity-Relationship database schemas and primary key links.

### 2. C4 Model Family (4 Types)
- **`c4-context`**: System context and external actors/systems.
- **`c4-container`**: Microservices, web apps, databases, and message queues.
- **`c4-component`**: Internal components within a container/module.
- **`c4-code`**: Code-level class and method structures within a component.

### 3. Specialized Family (17 Types)
- **`architecture`**: High-level structural layer overview.
- **`callgraph`**: Method and function invocation call graph.
- **`cfg`**: Control Flow Graph (branches, conditionals, loops).
- **`dfg`**: Data Flow Graph (variable assignments and taint flow).
- **`concurrency`**: Goroutine/thread spawns, channels, and async tasks.
- **`dataflow`**: Data movement pipelines and memory flows.
- **`dependency`**: Module and package dependency graph.
- **`filedeps`**: File-to-file import and usage dependencies.
- **`boundary`**: External API endpoints, webhooks, and boundary ports.
- **`security`**: Taint sources, propagation paths, and security sinks.
- **`memory`**: Heap allocations, escapes, and pointer aliases.
- **`eventsourcing`**: Event publishers, event handlers, and message buses.
- **`rpc`**: Remote procedure calls, gRPC, and cloud API links.
- **`constraints`**: Branch conditions and guard assertions.
- **`di`**: Dependency injection providers, bindings, and injections.
- **`type`**: Type aliases and generic type instantiations.

---

## Output Formats

1. **Mermaid (`mermaid` / `.mmd`)**: Native markdown rendering for GitHub, IDEs, and web viewers.
2. **PlantUML (`plantuml` / `.puml`)**: Rich UML rendering for enterprise documentation and tooling.
3. **Graphviz DOT (`dot` / `.dot`)**: Standard graph visualization language for automated layout.

---

## Scope Levels

- **`global`**: Scopes the entire workspace graph.
- **`package`**: Scopes to a specific package/directory namespace.
- **`file`**: Scopes to a single source file, rendering external references as boundary ports (`[+N external]`).

---

## Projection & Streaming Rules

1. **Determinism:** All diagram nodes and relationships are sorted lexicographically before rendering to ensure byte-equal output across runs.
2. **Streaming:** Renderers stream output using `strings.Builder` without intermediate string copies.
3. **Max Nodes:** `--max-nodes` truncation is enforced at the projection layer before rendering starts.
