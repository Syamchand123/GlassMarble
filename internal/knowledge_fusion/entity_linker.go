package knowledge_fusion

import (
	"sort"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage4"
	"github.com/Syamchand123/GlassMarble/internal/developer_memory"
)

// LinkDocumentClaimsToAKG maps entity mentions and file paths in claims to
// AKG node IDs — WITHOUT destroying what it links.
//
// Linking discipline (this file is the contract):
//
//   - The claim's Subject and Object TEXT is always preserved verbatim, so
//     every persisted claim stays text-queryable ("gmb memory --ask redis"
//     keeps matching "Redis" long after linking). Resolved node IDs land in
//     the additive SubjectID/ObjectID fields.
//   - Object is linked first, then Subject, so a claim whose subject is a
//     file path and whose object is a node name gets both resolved.
//   - When the subject is a file path known to the graph, the claim is
//     expanded into one claim per node defined in that file — each
//     expansion carrying the same human-readable subject text and its own
//     SubjectID. Claim IDs incorporate the SubjectID (see fusedClaimID), so
//     the memory store's ID dedup keeps every expansion instead of silently
//     dropping all but the first.
//   - A file path with no nodes in the graph keeps the claim as-is with an
//     empty SubjectID: the file may have been deleted or never parsed, and
//     the claim (e.g. "auth/service.go was_modified_by_pr 42") is still
//     historically meaningful. Claims are never dropped during linking.
//   - The global "architecture" subject is never expanded.
//
// The output preserves input order and is fully deterministic: node-name
// collisions are resolved by the lowest node ID, and expansions are ordered
// by node ID.
func LinkDocumentClaimsToAKG(claims []developer_memory.KnowledgeClaim, graph *akg.CodePropertyGraph) []developer_memory.KnowledgeClaim {
	exactMap, lowerMap := buildNodeNameIndex(graph)

	var linked []developer_memory.KnowledgeClaim
	for _, claim := range claims {
		claim.ObjectID = resolveName(claim.Object, exactMap, lowerMap)
		claim.SubjectID = resolveName(claim.Subject, exactMap, lowerMap)

		// File-path subjects expand to per-node claims (except the global
		// "architecture" subject, which is never a file path).
		if claim.SubjectID == "" && graph != nil && graph.FileNodeIndex != nil {
			if nodeIDs, ok := graph.FileNodeIndex.Get(claim.Subject); ok {
				ids := sortedNodeIDs(nodeIDs)
				if len(ids) == 0 {
					linked = append(linked, claim)
					continue
				}
				for _, id := range ids {
					expanded := claim
					expanded.SubjectID = id
					// Re-derive the ID so each expansion is distinct in the
					// memory store's dedup (the subject text stays the file
					// path for queryability).
					expanded.ID = fusedClaimID(
						"file", evidenceReference(claim.Evidence), claim.Subject,
						claim.Predicate, claim.Object, id, claim.ObjectID,
					)
					linked = append(linked, expanded)
				}
				continue
			}
		}

		linked = append(linked, claim)
	}
	return linked
}

// buildNodeNameIndex indexes every graph node by exact name and by
// lowercased name. Collisions (several nodes sharing a name) resolve
// deterministically to the lexicographically lowest node ID.
func buildNodeNameIndex(graph *akg.CodePropertyGraph) (exact, lower map[string]string) {
	exact = make(map[string]string)
	lower = make(map[string]string)
	if graph == nil || graph.Nodes == nil {
		return exact, lower
	}
	graph.Nodes.Iterate(func(id string, node *stage4.ResolvedNode) {
		if node == nil || node.Name == "" {
			return
		}
		claimID := func(m map[string]string, key string) {
			prev, ok := m[key]
			if !ok || id < prev {
				m[key] = id
			}
		}
		claimID(exact, node.Name)
		claimID(lower, strings.ToLower(node.Name))
	})
	return exact, lower
}

// resolveName returns the AKG node ID for a mention, or "" when the mention
// is not a graph node name. Exact matches win over case-insensitive matches.
func resolveName(name string, exact, lower map[string]string) string {
	if name == "" {
		return ""
	}
	if id, ok := exact[name]; ok {
		return id
	}
	return lower[strings.ToLower(name)]
}

// sortedNodeIDs returns the node IDs of a file's node set in deterministic
// (lexicographic) order.
func sortedNodeIDs(set map[string]bool) []string {
	ids := make([]string, 0, len(set))
	for id := range set {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
