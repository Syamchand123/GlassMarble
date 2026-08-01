package akg

import (
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage4"
)

// MeasureGraphQuality audits the persisted (merged) graph, not a delta
// payload. A delta view reports edges to base-graph nodes as dangling — a
// false positive for the end-of-analyze report (AUDIT Issue 5 Phase 5A-3).
// Used by `gmb analyze` after commit and kept in sync with
// stage4.MeasureQuality.
func MeasureGraphQuality(graph *CodePropertyGraph) stage4.QualityMetrics {
	q := stage4.QualityMetrics{
		TotalNodes: graph.Nodes.Len(),
	}
	graph.OutboundEdges.Iterate(func(sourceID string, edges []stage4.ResolvedEdge) {
		for _, e := range edges {
			q.TotalEdges++
			if _, ok := graph.Nodes.Get(sourceID); !ok {
				q.DanglingEdges++
			}
			if _, ok := graph.Nodes.Get(e.TargetID); !ok {
				q.DanglingEdges++
			}
		}
	})
	graph.Nodes.Iterate(func(id string, node *stage4.ResolvedNode) {
		if stage4.IsVirtualID(id) || strings.HasPrefix(node.Kind, "VIRTUAL_") {
			q.VirtualNodes++
		}
	})
	return q
}
