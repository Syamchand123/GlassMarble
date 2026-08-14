package commit_reasoning

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/arch_intelligence"
	"github.com/Syamchand123/GlassMarble/internal/archmodel"
)

func TestReasonCommit_Integration(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not available in PATH")
	}
	repoDir := t.TempDir()
	gitInit(t, repoDir)

	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(repoDir, name), []byte(content), 0644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repoDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	write("go.mod", "module example.com/test\n")
	git("add", "go.mod")
	git("commit", "-m", "init", "--date", "@1700000000")

	// A commit with real content, a squash PR marker and an issue keyword.
	if err := os.MkdirAll(filepath.Join(repoDir, "internal/pay"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	write("internal/pay/pay.go", "package pay\nfunc Run() {}\n")
	git("add", "internal/pay/pay.go")
	git("commit", "-m", "Add payment service fixes #42 (#88)", "--date", "@1700000100")

	head, err := exec.Command("git", "-C", repoDir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("rev-parse: %v", err)
	}
	commitHash := string(head[:len(head)-1])

	headSnap := &archmodel.ArchSnapshot{
		ID:         "head-snap",
		CommitHash: commitHash,
		Components: []archmodel.DetectedComponent{
			{ID: "pay", Name: "payment", Directories: []string{"internal/pay"}},
		},
		Metrics: archmodel.ArchMetrics{},
	}

	r := NewReasoner()
	events, err := r.ReasonCommit(context.Background(), ReasonInput{
		RepoDir:    repoDir,
		CommitHash: commitHash,
		BaseSnap:   &archmodel.ArchSnapshot{ID: "base-snap", CommitHash: "base"},
		HeadSnap:   headSnap,
	})
	if err != nil {
		t.Fatalf("ReasonCommit: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("expected at least one event")
	}
	for _, ev := range events {
		if ev.ID == "" {
			t.Error("event has empty ID")
		}
		if ev.CommitHash != commitHash {
			t.Errorf("CommitHash = %q, want %q", ev.CommitHash, commitHash)
		}
		if ev.Evidence.IsEmpty() {
			t.Errorf("event %s has empty evidence", ev.Kind)
		}
		if !ev.Timestamp.Equal(time.Unix(1700000100, 0).UTC()) {
			t.Errorf("event timestamp = %v, want 1700000100", ev.Timestamp)
		}
	}
	// The squash marker and the issue keyword must both be extracted.
	found := false
	for _, ev := range events {
		assertStrings(t, "PRs", ev.RelatedPRs, []string{"88"})
		assertStrings(t, "issues", ev.RelatedIssues, []string{"42"})
		// Intent must be wired onto every event.
		if ev.Intent == "" || ev.IntentSrc == "" {
			t.Errorf("event %s: empty intent (%q / %q)", ev.Kind, ev.Intent, ev.IntentSrc)
		}
		found = true
	}
	if !found {
		t.Fatal("no events examined")
	}
}

func TestReasonCommit_IDMatchesIntelligence(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not available in PATH")
	}
	repoDir := t.TempDir()
	gitInit(t, repoDir)
	writeAndCommit := func(name, content, msg string) string {
		t.Helper()
		if err := os.WriteFile(filepath.Join(repoDir, name), []byte(content), 0644); err != nil {
			t.Fatalf("write: %v", err)
		}
		cmd := exec.Command("git", "-C", repoDir, "add", name)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("add: %v\n%s", err, out)
		}
		cmd = exec.Command("git", "-C", repoDir, "commit", "-m", msg)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("commit: %v\n%s", err, out)
		}
		hash, err := exec.Command("git", "-C", repoDir, "rev-parse", "HEAD").Output()
		if err != nil {
			t.Fatalf("rev-parse: %v", err)
		}
		return string(hash[:len(hash)-1])
	}
	writeAndCommit("a.txt", "a\n", "init")

	// Base snapshot has one component without the "new" dependency; head has
	// the dependency added — both generators must emit DEPENDENCY_ADDED with
	// the SAME event ID.
	base := &archmodel.ArchSnapshot{
		Components: []archmodel.DetectedComponent{{ID: "svc", Name: "service"}},
	}
	head := &archmodel.ArchSnapshot{
		Components: []archmodel.DetectedComponent{{ID: "svc", Name: "service", Dependencies: []string{"db"}}, {ID: "db", Name: "db"}},
	}

	r := NewReasoner()
	commitHash := writeAndCommit("b.go", "package b\n", "Add dependency on db")
	events, err := r.ReasonCommit(context.Background(), ReasonInput{
		RepoDir:    repoDir,
		CommitHash: commitHash,
		BaseSnap:   base,
		HeadSnap:   head,
	})
	if err != nil {
		t.Fatalf("ReasonCommit: %v", err)
	}

	fiveD := arch_intelligence.GenerateEvents(base, head, nil, arch_intelligence.CommitMeta{Hash: commitHash, Timestamp: time.Now()})

	reasoningIDs := make(map[archmodel.EventKind]string)
	for _, ev := range events {
		reasoningIDs[ev.Kind] = ev.ID
	}
	for _, ev := range fiveD {
		if id, ok := reasoningIDs[ev.Kind]; ok && ev.ID != id {
			t.Errorf("kind %s: reasoning ID %q != intelligence ID %q — dedup will fail", ev.Kind, id, ev.ID)
		}
	}
}

func TestReasonCommit_InvalidInput(t *testing.T) {
	r := NewReasoner()
	if _, err := r.ReasonCommit(context.Background(), ReasonInput{}); err == nil {
		t.Error("empty input must error")
	}
	if _, err := r.ReasonCommitRange(context.Background(), "", "a", "b"); err == nil {
		t.Error("empty repo dir must error")
	}
}

func gitInit(t *testing.T, repoDir string) {
	t.Helper()
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "Test"},
	} {
		cmd := exec.Command("git", append([]string{"-C", repoDir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}
