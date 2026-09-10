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

func TestScanArchEventsDefaultsNonEmpty(t *testing.T) {
	logs := []string{
		"feat: split package into submodules to reduce coupling",
		"feat: introduce cycle between a and b",
		"fix: layer violation in billing boundary",
		"feat: add new service for notifications",
		"BREAKING: rename public API signature",
		"feat: add oauth login flow",
		"feat: create new subsystem for search",
		"feat: add database storage layer",
		"fix: typo in readme",
	}
	events := ScanArchEvents(logs)
	if len(events) != 8 {
		t.Fatalf("expected 8 events (9 types coverable), got %d: %+v", len(events), events)
	}
	seen := map[string]bool{}
	for _, e := range events {
		seen[e.Type] = true
		if strings.TrimSpace(e.Context) == "" || strings.TrimSpace(e.Decision) == "" || strings.TrimSpace(e.Consequence) == "" {
			t.Errorf("event %s has empty defaults: %+v", e.Type, e)
		}
	}
	for _, want := range []string{"COMPONENT_SPLIT", "CYCLE_INTRODUCED", "LAYER_VIOLATION", "SERVICE_ADDED", "INTERFACE_CHANGED", "AUTH_ARCHITECTURE", "NEW_SUBSYSTEM", "NEW_DATABASE_LAYER"} {
		if !seen[want] {
			t.Errorf("expected event type %s to be detected", want)
		}
	}
}

func TestAutoGenerateADRs(t *testing.T) {
	tempDir := t.TempDir()

	paths, err := AutoGenerateADRs(tempDir, "abc1234", "feat: split package into submodules to reduce coupling")
	if err != nil {
		t.Fatalf("AutoGenerateADRs failed: %v", err)
	}
	if len(paths) != 1 {
		t.Fatalf("expected 1 ADR path, got %v", paths)
	}
	if !strings.HasPrefix(paths[0], "docs/adr/0001-") {
		t.Errorf("expected 0001 ADR path, got %q", paths[0])
	}
	data, err := os.ReadFile(filepath.Join(tempDir, filepath.FromSlash(paths[0])))
	if err != nil {
		t.Fatalf("reading generated ADR: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "abc1234") {
		t.Errorf("expected commit hash in ADR:\n%s", content)
	}
	if !strings.Contains(content, "## Context") || !strings.Contains(content, "## Decision") || !strings.Contains(content, "## Consequences") {
		t.Errorf("expected Context/Decision/Consequences sections:\n%s", content)
	}

	// Non-architectural message produces no ADRs and no error.
	paths, err = AutoGenerateADRs(tempDir, "def5678", "fix: typo in readme")
	if err != nil {
		t.Fatalf("AutoGenerateADRs (no-op) failed: %v", err)
	}
	if len(paths) != 0 {
		t.Errorf("expected 0 ADR paths for non-arch commit, got %v", paths)
	}
}
