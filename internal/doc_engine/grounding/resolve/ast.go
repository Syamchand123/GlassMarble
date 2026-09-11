package resolve

import (
	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/link"
)

// astResolve is the universal fallback: it resolves from the passed AKG
// graph by exact FQN (Nodes.Get) then short-name suffix match. Every input
// FQN gets an entry — hits carry Provenance "ast", misses carry
// "unresolved" (never omitted) so callers can distinguish.
func astResolve(fqns []string, graph *akg.CodePropertyGraph) map[string]Resolution {
	out := make(map[string]Resolution, len(fqns))
	if graph == nil || graph.Nodes == nil {
		for _, fqn := range fqns {
			out[fqn] = Resolution{FQN: fqn, Provenance: ProvenanceUnresolved}
		}
		return out
	}
	for _, fqn := range fqns {
		if n, ok := graph.Nodes.Get(fqn); ok && n != nil {
			out[fqn] = nodeResolution(fqn, n)
			continue
		}
		if best := suffixMatch(graph, shortName(fqn)); best != nil {
			out[fqn] = nodeResolution(fqn, best)
			continue
		}
		out[fqn] = Resolution{FQN: fqn, Provenance: ProvenanceUnresolved}
	}
	return out
}

// suffixMatch returns the node whose ID shares the query's short name,
// breaking ties by smallest node ID so results are deterministic
// regardless of map iteration order.
func suffixMatch(graph *akg.CodePropertyGraph, short string) *link.ResolvedNode {
	if short == "" {
		return nil
	}
	var best *link.ResolvedNode
	bestID := ""
	graph.Nodes.Iterate(func(id string, n *link.ResolvedNode) {
		if n == nil || shortName(id) != short {
			return
		}
		if best == nil || id < bestID {
			best, bestID = n, id
		}
	})
	return best
}

// nodeResolution maps an AKG node to an AST-provenance resolution.
func nodeResolution(fqn string, n *link.ResolvedNode) Resolution {
	return Resolution{
		FQN:        fqn,
		File:       n.FileSpec.Path,
		Line:       n.FileSpec.LineStart,
		EndLine:    n.FileSpec.LineEnd,
		Provenance: ProvenanceAST,
	}
}
