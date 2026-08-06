package stage2

import (
	"sort"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/product/ont"
	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/types"
)

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
			parts := strings.Split(id, "::")
			if len(parts) >= 2 {
				parentID := parts[0] + "::" + parts[1]
				if _, ok := sub.Nodes[parentID]; ok {
					nodeToClass[id] = parentID
				}
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
