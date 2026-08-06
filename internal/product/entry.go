package product

import (
	"fmt"
	"sort"
	"strings"

	producterrs "github.com/Syamchand123/GlassMarble/internal/product/errors"
	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/types"
)

// ResolveEntryPoint determines the effective entry node ID from graph and QueryOptions (W5-02 / §9.2).
func ResolveEntryPoint(graph *types.VirtualSubgraph, opts types.QueryOptions) (string, error) {
	if opts.EntryPointID != "" {
		ep := opts.EntryPointID
		if strings.HasPrefix(ep, "symbol:") {
			ep = strings.TrimPrefix(ep, "symbol:")
		} else if strings.HasPrefix(ep, "file:") {
			ep = strings.TrimPrefix(ep, "file:")
		} else if strings.HasPrefix(ep, "module:") {
			ep = strings.TrimPrefix(ep, "module:")
		}

		// Exact match
		if _, ok := graph.Nodes[ep]; ok {
			return ep, nil
		}

		// Soft match by node Name or suffix
		var matches []string
		for id, node := range graph.Nodes {
			if node.Name == ep || strings.HasSuffix(id, ep) {
				matches = append(matches, id)
			}
		}
		if len(matches) == 1 {
			return matches[0], nil
		}
		if len(matches) > 1 {
			sort.Strings(matches)
			return "", producterrs.Annotate(fmt.Errorf("ambiguous entry symbol %q matching multiple candidates: %s", ep, strings.Join(matches, ", ")), producterrs.ErrValidation)
		}
		return "", producterrs.Annotate(fmt.Errorf("entry symbol %q not found in architecture graph", ep), producterrs.ErrEntryMissing)
	}

	// Auto-entry discovery for entry-required diagrams
	var candidates []string
	for id, node := range graph.Nodes {
		nameLower := strings.ToLower(node.Name)
		if node.IsEntrypoint || nameLower == "main" || nameLower == "run" || strings.HasSuffix(id, "::main") {
			candidates = append(candidates, id)
		}
	}

	if len(candidates) == 1 {
		return candidates[0], nil
	}

	if len(candidates) > 1 {
		sort.Strings(candidates)
		// Pick main.go::main if present
		for _, c := range candidates {
			if strings.Contains(c, "main.go") || strings.HasSuffix(c, "::main") {
				return c, nil
			}
		}
		return candidates[0], nil
	}

	// Default fallback to first node if graph is non-empty
	if len(graph.Nodes) > 0 {
		var keys []string
		for id := range graph.Nodes {
			keys = append(keys, id)
		}
		sort.Strings(keys)
		return keys[0], nil
	}

	return "", producterrs.Annotate(fmt.Errorf("no suitable entry point candidate discovered"), producterrs.ErrEntryMissing)
}
