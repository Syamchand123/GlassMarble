package stage4

import (
	"github.com/Syamchand123/GlassMarble/internal/product/ont"
	"strings"
)

// QualityMetrics summarizes graph health for the end-of-analyze report
// (AUDIT Issue 1 Phase 1C-10 and Issue 5 plan item 3).
type QualityMetrics struct {
	TotalNodes    int
	TotalEdges    int
	DanglingEdges int
	VirtualNodes  int
	Unresolved    int
}

// virtualIDPrefixes identify synthetic nodes that carry no direct source
// symbol (used to surface heuristic noise in reports).
var virtualIDPrefixes = []string{
	"VIRTUAL_", "thread_or_coroutine", "TAINT:", "QUEUE::", "topic::",
	"event:", "endpoint:", "sink:", "resource:", "global:", "memory::",
	"alloc::", ont.PrefixExt, "DATABASE::", "CLOUD_API::",
}

// MeasureQuality audits a linked CPG. Dangling edges are edges whose source
// or target is missing from GraphNodes; virtual nodes are synthetic
// heuristic nodes.
func MeasureQuality(cpg *Stage4Output) QualityMetrics {
	q := QualityMetrics{
		TotalNodes: len(cpg.GraphNodes),
	}
	for _, edges := range cpg.OutboundEdges {
		for _, e := range edges {
			q.TotalEdges++
			_, srcOK := cpg.GraphNodes[e.SourceID]
			_, tgtOK := cpg.GraphNodes[e.TargetID]
			if !srcOK || !tgtOK {
				q.DanglingEdges++
			}
		}
	}
	for id, node := range cpg.GraphNodes {
		if IsVirtualID(id) || strings.HasPrefix(node.Kind, "VIRTUAL_") {
			q.VirtualNodes++
		}
	}
	return q
}

// IsVirtualID reports whether a node ID matches a known synthetic/virtual
// prefix. Exported for the `gmb status` virtual-share report
// (AUDIT Issue 5 Phase 5B-5).
func IsVirtualID(id string) bool {
	for _, p := range virtualIDPrefixes {
		if strings.HasPrefix(id, p) {
			return true
		}
	}
	return false
}
