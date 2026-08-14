package link

import (
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/aggregate"
)

// LinkEnterpriseSemantics converts the massive 10-dimension Step 2.3 Semantic Map into CPG Edges.
func LinkEnterpriseSemantics(aggregateOut *aggregate.AggregateOutput, cpg *LinkOutput) {
	if aggregateOut == nil || cpg == nil {
		return
	}

	for relPath, symTable := range aggregateOut.LocalTables {
		if symTable == nil {
			continue
		}

		// 1. Inheritances
		for _, inh := range symTable.Inheritances {
			sourceFQN := BuildUniversalID(relPath, "", inh.ChildName)
			targetFQN := resolveTypeToFQN(inh.ParentName, relPath, aggregateOut.GlobalDefinitionIndex, cpg)
			if targetFQN == "" {
				targetFQN = BuildUniversalID(relPath, "", inh.ParentName)
			}
			edgeType := EdgeExtends
			if inh.IsInterface {
				edgeType = EdgeImplements
			}
			cpg.AddEdge(sourceFQN, targetFQN, edgeType, inh.LineNumber)
		}

		// 2. Instantiations
		for _, inst := range symTable.Instantiations {
			callerFQN := "file:" + aggregate.NormalizeRelativePath(relPath)
			targetFQN := resolveTypeToFQN(inst.ObjectName, relPath, aggregateOut.GlobalDefinitionIndex, cpg)
			if targetFQN != "" {
				cpg.AddEdge(callerFQN, targetFQN, EdgeReferences, inst.LineNumber)
			}
		}

		// 3. Exceptions
		for _, exc := range symTable.Exceptions {
			if exc.Action == "THROW" {
				callerFQN := "file:" + aggregate.NormalizeRelativePath(relPath)
				targetFQN := resolveTypeToFQN(exc.ExceptionType, relPath, aggregateOut.GlobalDefinitionIndex, cpg)
				if targetFQN != "" {
					cpg.AddEdge(callerFQN, targetFQN, EdgeThrows, exc.LineNumber)
				}
			}
		}

		// 4/5. Concurrency spawns and event hooks are linked by
		// LinkConcurrencyAndAsyncControlFlow and LinkEventSourcing respectively.
		// The old duplicate SPAWNS_CONCURRENT (to a shared thread_or_coroutine
		// node) and DISPATCHES_EVENT ("event:unknown") edges here fabricated
		// noise and double-linked the same calls (AUDIT Issue 1.5 / Phase 1B-6).
	}

	// 6. Enterprise Semantics: Databases & Cloud APIs
	// Scan the call queue for known DB or Cloud calls based on primitives
	for _, call := range aggregateOut.GlobalCallQueue {
		if len(cpg.ModifiedFiles) > 0 && !cpg.ModifiedFiles[aggregate.NormalizeRelativePath(call.SourceFilePath)] {
			continue
		}

		callerID := call.SourceFileNodeID
		if callerID == "" {
			callerID = "file:" + aggregate.NormalizeRelativePath(call.SourceFilePath)
		}

		isDB := false
		isCloud := false
		for _, p := range call.Primitives {
			// Check both SQL and NoSQL variants using correct primitive constant values
			if string(p) == "DATABASE_SQL" || string(p) == "DATABASE_NOSQL" || string(p) == "DATABASE" {
				isDB = true
			}
			if string(p) == "NETWORK_IO" || string(p) == "RPC" || string(p) == "CLOUD_SDK" {
				isCloud = true
			}
		}

		// Heuristic checks
		lowerTarget := strings.ToLower(call.ReceiverName + "." + call.MethodName)
		if strings.Contains(lowerTarget, "db") || strings.Contains(lowerTarget, "sql") || strings.Contains(lowerTarget, "mongo") || strings.Contains(lowerTarget, "redis") || strings.Contains(lowerTarget, "postgres") || strings.Contains(lowerTarget, "query") || strings.Contains(lowerTarget, "orm") || strings.Contains(lowerTarget, "gorm") {
			isDB = true
		}
		if strings.Contains(lowerTarget, "http") || strings.Contains(lowerTarget, "fetch") || strings.Contains(lowerTarget, "aws") || strings.Contains(lowerTarget, "s3") || strings.Contains(lowerTarget, "stripe") || strings.Contains(lowerTarget, "twilio") {
			isCloud = true
		}

		if isDB {
			dbID := "DATABASE::" + call.ReceiverName
			ensureVirtualNode(dbID, "VIRTUAL_DATABASE", call.ReceiverName, cpg)
			cpg.AddEdge(callerID, dbID, EdgeQueriesDB, call.LineNumber)
		}
		if isCloud {
			cloudID := "CLOUD_API::" + call.ReceiverName
			ensureVirtualNode(cloudID, "VIRTUAL_CLOUD_API", call.ReceiverName, cpg)
			cpg.AddEdge(callerID, cloudID, EdgeCallsCloudAPI, call.LineNumber)
		}
	}

	for relPath, symTable := range aggregateOut.LocalTables {
		if symTable == nil {
			continue
		}

		// 6. Type Aliases
		for _, alias := range symTable.TypeAliases {
			sourceFQN := BuildUniversalID(relPath, "", alias.AliasName)
			targetFQN := resolveTypeToFQN(alias.TargetType, relPath, aggregateOut.GlobalDefinitionIndex, cpg)
			if targetFQN != "" {
				cpg.AddEdge(sourceFQN, targetFQN, EdgeAliasesType, alias.LineNumber)
			}
		}

		// 7. Endpoints
		for _, ep := range symTable.Endpoints {
			callerFQN := "file:" + aggregate.NormalizeRelativePath(relPath)
			targetID := "endpoint:" + ep.Method + ":" + ep.Route
			ensureVirtualNode(targetID, "VIRTUAL_ENDPOINT", ep.Method+":"+ep.Route, cpg)
			cpg.AddEdge(callerFQN, targetID, EdgeExposesEndpoint, ep.LineNumber)
		}

		// 8. Security Sinks
		for _, sink := range symTable.SecuritySinks {
			callerFQN := "file:" + aggregate.NormalizeRelativePath(relPath)
			sinkID := "sink:" + sink.SinkType
			ensureVirtualNode(sinkID, "VIRTUAL_SECURITY_SINK", sink.SinkType, cpg)
			cpg.AddEdge(callerFQN, sinkID, EdgeSecuritySink, sink.LineNumber)
		}

		// 9. Resource Links
		for _, res := range symTable.ResourceLinks {
			callerFQN := "file:" + aggregate.NormalizeRelativePath(relPath)
			resID := "resource:" + res.ResourceType
			ensureVirtualNode(resID, "VIRTUAL_RESOURCE", res.ResourceType, cpg)
			cpg.AddEdge(callerFQN, resID, EdgeConsumesResource, res.LineNumber)
		}

		// 10. Global State Declarations
		for _, gs := range symTable.GlobalState {
			callerFQN := "file:" + aggregate.NormalizeRelativePath(relPath)
			targetFQN := resolveTypeToFQN(gs.Name, relPath, aggregateOut.GlobalDefinitionIndex, cpg)
			if targetFQN == "" {
				targetFQN = "global:" + gs.Name
				ensureVirtualNode(targetFQN, "VIRTUAL_GLOBAL_STATE", gs.Name, cpg)
			}
			cpg.AddEdge(callerFQN, targetFQN, EdgeMutatesGlobal, 0)
		}
	}
}
