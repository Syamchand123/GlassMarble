package stage3

import (
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage2"
)

// IndexExternalDependencies scans all file symbol tables, extracts their imports,
// and if an import is not within the workspace modules, logs it as an ExternalSystem node.
func IndexExternalDependencies(output *Stage3Output) {
	if output.ExternalDependencies == nil {
		output.ExternalDependencies = make(map[string]*stage2.GASTNode)
	}

	for relPath, symTable := range output.LocalTables {
		if symTable == nil || len(symTable.Imports) == 0 {
			continue
		}

		for _, imp := range symTable.Imports {
			// Basic heuristic: if import contains "." (e.g. github.com) and is NOT in the workspace boundaries
			// then it's external.
			if !isLocalImport(imp, relPath, output) {
				if _, exists := output.ExternalDependencies[imp]; !exists {
					node := &stage2.GASTNode{
						Type:       stage2.GASTTypeDeclaration,
						Name:       imp,
						Visibility: "External",
						Properties: map[string]string{
							"is_external": "true",
							"primitive":   "EXTERNAL_SDK",
						},
					}
					output.ExternalDependencies[imp] = node
				}
			}
		}
	}
}

func isLocalImport(imp string, relPath string, output *Stage3Output) bool {
	// If it starts with relative paths, it's local
	if strings.HasPrefix(imp, ".") || strings.HasPrefix(imp, "/") {
		return true
	}

	// Use our new WorkspaceContext checks
	wc := output.WorkspaceCtx
	if wc != nil {
		// If it has our module prefix
		if wc.ModulePrefix != "" && strings.HasPrefix(imp, wc.ModulePrefix) {
			return true
		}

		// Or if it hits one of the workspace aliases
		for alias := range wc.Aliases {
			if strings.HasPrefix(imp, alias) {
				return true
			}
		}
	}

	// For standard libraries, usually no dots (e.g., 'fmt', 'net/http', 'react', 'fs')
	if !strings.Contains(imp, ".") {
		// Might be a core language lib, we'll still consider it 'external' to the project domain,
		// but typically we want to isolate third-party (github, npm domain, etc.). 
		// Actually, treating stdlib as external is perfectly fine.
		return false
	}

	return false
}
