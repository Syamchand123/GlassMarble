package cmd_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/archmodel"
	"github.com/Syamchand123/GlassMarble/internal/developer_memory"
	"github.com/Syamchand123/GlassMarble/internal/evidence"
)

// seedMemory writes a minimal developer-memory aggregate (one claim, one
// event, one component) so the correction surface has real targets.
func seedMemory(t *testing.T, repoDir string) {
	t.Helper()
	store := developer_memory.NewStoreForRepo(repoDir)
	now := time.Now().UTC()
	ev := archmodel.ArchEvent{
		ID:         "ev-1",
		Kind:       archmodel.EventPatternDetected,
		CommitHash: "abc123",
		Timestamp:  now,
		Title:      "Pattern Detected: CLEAN_ARCHITECTURE",
		Components: []string{"CLEAN_ARCHITECTURE"},
		Intent:     "unknown",
		Evidence:   evidence.NewBundle(evidence.EvidenceItem{Source: evidence.SourceGit, Reference: "abc123", Confidence: 0.85}),
	}
	if err := store.AppendEvent(ev); err != nil {
		t.Fatalf("append event: %v", err)
	}
	claim := developer_memory.KnowledgeClaim{
		ID:        "claim-1",
		Subject:   "PaymentService",
		Predicate: "uses_technology",
		Object:    "redis",
		ClaimKind: developer_memory.ClaimFact,
		State:     developer_memory.StateActive,
		ValidFrom: now,
		Evidence:  evidence.NewBundle(evidence.EvidenceItem{Source: evidence.SourceCode, Reference: "svc.go", Confidence: 0.95}),
	}
	if err := store.AppendClaim(claim); err != nil {
		t.Fatalf("append claim: %v", err)
	}
	mem, err := store.Rebuild()
	if err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if err := store.SaveMemoryAndTimeline(mem); err != nil {
		t.Fatalf("save memory: %v", err)
	}
}

func TestMemoryCorrectStateThenAskReflectsIt(t *testing.T) {
	tempDir := t.TempDir()
	seedMemory(t, tempDir)

	// Record a STATE correction on the claim.
	out, err := runGmbCommand(t, "memory", "--dir", tempDir,
		"--correct", "claim-1", "--kind", "STATE", "--value", "DEPRECATED", "--reason", "team decision")
	if err != nil {
		t.Fatalf("correct failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Recorded correction") || !strings.Contains(out, "claim-1") {
		t.Errorf("unexpected record output:\n%s", out)
	}

	// The query must reflect the correction immediately (master plan §8.3).
	out, err = runGmbCommand(t, "memory", "--dir", tempDir, "--ask", "redis")
	if err != nil {
		t.Fatalf("ask failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "state=") && !strings.Contains(out, "DEPRECATED") {
		t.Errorf("correction not reflected in ask output:\n%s", out)
	}
	if !strings.Contains(out, "correction(s) applied") {
		t.Errorf("audit summary missing in ask output:\n%s", out)
	}
}

func TestMemoryCorrectAutoCapturesOriginalValue(t *testing.T) {
	tempDir := t.TempDir()
	seedMemory(t, tempDir)

	// INTENT correction on the event — original value should be captured
	// from memory automatically.
	out, err := runGmbCommand(t, "memory", "--dir", tempDir,
		"--correct", "ev-1", "--kind", "INTENT", "--value", "decided in ADR-014")
	if err != nil {
		t.Fatalf("correct failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, `"unknown" -> "decided in ADR-014"`) {
		t.Errorf("original value not captured in output:\n%s", out)
	}
}

func TestMemoryRejectFlagsWithoutChangingState(t *testing.T) {
	tempDir := t.TempDir()
	seedMemory(t, tempDir)

	if _, err := runGmbCommand(t, "memory", "--dir", tempDir,
		"--correct", "claim-1", "--kind", "REJECT", "--reason", "false inference"); err != nil {
		t.Fatalf("reject failed: %v", err)
	}

	out, err := runGmbCommand(t, "memory", "--dir", tempDir, "--ask", "redis")
	if err != nil {
		t.Fatalf("ask failed: %v", err)
	}
	// D3 regression: rejection is a flag, the temporal state stays CURRENT.
	if !strings.Contains(out, "(rejected)") {
		t.Errorf("rejected marker missing:\n%s", out)
	}
	if strings.Contains(out, "state=REMOVED") {
		t.Errorf("REJECT must not rewrite the temporal state:\n%s", out)
	}
}

func TestMemoryCorrectionsAuditLog(t *testing.T) {
	tempDir := t.TempDir()
	seedMemory(t, tempDir)

	if _, err := runGmbCommand(t, "memory", "--dir", tempDir,
		"--correct", "claim-1", "--kind", "STATE", "--value", "EXPERIMENTAL", "--author", "alice"); err != nil {
		t.Fatalf("correct failed: %v", err)
	}

	out, err := runGmbCommand(t, "memory", "--dir", tempDir, "--corrections")
	if err != nil {
		t.Fatalf("corrections failed: %v", err)
	}
	if !strings.Contains(out, "1 correction(s)") {
		t.Errorf("audit log count missing:\n%s", out)
	}
	if !strings.Contains(out, "by alice") {
		t.Errorf("author not in audit log:\n%s", out)
	}

	// The log file lives at the master-plan path.
	if _, err := os.Stat(filepath.Join(tempDir, ".glassmarble", "memory", "corrections.jsonl")); err != nil {
		t.Errorf("corrections.jsonl missing: %v", err)
	}
}

func TestMemoryCorrectValidationErrors(t *testing.T) {
	tempDir := t.TempDir()
	seedMemory(t, tempDir)

	_, err := runGmbCommand(t, "memory", "--dir", tempDir,
		"--correct", "claim-1", "--kind", "BOGUS")
	if err == nil || !strings.Contains(err.Error(), "unknown correction kind") {
		t.Errorf("unknown kind: got %v, want kind error", err)
	}

	_, err = runGmbCommand(t, "memory", "--dir", tempDir,
		"--correct", "claim-1", "--kind", "INTENT")
	if err == nil || !strings.Contains(err.Error(), "--value is required") {
		t.Errorf("missing value: got %v, want --value error", err)
	}

	_, err = runGmbCommand(t, "memory", "--dir", tempDir,
		"--correct", "claim-1", "--kind", "STATE", "--value", "SOMETIMES")
	if err == nil || !strings.Contains(err.Error(), "not a valid knowledge state") {
		t.Errorf("invalid state: got %v, want state error", err)
	}
}

func TestMemoryAskJSONIncludesCorrectionsApplied(t *testing.T) {
	tempDir := t.TempDir()
	seedMemory(t, tempDir)

	if _, err := runGmbCommand(t, "memory", "--dir", tempDir,
		"--correct", "claim-1", "--kind", "STATE", "--value", "HISTORICAL"); err != nil {
		t.Fatalf("correct failed: %v", err)
	}

	out, err := runGmbCommand(t, "memory", "--dir", tempDir, "--ask", "redis", "--json")
	if err != nil {
		t.Fatalf("ask json failed: %v", err)
	}
	var parsed struct {
		Query              string `json:"query"`
		CorrectionsApplied []struct {
			TargetType string `json:"target_type"`
			Applied    bool   `json:"applied"`
		} `json:"corrections_applied"`
	}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("ask --json is not valid JSON: %v\n%s", err, out)
	}
	if parsed.Query != "redis" {
		t.Errorf("query field lost: %q", parsed.Query)
	}
	if len(parsed.CorrectionsApplied) != 1 || !parsed.CorrectionsApplied[0].Applied ||
		parsed.CorrectionsApplied[0].TargetType != "claim" {
		t.Errorf("corrections_applied missing from JSON: %+v", parsed.CorrectionsApplied)
	}
}

func TestMemoryOverviewReflectsStateCorrection(t *testing.T) {
	tempDir := t.TempDir()
	seedMemory(t, tempDir)

	// The component in memory is "CLEAN_ARCHITECTURE" (derived from the
	// pattern event); a STATE correction on it must show in the overview.
	if _, err := runGmbCommand(t, "memory", "--dir", tempDir,
		"--correct", "CLEAN_ARCHITECTURE", "--kind", "STATE", "--value", "DEPRECATED"); err != nil {
		t.Fatalf("correct failed: %v", err)
	}

	out, err := runGmbCommand(t, "memory", "--dir", tempDir)
	if err != nil {
		t.Fatalf("overview failed: %v", err)
	}
	if !strings.Contains(out, "deprecated") {
		t.Errorf("overview does not reflect component state correction:\n%s", out)
	}
	if !strings.Contains(out, "1 correction(s) applied") {
		t.Errorf("overview audit summary missing:\n%s", out)
	}
}

func TestMemoryCorrectUnknownTargetIsAudited(t *testing.T) {
	tempDir := t.TempDir()
	seedMemory(t, tempDir)

	if _, err := runGmbCommand(t, "memory", "--dir", tempDir,
		"--correct", "ghost-component", "--kind", "STATE", "--value", "DEPRECATED"); err != nil {
		t.Fatalf("correct failed: %v", err)
	}
	out, err := runGmbCommand(t, "memory", "--dir", tempDir)
	if err != nil {
		t.Fatalf("overview failed: %v", err)
	}
	// The correction is recorded (auditable) but must not claim to have
	// applied to anything.
	if strings.Contains(out, "1 correction(s) applied") {
		t.Errorf("unknown target must not be counted as applied:\n%s", out)
	}
	out, err = runGmbCommand(t, "memory", "--dir", tempDir, "--corrections")
	if err != nil {
		t.Fatalf("corrections failed: %v", err)
	}
	if !strings.Contains(out, "ghost-component") {
		t.Errorf("correction not in audit log:\n%s", out)
	}
}
