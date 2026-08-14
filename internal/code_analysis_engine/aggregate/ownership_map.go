package aggregate

import (
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/normalize"
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

	// v2 (master_overhaul_plan.md §5.3.1, fixes A-16): explicit ownership
	// backbone derived from the normalized GAST parent-child structure
	// (types → field/method children). Keyed by the member's primary
	// resolution key (canonical ID when present, else legacy FQN).
	OwnerOf   map[string]string   `json:"owner_of,omitempty"`   // member key → owning type key
	MembersOf map[string][]string `json:"members_of,omitempty"` // type key → member keys
}

func BuildOwnershipMap(globalIndex map[string][]*normalize.GASTNode, wc *WorkspaceContext) *OwnershipMap {
	om := &OwnershipMap{
		ByHierarchy: make(map[string]map[string]map[string][]SymbolEntry),
		ByName:      make(map[string][]SymbolEntry),
		ByImport:    make(map[string][]SymbolEntry),
		OwnerOf:     make(map[string]string),
		MembersOf:   make(map[string][]string),
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

			// v2 ownership backbone (§5.3.1, A-16): type nodes own their
			// field and method children (method re-parenting happens in the
			// normalizer). Same-file lookup via primary key.
			if node.Type == normalize.GASTTypeDeclaration {
				typeKey := resolutionKey(node)
				for _, child := range node.Children {
					if child == nil {
						continue
					}
					if child.Type != normalize.GASTField && !(child.Type == normalize.GASTFunction && child.Kind == "method") {
						continue
					}
					memberKey := resolutionKey(child)
					if memberKey == "" {
						continue
					}
					if _, exists := om.OwnerOf[memberKey]; !exists {
						om.OwnerOf[memberKey] = typeKey
					}
					om.MembersOf[typeKey] = appendUnique(om.MembersOf[typeKey], memberKey)
				}
			}
		}
	}

	return om
}

// GetOwner returns the owning type key for a member key ("" when unknown).
func (om *OwnershipMap) GetOwner(id string) string {
	if om == nil || om.OwnerOf == nil {
		return ""
	}
	return om.OwnerOf[id]
}

// GetMembers returns the member keys owned by a type key (nil when unknown).
func (om *OwnershipMap) GetMembers(typeID string) []string {
	if om == nil || om.MembersOf == nil {
		return nil
	}
	return om.MembersOf[typeID]
}

// resolutionKey returns the primary key of a GAST node for ownership /
// definition-index lookups: the canonical ID (Phase 0 ids package) when
// present, else the legacy dotted FQN (§5.3.1).
func resolutionKey(node *normalize.GASTNode) string {
	if node == nil || node.Properties == nil {
		return ""
	}
	if cid := node.Properties["canonical_id"]; cid != "" {
		return cid
	}
	if fqn := node.Properties["fully_qualified_name"]; fqn != "" {
		return fqn
	}
	return node.Name
}

func appendUnique(xs []string, s string) []string {
	for _, x := range xs {
		if x == s {
			return xs
		}
	}
	return append(xs, s)
}
