package stage4

import (
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage2"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage3"
)

// LinkEscapeAnalysis performs a lightweight escape analysis to find variables
// that outlive their stack frames and must be allocated on the heap.
func LinkEscapeAnalysis(stage3Out *stage3.Stage3Output, cpg *Stage4Output) {
	if stage3Out == nil || stage3Out.RootNode == nil || cpg == nil {
		return
	}
	traverseForEscapes(stage3Out.RootNode, cpg)
}

func traverseForEscapes(dir *stage3.DirectoryNode, cpg *Stage4Output) {
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
		extractEscapesFromGAST(file.GASTRoot, file.RelativePath, "", cpg)
	}
	for _, subDir := range dir.SubFolders {
		traverseForEscapes(subDir, cpg)
	}
}

func extractEscapesFromGAST(node *stage2.GASTNode, relPath, currentFuncID string, cpg *Stage4Output) {
	if node == nil {
		return
	}
	funcID := currentFuncID
	if node.Type == stage2.GASTFunction {
		funcID = BuildUniversalID(relPath, node.ReceiverType, node.Name)
	}

	// 1. Returning a pointer to a local variable
	lowerType := strings.ToLower(string(node.Type) + " " + node.Kind + " " + node.Name)
	if funcID != "" && strings.Contains(lowerType, "return") {
		content := node.Properties["content"]
		if strings.Contains(content, "&") {
			// e.g. return &User{} -> Escapes to Heap
			parts := strings.Split(content, "&")
			if len(parts) > 1 {
				escapedObj := strings.TrimSpace(parts[1])
				idx := strings.IndexAny(escapedObj, " {(),.")
				if idx != -1 {
					escapedObj = escapedObj[:idx]
				}
				if escapedObj != "" {
					varID := funcID + "::VAR_" + escapedObj
					cpg.AddEdge(funcID, varID, EdgeEscapesToHeap, int(node.StartLine))
				}
			}
		}
	}

	// 2. Closure capture (goroutine capturing local variables)
	if funcID != "" && node.Type == stage2.GASTCallExpression && strings.HasPrefix(node.Properties["content"], "go ") {
		// All variables inside the closure escape.
		// For now we map an edge from the concurrency spawn to the generic HEAP
		cpg.AddEdge(funcID, "memory::HEAP", EdgeEscapesToHeap, int(node.StartLine))
	}

	for _, child := range node.Children {
		extractEscapesFromGAST(child, relPath, funcID, cpg)
	}
}
