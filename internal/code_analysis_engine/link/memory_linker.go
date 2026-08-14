package link

import (
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/normalize"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/aggregate"
)

// LinkEscapeAnalysis performs a lightweight escape analysis to find variables
// that outlive their stack frames and must be allocated on the heap.
// Escape edges are heuristic and only run in full mode (AUDIT Issue 1.4 /
// Phase 1B-5).
func LinkEscapeAnalysis(aggregateOut *aggregate.AggregateOutput, cpg *LinkOutput) {
	if aggregateOut == nil || aggregateOut.RootNode == nil || cpg == nil {
		return
	}
	if !isFullMode(cpg) {
		return
	}
	traverseForEscapes(aggregateOut.RootNode, cpg)
}

func traverseForEscapes(dir *aggregate.DirectoryNode, cpg *LinkOutput) {
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
		extractEscapesFromGAST(file.GASTRoot, file.RelativePath, "", cpg)
	}
	for _, subDir := range dir.SubFolders {
		traverseForEscapes(subDir, cpg)
	}
}

func extractEscapesFromGAST(node *normalize.GASTNode, relPath, currentFuncID string, cpg *LinkOutput) {
	if node == nil {
		return
	}
	funcID := currentFuncID
	if node.Type == normalize.GASTFunction {
		funcID = universalFuncID(relPath, node.ReceiverType, node.Name)
	}

	// 1. Returning a pointer to a local variable.
	// Exact control-flow kind match — never the old strings.Contains heuristic,
	// which matched any name containing "return" (e.g. "returnValue").
	isReturn := node.Type == normalize.GASTControlFlow &&
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
					if !cpg.HasNode(varID) {
						ensureVirtualNode(varID, "VIRTUAL_VARIABLE", escapedObj, cpg)
					}
					cpg.AddEdge(funcID, varID, EdgeEscapesToHeap, int(node.StartLine))
				}
			}
		}
	}

	// 2. Closure capture (goroutine capturing local variables)
	if funcID != "" && node.Type == normalize.GASTCallExpression && strings.HasPrefix(node.Properties["content"], "go ") {
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
