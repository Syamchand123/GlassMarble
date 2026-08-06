package akg

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage4"
)

func TestAutoMigrateOnLoad(t *testing.T) {
	tempDir := t.TempDir()
	ttlPath := filepath.Join(tempDir, "akg_state.ttl")

	// Create dummy TTL file
	if err := os.WriteFile(ttlPath, []byte("# dummy v2 ttl"), 0644); err != nil {
		t.Fatalf("failed to create dummy ttl: %v", err)
	}

	graph := NewCodePropertyGraph("commit123")
	graph.SchemaVersion = 2
	graph.Nodes = graph.Nodes.Set("node1", &stage4.ResolvedNode{
		ID:   "node1",
		Kind: "TYPE_DECL",
		Properties: map[string]string{
			"code": "sample snippet",
		},
	})

	bakPath, err := AutoMigrateOnLoad(tempDir, graph)
	if err != nil {
		t.Fatalf("AutoMigrateOnLoad failed: %v", err)
	}

	if bakPath == "" {
		t.Fatalf("expected backup path to be non-empty")
	}

	if _, err := os.Stat(bakPath); os.IsNotExist(err) {
		t.Fatalf("expected backup file to exist at %s", bakPath)
	}

	if graph.SchemaVersion != 3 {
		t.Fatalf("expected schema version 3, got %d", graph.SchemaVersion)
	}

	n, _ := graph.Nodes.Get("node1")
	if n.Kind != "STRUCT" {
		t.Fatalf("expected kind STRUCT, got %s", n.Kind)
	}
	if n.Properties["content"] != "sample snippet" {
		t.Fatalf("expected code key to be migrated to content key, got %v", n.Properties)
	}
}
