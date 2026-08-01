package akg

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage4"
	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/types"
)

// SerializeToTurtle transforms a CodePropertyGraph snapshot into W3C RDF Turtle (.ttl) format with RDF-star edge properties.
func SerializeToTurtle(graph *CodePropertyGraph, w io.Writer) error {
	if graph == nil {
		return fmt.Errorf("cannot serialize nil graph")
	}

	writeTTLPrefixes(w)

	// Metadata node: gm:schemaVersion and gm:version bound WAL replay on
	// recovery (AUDIT Issue 3 Phase 3B-7): a restored graph replays only WAL
	// entries with TxID > maxAppliedTx (= gm:version) instead of everything.
	writeTTLMetadata(w, graph)

	return writeGraphToWriter(w, graph)
}

func writeTTLPrefixes(w io.Writer) {
	fmt.Fprintf(w, "@prefix rdf: <http://www.w3.org/1999/02/22-rdf-syntax-ns#> .\n")
	fmt.Fprintf(w, "@prefix rdfs: <http://www.w3.org/2000/01/rdf-schema#> .\n")
	fmt.Fprintf(w, "@prefix xsd: <http://www.w3.org/2001/XMLSchema#> .\n")
	fmt.Fprintf(w, "@prefix gm: <http://glassmarble.org/schema#> .\n\n")
}

func writeTTLMetadata(w io.Writer, graph *CodePropertyGraph) {
	metaURI := "<http://glassmarble.org/node/metadata>"
	fmt.Fprintf(w, "%s a gm:MetaData ;\n", metaURI)
	fmt.Fprintf(w, "    gm:commitHash \"%s\" ;\n", escapeLiteral(graph.CommitHash))
	fmt.Fprintf(w, "    gm:schemaVersion %d ;\n", graph.SchemaVersion)
	fmt.Fprintf(w, "    gm:version %d ;\n", graph.Version)
	fmt.Fprintf(w, "    gm:name \"GlassMarble Project MetaData\" .\n\n")
}

// SerializeDeltaToTurtle transforms only the delta modifications and deletes into Turtle format for appending.
// version is the WAL replay bound (gm:version) of the committing graph; the
// delta's own metadata block must carry it or incremental appends would
// regress the replay bound to 0 (AUDIT Issue 3 Phase 3B-7).
func SerializeDeltaToTurtle(graph *stage4.Stage4Output, deletedNodes map[string]bool, version uint64, w io.Writer) error {
	if graph == nil {
		return fmt.Errorf("cannot serialize nil delta")
	}

	fmt.Fprintf(w, "\n# --- INCREMENTAL DELTA APPEND --- \n\n")

	// Write deleted nodes as tombstone node blocks.
	// A node block of the form `<uri> a gm:Deleted ; gm:status "DELETED" .`
	// removes the node and its incident edges on restore (AUDIT Issue 3
	// Phase 3B-6). The parser treats a node block whose gm:status is DELETED
	// as a deletion (and skips emitting the block as a node).
	for nodeID := range deletedNodes {
		nodeURI := types.FormatNodeURI(nodeID)
		fmt.Fprintf(w, "%s a gm:Deleted ;\n", nodeURI)
		fmt.Fprintf(w, "    gm:status \"DELETED\" .\n")
	}
	fmt.Fprintf(w, "\n")

	// The delta carries its own prefix + metadata block so appended files
	// never keep a stale WAL replay bound: scanTTLMetadata takes the max
	// gm:version across duplicate metadata blocks on restore.
	writeTTLPrefixes(w)
	tempGraph := NewCodePropertyGraph(graph.CommitHash)
	tempGraph.Version = version
	writeTTLMetadata(w, tempGraph)
	// Entrypoints and folder zones must survive delta appends too: the
	// serializer only writes gm:isEntrypoint / gm:primitiveZone from the
	// graph metadata, so they are carried over for the delta nodes
	// (AUDIT Issue 3 §3.2).
	tempGraph.Entrypoints = append([]string(nil), graph.EntrypointRegistry...)
	for k, v := range graph.FolderZones {
		tempGraph.FolderZones = tempGraph.FolderZones.Set(k, v)
	}
	nodes := NewCowMap[string, *stage4.ResolvedNode]()
	for id, n := range graph.GraphNodes {
		nodes = nodes.Set(id, n)
	}
	tempGraph.Nodes = nodes
	edges := NewCowMap[string, []stage4.ResolvedEdge]()
	for id, e := range graph.OutboundEdges {
		edges = edges.Set(id, e)
	}
	tempGraph.OutboundEdges = edges

	return writeGraphToWriter(w, tempGraph)
}

func writeGraphToWriter(w io.Writer, graph *CodePropertyGraph) error {

	entrypointSet := make(map[string]bool)
	for _, ep := range graph.Entrypoints {
		entrypointSet[ep] = true
	}

	// 3. Write Graph Nodes
	graph.Nodes.Iterate(func(nodeID string, node *stage4.ResolvedNode) {
		if node == nil {
			return
		}

		nodeURI := types.FormatNodeURI(nodeID)
		classType := mapKindToClass(node.Kind)

		fmt.Fprintf(w, "%s a %s ;\n", nodeURI, classType)
		fmt.Fprintf(w, "    gm:name \"%s\" ;\n", escapeLiteral(node.Name))

		if node.Primitive != "" {
			fmt.Fprintf(w, "    gm:primitiveType \"%s\" ;\n", escapeLiteral(node.Primitive))
		}

		if node.FileSpec.Path != "" {
			fileURI := types.FormatNodeURI("file:" + node.FileSpec.Path)
			fmt.Fprintf(w, "    gm:belongsToFile %s ;\n", fileURI)
		}

		if node.FileSpec.LineStart > 0 {
			fmt.Fprintf(w, "    gm:lineStart %d ;\n", node.FileSpec.LineStart)
		}
		if node.FileSpec.LineEnd > 0 {
			fmt.Fprintf(w, "    gm:lineEnd %d ;\n", node.FileSpec.LineEnd)
		}

		// Write dynamic properties (metrics, macro rules, blast radius, etc.)
		if node.Properties != nil {
			// Sort keys for deterministic output
			keys := make([]string, 0, len(node.Properties))
			for k := range node.Properties {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				if k == "macro_rules" {
					// Derived data only: macro inference output is recomputed on
					// restore and is intentionally NOT persisted (AUDIT Phase 3C-12).
					continue
				}
				val := node.Properties[k]
				cleanKey := strings.ReplaceAll(k, " ", "_")
				cleanKey = strings.ReplaceAll(cleanKey, "-", "_")
				fmt.Fprintf(w, "    gm:%s \"%s\" ;\n", cleanKey, escapeLiteral(val))
			}
		}
		if entrypointSet[nodeID] {
			fmt.Fprintf(w, "    gm:isEntrypoint true ;\n")
		}

		if node.Kind == "MODULE" && graph.FolderZones != nil {
			if zone, ok := graph.FolderZones.Get(nodeID); ok && zone != "" {
				fmt.Fprintf(w, "    gm:primitiveZone \"%s\" ;\n", escapeLiteral(zone))
			}
		}

		// Close statement
		fmt.Fprintf(w, "    .\n\n")
	})

	// 4. Write Outbound Edges with RDF-star line numbering. The TTL is
	// triple-oriented and the canonical parser keeps one edge per
	// (source, predicate, target) key, so parallel edges between the same
	// pair must be deduplicated here (keeping the highest line number) or
	// post-write verification fails on edge-count parity (AUDIT Issue 3
	// §3.2 / Issue 5 Phase 5A-1).
	graph.OutboundEdges.Iterate(func(sourceID string, edges []stage4.ResolvedEdge) {
		type dedupEdge struct {
			edge stage4.ResolvedEdge
			pred string
		}
		var ordered []dedupEdge
		index := make(map[string]int)
		for _, edge := range edges {
			predicate := mapEdgeTypeToPredicate(edge.Type)
			if predicate == "" {
				// Unknown edge type: never emit rdfs:seeAlso as a stand-in.
				// The graph must not fabricate vocabulary the ontology does
				// not declare (AUDIT Issue 3 Phase 3A-2).
				continue
			}
			key := predicate + "\x00" + edge.TargetID
			if i, ok := index[key]; ok {
				if edge.LineNumber > ordered[i].edge.LineNumber {
					ordered[i].edge.LineNumber = edge.LineNumber
				}
				continue
			}
			index[key] = len(ordered)
			ordered = append(ordered, dedupEdge{edge: edge, pred: predicate})
		}
		sourceURI := types.FormatNodeURI(sourceID)
		for _, de := range ordered {
			targetURI := types.FormatNodeURI(de.edge.TargetID)

			// Write base triple
			fmt.Fprintf(w, "%s %s %s .\n", sourceURI, de.pred, targetURI)

			// Write RDF-star edge attribute
			if de.edge.LineNumber > 0 {
				fmt.Fprintf(w, "<< %s %s %s >> gm:lineNumber %d .\n", sourceURI, de.pred, targetURI, de.edge.LineNumber)
			}
		}
	})

	return nil
}

func mapKindToClass(kind string) string {
	// SHARED KIND-VOCABULARY CONTRACT (AUDIT Issue 2 Phase 2A-5 / Issue 3
	// Phase 3A): kinds produced by the analysis engine map 1:1 to ontology
	// classes; extraction filters in internal/visualization_engine consume
	// these exact classes. Fallback classes (gm:TypeDecl, gm:Executable,
	// gm:ControlStructure, ...) remain for legacy engine kinds that still
	// emit them. Every class returned here must be declared in ontology.ttl
	// (enforced by ontology_test.go).
	switch kind {
	// Core structural kinds
	case "MODULE":
		return "gm:Module"
	case "NAMESPACE":
		return "gm:Namespace"
	case "FILE":
		return "gm:File"
	case "STRUCT":
		return "gm:Struct"
	case "CLASS":
		return "gm:Class"
	case "INTERFACE":
		return "gm:Interface"
	case "FUNCTION":
		return "gm:Function"
	case "METHOD":
		return "gm:Method"
	case "FIELD":
		return "gm:Member"
	case "PARAMETER":
		return "gm:Parameter"
	case "VARIABLE", "DFG_VAR":
		return "gm:Variable"
	case "PACKAGE":
		return "gm:Package"
	case "META_DATA":
		return "gm:MetaData"
	// Fallback classes for legacy engine kinds
	case "TYPE_DECL":
		return "gm:TypeDecl"
	case "EXECUTABLE":
		return "gm:Executable"
	case "IF_BRANCH", "LOOP_BRANCH", "SWITCH_BRANCH":
		return "gm:ControlStructure"
	case "CFG_SUMMARY":
		return "gm:CFGSummary"
	case "DFG_SUMMARY":
		return "gm:DFGSummary"
	case "EVENT_TOPIC":
		return "gm:EventTopic"
	case "VIRTUAL_DATABASE":
		return "gm:VirtualDatabase"
	case "VIRTUAL_ENDPOINT":
		return "gm:VirtualEndpoint"
	case "BLOCK":
		return "gm:Block"
	case "ANNOTATION", "DECORATOR":
		return "gm:Annotation"
	// Virtual / synthetic classes fabricated by the linker passes
	case "VIRTUAL_CONTEXT":
		return "gm:VirtualContext"
	case "VIRTUAL_QUEUE":
		return "gm:VirtualQueue"
	case "VIRTUAL_TAINT_SOURCE":
		return "gm:VirtualTaintSource"
	case "VIRTUAL_GLOBAL_STATE":
		return "gm:VirtualGlobalState"
	case "VIRTUAL_SECURITY_SINK":
		return "gm:VirtualSecuritySink"
	case "VIRTUAL_RESOURCE":
		return "gm:VirtualResource"
	case "VIRTUAL_CLOUD_API":
		return "gm:VirtualCloudAPI"
	case "EXTERNAL_SDK":
		return "gm:ExternalSDK"
	case "EXTERNAL_API":
		return "gm:ExternalAPI"
	case "EXTERNAL_FFI":
		return "gm:ExternalFFI"
	case "HEAP_ALLOCATION":
		return "gm:HeapAllocation"
	case "ABSTRACT_CONSTRAINT":
		return "gm:AbstractConstraint"
	case "CFG_FLOW":
		return "gm:CFGFlow"
	case "EXCEPTIONAL_BRANCH":
		return "gm:ExceptionalBranch"
	case "DELETED":
		return "gm:Deleted"
	default:
		return "rdfs:Class"
	}
}

// mapClassToKind is the inverse of mapKindToClass. It reconstructs an
// internal node kind from a serialized rdfs class URI so that graphs
// restored from Turtle keep their kinds across save/restore cycles.
func mapClassToKind(class string) string {
	switch class {
	case "gm:Module":
		return "MODULE"
	case "gm:Namespace":
		return "NAMESPACE"
	case "gm:File":
		return "FILE"
	case "gm:Struct":
		return "STRUCT"
	case "gm:Class":
		return "CLASS"
	case "gm:Interface":
		return "INTERFACE"
	case "gm:Function":
		return "FUNCTION"
	case "gm:Method":
		return "METHOD"
	case "gm:Member":
		return "FIELD"
	case "gm:Variable":
		return "VARIABLE"
	case "gm:Parameter":
		return "PARAMETER"
	case "gm:Package":
		return "PACKAGE"
	case "gm:TypeDecl":
		return "STRUCT"
	case "gm:Executable":
		return "FUNCTION"
	case "gm:ControlStructure":
		return "IF_BRANCH"
	case "gm:CFGSummary":
		return "CFG_SUMMARY"
	case "gm:DFGSummary":
		return "DFG_SUMMARY"
	case "gm:EventTopic":
		return "EVENT_TOPIC"
	case "gm:VirtualDatabase":
		return "VIRTUAL_DATABASE"
	case "gm:VirtualEndpoint":
		return "VIRTUAL_ENDPOINT"
	case "gm:Block":
		return "BLOCK"
	case "gm:Annotation":
		return "ANNOTATION"
	case "gm:MetaData":
		return "META_DATA"
	case "gm:VirtualContext":
		return "VIRTUAL_CONTEXT"
	case "gm:VirtualQueue":
		return "VIRTUAL_QUEUE"
	case "gm:VirtualTaintSource":
		return "VIRTUAL_TAINT_SOURCE"
	case "gm:VirtualGlobalState":
		return "VIRTUAL_GLOBAL_STATE"
	case "gm:VirtualSecuritySink":
		return "VIRTUAL_SECURITY_SINK"
	case "gm:VirtualResource":
		return "VIRTUAL_RESOURCE"
	case "gm:VirtualCloudAPI":
		return "VIRTUAL_CLOUD_API"
	case "gm:ExternalSDK":
		return "EXTERNAL_SDK"
	case "gm:ExternalAPI":
		return "EXTERNAL_API"
	case "gm:ExternalFFI":
		return "EXTERNAL_FFI"
	case "gm:HeapAllocation":
		return "HEAP_ALLOCATION"
	case "gm:AbstractConstraint":
		return "ABSTRACT_CONSTRAINT"
	case "gm:CFGFlow":
		return "CFG_FLOW"
	case "gm:ExceptionalBranch":
		return "EXCEPTIONAL_BRANCH"
	case "gm:Deleted":
		return "DELETED"
	default:
		return strings.TrimPrefix(class, "gm:")
	}
}

func mapEdgeTypeToPredicate(edgeType stage4.RelationshipType) string {
	switch edgeType {
	case stage4.EdgeCalls:
		return "gm:calls"
	case stage4.EdgeImplements:
		return "gm:inheritsFrom"
	case stage4.EdgeExtends:
		return "gm:extends"
	case stage4.EdgeComposes:
		return "gm:composes"
	case stage4.EdgeReferences:
		return "gm:references"
	case stage4.EdgeThrows:
		return "gm:throws"
	case stage4.EdgeSpawnsConcurrent:
		return "gm:spawnsConcurrent"
	case stage4.EdgeDispatchesEvent:
		return "gm:dispatchesEvent"
	case stage4.EdgeExposesEndpoint:
		return "gm:exposesEndpoint"
	case stage4.EdgeSecuritySink:
		return "gm:securitySink"
	case stage4.EdgeConsumesResource:
		return "gm:consumesResource"
	case stage4.EdgeMutatesGlobal:
		return "gm:mutatesGlobal"
	case stage4.EdgeAliasesType:
		return "gm:aliasesType"
	case stage4.EdgeControlFlow, stage4.EdgeConditionalBranch, stage4.EdgeLoopBranch, stage4.EdgeSwitchBranch:
		return "gm:controlFlowTo"
	case stage4.EdgeDataFlow:
		return "gm:dataFlowTo"
	case stage4.EdgeAliases:
		return "gm:aliasesPointer"
	case stage4.EdgeVulnerable:
		return "gm:vulnerableTaint"
	case stage4.EdgeInstantiates:
		return "gm:instantiatesGeneric"
	case stage4.EdgeSendsTo:
		return "gm:sendsMessage"
	case stage4.EdgeReceivesFrom:
		return "gm:receivesMessage"
	case stage4.EdgeCyclic:
		return "gm:cyclicDependency"
	case stage4.EdgeNetworkCall:
		return "gm:networkCall"
	case stage4.EdgeQueriesDB:
		return "gm:queriesDatabase"
	case stage4.EdgeCallsCloudAPI:
		return "gm:callsCloudAPI"
	case stage4.EdgeCatches:
		return "gm:catchesException"
	case stage4.EdgeDefers:
		return "gm:defersExecution"
	case stage4.EdgeBelongsTo:
		return "gm:belongsTo"
	// Structural & Membership Edges
	case stage4.EdgeDependsOn:
		return "gm:dependsOn"
	case stage4.EdgeContains:
		return "gm:contains"
	case stage4.EdgeMixes:
		return "gm:mixes"
	case stage4.EdgeHasField:
		return "gm:hasField"
	case stage4.EdgeHasParam:
		return "gm:hasParam"
	case stage4.EdgeReturns:
		return "gm:returns"
	// Phase 2 Enterprise Edges
	case stage4.EdgeContextCall:
		return "gm:contextualCall"
	case stage4.EdgePointsTo:
		return "gm:pointsTo"
	case stage4.EdgeHeapAlias:
		return "gm:heapAlias"
	case stage4.EdgeConstraint:
		return "gm:branchConstraint"
	case stage4.EdgeFFICall:
		return "gm:ffiCall"
	case stage4.EdgePublishes:
		return "gm:publishesEvent"
	case stage4.EdgeSubscribes:
		return "gm:subscribesEvent"
	case stage4.EdgeInjects:
		return "gm:diInjects"
	case stage4.EdgeEscapesToHeap:
		return "gm:escapesToHeap"
	default:
		// No silent stand-in vocabulary: unknown edge types are not
		// serialized at all (see writeGraphToWriter). Every relationship
		// type constant must map to a predicate declared in ontology.ttl
		// (enforced by ontology_test.go).
		return ""
	}
}

// mapPredicateToEdgeType is the inverse of mapEdgeTypeToPredicate. It
// reconstructs canonical RelationshipType constants from serialized
// predicates so that edges restored from Turtle keep their exact types
// (AUDIT Issue 3 Phase 3D-13).
func mapPredicateToEdgeType(pred string) stage4.RelationshipType {
	switch pred {
	case "gm:calls":
		return stage4.EdgeCalls
	case "gm:inheritsFrom":
		return stage4.EdgeImplements
	case "gm:extends":
		return stage4.EdgeExtends
	case "gm:composes":
		return stage4.EdgeComposes
	case "gm:references":
		return stage4.EdgeReferences
	case "gm:throws":
		return stage4.EdgeThrows
	case "gm:spawnsConcurrent":
		return stage4.EdgeSpawnsConcurrent
	case "gm:dispatchesEvent":
		return stage4.EdgeDispatchesEvent
	case "gm:exposesEndpoint":
		return stage4.EdgeExposesEndpoint
	case "gm:securitySink":
		return stage4.EdgeSecuritySink
	case "gm:consumesResource":
		return stage4.EdgeConsumesResource
	case "gm:mutatesGlobal":
		return stage4.EdgeMutatesGlobal
	case "gm:aliasesType":
		return stage4.EdgeAliasesType
	case "gm:controlFlowTo":
		return stage4.EdgeControlFlow
	case "gm:dataFlowTo":
		return stage4.EdgeDataFlow
	case "gm:aliasesPointer":
		return stage4.EdgeAliases
	case "gm:vulnerableTaint":
		return stage4.EdgeVulnerable
	case "gm:instantiatesGeneric":
		return stage4.EdgeInstantiates
	case "gm:sendsMessage":
		return stage4.EdgeSendsTo
	case "gm:receivesMessage":
		return stage4.EdgeReceivesFrom
	case "gm:cyclicDependency":
		return stage4.EdgeCyclic
	case "gm:networkCall":
		return stage4.EdgeNetworkCall
	case "gm:queriesDatabase":
		return stage4.EdgeQueriesDB
	case "gm:callsCloudAPI":
		return stage4.EdgeCallsCloudAPI
	case "gm:catchesException":
		return stage4.EdgeCatches
	case "gm:defersExecution":
		return stage4.EdgeDefers
	case "gm:belongsTo":
		return stage4.EdgeBelongsTo
	case "gm:dependsOn":
		return stage4.EdgeDependsOn
	case "gm:contains":
		return stage4.EdgeContains
	case "gm:mixes":
		return stage4.EdgeMixes
	case "gm:hasField":
		return stage4.EdgeHasField
	case "gm:hasParam":
		return stage4.EdgeHasParam
	case "gm:returns":
		return stage4.EdgeReturns
	case "gm:contextualCall":
		return stage4.EdgeContextCall
	case "gm:pointsTo":
		return stage4.EdgePointsTo
	case "gm:heapAlias":
		return stage4.EdgeHeapAlias
	case "gm:branchConstraint":
		return stage4.EdgeConstraint
	case "gm:ffiCall":
		return stage4.EdgeFFICall
	case "gm:publishesEvent":
		return stage4.EdgePublishes
	case "gm:subscribesEvent":
		return stage4.EdgeSubscribes
	case "gm:diInjects":
		return stage4.EdgeInjects
	case "gm:escapesToHeap":
		return stage4.EdgeEscapesToHeap
	default:
		// Unknown predicates (e.g. from legacy files): preserve the
		// predicate name as a RelationshipType string rather than dropping
		// the edge.
		return stage4.RelationshipType(strings.ToUpper(strings.TrimPrefix(pred, "gm:")))
	}
}

func escapeLiteral(str string) string {
	res := strings.ReplaceAll(str, "\\", "\\\\")
	res = strings.ReplaceAll(res, "\"", "\\\"")
	res = strings.ReplaceAll(res, "\n", "\\n")
	res = strings.ReplaceAll(res, "\r", "\\r")
	return res
}
