package akg

import (
	"bytes"
	"regexp"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage4"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// AUDIT Issue 3 Phase 3A-1: ontology conformance.
// The embedded ontology.ttl is the single source of truth for the serializer
// vocabulary. These tests assert that everything the serializer can emit is
// declared, and that a fully-populated serialization conforms to the ontology.
// ============================================================================

func TestOntologyEmbedded(t *testing.T) {
	if !OntologyEmbedded() {
		t.Fatal("ontology.ttl was not embedded at build time (go:embed failed)")
	}
	for _, required := range []string{
		"gm:Ontology", "gm:MetaData", "gm:calls", "gm:schemaVersion", "gm:status", "gm:Deleted",
		"gm:view", "gm:views",
	} {
		if !isOntologyTermDeclared(required) {
			t.Errorf("ontology missing required term %s", required)
		}
	}
}

// allEmittedKinds lists every kind handled by mapKindToClass.
func allEmittedKinds() []string {
	return []string{
		"MODULE", "NAMESPACE", "FILE", "STRUCT", "CLASS", "INTERFACE",
		"FUNCTION", "METHOD", "FIELD", "PARAMETER", "VARIABLE", "DFG_VAR",
		"IF_BRANCH", "LOOP_BRANCH", "SWITCH_BRANCH", "TYPE_DECL", "EXECUTABLE",
		"CFG_SUMMARY", "DFG_SUMMARY", "EVENT_TOPIC", "VIRTUAL_DATABASE",
		"VIRTUAL_ENDPOINT", "BLOCK", "ANNOTATION", "DECORATOR", "PACKAGE",
		"META_DATA",
		"VIRTUAL_CONTEXT", "VIRTUAL_QUEUE", "VIRTUAL_TAINT_SOURCE",
		"VIRTUAL_GLOBAL_STATE", "VIRTUAL_SECURITY_SINK", "VIRTUAL_RESOURCE",
		"VIRTUAL_CLOUD_API", "EXTERNAL_SDK", "EXTERNAL_API", "EXTERNAL_FFI",
		"HEAP_ALLOCATION", "ABSTRACT_CONSTRAINT", "CFG_FLOW", "EXCEPTIONAL_BRANCH",
	}
}

// allEmittedEdgeTypes lists every RelationshipType handled by mapEdgeTypeToPredicate.
func allEmittedEdgeTypes() []stage4.RelationshipType {
	return []stage4.RelationshipType{
		stage4.EdgeCalls, stage4.EdgeImplements, stage4.EdgeExtends, stage4.EdgeMixes,
		stage4.EdgeHasField, stage4.EdgeHasParam, stage4.EdgeReturns, stage4.EdgeThrows,
		stage4.EdgeDependsOn, stage4.EdgeComposes, stage4.EdgeReferences,
		stage4.EdgeSpawnsConcurrent, stage4.EdgeDispatchesEvent,
		stage4.EdgeExposesEndpoint, stage4.EdgeSecuritySink, stage4.EdgeConsumesResource,
		stage4.EdgeMutatesGlobal, stage4.EdgeAliasesType, stage4.EdgeContains,
		stage4.EdgeControlFlow, stage4.EdgeConditionalBranch, stage4.EdgeLoopBranch,
		stage4.EdgeSwitchBranch, stage4.EdgeCatches, stage4.EdgeDefers,
		stage4.EdgeDataFlow, stage4.EdgeAliases, stage4.EdgeVulnerable,
		stage4.EdgeInstantiates, stage4.EdgeSendsTo, stage4.EdgeReceivesFrom,
		stage4.EdgeCyclic, stage4.EdgeNetworkCall, stage4.EdgeQueriesDB,
		stage4.EdgeCallsCloudAPI, stage4.EdgeContextCall, stage4.EdgePointsTo,
		stage4.EdgeHeapAlias, stage4.EdgeConstraint, stage4.EdgeFFICall,
		stage4.EdgePublishes, stage4.EdgeSubscribes, stage4.EdgeInjects,
		stage4.EdgeEscapesToHeap, stage4.EdgeBelongsTo, stage4.EdgeHasReceiver,
	}
}

// TestOntologyDeclaresEdgeViews implements §4.2.1: every edge-type constant
// maps to a declared predicate (TestOntologyDeclaresAllEmittedPredicates) and
// a declared view. Views must be one of the gm:view vocabulary values
// ("structural" | "dynamic" | "security") declared in ontology.ttl.
func TestOntologyDeclaresEdgeViews(t *testing.T) {
	valid := map[string]bool{"structural": true, "dynamic": true, "security": true}
	for _, et := range allEmittedEdgeTypes() {
		view := stage4.ViewOfEdgeType(et)
		if view == "" {
			t.Errorf("edge type %s has no declared view", et)
			continue
		}
		if !valid[view] {
			t.Errorf("edge type %s maps to view %q which is not a declared gm:view value", et, view)
		}
	}
}

// dynamicPropertyVocabulary lists every gm:<key> the serializer may emit from
// node.Properties (AUDIT Issue 3 Appendix A emitted-but-undeclared list).
func dynamicPropertyVocabulary() []string {
	return []string{
		"content", "condition", "module_name", "file_path", "fully_qualified_name",
		"namespace_scope", "local_boundary", "receiver_type", "is_async", "type_params",
		"primitive_risk_score", "primitive_risk_level", "architecture_tier",
		"architectural_violations", "has_behavioral_primitives", "is_header", "role",
		"is_external", "primitive", "base_target", "caller_ctx", "logic", "param_count",
		"var_count", "params", "vars", "ffi_lang", "data_sensitivity_level",
		"data_privacy_violation", "n_plus_one_query_warning", "performance_hot_path",
		"resilience", "observability_blindspot", "macro_rules", "blast_radius",
		"instability", "pagerank", "betweenness_centrality", "cohesion", "hash", "code",
		"is_entrypoint", "has_async_side_effects",
	}
}

// parserAcceptedClasses lists every class the canonical parser can accept or
// fabricate but that the serializer never emits (AUDIT Issue 3 Phase 3A-2
// completeness): gm:External is fabricated by parseNodeBlock from ext:
// rdfs:Class blocks and referenced by the extraction filters; the legacy
// classes come from the parser's own testdata (all_kinds.ttl). Doctor's
// conformance scan flags anything undeclared, so each must be declared.
func parserAcceptedClasses() []string {
	return []string{
		"gm:External", "gm:Database", "gm:ExternalSystem",
	}
}

func TestOntologyDeclaresParserAcceptedClasses(t *testing.T) {
	for _, class := range parserAcceptedClasses() {
		if !isOntologyTermDeclared(class) {
			t.Errorf("class %s is accepted/fabricated by the canonical parser but not declared in ontology.ttl", class)
		}
	}
}

func TestOntologyDeclaresAllEmittedClasses(t *testing.T) {
	for _, kind := range allEmittedKinds() {
		class := mapKindToClass(kind)
		if !isOntologyTermDeclared(class) {
			t.Errorf("class %s (kind %s) is emitted by mapKindToClass but not declared in ontology.ttl", class, kind)
		}
	}
}

func TestOntologyDeclaresAllEmittedPredicates(t *testing.T) {
	for _, et := range allEmittedEdgeTypes() {
		pred := mapEdgeTypeToPredicate(et)
		if pred == "" {
			t.Errorf("edge type %s has no predicate mapping", et)
			continue
		}
		if !isOntologyTermDeclared(pred) {
			t.Errorf("predicate %s (edge type %s) is emitted by mapEdgeTypeToPredicate but not declared in ontology.ttl", pred, et)
		}
	}
}

func TestOntologyDeclaresFixedSerializerKeys(t *testing.T) {
	// Keys the serializer writes unconditionally or structurally.
	fixed := []string{
		"name", "primitiveType", "belongsToFile", "lineStart", "lineEnd",
		"isEntrypoint", "primitiveZone", "commitHash", "schemaVersion", "version",
		"status", "lineNumber",
	}
	for _, key := range fixed {
		if !isOntologyTermDeclared("gm:" + key) {
			t.Errorf("serializer key gm:%s not declared in ontology.ttl", key)
		}
	}
}

func TestOntologyDeclaresDynamicVocabulary(t *testing.T) {
	for _, key := range dynamicPropertyVocabulary() {
		if !isOntologyTermDeclared("gm:" + key) {
			t.Errorf("property key gm:%s emitted by serializer not declared in ontology.ttl", key)
		}
	}
}

// TestSerializedOutputConformsToOntology serializes a fully-populated graph
// (every kind, every edge type, every known property key) and asserts that
// every gm: predicate in the output is declared in the ontology.
func TestSerializedOutputConformsToOntology(t *testing.T) {
	g := NewCodePropertyGraph("conformance")
	g.Version = 7

	for i, kind := range allEmittedKinds() {
		id := "kind::" + kind
		g.Nodes = g.Nodes.Set(id, &stage4.ResolvedNode{
			ID: id, Kind: kind, Name: kind,
			FileSpec: stage4.LocationMeta{Path: "a.go", LineStart: i + 1, LineEnd: i + 10},
		})
	}

	g.Nodes = g.Nodes.Set("propnode", &stage4.ResolvedNode{
		ID: "propnode", Kind: "CLASS", Name: "PropNode",
		Properties: map[string]string{"name": "x"},
	})
	for _, key := range dynamicPropertyVocabulary() {
		if key == "name" {
			continue
		}
		node, _ := g.Nodes.Get("propnode")
		if node.Properties == nil {
			node.Properties = make(map[string]string)
		}
		node.Properties[key] = "value {with} |special| ^chars^"
	}

	edges := make([]stage4.ResolvedEdge, 0, len(allEmittedEdgeTypes()))
	for i, et := range allEmittedEdgeTypes() {
		edges = append(edges, stage4.ResolvedEdge{
			SourceID: "kind::MODULE", TargetID: "kind::FILE", Type: et, LineNumber: i + 1,
		})
	}
	g.OutboundEdges = g.OutboundEdges.Set("kind::MODULE", edges)
	g.Entrypoints = []string{"kind::FUNCTION"}

	var buf bytes.Buffer
	require.NoError(t, SerializeToTurtle(g, &buf))

	output := buf.String()
	predRe := regexp.MustCompile(`\bgm:([A-Za-z0-9_]+)\b`)
	for _, m := range predRe.FindAllStringSubmatch(output, -1) {
		if !isOntologyTermDeclared("gm:" + m[1]) {
			t.Errorf("serialized output uses undeclared predicate gm:%s", m[1])
		}
	}
}
