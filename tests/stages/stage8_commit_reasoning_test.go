package stages_test

// Stage 8 (commit_reasoning) tests against a real git repository in the
// harness sandbox.
//
// MAJOR discrepancy from API_REFERENCE.md: the documented API
// (NewReasoner(repoPath), ReasonAboutCommit, CommitReason, CommitMatchesCategory,
// ContainsMergedPR, ContainsDeployment, BuildContext, and the BUILD/FIX/
// RESOURCE/MISC category enum) does NOT exist in internal/commit_reasoning.
// The real API is:
//
//   - NewReasoner(opts ...ReasonerOption) *Reasoner        (no repo path)
//   - ReasonCommit(ctx, ReasonInput) ([]archmodel.ArchEvent, error)
//   - ReasonCommitRange(ctx, repoDir, from, to) ([]archmodel.ArchEvent, error)
//   - NewIntentExtractor / ClassifyIntent for intent classification
//     (IntentFixBug, IntentInfrastructure, IntentAddFeature, IntentRefactor, ...)
//   - ExtractRelatedRefs for PR/issue extraction from commit messages
//
// Events are only produced when snapshot/graph evidence is supplied: a bare
// message-only commit yields zero events (no message-level classification
// pass exists in classifier.go). PR/deploy detection is therefore asserted
// through ExtractRelatedRefs and ClassifyIntent, the real equivalents.

import (
	"context"
	"strings"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/archmodel"
	"github.com/Syamchand123/GlassMarble/internal/commit_reasoning"
	"github.com/Syamchand123/GlassMarble/internal/git"
	"github.com/Syamchand123/GlassMarble/tests/harness"
)

func TestReasonCommitFixCommit(t *testing.T) {
	sb := harness.NewSandbox(t)
	sb.GitInit()
	hash := sb.GitCommitFiles("fix: resolve cache race condition", map[string]string{
		"internal/cache/cache.go": "package cache\n\nfunc Get(k string) string { return \"\" }\n",
	})

	base := &archmodel.ArchSnapshot{ID: "base", CommitHash: "base",
		Components: []archmodel.DetectedComponent{{ID: "comp_cache", Name: "cache"}}}
	head := &archmodel.ArchSnapshot{ID: "head", CommitHash: hash,
		Components: []archmodel.DetectedComponent{
			{ID: "comp_cache", Name: "cache", Dependencies: []string{"comp_db"}},
			{ID: "comp_db", Name: "db"},
		}}

	r := commit_reasoning.NewReasoner()
	in := commit_reasoning.ReasonInput{RepoDir: sb.Root, CommitHash: hash, BaseSnap: base, HeadSnap: head}
	events, err := r.ReasonCommit(context.Background(), in)
	if err != nil {
		t.Fatalf("ReasonCommit: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("no events produced for the fix commit")
	}
	depFound := false
	for _, ev := range events {
		if ev.ID == "" {
			t.Error("event with empty ID")
		}
		if ev.CommitHash != hash {
			t.Errorf("CommitHash = %q, want %q", ev.CommitHash, hash)
		}
		if ev.Evidence.IsEmpty() {
			t.Errorf("event %s has empty evidence", ev.Kind)
		}
		if ev.Intent != string(commit_reasoning.IntentFixBug) {
			t.Errorf("event %s intent = %q, want FIX_BUG (message starts with %q)", ev.Kind, ev.Intent, "fix:")
		}
		if ev.Kind == archmodel.EventDependencyAdded {
			depFound = true
		}
	}
	if !depFound {
		t.Error("expected a DEPENDENCY_ADDED event for cache -> db")
	}

	again, err := r.ReasonCommit(context.Background(), in)
	if err != nil {
		t.Fatalf("second ReasonCommit: %v", err)
	}
	if len(again) != len(events) {
		t.Fatalf("runs differ in event count: %d vs %d", len(again), len(events))
	}
	for i := range events {
		if events[i].ID != again[i].ID || events[i].Kind != again[i].Kind {
			t.Errorf("event %d differs between runs: %s/%s vs %s/%s",
				i, events[i].ID, events[i].Kind, again[i].ID, again[i].Kind)
		}
	}
}

func TestReasonCommitIntentCategories(t *testing.T) {
	cases := []struct {
		name    string
		subject string
		files   []string
		want    commit_reasoning.Intent
	}{
		{name: "fix", subject: "fix: resolve cache race condition", want: commit_reasoning.IntentFixBug},
		// Structural evidence (CI file) wins over the "add" keyword:
		// every touched file matches a build/CI rule.
		{name: "build ci pipeline", subject: "build: add CI pipeline",
			files: []string{".github/workflows/ci.yml"}, want: commit_reasoning.IntentInfrastructure},
		{name: "add feature", subject: "adds new resource bundle", want: commit_reasoning.IntentAddFeature},
		{name: "refactor", subject: "refactor stuff", want: commit_reasoning.IntentRefactor},
		{name: "deploy", subject: "deploy the cache service to production", want: commit_reasoning.IntentInfrastructure},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			meta := &git.CommitMeta{Subject: tc.subject, Files: tc.files}
			res := commit_reasoning.ClassifyIntent(meta, "")
			if res.Intent != tc.want {
				t.Errorf("ClassifyIntent(%q) = %q, want %q", tc.subject, res.Intent, tc.want)
			}
		})
	}
}

func TestReasonCommitMergedPRAndDeploy(t *testing.T) {
	sb := harness.NewSandbox(t)
	sb.GitInit()
	// Branch ref must not carry fix-shaped tokens ("cache-fix" would match
	// the FIX_BUG regex before the deploy rule fires).
	hash := sb.GitCommitFiles("Merge pull request #123 from acme/cache-service\n\nDeploy the cache service to production", map[string]string{
		"deploy.yaml": "image: cache:v2\n",
	})

	meta, err := git.ReadCommit(sb.Root, hash)
	if err != nil {
		t.Fatalf("ReadCommit: %v", err)
	}
	if !strings.Contains(meta.Subject, "Merge pull request") {
		t.Fatalf("subject = %q, want a merge-pull-request message", meta.Subject)
	}
	commit_reasoning.ExtractRelatedRefs(meta)
	if len(meta.RelatedPRs) != 1 || meta.RelatedPRs[0] != "123" {
		t.Errorf("RelatedPRs = %v, want [123] (squash/merge marker)", meta.RelatedPRs)
	}

	// The deployment mention classifies as infrastructure intent.
	res := commit_reasoning.ClassifyIntent(meta, "")
	if res.Intent != commit_reasoning.IntentInfrastructure {
		t.Errorf("deploy commit intent = %q, want INFRASTRUCTURE", res.Intent)
	}
}

func TestReasonCommitRange(t *testing.T) {
	sb := harness.NewSandbox(t)
	sb.GitInit()
	first := sb.GitCommitFiles("fix: cache race", map[string]string{"a.txt": "a\n"})
	second := sb.GitCommitFiles("add resource bundle", map[string]string{"b.txt": "b\n"})

	r := commit_reasoning.NewReasoner()
	events, err := r.ReasonCommitRange(context.Background(), sb.Root, first, second)
	if err != nil {
		t.Fatalf("ReasonCommitRange: %v", err)
	}
	// Without snapshot/graph evidence no classification pass fires, so the
	// range yields no events (verified against ClassifyChange); the call
	// still resolves the range without error.
	again, err := r.ReasonCommitRange(context.Background(), sb.Root, first, second)
	if err != nil {
		t.Fatalf("second ReasonCommitRange: %v", err)
	}
	if len(events) != len(again) {
		t.Errorf("range result not deterministic: %d vs %d events", len(events), len(again))
	}
	for _, ev := range events {
		if ev.CommitHash == "" || ev.ID == "" {
			t.Errorf("range event missing identity: %+v", ev)
		}
	}
}

func TestReasonCommitInvalidInputs(t *testing.T) {
	r := commit_reasoning.NewReasoner()

	if _, err := r.ReasonCommit(context.Background(), commit_reasoning.ReasonInput{}); err == nil {
		t.Error("ReasonCommit with empty input must error")
	}
	if _, err := r.ReasonCommit(context.Background(), commit_reasoning.ReasonInput{
		RepoDir: t.TempDir() + "/does-not-exist", CommitHash: "HEAD",
	}); err == nil {
		t.Error("ReasonCommit on a nonexistent repo must error")
	}
	if _, err := r.ReasonCommitRange(context.Background(), "", "a", "b"); err == nil {
		t.Error("ReasonCommitRange with empty repo dir must error")
	}
}
