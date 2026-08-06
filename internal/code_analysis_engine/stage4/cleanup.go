package stage4

import (
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage3"
	"github.com/Syamchand123/GlassMarble/internal/product/ont"
)

// CleanupCPG is the post-merge cleanup pass (master_overhaul_plan.md
// §5.4.6/W1-14) run once per Link() after all producer passes:
//
//   - A-11: self-heals legacy mangled external node IDs
//     ("ext:akgerrs \"github.com/org/mod/...\"") into the canonical
//     ext:<url-escaped path> scheme, moving the module path/alias into
//     properties when available. Edge endpoints are rewritten in place.
//   - §5.4.7: every edge without an explicit gm:provenance gets the
//     "heuristic" default, so renderers can always filter by evidence.
func CleanupCPG(stage3Out *stage3.Stage3Output, cpg *Stage4Output) {
	if cpg == nil {
		return
	}

	modulePrefix := ""
	if stage3Out != nil && stage3Out.WorkspaceCtx != nil {
		modulePrefix = stage3Out.WorkspaceCtx.ModulePrefix
	}

	// 1. A-11: rewrite mangled ext: node IDs.
	renames := make(map[string]string)
	for id := range cpg.GraphNodes {
		if clean, ok := cleanExtID(id, modulePrefix); ok && clean != id {
			renames[id] = clean
		}
	}
	if len(renames) > 0 {
		rewriteNodeIDs(cpg, renames)
	}

	// 2. §5.4.7: default provenance on edges that lack it.
	applyProvenanceDefaults(cpg)
}

// cleanExtID normalizes an ext: node ID. Returns the canonical key and
// true when the input was a legacy mangled spelling:
//   - extracts the path from `ext:alias "path"` / `ext:path with spaces`
//   - strips a known workspace module prefix from the path (module path
//     belongs in properties, never in the URI — §4.1)
func cleanExtID(id, modulePrefix string) (string, bool) {
	if !strings.HasPrefix(id, ont.PrefixExt) {
		return "", false
	}

	raw := strings.TrimPrefix(id, ont.PrefixExt)
	// Canonical keys are url.PathEscape'd — literal quotes, spaces,
	// backslashes or slashes mark a legacy mangled spelling.
	if !strings.ContainsAny(raw, "\"' /\\") {
		return "", false // already canonical
	}

	// Extract the quoted path if present, else take everything up to the
	// first space (old aliases baked into the URI).
	path := raw
	if q := strings.IndexByte(raw, '"'); q != -1 {
		rest := raw[q+1:]
		if end := strings.IndexByte(rest, '"'); end != -1 {
			path = rest[:end]
		}
	} else if sp := strings.IndexAny(raw, " '"); sp != -1 {
		path = raw[sp+1:]
	}
	path = strings.TrimSpace(strings.Trim(path, "\"'"))

	// Module path is property material: strip a known prefix so
	// `ext:github.com/org/mod/internal/errors` → `ext:internal/errors`.
	if modulePrefix != "" {
		path = strings.TrimPrefix(path, modulePrefix)
		path = strings.TrimPrefix(path, "/")
	}
	if path == "" {
		return "", false
	}
	return stage3.ResolveExternalKey(path), true
}

// rewriteNodeIDs renames nodes and rewrites every edge endpoint, keeping
// the adjacency maps consistent (self-healing, read-only wrt serialized
// graphs: nothing mutates the caller's external dependency index).
func rewriteNodeIDs(cpg *Stage4Output, renames map[string]string) {
	for oldID, newID := range renames {
		if node, ok := cpg.GraphNodes[oldID]; ok {
			node.ID = newID
			cpg.GraphNodes[newID] = node
			delete(cpg.GraphNodes, oldID)
		}
	}

	outbound := cpg.OutboundEdges
	cpg.OutboundEdges = make(map[string][]ResolvedEdge, len(outbound))
	rewrite := func(id string) string {
		if n, ok := renames[id]; ok {
			return n
		}
		return id
	}
	for src, edges := range outbound {
		newSrc := rewrite(src)
		for _, e := range edges {
			e.SourceID = rewrite(e.SourceID)
			e.TargetID = rewrite(e.TargetID)
			if e.SourceID == "" || e.TargetID == "" || e.SourceID == e.TargetID {
				continue
			}
			cpg.OutboundEdges[newSrc] = append(cpg.OutboundEdges[newSrc], e)
		}
	}

	inbound := cpg.InboundEdges
	cpg.InboundEdges = make(map[string][]ResolvedEdge, len(inbound))
	for dst, edges := range inbound {
		newDst := rewrite(dst)
		for _, e := range edges {
			e.SourceID = rewrite(e.SourceID)
			e.TargetID = rewrite(e.TargetID)
			if e.SourceID == "" || e.TargetID == "" || e.SourceID == e.TargetID {
				continue
			}
			cpg.InboundEdges[newDst] = append(cpg.InboundEdges[newDst], e)
		}
	}
}

// applyProvenanceDefaults stamps gm:provenance "heuristic" on every edge
// whose producer did not record stronger evidence (§5.4.7).
func applyProvenanceDefaults(cpg *Stage4Output) {
	stamp := func(edges []ResolvedEdge) {
		for i := range edges {
			if edges[i].Properties[ont.PredProvenance] == "" {
				if edges[i].Properties == nil {
					edges[i].Properties = make(map[string]string)
				}
				edges[i].Properties[ont.PredProvenance] = "heuristic"
			}
		}
	}
	for src, edges := range cpg.OutboundEdges {
		stamp(edges)
		cpg.OutboundEdges[src] = edges
	}
	for dst, edges := range cpg.InboundEdges {
		stamp(edges)
		cpg.InboundEdges[dst] = edges
	}
}
