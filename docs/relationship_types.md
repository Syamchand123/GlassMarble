# Relationship Types (Edge Taxonomy v2)

> Source of truth: the `RelationshipType` constants in
> `internal/code_analysis_engine/link/type.go` (enum + family comments), the
> view tags in `internal/visualization_engine/types/types.go`, and the
> predicate-group membership in
> `internal/visualization_engine/extract/extractor.go`.

Every `RelationshipType` constant belongs to exactly one of four families
with a **single producer policy**: one (or one class of) linker pass emits
the family, and `link.ViewOfEdgeType` maps the constant to one `gm:view` tag
(`structural` | `dynamic` | `security`). View tags classify edges for
diagram extraction; extraction configs declare the views they consume
(`ExtractionConfig.Views`, defaulting to `AllViews` when empty).

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

`link.ViewOfEdgeType` keeps these on their primary family view; security
filtering is applied at extraction time (`ViewSecurity` in extraction configs).

## Non-table edge

- `EdgeNetworkCall` (`NETWORK_RPC_CALL`) — BEHAVIORAL family member produced by
  the rpc linker that the family table above does not enumerate; kept because
  the rpc linker still emits it and the pipeline must not change behavior.

## View tags

`internal/visualization_engine/types` defines the vocabulary:

| Tag | Value | Covers |
|---|---|---|
| `ViewStructural` | `structural` | type/member ownership, runtime behavior, dependencies |
| `ViewDynamic` | `dynamic` | intra-function control and data movement |
| `ViewSecurity` | `security` | taint propagation and sinks |

`AllViews` lists every declared tag; `ExtractionConfig.Views` selects which
views a diagram reads (defaults to `AllViews` when empty). `link.ViewOfEdgeType`
maps a constant to its view.

## Predicate groups

For visualization, edges are additionally grouped by predicate
(`internal/visualization_engine/types/types.go`): `GroupCallGraph`,
`GroupTypeHierarchy`, `GroupComposition`, `GroupDataFlow`, `GroupControlFlow`,
`GroupStructural`, `GroupMessaging`, `GroupInfrastructure`, `GroupSecurity`,
`GroupBinding`, and `GroupAny`. Predicate membership is declared in
`internal/visualization_engine/extract/extractor.go`; each diagram type's
extraction config selects which groups it reads.