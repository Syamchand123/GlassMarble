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

	// 1. Write Namespaces & Prefix Declarations
	fmt.Fprintf(w, "@prefix rdf: <http://www.w3.org/1999/02/22-rdf-syntax-ns#> .\n")
	fmt.Fprintf(w, "@prefix rdfs: <http://www.w3.org/2000/01/rdf-schema#> .\n")
	fmt.Fprintf(w, "@prefix xsd: <http://www.w3.org/2001/XMLSchema#> .\n")
	fmt.Fprintf(w, "@prefix gm: <http://glassmarble.org/schema#> .\n\n")

	// 2. Write Metadata Node
	metaURI := "<http://glassmarble.org/node/metadata>"
	fmt.Fprintf(w, "%s a gm:MetaData ;\n", metaURI)
	fmt.Fprintf(w, "    gm:commitHash \"%s\" ;\n", escapeLiteral(graph.CommitHash))
	fmt.Fprintf(w, "    gm:name \"GlassMarble Project MetaData\" .\n\n")

	return writeGraphToWriter(w, graph)
}

// SerializeDeltaToTurtle transforms only the delta modifications and deletes into Turtle format for appending.
func SerializeDeltaToTurtle(graph *stage4.Stage4Output, deletedNodes map[string]bool, w io.Writer) error {
	if graph == nil {
		return fmt.Errorf("cannot serialize nil delta")
	}

	fmt.Fprintf(w, "\n# --- INCREMENTAL DELTA APPEND --- \n\n")

	// Write deleted nodes as special tombstone triples
	for nodeID := range deletedNodes {
		nodeURI := types.FormatNodeURI(nodeID)
		fmt.Fprintf(w, "%s gm:status \"DELETED\" .\n", nodeURI)
	}
	fmt.Fprintf(w, "\n")

	// Write the new nodes and edges using the same serialization logic
	// We construct a temporary CodePropertyGraph for serialization
	tempGraph := NewCodePropertyGraph(graph.CommitHash)
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

	// 4. Write Outbound Edges with RDF-star line numbering
	graph.OutboundEdges.Iterate(func(sourceID string, edges []stage4.ResolvedEdge) {
		for _, edge := range edges {
			sourceURI := types.FormatNodeURI(sourceID)
			targetURI := types.FormatNodeURI(edge.TargetID)
			predicate := mapEdgeTypeToPredicate(edge.Type)

			// Write base triple
			fmt.Fprintf(w, "%s %s %s .\n", sourceURI, predicate, targetURI)

			// Write RDF-star edge attribute
			if edge.LineNumber > 0 {
				fmt.Fprintf(w, "<< %s %s %s >> gm:lineNumber %d .\n", sourceURI, predicate, targetURI, edge.LineNumber)
			}
		}
	})

	return nil
}

func mapKindToClass(kind string) string {
	switch kind {
	case "MODULE":
		return "gm:Namespace"
	case "FILE":
		return "gm:File"
	case "STRUCT", "CLASS":
		return "gm:TypeDecl"
	case "INTERFACE":
		return "gm:TypeDecl"
	case "FUNCTION", "METHOD":
		return "gm:Executable"
	case "IF_BRANCH", "LOOP_BRANCH", "SWITCH_BRANCH":
		return "gm:ControlStructure"
	case "DFG_VAR":
		return "gm:Variable"
	case "PARAMETER":
		return "gm:Parameter"
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
	default:
		return "rdfs:Class"
	}
}

// mapClassToKind is the inverse of mapKindToClass. It reconstructs an
// internal node kind from a serialized rdfs class URI so that graphs
// restored from Turtle keep their kinds across save/restore cycles.
func mapClassToKind(class string) string {
	switch class {
	case "gm:Namespace":
		return "MODULE"
	case "gm:File":
		return "FILE"
	case "gm:TypeDecl":
		return "STRUCT"
	case "gm:Executable":
		return "FUNCTION"
	case "gm:ControlStructure":
		return "IF_BRANCH"
	case "gm:Variable":
		return "DFG_VAR"
	case "gm:Parameter":
		return "PARAMETER"
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
		return "rdfs:seeAlso"
	}
}

func escapeLiteral(str string) string {
	res := strings.ReplaceAll(str, "\\", "\\\\")
	res = strings.ReplaceAll(res, "\"", "\\\"")
	res = strings.ReplaceAll(res, "\n", "\\n")
	res = strings.ReplaceAll(res, "\r", "\\r")
	return res
}
