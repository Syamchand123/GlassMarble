package stage3

import (
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage2"
	"strings"
)

// EscalatePrimitives performs a bottom-up traversal of the directory tree
// to classify entire folders as specific architectural zones (e.g. DATABASE_ZONE).
func EscalatePrimitives(dir *DirectoryNode) map[string]int {
	if dir == nil {
		return nil
	}

	dir.mu.Lock()
	defer dir.mu.Unlock()

	primitiveCounts := make(map[string]int)

	// 1. Gather from files
	for _, fileNode := range dir.Files {
		if fileNode == nil || fileNode.GASTRoot == nil {
			continue
		}
		gatherFilePrimitives(fileNode.GASTRoot, primitiveCounts)
	}

	// 2. Gather from SubFolders (Bottom-Up Post-Order)
	for _, subDir := range dir.SubFolders {
		subCounts := EscalatePrimitives(subDir)
		for k, v := range subCounts {
			primitiveCounts[k] += v
		}
	}

	// 3. Evaluate Dominant/Critical Zone
	dominantZone := ""
	highestCount := 0

	// Security and Crypto are highly infectious
	if primitiveCounts["SECURITY_SINK"] > 0 || primitiveCounts["CRYPTO_OPS"] > 0 {
		dominantZone = "SECURITY_ZONE"
	} else if primitiveCounts["DATABASE_IO"] > 0 || primitiveCounts["ORM_MODEL"] > 0 {
		dominantZone = "DATABASE_ZONE"
	} else {
		for prim, count := range primitiveCounts {
			if count > highestCount {
				highestCount = count
				dominantZone = prim + "_ZONE"
			}
		}
	}

	if dominantZone != "" {
		dir.PrimitiveZone = dominantZone
	}

	return primitiveCounts
}

func gatherFilePrimitives(node *stage2.GASTNode, counts map[string]int) {
	if node == nil {
		return
	}

	if node.Properties != nil {
		if prim := node.Properties["primitive"]; prim != "" {
			// standardizing names
			cleanPrim := strings.ToUpper(strings.TrimSpace(prim))
			counts[cleanPrim]++
		}
	}

	for _, child := range node.Children {
		gatherFilePrimitives(child, counts)
	}
}
