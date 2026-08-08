package cmd_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestFusionStage_EndToEnd verifies the Stage 9 wiring: `gmb analyze
// --include-docs` must fuse ADR/README claims into developer memory, make
// them queryable through `gmb memory --ask`, and re-analyzing the same tree
// must not duplicate claims (pipeline-level idempotency).
func TestFusionStage_EndToEnd(t *testing.T) {
	root := setupAnalyzeGitRepo(t)
	adrDir := filepath.Join(root, "docs", "adr")
	if err := os.MkdirAll(adrDir, 0o755); err != nil {
		t.Fatal(err)
	}
	adr := "# Use Redis\n\n## Status\n\nAccepted\n\n## Decision\n\nUse Redis for session caching.\n"
	if err := os.WriteFile(filepath.Join(adrDir, "0001-use-redis.md"), []byte(adr), 0o644); err != nil {
		t.Fatal(err)
	}
	readme := "# Repo\n\nUses Redis and PostgreSQL.\n"
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte(readme), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitCmd(t, root, "add", ".")
	runGitCmd(t, root, "commit", "-q", "-m", "add docs")

	output, err := runGmbCommand(t, "analyze", "--dir", root, "--include-docs")
	if err != nil {
		t.Fatalf("analyze --include-docs failed: %v\n%s", err, output)
	}
	if !strings.Contains(output, "Stage 9") {
		t.Errorf("analyze output missing the Stage 9 summary:\n%s", output)
	}

	// Fused claims are queryable through the memory CLI.
	ask, err := runGmbCommand(t, "memory", "--dir", root, "--ask", "redis")
	if err != nil {
		t.Fatalf("gmb memory --ask failed: %v\n%s", err, ask)
	}
	if !strings.Contains(ask, "decided_to") || !strings.Contains(ask, "uses_technology") {
		t.Errorf("ask did not surface the fused ADR/README claims:\n%s", ask)
	}

	// Idempotency: re-running on the same tree appends nothing to the claims
	// WAL.
	claimsWAL := filepath.Join(root, ".glassmarble", "memory", "claims.jsonl")
	lines1 := countFileLines(t, claimsWAL)
	if lines1 == 0 {
		t.Fatalf("claims.jsonl empty after fusion")
	}
	if _, err := runGmbCommand(t, "analyze", "--dir", root, "--include-docs"); err != nil {
		t.Fatalf("second analyze failed: %v", err)
	}
	lines2 := countFileLines(t, claimsWAL)
	if lines2 != lines1 {
		t.Errorf("claims.jsonl grew from %d to %d lines after re-analyzing (idempotency violated)", lines1, lines2)
	}
}

// TestFusionStage_FlagDefaultsOff verifies --include-docs is opt-in: a plain
// `gmb analyze` must not run Stage 9 (no claims WAL is created).
func TestFusionStage_FlagDefaultsOff(t *testing.T) {
	root := setupAnalyzeGitRepo(t)
	adrDir := filepath.Join(root, "docs", "adr")
	if err := os.MkdirAll(adrDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(adrDir, "0001-use-redis.md"),
		[]byte("# Use Redis\n\n## Decision\n\nUse Redis.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitCmd(t, root, "add", ".")
	runGitCmd(t, root, "commit", "-q", "-m", "add docs")

	output, err := runGmbCommand(t, "analyze", "--dir", root)
	if err != nil {
		t.Fatalf("analyze failed: %v\n%s", err, output)
	}
	if strings.Contains(output, "Stage 9") {
		t.Errorf("Stage 9 ran without --include-docs:\n%s", output)
	}
	if _, err := os.Stat(filepath.Join(root, ".glassmarble", "memory", "claims.jsonl")); err == nil {
		t.Error("claims.jsonl exists after analyze WITHOUT --include-docs")
	}
}
