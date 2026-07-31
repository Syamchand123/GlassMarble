package stage3

import (
	"encoding/json"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type WorkspaceContext struct {
	mu               sync.RWMutex
	Aliases          map[string]string // e.g., "@mono-auth/auth-lib" -> "packages/auth-lib/src"
	ModulePrefix     string            // e.g., "github.com/org/repo"
	ModuleBoundaries []string            // List of folder paths that mark roots of inner modules
}

// NewWorkspaceContext creates a new workspace context.
func NewWorkspaceContext() *WorkspaceContext {
	return &WorkspaceContext{
		Aliases:          make(map[string]string),
		ModuleBoundaries: make([]string, 0),
	}
}

// ScanWorkspace looks for monorepo configuration files.
func (wc *WorkspaceContext) ScanWorkspace(rootDir string) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("stage3: ScanWorkspace panicked: %v", r)
		}
	}()

	wc.mu.Lock()
	defer wc.mu.Unlock()

	// 1. Check for Go module
	goModPath := filepath.Join(rootDir, "go.mod")
	if data, err := os.ReadFile(goModPath); err == nil {
		lines := strings.Split(string(data), "\n")
		for _, line := range lines {
			if strings.HasPrefix(line, "module ") {
				wc.ModulePrefix = strings.TrimSpace(strings.TrimPrefix(line, "module "))
				break
			}
		}
	}

	// 2. Check for tsconfig.json (paths)
	tsConfigPath := filepath.Join(rootDir, "tsconfig.json")
	if data, err := os.ReadFile(tsConfigPath); err == nil {
		var tsConfig struct {
			CompilerOptions struct {
				Paths map[string][]string `json:"paths"`
			} `json:"compilerOptions"`
		}
		if err := json.Unmarshal(data, &tsConfig); err == nil {
			for alias, paths := range tsConfig.CompilerOptions.Paths {
				if len(paths) > 0 {
					cleanAlias := strings.TrimSuffix(alias, "/*")
					cleanPath := strings.TrimSuffix(paths[0], "/*")
					wc.Aliases[cleanAlias] = cleanPath
				}
			}
		}
	}

	// 3. Scan for Go workspaces (go.work)
	goWorkPath := filepath.Join(rootDir, "go.work")
	if data, err := os.ReadFile(goWorkPath); err == nil {
		lines := strings.Split(string(data), "\n")
		inUseBlock := false
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "use (" {
				inUseBlock = true
				continue
			}
			if line == ")" {
				inUseBlock = false
				continue
			}
			if inUseBlock || strings.HasPrefix(line, "use ") {
				path := strings.TrimPrefix(line, "use ")
				path = strings.TrimSpace(path)
				if path != "" && path != "." {
					wc.ModuleBoundaries = append(wc.ModuleBoundaries, NormalizeRelativePath(path))
				}
			}
		}
	}

	// 4. Scan for Cargo Workspaces (Cargo.toml)
	cargoPath := filepath.Join(rootDir, "Cargo.toml")
	if data, err := os.ReadFile(cargoPath); err == nil {
		lines := strings.Split(string(data), "\n")
		inWorkspace := false
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "[workspace]" {
				inWorkspace = true
				continue
			}
			if strings.HasPrefix(line, "[") && inWorkspace {
				inWorkspace = false // new section
			}
			if inWorkspace && strings.HasPrefix(line, "members =") {
				// Parse array
				start := strings.Index(line, "[")
				end := strings.Index(line, "]")
				if start != -1 && end != -1 {
					membersStr := line[start+1 : end]
					members := strings.Split(membersStr, ",")
					for _, m := range members {
						m = strings.Trim(strings.TrimSpace(m), "\"'")
						if m != "" {
							// For globs, we would ideally expand them. For now, store the literal pattern base.
							m = strings.ReplaceAll(m, "/*", "")
							wc.ModuleBoundaries = append(wc.ModuleBoundaries, NormalizeRelativePath(m))
						}
					}
				}
			}
		}
	}
	
	// 5. Scan for Lerna/Pnpm (lerna.json, pnpm-workspace.yaml)
	// Simplified detection by looking for inner package.json files
	_ = filepath.WalkDir(rootDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && (d.Name() == "node_modules" || d.Name() == ".git") {
			return filepath.SkipDir
		}
		if d.Name() == "package.json" && path != filepath.Join(rootDir, "package.json") {
			rel, _ := filepath.Rel(rootDir, filepath.Dir(path))
			if rel != "." && rel != "" {
				wc.ModuleBoundaries = append(wc.ModuleBoundaries, NormalizeRelativePath(rel))
			}
		}
		if d.Name() == "go.mod" && path != goModPath {
			rel, _ := filepath.Rel(rootDir, filepath.Dir(path))
			if rel != "." && rel != "" {
				wc.ModuleBoundaries = append(wc.ModuleBoundaries, NormalizeRelativePath(rel))
			}
		}
		return nil
	})
}

// GetModuleBoundary returns the deepest known module boundary folder for a given file.
func (wc *WorkspaceContext) GetModuleBoundary(fileRelPath string) string {
	wc.mu.RLock()
	defer wc.mu.RUnlock()

	normPath := NormalizeRelativePath(fileRelPath)
	bestBoundary := ""
	
	for _, boundary := range wc.ModuleBoundaries {
		// If file is inside this boundary
		if strings.HasPrefix(normPath, boundary+"/") || normPath == boundary {
			if len(boundary) > len(bestBoundary) {
				bestBoundary = boundary
			}
		}
	}
	return bestBoundary
}
