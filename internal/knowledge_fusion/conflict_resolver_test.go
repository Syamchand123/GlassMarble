package knowledge_fusion

import (
	"testing"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/developer_memory"
	"github.com/Syamchand123/GlassMarble/internal/evidence"
)

func TestResolveConflict(t *testing.T) {
	a := developer_memory.KnowledgeClaim{
		Subject:   "AuthService",
		Predicate: "uses",
		Object:    "JWT",
		State:     developer_memory.StateActive,
	}
	bundleA := evidence.Bundle{}
	bundleA.Add(evidence.EvidenceItem{
		Source:     evidence.SourceDocs,
		Reference:  "README.md",
		Excerpt:    "Auth uses JWT",
		Confidence: 0.90,
		Timestamp:  time.Now(),
	})
	a.Evidence = bundleA

	b := developer_memory.KnowledgeClaim{
		Subject:   "AuthService",
		Predicate: "uses",
		Object:    "OAuth2",
		State:     developer_memory.StateActive,
	}
	bundleB := evidence.Bundle{}
	bundleB.Add(evidence.EvidenceItem{
		Source:     evidence.SourceCode, // Higher priority than Docs
		Reference:  "auth.go",
		Excerpt:    "func OAuth2Login()",
		Confidence: 1.0,
		Timestamp:  time.Now(),
	})
	b.Evidence = bundleB

	resolved := ResolveConflict(a, b)

	if resolved.Object != "OAuth2" {
		t.Errorf("Expected object 'OAuth2' (SourceCode wins over SourceDocs), got '%s'", resolved.Object)
	}

	if len(resolved.Evidence.Items) != 2 {
		t.Errorf("Expected 2 merged evidence items, got %d", len(resolved.Evidence.Items))
	}
}
