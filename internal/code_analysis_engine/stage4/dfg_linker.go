package stage4

import (
	"fmt"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage2"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage3"
)

// dfgVarSummary aggregates variable and parameter names per function for standard mode.
type dfgVarSummary struct {
	params []string
	vars   []string
}

// LinkDataFlowGraph constructs Data Flow Graph (DFG) parameter and variable lineage edges.
// The granularity depends on cpg.Config.LevelOfDetail:
//   - "standard": creates one DFG_SUMMARY node per function with variable names
//   - "full" (default): creates one DFG_VAR node per variable/parameter
//   - "architecture": should not be called (handled by LinkerConfig pass filtering)
func LinkDataFlowGraph(stage3Out *stage3.Stage3Output, cpg *Stage4Output) {
	if stage3Out == nil || stage3Out.RootNode == nil || cpg == nil {
		return
	}

	om := ownershipMap(cpg, stage3Out)

	var dfgSummaries map[string]*dfgVarSummary
	if cpg.Config.LevelOfDetail == LevelStandard {
		dfgSummaries = make(map[string]*dfgVarSummary)
	}

	traverseForDFG(stage3Out.RootNode, cpg, stage3Out, om, dfgSummaries)

	if dfgSummaries != nil {
		buildDFGSummaryNodes(dfgSummaries, cpg)
	}
}

func traverseForDFG(dir *stage3.DirectoryNode, cpg *Stage4Output, stage3Out *stage3.Stage3Output, om *stage3.OwnershipMap, dfgSummaries map[string]*dfgVarSummary) {
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

		extractDFGFromGAST(file.GASTRoot, file.RelativePath, "", cpg, stage3Out, om, dfgSummaries, fileNodeCount)
	}

	for _, subDir := range dir.SubFolders {
		traverseForDFG(subDir, cpg, stage3Out, om, dfgSummaries)
	}
}

func extractDFGFromGAST(node *stage2.GASTNode, relPath, currentFuncID string, cpg *Stage4Output, stage3Out *stage3.Stage3Output, om *stage3.OwnershipMap, dfgSummaries map[string]*dfgVarSummary, fileNodeCount map[string]int) {
	if node == nil {
		return
	}

	funcID := currentFuncID
	if node.Type == stage2.GASTFunction {
		funcID = universalFuncID(relPath, node.ReceiverType, node.Name)
		if dfgSummaries != nil && funcID != "" {
			if _, ok := dfgSummaries[funcID]; !ok {
				dfgSummaries[funcID] = &dfgVarSummary{}
			}
		}
	}

	// Trace variable assignments and parameters
	if funcID != "" && (node.Type == stage2.GASTVariable || node.Type == stage2.GASTParameter) {
		if dfgSummaries != nil {
			// Standard mode: aggregate into function summary
			if node.Type == stage2.GASTParameter {
				dfgSummaries[funcID].params = append(dfgSummaries[funcID].params, node.Name)
			} else {
				dfgSummaries[funcID].vars = append(dfgSummaries[funcID].vars, node.Name)
			}
		} else {
			// Full mode: create per-variable DFG_VAR nodes
			varID := fmt.Sprintf("%s::VAR_%s", funcID, node.Name)

			// Apply per-file node budget (MaxNodesPerFile)
			fileNodeCount[relPath]++
			if cpg.Config.MaxNodesPerFile > 0 && fileNodeCount[relPath] > cpg.Config.MaxNodesPerFile {
				goto skipDFGNode
			}

			cpg.GraphNodes[varID] = &ResolvedNode{
				ID:   varID,
				Kind: "DFG_VAR",
				Name: node.Name,
				FileSpec: LocationMeta{
					Path:      relPath,
					LineStart: int(node.StartLine),
					LineEnd:   int(node.EndLine),
				},
			}

			cpg.AddEdge(funcID, varID, EdgeDataFlow, int(node.StartLine))

			if node.DataType != "" {
				targetTypeFQN := resolveTypeToFQN(strings.TrimPrefix(node.DataType, "*"), relPath, nil, cpg)
				if targetTypeFQN != "" {
					cpg.AddEdge(varID, targetTypeFQN, EdgeDataFlow, int(node.StartLine))

					if strings.HasPrefix(node.DataType, "*") || strings.Contains(node.Name, "&") {
						cpg.AddEdge(varID, targetTypeFQN, EdgeAliases, int(node.StartLine))
						cpg.AddEdge(targetTypeFQN, varID, EdgeAliases, int(node.StartLine))
					}
				}
			}
		}
	}

skipDFGNode:
	// Trace call arguments data flow and Database Taint. Per-call DATA_FLOW
	// edges duplicate the CALLS edge from the callgraph linker; they only run
	// in full mode (AUDIT Issue 1.5 / Phase 1B-5).
	if isFullMode(cpg) && funcID != "" && node.Type == stage2.GASTCallExpression {
		content := node.Properties["content"]
		if content == "" {
			content = node.Name
		}

		isDB := false
		for _, p := range node.Primitives {
			if p == stage2.PrimDatabase {
				isDB = true
			}
		}

		if isDB {
			taintID := "TAINT:DATABASE"
			ensureVirtualNode(taintID, "VIRTUAL_TAINT_SOURCE", "Database Taint Source", cpg)
			cpg.AddEdge(taintID, funcID, EdgeDataFlow, int(node.StartLine))
		}

		if strings.Contains(content, "(") || strings.Contains(content, ".") {
			targetID, _ := resolveCallTarget(node.Name, node.Name, relPath, nil, om, cpg, stage3Out)
			if targetID != "" {
				cpg.AddEdge(funcID, targetID, EdgeDataFlow, int(node.StartLine))
			}
		}
	}

	for _, child := range node.Children {
		extractDFGFromGAST(child, relPath, funcID, cpg, stage3Out, om, dfgSummaries, fileNodeCount)
	}
}

// buildDFGSummaryNodes creates one DFG_SUMMARY node per function listing parameters and variables.
func buildDFGSummaryNodes(dfgSummaries map[string]*dfgVarSummary, cpg *Stage4Output) {
	for funcID, summary := range dfgSummaries {
		summaryID := funcID + "::DFG_SUMMARY"
		if cpg.NodeExists(summaryID) {
			continue
		}

		props := make(map[string]string)
		props["param_count"] = fmt.Sprintf("%d", len(summary.params))
		props["var_count"] = fmt.Sprintf("%d", len(summary.vars))
		if len(summary.params) > 0 {
			props["params"] = strings.Join(summary.params, ",")
		}
		if len(summary.vars) > 0 {
			props["vars"] = strings.Join(summary.vars, ",")
		}

		cpg.GraphNodes[summaryID] = &ResolvedNode{
			ID:         summaryID,
			Kind:       "DFG_SUMMARY",
			Name:       "Data Flow Summary",
			Properties: props,
		}

		cpg.AddEdge(funcID, summaryID, EdgeDataFlow, 0)
	}
}
