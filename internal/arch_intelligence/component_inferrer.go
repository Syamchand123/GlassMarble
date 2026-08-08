package arch_intelligence

import (
	"path/filepath"
	"strings"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/archmodel"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage4"
	"github.com/Syamchand123/GlassMarble/internal/evidence"
)

// InferComponents uses Louvain community detection and directory structures
// to group low-level nodes into high-level architectural components.
func InferComponents(graph *akg.CodePropertyGraph) []archmodel.DetectedComponent {
	comms := LouvainCommunityDetection(graph)

	// Group nodes by community ID
	commNodes := make(map[string][]string)
	graph.Nodes.Iterate(func(id string, _ *stage4.ResolvedNode) {
		cID := comms[id]
		commNodes[cID] = append(commNodes[cID], id)
	})

	var components []archmodel.DetectedComponent

	for commID, nodes := range commNodes {
		// Try to find a dominant directory for this community to name it
		dirCounts := make(map[string]int)
		for _, id := range nodes {
			if node, ok := graph.SafeGetNode(id); ok {
				if node.FileSpec.Path != "" {
					dir := filepath.Dir(node.FileSpec.Path)
					dir = strings.ReplaceAll(dir, "\\", "/")
					dirCounts[dir]++
				}
			}
		}

		bestDir := "unknown"
		bestCount := 0
		for dir, count := range dirCounts {
			if count > bestCount {
				bestCount = count
				bestDir = dir
			}
		}

		name := "Component " + commID
		if bestDir != "unknown" && bestDir != "." {
			parts := strings.Split(bestDir, "/")
			name = "Component " + parts[len(parts)-1]
		}

		b := evidence.Bundle{}
		b.Add(evidence.EvidenceItem{
			Source:     evidence.SourceRule,
			Reference:  "ComponentInference",
			Excerpt:    "Inferred via Louvain modularity optimization.",
			Confidence: 0.8,
			Timestamp:  time.Now(),
		})

		components = append(components, archmodel.DetectedComponent{
			ID:          "comp_" + commID,
			Name:        name,
			Directories: []string{bestDir},
			NodeIDs:     nodes,
			Confidence:  0.8,
			Evidence:    b,
		})
	}

	return components
}
