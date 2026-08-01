package stage4

import (
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage2"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage3"
)

// LinkEscapeAnalysis performs a lightweight escape analysis to find variables
// that outlive their stack frames and must be allocated on the heap.
// Escape edges are heuristic and only run in full mode (AUDIT Issue 1.4 /
// Phase 1B-5).
func LinkEscapeAnalysis(stage3Out *stage3.Stage3Output, cpg *Stage4Output) {
	if stage3Out == nil || stage3Out.RootNode == nil || cpg == nil {
		return
	}
	if !isFullMode(cpg) {
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
		funcID = universalFuncID(relPath, node.ReceiverType, node.Name)
	}

	// 1. Returning a pointer to a local variable.
	// Exact control-flow kind match — never the old strings.Contains heuristic,
	// which matched any name containing "return" (e.g. "returnValue").
	isReturn := node.Type == stage2.GASTControlFlow &&
		(node.Kind == "return_statement" || node.Kind == "return")
	if funcID != "" && isReturn {
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
					// The escape target must exist — previously every edge
					// dangled because VAR_ nodes were never created
					// (AUDIT Issue 1.6).
					if !cpg.NodeExists(varID) {
						ensureVirtualNode(varID, "VIRTUAL_VARIABLE", escapedObj, cpg)
					}
					cpg.AddEdge(funcID, varID, EdgeEscapesToHeap, int(node.StartLine))
				}
			}
		}
	}

	// 2. Closure capture (goroutine capturing local variables)
	if funcID != "" && node.Type == stage2.GASTCallExpression && strings.HasPrefix(node.Properties["content"], "go ") {
		// All variables inside the closure escape.
		// For now we map an edge from the concurrency spawn to the generic HEAP.
		// The HEAP target must exist — previously the edge dangled (AUDIT Issue 1.6).
		ensureVirtualNode("memory::HEAP", "VIRTUAL_HEAP", "Heap", cpg)
		cpg.AddEdge(funcID, "memory::HEAP", EdgeEscapesToHeap, int(node.StartLine))
	}

	for _, child := range node.Children {
		extractEscapesFromGAST(child, relPath, funcID, cpg)
	}
}
