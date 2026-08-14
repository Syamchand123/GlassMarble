package link

import (
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/aggregate"
	"strings"
)

// LinkCrossLanguageRPC constructs EdgeNetworkCall edges linking client fetch/axios calls
// to backend REST/gRPC handlers based on path strings.
func LinkCrossLanguageRPC(aggregateOut *aggregate.AggregateOutput, cpg *LinkOutput) {
	if aggregateOut == nil || cpg == nil {
		return
	}

	endpoints := make(map[string]string) // Route Path -> Handler ID

	// 1. Find all handlers (backend) from Phase 2.3 Semantic Map
	for _, symTable := range aggregateOut.LocalTables {
		if symTable == nil {
			continue
		}
		for _, ep := range symTable.Endpoints {
			if ep.Route != "" && ep.Route != "unknown" {
				// Normalize route for heuristic matching
				cleanRoute := strings.TrimSpace(ep.Route)
				// The handler node ID is typically the file level, but we link to the virtual endpoint node created by semantic_linker
				targetID := "endpoint:" + ep.Method + ":" + ep.Route
				endpoints[cleanRoute] = targetID
			}
		}
	}

	// 2. Find all fetch calls (frontend)
	for _, call := range aggregateOut.GlobalCallQueue {
		// Only check modified files for callers to maintain O(1)
		if len(cpg.ModifiedFiles) > 0 && !cpg.ModifiedFiles[aggregate.NormalizeRelativePath(call.SourceFilePath)] {
			continue
		}

		if call.MethodName == "fetch" || call.MethodName == "axios" || call.MethodName == "get" || call.MethodName == "post" {
			// Get the actual GAST node to inspect its raw source content for the URL
			callerNode, exists := cpg.GetNode(call.SourceFileNodeID)
			if !exists {
				continue
			}

			content := strings.ToLower(callerNode.Properties["content"])
			if content == "" {
				continue
			}

			// Try to extract hardcoded path from call arguments
			for route, targetID := range endpoints {
				routeLower := strings.ToLower(route)
				// Strip common annotation syntax (e.g. @GetMapping("/api/users") -> /api/users)
				if idx := strings.Index(routeLower, "(\""); idx != -1 {
					endIdx := strings.LastIndex(routeLower, "\")")
					if endIdx > idx {
						routeLower = routeLower[idx+2 : endIdx]
					}
				}

				// Crude heuristic for cross language link
				if routeLower != "" && strings.Contains(content, routeLower) {
					ensureVirtualNode(targetID, "VIRTUAL_ENDPOINT", "RPC:"+route, cpg)
					cpg.AddEdge(call.SourceFileNodeID, targetID, EdgeNetworkCall, call.LineNumber)
				}
			}
		}
	}
}
