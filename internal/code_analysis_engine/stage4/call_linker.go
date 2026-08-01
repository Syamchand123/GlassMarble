package stage4

import (
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage3"
	"path/filepath"
	"strings"
)

// LinkCallGraph processes Stage 3's GlobalCallQueue to draw CALLS edges across the CPG.
func LinkCallGraph(stage3Out *stage3.Stage3Output, cpg *Stage4Output) {
	if stage3Out == nil || cpg == nil {
		return
	}

	om := ownershipMap(cpg, stage3Out)

	for _, callSite := range stage3Out.GlobalCallQueue {
		if len(cpg.ModifiedFiles) > 0 && !cpg.ModifiedFiles[stage3.NormalizeRelativePath(callSite.SourceFilePath)] {
			continue
		}
		resolveCallSite(callSite, om, cpg, stage3Out)
	}
}

func resolveCallSite(callSite stage3.LinkedCallSite, om *stage3.OwnershipMap, cpg *Stage4Output, stage3Out *stage3.Stage3Output) {
	callerID := callSite.SourceFileNodeID
	if callerID == "" || !strings.Contains(callerID, "::") {
		callerID = "file:" + stage3.NormalizeRelativePath(callSite.SourceFilePath)
	}

	receiver := callSite.ReceiverName
	method := callSite.MethodName

	if method == "" {
		return
	}

	// 1. Check if receiver is a known interface type (Polymorphic Call Resolution)
	interfaceFQN := resolveTypeToFQN(receiver, callSite.SourceFilePath, stage3Out.GlobalDefinitionIndex, cpg)
	if interfaceFQN != "" {
		if ifaceNode, exists := cpg.GetNode(interfaceFQN); exists && ifaceNode.Kind == "INTERFACE" {
			// Find all concrete implementations of this interface
			for _, edges := range cpg.OutboundEdges {
				for _, e := range edges {
					if e.TargetID == interfaceFQN && e.Type == EdgeImplements {
						structID := e.SourceID
						// Look for matching method on concrete struct
						targetMethodFQN := structID + "::" + method
						if _, ok := cpg.GetNode(targetMethodFQN); ok {
							cpg.AddEdge(callerID, targetMethodFQN, EdgeCalls, callSite.LineNumber)
						}
					}
				}
			}
			return
		}
	}

	// 2. 0-CFA Data-Flow Assisted Call Resolution (Higher-Order Functions / Closures)
	if receiver == "" {
		// method is acting as a variable name here, e.g., "x()"
		varID := callerID + "::VAR_" + method
		// Find if this variable exists in our DFG
		if _, ok := cpg.GetNode(varID); ok {
			// Trace data flow to find the function it points to
			edges := cpg.OutboundEdges[varID]
			if len(edges) == 0 && cpg.db != nil {
				edges = cpg.db.GetOutboundEdges(varID)
			}
			for _, e := range edges {
				if e.Type == EdgeDataFlow {
					targetNode, exists := cpg.GetNode(e.TargetID)
					if exists && targetNode.Kind == "FUNCTION" {
						cpg.AddEdge(callerID, e.TargetID, EdgeCalls, callSite.LineNumber)
						return // Solved dynamically
					}
				}
			}
		}
	}

	// 3. Direct Static Target Resolution
	targetFQN, conf := resolveCallTarget(receiver, method, callSite.SourceFilePath, callSite.LocalImports, om, cpg, stage3Out)
	if targetFQN != "" && callerID != targetFQN {
		cpg.AddEdgeWithConfidence(callerID, targetFQN, EdgeCalls, callSite.LineNumber, conf)

		// 1-CFA: Context-Sensitive Resolution for High-Risk utility functions.
		// The VIRTUAL_CONTEXT nodes are synthetic noise; kept only behind full
		// mode (AUDIT Issue 1.4 / Phase 1B-5).
		if isFullMode(cpg) && isHighRiskUtility(method, targetFQN) {
			contextNodeID := targetFQN + "@ctx(" + callerID + ")"

			// Create a virtual contextual node if it doesn't exist
			if _, exists := cpg.GetNode(contextNodeID); !exists {
				cpg.GraphNodes[contextNodeID] = &ResolvedNode{
					ID:   contextNodeID,
					Kind: "VIRTUAL_CONTEXT",
					Name: method + " (Context: " + callerID + ")",
					Properties: map[string]string{
						"base_target": targetFQN,
						"caller_ctx":  callerID,
					},
				}
				// The virtual node is a specialization of the real node
				cpg.AddEdge(contextNodeID, targetFQN, EdgeInstantiates, 0)
			}

			cpg.AddEdge(callerID, contextNodeID, EdgeContextCall, callSite.LineNumber)
		}
		// 3. Mark Cycle Violation if detected in Stage 3
		isCycle := false
		for _, p := range callSite.Primitives {
			if string(p) == "CYCLE_VIOLATION" || string(p) == "EdgeCycleViolation" {
				isCycle = true
				break
			}
		}

		if isCycle {
			// Find the exact edge we just added (or that already existed) and mark it
			edgesOut := cpg.OutboundEdges[callerID]
			for i := range edgesOut {
				if edgesOut[i].TargetID == targetFQN && edgesOut[i].Type == EdgeCalls && edgesOut[i].LineNumber == callSite.LineNumber {
					cpg.OutboundEdges[callerID][i].IsCycle = true
					break
				}
			}
			edgesIn := cpg.InboundEdges[targetFQN]
			for i := range edgesIn {
				if edgesIn[i].SourceID == callerID && edgesIn[i].Type == EdgeCalls && edgesIn[i].LineNumber == callSite.LineNumber {
					cpg.InboundEdges[targetFQN][i].IsCycle = true
					break
				}
			}
		}
	}
}

func isHighRiskUtility(method, fqn string) bool {
	l := strings.ToLower(method)
	if strings.Contains(l, "log") || strings.Contains(l, "print") || strings.Contains(l, "query") || strings.Contains(l, "exec") || strings.Contains(l, "send") {
		return true
	}
	return false
}

func getResolver(ext string) stage3.ImportResolver {
	switch ext {
	case ".go":
		return &stage3.GoImportResolver{}
	case ".py":
		return &stage3.PythonImportResolver{}
	case ".java":
		return &stage3.JavaImportResolver{}
	case ".ts", ".tsx", ".js", ".jsx":
		return &stage3.TSImportResolver{}
	default:
		return &stage3.GenericImportResolver{}
	}
}

func resolveCallTarget(receiver, method, filePath string, localImports []string, om *stage3.OwnershipMap, cpg *Stage4Output, stage3Out *stage3.Stage3Output) (string, float32) {
	cleanPath := stage3.NormalizeRelativePath(filePath)
	ext := filepath.Ext(cleanPath)

	// Deconstruct nested dot selectors in method name
	if strings.Contains(method, ".") {
		parts := strings.Split(method, ".")
		method = parts[len(parts)-1]
		if receiver != "" {
			receiver = receiver + "." + strings.Join(parts[:len(parts)-1], ".")
		} else {
			receiver = strings.Join(parts[:len(parts)-1], ".")
		}
	}

	// Step 3.7: Generics Resolution
	if stage3Out != nil && stage3Out.GenericsRegistry != nil {
		if idx := strings.Index(receiver, "<"); idx != -1 {
			base := receiver[:idx]
			if _, ok := stage3Out.GenericsRegistry[base]; ok {
				receiver = base // Map to base template
			}
		}
		if idx := strings.Index(receiver, "["); idx != -1 {
			base := receiver[:idx]
			if _, ok := stage3Out.GenericsRegistry[base]; ok {
				receiver = base
			}
		}
	}

	// Attempt 0: Direct Universal ID match (same file exact match)
	if receiver != "" {
		targetID := BuildUniversalID(cleanPath, receiver, method)
		if _, exists := cpg.GetNode(targetID); exists {
			return targetID, 1.0
		}
	} else {
		targetID := BuildUniversalID(cleanPath, "", method)
		if _, exists := cpg.GetNode(targetID); exists {
			return targetID, 1.0
		}
	}

	// Attempt 1: Direct import path resolution via OwnershipMap.ByImport
	if om != nil && localImports != nil {
		resolver := getResolver(ext)
		for _, imp := range localImports {
			resolvedPaths := resolver.Resolve(imp, cleanPath, ".", stage3Out.WorkspaceCtx)
			for _, rp := range resolvedPaths {
				if entries, ok := om.ByImport[rp]; ok {
					for _, entry := range entries {
						if entry.Name == method && (receiver == "" || receiverMatchesTarget(receiver, entry.FQN, entry.ReceiverType)) {
							targetID := BuildUniversalID(entry.FilePath, entry.ReceiverType, entry.Name)
							if _, ok := cpg.GetNode(targetID); ok {
								return targetID, 0.7
							}
						}
					}
				}
			}
		}
	}

	// Attempt 2 & 3: Check GlobalDefinitionIndex by FQN / Symbol Name
	if stage3Out != nil && stage3Out.GlobalDefinitionIndex != nil {
		globalIndex := stage3Out.GlobalDefinitionIndex
		if receiver != "" {
			fqn := receiver + "." + method
			if nodes, exists := globalIndex[fqn]; exists && len(nodes) > 0 {
				node := nodes[0]
				// Use method as fallback when the node has no Name (e.g. hand-crafted test fixtures)
				resolvedName := node.Name
				if resolvedName == "" {
					resolvedName = method
				}
				targetID := BuildUniversalID(node.Properties["file_path"], node.ReceiverType, resolvedName)
				if _, ok := cpg.GetNode(targetID); ok {
					return targetID, 0.5
				}
			}
		} else {
			if nodes, exists := globalIndex[method]; exists && len(nodes) > 0 {
				node := nodes[0]
				resolvedName := node.Name
				if resolvedName == "" {
					resolvedName = method
				}
				targetID := BuildUniversalID(node.Properties["file_path"], node.ReceiverType, resolvedName)
				if _, ok := cpg.GetNode(targetID); ok {
					return targetID, 0.5
				}
			}
		}
	}

	// Attempt 4: Signature-aware resolution via the global symbol index.
	// Unlike the old first-match scan over every graph node, this is an O(1)
	// map lookup filtered by an exact receiver-type match, so it never returns
	// a plausible-looking but wrong target (AUDIT Issue 1.6 / Phase 1B-7).
	if stage3Out != nil && stage3Out.GlobalDefinitionIndex != nil {
		if nodes, exists := stage3Out.GlobalDefinitionIndex[method]; exists {
			for _, node := range nodes {
				if node == nil {
					continue
				}
				if receiver != "" && !receiverTypeMatches(receiver, node.ReceiverType, node.Properties["receiver_type"]) {
					continue
				}
				resolvedName := node.Name
				if resolvedName == "" {
					resolvedName = method
				}
				targetID := BuildUniversalID(node.Properties["file_path"], node.ReceiverType, resolvedName)
				if _, ok := cpg.GetNode(targetID); ok {
					return targetID, 0.5
				}
			}
		}
	}

	// Attempt 5: Fallback to Step 3.6 External Dependencies
	if stage3Out != nil && stage3Out.ExternalDependencies != nil {
		for _, imp := range localImports {
			if stage3.IsStdlibImport(imp, filePath) {
				continue
			}
			if extNode, ok := stage3Out.ExternalDependencies[imp]; ok {
				// Inject the external node into CPG if not present
				extID := "ext:" + imp
				if _, exists := cpg.GetNode(extID); !exists {
					cpg.GraphNodes[extID] = &ResolvedNode{
						ID:         extID,
						Kind:       "EXTERNAL_SDK",
						Name:       imp,
						Properties: extNode.Properties,
					}
				}
				// We link to the specific method/API on the external SDK
				apiID := extID + "::" + method
				if receiver != "" {
					apiID = extID + "::" + receiver + "." + method
				}
				if _, exists := cpg.GetNode(apiID); !exists {
					props := map[string]string{"primitive": "EXTERNAL_SDK_CALL"}
					cpg.GraphNodes[apiID] = &ResolvedNode{
						ID:         apiID,
						Kind:       "EXTERNAL_API",
						Name:       method,
						Properties: props,
					}
					cpg.AddEdge(extID, apiID, EdgeContains, 0)
				}
				return apiID, 0.9 // High confidence because we matched the exact external import
			}
		}
	}

	return "", 0.0
}

// receiverTypeMatches reports whether a call-site receiver name corresponds to
// a target's declared receiver type. It compares the last dot-segment of the
// receiver (e.g. "a.Store", "store", "Store") against the target's receiver
// type exactly — no substring fuzz, so it can never match "r" because
// "store" happens to contain it.
func receiverTypeMatches(receiver, receiverType, receiverTypeProp string) bool {
	if receiver == "" || receiverType == "" {
		return false
	}
	rec := strings.ToLower(receiver)
	if idx := strings.LastIndex(rec, "."); idx != -1 {
		rec = rec[idx+1:]
	}
	rec = strings.ReplaceAll(rec, "_", "")

	typ := strings.ToLower(receiverType)
	typ = strings.ReplaceAll(typ, "_", "")
	if rec == typ {
		return true
	}

	// Fall back to the property-form receiver type if set and different
	// (e.g. Go translator stores both receiver_type and node.ReceiverType).
	typProp := strings.ToLower(receiverTypeProp)
	typProp = strings.ReplaceAll(typProp, "_", "")
	return typProp != "" && rec == typProp
}

func receiverMatchesTarget(receiver, targetID, receiverType string) bool {
	if receiver == "" {
		return true
	}

	// Normalize to lowercase
	rec := strings.ToLower(receiver)
	tgt := strings.ToLower(targetID)
	recT := strings.ToLower(receiverType)

	// Get last part of receiver (e.g. "a.Store" -> "store")
	if idx := strings.LastIndex(rec, "."); idx != -1 {
		rec = rec[idx+1:]
	}

	// Strip common prefixes/suffixes
	cleanRec := rec
	cleanRec = strings.TrimSuffix(cleanRec, "_client")
	cleanRec = strings.TrimSuffix(cleanRec, "_service")
	cleanRec = strings.TrimSuffix(cleanRec, "_manager")
	cleanRec = strings.TrimSuffix(cleanRec, "_mgr")
	cleanRec = strings.TrimSuffix(cleanRec, "_store")
	cleanRec = strings.ReplaceAll(cleanRec, "_", "")

	cleanTgt := tgt
	cleanTgt = strings.ReplaceAll(cleanTgt, "_", "")

	cleanRecT := recT
	cleanRecT = strings.ReplaceAll(cleanRecT, "_", "")

	// 1. Direct contains check
	if strings.Contains(cleanTgt, cleanRec) || (cleanRecT != "" && strings.Contains(cleanRec, cleanRecT)) {
		return true
	}

	// 2. Fallback to check if they share a common root (longer than 3 chars)
	if len(cleanRec) >= 3 {
		if (cleanRecT != "" && strings.Contains(cleanRecT, cleanRec)) || (cleanRecT != "" && strings.Contains(cleanRec, cleanRecT)) {
			return true
		}
	}

	return false
}
