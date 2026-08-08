package knowledge_fusion

import (
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage4"
	"github.com/Syamchand123/GlassMarble/internal/developer_memory"
)

func TestLinkDocumentClaimsToAKG(t *testing.T) {
	graph := &akg.CodePropertyGraph{
		Nodes:         akg.NewCowMap[string, *stage4.ResolvedNode](),
		FileNodeIndex: akg.NewCowMap[string, map[string]bool](),
	}

	graph.Nodes = graph.Nodes.Set("auth-service-id", &stage4.ResolvedNode{
		Name: "AuthService",
	})
	graph.Nodes = graph.Nodes.Set("redis-cache-id", &stage4.ResolvedNode{
		Name: "redis.Cache",
	})

	graph.FileNodeIndex = graph.FileNodeIndex.Set("pkg/auth.go", map[string]bool{"auth-service-id": true})

	claims := []developer_memory.KnowledgeClaim{
		{
			Subject: "pkg/auth.go", // File match
			Object:  "redis.Cache", // Exact match
		},
		{
			Subject: "UnknownService", // No match
			Object:  "AuthService",    // Match
		},
	}

	linked := LinkDocumentClaimsToAKG(claims, graph)

	if len(linked) != 2 {
		t.Fatalf("Expected 2 claims, got %d", len(linked))
	}

	if linked[0].Subject != "auth-service-id" {
		t.Errorf("Expected Subject to link to 'auth-service-id' via FileNodeIndex, got '%s'", linked[0].Subject)
	}
	if linked[0].Object != "redis-cache-id" {
		t.Errorf("Expected Object to link to 'redis-cache-id', got '%s'", linked[0].Object)
	}

	if linked[1].Subject != "UnknownService" {
		t.Errorf("Expected Subject to remain 'UnknownService', got '%s'", linked[1].Subject)
	}
	if linked[1].Object != "auth-service-id" {
		t.Errorf("Expected Object to link to 'auth-service-id', got '%s'", linked[1].Object)
	}
}
