package stage4

import (
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage2"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage3"
)

// LinkFFI traces Cross-Language Foreign Function Interfaces (e.g., CGO).
func LinkFFI(stage3Out *stage3.Stage3Output, cpg *Stage4Output) {
	if stage3Out == nil || stage3Out.RootNode == nil || cpg == nil {
		return
	}
	traverseForFFI(stage3Out.RootNode, cpg)
}

func traverseForFFI(dir *stage3.DirectoryNode, cpg *Stage4Output) {
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
		
		// If it's a Go file and imports "C"
		hasCGO := false
		for _, node := range file.GASTRoot.Children {
			if node.Type == stage2.GASTImport && node.Name == "\"C\"" {
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

func extractFFIFromGAST(node *stage2.GASTNode, relPath, currentFuncID string, cpg *Stage4Output) {
	if node == nil {
		return
	}
	funcID := currentFuncID
	if node.Type == stage2.GASTFunction {
		funcID = BuildUniversalID(relPath, node.ReceiverType, node.Name)
	}

	if funcID != "" && node.Type == stage2.GASTCallExpression {
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
