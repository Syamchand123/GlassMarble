package akg

import (
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage4"
	"github.com/Syamchand123/GlassMarble/internal/product/ont"
)

// The Go-native AKG vocabulary: pure Go string constants and mapping
// functions replacing the RDF ontology substrate. The keys here are the
// vocabulary names consumed as property keys and ID prefixes by the
// analysis engine, the GraphJSON store, and the visualization bridge —
// no RDF machinery is involved (v3 JSON store plan §3/§4.6).

// mapKindToClass converts an internal node kind to its canonical
// vocabulary class name (stage4.KindToClass).
func mapKindToClass(kind string) string {
	return stage4.KindToClass(kind)
}

// mapClassToKind is the inverse of mapKindToClass. It reconstructs an
// internal node kind from a class name so that graphs restored from
// legacy Turtle keep their kinds across restore cycles.
func mapClassToKind(class string) string {
	return stage4.ClassToKind(class)
}

// mapEdgeTypeToPredicate converts a canonical RelationshipType constant
// to the predicate key emitted for it. Unknown edge types map to "" and
// are not emitted at all (no silent stand-in vocabulary).
func mapEdgeTypeToPredicate(edgeType stage4.RelationshipType) string {
	switch edgeType {
	case stage4.EdgeCalls:
		return ont.PredCalls
	case stage4.EdgeImplements:
		return ont.PredInheritsFrom
	case stage4.EdgeExtends:
		return ont.PredExtends
	case stage4.EdgeComposes:
		return ont.PredComposes
	case stage4.EdgeReferences:
		return ont.PredReferences
	case stage4.EdgeThrows:
		return ont.PredThrows
	case stage4.EdgeSpawnsConcurrent:
		return ont.PredSpawnsConcurrent
	case stage4.EdgeDispatchesEvent:
		return ont.PredDispatchesEvent
	case stage4.EdgeExposesEndpoint:
		return ont.PredExposesEndpoint
	case stage4.EdgeSecuritySink:
		return ont.PredSecuritySink
	case stage4.EdgeConsumesResource:
		return ont.PredConsumesResource
	case stage4.EdgeMutatesGlobal:
		return ont.PredMutatesGlobal
	case stage4.EdgeAliasesType:
		return ont.PredAliasesType
	case stage4.EdgeControlFlow, stage4.EdgeConditionalBranch, stage4.EdgeLoopBranch, stage4.EdgeSwitchBranch:
		return ont.PredControlFlowTo
	case stage4.EdgeDataFlow:
		return ont.PredDataFlowTo
	case stage4.EdgeAliases:
		return ont.PredAliasesPointer
	case stage4.EdgeVulnerable:
		return ont.PredVulnerableTaint
	case stage4.EdgeInstantiates:
		return ont.PredInstantiatesGeneric
	case stage4.EdgeVirtualContext:
		return ont.PredVirtualContextLink
	case stage4.EdgeSendsTo:
		return ont.PredSendsMessage
	case stage4.EdgeReceivesFrom:
		return ont.PredReceivesMessage
	case stage4.EdgeCyclic:
		return ont.PredCyclicDependency
	case stage4.EdgeNetworkCall:
		return ont.PredNetworkCall
	case stage4.EdgeQueriesDB:
		return ont.PredQueriesDatabase
	case stage4.EdgeCallsCloudAPI:
		return ont.PredCallsCloudAPI
	case stage4.EdgeCatches:
		return ont.PredCatchesException
	case stage4.EdgeDefers:
		return ont.PredDefersExecution
	case stage4.EdgeBelongsTo:
		return ont.PredBelongsTo
	case stage4.EdgeDependsOn:
		return ont.PredDependsOn
	case stage4.EdgeContains:
		return ont.PredContains
	case stage4.EdgeMixes:
		return ont.PredMixes
	case stage4.EdgeHasField:
		return ont.PredHasField
	case stage4.EdgeHasParam:
		return ont.PredHasParam
	case stage4.EdgeReturns:
		return ont.PredReturns
	case stage4.EdgeHasReceiver:
		return ont.PredHasReceiver
	case stage4.EdgeContextCall:
		return ont.PredContextualCall
	case stage4.EdgePointsTo:
		return ont.PredPointsTo
	case stage4.EdgeHeapAlias:
		return ont.PredHeapAlias
	case stage4.EdgeConstraint:
		return ont.PredBranchConstraint
	case stage4.EdgeFFICall:
		return ont.PredFfiCall
	case stage4.EdgePublishes:
		return ont.PredPublishesEvent
	case stage4.EdgeSubscribes:
		return ont.PredSubscribesEvent
	case stage4.EdgeInjects:
		return ont.PredDiInjects
	case stage4.EdgeEscapesToHeap:
		return ont.PredEscapesToHeap
	default:
		return ""
	}
}

// mapPredicateToEdgeType is the inverse of mapEdgeTypeToPredicate. It
// reconstructs canonical RelationshipType constants from serialized
// predicate keys so that restored edges keep their exact types.
func mapPredicateToEdgeType(pred string) stage4.RelationshipType {
	switch pred {
	case ont.PredCalls:
		return stage4.EdgeCalls
	case ont.PredInheritsFrom:
		return stage4.EdgeImplements
	case ont.PredExtends:
		return stage4.EdgeExtends
	case ont.PredComposes:
		return stage4.EdgeComposes
	case ont.PredReferences:
		return stage4.EdgeReferences
	case ont.PredThrows:
		return stage4.EdgeThrows
	case ont.PredSpawnsConcurrent:
		return stage4.EdgeSpawnsConcurrent
	case ont.PredDispatchesEvent:
		return stage4.EdgeDispatchesEvent
	case ont.PredExposesEndpoint:
		return stage4.EdgeExposesEndpoint
	case ont.PredSecuritySink:
		return stage4.EdgeSecuritySink
	case ont.PredConsumesResource:
		return stage4.EdgeConsumesResource
	case ont.PredMutatesGlobal:
		return stage4.EdgeMutatesGlobal
	case ont.PredAliasesType:
		return stage4.EdgeAliasesType
	case ont.PredControlFlowTo:
		return stage4.EdgeControlFlow
	case ont.PredDataFlowTo:
		return stage4.EdgeDataFlow
	case ont.PredAliasesPointer:
		return stage4.EdgeAliases
	case ont.PredVulnerableTaint:
		return stage4.EdgeVulnerable
	case ont.PredInstantiatesGeneric:
		return stage4.EdgeInstantiates
	case ont.PredVirtualContextLink:
		return stage4.EdgeVirtualContext
	case ont.PredSendsMessage:
		return stage4.EdgeSendsTo
	case ont.PredReceivesMessage:
		return stage4.EdgeReceivesFrom
	case ont.PredCyclicDependency:
		return stage4.EdgeCyclic
	case ont.PredNetworkCall:
		return stage4.EdgeNetworkCall
	case ont.PredQueriesDatabase:
		return stage4.EdgeQueriesDB
	case ont.PredCallsCloudAPI:
		return stage4.EdgeCallsCloudAPI
	case ont.PredCatchesException:
		return stage4.EdgeCatches
	case ont.PredDefersExecution:
		return stage4.EdgeDefers
	case ont.PredBelongsTo:
		return stage4.EdgeBelongsTo
	case ont.PredDependsOn:
		return stage4.EdgeDependsOn
	case ont.PredContains:
		return stage4.EdgeContains
	case ont.PredMixes:
		return stage4.EdgeMixes
	case ont.PredHasField:
		return stage4.EdgeHasField
	case ont.PredHasParam:
		return stage4.EdgeHasParam
	case ont.PredReturns:
		return stage4.EdgeReturns
	case ont.PredHasReceiver:
		return stage4.EdgeHasReceiver
	case ont.PredContextualCall:
		return stage4.EdgeContextCall
	case ont.PredPointsTo:
		return stage4.EdgePointsTo
	case ont.PredHeapAlias:
		return stage4.EdgeHeapAlias
	case ont.PredBranchConstraint:
		return stage4.EdgeConstraint
	case ont.PredFfiCall:
		return stage4.EdgeFFICall
	case ont.PredPublishesEvent:
		return stage4.EdgePublishes
	case ont.PredSubscribesEvent:
		return stage4.EdgeSubscribes
	case ont.PredDiInjects:
		return stage4.EdgeInjects
	case ont.PredEscapesToHeap:
		return stage4.EdgeEscapesToHeap
	default:
		// Unknown predicates (e.g. from legacy files): preserve the
		// predicate name as a RelationshipType string rather than dropping
		// the edge.
		return stage4.RelationshipType(strings.ToUpper(strings.TrimPrefix(pred, ont.PrefixGM)))
	}
}

// EdgeTypeToPredicate is the exported form of mapEdgeTypeToPredicate: it
// converts a canonical RelationshipType constant to the predicate string
// consumed by the visualization extraction configs
// (internal/visualization_engine/stage1).
func EdgeTypeToPredicate(edgeType stage4.RelationshipType) string {
	return mapEdgeTypeToPredicate(edgeType)
}
