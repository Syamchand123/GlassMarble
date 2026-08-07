package akg

import (
	"fmt"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage4"
	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/types"
)

// This file exposes a persisted GraphJSON state document as the
// visualization graph view the extraction pipeline consumes. The functions
// are the JSON-store equivalents of the legacy Turtle parsers in
// internal/visualization_engine/stage1: same inputs, same (nodes, edges)
// shapes, same last-wins and scoping semantics. Node kinds and edge
// predicates are converted with the shared kind-vocabulary contract
// (stage4.KindToClass / EdgeTypeToPredicate), so a graph loaded from JSON
// feeds the extraction pipeline identically. Deleted nodes and tombstones
// do not exist in the JSON store — they are simply absent from the
// document. Each function streams graphPath directly (the file name need
// not be akg.json).

// ParseGraphFile loads a persisted GraphJSON state document and returns
// extracted nodes and edges in the visualization form (NativeNode/
// NativeEdge — the same shapes stage1.ParseTTLFile produced from Turtle).
func ParseGraphFile(graphPath string) (map[string]*types.NativeNode, []types.NativeEdge, error) {
	entrypoints := make(map[string]bool)
	if err := streamJSONState(graphPath, jsonStreamHooks{
		onEntrypoint: func(id string) bool {
			entrypoints[id] = true
			return true
		},
	}); err != nil {
		return nil, nil, fmt.Errorf("reading graph state: %w", err)
	}

	nodes := make(map[string]*types.NativeNode)
	if err := streamJSONState(graphPath, jsonStreamHooks{
		onNode: func(n GraphNodeJSON) bool {
			nn := ResolvedNodeToNativeNode(graphNodeToResolved(n))
			nn.IsEntrypoint = entrypoints[n.ID]
			nodes[n.ID] = nn
			return true
		},
	}); err != nil {
		return nil, nil, fmt.Errorf("reading graph state: %w", err)
	}

	edges := make([]types.NativeEdge, 0)
	best := make(map[string]types.NativeEdge)
	if err := streamJSONState(graphPath, jsonStreamHooks{
		onEdge: func(e GraphEdgeJSON) bool {
			pred := EdgeTypeToPredicate(stage4.RelationshipType(e.Type))
			if pred == "" {
				return true
			}
			key := e.SourceID + "|" + pred + "|" + e.TargetID
			if cur, ok := best[key]; !ok || e.LineNumber > cur.LineNumber {
				best[key] = types.NativeEdge{SourceID: e.SourceID, Predicate: pred, TargetID: e.TargetID, LineNumber: e.LineNumber}
			}
			return true
		},
	}); err != nil {
		return nil, nil, fmt.Errorf("reading graph state: %w", err)
	}
	for _, e := range best {
		edges = append(edges, e)
	}
	return nodes, edges, nil
}

// ParseGraphFileToNative loads a GraphJSON state document into a
// NativeGraph directly.
func ParseGraphFileToNative(graphPath string) (*types.NativeGraph, error) {
	nodes, edges, err := ParseGraphFile(graphPath)
	if err != nil {
		return nil, err
	}
	return &types.NativeGraph{Nodes: nodes, Edges: edges}, nil
}

// ParseGraphNodeByID streams a GraphJSON state document and returns the
// LAST node whose ID matches (appended edits win), plus every edge touching
// that node — the JSON equivalent of stage1.ParseTTLNodeByID. Memory is
// bounded by the node's incident degree. If the node is absent,
// (nil, nil, nil) is returned with no error.
func ParseGraphNodeByID(graphPath, nodeID string) (*types.NativeNode, []types.NativeEdge, error) {
	var found *types.NativeNode
	if err := streamJSONState(graphPath, jsonStreamHooks{
		onNode: func(n GraphNodeJSON) bool {
			if n.ID == nodeID {
				found = ResolvedNodeToNativeNode(graphNodeToResolved(n))
			}
			return true
		},
	}); err != nil {
		return nil, nil, fmt.Errorf("lazy node lookup failed: %w", err)
	}
	if found == nil {
		return nil, nil, nil
	}

	if err := streamJSONState(graphPath, jsonStreamHooks{
		onEntrypoint: func(id string) bool {
			if id == nodeID {
				found.IsEntrypoint = true
			}
			return true
		},
	}); err != nil {
		return nil, nil, fmt.Errorf("lazy node lookup failed: %w", err)
	}

	var edges []types.NativeEdge
	if err := streamJSONState(graphPath, jsonStreamHooks{
		onEdge: func(e GraphEdgeJSON) bool {
			if e.SourceID != nodeID && e.TargetID != nodeID {
				return true
			}
			pred := EdgeTypeToPredicate(stage4.RelationshipType(e.Type))
			if pred == "" {
				return true
			}
			edges = append(edges, types.NativeEdge{SourceID: e.SourceID, Predicate: pred, TargetID: e.TargetID, LineNumber: e.LineNumber})
			return true
		},
	}); err != nil {
		return nil, nil, fmt.Errorf("lazy edge lookup failed: %w", err)
	}
	return found, edges, nil
}

// StreamGraphNodes lazily streams every node from a GraphJSON state
// document in its visualization form; iteration stops when fn returns
// false. Node entrypoint flags are applied before the callback.
func StreamGraphNodes(graphPath string, fn func(*types.NativeNode) bool) error {
	entrypoints := make(map[string]bool)
	if err := streamJSONState(graphPath, jsonStreamHooks{
		onEntrypoint: func(id string) bool {
			entrypoints[id] = true
			return true
		},
	}); err != nil {
		return fmt.Errorf("reading graph state: %w", err)
	}
	return streamJSONState(graphPath, jsonStreamHooks{
		onNode: func(n GraphNodeJSON) bool {
			nn := ResolvedNodeToNativeNode(graphNodeToResolved(n))
			nn.IsEntrypoint = entrypoints[n.ID]
			return fn(nn)
		},
	})
}

// StreamGraphEdges lazily streams every edge from a GraphJSON state
// document in its visualization form; iteration stops when fn returns
// false.
func StreamGraphEdges(graphPath string, fn func(*types.NativeEdge) bool) error {
	return streamJSONState(graphPath, jsonStreamHooks{
		onEdge: func(e GraphEdgeJSON) bool {
			pred := EdgeTypeToPredicate(stage4.RelationshipType(e.Type))
			if pred == "" {
				return true
			}
			return fn(&types.NativeEdge{SourceID: e.SourceID, Predicate: pred, TargetID: e.TargetID, LineNumber: e.LineNumber})
		},
	})
}

// ParseGraphFileToNativeScoped streams the GraphJSON state and materializes
// ONLY the nodes belonging to scopePath plus the edges whose endpoints are
// both in that set: file-scope diagrams must load only the file's nodes
// instead of the whole database. Matching rules mirror
// stage1.ApplyScope(ScopeFile) exactly, so the result equals a full load
// followed by ApplyScope.
func ParseGraphFileToNativeScoped(graphPath, scopePath string) (*types.NativeGraph, error) {
	if scopePath == "" {
		return nil, fmt.Errorf("file scope requires a non-empty path")
	}
	graph := &types.NativeGraph{
		Nodes: make(map[string]*types.NativeNode),
		Edges: make([]types.NativeEdge, 0),
	}
	matched := make(map[string]bool)
	scopedPath := "/" + strings.TrimPrefix(scopePath, "/")

	entrypoints := make(map[string]bool)
	if err := streamJSONState(graphPath, jsonStreamHooks{
		onEntrypoint: func(id string) bool {
			entrypoints[id] = true
			return true
		},
	}); err != nil {
		return nil, fmt.Errorf("scoped graph load failed: %w", err)
	}

	if err := streamJSONState(graphPath, jsonStreamHooks{
		onNode: func(n GraphNodeJSON) bool {
			nn := ResolvedNodeToNativeNode(graphNodeToResolved(n))
			if matchesFileScope(nn, nn.ID, scopePath, scopedPath) {
				nn.IsEntrypoint = entrypoints[nn.ID]
				graph.Nodes[nn.ID] = nn
				matched[nn.ID] = true
			}
			return true
		},
	}); err != nil {
		return nil, fmt.Errorf("scoped graph load failed: %w", err)
	}

	edgeMap := make(map[string]*types.NativeEdge)
	if err := streamJSONState(graphPath, jsonStreamHooks{
		onEdge: func(e GraphEdgeJSON) bool {
			pred := EdgeTypeToPredicate(stage4.RelationshipType(e.Type))
			if pred == "" || !matched[e.SourceID] || !matched[e.TargetID] {
				return true
			}
			edge := &types.NativeEdge{SourceID: e.SourceID, Predicate: pred, TargetID: e.TargetID, LineNumber: e.LineNumber}
			key := e.SourceID + "|" + pred + "|" + e.TargetID
			if cur, ok := edgeMap[key]; !ok || edge.LineNumber > cur.LineNumber {
				edgeMap[key] = edge
			}
			return true
		},
	}); err != nil {
		return nil, fmt.Errorf("scoped graph load failed: %w", err)
	}
	for _, e := range edgeMap {
		graph.Edges = append(graph.Edges, *e)
	}
	return graph, nil
}

// matchesFileScope reproduces stage1.ApplyScope(ScopeFile)'s node predicate.
func matchesFileScope(n *types.NativeNode, id, scopePath, scopedPath string) bool {
	return n.FileURI == scopedPath || strings.HasSuffix(n.FileURI, scopedPath) ||
		strings.HasPrefix(id, scopedPath) || strings.HasPrefix(id, scopePath)
}

// ParseGraphForQuery adapts the JSON-store readers to the visualization
// pipeline's parser contract (visualization_engine.ParseFn): a global load
// for whole-graph queries and a lazy file-scoped load for ScopeFile queries.
// It is wired into EngineCoordinator by request-layer callers because akg
// cannot be imported from visualization_engine or product (import cycle via
// stage4 → product).
func ParseGraphForQuery(graphPath string, opts types.QueryOptions) (*types.NativeGraph, error) {
	if opts.Scope == types.ScopeFile && opts.ScopePath != "" {
		return ParseGraphFileToNativeScoped(graphPath, opts.ScopePath)
	}
	return ParseGraphFileToNative(graphPath)
}
