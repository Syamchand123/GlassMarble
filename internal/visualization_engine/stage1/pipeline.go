package stage1

import "github.com/Syamchand123/GlassMarble/internal/visualization_engine/types"



// ExtractFromSubgraph extracts a subgraph from a full graph using the given extraction config and options.
func ExtractFromSubgraph(full *types.VirtualSubgraph, cfg types.ExtractionConfig, opts types.QueryOptions) *types.VirtualSubgraph {
	opts.IncludeUnused = opts.IncludeUnused || cfg.IncludeUnused
	return extractWithConfig(full.Nodes, full.Edges, cfg, opts)
}
