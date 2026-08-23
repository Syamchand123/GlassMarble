package link

// LinkSecurityVulnerabilities traces Data Flow (DFG) edges from untrusted sources to dangerous sinks.
func LinkSecurityVulnerabilities(cpg *LinkOutput) {
	if cpg == nil {
		return
	}

	var sources []string
	var sinks []string

	for id, node := range cpg.GraphNodes {
		if node.Primitive != "" || len(node.PrimitiveScores) > 0 {
			// Sources: external network input handlers (the entry points of untrusted data)
			if hasScore(node, "NETWORK_IO") || hasScore(node, "RPC") {
				sources = append(sources, id)
			}
			// Sinks: dangerous operations where tainted data must not reach
			if hasScore(node, "DATABASE_SQL") || hasScore(node, "DATABASE_NOSQL") || hasScore(node, "DISK_IO") || hasScore(node, "IPC") {
				sinks = append(sinks, id)
			}
		}
	}

	if cpg.db != nil {
		globalFuncs := cpg.db.GetNodesByKind("FUNCTION")
		for _, node := range globalFuncs {
			if node.Primitive != "" || len(node.PrimitiveScores) > 0 {
				if hasScore(node, "NETWORK_IO") || hasScore(node, "RPC") {
					sources = append(sources, node.ID)
				}
				if hasScore(node, "DATABASE_SQL") || hasScore(node, "DATABASE_NOSQL") || hasScore(node, "DISK_IO") || hasScore(node, "IPC") {
					sinks = append(sinks, node.ID)
				}
			}
		}
	}

	sinkMap := make(map[string]bool)
	for _, s := range sinks {
		sinkMap[s] = true
	}

	for _, src := range sources {
		visited := make(map[string]bool)
		dfsTaint(src, src, cpg, sinkMap, visited, 0)
	}
}

func hasScore(node *ResolvedNode, prim string) bool {
	if node.PrimitiveScores != nil && node.PrimitiveScores[prim] > 0 {
		return true
	}
	return false
}

func dfsTaint(originID string, currentID string, cpg *LinkOutput, sinks map[string]bool, visited map[string]bool, depth int) {
	if depth > 15 {
		return
	}
	if visited[currentID] {
		return
	}
	visited[currentID] = true

	if sinks[currentID] && originID != currentID {
		cpg.AddEdge(originID, currentID, EdgeVulnerable, 0)
		return
	}

	edges := cpg.OutboundEdges[currentID]
	if len(edges) == 0 && cpg.db != nil {
		edges = cpg.db.GetOutboundEdges(currentID)
	}

	for _, e := range edges {
		// Traverse DataFlow, normal Calls, and Context-Sensitive Calls
		if e.Type == EdgeDataFlow || e.Type == EdgeCalls || e.Type == EdgeContextCall {
			if isSanitizer(e.TargetID, cpg) {
				continue
			}
			dfsTaint(originID, e.TargetID, cpg, sinks, visited, depth+1)
		}
	}
}

func isSanitizer(id string, cpg *LinkOutput) bool {
	node, exists := cpg.GetNode(id)
	if !exists {
		return false
	}
	if hasScore(node, "SANITIZER") || hasScore(node, "VALIDATOR") {
		return true
	}
	return false
}
