package normalize

import (
	"sort"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/product/ids"
	"github.com/Syamchand123/GlassMarble/internal/product/ont"
	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/types"
)

// parseIDParts splits a node ID into (path, receiver, symbol). Both the
// legacy `path::symbol` dialect and the canonical scheme
// (type:path:owner:symbol, master_overhaul_plan.md §4.1) are accepted:
// legacy IDs are normalized onto the canonical grammar first
// (ids.NormalizeLegacyID is idempotent) and then parsed, so URL-encoded
// path segments come back decoded (GAP-C-02 / GAP-C-03).
func parseIDParts(id string) (path, receiver, symbol string) {
	norm := ids.NormalizeLegacyID(id)
	if c, err := ids.ParseCanonicalID(norm); err == nil {
		return c.Path, c.Owner, c.Symbol
	}
	parts := strings.Split(id, "::")
	switch len(parts) {
	case 3:
		return parts[0], parts[1], parts[2]
	case 2:
		return parts[0], "", parts[1]
	}
	return id, "", ""
}

// classIDCandidates returns the class-level IDs that could own a member with
// the given parsed path/receiver, covering the legacy `path::Receiver`
// dialect and the canonical `type:` / `method:` forms (GAP-C-02).
func classIDCandidates(path, rec string) []string {
	return []string{
		path + "::" + rec,
		ids.CanonicalID{Kind: ids.KindType, Path: path, Symbol: rec}.String(),
		ids.CanonicalID{Kind: ids.KindMethod, Path: path, Symbol: rec}.String(),
	}
}

// memberParentClassID resolves the class-level ID that structurally owns id
// (e.g. path::Receiver::Method → path::Receiver). Both ID dialects are
// tried so legacy and canonical graphs map members identically. Nodes
// without a receiver (path::Symbol) are treated as class-level themselves,
// preserving the legacy two-part self-mapping.
func memberParentClassID(id string, nodes map[string]*types.TTLNode) string {
	path, rec, sym := parseIDParts(id)
	if rec == "" {
		if sym != "" {
			return id
		}
		return ""
	}
	for _, candidate := range classIDCandidates(path, rec) {
		if _, ok := nodes[candidate]; ok {
			return candidate
		}
	}
	return ""
}

// ClassRelation represents an aggregated type-level relationship between two classes/types (V-03 / §7.1.3).
type ClassRelation struct {
	SourceClassID string
	TargetClassID string
	Predicate     string
	Kind          string // "hierarchy" | "composition" | "usage"
	Label         string
	Count         int
}

// EdgeProjection holds the aggregated, type-level edge relations for diagram rendering.
type EdgeProjection struct {
	EdgeCount      int
	ClassRelations []ClassRelation
}

// ClassifyEdges aggregates raw CPG edges into class-level relations with multiplicity (V-03 / §7.1.3).
// Same-file cross-class edges are preserved; self-loops (src == tgt) are eliminated.
// Results are deterministically sorted by Source, Target, and Predicate.
func ClassifyEdges(sub *types.VirtualSubgraph) *EdgeProjection {
	if sub == nil || len(sub.Nodes) == 0 {
		return &EdgeProjection{}
	}

	// 1. Build map from node ID to structural parent class ID
	nodeToClass := make(map[string]string)
	for id, node := range sub.Nodes {
		if isClassKind(node.Kind) {
			nodeToClass[id] = id
		}
	}
	// Map member and method nodes to their containing class ID if present in subgraph
	for id := range sub.Nodes {
		if _, ok := nodeToClass[id]; !ok {
			if parentID := memberParentClassID(id, sub.Nodes); parentID != "" {
				nodeToClass[id] = parentID
			}
		}
	}

	// 2. Aggregate edges by (srcClass, tgtClass, predicate)
	type relKey struct {
		src  string
		tgt  string
		pred string
	}
	counts := make(map[relKey]int)

	for _, edge := range sub.Edges {
		srcClass, okSrc := nodeToClass[edge.SourceID]
		tgtClass, okTgt := nodeToClass[edge.TargetID]
		if !okSrc || !okTgt {
			continue
		}
		// Self-loops on the exact same class box are dropped
		if srcClass == tgtClass {
			continue
		}

		key := relKey{src: srcClass, tgt: tgtClass, pred: edge.Predicate}
		counts[key]++
	}

	// 3. Build ClassRelations
	var relations []ClassRelation
	for k, count := range counts {
		kind := "usage"
		label := "uses"

		switch k.pred {
		case ont.PredInheritsFrom, ont.PredExtends, ont.PredImplements, ont.PredMixes:
			kind = "hierarchy"
			label = "extends"
		case ont.PredHasField, ont.PredComposes:
			kind = "composition"
			label = "has"
		case ont.PredCalls, ont.PredContextualCall:
			kind = "usage"
			label = "uses"
		default:
			label = strings.TrimPrefix(k.pred, ont.PrefixGM)
		}

		relations = append(relations, ClassRelation{
			SourceClassID: k.src,
			TargetClassID: k.tgt,
			Predicate:     k.pred,
			Kind:          kind,
			Label:         label,
			Count:         count,
		})
	}

	// 4. Deterministic sorting: Source -> Target -> Predicate
	sort.Slice(relations, func(i, j int) bool {
		if relations[i].SourceClassID != relations[j].SourceClassID {
			return relations[i].SourceClassID < relations[j].SourceClassID
		}
		if relations[i].TargetClassID != relations[j].TargetClassID {
			return relations[i].TargetClassID < relations[j].TargetClassID
		}
		return relations[i].Predicate < relations[j].Predicate
	})

	return &EdgeProjection{
		EdgeCount:      len(sub.Edges),
		ClassRelations: relations,
	}
}

func isClassKind(kind string) bool {
	k := strings.TrimPrefix(kind, ont.PrefixGM)
	switch k {
	case "Struct", "Class", "Interface", "TypeDecl", "STRUCT", "CLASS", "INTERFACE", "TYPE_DECL":
		return true
	default:
		return false
	}
}
