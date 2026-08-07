package stage3

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/product/ids"
	"github.com/Syamchand123/GlassMarble/internal/product/ont"
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

// aliasRegistry assigns unique, sanitized aliases to graph IDs. sanitizeName
// alone is not injective (AUDIT Issue 2 Phase 2C-11): IDs like
// "user-service.go::X" and "user_service.go::X" both map to
// "user_service_go_X", which would collide in Mermaid/PlantUML/DOT.
// Collisions get numeric suffixes. Registration order is deterministic
// because renderers walk the layout tree, whose nodes and children are sorted.
type aliasRegistry struct {
	used  map[string]string
	count map[string]int
	byID  map[string]string
}

func newAliasRegistry() *aliasRegistry {
	return &aliasRegistry{used: make(map[string]string), count: make(map[string]int), byID: make(map[string]string)}
}

func (r *aliasRegistry) alias(id string) string {
	if a, ok := r.byID[id]; ok {
		return a
	}
	base := sanitizeName(id)
	if base == "" {
		base = "node"
	}
	n := r.count[base]
	for {
		candidate := base
		if n > 0 {
			candidate = fmt.Sprintf("%s_%d", base, n)
		}
		if _, taken := r.used[candidate]; !taken {
			r.used[candidate] = id
			r.byID[id] = candidate
			r.count[base] = n + 1
			return candidate
		}
		n++
	}
}

// boundary registers a boundary (subgraph) alias in a namespace separate from
// node aliases so a boundary and a node can never collide.
func (r *aliasRegistry) boundary(name string) string {
	return r.alias("sb_" + sanitizeName(name))
}

// registerTreeAliases pre-registers deterministic aliases for every node and
// boundary in the tree, walking it in the same order the renderers do.
func registerTreeAliases(tree *types.LayoutTree, reg *aliasRegistry) {
	if tree == nil {
		return
	}
	if tree.BoundaryName != "Root" && tree.BoundaryName != "" {
		reg.boundary(tree.BoundaryName)
	}
	for _, n := range tree.Nodes {
		reg.alias(n.ID)
	}
	for _, child := range tree.Children {
		registerTreeAliases(child, reg)
	}
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

// parseFQN splits a node ID into (path, receiver, symbol). Both the legacy
// `path::symbol` dialect and the canonical scheme (type:path:owner:symbol,
// master_overhaul_plan.md §4.1) are accepted: legacy IDs are normalized onto
// the canonical grammar first (ids.NormalizeLegacyID is idempotent for
// canonical IDs) and then parsed, so URL-encoded path segments are decoded
// (GAP-C-01 / GAP-C-04). IDs that match neither grammar fall back to the
// legacy split so plain fixture IDs keep working.
func parseFQN(id string) (path, receiver, symbol string) {
	norm := ids.NormalizeLegacyID(id)
	if c, err := ids.ParseCanonicalID(norm); err == nil {
		return c.Path, c.Owner, c.Symbol
	}
	parts := strings.Split(id, "::")
	switch len(parts) {
	case 3:
		return parts[0], parts[1], parts[2]
	case 2:
		return parts[0], "", parts[1]
	}
	return id, "", ""
}

// classIDCandidates returns the plausible class-level IDs for a member whose
// parsed path/receiver are known, covering both the legacy `path::Receiver`
// dialect and the canonical `type:path:Receiver` / `method:path:Receiver`
// forms. Lookups try every candidate so resolution works regardless of which
// ID grammar the current graph uses (GAP-C-01).
func classIDCandidates(path, rec string) []string {
	return []string{
		fmt.Sprintf("%s::%s", path, rec),
		ids.CanonicalID{Kind: ids.KindType, Path: path, Symbol: rec}.String(),
		ids.CanonicalID{Kind: ids.KindMethod, Path: path, Symbol: rec}.String(),
	}
}

func findParentClassID(methodID string, classes map[string]*types.LayoutNode) string {
	path, rec, _ := parseFQN(methodID)
	if rec == "" {
		return ""
	}

	for _, candidate := range classIDCandidates(path, rec) {
		if _, exists := classes[candidate]; exists {
			return candidate
		}
	}
	return ""
}

// classIndex maps a normalized file path to the class IDs declared in it.
// resolveNodeToClass uses it for O(1) member→class resolution instead of a
// linear scan over the whole classes map (GAP-M-05).
type classIndex struct {
	byPath map[string][]string
}

// buildClassIndex pre-indexes the classes map by parsed file path.
func buildClassIndex(classes map[string]*types.LayoutNode) *classIndex {
	idx := &classIndex{byPath: make(map[string][]string, len(classes))}
	for id := range classes {
		path, _, _ := parseFQN(id)
		if path == "" {
			continue
		}
		idx.byPath[path] = append(idx.byPath[path], id)
	}
	for p := range idx.byPath {
		sort.Strings(idx.byPath[p])
	}
	return idx
}

// lookup returns the class IDs in path whose symbol (or owner, in dialects
// that carry one) equals rec. Both members (`rec == class symbol`) and
// owners (`rec == class owner`) are matched so legacy and canonical graphs
// resolve identically.
func (idx *classIndex) lookup(path, rec string) []string {
	var ids []string
	for _, classID := range idx.byPath[path] {
		cPath, cRec, cSym := parseFQN(classID)
		if cPath != path {
			continue
		}
		if rec == cSym || (cRec != "" && rec == cRec) {
			ids = append(ids, classID)
		}
	}
	return ids
}

func resolveNodeToClass(nodeID string, classes map[string]*types.LayoutNode, idx *classIndex) []string {
	if _, exists := classes[nodeID]; exists {
		return []string{nodeID}
	}

	path, rec, _ := parseFQN(nodeID)
	if rec != "" && idx != nil {
		if ids := idx.lookup(path, rec); len(ids) > 0 {
			return ids
		}
	}

	// Single-type-file fallback: a free function (Go idiom: no receiver) is
	// mapped to the class in its file only when that file declares exactly
	// one type. This never fans one function edge out across sibling classes
	// (AUDIT Issue 2 Phase 2C-12); files with several types stay unmapped.
	if path != "" && idx != nil {
		if ids := idx.byPath[path]; len(ids) == 1 {
			return []string{ids[0]}
		}
	}
	return nil
}

func cleanPathFromID(id string) string {
	res := id
	res = strings.TrimPrefix(res, "http://glassmarble.org/node/")
	res = strings.TrimPrefix(res, "http://glassmarble.org/file/")
	res = strings.TrimPrefix(res, "file:")
	res = strings.TrimPrefix(res, "module:")

	// Canonical IDs URL-encode path separators (file%2Fsrc%2Fmain.go), so the
	// path must be extracted and percent-decoded through parseFQN rather than
	// string-sliced (GAP-C-04).
	path, _, _ := parseFQN(res)
	if path != "" {
		return path
	}
	return strings.TrimSpace(res)
}

func getShortKind(kind string) string {
	return strings.TrimPrefix(kind, ont.PrefixGM)
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
	// Newlines and carriage returns break every renderer (AUDIT Issue 2
	// Phase 2C-11); collapse them to spaces.
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	// Truncate on rune boundaries, never mid-rune.
	runes := []rune(s)
	if len(runes) > 60 {
		return string(runes[:57]) + "..."
	}
	return s
}

func shortPredicate(pred string) string {
	return strings.TrimPrefix(pred, ont.PrefixGM)
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
	return node.Kind == ont.PredDatabase || node.Kind == ont.PredVirtualDatabase || strings.Contains(node.PrimitiveType, "DATABASE")
}

func isExternalSystem(node *types.LayoutNode) bool {
	switch node.Kind {
	case ont.PredExternalSystem, ont.PredExternalSDK, ont.PredExternalAPI, ont.PredExternalFFI, ont.PredExternal:
		return true
	}
	return strings.Contains(node.PrimitiveType, "NETWORK_IO")
}

// collectExternalNodes returns all nodes whose kind represents an external
// system (the classes the serializer can actually emit).
func collectExternalNodes(tree *types.LayoutTree) []*types.LayoutNode {
	var result []*types.LayoutNode
	for _, n := range collectAllNodes(tree) {
		switch n.Kind {
		case ont.PredExternalSystem, ont.PredExternalSDK, ont.PredExternalAPI, ont.PredExternalFFI, ont.PredExternal:
			result = append(result, n)
		}
	}
	return result
}

func isSystemBoundary(boundary *types.LayoutTree) bool {
	if boundary == nil {
		return false
	}
	for _, node := range boundary.Nodes {
		if node.Kind == ont.PredNamespace || node.Kind == ont.PredModule || node.Kind == ont.PredFile {
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
	case ont.PredExecutable, ont.PredFunction, ont.PredMethod:
		return "Go/Executable"
	case ont.PredTypeDecl:
		return "Go/Type"
	case ont.PredNamespace, ont.PredModule:
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
	sb.WriteString(fmt.Sprintf("    %% Graph Summary: %d nodes, %d edges, density=%.4f, diameter=%d, avg_path=%.2f, clusters=%d, largest_scc=%d, god_objects=%d, components=%d\n",
		s.NodeCount, s.EdgeCount, s.Density, s.Diameter,
		s.AvgPathLength, s.ClusterCount, s.LargestSCCSize, s.GodObjectCount, s.ConnectedComponents))
}

func renderPlantUMLSummaryFooter(tree *types.LayoutTree, sb *strings.Builder) {
	if tree == nil || tree.Summary == nil {
		return
	}
	s := tree.Summary
	sb.WriteString(fmt.Sprintf("' Graph Summary: %d nodes, %d edges, density=%.4f, diameter=%d, avg_path=%.2f, clusters=%d, largest_scc=%d, god_objects=%d, components=%d\n",
		s.NodeCount, s.EdgeCount, s.Density, s.Diameter,
		s.AvgPathLength, s.ClusterCount, s.LargestSCCSize, s.GodObjectCount, s.ConnectedComponents))
}

func renderDOTSummaryFooter(tree *types.LayoutTree, sb *strings.Builder) {
	if tree == nil || tree.Summary == nil {
		return
	}
	s := tree.Summary
	sb.WriteString(fmt.Sprintf("    // Graph Summary: %d nodes, %d edges, density=%.4f, diameter=%d, avg_path=%.2f, clusters=%d, largest_scc=%d, god_objects=%d, components=%d\n",
		s.NodeCount, s.EdgeCount, s.Density, s.Diameter,
		s.AvgPathLength, s.ClusterCount, s.LargestSCCSize, s.GodObjectCount, s.ConnectedComponents))
}

func renderC4Edges(tree *types.LayoutTree, reg *aliasRegistry, sb *strings.Builder) {
	drawn := make(map[string]bool)
	for _, edge := range tree.Edges {
		src := reg.alias(edge.SourceID)
		tgt := reg.alias(edge.TargetID)
		key := src + "->" + tgt
		if drawn[key] {
			continue
		}
		drawn[key] = true
		label := sanitizeMermaidLabel(shortPredicate(edge.Predicate))
		sb.WriteString(fmt.Sprintf("    Rel(%s, %s, \"%s\")\n", src, tgt, label))
	}
}
