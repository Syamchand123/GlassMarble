package aggregate

import (
	"net/url"
	"path/filepath"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/normalize"
	"github.com/Syamchand123/GlassMarble/internal/product/ont"
)

// ExternalKey returns the v2 external system ID for an import path:
// "ext:" + URL-escaped path (master_overhaul_plan.md §5.3.1 W1-09).
// URL escaping keeps slashes/colons out of the ID space so keys are
// safe to embed in CPG node IDs and JSON lookups.
func ExternalKey(imp string) string {
	return ont.PrefixExt + url.PathEscape(imp)
}

// ResolveExternalKey self-heals between the v1 spelling (raw import
// path, e.g. "github.com/acme/lib") and the v2 spelling ("ext:github.com%2Facme%2Flib"):
// v1 keys are accepted for reads (old caches/deps.json), v2 for writes.
// Read-only: never mutates the caller's map.
func ResolveExternalKey(key string) string {
	if strings.HasPrefix(key, ont.PrefixExt) {
		return key
	}
	return ExternalKey(key)
}

// moduleNameOf derives the gm:module_name property for an external
// dependency: the workspace module for self-module crossings, otherwise
// the import's first path segment (§5.3.3 — module path belongs in
// properties, never in the ext: URI, §4.1).
func moduleNameOf(imp, modulePrefix string) string {
	if modulePrefix != "" && strings.HasPrefix(imp, modulePrefix) {
		return modulePrefix
	}
	if idx := strings.Index(imp, "/"); idx != -1 {
		return imp[:idx]
	}
	return imp
}

// IndexExternalDependencies scans all file symbol tables, extracts their imports,
// and if an import is not within the workspace modules, logs it as an ExternalSystem node.
// Language stdlib imports (fmt, os, java.*, node builtins) are deliberately excluded:
// they are not part of the project domain and fabricating EXTERNAL_SDK nodes for them
// bloats the graph (AUDIT Issue 1.4).
func IndexExternalDependencies(output *AggregateOutput) {
	// Rebuild from scratch to purge orphans from deleted files (C2-10).
	newDeps := make(map[string]*normalize.GASTNode)
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
				key := ExternalKey(imp)
				if _, exists := newDeps[key]; !exists {
					modulePrefix := ""
					if output.WorkspaceCtx != nil {
						modulePrefix = output.WorkspaceCtx.ModulePrefix
					}
					node := &normalize.GASTNode{
						Type:       normalize.GASTTypeDeclaration,
						Name:       imp,
						Visibility: "External",
						Properties: map[string]string{
							"is_external":          "true",
							ont.PredIsExternalMark: "true",
							"primitive":            "EXTERNAL_SDK",
							ont.PredPrimitive:      "EXTERNAL_SDK",
							ont.PredImportPath:     imp,
							ont.PredModuleName:     moduleNameOf(imp, modulePrefix),
							"import_path":          imp,
							"ext_id":               key,
						},
					}
					if alias, ok := symTable.ImportAliases[imp]; ok {
						node.Properties["alias"] = alias
						node.Properties[ont.PredImportAlias] = alias
					}
					newDeps[key] = node
				}
			} else if output.WorkspaceCtx != nil && output.WorkspaceCtx.ModulePrefix != "" {
				// v2 (W1-09, §5.3.5 TestExternalIDs): an ALIASED import of
				// this module's own path crosses the module boundary; index
				// it as ext:<module-relative path> with the alias recorded.
				prefix := output.WorkspaceCtx.ModulePrefix + "/"
				alias, hasAlias := symTable.ImportAliases[imp]
				if hasAlias && strings.HasPrefix(imp, prefix) {
					rel := strings.TrimPrefix(imp, prefix)
					key := ExternalKey(rel)
					if _, exists := newDeps[key]; !exists {
						newDeps[key] = &normalize.GASTNode{
							Type:       normalize.GASTTypeDeclaration,
							Name:       rel,
							Visibility: "External",
							Properties: map[string]string{
								"is_external":          "true",
								ont.PredIsExternalMark: "true",
								"primitive":            "EXTERNAL_SDK",
								ont.PredPrimitive:      "EXTERNAL_SDK",
								ont.PredImportPath:     imp,
								ont.PredModuleName:     output.WorkspaceCtx.ModulePrefix,
								ont.PredImportAlias:    alias,
								"import_path":          imp,
								"ext_id":               key,
								"alias":                alias,
							},
						}
					}
				}
			}
		}
	}
	output.ExternalDependencies = newDeps
}

// IsStdlibImport reports whether an import path belongs to the language's
// standard library rather than a third-party dependency. Shared by the
// indexer and link call resolution so stdlib calls never fabricate
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

func isLocalImport(imp string, relPath string, output *AggregateOutput) bool {
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
