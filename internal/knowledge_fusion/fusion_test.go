package knowledge_fusion

import (
	"context"
	"testing"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage4"
)

type MockPRAdapter struct{}

func (m *MockPRAdapter) Name() string { return "MockPR" }
func (m *MockPRAdapter) FetchRelatedPRs(ctx context.Context, refs []string) ([]PullRequest, error) {
	return []PullRequest{
		{ID: "1", Title: "Add Auth", Description: "Adds Redis for caching Auth", FilesChanged: []string{"auth/service.go"}},
	}, nil
}

type MockIssueAdapter struct{}

func (m *MockIssueAdapter) Name() string { return "MockIssue" }
func (m *MockIssueAdapter) FetchRelatedIssues(ctx context.Context, refs []string) ([]Issue, error) {
	return []Issue{
		{ID: "42", Title: "Bug in Cache", Description: "Cache misses", FilesChanged: []string{"cache/redis.go"}},
	}, nil
}

func TestFusionEngine_Fuse(t *testing.T) {
	engine := NewFusionEngine(&MockPRAdapter{}, &MockIssueAdapter{})

	graph := &akg.CodePropertyGraph{
		Nodes:         akg.NewCowMap[string, *stage4.ResolvedNode](),
		FileNodeIndex: akg.NewCowMap[string, map[string]bool](),
	}

	graph.Nodes = graph.Nodes.Set("node-auth", &stage4.ResolvedNode{Name: "AuthService"})
	graph.Nodes = graph.Nodes.Set("node-cache", &stage4.ResolvedNode{Name: "RedisCache"})

	graph.FileNodeIndex = graph.FileNodeIndex.Set("auth/service.go", map[string]bool{"node-auth": true})
	graph.FileNodeIndex = graph.FileNodeIndex.Set("cache/redis.go", map[string]bool{"node-cache": true})

	claims, err := engine.Fuse(
		context.Background(),
		".",
		[]string{}, // adrs
		[]string{}, // readmes
		[]string{"1"}, // prs
		[]string{"42"}, // issues
		graph,
	)

	if err != nil {
		t.Fatalf("Fuse failed: %v", err)
	}

	if len(claims) != 2 {
		t.Errorf("Expected 2 claims from PR and Issue, got %d", len(claims))
	}

	hasPR := false
	hasIssue := false

	for _, c := range claims {
		if c.Object == "1" {
			hasPR = true
			if c.Subject != "node-auth" { // Mapped from auth/service.go
				t.Errorf("Unexpected subject for PR claim: %s", c.Subject)
			}
			if c.Predicate != "was_modified_by_pr" {
				t.Errorf("Unexpected predicate for PR claim: %s", c.Predicate)
			}
			if c.Evidence.Items[0].Confidence != 0.90 {
				t.Errorf("Expected confidence 0.90 for PR, got %f", c.Evidence.Items[0].Confidence)
			}
			if time.Since(c.ValidFrom) > time.Minute {
				t.Errorf("Expected ValidFrom to be recent, got %v", c.ValidFrom)
			}
		}
		if c.Object == "42" {
			hasIssue = true
			if c.Subject != "node-cache" { // Mapped from cache/redis.go
				t.Errorf("Unexpected subject for Issue claim: %s", c.Subject)
			}
			if c.Predicate != "fixes_issue" {
				t.Errorf("Unexpected predicate for Issue claim: %s", c.Predicate)
			}
			if c.Evidence.Items[0].Confidence != 0.98 {
				t.Errorf("Expected confidence 0.98 for Issue, got %f", c.Evidence.Items[0].Confidence)
			}
		}
	}

	if !hasPR || !hasIssue {
		t.Errorf("Missing expected PR or Issue claim")
	}
}
