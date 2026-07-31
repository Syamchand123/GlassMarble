package stage4

import (
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage2"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage3"
)

// LinkConstraints performs Abstract Interpretation on Conditional Branches (CFG_IF)
// and maps mathematical/logical constraints to edges to enable Dead Code & Nil Pointer detection.
func LinkConstraints(stage3Out *stage3.Stage3Output, cpg *Stage4Output) {
	if stage3Out == nil || stage3Out.RootNode == nil || cpg == nil {
		return
	}
	traverseForConstraints(stage3Out.RootNode, cpg)
}

func traverseForConstraints(dir *stage3.DirectoryNode, cpg *Stage4Output) {
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
		extractConstraintsFromGAST(file.GASTRoot, file.RelativePath, "", cpg)
	}
	for _, subDir := range dir.SubFolders {
		traverseForConstraints(subDir, cpg)
	}
}

func extractConstraintsFromGAST(node *stage2.GASTNode, relPath, currentFuncID string, cpg *Stage4Output) {
	if node == nil {
		return
	}
	funcID := currentFuncID
	if node.Type == stage2.GASTFunction {
		funcID = BuildUniversalID(relPath, node.ReceiverType, node.Name)
	}

	lowerType := strings.ToLower(string(node.Type) + " " + node.Kind + " " + node.Name)
	if funcID != "" && strings.Contains(lowerType, "if") {
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
