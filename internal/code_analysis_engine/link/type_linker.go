package link

import (
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/normalize"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/aggregate"
	"github.com/Syamchand123/GlassMarble/internal/product/ont"
)

// LinkTypesAndComposition resolves cross-file type references and field compositions.
func LinkTypesAndComposition(aggregateOut *aggregate.AggregateOutput, cpg *LinkOutput) {
	if aggregateOut == nil || aggregateOut.RootNode == nil || cpg == nil {
		return
	}

	traverseForTypeLinking(aggregateOut.RootNode, aggregateOut.GlobalDefinitionIndex, cpg)

	// v2 (W1-12): transitive hierarchy closure — extends (direct EXTENDS)
	// and inheritsFrom (direct IMPLEMENTS) chains, depth-capped (A-02).
	emitTransitiveHierarchy(cpg)
}

// maxInheritanceDepth caps the transitive closure to keep wide enterprise
// hierarchies bounded (§5.4.1 W1-12, A-02).
const maxInheritanceDepth = 5

// emitTransitiveHierarchy walks the direct EXTENDS and IMPLEMENTS edges and
// emits depth-capped transitive closures under the same predicates
// (gm:extends / gm:inheritsFrom, W1-12/A-02). Only ancestors at depth ≥ 2
// are re-emitted (direct edges already exist). Cycles are bounded by the
// per-root visited set.
func emitTransitiveHierarchy(cpg *LinkOutput) {
	if cpg == nil {
		return
	}

	extParents := make(map[string][]string)
	implParents := make(map[string][]string)
	for src, edges := range cpg.OutboundEdges {
		for _, e := range edges {
			switch e.Type {
			case EdgeExtends:
				extParents[src] = append(extParents[src], e.TargetID)
			case EdgeImplements:
				implParents[src] = append(implParents[src], e.TargetID)
			}
		}
	}

	// Only type nodes participate (skip synthetic/virtual nodes).
	isType := func(id string) bool {
		n, ok := cpg.GetNode(id)
		return ok && (n.Kind == "STRUCT" || n.Kind == "CLASS" || n.Kind == "INTERFACE")
	}

	// ancestors returns the transitive targets at depth 2..max, skipping the
	// direct parents already linked.
	ancestors := func(root string, parents map[string][]string) []string {
		visited := map[string]bool{}
		var acc []string
		prev := parents[root]
		for depth := 2; depth <= maxInheritanceDepth && len(prev) > 0; depth++ {
			var next []string
			for _, p := range prev {
				for _, ancestor := range parents[p] {
					if visited[ancestor] {
						continue
					}
					visited[ancestor] = true
					next = append(next, ancestor)
					acc = append(acc, ancestor)
				}
			}
			prev = next
		}
		return acc
	}

	for id := range cpg.GraphNodes {
		if !isType(id) {
			continue
		}
		for _, anc := range ancestors(id, extParents) {
			if isType(anc) {
				cpg.AddEdgeProperties(id, anc, EdgeExtends, 0, 0.9,
					map[string]string{ont.PredProvenance: "heuristic"})
			}
		}
		for _, anc := range ancestors(id, implParents) {
			if isType(anc) {
				cpg.AddEdgeProperties(id, anc, EdgeImplements, 0, 0.9,
					map[string]string{ont.PredProvenance: "heuristic"})
			}
		}
	}
}

func traverseForTypeLinking(dir *aggregate.DirectoryNode, globalIndex map[string][]*normalize.GASTNode, cpg *LinkOutput) {
	if dir == nil {
		return
	}

	for _, file := range dir.Files {
		if file == nil || file.GASTRoot == nil {
			continue
		}
		if len(cpg.ModifiedFiles) > 0 && !cpg.ModifiedFiles[aggregate.NormalizeRelativePath(file.RelativePath)] {
			continue
		}

		linkNodesInGAST(file.GASTRoot, file.RelativePath, globalIndex, cpg)
	}

	for _, subDir := range dir.SubFolders {
		traverseForTypeLinking(subDir, globalIndex, cpg)
	}
}

func linkNodesInGAST(node *normalize.GASTNode, relPath string, globalIndex map[string][]*normalize.GASTNode, cpg *LinkOutput) {
	if node == nil {
		return
	}

	if node.Type == normalize.GASTTypeDeclaration {
		sourceFQN := BuildUniversalID(relPath, "", node.Name)

		// v2 (W1-12): structural languages feed inheritance exclusively via
		// GASTNode.BaseTypes (member_linker emits the direct edges); the
		// legacy content-property fallback survives only for grammars
		// without a base-class role (CSS/HTML/JSON/unknown).
		if len(node.BaseTypes) == 0 {
			// Check both "extends" and "inherits" properties (Python uses inherits)
			baseClass := node.Properties["extends"]
			if baseClass == "" {
				baseClass = node.Properties["inherits"]
			}
			if baseClass != "" {
				targetFQN := resolveTypeToFQN(baseClass, relPath, globalIndex, cpg)
				if targetFQN == "" {
					// Try direct node ID lookup
					targetFQN = BuildUniversalID(relPath, "", baseClass)
				}
				if targetFQN != "" && sourceFQN != "" && sourceFQN != targetFQN {
					cpg.AddEdgeProperties(sourceFQN, targetFQN, EdgeExtends, int(node.StartLine), 0.8,
						map[string]string{ont.PredProvenance: "content-regex"})
				}
			}
		}

		// Inspect children fields and content for composition and type references
		for _, child := range node.Children {
			if child.Type == normalize.GASTField || child.Type == normalize.GASTVariable {
				// Handle Generics e.g. List<User>
				cleanType := child.DataType
				genericType := ""
				if startIdx := strings.Index(cleanType, "<"); startIdx != -1 {
					if endIdx := strings.LastIndex(cleanType, ">"); endIdx > startIdx {
						genericType = cleanType[startIdx+1 : endIdx]
						cleanType = cleanType[:startIdx]
					}
				}

				targetFQN := resolveTypeToFQN(cleanType, relPath, globalIndex, cpg)
				if targetFQN != "" && sourceFQN != "" && sourceFQN != targetFQN {
					cpg.AddEdge(sourceFQN, targetFQN, EdgeComposes, int(child.StartLine))
				}

				if genericType != "" {
					genericFQN := resolveTypeToFQN(genericType, relPath, globalIndex, cpg)
					if genericFQN != "" && sourceFQN != "" && sourceFQN != genericFQN {
						cpg.AddEdge(sourceFQN, genericFQN, EdgeInstantiates, int(child.StartLine))
					}
				}
			}
		}
	} else if node.Type == normalize.GASTFunction {
		sourceFQN := BuildUniversalID(relPath, node.ReceiverType, node.Name)

		// Link parameter and return types as REFERENCES
		if node.DataType != "" {
			targetFQN := resolveTypeToFQN(node.DataType, relPath, globalIndex, cpg)
			if targetFQN != "" && sourceFQN != "" && sourceFQN != targetFQN {
				cpg.AddEdge(sourceFQN, targetFQN, EdgeReferences, int(node.StartLine))
			}
		}
	}

	for _, child := range node.Children {
		linkNodesInGAST(child, relPath, globalIndex, cpg)
	}
}

// isPredeclaredType reports whether a type string is a Go predeclared type
// or constant. Such names can never be resolved to a user-defined node.
func isPredeclaredType(t string) bool {
	switch t {
	case "bool", "byte", "complex64", "complex128", "error",
		"float32", "float64", "int", "int8", "int16", "int32", "int64",
		"rune", "string", "uint", "uint8", "uint16", "uint32", "uint64",
		"uintptr", "any", "comparable", "true", "false", "iota", "nil":
		return true
	}
	return false
}

// resolveTypeToFQN attempts to match a raw type string (e.g. "PostgresStore", "database.PostgresStore", "*UserStore")
// to a universal signature ID in cpg.GraphNodes, GlobalDefinitionIndex, or DB.
func resolveTypeToFQN(rawType, currentFilePath string, globalIndex map[string][]*normalize.GASTNode, cpg *LinkOutput) string {
	clean := strings.TrimPrefix(rawType, "*")
	clean = strings.TrimPrefix(clean, "[]")
	clean = strings.TrimSpace(clean)
	if clean == "" {
		return ""
	}

	// Predeclared types (string, int, error, ...) never denote user-defined
	// nodes; emitting COMPOSES/REFERENCES edges to a struct that merely
	// shares a primitive name is false truth (GAP-TYP-03).
	if isPredeclaredType(clean) {
		return ""
	}

	folderPath := aggregate.NormalizeRelativePath(currentFilePath)

	// 1. Direct universal ID match in cpg.GraphNodes
	universalLocalID := BuildUniversalID(folderPath, "", clean)
	if _, ok := cpg.GetNode(universalLocalID); ok {
		return universalLocalID
	}

	// 2. Search GlobalDefinitionIndex
	if targetNodes, ok := globalIndex[clean]; ok && len(targetNodes) > 0 {
		targetNode := targetNodes[0]
		return BuildUniversalID(targetNode.Properties["file_path"], targetNode.ReceiverType, targetNode.Name)
	}

	// 3. v2 (W1-12 / A-15): exact-map type-name lookup, no linear scan.
	if id, ok := cpg.nameToNodeID()[clean]; ok {
		return id
	}

	// 4. v3 (incremental): persisted-graph type-name lookup. The delta's
	// global index only knows modified files, so types defined in
	// unmodified files (e.g. a delta of service.go referencing store.DB)
	// resolve against the linked base graph. Stored node names are the
	// bare type name ("DB"), so package-qualified refs ("store.DB") are
	// reduced to their last dot-segment. Mirrors nameToNodeID's
	// STRUCT/CLASS/INTERFACE semantics — no PARAM/FIELD false positives.
	// A full rescan links against an empty base, so this never fires there.
	if cpg.db != nil {
		bare := clean
		if i := strings.LastIndex(bare, "."); i != -1 {
			bare = bare[i+1:]
		}
		for _, kind := range []string{"STRUCT", "CLASS", "INTERFACE"} {
			for _, n := range cpg.db.GetNodesByKind(kind) {
				if n != nil && n.Name == bare {
					return n.ID
				}
			}
		}
	}

	return ""
}
