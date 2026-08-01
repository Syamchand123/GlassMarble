package stage3

import (
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage2"
)

type SymbolEntry struct {
	Name         string `json:"name"`
	FQN          string `json:"fqn"`
	Kind         string `json:"kind"`
	FilePath     string `json:"file_path"`
	ReceiverType string `json:"receiver_type,omitempty"`
}

// OwnershipMap maps symbols hierarchically to prevent massive collisions in enterprise codebases.
// Hierarchy: Module -> Package -> File -> Symbols
type OwnershipMap struct {
	// ByHierarchy is map[Module]map[Package]map[File][]SymbolEntry
	ByHierarchy map[string]map[string]map[string][]SymbolEntry `json:"by_hierarchy"`
	ByName      map[string][]SymbolEntry                       `json:"by_name"`
	ByImport    map[string][]SymbolEntry                       `json:"by_import"`
}

func BuildOwnershipMap(globalIndex map[string][]*stage2.GASTNode, wc *WorkspaceContext) *OwnershipMap {
	om := &OwnershipMap{
		ByHierarchy: make(map[string]map[string]map[string][]SymbolEntry),
		ByName:      make(map[string][]SymbolEntry),
		ByImport:    make(map[string][]SymbolEntry),
	}

	if globalIndex == nil {
		return om
	}

	for fqn, nodes := range globalIndex {
		for _, node := range nodes {
			if node == nil {
				continue
			}
			filePath := node.Properties["file_path"]
			entry := SymbolEntry{
				Name:         node.Name,
				FQN:          fqn,
				Kind:         node.Kind,
				FilePath:     filePath,
				ReceiverType: node.ReceiverType,
			}

			// 1. Module Level
			moduleBoundary := wc.GetModuleBoundary(filePath)
			if moduleBoundary == "" {
				moduleBoundary = "root"
			}

			// 2. Package Level
			pkg := node.Namespace
			if pkg == "" {
				// infer package from file path if empty
				dirs, _ := SplitPathToDirectories(filePath)
				if len(dirs) > 0 {
					pkg = strings.Join(dirs, "/")
				} else {
					pkg = "main"
				}
			}

			// Initialize Maps
			if om.ByHierarchy[moduleBoundary] == nil {
				om.ByHierarchy[moduleBoundary] = make(map[string]map[string][]SymbolEntry)
			}
			if om.ByHierarchy[moduleBoundary][pkg] == nil {
				om.ByHierarchy[moduleBoundary][pkg] = make(map[string][]SymbolEntry)
			}
			om.ByHierarchy[moduleBoundary][pkg][filePath] = append(om.ByHierarchy[moduleBoundary][pkg][filePath], entry)

			// Flat maps for fast lookup
			om.ByName[node.Name] = append(om.ByName[node.Name], entry)
			if node.Namespace != "" {
				om.ByImport[node.Namespace] = append(om.ByImport[node.Namespace], entry)
			}
		}
	}

	return om
}
