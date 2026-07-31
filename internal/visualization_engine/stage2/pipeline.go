package stage2

import (
	"fmt"

	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/types"
)

// NewStageMetrics computes all metrics for a subgraph (convenience wrapper around ComputeAllMetrics).
func NewStageMetrics(sub *types.VirtualSubgraph, opts types.QueryOptions) *DiagramMetrics {
	return ComputeAllMetrics(sub)
}

// BuildFromSubgraph computes metrics and builds a layout tree from a subgraph (one-stop pipeline helper).
func BuildFromSubgraph(sub *types.VirtualSubgraph, opts types.QueryOptions, t types.DiagramType) *types.LayoutTree {
	metrics := ComputeAllMetrics(sub)
	clustering := metrics.Communities
	return BuildLayoutTreeEx(sub, metrics, clustering, opts, t)
}

func StageMetricsToString(metrics *DiagramMetrics) string {
	if metrics == nil {
		return "no metrics"
	}
	s := metrics.Summary
	if s == nil {
		return fmt.Sprintf("PageRank=%d, Betweenness=%d, Degrees=%d, GodObjects=%d, KCore=%d, SCCs=%d",
			len(metrics.PageRank), len(metrics.Betweenness),
			len(metrics.DegreeIn), len(metrics.GodObjects),
			len(metrics.KCore), len(metrics.SCCs))
	}
	return fmt.Sprintf("nodes=%d edges=%d density=%.4f diameter=%d avgpath=%.2f clusters=%d godobjects=%d",
		s.NodeCount, s.EdgeCount, s.Density, s.Diameter,
		s.AvgPathLength, s.ClusterCount, s.GodObjectCount)
}
