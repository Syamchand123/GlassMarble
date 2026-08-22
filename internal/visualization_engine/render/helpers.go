package aggregate

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/product/ids"
	"github.com/Syamchand123/GlassMarble/internal/product/ont"
	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/types"
)

func sanitizeName(name string) string {
	var sb strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			sb.WriteRune(r)
		} else {
			sb.WriteRune('_')
		}
	}
	res := sb.String()
	for strings.Contains(res, "__") {
		res = strings.ReplaceAll(res, "__", "_")
	}
	res = strings.Trim(res, "_")
	if res == "" {
		res = "node"
	}
	if res[0] >= '0' && res[0] <= '9' {
		res = "n_" + res
	}
	return res
}

// aliasRegistry assigns unique, sanitized aliases to graph IDs. sanitizeName
// alone is not injective (AUDIT Issue 2 Phase 2C-11): IDs like
// "user-service.go::X" and "user_service.go::X" both map to
// "user_service_go_X", which would collide in Mermaid/PlantUML/DOT.
// Collisions get numeric suffixes. Registration order is deterministic
// because renderers walk the layout tree, whose nodes and children are sorted.
type aliasRegistry struct {
	used     map[string]string
	count    map[string]int
	byID     map[string]string
	declared map[string]bool
	// boundaryNodeAlias maps each layout node to the alias of the boundary
	// block that actually contains it, so edge fallback resolution is
	// position-accurate even when two subtrees share a boundary name
	// (GAP-C4-03).
	boundaryNodeAlias map[string]string
}

func newAliasRegistry() *aliasRegistry {
	return &aliasRegistry{
		used:              make(map[string]string),
		count:             make(map[string]int),
		byID:              make(map[string]string),
		declared:          make(map[string]bool),
		boundaryNodeAlias: make(map[string]string),
	}
}

func (r *aliasRegistry) markDeclared(alias string) {
	if r.declared == nil {
		r.declared = make(map[string]bool)
	}
	r.declared[alias] = true
}

func (r *aliasRegistry) isDeclared(alias string) bool {
	if r.declared == nil {
		return false
	}
	return r.declared[alias]
}

// bindBoundary records that nodeID lives inside the boundary block emitted
// with the given alias, so renderC4Edges can fall back to a declared alias
// for aggregated relationships (GAP-C4-03).
func (r *aliasRegistry) bindBoundary(nodeID, alias string) {
	if r.boundaryNodeAlias == nil {
		r.boundaryNodeAlias = make(map[string]string)
	}
	r.boundaryNodeAlias[nodeID] = alias
}

func (r *aliasRegistry) boundaryAliasOf(nodeID string) string {
	if r.boundaryNodeAlias == nil {
		return ""
	}
	return r.boundaryNodeAlias[nodeID]
}

// uniqueAlias returns base if it is not yet declared, otherwise base with a
// numeric suffix (base_2, base_3, ...). Used when a boundary name appears at
// more than one tree position (e.g. file and package scope subtrees) so
// renderers never emit duplicate declarations.
func (r *aliasRegistry) uniqueAlias(base string) string {
	if !r.isDeclared(base) {
		return base
	}
	n := 2
	for r.isDeclared(fmt.Sprintf("%s_%d", base, n)) {
		n++
	}
	return fmt.Sprintf("%s_%d", base, n)
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
	s = strings.ReplaceAll(s, "%smell detection", "\\")
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
	if boundary.BoundaryName != "Root" && boundary.BoundaryName != "" {
		return len(boundary.Nodes) > 0 || len(boundary.Children) > 0
	}
	for _, node := range boundary.Nodes {
		if node.Kind == ont.PredNamespace || node.Kind == ont.PredModule || node.Kind == ont.PredFile || node.Kind == ont.PredPackage {
			return true
		}
	}
	return false
}

func detectContainerTechnology(boundary *types.LayoutTree) string {
	for _, node := range boundary.Nodes {
		if node.PrimitiveType != "" && node.PrimitiveType != ont.PrefixGM {
			return strings.TrimPrefix(node.PrimitiveType, ont.PrefixGM)
		}
		tech := detectNodeTechnology(node)
		if tech != "Go Module" && tech != "Go/Generic" {
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
	if node == nil {
		return "Component"
	}
	if isDatabase(node) {
		return "Data Store"
	}
	if isExternalSystem(node) {
		return "External Integration"
	}
	kind := getShortKind(node.Kind)
	if kind == "" {
		kind = "Go"
	}
	return fmt.Sprintf("%s Component", kind)
}

func detectNodeTechnology(node *types.LayoutNode) string {
	if node == nil {
		return "Go/Generic"
	}
	if node.PrimitiveType != "" && node.PrimitiveType != ont.PrefixGM {
		return strings.TrimPrefix(node.PrimitiveType, ont.PrefixGM)
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
	case ont.PredTypeDecl, ont.PredStruct, ont.PredClass:
		return "Go/Type"
	case ont.PredInterface:
		return "Go/Interface"
	case ont.PredNamespace, ont.PredModule, ont.PredPackage:
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
	sb.WriteString(fmt.Sprintf("    %%%% Graph Summary: %d nodes, %d edges, density=%.4f, diameter=%d, avg_path=%.2f, clusters=%d, largest_scc=%d, god_objects=%d, components=%d\n",
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

	// Resolve undeclared endpoints to the alias of the boundary block that
	// actually contains them (recorded position-accurately by renderC4Blocks),
	// so aggregated relationships (cmd -> internal -> tests) survive and
	// always reference an alias that exists in the markup (GAP-C4-02/03).
	boundaryOf := make(map[string]string)
	if len(tree.Children) > 0 {
		for _, child := range tree.Children {
			var mark func(t *types.LayoutTree)
			mark = func(t *types.LayoutTree) {
				for _, n := range t.Nodes {
					boundaryOf[n.ID] = child.BoundaryName
				}
				for _, c := range t.Children {
					mark(c)
				}
			}
			mark(child)
		}
	}

	for _, edge := range tree.Edges {
		src := reg.alias(edge.SourceID)
		tgt := reg.alias(edge.TargetID)
		if !reg.isDeclared(src) {
			if bound := reg.boundaryAliasOf(edge.SourceID); bound != "" && reg.isDeclared(bound) {
				src = bound
			} else if name, ok := boundaryOf[edge.SourceID]; ok {
				if base := reg.boundary(name); reg.isDeclared(base) {
					src = base
				}
			}
		}
		if !reg.isDeclared(tgt) {
			if bound := reg.boundaryAliasOf(edge.TargetID); bound != "" && reg.isDeclared(bound) {
				tgt = bound
			} else if name, ok := boundaryOf[edge.TargetID]; ok {
				if base := reg.boundary(name); reg.isDeclared(base) {
					tgt = base
				}
			}
		}
		if !reg.isDeclared(src) || !reg.isDeclared(tgt) {
			continue
		}
		if src == tgt {
			continue
		}
		key := src + "->" + tgt
		if drawn[key] {
			continue
		}
		drawn[key] = true
		label := sanitizeMermaidLabel(shortPredicate(edge.Predicate))
		sb.WriteString(fmt.Sprintf("    Rel(%s, %s, \"%s\")\n", src, tgt, label))
	}
}
