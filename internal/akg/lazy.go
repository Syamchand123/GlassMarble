package akg

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage4"
	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/stage1"
	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/types"
)

// QueryNode performs a lazy single-node lookup against the persisted TTL
// artifact without restoring the whole CodePropertyGraph: the file is
// streamed once, only the requested node block is kept (last one wins, so
// tombstones and edits are honored), and only the edges incident to that
// node are collected. Memory is bounded by node degree.
// (AUDIT Issue 4 Phase 4A-2)
//
// Returns (nil, nil, nil, nil) when the node does not exist. A missing
// state file is reported as an error (os.ErrNotExist wrapped).
func QueryNode(storageDir, nodeID string) (*stage4.ResolvedNode, []stage4.ResolvedEdge, []stage4.ResolvedEdge, error) {
	ttlPath := filepath.Join(storageDir, "akg_state.ttl")
	if _, err := os.Stat(ttlPath); err != nil {
		return nil, nil, nil, fmt.Errorf("AKG state not found: %w", err)
	}

	node, edges, err := stage1.ParseTTLNodeByID(ttlPath, nodeID)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("lazy node lookup failed: %w", err)
	}
	if node == nil {
		return nil, nil, nil, nil
	}

	res := ttlNodeToResolved(node)
	var out, in []stage4.ResolvedEdge
	for _, e := range edges {
		if e.Predicate == "gm:status" {
			continue
		}
		re := stage4.ResolvedEdge{
			SourceID:   e.SourceID,
			TargetID:   e.TargetID,
			Type:       mapPredicateToEdgeType(e.Predicate),
			LineNumber: e.LineNumber,
		}
		if e.SourceID == nodeID {
			out = append(out, re)
		}
		if e.TargetID == nodeID {
			in = append(in, re)
		}
	}
	return res, out, in, nil
}

// StreamNodes lazily streams every non-tombstone node block from the TTL
// artifact, converting each one to a ResolvedNode on the fly. Returning
// false from fn stops iteration cleanly (nil error). Memory is bounded by a
// single node; the graph indices are NOT built, so Query() on nodes
// collected this way falls back to its linear-scan path.
// (AUDIT Issue 4 Phase 4A-2)
func StreamNodes(storageDir string, fn func(*stage4.ResolvedNode) bool) error {
	ttlPath := filepath.Join(storageDir, "akg_state.ttl")
	if _, err := os.Stat(ttlPath); err != nil {
		return fmt.Errorf("AKG state not found: %w", err)
	}

	err := stage1.StreamTTLNodes(ttlPath, func(tn *types.TTLNode) bool {
		return fn(ttlNodeToResolved(tn))
	})
	if stage1.StopStreaming(err) {
		return nil
	}
	return err
}

// LazyStats holds every figure computable from a single streaming pass over
// the TTL artifact — nothing that requires restoring the graph or building
// its indexes (AUDIT Issue 4 Phase 4A-2). Edges is the count of distinct
// (source, predicate, target) triples, which is the cumulative outbound AND
// inbound edge total of the restored graph (the persistence layer collapses
// parallel edges to one canonical triple).
type LazyStats struct {
	NodeCount    int
	VirtualCount int
	Entrypoints  int
	Edges        int
	Dangling     int
	IndexedFiles int
}

// StreamGraphStats computes LazyStats in two streaming passes (nodes, then
// edges), with memory bounded by the node-ID and file-path sets rather than
// the full graph.
func StreamGraphStats(storageDir string) (*LazyStats, error) {
	ttlPath := filepath.Join(storageDir, "akg_state.ttl")
	if _, err := os.Stat(ttlPath); err != nil {
		return nil, fmt.Errorf("AKG state not found: %w", err)
	}

	st := &LazyStats{}
	ids := make(map[string]bool)
	files := make(map[string]bool)

	err := stage1.StreamTTLNodes(ttlPath, func(tn *types.TTLNode) bool {
		st.NodeCount++
		ids[tn.ID] = true
		if stage4.IsVirtualID(tn.ID) {
			st.VirtualCount++
		}
		if tn.IsEntrypoint {
			st.Entrypoints++
		}
		// normalizePath("") is "." on Windows, so the empty-path virtual
		// nodes land in the same set slot the restore's FileNodeIndex gives
		// them: the two views must agree file-for-file.
		files[normalizePath(strings.TrimPrefix(tn.FileURI, "file:"))] = true
		return true
	})
	if err != nil {
		return nil, err
	}
	st.IndexedFiles = len(files)

	err = stage1.StreamTTLBlocks(ttlPath, func(block string) error {
		trimmed := strings.TrimSuffix(strings.TrimSpace(block), ".")
		if strings.HasPrefix(trimmed, "<<") {
			return nil // reified property triple: its edge is counted via the base block
		}
		parts := strings.Fields(trimmed)
		if len(parts) != 3 || parts[1] == "a" {
			return nil // node/metadata blocks are not edges
		}
		if parts[1] == "gm:status" {
			return nil // tombstone marker, not an edge
		}
		st.Edges++
		src := types.ParseNodeURI(parts[0])
		tgt := types.ParseNodeURI(parts[2])
		if !ids[src] || !ids[tgt] {
			st.Dangling++
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return st, nil
}

// WALFreshness reports whether the WAL holds any entries (a crash may have
// left transactions unpersisted) and whether the newest WAL segment is
// newer than the TTL (AUDIT Issue 4 Phase 4B-5 streaming read, no full
// load).
func WALFreshness(storageDir string) (stale bool, txCount int, err error) {
	walDir := filepath.Join(storageDir, "wal")
	wal, err := NewWriteAheadLog(walDir)
	if err != nil {
		if os.IsNotExist(err) {
			return false, 0, nil
		}
		return false, 0, err
	}
	err = wal.ForEachEntry(func(*WALEntry) error {
		txCount++
		return nil
	})
	if err != nil {
		return false, 0, err
	}

	ttlStat, err := os.Stat(filepath.Join(storageDir, "akg_state.ttl"))
	if err != nil {
		return false, txCount, nil
	}
	latest := ttlStat.ModTime()
	if entries, rerr := os.ReadDir(walDir); rerr == nil {
		for _, f := range entries {
			if f.IsDir() {
				continue
			}
			if info, ierr := f.Info(); ierr == nil && info.ModTime().After(latest) {
				latest = info.ModTime()
			}
		}
	}
	return latest.After(ttlStat.ModTime()) && txCount > 0, txCount, nil
}

// ToNativeGraph converts this restored CodePropertyGraph into the shared
// NativeGraph form consumed by the visualization engine, without re-reading
// the TTL artifact (AUDIT Issue 4 Phase 4A-1). Node kinds and properties are
// carried verbatim; edges are canonicalized with the persistence layer's
// dedup rule (one edge per (source, predicate, target), keeping the
// last-seen line number) so the conversion mirrors what the serializer
// writes to disk.
func (c *CodePropertyGraph) ToNativeGraph() *types.NativeGraph {
	c.mu.RLock()
	defer c.mu.RUnlock()

	out := &types.NativeGraph{
		Nodes: make(map[string]*types.NativeNode, c.Nodes.Len()),
	}

	entrypointSet := make(map[string]bool, len(c.Entrypoints))
	for _, ep := range c.Entrypoints {
		entrypointSet[ep] = true
	}

	c.Nodes.Iterate(func(_ string, n *stage4.ResolvedNode) {
		if n == nil {
			return
		}
		nn := nodeToTTLNode(n)
		nn.IsEntrypoint = entrypointSet[n.ID]
		if zone, ok := c.FolderZones.Get(n.ID); ok {
			nn.PrimitiveZone = zone
		}
		out.Nodes[n.ID] = nn
	})

	type keyT struct{ s, p, t string }
	best := make(map[keyT]types.NativeEdge, c.OutboundEdges.Len())
	c.OutboundEdges.Iterate(func(sourceID string, edges []stage4.ResolvedEdge) {
		for _, e := range edges {
			pred := mapEdgeTypeToPredicate(e.Type)
			if pred == "" {
				continue
			}
			k := keyT{sourceID, pred, e.TargetID}
			if cur, ok := best[k]; !ok || e.LineNumber > cur.LineNumber {
				best[k] = types.NativeEdge{SourceID: sourceID, Predicate: pred, TargetID: e.TargetID, LineNumber: e.LineNumber}
			}
		}
	})
	for _, e := range best {
		out.Edges = append(out.Edges, e)
	}

	return out
}

// ttlNodeToResolved mirrors reconstructFromTTLFile's node conversion so lazy
// reads return exactly what a restored graph would hold.
func ttlNodeToResolved(tn *types.TTLNode) *stage4.ResolvedNode {
	res := &stage4.ResolvedNode{
		ID:        tn.ID,
		Kind:      mapClassToKind(tn.Kind),
		Name:      tn.Name,
		Primitive: strings.TrimPrefix(tn.PrimitiveType, "gm:"),
		FileSpec: stage4.LocationMeta{
			Path:      strings.TrimPrefix(tn.FileURI, "file:"),
			LineStart: tn.LineStart,
			LineEnd:   tn.LineEnd,
		},
		Properties: make(map[string]string, len(tn.Properties)),
	}
	for k, v := range tn.Properties {
		res.Properties[k] = v
	}
	if tn.Code != "" {
		res.Properties["code"] = tn.Code
	}
	return res
}

// nodeToTTLNode mirrors turtle_serializer.go's node serialization so
// ToNativeGraph produces exactly what a parse-back of the persisted file
// would yield.
func nodeToTTLNode(n *stage4.ResolvedNode) *types.NativeNode {
	props := make(map[string]string, len(n.Properties))
	for k, v := range n.Properties {
		props[k] = v
	}
	return &types.NativeNode{
		ID:            n.ID,
		Kind:          mapKindToClass(n.Kind),
		Name:          n.Name,
		PrimitiveType: "gm:" + n.Primitive,
		FileURI:       "file:" + n.FileSpec.Path,
		LineStart:     n.FileSpec.LineStart,
		LineEnd:       n.FileSpec.LineEnd,
		Code:          props["code"],
		Properties:    props,
	}
}
