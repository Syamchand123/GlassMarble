// Package grounding — diagram.go
// Generates living Mermaid architecture and callgraph diagrams from the AKG.
package grounding

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/link"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/config"
)

// GenerateDiagram renders a living diagram from the AKG matching the DiagramRef specification.
// Returns a fenced markdown block (```mermaid ... ```).
func GenerateDiagram(ref config.DiagramRef, graph *akg.CodePropertyGraph) (string, error) {
	diagType := strings.ToLower(ref.Type)
	switch diagType {
	case "callgraph", "sequence":
		return generateCallgraph(ref.Entry, graph), nil
	case "dependency", "components", "component":
		return generateDependencyDiagram(ref.Scope, graph), nil
	case "layered", "architecture":
		return generateLayeredDiagram(graph), nil
	default:
		return generateDependencyDiagram(ref.Scope, graph), nil
	}
}

func generateCallgraph(entry string, graph *akg.CodePropertyGraph) string {
	var sb strings.Builder
	sb.WriteString("```mermaid\ngraph TD\n")

	if graph == nil || graph.OutboundEdges == nil || entry == "" {
		sb.WriteString("  EmptyNode[\"No call graph edges detected\"]\n```")
		return sb.String()
	}

	// BFS up to depth 2 from entry point
	visited := make(map[string]bool)
	type queueItem struct {
		id    string
		depth int
	}

	queue := []queueItem{{id: entry, depth: 0}}
	visited[entry] = true

	type edge struct {
		from, to string
	}
	var edges []edge

	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]

		if curr.depth >= 2 {
			continue
		}

		outEdges := graph.GetOutboundEdges(curr.id)
		for _, e := range outEdges {
			if e.Type == link.EdgeCalls {
				edges = append(edges, edge{from: curr.id, to: e.TargetID})
				if !visited[e.TargetID] {
					visited[e.TargetID] = true
					queue = append(queue, queueItem{id: e.TargetID, depth: curr.depth + 1})
				}
			}
		}
	}

	if len(edges) == 0 {
		sb.WriteString(fmt.Sprintf("  %s[\"%s\"]\n", sanitizeNodeID(entry), cleanSymbolName(entry)))
		sb.WriteString("```")
		return sb.String()
	}

	// Sort edges deterministically
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].from != edges[j].from {
			return edges[i].from < edges[j].from
		}
		return edges[i].to < edges[j].to
	})

	nodeNames := make(map[string]string)
	for _, e := range edges {
		fromID := sanitizeNodeID(e.from)
		toID := sanitizeNodeID(e.to)
		nodeNames[fromID] = cleanSymbolName(e.from)
		nodeNames[toID] = cleanSymbolName(e.to)
		sb.WriteString(fmt.Sprintf("  %s --> %s\n", fromID, toID))
	}

	// Define node labels
	for id, label := range nodeNames {
		sb.WriteString(fmt.Sprintf("  %s[\"%s\"]\n", id, label))
	}

	sb.WriteString("```")
	return sb.String()
}

func generateDependencyDiagram(scopePattern string, graph *akg.CodePropertyGraph) string {
	var sb strings.Builder
	sb.WriteString("```mermaid\ngraph LR\n")

	if graph == nil || graph.OutboundEdges == nil {
		sb.WriteString("  Empty[\"No package dependencies\"]\n```")
		return sb.String()
	}

	// Aggregate edges at package level
	pkgEdges := make(map[string]map[string]int)

	graph.OutboundEdges.Iterate(func(src string, edges []link.ResolvedEdge) {
		srcPkg := extractPackage(src)
		if srcPkg == "" {
			return
		}
		for _, e := range edges {
			tgtPkg := extractPackage(e.TargetID)
			if tgtPkg == "" || tgtPkg == srcPkg {
				continue
			}

			if pkgEdges[srcPkg] == nil {
				pkgEdges[srcPkg] = make(map[string]int)
			}
			pkgEdges[srcPkg][tgtPkg]++
		}
	})

	var edgeList []string
	for src, targets := range pkgEdges {
		for tgt, count := range targets {
			srcID := sanitizeNodeID(src)
			tgtID := sanitizeNodeID(tgt)
			edgeList = append(edgeList, fmt.Sprintf("  %s -->|%d| %s", srcID, count, tgtID))
		}
	}

	if len(edgeList) == 0 {
		sb.WriteString("  Root[\"Core\"]\n```")
		return sb.String()
	}

	sort.Strings(edgeList)
	// Cap to top 20 edges for clean rendering
	if len(edgeList) > 20 {
		edgeList = edgeList[:20]
	}

	for _, e := range edgeList {
		sb.WriteString(e + "\n")
	}

	sb.WriteString("```")
	return sb.String()
}

func generateLayeredDiagram(graph *akg.CodePropertyGraph) string {
	var sb strings.Builder
	sb.WriteString("```mermaid\ngraph TD\n")
	sb.WriteString("  subgraph Entrypoints [CLI & Commands]\n")
	sb.WriteString("    CMD[\"cmd\"]\n")
	sb.WriteString("  end\n")
	sb.WriteString("  subgraph Core [Intelligence Engines]\n")
	sb.WriteString("    DOC[\"doc_engine\"]\n")
	sb.WriteString("    AKG[\"akg\"]\n")
	sb.WriteString("    AI[\"ai_engine\"]\n")
	sb.WriteString("  end\n")
	sb.WriteString("  subgraph Foundation [Storage & System]\n")
	sb.WriteString("    STORE[\"storage\"]\n")
	sb.WriteString("    GIT[\"git\"]\n")
	sb.WriteString("  end\n")
	sb.WriteString("  CMD --> DOC\n")
	sb.WriteString("  DOC --> AKG\n")
	sb.WriteString("  DOC --> AI\n")
	sb.WriteString("  AKG --> STORE\n")
	sb.WriteString("  DOC --> GIT\n")
	sb.WriteString("```")
	return sb.String()
}

func sanitizeNodeID(raw string) string {
	safe := strings.ReplaceAll(raw, "/", "_")
	safe = strings.ReplaceAll(safe, ".", "_")
	safe = strings.ReplaceAll(safe, ":", "_")
	safe = strings.ReplaceAll(safe, "-", "_")
	safe = strings.ReplaceAll(safe, "*", "_")
	return "n_" + safe
}

func cleanSymbolName(fqn string) string {
	parts := strings.Split(fqn, "::")
	if len(parts) > 1 {
		return parts[len(parts)-1]
	}
	return fqn
}

func extractPackage(fqn string) string {
	parts := strings.Split(fqn, "::")
	pathPart := parts[0]
	idx := strings.LastIndex(pathPart, "/")
	if idx > 0 {
		return pathPart[:idx]
	}
	return pathPart
}
