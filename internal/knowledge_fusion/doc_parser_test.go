package knowledge_fusion

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseADR(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "adr_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	adrPath := filepath.Join(tempDir, "001-use-redis.md")
	content := `# Use Redis
## Status
Accepted
## Context
We need a cache.
## Decision
We will use Redis.
## Consequences
Fast cache.`

	if err := os.WriteFile(adrPath, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write ADR: %v", err)
	}

	claim, err := ParseADR(adrPath)
	if err != nil {
		t.Fatalf("ParseADR failed: %v", err)
	}

	if claim.Subject != "Use Redis" {
		t.Errorf("Expected subject 'Use Redis', got '%s'", claim.Subject)
	}
	if claim.Object != "We will use Redis." {
		t.Errorf("Expected object 'We will use Redis.', got '%s'", claim.Object)
	}
	if !strings.Contains(claim.Evidence.Items[0].Excerpt, "Context: We need a cache. | Decision: We will use Redis.") {
		t.Errorf("Unexpected excerpt: %s", claim.Evidence.Items[0].Excerpt)
	}
}

func TestParseReadme(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "readme_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	readmePath := filepath.Join(tempDir, "README.md")
	content := `This is our cool project.
It uses Redis for caching.
And PostgreSQL for the database.`

	if err := os.WriteFile(readmePath, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write README: %v", err)
	}

	claims := ParseReadme(readmePath)
	if len(claims) != 2 {
		t.Fatalf("Expected 2 claims, got %d", len(claims))
	}

	hasRedis := false
	hasPostgres := false
	for _, c := range claims {
		if c.Object == "Redis" {
			hasRedis = true
		}
		if c.Object == "PostgreSQL" {
			hasPostgres = true
		}
	}

	if !hasRedis || !hasPostgres {
		t.Errorf("Did not find both Redis and PostgreSQL claims")
	}
}
