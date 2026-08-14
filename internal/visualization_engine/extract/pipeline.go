package ingest

import "github.com/Syamchand123/GlassMarble/internal/visualization_engine/types"

// ExtractFromSubgraph extracts a subgraph from a full graph using the given extraction config and options,
// returning the extracted VirtualSubgraph and the merged effective QueryOptions (W3-01 / §7.1.1).
func ExtractFromSubgraph(full *types.VirtualSubgraph, cfg types.ExtractionConfig, opts types.QueryOptions) (*types.VirtualSubgraph, types.QueryOptions, error) {
	opts.IncludeUnused = opts.IncludeUnused || cfg.IncludeUnused
	sub, err := extractWithConfig(full.Nodes, full.Edges, cfg, opts)
	return sub, opts, err
}
