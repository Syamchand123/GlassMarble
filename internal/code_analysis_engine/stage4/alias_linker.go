package stage4

import (
	"fmt"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage2"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage3"
)

// LinkAliasAnalysis performs Andersen's Points-To subset-based Alias tracking.
// HEAP_ALLOCATION nodes are produced by a raw-text heuristic; they only run in
// full mode (AUDIT Issue 1.4 / Phase 1B-5).
func LinkAliasAnalysis(stage3Out *stage3.Stage3Output, cpg *Stage4Output) {
	if stage3Out == nil || stage3Out.RootNode == nil || cpg == nil {
		return
	}
	if !isFullMode(cpg) {
		return
	}
	traverseForAliases(stage3Out, stage3Out.RootNode, cpg)
}

func traverseForAliases(stage3Out *stage3.Stage3Output, dir *stage3.DirectoryNode, cpg *Stage4Output) {
	if dir == nil {
		return
	}
	for _, file := range dir.Files {
		if file == nil || file.GASTRoot == nil {
			continue
		}
		if len(cpg.ModifiedFiles) > 0 && !cpg.ModifiedFiles[stage3.NormalizeRelativePath(file.RelativePath)] {
			continue
		}
		extractAliasesFromGAST(stage3Out, file.GASTRoot, file.RelativePath, "", cpg)
	}
	for _, subDir := range dir.SubFolders {
		traverseForAliases(stage3Out, subDir, cpg)
	}
}

func extractAliasesFromGAST(stage3Out *stage3.Stage3Output, node *stage2.GASTNode, relPath, currentFuncID string, cpg *Stage4Output) {
	if node == nil {
		return
	}
	funcID := currentFuncID
	if node.Type == stage2.GASTFunction {
		funcID = universalFuncID(relPath, node.ReceiverType, node.Name)
	}

	if funcID != "" && node.Type == stage2.GASTVariable {
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
				var globalIdx map[string][]*stage2.GASTNode
				if stage3Out != nil {
					globalIdx = stage3Out.GlobalDefinitionIndex
				}
				targetTypeFQN := resolveTypeToFQN(strings.TrimPrefix(node.DataType, "*"), relPath, globalIdx, cpg)
				if targetTypeFQN != "" {
					cpg.AddEdge(allocID, targetTypeFQN, EdgeHeapAlias, int(node.StartLine))
				}
			}
		}
	}

	for _, child := range node.Children {
		extractAliasesFromGAST(stage3Out, child, relPath, funcID, cpg)
	}
}
