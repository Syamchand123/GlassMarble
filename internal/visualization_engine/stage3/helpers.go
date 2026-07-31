package stage3

import (
	"fmt"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/types"
)

func sanitizeName(name string) string {
	res := name
	res = strings.ReplaceAll(res, "%20", "_")
	res = strings.ReplaceAll(res, "%3A", "_")
	res = strings.ReplaceAll(res, "%2F", "_")
	res = strings.ReplaceAll(res, "%5C", "_")
	res = strings.ReplaceAll(res, "::", "_")
	res = strings.ReplaceAll(res, ":", "_")
	res = strings.ReplaceAll(res, ".", "_")
	res = strings.ReplaceAll(res, "/", "_")
	res = strings.ReplaceAll(res, "\\", "_")
	res = strings.ReplaceAll(res, "-", "_")
	res = strings.ReplaceAll(res, "%", "_")
	res = strings.ReplaceAll(res, "(", "_")
	res = strings.ReplaceAll(res, ")", "_")
	res = strings.ReplaceAll(res, "<", "_")
	res = strings.ReplaceAll(res, ">", "_")
	res = strings.ReplaceAll(res, "[", "_")
	res = strings.ReplaceAll(res, "]", "_")
	res = strings.ReplaceAll(res, " ", "_")
	return res
}

func getParticipantLabel(id string) string {
	_, rec, sym := parseFQN(id)
	if rec != "" {
		return fmt.Sprintf("\"%s::%s\"", sanitizeMermaidLabel(rec), sanitizeMermaidLabel(sym))
	}
	if sym != "" {
		return fmt.Sprintf("\"%s\"", sanitizeMermaidLabel(sym))
	}
	return fmt.Sprintf("\"%s\"", sanitizeMermaidLabel(id))
}

func parseFQN(id string) (path, receiver, symbol string) {
	parts := strings.Split(id, "::")
	if len(parts) == 3 {
		return parts[0], parts[1], parts[2]
	} else if len(parts) == 2 {
		return parts[0], "", parts[1]
	}
	return id, "", ""
}

func findParentClassID(methodID string, classes map[string]*types.LayoutNode) string {
	path, rec, _ := parseFQN(methodID)
	if rec == "" {
		return ""
	}

	classID := fmt.Sprintf("%s::%s", path, rec)
	if _, exists := classes[classID]; exists {
		return classID
	}
	return ""
}

func resolveNodeToClass(nodeID string, classes map[string]*types.LayoutNode) []string {
	if _, exists := classes[nodeID]; exists {
		return []string{nodeID}
	}

	path, rec, _ := parseFQN(nodeID)
	if rec != "" {
		cleanPath := cleanPathFromID(path)
		for classID := range classes {
			cleanClassPath := cleanPathFromID(classID)
			_, _, classRec := parseFQN(classID)
			if cleanClassPath == cleanPath && classRec == rec {
				return []string{classID}
			}
		}
	}

	cleanFile := cleanPathFromID(nodeID)
	if cleanFile == "" {
		return nil
	}

	var resolved []string
	for classID := range classes {
		cleanClassFile := cleanPathFromID(classID)
		if cleanClassFile == cleanFile {
			resolved = append(resolved, classID)
		}
	}
	return resolved
}

func cleanPathFromID(id string) string {
	res := id
	res = strings.TrimPrefix(res, "http://glassmarble.org/node/")
	res = strings.TrimPrefix(res, "http://glassmarble.org/file/")
	res = strings.TrimPrefix(res, "file:")
	res = strings.TrimPrefix(res, "module:")

	if idx := strings.Index(res, "::"); idx != -1 {
		res = res[:idx]
	}
	return strings.TrimSpace(res)
}

func getShortKind(kind string) string {
	return strings.TrimPrefix(kind, "gm:")
}

func sanitizeMermaidLabel(s string) string {
	s = strings.ReplaceAll(s, "%20", " ")
	s = strings.ReplaceAll(s, "%3A", ":")
	s = strings.ReplaceAll(s, "%2F", "/")
	s = strings.ReplaceAll(s, "%5C", "\\")
	s = strings.ReplaceAll(s, "%", "")
	s = strings.ReplaceAll(s, "\"", "'")
	s = strings.ReplaceAll(s, "()", "")
	s = strings.ReplaceAll(s, "<", "~")
	s = strings.ReplaceAll(s, ">", "~")
	s = strings.ReplaceAll(s, "[", "(")
	s = strings.ReplaceAll(s, "]", ")")
	if len(s) > 60 {
		s = s[:57] + "..."
	}
	return s
}

func shortPredicate(pred string) string {
	return strings.TrimPrefix(pred, "gm:")
}

func collectAllNodes(tree *types.LayoutTree) []*types.LayoutNode {
	var nodes []*types.LayoutNode
	var walk func(t *types.LayoutTree)
	walk = func(t *types.LayoutTree) {
		if t == nil {
			return
		}
		nodes = append(nodes, t.Nodes...)
		for _, child := range t.Children {
			walk(child)
		}
	}
	walk(tree)
	return nodes
}

func collectNodesByKind(tree *types.LayoutTree, kind string) []*types.LayoutNode {
	var result []*types.LayoutNode
	for _, n := range collectAllNodes(tree) {
		if n.Kind == kind {
			result = append(result, n)
		}
	}
	return result
}

func collectNodesByPrimitive(tree *types.LayoutTree, prim string) []*types.LayoutNode {
	var result []*types.LayoutNode
	for _, n := range collectAllNodes(tree) {
		if strings.Contains(n.PrimitiveType, prim) {
			result = append(result, n)
		}
	}
	return result
}

func isDatabase(node *types.LayoutNode) bool {
	return node.Kind == "gm:Database" || strings.Contains(node.PrimitiveType, "DATABASE")
}

func isExternalSystem(node *types.LayoutNode) bool {
	return node.Kind == "gm:ExternalSystem" || strings.Contains(node.PrimitiveType, "NETWORK_IO")
}

func isSystemBoundary(boundary *types.LayoutTree) bool {
	if boundary == nil {
		return false
	}
	for _, node := range boundary.Nodes {
		if node.Kind == "gm:Namespace" || node.Kind == "gm:Module" || node.Kind == "gm:File" {
			return true
		}
	}
	return false
}

func detectContainerTechnology(boundary *types.LayoutTree) string {
	for _, node := range boundary.Nodes {
		if node.PrimitiveType != "" {
			return node.PrimitiveType
		}
		tech := detectNodeTechnology(node)
		if tech != "Go Module" {
			return tech
		}
	}
	return "Go Module"
}

func getContainerDescription(boundary *types.LayoutTree) string {
	nodeCount := len(boundary.Nodes)
	if nodeCount == 0 {
		return "Container"
	}
	hasDB := false
	for _, node := range boundary.Nodes {
		if isDatabase(node) {
			hasDB = true
			break
		}
	}
	if hasDB {
		return fmt.Sprintf("Data Container (%d nodes)", nodeCount)
	}
	return fmt.Sprintf("Application Container (%d nodes)", nodeCount)
}

func getNodeDescription(node *types.LayoutNode) string {
	if isDatabase(node) {
		return "Data Store"
	}
	if isExternalSystem(node) {
		return "External Integration"
	}
	return fmt.Sprintf("%s Component", node.Kind)
}

func detectNodeTechnology(node *types.LayoutNode) string {
	if node.PrimitiveType != "" {
		return node.PrimitiveType
	}
	if isDatabase(node) {
		return "Database"
	}
	if isExternalSystem(node) {
		return "Network"
	}
	switch node.Kind {
	case "gm:Executable", "gm:Function", "gm:Method":
		return "Go/Executable"
	case "gm:TypeDecl":
		return "Go/Type"
	case "gm:Namespace", "gm:Module":
		return "Go/Module"
	default:
		return "Go/Generic"
	}
}

func getDiagramTitle(tree *types.LayoutTree, fallback string) string {
	if tree != nil && tree.BoundaryName != "Root" && tree.BoundaryName != "" {
		return sanitizeMermaidLabel(tree.BoundaryName) + " " + fallback
	}
	return fallback
}

func renderSummaryFooter(tree *types.LayoutTree, sb *strings.Builder) {
	if tree == nil || tree.Summary == nil {
		return
	}
	s := tree.Summary
	sb.WriteString(fmt.Sprintf("    %% Graph Summary: %d nodes, %d edges, density=%.4f, diameter=%d, avg_path=%.2f, clusters=%d, largest_scc=%d, god_objects=%d\n",
		s.NodeCount, s.EdgeCount, s.Density, s.Diameter,
		s.AvgPathLength, s.ClusterCount, s.LargestSCCSize, s.GodObjectCount))
}

func renderPlantUMLSummaryFooter(tree *types.LayoutTree, sb *strings.Builder) {
	if tree == nil || tree.Summary == nil {
		return
	}
	s := tree.Summary
	sb.WriteString(fmt.Sprintf("' Graph Summary: %d nodes, %d edges, density=%.4f, diameter=%d, avg_path=%.2f, clusters=%d, largest_scc=%d, god_objects=%d\n",
		s.NodeCount, s.EdgeCount, s.Density, s.Diameter,
		s.AvgPathLength, s.ClusterCount, s.LargestSCCSize, s.GodObjectCount))
}

func renderDOTSummaryFooter(tree *types.LayoutTree, sb *strings.Builder) {
	if tree == nil || tree.Summary == nil {
		return
	}
	s := tree.Summary
	sb.WriteString(fmt.Sprintf("    // Graph Summary: %d nodes, %d edges, density=%.4f, diameter=%d, avg_path=%.2f, clusters=%d, largest_scc=%d, god_objects=%d\n",
		s.NodeCount, s.EdgeCount, s.Density, s.Diameter,
		s.AvgPathLength, s.ClusterCount, s.LargestSCCSize, s.GodObjectCount))
}

func renderC4Edges(tree *types.LayoutTree, sb *strings.Builder) {
	drawn := make(map[string]bool)
	for _, edge := range tree.Edges {
		src := sanitizeName(edge.SourceID)
		tgt := sanitizeName(edge.TargetID)
		key := src + "->" + tgt
		if drawn[key] {
			continue
		}
		drawn[key] = true
		label := sanitizeMermaidLabel(shortPredicate(edge.Predicate))
		sb.WriteString(fmt.Sprintf("    Rel(%s, %s, \"%s\")\n", src, tgt, label))
	}
}
