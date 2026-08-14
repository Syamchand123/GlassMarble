package link

import (
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/normalize"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/aggregate"
)

// LinkConstraints performs Abstract Interpretation on Conditional Branches (CFG_IF)
// and maps mathematical/logical constraints to edges to enable Dead Code & Nil Pointer detection.
// ABSTRACT_CONSTRAINT nodes are per-statement synthetic nodes; they only run in
// full mode (AUDIT Issue 1.4 / Phase 1B-5).
func LinkConstraints(aggregateOut *aggregate.AggregateOutput, cpg *LinkOutput) {
	if aggregateOut == nil || aggregateOut.RootNode == nil || cpg == nil {
		return
	}
	if !isFullMode(cpg) {
		return
	}
	traverseForConstraints(aggregateOut.RootNode, cpg)
}

func traverseForConstraints(dir *aggregate.DirectoryNode, cpg *LinkOutput) {
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
		extractConstraintsFromGAST(file.GASTRoot, file.RelativePath, "", cpg)
	}
	for _, subDir := range dir.SubFolders {
		traverseForConstraints(subDir, cpg)
	}
}

func extractConstraintsFromGAST(node *normalize.GASTNode, relPath, currentFuncID string, cpg *LinkOutput) {
	if node == nil {
		return
	}
	funcID := currentFuncID
	if node.Type == normalize.GASTFunction {
		funcID = universalFuncID(relPath, node.ReceiverType, node.Name)
	}

	// Exact control-flow kind match — never the old strings.Contains heuristic,
	// which matched names like "specification" and fabricated constraints on
	// non-branch nodes (AUDIT Issue 1.4 / 1.8).
	isBranch := node.Type == normalize.GASTControlFlow &&
		(node.Kind == "if_statement" || node.Kind == "if" || node.Kind == "conditional")
	if funcID != "" && isBranch {
		cond := node.Properties["condition"]
		if cond != "" {
			condID := funcID + "::CONSTRAINT_" + cond

			// Build the abstract interpretation constraint node
			cpg.GraphNodes[condID] = &ResolvedNode{
				ID:   condID,
				Kind: "ABSTRACT_CONSTRAINT",
				Name: cond,
				Properties: map[string]string{
					"logic": extractLogic(cond),
				},
				FileSpec: LocationMeta{
					Path:      relPath,
					LineStart: int(node.StartLine),
					LineEnd:   int(node.EndLine),
				},
			}

			// Attach the constraint to the CFG edge
			cpg.AddEdge(funcID, condID, EdgeConstraint, int(node.StartLine))
		}
	}

	for _, child := range node.Children {
		extractConstraintsFromGAST(child, relPath, funcID, cpg)
	}
}

func extractLogic(cond string) string {
	if strings.Contains(cond, "!=") {
		return "NOT_EQUAL"
	}
	if strings.Contains(cond, "==") {
		return "EQUAL"
	}
	if strings.Contains(cond, ">") {
		return "GREATER"
	}
	if strings.Contains(cond, "<") {
		return "LESS"
	}
	return "TRUTHY"
}
