package akg

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/link"
	"github.com/Syamchand123/GlassMarble/internal/product/ont"
)

// ExportNeo4jCypher exports the AKG graph to Cypher script format (.cypher) for Neo4j import.
func ExportNeo4jCypher(graph *CodePropertyGraph, w io.Writer) error {
	if graph == nil || graph.Nodes == nil {
		return fmt.Errorf("cannot export nil graph")
	}

	fmt.Fprintf(w, "// GlassMarble Architecture Knowledge Graph - Cypher Export\n")
	fmt.Fprintf(w, "// Commit: %s, Version: %d, Schema: %d\n\n", graph.CommitHash, graph.Version, graph.SchemaVersion)

	// Collect and sort nodes deterministically
	var nodeIDs []string
	graph.Nodes.Iterate(func(id string, _ *link.ResolvedNode) {
		nodeIDs = append(nodeIDs, id)
	})
	sort.Strings(nodeIDs)

	// Write node creation
	for _, id := range nodeIDs {
		node, ok := graph.Nodes.Get(id)
		if !ok || node == nil {
			continue
		}
		label := cleanCypherIdentifier(node.Kind)
		if label == "" {
			label = "GMNode"
		} else {
			label = "GMNode:" + label
		}

		props := make(map[string]interface{})
		props["id"] = node.ID
		if node.Name != "" {
			props["name"] = node.Name
		}
		if node.Kind != "" {
			props["kind"] = node.Kind
		}
		if node.Primitive != "" {
			props["primitive"] = node.Primitive
		}
		if node.FileSpec.Path != "" {
			props["file_path"] = node.FileSpec.Path
			props["line_start"] = node.FileSpec.LineStart
			props["line_end"] = node.FileSpec.LineEnd
		}
		for k, v := range node.Properties {
			props["prop_"+cleanCypherKey(k)] = v
		}

		fmt.Fprintf(w, "CREATE (:%s %s);\n", label, formatCypherMap(props))
	}

	fmt.Fprintf(w, "\n// Edges\n")

	// Collect and sort outbound edges deterministically
	var sourceIDs []string
	if graph.OutboundEdges != nil {
		graph.OutboundEdges.Iterate(func(srcID string, _ []link.ResolvedEdge) {
			sourceIDs = append(sourceIDs, srcID)
		})
	}
	sort.Strings(sourceIDs)

	for _, srcID := range sourceIDs {
		edges, _ := graph.OutboundEdges.Get(srcID)
		for _, edge := range edges {
			relType := cleanCypherIdentifier(string(edge.Type))
			if relType == "" {
				relType = "RELATION"
			}

			edgeProps := make(map[string]interface{})
			if edge.LineNumber > 0 {
				edgeProps["line_number"] = edge.LineNumber
			}
			if edge.Confidence > 0 {
				edgeProps["confidence"] = edge.Confidence
			}
			if edge.IsCycle {
				edgeProps["is_cycle"] = true
			}
			for k, v := range edge.Properties {
				edgeProps["prop_"+cleanCypherKey(k)] = v
			}

			fmt.Fprintf(w, "MATCH (s {id: %q}), (t {id: %q}) CREATE (s)-[:%s %s]->(t);\n",
				srcID, edge.TargetID, relType, formatCypherMap(edgeProps))
		}
	}

	return nil
}

func cleanCypherIdentifier(s string) string {
	var sb strings.Builder
	for _, r := range s {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

func cleanCypherKey(s string) string {
	s = strings.TrimPrefix(s, ont.PrefixGM)
	s = strings.TrimPrefix(s, "http://glassmarble.org/schema/")
	return cleanCypherIdentifier(s)
}

func formatCypherMap(props map[string]interface{}) string {
	var keys []string
	for k := range props {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var parts []string
	for _, k := range keys {
		v := props[k]
		switch val := v.(type) {
		case string:
			parts = append(parts, fmt.Sprintf("%s: %q", k, val))
		case int:
			parts = append(parts, fmt.Sprintf("%s: %d", k, val))
		case int64:
			parts = append(parts, fmt.Sprintf("%s: %d", k, val))
		case float32:
			parts = append(parts, fmt.Sprintf("%s: %f", k, val))
		case float64:
			parts = append(parts, fmt.Sprintf("%s: %f", k, val))
		case bool:
			parts = append(parts, fmt.Sprintf("%s: %t", k, val))
		default:
			parts = append(parts, fmt.Sprintf("%s: %q", k, fmt.Sprintf("%v", val)))
		}
	}
	return "{" + strings.Join(parts, ", ") + "}"
}
