package akg

import (
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/link"
	"github.com/Syamchand123/GlassMarble/internal/product/ont"
)

// The Go-native AKG vocabulary: pure Go string constants and mapping
// functions replacing the RDF ontology substrate. The keys here are the
// vocabulary names consumed as property keys and ID prefixes by the
// analysis engine, the GraphJSON store, and the visualization bridge —
// no RDF machinery is involved (v3 JSON store plan §3/§4.6).

// mapKindToClass converts an internal node kind to its canonical
// vocabulary class name (link.KindToClass).
func mapKindToClass(kind string) string {
	return link.KindToClass(kind)
}

// mapClassToKind is the inverse of mapKindToClass. It reconstructs an
// internal node kind from a class name so that graphs restored from
// legacy Turtle keep their kinds across restore cycles.
func mapClassToKind(class string) string {
	return link.ClassToKind(class)
}

// mapEdgeTypeToPredicate converts a canonical RelationshipType constant
// to the predicate key emitted for it. Unknown edge types map to "" and
// are not emitted at all (no silent stand-in vocabulary).
func mapEdgeTypeToPredicate(edgeType link.RelationshipType) string {
	switch edgeType {
	case link.EdgeCalls:
		return ont.PredCalls
	case link.EdgeImplements:
		return ont.PredInheritsFrom
	case link.EdgeExtends:
		return ont.PredExtends
	case link.EdgeComposes:
		return ont.PredComposes
	case link.EdgeReferences:
		return ont.PredReferences
	case link.EdgeThrows:
		return ont.PredThrows
	case link.EdgeSpawnsConcurrent:
		return ont.PredSpawnsConcurrent
	case link.EdgeDispatchesEvent:
		return ont.PredDispatchesEvent
	case link.EdgeExposesEndpoint:
		return ont.PredExposesEndpoint
	case link.EdgeSecuritySink:
		return ont.PredSecuritySink
	case link.EdgeConsumesResource:
		return ont.PredConsumesResource
	case link.EdgeMutatesGlobal:
		return ont.PredMutatesGlobal
	case link.EdgeAliasesType:
		return ont.PredAliasesType
	case link.EdgeControlFlow, link.EdgeConditionalBranch, link.EdgeLoopBranch, link.EdgeSwitchBranch:
		return ont.PredControlFlowTo
	case link.EdgeDataFlow:
		return ont.PredDataFlowTo
	case link.EdgeAliases:
		return ont.PredAliasesPointer
	case link.EdgeVulnerable:
		return ont.PredVulnerableTaint
	case link.EdgeInstantiates:
		return ont.PredInstantiatesGeneric
	case link.EdgeVirtualContext:
		return ont.PredVirtualContextLink
	case link.EdgeSendsTo:
		return ont.PredSendsMessage
	case link.EdgeReceivesFrom:
		return ont.PredReceivesMessage
	case link.EdgeCyclic:
		return ont.PredCyclicDependency
	case link.EdgeNetworkCall:
		return ont.PredNetworkCall
	case link.EdgeQueriesDB:
		return ont.PredQueriesDatabase
	case link.EdgeCallsCloudAPI:
		return ont.PredCallsCloudAPI
	case link.EdgeCatches:
		return ont.PredCatchesException
	case link.EdgeDefers:
		return ont.PredDefersExecution
	case link.EdgeBelongsTo:
		return ont.PredBelongsTo
	case link.EdgeDependsOn:
		return ont.PredDependsOn
	case link.EdgeContains:
		return ont.PredContains
	case link.EdgeMixes:
		return ont.PredMixes
	case link.EdgeHasField:
		return ont.PredHasField
	case link.EdgeHasParam:
		return ont.PredHasParam
	case link.EdgeReturns:
		return ont.PredReturns
	case link.EdgeHasReceiver:
		return ont.PredHasReceiver
	case link.EdgeContextCall:
		return ont.PredContextualCall
	case link.EdgePointsTo:
		return ont.PredPointsTo
	case link.EdgeHeapAlias:
		return ont.PredHeapAlias
	case link.EdgeConstraint:
		return ont.PredBranchConstraint
	case link.EdgeFFICall:
		return ont.PredFfiCall
	case link.EdgePublishes:
		return ont.PredPublishesEvent
	case link.EdgeSubscribes:
		return ont.PredSubscribesEvent
	case link.EdgeInjects:
		return ont.PredDiInjects
	case link.EdgeEscapesToHeap:
		return ont.PredEscapesToHeap
	default:
		return ""
	}
}

// mapPredicateToEdgeType is the inverse of mapEdgeTypeToPredicate. It
// reconstructs canonical RelationshipType constants from serialized
// predicate keys so that restored edges keep their exact types.
func mapPredicateToEdgeType(pred string) link.RelationshipType {
	switch pred {
	case ont.PredCalls:
		return link.EdgeCalls
	case ont.PredInheritsFrom:
		return link.EdgeImplements
	case ont.PredExtends:
		return link.EdgeExtends
	case ont.PredComposes:
		return link.EdgeComposes
	case ont.PredReferences:
		return link.EdgeReferences
	case ont.PredThrows:
		return link.EdgeThrows
	case ont.PredSpawnsConcurrent:
		return link.EdgeSpawnsConcurrent
	case ont.PredDispatchesEvent:
		return link.EdgeDispatchesEvent
	case ont.PredExposesEndpoint:
		return link.EdgeExposesEndpoint
	case ont.PredSecuritySink:
		return link.EdgeSecuritySink
	case ont.PredConsumesResource:
		return link.EdgeConsumesResource
	case ont.PredMutatesGlobal:
		return link.EdgeMutatesGlobal
	case ont.PredAliasesType:
		return link.EdgeAliasesType
	case ont.PredControlFlowTo:
		return link.EdgeControlFlow
	case ont.PredDataFlowTo:
		return link.EdgeDataFlow
	case ont.PredAliasesPointer:
		return link.EdgeAliases
	case ont.PredVulnerableTaint:
		return link.EdgeVulnerable
	case ont.PredInstantiatesGeneric:
		return link.EdgeInstantiates
	case ont.PredVirtualContextLink:
		return link.EdgeVirtualContext
	case ont.PredSendsMessage:
		return link.EdgeSendsTo
	case ont.PredReceivesMessage:
		return link.EdgeReceivesFrom
	case ont.PredCyclicDependency:
		return link.EdgeCyclic
	case ont.PredNetworkCall:
		return link.EdgeNetworkCall
	case ont.PredQueriesDatabase:
		return link.EdgeQueriesDB
	case ont.PredCallsCloudAPI:
		return link.EdgeCallsCloudAPI
	case ont.PredCatchesException:
		return link.EdgeCatches
	case ont.PredDefersExecution:
		return link.EdgeDefers
	case ont.PredBelongsTo:
		return link.EdgeBelongsTo
	case ont.PredDependsOn:
		return link.EdgeDependsOn
	case ont.PredContains:
		return link.EdgeContains
	case ont.PredMixes:
		return link.EdgeMixes
	case ont.PredHasField:
		return link.EdgeHasField
	case ont.PredHasParam:
		return link.EdgeHasParam
	case ont.PredReturns:
		return link.EdgeReturns
	case ont.PredHasReceiver:
		return link.EdgeHasReceiver
	case ont.PredContextualCall:
		return link.EdgeContextCall
	case ont.PredPointsTo:
		return link.EdgePointsTo
	case ont.PredHeapAlias:
		return link.EdgeHeapAlias
	case ont.PredBranchConstraint:
		return link.EdgeConstraint
	case ont.PredFfiCall:
		return link.EdgeFFICall
	case ont.PredPublishesEvent:
		return link.EdgePublishes
	case ont.PredSubscribesEvent:
		return link.EdgeSubscribes
	case ont.PredDiInjects:
		return link.EdgeInjects
	case ont.PredEscapesToHeap:
		return link.EdgeEscapesToHeap
	default:
		// Unknown predicates (e.g. from legacy files): preserve the
		// predicate name as a RelationshipType string rather than dropping
		// the edge.
		return link.RelationshipType(strings.ToUpper(strings.TrimPrefix(pred, ont.PrefixGM)))
	}
}

// EdgeTypeToPredicate is the exported form of mapEdgeTypeToPredicate: it
// converts a canonical RelationshipType constant to the predicate string
// consumed by the visualization extraction configs
// (internal/visualization_engine/extract).
func EdgeTypeToPredicate(edgeType link.RelationshipType) string {
	return mapEdgeTypeToPredicate(edgeType)
}
