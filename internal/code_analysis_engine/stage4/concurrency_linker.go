package stage4

import (
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage3"
)

// LinkConcurrencyAndAsyncControlFlow scans call queues and graph nodes for async/thread forks.
func LinkConcurrencyAndAsyncControlFlow(stage3Out *stage3.Stage3Output, cpg *Stage4Output) {
	if stage3Out == nil || cpg == nil {
		return
	}

	om := ownershipMap(cpg, stage3Out)

	for _, callSite := range stage3Out.GlobalCallQueue {
		if len(cpg.ModifiedFiles) > 0 && !cpg.ModifiedFiles[stage3.NormalizeRelativePath(callSite.SourceFilePath)] {
			continue
		}
		if isConcurrentCall(callSite) {
			callerID := callSite.SourceFileNodeID
			if callerID == "" {
				callerID = "file:" + callSite.SourceFilePath
			}

			targetID, _ := resolveCallTarget(callSite.ReceiverName, callSite.MethodName, callSite.SourceFolderPath, callSite.LocalImports, om, cpg, stage3Out)
			if targetID != "" {
				edgeType := EdgeSpawnsConcurrent
				if strings.Contains(strings.ToLower(callSite.MethodName+callSite.ReceiverName), "event") || strings.Contains(strings.ToLower(callSite.MethodName), "notify") {
					edgeType = EdgeDispatchesEvent
				}
				cpg.AddEdge(callerID, targetID, edgeType, callSite.LineNumber)
			}
		} else if isMessagePassing(callSite) {
			// Construct Message Passing Graph (MPG) edges
			callerID := callSite.SourceFileNodeID
			if callerID == "" {
				callerID = "file:" + callSite.SourceFilePath
			}

			// We can use the receiver as the Channel/Queue ID
			queueID := "QUEUE::" + callSite.ReceiverName
			ensureVirtualNode(queueID, "VIRTUAL_QUEUE", callSite.ReceiverName, cpg)

			lowerMethod := strings.ToLower(callSite.MethodName)
			if strings.Contains(lowerMethod, "send") || strings.Contains(lowerMethod, "publish") || strings.Contains(lowerMethod, "produce") || strings.Contains(lowerMethod, "emit") {
				cpg.AddEdge(callerID, queueID, EdgeSendsTo, callSite.LineNumber)
			} else if strings.Contains(lowerMethod, "recv") || strings.Contains(lowerMethod, "receive") || strings.Contains(lowerMethod, "consume") || strings.Contains(lowerMethod, "subscribe") || strings.Contains(lowerMethod, "on") {
				cpg.AddEdge(queueID, callerID, EdgeReceivesFrom, callSite.LineNumber)
			}
		}
	}
}

func isMessagePassing(site stage3.LinkedCallSite) bool {
	combined := strings.ToLower(site.ReceiverName + "." + site.MethodName)
	return strings.Contains(combined, "send") ||
		strings.Contains(combined, "publish") ||
		strings.Contains(combined, "produce") ||
		strings.Contains(combined, "emit") ||
		strings.Contains(combined, "recv") ||
		strings.Contains(combined, "receive") ||
		strings.Contains(combined, "consume") ||
		strings.Contains(combined, "subscribe") ||
		strings.Contains(combined, "chan") ||
		strings.Contains(combined, "kafka") ||
		strings.Contains(combined, "rabbitmq") ||
		strings.Contains(combined, "pubsub")
}

func isConcurrentCall(site stage3.LinkedCallSite) bool {
	combined := strings.ToLower(site.ReceiverName + "." + site.MethodName)

	for _, p := range site.Primitives {
		if p == "CONCURRENCY" {
			return true
		}
	}

	return strings.Contains(combined, "go ") ||
		strings.Contains(combined, "async") ||
		strings.Contains(combined, "await") ||
		strings.Contains(combined, "task.run") ||
		strings.Contains(combined, "runasync") ||
		strings.Contains(combined, "thread") ||
		strings.Contains(combined, "future") ||
		strings.Contains(combined, "promise") ||
		strings.Contains(combined, "dispatch") ||
		strings.Contains(combined, "tokio::spawn") ||
		strings.Contains(combined, "completablefuture") ||
		strings.Contains(combined, "executorservice") ||
		strings.Contains(combined, "asyncio") ||
		strings.Contains(combined, "std::thread") ||
		strings.Contains(combined, "std::async") ||
		strings.Contains(combined, "worker")
}
