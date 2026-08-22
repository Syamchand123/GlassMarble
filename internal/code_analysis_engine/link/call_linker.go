package link

import (
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/aggregate"
	"github.com/Syamchand123/GlassMarble/internal/product/ont"
)

// LinkCallGraph processes Aggregation's GlobalCallQueue to draw CALLS edges across the CPG.
func LinkCallGraph(aggregateOut *aggregate.AggregateOutput, cpg *LinkOutput) {
	if aggregateOut == nil || cpg == nil {
		return
	}

	om := ownershipMap(cpg, aggregateOut)

	for _, callSite := range aggregateOut.GlobalCallQueue {
		if len(cpg.ModifiedFiles) > 0 && !cpg.ModifiedFiles[aggregate.NormalizeRelativePath(callSite.SourceFilePath)] {
			continue
		}
		resolveCallSite(callSite, om, cpg, aggregateOut)
	}
}

func resolveCallSite(callSite aggregate.LinkedCallSite, om *aggregate.OwnershipMap, cpg *LinkOutput, aggregateOut *aggregate.AggregateOutput) {
	callerID := callSite.SourceFileNodeID
	if callerID == "" || !strings.Contains(callerID, "::") {
		callerID = "file:" + aggregate.NormalizeRelativePath(callSite.SourceFilePath)
	}

	receiver := callSite.ReceiverName
	method := callSite.MethodName

	if method == "" {
		return
	}

	// Type conversions (e.g. string(x), float64(v)) surface as call sites
	// whose "method" is a predeclared type name. No such symbol exists, so
	// never fabricate a CALLS edge for them.
	if isPredeclaredType(method) {
		return
	}

	// 1. Check if receiver is a known interface type (Polymorphic Call Resolution)
	interfaceFQN := resolveTypeToFQN(receiver, callSite.SourceFilePath, aggregateOut.GlobalDefinitionIndex, cpg)
	if interfaceFQN != "" {
		if ifaceNode, exists := cpg.GetNode(interfaceFQN); exists && ifaceNode.Kind == "INTERFACE" {
			// Find all concrete implementations of this interface
			for _, e := range cpg.InboundEdges[interfaceFQN] {
				if e.Type == EdgeImplements {
					structID := e.SourceID
					// Look for matching method on concrete struct
					targetMethodFQN := structID + "::" + method
					if _, ok := cpg.GetNode(targetMethodFQN); ok {
						cpg.AddEdge(callerID, targetMethodFQN, EdgeCalls, callSite.LineNumber)
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
	targetFQN, conf := resolveCallTarget(receiver, method, callSite.SourceFilePath, callSite.LocalImports, om, cpg, aggregateOut)
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
				// The virtual node is a specialization of the real node;
				// the dedicated predicate keeps gm:instantiatesGeneric
				// reserved for real generic instantiation (W1-18/A-18).
				cpg.AddEdge(contextNodeID, targetFQN, EdgeVirtualContext, 0)
			}

			cpg.AddEdge(callerID, contextNodeID, EdgeContextCall, callSite.LineNumber)
		}
		// 3. Mark Cycle Violation if detected in Aggregation
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

func getResolver(ext string) aggregate.ImportResolver {
	switch ext {
	case ".go":
		return &aggregate.GoImportResolver{}
	case ".py":
		return &aggregate.PythonImportResolver{}
	case ".java":
		return &aggregate.JavaImportResolver{}
	case ".ts", ".tsx", ".js", ".jsx":
		return &aggregate.TSImportResolver{}
	default:
		return &aggregate.GenericImportResolver{}
	}
}

func resolveCallTarget(receiver, method, filePath string, localImports []string, om *aggregate.OwnershipMap, cpg *LinkOutput, aggregateOut *aggregate.AggregateOutput) (string, float32) {
	cleanPath := aggregate.NormalizeRelativePath(filePath)
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
	if aggregateOut != nil && aggregateOut.GenericsRegistry != nil {
		if idx := strings.Index(receiver, "<"); idx != -1 {
			base := receiver[:idx]
			if _, ok := aggregateOut.GenericsRegistry[base]; ok {
				receiver = base // Map to base template
			}
		}
		if idx := strings.Index(receiver, "["); idx != -1 {
			base := receiver[:idx]
			if _, ok := aggregateOut.GenericsRegistry[base]; ok {
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
			resolvedPaths := resolver.Resolve(imp, cleanPath, ".", aggregateOut.WorkspaceCtx)
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
	if aggregateOut != nil && aggregateOut.GlobalDefinitionIndex != nil {
		globalIndex := aggregateOut.GlobalDefinitionIndex
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
	if aggregateOut != nil && aggregateOut.GlobalDefinitionIndex != nil {
		if nodes, exists := aggregateOut.GlobalDefinitionIndex[method]; exists {
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

	// Attempt 5: Fallback to Step 3.6 External Dependencies. The receiver's
	// first dot-segment must plausibly denote the import (explicit alias or
	// the import's final path segment, gopkg.in / major-version suffixes
	// stripped); otherwise the fabricated ext: API node would be noise, so
	// the edge is dropped instead (GAP-CALL-05). Bare calls never fabricate.
	if aggregateOut != nil && aggregateOut.ExternalDependencies != nil && receiver != "" {
		qualifier := receiver
		if idx := strings.Index(qualifier, "."); idx != -1 {
			qualifier = qualifier[:idx]
		}
		var aliases map[string]string
		if aggregateOut.LocalTables != nil {
			if st := aggregateOut.LocalTables[cleanPath]; st != nil {
				aliases = st.ImportAliases
			}
		}
		for _, imp := range localImports {
			if aggregate.IsStdlibImport(imp, filePath) {
				continue
			}
			if !qualifierMatchesImport(qualifier, imp, aliases) {
				continue
			}
			// v2 (W1-09): ext:<escaped> primary; raw v1 spelling self-healed
			// for reads (old caches/deps.json emit raw import paths).
			extNode, ok := aggregateOut.ExternalDependencies[imp]
			if !ok {
				extNode, ok = aggregateOut.ExternalDependencies[aggregate.ResolveExternalKey(imp)]
			}
			if ok {
				// v2 (W1-09/W1-14, A-11): ext node IDs are the canonical
				// ext:<escaped> keys — never alias- or module-path-mangled
				// ("ext:akgerrs \"path\""). Legacy cached raw keys self-heal.
				extID := aggregate.ResolveExternalKey(imp)
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
					props := map[string]string{
						"primitive":        "EXTERNAL_SDK_CALL",
						ont.PredProvenance: "ast",
					}
					cpg.GraphNodes[apiID] = &ResolvedNode{
						ID:         apiID,
						Kind:       "EXTERNAL_API",
						Name:       method,
						Properties: props,
					}
					// A-11 (§5.3.3): ext→api CONTAINS edges removed; file→ext
					// DEPENDS_ON is emitted by the dependency linker and the
					// call site links to the API directly.
				}
				return apiID, 0.9 // High confidence because we matched the exact external import
			}
		}
	}

	// Attempt 6: v3 (incremental): persisted-graph symbol lookup. In a delta,
	// the symbol index and ownership map only cover modified files, so targets
	// defined in unmodified files (e.g. a delta of service.go calling
	// store.Open) resolve against the linked base graph instead of vanishing.
	// Candidate order is preserved; the receiver filter prefers the exact
	// type/package owner when multiple symbols share a name.
	if cpg.db != nil {
		// FUNCTION/METHOD only — never PARAM/FIELD or virtual nodes that
		// merely share the callee's name.
		var hits []*ResolvedNode
		for _, kind := range []string{"FUNCTION", "METHOD"} {
			hits = append(hits, cpg.db.GetNodesByKind(kind)...)
		}
		if len(hits) > 0 {
			if receiver != "" {
				for _, n := range hits {
					if n != nil && n.Name == method && dbCandidateMatches(receiver, n.ID) {
						return n.ID, 0.7
					}
				}
			}
			for _, n := range hits {
				if n != nil && n.Name == method {
					return n.ID, 0.6
				}
			}
		}
	}

	return "", 0.0
}

// qualifierMatchesImport reports whether a call-site receiver qualifier can
// plausibly denote the given import: it matches the import's explicit alias
// or its final path segment (with gopkg.in / major-version suffixes
// stripped). This prevents fabricating ext: API nodes for receivers that
// name a different package (GAP-CALL-05).
func qualifierMatchesImport(qualifier, imp string, aliases map[string]string) bool {
	if qualifier == "" {
		return false
	}
	if aliases != nil {
		if alias, ok := aliases[imp]; ok && alias == qualifier {
			return true
		}
	}
	base := importBaseName(imp)
	return base != "" && base == qualifier
}

// importBaseName returns the package identifier a caller would use for an
// import path: the final path segment, with version suffixes stripped
// ("gopkg.in/yaml.v3" → "yaml", "github.com/foo/bar/v2" → "bar").
func importBaseName(imp string) string {
	parts := strings.Split(imp, "/")
	base := parts[len(parts)-1]
	if i := strings.LastIndex(base, ".v"); i != -1 {
		if _, err := strconv.Atoi(base[i+2:]); err == nil {
			base = base[:i]
		}
	}
	if len(parts) > 1 {
		v := parts[len(parts)-1]
		if strings.HasPrefix(v, "v") {
			if _, err := strconv.Atoi(v[1:]); err == nil {
				base = parts[len(parts)-2]
			}
		}
	}
	return base
}

// dbCandidateMatches reports whether a persisted-graph node ID plausibly owns
// the call-site receiver: the receiver segment of a universal ID (the last
// "::" segment) for method targets, or the file's package name for free
// functions ("path/pkg::Open" called via pkg.Open).
func dbCandidateMatches(receiver, id string) bool {
	rcv := strings.ToLower(receiver)
	parts := strings.Split(id, "::")
	if len(parts) >= 2 && strings.EqualFold(parts[len(parts)-2], receiver) {
		return true
	}
	fileSeg := parts[0]
	if i := strings.LastIndex(fileSeg, "/"); i != -1 {
		fileSeg = fileSeg[i+1:]
	}
	fileSeg = strings.TrimSuffix(fileSeg, filepath.Ext(fileSeg))
	return strings.EqualFold(fileSeg, receiver) || strings.Contains(strings.ToLower(fileSeg), rcv)
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
