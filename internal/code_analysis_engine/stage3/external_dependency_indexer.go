package stage3

import (
	"path/filepath"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage2"
)

// IndexExternalDependencies scans all file symbol tables, extracts their imports,
// and if an import is not within the workspace modules, logs it as an ExternalSystem node.
// Language stdlib imports (fmt, os, java.*, node builtins) are deliberately excluded:
// they are not part of the project domain and fabricating EXTERNAL_SDK nodes for them
// bloats the graph (AUDIT Issue 1.4).
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
			if IsStdlibImport(imp, relPath) {
				continue
			}
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

// IsStdlibImport reports whether an import path belongs to the language's
// standard library rather than a third-party dependency. Shared by the
// indexer and stage4 call resolution so stdlib calls never fabricate
// EXTERNAL_SDK/EXTERNAL_API nodes.
func IsStdlibImport(imp string, relPath string) bool {
	if imp == "" {
		return false
	}

	first := imp
	if idx := strings.Index(imp, "/"); idx != -1 {
		first = imp[:idx]
	}

	ext := strings.ToLower(filepath.Ext(relPath))
	switch ext {
	case ".go":
		// Go stdlib import paths never contain a dot in the first segment
		// (e.g. "fmt", "net/http", "encoding/json").
		return !strings.Contains(first, ".")
	case ".py":
		// Python builtin/stdlib modules import without dots (os, json, math);
		// third-party packages almost always carry a dot (flask is the notable
		// exception, tolerated to avoid stdlib fabrication).
		return !strings.Contains(imp, ".")
	case ".java", ".kt", ".kts":
		return strings.HasPrefix(imp, "java.") || strings.HasPrefix(imp, "javax.") ||
			strings.HasPrefix(imp, "jakarta.") || strings.HasPrefix(imp, "sun.") ||
			strings.HasPrefix(imp, "com.sun.")
	case ".js", ".jsx", ".ts", ".tsx", ".mjs", ".cjs":
		// Node.js core modules (fs, path, node:*) import without dots/slashes.
		return strings.HasPrefix(imp, "node:") || (!strings.Contains(imp, "/") && !strings.Contains(imp, "."))
	default:
		return false
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
