package aggregate

import (
	"path/filepath"
	"strings"
)

type ImportResolver interface {
	Resolve(importPath, fromFile, rootDir string, wc *WorkspaceContext) []string
}

type GenericImportResolver struct{}

func (r *GenericImportResolver) Resolve(importPath, fromFile, rootDir string, wc *WorkspaceContext) []string {
	if importPath == "" {
		return nil
	}

	cleanImport := strings.Trim(importPath, "\"';`<>")

	// Direct relative path resolution (e.g. ./utils or ../models)
	if strings.HasPrefix(cleanImport, ".") {
		dir := filepath.Dir(fromFile)
		target := filepath.Clean(filepath.Join(dir, cleanImport))
		return []string{filepath.ToSlash(target)}
	}

	// Package style import (e.g. github.com/user/repo/pkg/auth -> pkg/auth)
	parts := strings.Split(cleanImport, "/")
	n := len(parts)
	if n >= 2 {
		shortPath := strings.Join(parts[n-2:], "/")
		return []string{shortPath, parts[n-1]}
	}
	if n == 1 {
		return []string{cleanImport}
	}

	return nil
}

type GoImportResolver struct{}

func (r *GoImportResolver) Resolve(importPath, fromFile, rootDir string, wc *WorkspaceContext) []string {
	clean := strings.Trim(importPath, "\"';`<>")

	// Leverage Workspace Context for exact local mapping (Enterprise Monorepo)
	if wc != nil && wc.ModulePrefix != "" {
		if strings.HasPrefix(clean, wc.ModulePrefix) {
			localPath := strings.TrimPrefix(clean, wc.ModulePrefix)
			localPath = strings.TrimPrefix(localPath, "/")
			return []string{clean, localPath}
		}
	}

	parts := strings.Split(clean, "/")
	if len(parts) > 0 {
		return []string{clean, parts[len(parts)-1]}
	}
	return []string{clean}
}

type PythonImportResolver struct{}

func (r *PythonImportResolver) Resolve(importPath, fromFile, rootDir string, wc *WorkspaceContext) []string {
	clean := strings.Trim(importPath, "\"';`<>")
	// Python imports use dots: from x.y import z -> x.y.z
	parts := strings.Split(clean, ".")
	return []string{clean, filepath.Join(parts...), parts[len(parts)-1]}
}

type JavaImportResolver struct{}

func (r *JavaImportResolver) Resolve(importPath, fromFile, rootDir string, wc *WorkspaceContext) []string {
	clean := strings.Trim(importPath, "\"';`<>")
	parts := strings.Split(clean, ".")
	return []string{clean, filepath.Join(parts...), parts[len(parts)-1]}
}

type TSImportResolver struct{}

func (r *TSImportResolver) Resolve(importPath, fromFile, rootDir string, wc *WorkspaceContext) []string {
	clean := strings.Trim(importPath, "\"';`<>")
	if strings.HasPrefix(clean, ".") {
		dir := filepath.Dir(fromFile)
		target := filepath.Clean(filepath.Join(dir, clean))
		return []string{filepath.ToSlash(target)}
	}
	// Check explicit WorkspaceContext aliases
	if wc != nil {
		wc.mu.RLock()
		for alias, actualPath := range wc.Aliases {
			if strings.HasPrefix(clean, alias) {
				clean = strings.Replace(clean, alias, actualPath, 1)
				break
			}
		}
		wc.mu.RUnlock()
	}

	// aliases like @/components/...
	if strings.HasPrefix(clean, "@/") || strings.HasPrefix(clean, "~/") {
		clean = clean[2:]
		return []string{clean}
	}
	return []string{clean}
}
