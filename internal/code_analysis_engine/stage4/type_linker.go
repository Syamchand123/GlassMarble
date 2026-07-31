package stage4

import (
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage2"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage3"
)

// LinkTypesAndComposition resolves cross-file type references and field compositions.
func LinkTypesAndComposition(stage3Out *stage3.Stage3Output, cpg *Stage4Output) {
	if stage3Out == nil || stage3Out.RootNode == nil || cpg == nil {
		return
	}

	traverseForTypeLinking(stage3Out.RootNode, stage3Out.GlobalDefinitionIndex, cpg)
}

func traverseForTypeLinking(dir *stage3.DirectoryNode, globalIndex map[string][]*stage2.GASTNode, cpg *Stage4Output) {
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

		linkNodesInGAST(file.GASTRoot, file.RelativePath, globalIndex, cpg)
	}

	for _, subDir := range dir.SubFolders {
		traverseForTypeLinking(subDir, globalIndex, cpg)
	}
}

func linkNodesInGAST(node *stage2.GASTNode, relPath string, globalIndex map[string][]*stage2.GASTNode, cpg *Stage4Output) {
	if node == nil {
		return
	}

	if node.Type == stage2.GASTTypeDeclaration {
		sourceFQN := BuildUniversalID(relPath, "", node.Name)

		// Check both "extends" and "inherits" properties (Python uses inherits)
		baseClass := node.Properties["extends"]
		if baseClass == "" {
			baseClass = node.Properties["inherits"]
		}
		if baseClass != "" {
			targetFQN := resolveTypeToFQN(baseClass, relPath, globalIndex, cpg)
			if targetFQN == "" {
				// Try direct node ID lookup
				targetFQN = BuildUniversalID(relPath, "", baseClass)
			}
			if targetFQN != "" && sourceFQN != "" && sourceFQN != targetFQN {
				cpg.AddEdge(sourceFQN, targetFQN, EdgeExtends, int(node.StartLine))
			}
		}

		// Inspect children fields and content for composition and type references
		for _, child := range node.Children {
			if child.Type == stage2.GASTField || child.Type == stage2.GASTVariable {
				// Handle Generics e.g. List<User>
				cleanType := child.DataType
				genericType := ""
				if startIdx := strings.Index(cleanType, "<"); startIdx != -1 {
					if endIdx := strings.LastIndex(cleanType, ">"); endIdx > startIdx {
						genericType = cleanType[startIdx+1 : endIdx]
						cleanType = cleanType[:startIdx]
					}
				}

				targetFQN := resolveTypeToFQN(cleanType, relPath, globalIndex, cpg)
				if targetFQN != "" && sourceFQN != "" && sourceFQN != targetFQN {
					cpg.AddEdge(sourceFQN, targetFQN, EdgeComposes, int(child.StartLine))
				}

				if genericType != "" {
					genericFQN := resolveTypeToFQN(genericType, relPath, globalIndex, cpg)
					if genericFQN != "" && sourceFQN != "" && sourceFQN != genericFQN {
						cpg.AddEdge(sourceFQN, genericFQN, EdgeInstantiates, int(child.StartLine))
					}
				}
			}
		}
	} else if node.Type == stage2.GASTFunction {
		sourceFQN := BuildUniversalID(relPath, node.ReceiverType, node.Name)

		// Link parameter and return types as REFERENCES
		if node.DataType != "" {
			targetFQN := resolveTypeToFQN(node.DataType, relPath, globalIndex, cpg)
			if targetFQN != "" && sourceFQN != "" && sourceFQN != targetFQN {
				cpg.AddEdge(sourceFQN, targetFQN, EdgeReferences, int(node.StartLine))
			}
		}
	}

	for _, child := range node.Children {
		linkNodesInGAST(child, relPath, globalIndex, cpg)
	}
}

// resolveTypeToFQN attempts to match a raw type string (e.g. "PostgresStore", "database.PostgresStore", "*UserStore")
// to a universal signature ID in cpg.GraphNodes, GlobalDefinitionIndex, or DB.
func resolveTypeToFQN(rawType, currentFilePath string, globalIndex map[string][]*stage2.GASTNode, cpg *Stage4Output) string {
	clean := strings.TrimPrefix(rawType, "*")
	clean = strings.TrimPrefix(clean, "[]")
	clean = strings.TrimSpace(clean)
	if clean == "" {
		return ""
	}

	folderPath := stage3.NormalizeRelativePath(currentFilePath)

	// 1. Direct universal ID match in cpg.GraphNodes
	universalLocalID := BuildUniversalID(folderPath, "", clean)
	if _, ok := cpg.GetNode(universalLocalID); ok {
		return universalLocalID
	}

	// 2. Search GlobalDefinitionIndex
	if targetNodes, ok := globalIndex[clean]; ok && len(targetNodes) > 0 {
		targetNode := targetNodes[0]
		return BuildUniversalID(targetNode.Properties["file_path"], targetNode.ReceiverType, targetNode.Name)
	}

	// 3. Search cpg.GraphNodes by trailing symbol name match
	for nodeID, node := range cpg.GraphNodes {
		if node.Name == clean && (node.Kind == "STRUCT" || node.Kind == "CLASS" || node.Kind == "INTERFACE") {
			return nodeID
		}
	}

	return ""
}
