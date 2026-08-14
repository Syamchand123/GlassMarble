package akg

import (
	"sort"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/link"
)

// GraphDiff summarizes the structural delta between two AKG snapshots (base vs
// head). Used by `gmb compare` and the CI PR-comment workflow to surface what a
// change set did to the architecture.
type GraphDiff struct {
	// Base and head snapshot identifiers.
	BaseCommit string `json:"base_commit"`
	HeadCommit string `json:"head_commit"`
	// Node additions/removals by ID.
	NodesAdded   []DiffNode `json:"nodes_added"`
	NodesRemoved []DiffNode `json:"nodes_removed"`
	// Edge additions/removals keyed by (type, source, target).
	EdgesAdded   []DiffEdge `json:"edges_added"`
	EdgesRemoved []DiffEdge `json:"edges_removed"`
	// Files touched (union of node file paths) so the comment can name them.
	FilesChanged []string `json:"files_changed"`
}

// DiffNode carries a node's identity and kind for a diff entry.
type DiffNode struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
	Name string `json:"name"`
	File string `json:"file,omitempty"`
}

// DiffEdge is a structural edge difference.
type DiffEdge struct {
	Type     string `json:"type"`
	SourceID string `json:"source_id"`
	TargetID string `json:"target_id"`
	Line     int    `json:"line,omitempty"`
}

// edgeKey uniquely identifies an edge within a snapshot for set comparison.
func edgeKey(e link.ResolvedEdge) string {
	return string(e.Type) + "\x00" + e.SourceID + "\x00" + e.TargetID
}

// DiffGraphs compares two CodePropertyGraph snapshots and returns a structural
// diff. Order is deterministic for stable CI output.
func DiffGraphs(base, head *CodePropertyGraph) *GraphDiff {
	diff := &GraphDiff{}
	if base != nil {
		diff.BaseCommit = base.CommitHash
	}
	if head != nil {
		diff.HeadCommit = head.CommitHash
	}

	baseNodes := map[string]*link.ResolvedNode{}
	if base != nil && base.Nodes != nil {
		base.Nodes.Iterate(func(id string, n *link.ResolvedNode) { baseNodes[id] = n })
	}
	headNodes := map[string]*link.ResolvedNode{}
	if head != nil && head.Nodes != nil {
		head.Nodes.Iterate(func(id string, n *link.ResolvedNode) { headNodes[id] = n })
	}

	for id, n := range headNodes {
		if _, ok := baseNodes[id]; !ok {
			diff.NodesAdded = append(diff.NodesAdded, toDiffNode(n))
			if n != nil {
				diff.FilesChanged = append(diff.FilesChanged, n.FileSpec.Path)
			}
		}
	}
	for id, n := range baseNodes {
		if _, ok := headNodes[id]; !ok {
			diff.NodesRemoved = append(diff.NodesRemoved, toDiffNode(n))
			if n != nil {
				diff.FilesChanged = append(diff.FilesChanged, n.FileSpec.Path)
			}
		}
	}

	baseEdges := map[string]link.ResolvedEdge{}
	if base != nil && base.OutboundEdges != nil {
		base.OutboundEdges.Iterate(func(_ string, edges []link.ResolvedEdge) {
			for _, e := range edges {
				baseEdges[edgeKey(e)] = e
			}
		})
	}
	headEdges := map[string]link.ResolvedEdge{}
	if head != nil && head.OutboundEdges != nil {
		head.OutboundEdges.Iterate(func(_ string, edges []link.ResolvedEdge) {
			for _, e := range edges {
				headEdges[edgeKey(e)] = e
			}
		})
	}

	for k, e := range headEdges {
		if _, ok := baseEdges[k]; !ok {
			diff.EdgesAdded = append(diff.EdgesAdded, toDiffEdge(e))
		}
	}
	for k, e := range baseEdges {
		if _, ok := headEdges[k]; !ok {
			diff.EdgesRemoved = append(diff.EdgesRemoved, toDiffEdge(e))
		}
	}

	sortDiff(diff)
	return diff
}

func toDiffNode(n *link.ResolvedNode) DiffNode {
	d := DiffNode{}
	if n == nil {
		return d
	}
	d.ID = n.ID
	d.Kind = n.Kind
	d.Name = n.Name
	d.File = n.FileSpec.Path
	return d
}

func toDiffEdge(e link.ResolvedEdge) DiffEdge {
	return DiffEdge{
		Type:     string(e.Type),
		SourceID: e.SourceID,
		TargetID: e.TargetID,
		Line:     e.LineNumber,
	}
}

// sortDiff orders all diff slices deterministically for stable CI comments.
func sortDiff(d *GraphDiff) {
	sort.Slice(d.NodesAdded, func(i, j int) bool { return d.NodesAdded[i].ID < d.NodesAdded[j].ID })
	sort.Slice(d.NodesRemoved, func(i, j int) bool { return d.NodesRemoved[i].ID < d.NodesRemoved[j].ID })
	sort.Slice(d.EdgesAdded, func(i, j int) bool {
		a, b := d.EdgesAdded[i], d.EdgesAdded[j]
		if a.SourceID != b.SourceID {
			return a.SourceID < b.SourceID
		}
		if a.TargetID != b.TargetID {
			return a.TargetID < b.TargetID
		}
		return a.Type < b.Type
	})
	sort.Slice(d.EdgesRemoved, func(i, j int) bool {
		a, b := d.EdgesRemoved[i], d.EdgesRemoved[j]
		if a.SourceID != b.SourceID {
			return a.SourceID < b.SourceID
		}
		if a.TargetID != b.TargetID {
			return a.TargetID < b.TargetID
		}
		return a.Type < b.Type
	})
	sort.Strings(d.FilesChanged)
	// Deduplicate files while preserving sort order.
	if len(d.FilesChanged) > 1 {
		uniq := d.FilesChanged[:1]
		for _, f := range d.FilesChanged[1:] {
			if f != uniq[len(uniq)-1] {
				uniq = append(uniq, f)
			}
		}
		d.FilesChanged = uniq
	}
}
