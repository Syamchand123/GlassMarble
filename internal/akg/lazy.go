package akg

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/link"
	"github.com/Syamchand123/GlassMarble/internal/product/ont"
	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/types"
)

// jsonStatePath returns the canonical GraphJSON state file for a storage dir.
func jsonStatePath(storageDir string) string {
	return filepath.Join(storageDir, jsonStateFile)
}

// errStreamStopped is the internal sentinel for clean early termination of a
// lazy JSON stream (mirrors ingest.StopStreaming).
var errStreamStopped = errors.New("json stream stopped")

// jsonStreamHooks carries the per-document callbacks for streamJSONState.
type jsonStreamHooks struct {
	onEntrypoint func(string) bool
	onNode       func(GraphNodeJSON) bool
	onEdge       func(GraphEdgeJSON) bool
}

// streamJSONState walks a GraphJSON document with a json.Decoder so memory
// stays bounded by a single node/edge instead of the full graph
// (AUDIT Issue 4 Phase 4A-2 / R7). Nodes are emitted before edges, in
// document order. Returning false from a callback stops iteration cleanly
// (nil error); unknown top-level fields are skipped without materialization.
func streamJSONState(jsonPath string, hooks jsonStreamHooks) error {
	f, err := os.Open(jsonPath)
	if err != nil {
		return err
	}
	defer f.Close()

	dec := json.NewDecoder(f)
	tok, err := dec.Token()
	if err != nil {
		return fmt.Errorf("failed to read JSON state: %w", err)
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return fmt.Errorf("failed to read JSON state: top-level value is not an object")
	}

	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return fmt.Errorf("failed to read JSON state: %w", err)
		}
		key, ok := keyTok.(string)
		if !ok {
			return fmt.Errorf("failed to read JSON state: malformed object key")
		}
		switch key {
		case "entrypoints":
			if hooks.onEntrypoint == nil {
				var raw json.RawMessage
				if err := dec.Decode(&raw); err != nil {
					return fmt.Errorf("failed to read JSON state: %w", err)
				}
				continue
			}
			if err := streamJSONStringArray(dec, hooks.onEntrypoint); err != nil {
				return unwrapStreamStop(err)
			}
		case "nodes":
			if hooks.onNode == nil {
				var raw json.RawMessage
				if err := dec.Decode(&raw); err != nil {
					return fmt.Errorf("failed to read JSON state: %w", err)
				}
				continue
			}
			if err := streamJSONArray(dec, func() (bool, error) {
				var n GraphNodeJSON
				if err := dec.Decode(&n); err != nil {
					return false, err
				}
				if n.ID == "" {
					return true, nil
				}
				if !hooks.onNode(n) {
					return false, errStreamStopped
				}
				return true, nil
			}); err != nil {
				return unwrapStreamStop(err)
			}
		case "edges":
			if hooks.onEdge == nil {
				var raw json.RawMessage
				if err := dec.Decode(&raw); err != nil {
					return fmt.Errorf("failed to read JSON state: %w", err)
				}
				continue
			}
			if err := streamJSONArray(dec, func() (bool, error) {
				var e GraphEdgeJSON
				if err := dec.Decode(&e); err != nil {
					return false, err
				}
				if e.SourceID == "" || e.TargetID == "" {
					return true, nil
				}
				if !hooks.onEdge(e) {
					return false, errStreamStopped
				}
				return true, nil
			}); err != nil {
				return unwrapStreamStop(err)
			}
		default:
			var raw json.RawMessage
			if err := dec.Decode(&raw); err != nil {
				return fmt.Errorf("failed to read JSON state: %w", err)
			}
		}
	}
	return nil
}

// streamJSONArray decodes the array currently at the decoder's position,
// invoking each for every element.
func streamJSONArray(dec *json.Decoder, each func() (bool, error)) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if d, ok := tok.(json.Delim); !ok || d != '[' {
		return fmt.Errorf("expected a JSON array")
	}
	for dec.More() {
		cont, err := each()
		if err != nil {
			return err
		}
		if !cont {
			return errStreamStopped
		}
	}
	_, err = dec.Token() // consume the closing ']'
	return err
}

// streamJSONStringArray decodes an array of strings, invoking fn per element.
func streamJSONStringArray(dec *json.Decoder, fn func(string) bool) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if d, ok := tok.(json.Delim); !ok || d != '[' {
		return fmt.Errorf("expected a JSON array")
	}
	for dec.More() {
		var s string
		if err := dec.Decode(&s); err != nil {
			return err
		}
		if !fn(s) {
			return errStreamStopped
		}
	}
	_, err = dec.Token()
	return err
}

func unwrapStreamStop(err error) error {
	if errors.Is(err, errStreamStopped) {
		return nil
	}
	return err
}

// QueryNode performs a lazy single-node lookup against the persisted akg.json
// artifact without restoring the whole CodePropertyGraph: the document is
// streamed once, only the requested node object is kept (last one wins, so
// later blocks override earlier ones), and only the edges incident to that
// node are collected. Memory is bounded by node degree.
// (AUDIT Issue 4 Phase 4A-2)
//
// Returns (nil, nil, nil, nil) when the node does not exist. A missing
// state file is reported as an error (os.ErrNotExist wrapped).
func QueryNode(storageDir, nodeID string) (*link.ResolvedNode, []link.ResolvedEdge, []link.ResolvedEdge, error) {
	jsonPath := jsonStatePath(storageDir)
	if _, err := os.Stat(jsonPath); err != nil {
		return nil, nil, nil, fmt.Errorf("AKG state not found: %w", err)
	}

	var found *link.ResolvedNode
	var out, in []link.ResolvedEdge

	err := streamJSONState(jsonPath, jsonStreamHooks{
		onNode: func(n GraphNodeJSON) bool {
			if n.ID == nodeID {
				found = graphNodeToResolved(n)
			}
			return true
		},
		onEdge: func(e GraphEdgeJSON) bool {
			if e.SourceID == nodeID {
				out = append(out, graphEdgeToResolved(e))
			}
			if e.TargetID == nodeID {
				in = append(in, graphEdgeToResolved(e))
			}
			return true
		},
	})
	if err != nil {
		return nil, nil, nil, fmt.Errorf("lazy node lookup failed: %w", err)
	}
	return found, out, in, nil
}

// StreamNodes lazily streams every node object from the akg.json artifact,
// converting each one to a ResolvedNode on the fly. Returning false from fn
// stops iteration cleanly (nil error). Memory is bounded by a single node;
// the graph indices are NOT built, so Query() on nodes collected this way
// falls back to its linear-scan path.
// (AUDIT Issue 4 Phase 4A-2)
func StreamNodes(storageDir string, fn func(*link.ResolvedNode) bool) error {
	jsonPath := jsonStatePath(storageDir)
	if _, err := os.Stat(jsonPath); err != nil {
		return fmt.Errorf("AKG state not found: %w", err)
	}
	return streamJSONState(jsonPath, jsonStreamHooks{
		onNode: func(n GraphNodeJSON) bool {
			return fn(graphNodeToResolved(n))
		},
	})
}

// StreamEdges lazily streams every edge object from the akg.json artifact.
// Returning false from fn stops iteration cleanly (nil error); the caller
// receives the raw GraphEdgeJSON form, which carries the canonical
// RelationshipType string (convert with EdgeTypeToPredicate).
func StreamEdges(storageDir string, fn func(GraphEdgeJSON) bool) error {
	jsonPath := jsonStatePath(storageDir)
	if _, err := os.Stat(jsonPath); err != nil {
		return fmt.Errorf("AKG state not found: %w", err)
	}
	return streamJSONState(jsonPath, jsonStreamHooks{
		onEdge: func(e GraphEdgeJSON) bool {
			return fn(e)
		},
	})
}

// StreamEntrypoints lazily streams the entrypoint IDs from the akg.json
// artifact. Returning false from fn stops iteration cleanly.
func StreamEntrypoints(storageDir string, fn func(string) bool) error {
	jsonPath := jsonStatePath(storageDir)
	if _, err := os.Stat(jsonPath); err != nil {
		return fmt.Errorf("AKG state not found: %w", err)
	}
	return streamJSONState(jsonPath, jsonStreamHooks{
		onEntrypoint: fn,
	})
}

// LazyStats holds every figure computable from a single streaming pass over
// the akg.json artifact — nothing that requires restoring the graph or
// building its indexes (AUDIT Issue 4 Phase 4A-2). Edges is the count of
// distinct (source, type, target) tuples, which matches the canonical edge
// set the persistence layer dedups parallel edges into — the same figure the
// TTL path reported.
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
	jsonPath := jsonStatePath(storageDir)
	if _, err := os.Stat(jsonPath); err != nil {
		return nil, fmt.Errorf("AKG state not found: %w", err)
	}

	st := &LazyStats{}
	ids := make(map[string]bool)
	files := make(map[string]bool)
	seenEdges := make(map[string]bool)

	err := streamJSONState(jsonPath, jsonStreamHooks{
		onEntrypoint: func(_ string) bool {
			st.Entrypoints++
			return true
		},
		onNode: func(n GraphNodeJSON) bool {
			st.NodeCount++
			ids[n.ID] = true
			if link.IsVirtualID(n.ID) {
				st.VirtualCount++
			}
			// normalizePath("") is "." on Windows, so the empty-path virtual
			// nodes land in the same set slot the restore's FileNodeIndex gives
			// them: the two views must agree file-for-file.
			files[normalizePath(n.FileSpec.Path)] = true
			return true
		},
		onEdge: func(e GraphEdgeJSON) bool {
			edgeKey := e.SourceID + "|" + e.Type + "|" + e.TargetID
			if seenEdges[edgeKey] {
				return true
			}
			seenEdges[edgeKey] = true
			st.Edges++
			if !ids[e.SourceID] || !ids[e.TargetID] {
				st.Dangling++
			}
			return true
		},
	})
	if err != nil {
		return nil, err
	}
	st.IndexedFiles = len(files)

	return st, nil
}

// ToNativeGraph converts this restored CodePropertyGraph into the shared
// NativeGraph form consumed by the visualization engine, without re-reading
// the JSON artifact (AUDIT Issue 4 Phase 4A-1). Node kinds and properties are
// carried verbatim; edges are canonicalized with the persistence layer's
// dedup rule (one edge per (source, predicate, target), keeping the
// last-seen line number) so the conversion mirrors what is written to disk.
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

	c.Nodes.Iterate(func(_ string, n *link.ResolvedNode) {
		if n == nil {
			return
		}
		nn := ResolvedNodeToNativeNode(n)
		nn.IsEntrypoint = entrypointSet[n.ID]
		if zone, ok := c.FolderZones.Get(n.ID); ok {
			nn.PrimitiveZone = zone
		}
		out.Nodes[n.ID] = nn
	})

	type keyT struct{ s, p, t string }
	best := make(map[keyT]types.NativeEdge, c.OutboundEdges.Len())
	c.OutboundEdges.Iterate(func(sourceID string, edges []link.ResolvedEdge) {
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

// graphNodeToResolved converts a streamed GraphJSON node into a ResolvedNode
// exactly as ImportGraphJSON would (verbatim fields, copied properties).
func graphNodeToResolved(n GraphNodeJSON) *link.ResolvedNode {
	return &link.ResolvedNode{
		ID:        n.ID,
		Kind:      n.Kind,
		Name:      n.Name,
		Primitive: n.Primitive,
		FileSpec: link.LocationMeta{
			Path:      n.FileSpec.Path,
			LineStart: n.FileSpec.LineStart,
			LineEnd:   n.FileSpec.LineEnd,
		},
		Properties: copyStringMap(n.Properties),
	}
}

// graphEdgeToResolved converts a streamed GraphJSON edge into a ResolvedEdge
// exactly as ImportGraphJSON would.
func graphEdgeToResolved(e GraphEdgeJSON) link.ResolvedEdge {
	return link.ResolvedEdge{
		SourceID:   e.SourceID,
		TargetID:   e.TargetID,
		Type:       link.RelationshipType(e.Type),
		LineNumber: e.LineNumber,
		Confidence: e.Confidence,
		IsCycle:    e.IsCycle,
		Properties: copyStringMap(e.Properties),
	}
}

// ResolvedNodeToNativeNode converts a restored node into the shared
// NativeGraph form exactly as the persistence layer would have serialized
// it: kind and properties are carried via the shared vocabulary, so a node
// loaded from GraphJSON equals the node a parse-back of the persisted file
// would yield.
func ResolvedNodeToNativeNode(n *link.ResolvedNode) *types.NativeNode {
	props := make(map[string]string, len(n.Properties))
	for k, v := range n.Properties {
		props[k] = v
	}
	return &types.NativeNode{
		ID:            n.ID,
		Kind:          mapKindToClass(n.Kind),
		Name:          n.Name,
		PrimitiveType: ont.PrefixGM + n.Primitive,
		FileURI:       "file:" + n.FileSpec.Path,
		LineStart:     n.FileSpec.LineStart,
		LineEnd:       n.FileSpec.LineEnd,
		Code:          props["code"],
		Properties:    props,
	}
}
