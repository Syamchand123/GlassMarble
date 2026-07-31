package tools

import (
	"context"
	"fmt"
	"sort"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage4"
)

// akgTools builds the AKG query tools. Every handler operates on the live
// snapshot provided by the bridge and is strictly read-only.
func akgTools() []Tool {
	return []Tool{
		{
			Name:        "akg_status",
			Description: "Overview of the AKG knowledge graph: commit, node/edge/file counts, entrypoints, dangling references, detected pattern count.",
			Category:    CategoryAKG,
			Parameters:  Schema(nil),
			Handler: func(ctx context.Context, env *Env, args map[string]any) (any, error) {
				return withSnapshot(env, func(snap *akg.CodePropertyGraph) (any, error) {
					out := map[string]any{
						"commit":              snap.CommitHash,
						"version":             snap.Version,
						"schema_version":      snap.SchemaVersion,
						"nodes":               snap.Nodes.Len(),
						"edges":               edgeCount(snap),
						"files":               snap.FileNodeIndex.Len(),
						"entrypoints":         len(snap.Entrypoints),
						"dangling_references": len(snap.Errors),
					}
					if snap.Summary != nil {
						out["patterns"] = len(snap.Summary.PrimaryPatterns)
						out["generated_at"] = snap.Summary.GeneratedAt
					}
					return out, nil
				})
			},
		},
		{
			Name:        "akg_summary",
			Description: "Architectural summary of the AKG: detected patterns, layer distribution, hotspot nodes, entry points, and external dependencies.",
			Category:    CategoryAKG,
			Parameters:  Schema(nil),
			Handler: func(ctx context.Context, env *Env, args map[string]any) (any, error) {
				return withSnapshot(env, func(snap *akg.CodePropertyGraph) (any, error) {
					if snap.Summary == nil {
						return nil, fmt.Errorf("no architectural summary available — the AKG may be empty")
					}
					return snap.Summary, nil
				})
			},
		},
		{
			Name:        "akg_search",
			Description: "Search AKG nodes with an optional kind, name substring, primitive, and property filters. Returns matching nodes with file locations.",
			Category:    CategoryAKG,
			Parameters: Schema(map[string]Prop{
				"kind":          {Type: "string", Description: "Exact node kind: MODULE, FILE, STRUCT, CLASS, INTERFACE, FUNCTION, METHOD, PACKAGE, ..."},
				"name_contains": {Type: "string", Description: "Substring of the node name (case-insensitive)"},
				"primitive":     {Type: "string", Description: "Exact primitive type: DISK_IO, NETWORK_IO, COMPUTE, CONCURRENCY, DATABASE, CACHE, EXTERNAL, ..."},
				"properties":    {Type: "object", Description: "Property key/value pairs that must all match exactly, e.g. {\"macro_rules\": \"data_layer\"}"},
				"limit":         {Type: "integer", Description: "Max results (default 20, max 100)", Default: float64(20)},
				"offset":        {Type: "integer", Description: "Pagination offset", Default: float64(0)},
			}),
			Handler: func(ctx context.Context, env *Env, args map[string]any) (any, error) {
				return withSnapshot(env, func(snap *akg.CodePropertyGraph) (any, error) {
					filter := stage4.QueryFilter{
						Kind:         strArg(args, "kind", ""),
						NameContains: strArg(args, "name_contains", ""),
						Primitive:    strArg(args, "primitive", ""),
						Limit:        intArg(args, "limit", 20, 1, 100),
						Offset:       intArg(args, "offset", 0, 0, 100000),
					}
					if props := propsArg(args["properties"]); len(props) > 0 {
						filter.Properties = props
					}
					nodes := snap.Query(filter)
					if nodes == nil {
						nodes = []*stage4.ResolvedNode{}
					}
					out := make([]nodeBrief, 0, len(nodes))
					for _, n := range nodes {
						out = append(out, brief(n))
					}
					return map[string]any{"count": len(out), "nodes": out}, nil
				})
			},
		},
		{
			Name:        "akg_get_node",
			Description: "Full details of one AKG node by ID: properties (macro_rules, pagerank, blast_radius, instability, ...), file location, and inbound/outbound edge counts.",
			Category:    CategoryAKG,
			Parameters: Schema(map[string]Prop{
				"id": {Type: "string", Description: "Full node ID, e.g. \"src/db.go::PostgresStore::Save\"", Required: true},
			}),
			Handler: func(ctx context.Context, env *Env, args map[string]any) (any, error) {
				return withSnapshot(env, func(snap *akg.CodePropertyGraph) (any, error) {
					id := strArg(args, "id", "")
					n, ok := snap.GetNode(id)
					if !ok {
						return nil, fmt.Errorf("node %q not found in the AKG", id)
					}
					full := nodeFull{
						nodeBrief:  brief(n),
						Properties: n.Properties,
						Inbound:    len(snap.GetInboundEdges(id)),
						Outbound:   len(snap.GetOutboundEdges(id)),
					}
					return full, nil
				})
			},
		},
		{
			Name:        "akg_edges",
			Description: "List edges of a node by direction and optional predicate (e.g. CALLS, DEPENDS_ON, BELONGS_TO, DATA_FLOW, IMPLEMENTS).",
			Category:    CategoryAKG,
			Parameters: Schema(map[string]Prop{
				"id":        {Type: "string", Description: "Full node ID", Required: true},
				"direction": {Type: "string", Description: "Which edges to list", Enum: []string{"in", "out", "both"}, Default: "both"},
				"predicate": {Type: "string", Description: "Optional edge type filter, e.g. CALLS"},
				"limit":     {Type: "integer", Description: "Max edges (default 50, max 200)", Default: float64(50)},
			}),
			Handler: func(ctx context.Context, env *Env, args map[string]any) (any, error) {
				return withSnapshot(env, func(snap *akg.CodePropertyGraph) (any, error) {
					id := strArg(args, "id", "")
					if _, ok := snap.GetNode(id); !ok {
						return nil, fmt.Errorf("node %q not found in the AKG", id)
					}
					direction := strArg(args, "direction", "both")
					limit := intArg(args, "limit", 50, 1, 200)
					predicate := strArg(args, "predicate", "")

					var edges []stage4.ResolvedEdge
					switch direction {
					case "in":
						edges = snap.GetInboundEdges(id)
					case "out":
						edges = snap.GetOutboundEdges(id)
					default:
						edges = append(edges, snap.GetInboundEdges(id)...)
						edges = append(edges, snap.GetOutboundEdges(id)...)
					}
					total := len(edges)
					out := make([]edgeInfo, 0, min(limit, total))
					for _, e := range edges {
						if predicate != "" && string(e.Type) != predicate {
							continue
						}
						out = append(out, edgeInfo{
							SourceID:   e.SourceID,
							TargetID:   e.TargetID,
							Type:       string(e.Type),
							LineNumber: e.LineNumber,
							Confidence: e.Confidence,
							IsCycle:    e.IsCycle,
						})
						if len(out) >= limit {
							break
						}
					}
					return map[string]any{
						"node":      id,
						"direction": direction,
						"total":     total,
						"shown":     len(out),
						"edges":     out,
					}, nil
				})
			},
		},
		{
			Name:        "akg_traverse",
			Description: "Breadth-first walk of the call/dependency graph from a start node, returning reachable nodes level by level.",
			Category:    CategoryAKG,
			Parameters: Schema(map[string]Prop{
				"start_id":    {Type: "string", Description: "Full node ID to start from", Required: true},
				"predicate":   {Type: "string", Description: "Optional edge type to follow, e.g. CALLS"},
				"depth":       {Type: "integer", Description: "Max levels (default 3, max 10)", Default: float64(3)},
				"max_results": {Type: "integer", Description: "Max nodes returned (default 200, max 500)", Default: float64(200)},
			}),
			Handler: func(ctx context.Context, env *Env, args map[string]any) (any, error) {
				return withSnapshot(env, func(snap *akg.CodePropertyGraph) (any, error) {
					start := strArg(args, "start_id", "")
					if _, ok := snap.GetNode(start); !ok {
						return nil, fmt.Errorf("node %q not found in the AKG", start)
					}
					depth := intArg(args, "depth", 3, 1, 10)
					maxResults := intArg(args, "max_results", 200, 1, 500)
					predicate := strArg(args, "predicate", "")

					type level struct {
						Level int         `json:"level"`
						Nodes []nodeBrief `json:"nodes"`
					}
					visited := map[string]bool{start: true}
					levels := []level{}
					current := []string{start}
					total := 1
					for d := 1; d <= depth && len(current) > 0; d++ {
						next := []string{}
						for _, id := range current {
							for _, e := range snap.GetOutboundEdges(id) {
								if predicate != "" && string(e.Type) != predicate {
									continue
								}
								if visited[e.TargetID] {
									continue
								}
								visited[e.TargetID] = true
								next = append(next, e.TargetID)
							}
						}
						cur := level{Level: d}
						for _, id := range next {
							if len(cur.Nodes) >= maxResults {
								break
							}
							if n, ok := snap.GetNode(id); ok {
								cur.Nodes = append(cur.Nodes, brief(n))
							}
							total++
						}
						levels = append(levels, cur)
						current = next
					}
					return map[string]any{
						"start":           start,
						"total_reachable": len(visited),
						"levels":          levels,
					}, nil
				})
			},
		},
		{
			Name:        "akg_path",
			Description: "Shortest dependency path between two nodes (BFS). Useful for 'how does X reach Y' questions.",
			Category:    CategoryAKG,
			Parameters: Schema(map[string]Prop{
				"start_id":  {Type: "string", Description: "Full node ID", Required: true},
				"target_id": {Type: "string", Description: "Full node ID", Required: true},
				"max_depth": {Type: "integer", Description: "Max search depth (default 10)", Default: float64(10)},
			}),
			Handler: func(ctx context.Context, env *Env, args map[string]any) (any, error) {
				return withSnapshot(env, func(snap *akg.CodePropertyGraph) (any, error) {
					start := strArg(args, "start_id", "")
					target := strArg(args, "target_id", "")
					if _, ok := snap.GetNode(start); !ok {
						return nil, fmt.Errorf("node %q not found in the AKG", start)
					}
					if _, ok := snap.GetNode(target); !ok {
						return nil, fmt.Errorf("node %q not found in the AKG", target)
					}
					path := snap.FindPath(start, target, intArg(args, "max_depth", 10, 1, 50))
					if path == nil {
						return map[string]any{"found": false, "start": start, "target": target}, nil
					}
					out := make([]nodeBrief, 0, len(path))
					for _, id := range path {
						if n, ok := snap.GetNode(id); ok {
							out = append(out, brief(n))
						}
					}
					return map[string]any{"found": true, "start": start, "target": target, "path": out}, nil
				})
			},
		},
		{
			Name:        "akg_cycles",
			Description: "Detect cycles in the dependency graph (Tarjan SCC). Cycles indicate circular dependencies worth explaining.",
			Category:    CategoryAKG,
			Parameters: Schema(map[string]Prop{
				"limit": {Type: "integer", Description: "Max cycles returned (default 20, max 100)", Default: float64(20)},
			}),
			Handler: func(ctx context.Context, env *Env, args map[string]any) (any, error) {
				return withSnapshot(env, func(snap *akg.CodePropertyGraph) (any, error) {
					limit := intArg(args, "limit", 20, 1, 100)
					cycles := snap.DetectCycles()
					total := len(cycles)
					out := make([][]string, 0, min(limit, total))
					for _, c := range cycles {
						names := make([]string, 0, len(c))
						for _, id := range c {
							names = append(names, displayName(snap, id))
						}
						out = append(out, names)
						if len(out) >= limit {
							break
						}
					}
					return map[string]any{"count": total, "cycles": out}, nil
				})
			},
		},
		{
			Name:        "akg_orphans",
			Description: "Find dead code: nodes with no inbound edges that are not entrypoints.",
			Category:    CategoryAKG,
			Parameters: Schema(map[string]Prop{
				"limit": {Type: "integer", Description: "Max nodes returned (default 50, max 200)", Default: float64(50)},
			}),
			Handler: func(ctx context.Context, env *Env, args map[string]any) (any, error) {
				return withSnapshot(env, func(snap *akg.CodePropertyGraph) (any, error) {
					limit := intArg(args, "limit", 50, 1, 200)
					ids := snap.GetOrphanNodes()
					total := len(ids)
					out := resolveBriefs(snap, ids, limit)
					return map[string]any{"count": total, "nodes": out}, nil
				})
			},
		},
		{
			Name:        "akg_god_objects",
			Description: "Detect god objects: STRUCT/CLASS/MODULE/FILE nodes with abnormally high fan-in and fan-out.",
			Category:    CategoryAKG,
			Parameters:  Schema(nil),
			Handler: func(ctx context.Context, env *Env, args map[string]any) (any, error) {
				return withSnapshot(env, func(snap *akg.CodePropertyGraph) (any, error) {
					ids := snap.DetectGodObjects()
					out := resolveBriefs(snap, ids, 100)
					return map[string]any{"count": len(ids), "nodes": out}, nil
				})
			},
		},
		{
			Name:        "akg_hotspots",
			Description: "Top nodes by total degree (inbound + outbound edges): the busiest symbols in the graph.",
			Category:    CategoryAKG,
			Parameters: Schema(map[string]Prop{
				"limit": {Type: "integer", Description: "How many to return (default 10, max 50)", Default: float64(10)},
			}),
			Handler: func(ctx context.Context, env *Env, args map[string]any) (any, error) {
				return withSnapshot(env, func(snap *akg.CodePropertyGraph) (any, error) {
					limit := intArg(args, "limit", 10, 1, 50)
					type score struct {
						Node   nodeBrief `json:"node"`
						Degree int       `json:"degree"`
						In     int       `json:"in"`
						Out    int       `json:"out"`
					}
					hot := make([]score, 0, snap.Nodes.Len())
					snap.Nodes.Iterate(func(id string, _ *stage4.ResolvedNode) {
						hot = append(hot, score{Node: brief(mustNode(snap, id)), Degree: 0})
					})
					// Compute degrees via the adjacency maps.
					indeg := map[string]int{}
					outdeg := map[string]int{}
					snap.InboundEdges.Iterate(func(id string, edges []stage4.ResolvedEdge) { indeg[id] = len(edges) })
					snap.OutboundEdges.Iterate(func(id string, edges []stage4.ResolvedEdge) { outdeg[id] = len(edges) })
					for i := range hot {
						hot[i].In = indeg[hot[i].Node.ID]
						hot[i].Out = outdeg[hot[i].Node.ID]
						hot[i].Degree = hot[i].In + hot[i].Out
					}
					sort.Slice(hot, func(i, j int) bool { return hot[i].Degree > hot[j].Degree })
					if len(hot) > limit {
						hot = hot[:limit]
					}
					return map[string]any{"count": len(hot), "hotspots": hot}, nil
				})
			},
		},
		{
			Name:        "akg_page_rank",
			Description: "PageRank scores over the AKG graph: the most structurally important nodes.",
			Category:    CategoryAKG,
			Parameters: Schema(map[string]Prop{
				"iterations": {Type: "integer", Description: "Power-iteration count (default 20)", Default: float64(20)},
				"damping":    {Type: "number", Description: "Damping factor (default 0.85)", Default: float64(0.85)},
				"top":        {Type: "integer", Description: "How many top nodes to return (default 10, max 50)", Default: float64(10)},
			}),
			Handler: func(ctx context.Context, env *Env, args map[string]any) (any, error) {
				return withSnapshot(env, func(snap *akg.CodePropertyGraph) (any, error) {
					ranks := snap.CalculatePageRank(intArg(args, "iterations", 20, 1, 100), floatArg(args, "damping", 0.85, 0.1, 1.0))
					top := intArg(args, "top", 10, 1, 50)
					type entry struct {
						Node nodeBrief `json:"node"`
						Rank float64   `json:"rank"`
					}
					sorted := make([]entry, 0, len(ranks))
					for id, r := range ranks {
						n := nodeBrief{ID: id}
						if node, ok := snap.GetNode(id); ok {
							n = brief(node)
						}
						sorted = append(sorted, entry{Node: n, Rank: r})
					}
					sort.Slice(sorted, func(i, j int) bool { return sorted[i].Rank > sorted[j].Rank })
					if len(sorted) > top {
						sorted = sorted[:top]
					}
					return map[string]any{"total": len(ranks), "top": sorted}, nil
				})
			},
		},
		{
			Name:        "akg_impact_radius",
			Description: "Blast radius of a node: how many nodes are upstream of it (would be affected if it changed).",
			Category:    CategoryAKG,
			Parameters: Schema(map[string]Prop{
				"id": {Type: "string", Description: "Full node ID", Required: true},
			}),
			Handler: func(ctx context.Context, env *Env, args map[string]any) (any, error) {
				return withSnapshot(env, func(snap *akg.CodePropertyGraph) (any, error) {
					id := strArg(args, "id", "")
					if _, ok := snap.GetNode(id); !ok {
						return nil, fmt.Errorf("node %q not found in the AKG", id)
					}
					count := snap.CalculateImpactRadius(id)
					affected := upstreamNodes(snap, id, 100)
					return map[string]any{
						"node":           id,
						"affected_count": count,
						"affected_nodes": affected,
					}, nil
				})
			},
		},
		{
			Name:        "akg_communities",
			Description: "Community structure of the AKG via label propagation: tightly coupled clusters of nodes.",
			Category:    CategoryAKG,
			Parameters: Schema(map[string]Prop{
				"iterations": {Type: "integer", Description: "Label-propagation rounds (default 10)", Default: float64(10)},
				"top":        {Type: "integer", Description: "How many clusters to return (default 5, max 10)", Default: float64(5)},
			}),
			Handler: func(ctx context.Context, env *Env, args map[string]any) (any, error) {
				return withSnapshot(env, func(snap *akg.CodePropertyGraph) (any, error) {
					clusters := communities(snap, intArg(args, "iterations", 10, 1, 50))
					top := intArg(args, "top", 5, 1, 10)
					if len(clusters) > top {
						clusters = clusters[:top]
					}
					type cluster struct {
						Size    int         `json:"size"`
						Members []nodeBrief `json:"members"`
					}
					out := make([]cluster, 0, len(clusters))
					for _, ids := range clusters {
						out = append(out, cluster{Size: len(ids), Members: resolveBriefs(snap, ids, 20)})
					}
					return map[string]any{"clusters": out}, nil
				})
			},
		},
		{
			Name:        "akg_articulation_points",
			Description: "Articulation (cut) vertices: nodes whose removal would disconnect the dependency graph.",
			Category:    CategoryAKG,
			Parameters:  Schema(nil),
			Handler: func(ctx context.Context, env *Env, args map[string]any) (any, error) {
				return withSnapshot(env, func(snap *akg.CodePropertyGraph) (any, error) {
					ids := snap.FindArticulationPoints()
					out := resolveBriefs(snap, ids, 100)
					return map[string]any{"count": len(ids), "nodes": out}, nil
				})
			},
		},
		{
			Name:        "akg_topological_order",
			Description: "A topological (dependency-respecting) order of the graph. cyclic=true means the graph has cycles and the order is partial.",
			Category:    CategoryAKG,
			Parameters: Schema(map[string]Prop{
				"limit": {Type: "integer", Description: "Max nodes returned (default 200, max 500)", Default: float64(200)},
			}),
			Handler: func(ctx context.Context, env *Env, args map[string]any) (any, error) {
				return withSnapshot(env, func(snap *akg.CodePropertyGraph) (any, error) {
					order, cyclic := snap.GetTopologicalSort()
					total := len(order)
					limit := intArg(args, "limit", 200, 1, 500)
					out := resolveBriefs(snap, order, limit)
					return map[string]any{"cyclic": cyclic, "count": total, "nodes": out}, nil
				})
			},
		},
		{
			Name:        "akg_entrypoints",
			Description: "List the recorded entry points of the repository (mains, endpoints, services).",
			Category:    CategoryAKG,
			Parameters: Schema(map[string]Prop{
				"limit": {Type: "integer", Description: "Max nodes returned (default 50, max 100)", Default: float64(50)},
			}),
			Handler: func(ctx context.Context, env *Env, args map[string]any) (any, error) {
				return withSnapshot(env, func(snap *akg.CodePropertyGraph) (any, error) {
					limit := intArg(args, "limit", 50, 1, 100)
					out := resolveBriefs(snap, snap.Entrypoints, limit)
					return map[string]any{"count": len(snap.Entrypoints), "entrypoints": out}, nil
				})
			},
		},
		{
			Name:        "akg_similarity",
			Description: "Structural similarity between two nodes (Jaccard over outbound targets) plus the shared-outbound count.",
			Category:    CategoryAKG,
			Parameters: Schema(map[string]Prop{
				"node_a": {Type: "string", Description: "Full node ID", Required: true},
				"node_b": {Type: "string", Description: "Full node ID", Required: true},
			}),
			Handler: func(ctx context.Context, env *Env, args map[string]any) (any, error) {
				return withSnapshot(env, func(snap *akg.CodePropertyGraph) (any, error) {
					a := strArg(args, "node_a", "")
					b := strArg(args, "node_b", "")
					for _, id := range []string{a, b} {
						if _, ok := snap.GetNode(id); !ok {
							return nil, fmt.Errorf("node %q not found in the AKG", id)
						}
					}
					sim := snap.GetStructuralSimilarity(a, b)
					targets := func(id string) map[string]bool {
						out := map[string]bool{}
						for _, e := range snap.GetOutboundEdges(id) {
							out[e.TargetID] = true
						}
						return out
					}
					ta, tb := targets(a), targets(b)
					shared := 0
					for t := range ta {
						if tb[t] {
							shared++
						}
					}
					return map[string]any{
						"node_a":          a,
						"node_b":          b,
						"similarity":      sim,
						"shared_outbound": shared,
						"a_outbound":      len(ta),
						"b_outbound":      len(tb),
					}, nil
				})
			},
		},
	}
}

// ---- shared helpers ----

// withSnapshot resolves the live AKG snapshot through the bridge, reporting
// actionable guidance when the repository has not been analyzed.
func withSnapshot(env *Env, fn func(*akg.CodePropertyGraph) (any, error)) (any, error) {
	if env == nil || env.Bridge == nil {
		return nil, fmt.Errorf("AKG access is not configured for this session")
	}
	snap, err := env.Bridge.Snapshot()
	if err != nil {
		return nil, err
	}
	return fn(snap)
}

type nodeBrief struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Primitive string `json:"primitive,omitempty"`
	Path      string `json:"path,omitempty"`
	Line      int    `json:"line,omitempty"`
}

type nodeFull struct {
	nodeBrief
	Properties map[string]string `json:"properties,omitempty"`
	Inbound    int               `json:"inbound_edges"`
	Outbound   int               `json:"outbound_edges"`
}

type edgeInfo struct {
	SourceID   string  `json:"source_id"`
	TargetID   string  `json:"target_id"`
	Type       string  `json:"type"`
	LineNumber int     `json:"line_number,omitempty"`
	Confidence float32 `json:"confidence,omitempty"`
	IsCycle    bool    `json:"is_cycle,omitempty"`
}

func brief(n *stage4.ResolvedNode) nodeBrief {
	return nodeBrief{
		ID:        n.ID,
		Name:      n.Name,
		Kind:      n.Kind,
		Primitive: n.Primitive,
		Path:      n.FileSpec.Path,
		Line:      n.FileSpec.LineStart,
	}
}

func mustNode(snap *akg.CodePropertyGraph, id string) *stage4.ResolvedNode {
	if n, ok := snap.GetNode(id); ok {
		return n
	}
	return &stage4.ResolvedNode{ID: id, Name: id, Kind: "UNKNOWN"}
}

func displayName(snap *akg.CodePropertyGraph, id string) string {
	if n, ok := snap.GetNode(id); ok {
		return n.Name
	}
	return id
}

func resolveBriefs(snap *akg.CodePropertyGraph, ids []string, limit int) []nodeBrief {
	out := make([]nodeBrief, 0, min(limit, len(ids)))
	for _, id := range ids {
		if len(out) >= limit {
			break
		}
		out = append(out, brief(mustNode(snap, id)))
	}
	return out
}

func edgeCount(snap *akg.CodePropertyGraph) int {
	total := 0
	snap.OutboundEdges.Iterate(func(_ string, edges []stage4.ResolvedEdge) { total += len(edges) })
	return total
}

func propsArg(v any) map[string]string {
	out := map[string]string{}
	if m, ok := v.(map[string]any); ok {
		for k, val := range m {
			if s, ok := val.(string); ok {
				out[k] = s
			}
		}
	}
	return out
}

// upstreamNodes returns node IDs reachable by walking inbound edges (the
// nodes that depend on id), capped at limit.
func upstreamNodes(snap *akg.CodePropertyGraph, id string, limit int) []string {
	seen := map[string]bool{}
	out := []string{}
	queue := []string{id}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, e := range snap.GetInboundEdges(cur) {
			if seen[e.SourceID] {
				continue
			}
			seen[e.SourceID] = true
			out = append(out, e.SourceID)
			if len(out) >= limit {
				return out
			}
			queue = append(queue, e.SourceID)
		}
	}
	return out
}

// communities runs label propagation over the snapshot adjacency and returns
// clusters (lists of node IDs) sorted by size descending. Deterministic:
// iteration follows the CowMap key order.
func communities(snap *akg.CodePropertyGraph, iterations int) [][]string {
	ids := snap.Nodes.Keys()
	index := make(map[string]int, len(ids))
	for i, id := range ids {
		index[id] = i
	}
	labels := make([]int, len(ids))
	for i := range labels {
		labels[i] = i
	}
	neighbors := func(id string) []string {
		seen := map[string]bool{}
		out := []string{}
		for _, e := range snap.GetOutboundEdges(id) {
			if !seen[e.TargetID] {
				seen[e.TargetID] = true
				out = append(out, e.TargetID)
			}
		}
		for _, e := range snap.GetInboundEdges(id) {
			if !seen[e.SourceID] {
				seen[e.SourceID] = true
				out = append(out, e.SourceID)
			}
		}
		return out
	}
	for iter := 0; iter < iterations; iter++ {
		changed := false
		for _, id := range ids {
			nbrs := neighbors(id)
			if len(nbrs) == 0 {
				continue
			}
			counts := map[int]int{}
			for _, n := range nbrs {
				counts[labels[index[n]]]++
			}
			best := labels[index[id]]
			bestCount := 0
			for l, c := range counts {
				if c > bestCount || (c == bestCount && l < best) {
					best = l
					bestCount = c
				}
			}
			if best != labels[index[id]] {
				labels[index[id]] = best
				changed = true
			}
		}
		if !changed {
			break
		}
	}
	byLabel := map[int][]string{}
	for i, id := range ids {
		byLabel[labels[i]] = append(byLabel[labels[i]], id)
	}
	clusters := make([][]string, 0, len(byLabel))
	for _, members := range byLabel {
		clusters = append(clusters, members)
	}
	sort.Slice(clusters, func(i, j int) bool { return len(clusters[i]) > len(clusters[j]) })
	return clusters
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
