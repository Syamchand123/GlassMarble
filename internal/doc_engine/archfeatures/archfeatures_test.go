package archfeatures

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGenerateADR(t *testing.T) {
	tempDir := t.TempDir()

	event := ADREvent{
		Type:        "COMPONENT_SPLIT",
		Title:       "Split Monolith into Subsystems",
		Context:     "High coupling between ingest and link layers.",
		Decision:    "Split packages into autonomous bounded contexts.",
		Consequence: "Clean architectural boundaries and isolated unit tests.",
		CommitHash:  "abc1234",
		Timestamp:   time.Date(2026, 9, 9, 0, 0, 0, 0, time.UTC),
	}

	relPath, err := GenerateADR(tempDir, event)
	if err != nil {
		t.Fatalf("GenerateADR failed: %v", err)
	}

	if !strings.HasPrefix(relPath, "docs/adr/0001-") {
		t.Errorf("expected 0001 ADR path, got %q", relPath)
	}

	fullPath := filepath.Join(tempDir, filepath.FromSlash(relPath))
	data, err := os.ReadFile(fullPath)
	if err != nil {
		t.Fatalf("failed to read generated ADR: %v", err)
	}
	content := string(data)

	if !strings.Contains(content, "id: adr-0001") {
		t.Errorf("missing frontmatter id in ADR:\n%s", content)
	}
	if !strings.Contains(content, "COMPONENT_SPLIT") {
		t.Errorf("missing event type in ADR:\n%s", content)
	}

	// Test sequential numbering on second ADR
	event2 := ADREvent{
		Type:       "NEW_DATABASE_LAYER",
		Title:      "Add Atomic Storage Engine",
		CommitHash: "def5678",
		Timestamp:  time.Now(),
	}
	relPath2, err := GenerateADR(tempDir, event2)
	if err != nil {
		t.Fatalf("GenerateADR 2 failed: %v", err)
	}
	if !strings.HasPrefix(relPath2, "docs/adr/0002-") {
		t.Errorf("expected 0002 ADR path, got %q", relPath2)
	}
}

func TestScanArchEvents(t *testing.T) {
	logs := []string{
		"feat: split package into submodules to reduce coupling",
		"fix: typo in readme",
		"refactor: break cycle between auth and user layers",
		"feat: add database storage layer with atomic locking",
	}

	events := ScanArchEvents(logs)
	if len(events) != 3 {
		t.Fatalf("expected 3 detected arch events, got %d", len(events))
	}
	if events[0].Type != "COMPONENT_SPLIT" {
		t.Errorf("expected COMPONENT_SPLIT, got %s", events[0].Type)
	}
	if events[1].Type != "CYCLE_RESOLVED" {
		t.Errorf("expected CYCLE_RESOLVED, got %s", events[1].Type)
	}
	if events[2].Type != "NEW_DATABASE_LAYER" {
		t.Errorf("expected NEW_DATABASE_LAYER, got %s", events[2].Type)
	}
}

func TestGenerateEvolutionChapter(t *testing.T) {
	tempDir := t.TempDir()

	ch, err := GenerateEvolutionChapter(tempDir, "", "")
	if err != nil {
		t.Fatalf("GenerateEvolutionChapter failed: %v", err)
	}
	if !strings.Contains(ch, "System Evolution & Architectural History") {
		t.Errorf("missing evolution title in chapter:\n%s", ch)
	}
}

func TestGenerateDomainGlossary(t *testing.T) {
	tempDir := t.TempDir()

	// Create sample bounded contexts with homonym collision
	authPkg := filepath.Join(tempDir, "internal", "auth")
	billingPkg := filepath.Join(tempDir, "internal", "billing")
	_ = os.MkdirAll(authPkg, 0755)
	_ = os.MkdirAll(billingPkg, 0755)

	authCode := "package auth\n// Account represents a user security identity\ntype Account struct { ID string }\n"
	billingCode := "package billing\n// Account represents a customer subscription ledger\ntype Account struct { StripeID string }\n"

	_ = os.WriteFile(filepath.Join(authPkg, "account.go"), []byte(authCode), 0644)
	_ = os.WriteFile(filepath.Join(billingPkg, "account.go"), []byte(billingCode), 0644)

	glossary, err := GenerateDomainGlossary(tempDir)
	if err != nil {
		t.Fatalf("GenerateDomainGlossary failed: %v", err)
	}

	if !strings.Contains(glossary, "Cross-Context Terminology Collisions") {
		t.Errorf("expected cross-context collision section in glossary:\n%s", glossary)
	}
	if !strings.Contains(glossary, "Account") {
		t.Errorf("expected Account term in glossary:\n%s", glossary)
	}

	// Verify docs/glossary.md file exists
	dest := filepath.Join(tempDir, "docs", "glossary.md")
	if _, err := os.Stat(dest); os.IsNotExist(err) {
		t.Errorf("docs/glossary.md was not written to disk")
	}
}
