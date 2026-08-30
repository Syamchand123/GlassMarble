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

	// 3. Test Graph API
	resp, err = client.Get(baseURL + "/api/graph")
	if err != nil {
		t.Fatalf("GET /api/graph failed: %v", err)
	}
	var gData struct {
		Nodes []any `json:"nodes"`
		Edges []any `json:"edges"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&gData); err != nil {
		t.Fatalf("failed to decode /api/graph: %v", err)
	}
	resp.Body.Close()

	if len(gData.Nodes) != 1 {
		t.Errorf("expected 1 node, got %d", len(gData.Nodes))
	}

	// Graceful Stop
	if err := server.Stop(); err != nil {
		t.Errorf("server.Stop failed: %v", err)
	}
}
