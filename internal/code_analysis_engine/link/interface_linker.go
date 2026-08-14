package link

import (
	"hash/fnv"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/normalize"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/aggregate"
	"github.com/Syamchand123/GlassMarble/internal/product/ont"
)

// LinkInterfacesAndRealizations processes explicit and implicit duck-typing
// interface implementations (master_overhaul_plan.md §5.4.2 / W1-13).
//
// v2 changes:
//   - A-05: the both-unmodified skip is gated on a full rebuild
//     (ModifiedFiles empty ⇒ always run); previously a full rebuild skipped
//     every pair because the ModifiedFiles lookup always missed.
//   - A-15: interface/struct methods come from exact global-index membership
//     (name key + file path match), never strings.Contains FQN scans.
//   - Signature-match primary: a method satisfies the interface method when
//     the normalized GAST signatures agree; name-only matching is the
//     fallback when either signature is empty (A-17).
func LinkInterfacesAndRealizations(aggregateOut *aggregate.AggregateOutput, cpg *LinkOutput) {
	if aggregateOut == nil || cpg == nil {
		return
	}

	// 1. Collect all INTERFACE nodes and their required methods
	interfaces := collectInterfaceNodes(cpg)
	if len(interfaces) == 0 {
		return
	}

	// 2. Collect all STRUCT / CLASS nodes and their defined methods
	structs := collectStructNodes(cpg)

	// A-05: full rebuilds (ModifiedFiles empty) always run the pass;
	// incremental deltas skip pairs where neither side changed.
	isFullRebuild := len(cpg.ModifiedFiles) == 0

	// 3. Compare methods to bind IMPLEMENTS edges (Explicit & Implicit Duck-Typing)
	for _, iface := range interfaces {
		ifaceMethods := getInterfaceRequiredMethods(iface, aggregateOut.GlobalDefinitionIndex, cpg)
		if len(ifaceMethods) == 0 {
			continue
		}

		ifaceBits := computeMethodBitset(ifaceMethods)

		for _, strct := range structs {
			// A-05: OLD (bug) skipped when BOTH were unmodified AND on full
			// rebuilds (empty ModifiedFiles ⇒ always "unmodified").
			isIfaceModified := cpg.ModifiedFiles[aggregate.NormalizeRelativePath(iface.FileSpec.Path)]
			isStructModified := cpg.ModifiedFiles[aggregate.NormalizeRelativePath(strct.FileSpec.Path)]

			if !isFullRebuild && !isIfaceModified && !isStructModified {
				continue
			}

			structMethods := getStructDefinedMethods(strct, aggregateOut.GlobalDefinitionIndex, cpg)
			structBits := computeMethodBitset(structMethods)

			// Step 4.3: Bitset Signatures for Lightning-fast O(1) Rejection
			if (structBits & ifaceBits) == ifaceBits {
				// Bloom filter passed, do exact subset match to avoid false positives
				if matched, provenance := implementsAllMethods(ifaceMethods, structMethods); matched {
					// §5.4.7/W1-14: evidence kind rides the edge —
					// "signature-match" (A-17) or "name-match" fallback.
					cpg.AddEdgeProperties(strct.ID, iface.ID, EdgeImplements, strct.FileSpec.LineStart, 1.0,
						map[string]string{ont.PredProvenance: provenance})
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

func collectInterfaceNodes(cpg *LinkOutput) []*ResolvedNode {
	var list []*ResolvedNode
	seen := make(map[string]bool)

	// Local Delta Nodes
	for _, node := range cpg.GraphNodes {
		if node.Kind == "INTERFACE" {
			list = append(list, node)
			seen[node.ID] = true
		}
	}

	// Base Nodes (initial nodes from BuildInitialNodes)
	for _, node := range cpg.baseNodes {
		if node.Kind == "INTERFACE" && !seen[node.ID] {
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

func collectStructNodes(cpg *LinkOutput) []*ResolvedNode {
	var list []*ResolvedNode
	seen := make(map[string]bool)

	// Local Delta Nodes
	for _, node := range cpg.GraphNodes {
		if node.Kind == "STRUCT" || node.Kind == "CLASS" {
			list = append(list, node)
			seen[node.ID] = true
		}
	}

	// Base Nodes (initial nodes from BuildInitialNodes)
	for _, node := range cpg.baseNodes {
		if (node.Kind == "STRUCT" || node.Kind == "CLASS") && !seen[node.ID] {
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

// gastTypeMembers returns the GASTFunction children (Kind "method") of the
// GAST node(s) exactly matching (name, file path) in the global index —
// A-15: exact canonical membership, no fuzzy FQN scans. Values are the
// normalized GAST signatures (§5.2.3) used for signature-match (A-17).
func gastTypeMembers(name, filePath string, globalIndex map[string][]*normalize.GASTNode) map[string]string {
	methods := make(map[string]string)
	nodes := globalIndex[name]
	if len(nodes) == 0 {
		return methods
	}
	for _, gastNode := range nodes {
		if gastNode == nil || gastNode.Type != normalize.GASTTypeDeclaration {
			continue
		}
		if filePath != "" && aggregate.NormalizeRelativePath(gastNode.Properties["file_path"]) != aggregate.NormalizeRelativePath(filePath) {
			continue
		}
		for _, child := range gastNode.Children {
			if child == nil || child.Type != normalize.GASTFunction || child.Kind != "method" {
				continue
			}
			if _, dup := methods[child.Name]; !dup {
				methods[child.Name] = child.Signature
			}
		}
	}
	return methods
}

func getInterfaceRequiredMethods(iface *ResolvedNode, globalIndex map[string][]*normalize.GASTNode, cpg *LinkOutput) map[string]string {
	// v2 (A-15): exact global-index membership first (canonical signatures).
	if methods := gastTypeMembers(iface.Name, iface.FileSpec.Path, globalIndex); len(methods) > 0 {
		return methods
	}

	// Fallback: CPG children of the interface node (DB nodes etc.).
	methods := make(map[string]string)
	prefix := iface.ID + "::"
	for nodeID, node := range cpg.GraphNodes {
		if strings.HasPrefix(nodeID, prefix) && (node.Kind == "METHOD" || node.Kind == "FUNCTION") {
			methods[node.Name] = node.Primitive
		}
	}
	return methods
}

func getStructDefinedMethods(strct *ResolvedNode, globalIndex map[string][]*normalize.GASTNode, cpg *LinkOutput) map[string]string {
	// v2 (A-15): exact global-index membership first (canonical signatures).
	rawMethods := gastTypeMembers(strct.Name, strct.FileSpec.Path, globalIndex)
	if len(rawMethods) > 0 {
		// Normalize method names by stripping receiver prefix (e.g., "Robot.Run" -> "Run")
		methods := make(map[string]string)
		for name, sig := range rawMethods {
			if idx := strings.LastIndex(name, "."); idx != -1 {
				name = name[idx+1:]
			}
			methods[name] = sig
		}
		return methods
	}

	// Fallback: CPG children of the struct node (DB nodes etc.).
	methods := make(map[string]string)
	prefix := strct.ID + "::"
	for nodeID, node := range cpg.GraphNodes {
		if strings.HasPrefix(nodeID, prefix) || node.Properties["receiver_type"] == strct.Name {
			name := node.Name
			if idx := strings.LastIndex(name, "."); idx != -1 {
				name = name[idx+1:]
			}
			methods[name] = node.Primitive
		}
	}
	return methods
}

// implementsAllMethods checks every interface method against the struct's
// methods: signature-match primary (both normalized signatures present),
// name-only match as fallback (A-17). The second return is the gm:provenance
// evidence kind (§5.4.7): "signature-match" when every comparison used
// signatures, "name-match" when any fallback fired.
func implementsAllMethods(ifaceMethods, structMethods map[string]string) (bool, string) {
	usedNameFallback := false
	for name, ifaceSig := range ifaceMethods {
		structSig, exists := structMethods[name]
		if !exists {
			return false, ""
		}
		if ifaceSig == "" || structSig == "" {
			// Either side lacks a normalized signature: name match suffices.
			usedNameFallback = true
			continue
		}
		if !signaturesEqual(ifaceSig, structSig) {
			return false, ""
		}
	}
	if usedNameFallback {
		return true, "name-match"
	}
	return true, "signature-match"
}

// signaturesEqual compares normalized signature texts insensitive to
// whitespace (the Signature.Text is already name(paramTypes) ret).
func signaturesEqual(a, b string) bool {
	norm := func(s string) string {
		return strings.Join(strings.Fields(s), " ")
	}
	return norm(a) == norm(b)
}
