# GlassMarble Neo4j Export & Import Recipes

GlassMarble supports 1:1 lossless export of its Architecture Knowledge Graph (AKG) to Neo4j Cypher scripts for graph database analysis, visualization in Neo4j Bloom, and enterprise queries.

---

## 1. Exporting from GlassMarble

Run `gmb export` with `--format neo4j`:

```bash
gmb export --format neo4j --output dump.cypher
```

Or simply specify a `.cypher` output filename:

```bash
gmb export -o architecture.cypher
```

This generates a deterministic Cypher script containing node `CREATE` statements (labeled by node kind, e.g. `:GMNode:STRUCT`, `:GMNode:FUNCTION`) and edge `MATCH ... CREATE` relationships (e.g. `:CALLS`, `:DEPENDS_ON`, `:IMPLEMENTS`).

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
