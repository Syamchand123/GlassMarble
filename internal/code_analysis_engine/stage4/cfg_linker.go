package stage4

import (
	"fmt"
	"strconv"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage2"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage3"
)

// LinkIntraProceduralControlFlow constructs Control Flow Graph (CFG) branch nodes and CFG edges.
// The granularity depends on cpg.Config.LevelOfDetail:
//   - "standard": creates one CFG_SUMMARY node per function with branch type counts
//   - "full" (default): creates one node per control-flow construct
//   - "architecture": should not be called (handled by LinkerConfig pass filtering)
func LinkIntraProceduralControlFlow(stage3Out *stage3.Stage3Output, cpg *Stage4Output) {
	if stage3Out == nil || stage3Out.RootNode == nil || cpg == nil {
		return
	}

	om := stage3.BuildOwnershipMap(stage3Out.GlobalDefinitionIndex, stage3Out.WorkspaceCtx)

	// In standard mode, aggregate branch counts per function; in full mode, pass nil
	var branchCounts map[string]map[string]int
	if cpg.Config.LevelOfDetail == LevelStandard {
		branchCounts = make(map[string]map[string]int)
	}


	traverseForCFG(stage3Out.RootNode, cpg, stage3Out, om, branchCounts)

	// After traversal, create CFG_SUMMARY nodes for standard mode
	if branchCounts != nil {
		buildCFGSummaryNodes(branchCounts, cpg)
	}
}

func traverseForCFG(dir *stage3.DirectoryNode, cpg *Stage4Output, stage3Out *stage3.Stage3Output, om *stage3.OwnershipMap, branchCounts map[string]map[string]int) {
	if dir == nil {
		return
	}

	fileNodeCount := make(map[string]int)

	for _, file := range dir.Files {
		if file == nil || file.GASTRoot == nil {
			continue
		}
		if len(cpg.ModifiedFiles) > 0 && !cpg.ModifiedFiles[stage3.NormalizeRelativePath(file.RelativePath)] {
			continue
		}

		extractCFGNodesFromGAST(file.GASTRoot, file.RelativePath, "", cpg, stage3Out, om, branchCounts, fileNodeCount)
	}

	for _, subDir := range dir.SubFolders {
		traverseForCFG(subDir, cpg, stage3Out, om, branchCounts)
	}
}

func extractCFGNodesFromGAST(node *stage2.GASTNode, relPath, currentEnclosingFuncID string, cpg *Stage4Output, stage3Out *stage3.Stage3Output, om *stage3.OwnershipMap, branchCounts map[string]map[string]int, fileNodeCount map[string]int) {
	if node == nil {
		return
	}

	enclosingFuncID := currentEnclosingFuncID

	if node.Type == stage2.GASTFunction {
		receiver := node.ReceiverType
		enclosingFuncID = BuildUniversalID(relPath, receiver, node.Name)
		// Initialize counters for this function in standard mode
		if branchCounts != nil && enclosingFuncID != "" {
			if _, ok := branchCounts[enclosingFuncID]; !ok {
				branchCounts[enclosingFuncID] = make(map[string]int)
			}
		}
	}

	if enclosingFuncID != "" && node.Type == stage2.GASTControlFlow {
		if branchCounts != nil {
			// Standard mode: aggregate branch count
			_, branchKind := classifyControlFlowKind(node.Kind)
			if branchKind != "" {
				branchCounts[enclosingFuncID][branchKind]++
			}
		} else {
			// Full mode: create per-branch nodes and edges
			edgeType, branchKind := classifyControlFlowKind(node.Kind)
			if branchKind != "" {
				branchID := fmt.Sprintf("%s::%s_L%d", enclosingFuncID, branchKind, node.StartLine)

				// Apply per-file node budget (MaxNodesPerFile)
				fileNodeCount[relPath]++
				if cpg.Config.MaxNodesPerFile > 0 && fileNodeCount[relPath] > cpg.Config.MaxNodesPerFile {
					goto skipCFGNode
				}

				cpg.GraphNodes[branchID] = &ResolvedNode{
					ID:   branchID,
					Kind: branchKind,
					Name: branchKind,
					FileSpec: LocationMeta{
						Path:      relPath,
						LineStart: int(node.StartLine),
						LineEnd:   int(node.EndLine),
					},
				}

				cpg.AddEdge(enclosingFuncID, branchID, edgeType, int(node.StartLine))

				for _, child := range node.Children {
					if child.Type == stage2.GASTCallExpression {
						targetCallFQN, _ := resolveCallTarget(child.ReceiverType, child.Name, relPath, nil, om, cpg, stage3Out)
						if targetCallFQN != "" {
							cpg.AddEdge(branchID, targetCallFQN, EdgeControlFlow, int(child.StartLine))
						}
					}
				}
			}
		}
	}

skipCFGNode:
	for _, child := range node.Children {
		extractCFGNodesFromGAST(child, relPath, enclosingFuncID, cpg, stage3Out, om, branchCounts, fileNodeCount)
	}
}

// buildCFGSummaryNodes creates one CFG_SUMMARY node per function with branch type counts.
func buildCFGSummaryNodes(branchCounts map[string]map[string]int, cpg *Stage4Output) {
	for funcID, counts := range branchCounts {
		if len(counts) == 0 {
			continue
		}

		summaryID := funcID + "::CFG_SUMMARY"
		if cpg.NodeExists(summaryID) {
			continue
		}

		props := make(map[string]string)
		for kind, count := range counts {
			props[kind] = strconv.Itoa(count)
		}

		cpg.GraphNodes[summaryID] = &ResolvedNode{
			ID:         summaryID,
			Kind:       "CFG_SUMMARY",
			Name:       "Control Flow Summary",
			Properties: props,
		}

		cpg.AddEdge(funcID, summaryID, EdgeControlFlow, 0)
	}
}

// classifyControlFlowKind maps a GAST node's Kind string to a CFG edge type and branch node kind.
// Called only for nodes with Type == GASTControlFlow, so all entries are legitimate control-flow
// tree-sitter kind strings set by stage2's normalizer.
func classifyControlFlowKind(kind string) (RelationshipType, string) {
	switch kind {
	case "if_statement", "if", "else_clause", "conditional":
		return EdgeConditionalBranch, "IF_BRANCH"
	case "for_statement", "for", "for_each", "foreach",
		"for_in_statement", "for_of_statement",
		"while_statement", "while", "do_statement", "do", "loop":
		return EdgeLoopBranch, "LOOP_BRANCH"
	case "switch_statement", "switch", "switch_case", "case":
		return EdgeSwitchBranch, "SWITCH_BRANCH"
	case "try_statement", "try", "catch_clause", "catch", "finally_clause", "finally":
		return EdgeCatches, "EXCEPTIONAL_BRANCH"
	case "defer", "defer_statement":
		return EdgeDefers, "EXCEPTIONAL_BRANCH"
	case "return_statement", "return",
		"go_statement", "go",
		"panic", "recover":
		return EdgeControlFlow, "CFG_FLOW"
	case "throw_statement", "throw", "raise":
		return EdgeThrows, "EXCEPTIONAL_BRANCH"
	default:
		return "", ""
	}
}
