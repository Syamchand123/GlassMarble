# GlassMarble Neo4j Export & Import Recipes

GlassMarble exports its Architecture Knowledge Graph (AKG) as a deterministic
Cypher script for graph database analysis, visualization in Neo4j Bloom, and
enterprise queries. The GraphJSON store (`gmb export --format graphjson`) is
the lossless interchange format; the Neo4j export is a faithful projection of
the same graph.

---

## 1. Exporting from GlassMarble

Run `gmb export` with `--format neo4j`:

```bash
gmb export --format neo4j --output dump.cypher
```

The format defaults to `graphjson`, so a `.cypher` filename alone does not
switch formats — pass `-f neo4j` explicitly:

```bash
gmb export -f neo4j -o architecture.cypher
```

The generated script is deterministic (nodes and edges are sorted before
emission) and contains:

- a header comment with the commit hash, graph version, and schema version,
- one `CREATE (:<Label> {...})` statement per node — labels are
  `GMNode:<Kind>` (e.g. `:GMNode:CLASS`, `:GMNode:FUNCTION`,
  `:GMNode:MODULE`), with `id`, `name`, `kind`, `primitive`, `file_path`,
  `line_start`, `line_end`, and any `prop_*` node properties,
- one `MATCH (s {id: ...}), (t {id: ...}) CREATE (s)-[:REL]->(t)` statement
  per edge — relationship types are the edge type constants
  (e.g. `:CALLS`, `:DEPENDS_ON`, `:IMPLEMENTS`, `:EXTENDS`, `:COMPOSES`,
  `:REFERENCES`, `:CFG_FLOW`, `:DATA_FLOW`), with `line_number`,
  `confidence`, `is_cycle`, and any `prop_*` edge properties.

The header comment also records the commit and schema version:

```cypher
// GlassMarble Architecture Knowledge Graph - Cypher Export
// Commit: f438841a7ac27c9c910881070f65fd9fd2c90a72, Version: 5, Schema: 3
```

---

## 2. Importing into Neo4j

### Option A: Using `cypher-shell` (CLI)

```bash
cypher-shell -u neo4j -p password -f dump.cypher
```

### Option B: Using Neo4j Desktop / Browser

1. Open Neo4j Desktop or Neo4j Browser.
2. Open `dump.cypher` in a text editor.
3. Copy and execute the Cypher statements in Neo4j Browser.

Because edge statements match nodes by `id`, the whole script must execute
against the same database session.

---

## 3. Sample Neo4j Cypher Queries

### Find All Microservice Boundary Dependencies
```cypher
MATCH (a:GMNode)-[r:CALLS|NETWORK_RPC_CALL]->(b:GMNode)
WHERE a.file_path <> b.file_path
RETURN a.name, type(r), b.name
```

### Find Cyclic Dependencies
```cypher
MATCH path = (a:GMNode)-[:DEPENDS_ON|CALLS*..5]->(a)
RETURN path
```

### Find High-Centrality Hotspots
```cypher
MATCH (n:GMNode)<-[r:CALLS]-()
RETURN n.name, n.kind, count(r) AS in_degree
ORDER BY in_degree DESC
LIMIT 10
```