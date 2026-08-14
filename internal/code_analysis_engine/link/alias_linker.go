package link

import (
	"fmt"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/normalize"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/aggregate"
)

// LinkAliasAnalysis performs Andersen's Points-To subset-based Alias tracking.
// HEAP_ALLOCATION nodes are produced by a raw-text heuristic; they only run in
// full mode (AUDIT Issue 1.4 / Phase 1B-5).
func LinkAliasAnalysis(aggregateOut *aggregate.AggregateOutput, cpg *LinkOutput) {
	if aggregateOut == nil || aggregateOut.RootNode == nil || cpg == nil {
		return
	}
	if !isFullMode(cpg) {
		return
	}
	traverseForAliases(aggregateOut, aggregateOut.RootNode, cpg)
}

func traverseForAliases(aggregateOut *aggregate.AggregateOutput, dir *aggregate.DirectoryNode, cpg *LinkOutput) {
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
		extractAliasesFromGAST(aggregateOut, file.GASTRoot, file.RelativePath, "", cpg)
	}
	for _, subDir := range dir.SubFolders {
		traverseForAliases(aggregateOut, subDir, cpg)
	}
}

func extractAliasesFromGAST(aggregateOut *aggregate.AggregateOutput, node *normalize.GASTNode, relPath, currentFuncID string, cpg *LinkOutput) {
	if node == nil {
		return
	}
	funcID := currentFuncID
	if node.Type == normalize.GASTFunction {
		funcID = universalFuncID(relPath, node.ReceiverType, node.Name)
	}

	if funcID != "" && node.Type == normalize.GASTVariable {
		// New Heap Allocation tracking via `new` or `&` or `make`
		content := node.Properties["content"]
		if strings.Contains(content, "new(") || strings.Contains(content, "make(") || strings.Contains(content, "&") {
			allocID := fmt.Sprintf("alloc::%s::%s", funcID, node.Name)
			varID := fmt.Sprintf("%s::VAR_%s", funcID, node.Name)

			if _, exists := cpg.GetNode(allocID); !exists {
				cpg.GraphNodes[allocID] = &ResolvedNode{
					ID:   allocID,
					Kind: "HEAP_ALLOCATION",
					Name: "heap_alloc",
				}
			}

			// Andersen's Inclusion Edge (p ⊇ {alloc})
			cpg.AddEdge(varID, allocID, EdgePointsTo, int(node.StartLine))

			// Field access alias (e.g., p.Name = "x")
			// Pass the globalIndex for correct struct type resolution
			if node.DataType != "" {
				var globalIdx map[string][]*normalize.GASTNode
				if aggregateOut != nil {
					globalIdx = aggregateOut.GlobalDefinitionIndex
				}
				targetTypeFQN := resolveTypeToFQN(strings.TrimPrefix(node.DataType, "*"), relPath, globalIdx, cpg)
				if targetTypeFQN != "" {
					cpg.AddEdge(allocID, targetTypeFQN, EdgeHeapAlias, int(node.StartLine))
				}
			}
		}
	}

	for _, child := range node.Children {
		extractAliasesFromGAST(aggregateOut, child, relPath, funcID, cpg)
	}
}
