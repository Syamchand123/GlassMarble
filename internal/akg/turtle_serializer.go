package akg

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage4"
	"github.com/Syamchand123/GlassMarble/internal/product/ont"
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
	fmt.Fprintf(w, "@prefix %s <%s> .\n\n", ont.PrefixGM, ont.SchemaNS)
}

func writeTTLMetadata(w io.Writer, graph *CodePropertyGraph) {
	metaURI := "<http://glassmarble.org/node/metadata>"
	fmt.Fprintf(w, "%s a %s ;\n", metaURI, ont.PredMetaData)
	fmt.Fprintf(w, "    %s \"%s\" ;\n", ont.PredCommitHash, escapeLiteral(graph.CommitHash))
	fmt.Fprintf(w, "    %s %d ;\n", ont.PredSchemaVersion, graph.SchemaVersion)
	fmt.Fprintf(w, "    %s %d ;\n", ont.PredVersion, graph.Version)
	fmt.Fprintf(w, "    %s \"1.0.0-overhaul\" ;\n", ont.PredAnalyzerVersion)
	fmt.Fprintf(w, "    %s \"%s\" ;\n", ont.PredGeneratedAt, time.Now().UTC().Format(time.RFC3339))
	fmt.Fprintf(w, "    %s \"structural\" ;\n", ont.PredViews)
	fmt.Fprintf(w, "    %s \"architecture\" ;\n", ont.PredLinkLevel)
	fmt.Fprintf(w, "    %s \"GlassMarble Project MetaData\" .\n\n", ont.PredName)
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

	// Write deleted nodes as tombstone node blocks in deterministic key order.
	deletedKeys := make([]string, 0, len(deletedNodes))
	for nodeID := range deletedNodes {
		deletedKeys = append(deletedKeys, nodeID)
	}
	sort.Strings(deletedKeys)
	for _, nodeID := range deletedKeys {
		nodeURI := types.FormatNodeURI(nodeID)
		fmt.Fprintf(w, "%s a %s ;\n", nodeURI, ont.PredDeleted)
		fmt.Fprintf(w, "    %s \"DELETED\" .\n", ont.PredStatus)
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

	// 3. Write Graph Nodes in lexicographical key order for determinism (K-01 / §18.3)
	var nodeIDs []string
	graph.Nodes.Iterate(func(nodeID string, node *stage4.ResolvedNode) {
		if node != nil {
			nodeIDs = append(nodeIDs, nodeID)
		}
	})
	sort.Strings(nodeIDs)

	storeCode := GetStoreCode()

	for _, nodeID := range nodeIDs {
		node, _ := graph.Nodes.Get(nodeID)
		if node == nil {
			continue
		}

		nodeURI := types.FormatNodeURI(nodeID)
		classType := mapKindToClass(node.Kind)

		fmt.Fprintf(w, "%s a %s ;\n", nodeURI, classType)
		fmt.Fprintf(w, "    %s \"%s\" ;\n", ont.PredName, escapeLiteral(node.Name))

		if node.Primitive != "" {
			fmt.Fprintf(w, "    %s \"%s\" ;\n", ont.PredPrimitiveType, escapeLiteral(node.Primitive))
		}

		if node.FileSpec.Path != "" {
			fileURI := types.FormatNodeURI("file:" + node.FileSpec.Path)
			fmt.Fprintf(w, "    %s %s ;\n", ont.PredBelongsToFile, fileURI)
		}

		if node.FileSpec.LineStart > 0 {
			fmt.Fprintf(w, "    %s %d ;\n", ont.PredLineStart, node.FileSpec.LineStart)
		}
		if node.FileSpec.LineEnd > 0 {
			fmt.Fprintf(w, "    %s %d ;\n", ont.PredLineEnd, node.FileSpec.LineEnd)
		}

		// Write dynamic properties without in-place map mutation (K-05 / thread safety)
		hasContentVal := "false"
		if node.Properties != nil {
			keys := make([]string, 0, len(node.Properties))
			for k := range node.Properties {
				if k == "macro_rules" || k == "code" || k == "hasContent" {
					continue
				}
				if k == "content" {
					if !storeCode {
						continue
					}
					switch node.Kind {
					case "STRUCT", "CLASS", "INTERFACE", "TYPE_DECL", "FUNCTION", "METHOD":
						hasContentVal = "true"
					default:
						continue
					}
				}
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				val := node.Properties[k]
				if k == "content" && len(val) > MaxContentLength {
					val = val[:MaxContentLength]
				}
				cleanKey := strings.ReplaceAll(k, " ", "_")
				cleanKey = strings.ReplaceAll(cleanKey, "-", "_")
				fmt.Fprintf(w, "    %s%s \"%s\" ;\n", ont.PrefixGM, cleanKey, escapeLiteral(val))
			}
		}
		fmt.Fprintf(w, "    %s \"%s\" ;\n", ont.PredHasContent, hasContentVal)
		if entrypointSet[nodeID] {
			fmt.Fprintf(w, "    %s true ;\n", ont.PredIsEntrypoint)
		}

		if node.Kind == "MODULE" && graph.FolderZones != nil {
			if zone, ok := graph.FolderZones.Get(nodeID); ok && zone != "" {
				fmt.Fprintf(w, "    %s \"%s\" ;\n", ont.PredPrimitiveZone, escapeLiteral(zone))
			}
		}

		// Close statement
		fmt.Fprintf(w, "    .\n\n")
	}

	// 4. Write Outbound Edges as SINGLE RDF-star statements (K-01 / W2-01).
	// Eliminate base triple double-writes. Sort lexicographically by source and target for determinism.
	var sourceIDs []string
	graph.OutboundEdges.Iterate(func(sourceID string, _ []stage4.ResolvedEdge) {
		sourceIDs = append(sourceIDs, sourceID)
	})
	sort.Strings(sourceIDs)

	for _, sourceID := range sourceIDs {
		edges, _ := graph.OutboundEdges.Get(sourceID)
		type dedupEdge struct {
			edge stage4.ResolvedEdge
			pred string
		}
		var ordered []dedupEdge
		index := make(map[string]int)
		for _, edge := range edges {
			predicate := mapEdgeTypeToPredicate(edge.Type)
			if predicate == "" {
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
		// Sort edges by predicate and target ID for determinism
		sort.Slice(ordered, func(i, j int) bool {
			if ordered[i].pred != ordered[j].pred {
				return ordered[i].pred < ordered[j].pred
			}
			return ordered[i].edge.TargetID < ordered[j].edge.TargetID
		})

		sourceURI := types.FormatNodeURI(sourceID)
		for _, de := range ordered {
			targetURI := types.FormatNodeURI(de.edge.TargetID)

			// Single RDF-star statement with attributes (K-01)
			fmt.Fprintf(w, "<< %s %s %s >>", sourceURI, de.pred, targetURI)
			var attrs []string
			if de.edge.LineNumber > 0 {
				attrs = append(attrs, fmt.Sprintf("%s %d", ont.PredLineNumber, de.edge.LineNumber))
			}
			if de.edge.Confidence > 0 {
				attrs = append(attrs, fmt.Sprintf("%s %g", ont.PredConfidence, de.edge.Confidence))
			}
			attrs = append(attrs, fmt.Sprintf("%s \"structural\"", ont.PredView))
			if len(de.edge.Properties) > 0 {
				propKeys := make([]string, 0, len(de.edge.Properties))
				for k := range de.edge.Properties {
					propKeys = append(propKeys, k)
				}
				sort.Strings(propKeys)
				for _, k := range propKeys {
					v := de.edge.Properties[k]
					attrs = append(attrs, fmt.Sprintf("%s%s \"%s\"", ont.PrefixGM, k, escapeLiteral(v)))
				}
			}

			if len(attrs) > 0 {
				fmt.Fprintf(w, " %s .\n", strings.Join(attrs, " ; "))
			} else {
				fmt.Fprintf(w, " .\n")
			}
		}
	}

	return nil
}

func mapKindToClass(kind string) string {
	return stage4.KindToClass(kind)
}

// mapClassToKind is the inverse of mapKindToClass. It reconstructs an
// internal node kind from a serialized rdfs class URI so that graphs
// restored from Turtle keep their kinds across save/restore cycles.
func mapClassToKind(class string) string {
	return stage4.ClassToKind(class)
}

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
	// Structural & Membership Edges
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
	// Phase 2 Enterprise Edges
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
// converts a canonical RelationshipType constant to the visualization
// predicate string consumed by the extraction configs
// (internal/visualization_engine/stage1).
func EdgeTypeToPredicate(edgeType stage4.RelationshipType) string {
	return mapEdgeTypeToPredicate(edgeType)
}

func escapeLiteral(str string) string {
	res := strings.ReplaceAll(str, "\\", "\\\\")
	res = strings.ReplaceAll(res, "\"", "\\\"")
	res = strings.ReplaceAll(res, "\n", "\\n")
	res = strings.ReplaceAll(res, "\r", "\\r")
	return res
}
