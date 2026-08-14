# Relationship Types (Edge Taxonomy v2)

> Source of truth: `master_overhaul_plan.md` §4.2 (edge taxonomy v2), enforced by
> `internal/akg/ontology_test.go` conformance tests and declared in
> `internal/akg/ontology.ttl`.

Every `RelationshipType` constant in
`internal/code_analysis_engine/link/type.go` belongs to exactly one of four
families with a **single producer policy**: one (or one class of) link
linker passes emits the family, and the family maps to one `gm:view` tag
(`structural` | `dynamic` | `security`). The serializer emits that view tag as
a `gm:view` RDF-star attribute on every triple (K-01).

## Families

| Family | Constants | Producer (link pass) | View |
|---|---|---|---|
| STRUCTURAL | `EdgeContains, EdgeBelongsTo, EdgeDependsOn, EdgeImplements, EdgeExtends, EdgeMixes, EdgeComposes, EdgeHasField, EdgeHasParam, EdgeReturns, EdgeHasReceiver` | builder / type_linker / member_linker | structural |
| BEHAVIORAL | `EdgeCalls, EdgeContextCall, EdgeSpawnsConcurrent, EdgeDefers, EdgeCatches, EdgeThrows, EdgeReferences, EdgeInstantiates, EdgeDispatchesEvent, EdgePublishes, EdgeSubscribes, EdgeSendsTo, EdgeReceivesFrom, EdgeQueriesDB, EdgeCallsCloudAPI, EdgeExposesEndpoint, EdgeFFICall, EdgeInjects, EdgeConsumesResource, EdgeMutatesGlobal` | call / concurrency / event / rpc / ffi / di / security linkers | structural |
| DYNAMIC | `EdgeControlFlow, EdgeConditionalBranch, EdgeLoopBranch, EdgeSwitchBranch, EdgeConstraint, EdgeDataFlow, EdgePointsTo, EdgeHeapAlias, EdgeAliases, EdgeAliasesType, EdgeCyclic, EdgeVulnerable, EdgeEscapesToHeap` | cfg / dfg / alias / memory / constraint linkers | dynamic |
| SECURITY | `EdgeSecuritySink` | security_linker | security |

## Shared edges

Two edges participate in a second family without leaving their primary one:

- `EdgeVulnerable` — DYNAMIC (primary) **and** SECURITY (taint flow into sinks).
- `EdgeQueriesDB` — BEHAVIORAL (primary) **and** SECURITY (when the query is a sink).

The serializer emits a single `gm:view` attribute per triple (K-01), so these
keep their primary family view in the TTL; security filtering is applied at
extraction time (`ViewSecurity` in extraction configs).

## Non-table edge

- `EdgeNetworkCall` (`NETWORK_RPC_CALL`) — BEHAVIORAL family member produced by
  rpc_linker that the §4.2 table does not enumerate; kept because rpc_linker
  still emits it and Phase 0 must not change behavior.

## View tags

`internal/visualization_engine/types` defines the vocabulary:

| Tag | Value | Covers |
|---|---|---|
| `ViewStructural` | `structural` | type/member ownership, runtime behavior, dependencies |
| `ViewDynamic` | `dynamic` | intra-function control and data movement |
| `ViewSecurity` | `security` | taint propagation and sinks |

`AllViews` lists every declared tag; `ExtractionConfig.Views` selects which
views a diagram reads (defaults to `AllViews` when empty). `link.ViewOfEdgeType`
maps a constant to its view and is checked by `TestOntologyDeclaresEdgeViews`.
