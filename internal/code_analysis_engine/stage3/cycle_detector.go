package stage3

import (
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage2"
)

// DetectArchitecturalCycles analyzes the GlobalCallQueue for circular dependencies between folders.
// It tags offending calls with "EdgeCycleViolation" primitive to alert Stage 4 and Visualization.
func DetectArchitecturalCycles(queue []LinkedCallSite) {
	// 1. Build a coarse-grained dependency graph: Folder -> Set of Folders
	folderGraph := make(map[string]map[string]bool)

	for _, call := range queue {
		srcFolder := call.SourceFolderPath
		// We use receiver or file hint to guess target folder at this stage
		// Since we don't have perfect Stage 4 linking yet, this is a coarse heuristic
		tgtFolder := guessTargetFolder(call)
		if srcFolder != "" && tgtFolder != "" && srcFolder != tgtFolder {
			if folderGraph[srcFolder] == nil {
				folderGraph[srcFolder] = make(map[string]bool)
			}
			folderGraph[srcFolder][tgtFolder] = true
		}
	}

	// 2. Tarjan's or simple DFS to find backedges
	visited := make(map[string]bool)
	recStack := make(map[string]bool)
	cycleEdges := make(map[string]map[string]bool) // src -> tgt -> true

	var dfs func(node string) bool
	dfs = func(node string) bool {
		visited[node] = true
		recStack[node] = true

		for neighbor := range folderGraph[node] {
			if !visited[neighbor] {
				if dfs(neighbor) {
					// Register backedge
					if cycleEdges[node] == nil {
						cycleEdges[node] = make(map[string]bool)
					}
					cycleEdges[node][neighbor] = true
				}
			} else if recStack[neighbor] {
				// Cycle detected
				if cycleEdges[node] == nil {
					cycleEdges[node] = make(map[string]bool)
				}
				cycleEdges[node][neighbor] = true
			}
		}
		recStack[node] = false
		return false // Always false here to keep searching all branches
	}

	for node := range folderGraph {
		if !visited[node] {
			dfs(node)
		}
	}

	// 3. Mark the queue
	for i := range queue {
		call := &queue[i]
		srcFolder := call.SourceFolderPath
		tgtFolder := guessTargetFolder(*call)

		if cycleEdges[srcFolder] != nil && cycleEdges[srcFolder][tgtFolder] {
			// Tag it
			call.HasPrimitive = true
			found := false
			for _, p := range call.Primitives {
				if p == stage2.PrimCycleViolation {
					found = true
					break
				}
			}
			if !found {
				call.Primitives = append(call.Primitives, stage2.PrimCycleViolation)
			}
		}
	}
}

func guessTargetFolder(call LinkedCallSite) string {
	// A rough heuristic for architectural bounds at Stage 3
	// e.g. ReceiverName might be "database.DB", we extract "database"
	parts := strings.Split(call.ReceiverName, ".")
	if len(parts) > 1 {
		return parts[0]
	}
	return ""
}
