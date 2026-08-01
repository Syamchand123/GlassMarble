# GlassMarble Core Engines — Audit Scoreboard

Status of the AUDIT workstreams against the core engines (AKG persistence,
canonical parser, visualization engine, memory envelope). All phases below are
**DONE** and verified by tests and live runs.

## Issue 2 — Parser / Ontology Foundation (DONE)

| Phase | Item | Status | Evidence |
| --- | --- | --- | --- |
| 2A-4 | Legacy tombstone triple `<uri> gm:status "DELETED" .` treated as deletion, not edge | DONE | `stage1` `parseBaseEdge`, parser tests |
| 2A-5 | Shared kind-vocabulary contract (kinds ⇄ ontology classes 1:1) | DONE | `akg.turtle_serializer.mapKindToClass/mapClassToKind` + `ontology_test.go` |
| 2B-9 | Scoping never mutates a shared/cached graph (private clone) | DONE | `visualizer.ProjectDiagramFromGraph`, mutation test |

## Issue 3 — Persistence Integrity (DONE)

| Phase | Item | Status | Evidence |
| --- | --- | --- | --- |
| 3A-2 | Ontology declares every `gm:` term; conformance scan literal-aware | DONE | `gm:is_entrypoint` + `gm:has_async_side_effects` declared in `ontology.ttl`; doctor: `Unknown gm: terms: 0` |
| 3A-3 | Newer schema versions rejected loudly on load | DONE | `reconstructFromTTLFile` + `ErrSchemaVersion` test |
| 3B-6 | Tombstones remove node AND incident edges on restore (no resurrection) | DONE | `reconstructFromTTLFile` deleted-set scan, tests |
| 3B-7 | WAL replay bound persisted as `gm:version` (delta appends keep it) | DONE | `writeTTLMetadata`/`scanTTLMetadata`, serializer tests |
| 3B-8 | Entrypoints / folder zones / code / commit hash restored | DONE | `reconstructFromTTLFile` |
| 3C-12 | Macro rules derived on restore, never persisted | DONE | serializer skips `macro_rules` |
| 3D-13 | Every non-serialized index rebuilt on restore (Kind/Hash/File/Line) | DONE | `reconstructFromTTLFile` |

## Issue 4 — Memory Envelope & Lazy Reads (DONE)

| Phase | Item | Status | Evidence |
| --- | --- | --- | --- |
| 4A-1 | Single canonical parser; viz consumes in-memory AKG via from-graph entry points | DONE | `stage1.ParseTTLFileToNative` is the one parser; `akg.ToNativeGraph` + `ProjectDiagramFromGraph`/`ComputeGraphSummaryFromGraph` (parity tests); AI bridge renders from snapshot |
| 4A-2 | Lazy Query-based reads: status / inspect / visualize | DONE | `stage1.StreamTTLNodes`/`ParseTTLNodeByID`/`ParseTTLFileToNativeScoped`/`StreamTTLBlocks`; `akg.QueryNode`/`StreamNodes`/`StreamGraphStats`/`WALFreshness`; lazy `cmd/status.go` + `cmd/inspect.go`; `parseGraph` scoped branch |
| 4A-3 | SubgraphCache byte-bounded | DONE | 64 MiB LRU budget, `estimatedBytes`, scope in cache key, `TestSubgraphCacheByteBudget` |
| 4A-4 | `--max-ttl-mb` guard on load and commit | DONE | `MaxTTLBytes` + `enforceTTLBudget`; flag in `cmd/root.go`; oversized commit/load tests |
| 4B-5 | No `ReadAllEntries` of all WAL segments on open | DONE | `WriteAheadLog.ForEachEntry`; `Recover()` streams single pass, bounded by in-flight tx |
| 4B-7 | Macro cache capped | DONE | `maxMacroCacheEntries = 10000` + `capMacroCache` now covered by `TestMacroCacheCap` |
| 4B-8 | `.glassmarble` housekeeping (prune marbles/ + sessions/, WAL truncate) | DONE | `gmb housekeeping [--prune --older-than N]`; `TestHousekeepingPrune` |
| 4C-12 | Memory envelope documented | DONE | `docs/architecture.md` §10.7; `--max-ttl-mb` help text |

## Issue 5 — Health & Regression Guards (DONE)

| Phase | Item | Status | Evidence |
| --- | --- | --- | --- |
| 5A-1 | Post-write verification (node/edge parity, zero-dangling guard) | DONE | `verifyFile` in transaction manager |
| 5B-4 | `gmb doctor` health dashboard | DONE | doctor report: parse-back, dangling, duplicates, conformance, WAL, freshness |
| 5B-5 | `gmb status` cumulative node/edge totals | DONE | sums real outbound/inbound edges (3813/3813 live) via lazy `StreamGraphStats` |
| 5C-8 | Bloat-regression guard (real pipeline under a node budget) | DONE | `cmd/bloat_guard_test.go`: full stage1→4 run, node ≤ 3500, deduped edges ≤ 9000 |

## Live verification (this repo, 2026-08-01)

```
gmb doctor:    Parse-back: ok (2308 nodes, 3813 edges), Dangling: 0, Unknown gm: terms: 0
gmb status:    Nodes 2308, Edges 3813/3813, Files 201, Virtual 527 (22.8%), Storage TTL 3.5MB | WAL 0B, verified
gmb inspect:   --list / --search / <node-id> / --file+--line all streaming (lazy, no graph restore)
gmb visualize: --scope file:internal/akg/transaction_manager.go parses only that file's triples
gmb housekeeping: state 3.5MB 1 file(s), wal 0B
```

## Known deliberate tradeoffs

* `gmb status` (lazy) omits the macro-rule count (derived data, restore-only)
  and the persisted verification stamp (a zero-dangling TTL is reported as
  verified — the same test the restore path applies).
* The bloat guard budgets the *deduplicated* edge count (the persistence
  layer collapses parallel edges to one canonical triple — raw linker output
  for this repo is 12,091 edges, deduped 5,479).
