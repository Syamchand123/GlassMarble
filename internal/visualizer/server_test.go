package visualizer

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/link"
)

func TestVisualizerServer(t *testing.T) {
	graph := akg.NewCodePropertyGraph("commit1")

	graph.Nodes = graph.Nodes.Set("n1", &link.ResolvedNode{
		ID:   "n1",
		Name: "AppService",
		Kind: "STRUCT",
		FileSpec: link.LocationMeta{
			Path: "internal/service/app.go",
		},
	})

	graph.Nodes = graph.Nodes.Set("n2", &link.ResolvedNode{
		ID:   "n2",
		Name: "DatabaseStore",
		Kind: "DATABASE",
		FileSpec: link.LocationMeta{
			Path: "internal/db/store.go",
		},
	})

	graph.OutboundEdges = graph.OutboundEdges.Set("n1", []link.ResolvedEdge{
		{SourceID: "n1", TargetID: "n2", Type: link.EdgeDependsOn},
	})

	server := NewServer(graph, ServerOptions{
		Host:     "127.0.0.1",
		Port:     0, // Random port
		AutoOpen: false,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	port, err := server.Start(ctx)
	if err != nil {
		t.Fatalf("server.Start failed: %v", err)
	}

	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	client := &http.Client{Timeout: 3 * time.Second}

	// 1. Test Health
	resp, err := client.Get(baseURL + "/api/health")
	if err != nil {
		t.Fatalf("GET /api/health failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("health returned status %d", resp.StatusCode)
	}
	resp.Body.Close()

	// 2. Test Index HTML
	resp, err = client.Get(baseURL + "/")
	if err != nil {
		t.Fatalf("GET / failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("index returned status %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if len(body) == 0 {
		t.Error("index body is empty")
	}

	// 3. Test Graph API (Cytoscape Elements)
	for _, view := range []string{"components", "packages", "symbols"} {
		resp, err = client.Get(baseURL + "/api/graph?view=" + view)
		if err != nil {
			t.Fatalf("GET /api/graph?view=%s failed: %v", view, err)
		}
		var elements []map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&elements); err != nil {
			t.Fatalf("failed to decode /api/graph?view=%s: %v", view, err)
		}
		resp.Body.Close()

		if len(elements) == 0 {
			t.Errorf("expected non-empty cytoscape elements for view %s", view)
		}
	}

	// 4. Test Layers API
	resp, err = client.Get(baseURL + "/api/layers")
	if err != nil {
		t.Fatalf("GET /api/layers failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("layers returned status %d", resp.StatusCode)
	}
	resp.Body.Close()

	// 5. Test Path Trace API
	resp, err = client.Get(baseURL + "/api/paths?source=AppService&target=DatabaseStore")
	if err != nil {
		t.Fatalf("GET /api/paths failed: %v", err)
	}
	var pathRes map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&pathRes); err != nil {
		t.Fatalf("failed to decode /api/paths: %v", err)
	}
	resp.Body.Close()

	if found, ok := pathRes["found"].(bool); !ok || !found {
		t.Errorf("expected path from AppService to DatabaseStore to be found, got: %v", pathRes)
	}

	// 6. Test Intelligence, Timeline, Marbles, and Graph Algorithm APIs
	algoEndpoints := []string{
		"/api/intelligence", "/api/timeline", "/api/marbles", "/api/snapshots", "/api/conventions",
		"/api/algorithms/cycles", "/api/algorithms/toposort", "/api/algorithms/cutvertices",
		"/api/algorithms/pagerank", "/api/algorithms/similarity?nodeA=n1&nodeB=n2", "/api/algorithms/orphans",
	}
	for _, ep := range algoEndpoints {
		resp, err := client.Get(baseURL + ep)
		if err != nil {
			t.Fatalf("GET %s failed: %v", ep, err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Errorf("%s returned status %d", ep, resp.StatusCode)
		}
		resp.Body.Close()
	}

	// Release pooled keep-alive connections before shutting down; otherwise
	// Shutdown waits on this client's idle sockets and the test races its own
	// grace period under parallel load.
	client.CloseIdleConnections()

	// Graceful Stop
	if err := server.Stop(); err != nil {
		t.Errorf("server.Stop failed: %v", err)
	}
}
