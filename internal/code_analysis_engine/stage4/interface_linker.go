package stage4

import (
	"hash/fnv"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage2"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage3"
)

// LinkInterfacesAndRealizations processes explicit and implicit duck-typing interface implementations.
func LinkInterfacesAndRealizations(stage3Out *stage3.Stage3Output, cpg *Stage4Output) {
	if stage3Out == nil || cpg == nil {
		return
	}

	// 1. Collect all INTERFACE nodes and their required methods
	interfaces := collectInterfaceNodes(cpg)
	if len(interfaces) == 0 {
		return
	}

	// 2. Collect all STRUCT / CLASS nodes and their defined methods
	structs := collectStructNodes(cpg)

	// 3. Compare methods to bind IMPLEMENTS edges (Explicit & Implicit Duck-Typing)
	for _, iface := range interfaces {
		ifaceMethods := getInterfaceRequiredMethods(iface, stage3Out.GlobalDefinitionIndex, cpg)
		if len(ifaceMethods) == 0 {
			continue
		}

		ifaceBits := computeMethodBitset(ifaceMethods)

		for _, strct := range structs {
			// CRITICAL: We only want to generate an edge if at least one of them was modified!
			// If both are unmodified, their edges already exist in the AKG.
			isIfaceModified := cpg.ModifiedFiles[stage3.NormalizeRelativePath(iface.FileSpec.Path)]
			isStructModified := cpg.ModifiedFiles[stage3.NormalizeRelativePath(strct.FileSpec.Path)]
			
			if !isIfaceModified && !isStructModified {
				continue
			}

			structMethods := getStructDefinedMethods(strct, stage3Out.GlobalDefinitionIndex, cpg)
			structBits := computeMethodBitset(structMethods)

			// Step 4.3: Bitset Signatures for Lightning-fast O(1) Rejection
			if (structBits & ifaceBits) == ifaceBits {
				// Bloom filter passed, do exact subset match to avoid false positives
				if implementsAllMethods(ifaceMethods, structMethods) {
					cpg.AddEdge(strct.ID, iface.ID, EdgeImplements, strct.FileSpec.LineStart)
				}
			}
		}
	}
}

func computeMethodBitset(methods map[string]string) uint64 {
	var bitset uint64
	for name := range methods {
		h := fnv.New64a()
		h.Write([]byte(name))
		bitIndex := h.Sum64() % 64
		bitset |= (1 << bitIndex)
	}
	return bitset
}

func collectInterfaceNodes(cpg *Stage4Output) []*ResolvedNode {
	var list []*ResolvedNode
	seen := make(map[string]bool)

	// Local Delta Nodes
	for _, node := range cpg.GraphNodes {
		if node.Kind == "INTERFACE" {
			list = append(list, node)
			seen[node.ID] = true
		}
	}

	// Global DB Nodes
	if cpg.db != nil {
		globalIfaces := cpg.db.GetNodesByKind("INTERFACE")
		for _, node := range globalIfaces {
			if !seen[node.ID] {
				list = append(list, node)
			}
		}
	}

	return list
}

func collectStructNodes(cpg *Stage4Output) []*ResolvedNode {
	var list []*ResolvedNode
	seen := make(map[string]bool)

	// Local Delta Nodes
	for _, node := range cpg.GraphNodes {
		if node.Kind == "STRUCT" || node.Kind == "CLASS" {
			list = append(list, node)
			seen[node.ID] = true
		}
	}

	// Global DB Nodes
	if cpg.db != nil {
		globalStructs := cpg.db.GetNodesByKind("STRUCT")
		for _, node := range globalStructs {
			if !seen[node.ID] {
				list = append(list, node)
			}
		}
		globalClasses := cpg.db.GetNodesByKind("CLASS")
		for _, node := range globalClasses {
			if !seen[node.ID] {
				list = append(list, node)
			}
		}
	}

	return list
}

func getInterfaceRequiredMethods(iface *ResolvedNode, globalIndex map[string][]*stage2.GASTNode, cpg *Stage4Output) map[string]string {
	methods := make(map[string]string)

	prefix := iface.ID + "::"
	for nodeID, node := range cpg.GraphNodes {
		if strings.HasPrefix(nodeID, prefix) && (node.Kind == "METHOD" || node.Kind == "FUNCTION") {
			methods[node.Name] = node.Primitive
		}
	}

	// Fallback to GASTNode children
	if len(methods) == 0 {
		for fqn, gastNodes := range globalIndex {
			if strings.Contains(fqn, iface.Name) || iface.ID == fqn {
				for _, gastNode := range gastNodes {
					for _, child := range gastNode.Children {
						if child.Type == stage2.GASTFunction || child.Kind == "method" {
							methods[child.Name] = child.DataType
						}
					}
				}
			}
		}
	}

	return methods
}

func getStructDefinedMethods(strct *ResolvedNode, globalIndex map[string][]*stage2.GASTNode, cpg *Stage4Output) map[string]string {
	methods := make(map[string]string)

	prefix := strct.ID + "::"
	for nodeID, node := range cpg.GraphNodes {
		if strings.HasPrefix(nodeID, prefix) || node.Properties["receiver_type"] == strct.Name {
			methods[node.Name] = node.Primitive
		}
	}

	// Fallback to GASTNode children
	if len(methods) == 0 {
		for fqn, gastNodes := range globalIndex {
			if strings.Contains(fqn, strct.Name) || strct.ID == fqn {
				for _, gastNode := range gastNodes {
					for _, child := range gastNode.Children {
						if child.Type == stage2.GASTFunction || child.Kind == "method" {
							methods[child.Name] = child.DataType
						}
					}
				}
			}
		}
	}

	return methods
}

func implementsAllMethods(ifaceMethods, structMethods map[string]string) bool {
	for name := range ifaceMethods {
		if _, exists := structMethods[name]; !exists {
			return false
		}
	}
	return true
}
