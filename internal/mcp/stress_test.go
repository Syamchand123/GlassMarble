package mcp

import (
	"context"
	"sync"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"
)

// TestStress_ConcurrentToolInvocations runs parallel goroutines invoking various
// tools simultaneously to guarantee race-free thread safety under load.
func TestStress_ConcurrentToolInvocations(t *testing.T) {
	cfg := DefaultConfig()
	cfg.RootDir = "."

	srv, err := NewServer(cfg)
	require.NoError(t, err)
	defer srv.Close()

	ctx := context.Background()

	const workers = 10
	const iterations = 4

	var wg sync.WaitGroup
	wg.Add(workers)

	for w := 0; w < workers; w++ {
		go func(workerID int) {
			defer wg.Done()

			for i := 0; i < iterations; i++ {
				// 1. gmb_status
				var statusReq mcp.CallToolRequest
				statusReq.Params.Name = "gmb_status"
				res1, err := srv.handleStatusTool(ctx, statusReq)
				if err != nil || res1 == nil {
					t.Errorf("worker %d iteration %d: gmb_status failed: %v", workerID, i, err)
				}

				// 2. gmb_inspect_search
				var searchReq mcp.CallToolRequest
				searchReq.Params.Name = "gmb_inspect_search"
				searchReq.Params.Arguments = map[string]any{"query": "Execute"}
				res2, err := srv.handleInspectSearchTool(ctx, searchReq)
				if err != nil || res2 == nil {
					t.Errorf("worker %d iteration %d: gmb_inspect_search failed: %v", workerID, i, err)
				}

				// 3. gmb_list_diagram_types
				var diagReq mcp.CallToolRequest
				diagReq.Params.Name = "gmb_list_diagram_types"
				res3, err := srv.handleListDiagramTypesTool(ctx, diagReq)
				if err != nil || res3 == nil {
					t.Errorf("worker %d iteration %d: gmb_list_diagram_types failed: %v", workerID, i, err)
				}

				// 4. Resource read gmb://status
				var resReq mcp.ReadResourceRequest
				resReq.Params.URI = "gmb://status"
				resContents, err := srv.handleStatusResource(ctx, resReq)
				if err != nil || len(resContents) == 0 {
					t.Errorf("worker %d iteration %d: read gmb://status failed: %v", workerID, i, err)
				}

				// 5. Prompt fetch gmb_pre_commit_audit
				var promptReq mcp.GetPromptRequest
				promptReq.Params.Name = "gmb_pre_commit_audit"
				promptRes, err := srv.handlePreCommitAuditPrompt(ctx, promptReq)
				if err != nil || promptRes == nil {
					t.Errorf("worker %d iteration %d: get prompt failed: %v", workerID, i, err)
				}
			}
		}(w)
	}

	wg.Wait()
}

// TestStress_BridgeConcurrency verifies that the Bridge thread-safely handles concurrent snapshot reads.
func TestStress_BridgeConcurrency(t *testing.T) {
	b := NewBridge(".", "", 256)
	defer b.Close()

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = b.Snapshot()
			_, _ = b.MemoryStore()
			_, _ = b.SnapshotStore()
		}()
	}
	wg.Wait()
}
