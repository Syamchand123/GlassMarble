package link

import (
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/normalize"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/aggregate"
)

// LinkFFI traces Cross-Language Foreign Function Interfaces (e.g., CGO).
func LinkFFI(aggregateOut *aggregate.AggregateOutput, cpg *LinkOutput) {
	if aggregateOut == nil || aggregateOut.RootNode == nil || cpg == nil {
		return
	}
	traverseForFFI(aggregateOut.RootNode, cpg)
}

func traverseForFFI(dir *aggregate.DirectoryNode, cpg *LinkOutput) {
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

		// If it's a Go file and imports "C"
		hasCGO := false
		for _, node := range file.GASTRoot.Children {
			if node.Type == normalize.GASTImport && node.Name == "\"C\"" {
				hasCGO = true
				break
			}
		}

		if hasCGO {
			extractFFIFromGAST(file.GASTRoot, file.RelativePath, "", cpg)
		}
	}
	for _, subDir := range dir.SubFolders {
		traverseForFFI(subDir, cpg)
	}
}

func extractFFIFromGAST(node *normalize.GASTNode, relPath, currentFuncID string, cpg *LinkOutput) {
	if node == nil {
		return
	}
	funcID := currentFuncID
	if node.Type == normalize.GASTFunction {
		funcID = universalFuncID(relPath, node.ReceiverType, node.Name)
	}

	if funcID != "" && node.Type == normalize.GASTCallExpression {
		// Example: C.malloc, C.free, C.call_something
		if strings.HasPrefix(node.Name, "C.") {
			cFunc := strings.TrimPrefix(node.Name, "C.")
			cNodeID := "ffi:C::" + cFunc

			if _, exists := cpg.GetNode(cNodeID); !exists {
				cpg.GraphNodes[cNodeID] = &ResolvedNode{
					ID:   cNodeID,
					Kind: "EXTERNAL_FFI",
					Name: cFunc,
					Properties: map[string]string{
						"ffi_lang": "C",
					},
				}
			}
			cpg.AddEdge(funcID, cNodeID, EdgeFFICall, int(node.StartLine))
		}
	}

	for _, child := range node.Children {
		extractFFIFromGAST(child, relPath, funcID, cpg)
	}
}
