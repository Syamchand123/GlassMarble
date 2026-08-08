package knowledge_fusion

import (
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage4"
	"github.com/Syamchand123/GlassMarble/internal/developer_memory"
)

// LinkDocumentClaimsToAKG maps entity mentions or file paths to AKG node IDs.
func LinkDocumentClaimsToAKG(
	claims []developer_memory.KnowledgeClaim,
	graph *akg.CodePropertyGraph,
) []developer_memory.KnowledgeClaim {
	
	exactMap := make(map[string]string)
	lowerMap := make(map[string]string)
	
	if graph.Nodes != nil {
		graph.Nodes.Iterate(func(id string, node *stage4.ResolvedNode) {
			exactMap[node.Name] = id
			lowerMap[strings.ToLower(node.Name)] = id
		})
	}

	var linkedClaims []developer_memory.KnowledgeClaim

	for _, claim := range claims {
		// Attempt name linking on Object first (so expanded claims inherit it)
		if id, found := exactMap[claim.Object]; found {
			claim.Object = id
		} else if id, found := lowerMap[strings.ToLower(claim.Object)]; found {
			claim.Object = id
		}

		// Does this claim's subject refer to a file path?
		if graph.FileNodeIndex != nil {
			if nodeIDsMap, found := graph.FileNodeIndex.Get(claim.Subject); found {
				// Expand the claim for every node in this file
				for nodeID := range nodeIDsMap {
					expandedClaim := claim
					expandedClaim.Subject = nodeID
					linkedClaims = append(linkedClaims, expandedClaim)
				}
				continue
			}
		}

		// Otherwise, attempt name linking on Subject
		if id, found := exactMap[claim.Subject]; found {
			claim.Subject = id
		} else if id, found := lowerMap[strings.ToLower(claim.Subject)]; found {
			claim.Subject = id
		}

		linkedClaims = append(linkedClaims, claim)
	}

	return linkedClaims
}
