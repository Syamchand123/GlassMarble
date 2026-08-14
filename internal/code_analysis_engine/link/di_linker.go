package link

import (
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/normalize"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/aggregate"
)

// LinkDependencyInjection connects DI container providers to their injection sites.
func LinkDependencyInjection(aggregateOut *aggregate.AggregateOutput, cpg *LinkOutput) {
	if aggregateOut == nil || aggregateOut.RootNode == nil || cpg == nil {
		return
	}
	om := ownershipMap(cpg, aggregateOut)
	traverseForDI(aggregateOut.RootNode, om, aggregateOut, cpg)
}

func traverseForDI(dir *aggregate.DirectoryNode, om *aggregate.OwnershipMap, aggregateOut *aggregate.AggregateOutput, cpg *LinkOutput) {
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
		extractDIFromGAST(file.GASTRoot, file.RelativePath, "", file.LocalImports, om, aggregateOut, cpg)
	}
	for _, subDir := range dir.SubFolders {
		traverseForDI(subDir, om, aggregateOut, cpg)
	}
}

func extractDIFromGAST(node *normalize.GASTNode, relPath, currentFuncID string, localImports []string, om *aggregate.OwnershipMap, aggregateOut *aggregate.AggregateOutput, cpg *LinkOutput) {
	if node == nil {
		return
	}
	funcID := currentFuncID
	if node.Type == normalize.GASTFunction {
		funcID = universalFuncID(relPath, node.ReceiverType, node.Name)
	}

	if funcID != "" && node.Type == normalize.GASTCallExpression {
		// Heuristic for DI: wire.Build, fx.Provide, Container.Bind
		if strings.Contains(node.Name, "wire.Build") || strings.Contains(node.Name, "fx.Provide") || strings.Contains(node.Name, "Bind") {
			// Find arguments (the constructors/providers being injected)
			content := node.Properties["content"]
			if content != "" {
				// Naively match potential function names
				parts := strings.FieldsFunc(content, func(r rune) bool {
					return r == '(' || r == ')' || r == ',' || r == ' ' || r == '\n' || r == '\t'
				})

				for _, part := range parts {
					if part == "wire.Build" || part == "fx.Provide" || part == "Bind" {
						continue
					}
					// Strip package prefix if it exists to check the actual function name
					partTrimmed := part
					if idx := strings.LastIndex(part, "."); idx != -1 {
						partTrimmed = part[idx+1:]
					}
					// If it looks like a provider function (e.g., NewUserService)
					if strings.HasPrefix(partTrimmed, "New") || strings.HasSuffix(partTrimmed, "Provider") {
						targetID, _ := resolveCallTarget("", part, relPath, localImports, om, cpg, aggregateOut)
						if targetID != "" {
							// Draw an EdgeInjects from the DI initialization func to the provider
							cpg.AddEdge(funcID, targetID, EdgeInjects, int(node.StartLine))
						}
					}
				}
			}
		}
	}

	for _, child := range node.Children {
		extractDIFromGAST(child, relPath, funcID, localImports, om, aggregateOut, cpg)
	}
}
