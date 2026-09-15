// Package doc_engine — doc_engine_extra_test.go
//
// Whitebox coverage for Run-level internals: dossierChangeCount scoring,
// the branch-policy matrix (resolveWritePermission), comment-only bail,
// GlobalStyle merge precedence (via a recording stub provider through the
// real orchestrator), freshness recompute paths, and compute-only
// no-write-no-save proof. No network; git-backed cases skip when git is
// unavailable.
package doc_engine

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/ai_engine/provider"
	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/link"
	docconfig "github.com/Syamchand123/GlassMarble/internal/doc_engine/config"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/renderer"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/storage"
)

// ────────────────────────────────────────────────────────────────────────────
// dossierChangeCount scoring
// ────────────────────────────────────────────────────────────────────────────

func TestExtraDossierChangeCount(t *testing.T) {
	if got := dossierChangeCount(nil); got != 0 {
		t.Errorf("nil dossier = %d, want 0", got)
	}
	if got := dossierChangeCount(&docconfig.GlobalCommitDossier{}); got != 0 {
		t.Errorf("empty dossier = %d, want 0", got)
	}
	d := &docconfig.GlobalCommitDossier{
		AddedSymbols:      make([]docconfig.SymbolFact, 2),
		ModifiedSymbols:   make([]docconfig.SymbolDelta, 1),
		RemovedSymbols:    []string{"a", "b", "c"},
		AddedConfigVars:   make([]docconfig.ConfigVarFact, 1),
		RemovedConfigVars: []string{"OLD_FLAG"},
		AddedSentinels:    make([]docconfig.SentinelFact, 1),
		ModifiedSentinels: make([]docconfig.SentinelDelta, 2),
		ArchEvents:        []string{"LAYER_VIOLATION"},
	}
	// 2+1+3+1+1+1+2+1 = 12. Every field must contribute: a commit that only
	// touches config vars or sentinels is still a code change (never bailed).
	if got, want := dossierChangeCount(d), 12; got != want {
		t.Errorf("dossierChangeCount = %d, want %d", got, want)
	}
	for name, mutate := range map[string]func(*docconfig.GlobalCommitDossier){
		"added":    func(x *docconfig.GlobalCommitDossier) { x.AddedSymbols = []docconfig.SymbolFact{{}} },
		"modified": func(x *docconfig.GlobalCommitDossier) { x.ModifiedSymbols = []docconfig.SymbolDelta{{}} },
		"removed":  func(x *docconfig.GlobalCommitDossier) { x.RemovedSymbols = []string{"x"} },
		"cfg-add":  func(x *docconfig.GlobalCommitDossier) { x.AddedConfigVars = []docconfig.ConfigVarFact{{}} },
		"cfg-rm":   func(x *docconfig.GlobalCommitDossier) { x.RemovedConfigVars = []string{"x"} },
		"sent-add": func(x *docconfig.GlobalCommitDossier) { x.AddedSentinels = []docconfig.SentinelFact{{}} },
		"sent-mod": func(x *docconfig.GlobalCommitDossier) { x.ModifiedSentinels = []docconfig.SentinelDelta{{}} },
		"arch":     func(x *docconfig.GlobalCommitDossier) { x.ArchEvents = []string{"E"} },
	} {
		solo := &docconfig.GlobalCommitDossier{}
		mutate(solo)
		if got := dossierChangeCount(solo); got == 0 {
			t.Errorf("field %q alone must count as a change", name)
		}
	}
}

// ────────────────────────────────────────────────────────────────────────────
// Branch-policy matrix (main/feature/draft/tag-only/unknown × --write)
// ────────────────────────────────────────────────────────────────────────────

// extraGitRunner inits a git repo with one commit on main and returns a
// helper that runs git inside it.
func extraGitRunner(t *testing.T, dir string) func(...string) string {
	t.Helper()
	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	run("init", "-q", "-b", "main")
	run("config", "user.email", "extra@test.local")
	run("config", "user.name", "Extra Test")
	if err := os.WriteFile(filepath.Join(dir, "seed.txt"), []byte("seed\n"), 0644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-qm", "feat: seed commit")
	return run
}

func extraBranchRepo(t *testing.T) (string, func(...string) string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	return dir, extraGitRunner(t, dir)
}

func TestExtraBranchPolicyMatrix(t *testing.T) {
	cases := []struct {
		name       string
		setup      func(t *testing.T, dir string, run func(...string) string)
		policy     string
		forceWrite bool
		wantWrite  bool
		wantWarn   []string // fragments that must appear in warnings
		wantNoWarn []string // fragments that must NOT appear
	}{
		{
			name: "main-only on main writes silently",
			setup: func(t *testing.T, dir string, run func(...string) string) {
			},
			policy: "main-only", wantWrite: true,
		},
		{
			name: "main-only on master writes silently",
			setup: func(t *testing.T, dir string, run func(...string) string) {
				run("checkout", "-q", "-b", "master")
			},
			policy: "main-only", wantWrite: true,
		},
		{
			name: "main-only on feature is compute-only",
			setup: func(t *testing.T, dir string, run func(...string) string) {
				run("checkout", "-q", "-b", "feature/docs-refresh")
			},
			policy: "main-only", wantWrite: false,
			wantWarn: []string{"compute-only"},
		},
		{
			name: "feature with --write overrides",
			setup: func(t *testing.T, dir string, run func(...string) string) {
				run("checkout", "-q", "-b", "feature/docs-refresh")
			},
			policy: "main-only", forceWrite: true, wantWrite: true,
			wantWarn: []string{"ForceWrite override"},
		},
		{
			name:       "main with --write is a no-op confirmation without warning",
			setup:      func(t *testing.T, dir string, run func(...string) string) {},
			policy:     "main-only",
			forceWrite: true, wantWrite: true,
			wantNoWarn: []string{"ForceWrite override"},
		},
		{
			name: "any policy writes on feature without warning",
			setup: func(t *testing.T, dir string, run func(...string) string) {
				run("checkout", "-q", "-b", "feature/x")
			},
			policy: "any", wantWrite: true,
		},
		{
			name: "draft branch is compute-only even with --write",
			setup: func(t *testing.T, dir string, run func(...string) string) {
				run("checkout", "-q", "-b", "draft/docs-refresh")
			},
			policy: "any", forceWrite: true, wantWrite: false,
			wantWarn: []string{"draft"},
		},
		{
			name: "wip branch is compute-only (case-insensitive)",
			setup: func(t *testing.T, dir string, run func(...string) string) {
				run("checkout", "-q", "-b", "WIP-login-flow")
			},
			policy: "main-only", wantWrite: false,
			wantWarn: []string{"compute-only"},
		},
		{
			// NOTE: isExactTagHead probes `git describe --exact-match HEAD`,
			// which only matches ANNOTATED tags — a lightweight tag stays
			// compute-only by design (release tags are annotated).
			name: "tag-only on exact annotated tag writes",
			setup: func(t *testing.T, dir string, run func(...string) string) {
				run("tag", "-a", "v1.2.0", "-m", "release v1.2.0")
			},
			policy: "tag-only", wantWrite: true,
		},
		{
			name: "tag-only on lightweight tag stays compute-only",
			setup: func(t *testing.T, dir string, run func(...string) string) {
				run("tag", "v1.2.0")
			},
			policy: "tag-only", wantWrite: false,
			wantWarn: []string{"tag-only"},
		},
		{
			name: "tag-only off tag is compute-only",
			setup: func(t *testing.T, dir string, run func(...string) string) {
			},
			policy: "tag-only", wantWrite: false,
			wantWarn: []string{"tag-only"},
		},
		{
			name: "tag-only off tag stays compute-only even with --write",
			setup: func(t *testing.T, dir string, run func(...string) string) {
				run("checkout", "-q", "-b", "feature/x")
			},
			policy: "tag-only", forceWrite: true, wantWrite: true,
			// --write overrides tag-only on a non-draft branch.
			wantWarn: []string{"ForceWrite override"},
		},
		{
			name: "detached HEAD writes with warning",
			setup: func(t *testing.T, dir string, run func(...string) string) {
				run("checkout", "-q", "--detach", "HEAD")
			},
			policy: "main-only", wantWrite: true,
			wantWarn: []string{"detached HEAD"},
		},
		{
			name: "unknown policy falls back to main-only with warning",
			setup: func(t *testing.T, dir string, run func(...string) string) {
			},
			policy: "sometimes", wantWrite: true,
			wantWarn: []string{"unknown branch-policy"},
		},
		{
			name: "empty policy defaults to main-only",
			setup: func(t *testing.T, dir string, run func(...string) string) {
			},
			policy: "", wantWrite: true,
			wantNoWarn: []string{"unknown branch-policy"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel() // hermetic temp-dir git repos; no shared state
			dir, run := extraBranchRepo(t)
			tc.setup(t, dir, run)
			got, branch, warns := resolveWritePermission(dir, tc.policy, tc.forceWrite)
			if got != tc.wantWrite {
				t.Errorf("canWrite = %v, want %v (branch %q, warnings %v)", got, tc.wantWrite, branch, warns)
			}
			joined := strings.Join(warns, "\n")
			for _, w := range tc.wantWarn {
				if !strings.Contains(joined, w) {
					t.Errorf("warnings missing %q (branch %q): %v", w, branch, warns)
				}
			}
			for _, w := range tc.wantNoWarn {
				if strings.Contains(joined, w) {
					t.Errorf("warnings unexpectedly contain %q (branch %q): %v", w, branch, warns)
				}
			}
		})
	}
}

func TestExtraBranchPolicyUnknownBranch(t *testing.T) {
	// A non-git directory reports an unknown branch: strictly compute-only
	// under main-only, with an explicit warning.
	dir := t.TempDir()
	got, branch, warns := resolveWritePermission(dir, "main-only", false)
	if branch != "" {
		t.Errorf("branch = %q, want empty (unknown)", branch)
	}
	if got {
		t.Errorf("unknown branch must be compute-only, got canWrite=true (warnings %v)", warns)
	}
	if joined := strings.Join(warns, "\n"); !strings.Contains(joined, "unknown branch") {
		t.Errorf("expected unknown-branch warning, got %v", warns)
	}
	// "any" still writes when the branch is unknown.
	if got, _, _ := resolveWritePermission(dir, "any", false); !got {
		t.Error("policy any must write even with unknown branch")
	}
}

// ────────────────────────────────────────────────────────────────────────────
// Comment-only bail + compute-only no-write-no-save proof (via Run)
// ────────────────────────────────────────────────────────────────────────────

// extraDocRepo writes a minimal valid docs.yaml + one managed doc file.
func extraDocRepo(t *testing.T, dir string) {
	t.Helper()
	doc := docconfig.DocSpec{
		ID:         "demo",
		TargetPath: "docs/demo.md",
		Title:      "Demo",
		Purpose:    "Extra coverage fixture",
		Audience:   "Developers",
		Scope:      docconfig.ScopeRule{Paths: []string{"internal/demo/**"}},
		Sections: []docconfig.SectionSpec{
			{ID: "overview", Title: "Overview", Instruction: "Explain the demo module", Managed: true},
		},
	}
	if err := updateDocsYAML(dir, doc); err != nil {
		t.Fatalf("updateDocsYAML: %v", err)
	}
	target := filepath.Join(dir, "docs/demo.md")
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		t.Fatal(err)
	}
	md := "# Demo\n\n<!-- gmb:begin:overview -->\nOld demo body.\n<!-- gmb:end:overview -->\n"
	if err := os.WriteFile(target, []byte(md), 0644); err != nil {
		t.Fatal(err)
	}
}

func extraDemoGraph(commit string, withNode bool) *akg.CodePropertyGraph {
	g := akg.NewCodePropertyGraph(commit)
	if withNode {
		g.Nodes = g.Nodes.Set("internal/demo/app.go::Serve", &link.ResolvedNode{
			ID:   "internal/demo/app.go::Serve",
			Name: "Serve",
			Kind: "FUNCTION",
			FileSpec: link.LocationMeta{
				Path:      "internal/demo/app.go",
				LineStart: 10,
				LineEnd:   20,
			},
			Properties: map[string]string{"signature": "func Serve() error"},
		})
	}
	return g
}

func TestExtraRunCommentOnlyBail(t *testing.T) {
	t.Setenv("GMB_DOC_STATE", "json")
	dir := t.TempDir() // non-git → unknown branch, but comment-only short-circuits first
	extraDocRepo(t, dir)
	before, err := os.ReadFile(filepath.Join(dir, "docs/demo.md"))
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	res := Run(dir, RunOptions{CommitHash: "comment-only-hash", NoLLM: true, Out: &out})
	if res.Err != nil {
		t.Fatalf("Run returned critical error: %v", res.Err)
	}
	// Dirty discovery still ran (the section WOULD be stale on a real code
	// change), but the bail skips render: nothing is re-rendered or written.
	if res.SectionsUpdated != 1 {
		t.Errorf("comment-only run should still report 1 dirty section, got %d", res.SectionsUpdated)
	}
	if res.DocsUpdated != 0 {
		t.Errorf("comment-only run must write no docs, got %d", res.DocsUpdated)
	}
	after, err := os.ReadFile(filepath.Join(dir, "docs/demo.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Error("comment-only bail must not touch the target file")
	}
	// The run still marks the commit processed so the next run fast-bails.
	sm := storage.NewStateManager(docconfig.StorageDirPath(dir))
	state, err := sm.Load()
	if err != nil {
		t.Fatalf("loading state: %v", err)
	}
	if state.LastCommit != "comment-only-hash" {
		t.Errorf("LastCommit = %q, want %q", state.LastCommit, "comment-only-hash")
	}
}

// TestExtraRunWarnsOnMissingTargetFile guards against a regression where a
// document configured in docs.yaml whose target markdown file was never
// scaffolded (deleted by hand, or a docs.yaml entry hand-written without
// ever running `gmb doc init`) was silently skipped by Run() with no
// warning at all — `gmb doc check` reports "FAIL: file missing" clearly,
// but `gmb doc` itself gave no indication the document was never touched.
func TestExtraRunWarnsOnMissingTargetFile(t *testing.T) {
	t.Setenv("GMB_DOC_STATE", "json")
	dir := t.TempDir()
	// A real, working doc alongside the ghost one, so the run has a
	// genuine dossier change somewhere and doesn't fast-bail at the
	// top-level "no code changes in dossier" check before ever reaching
	// the per-document loop — matching a real `gmb doc --write` run
	// (no --force) against a repo with at least one real code change.
	extraDocRepo(t, dir)
	ghost := docconfig.DocSpec{
		ID:         "ghost",
		TargetPath: "docs/ghost.md",
		Title:      "Ghost",
		Scope:      docconfig.ScopeRule{Paths: []string{"internal/ghost/**"}},
		Sections: []docconfig.SectionSpec{
			{ID: "overview", Title: "Overview", Instruction: "Explain the ghost module", Managed: true},
		},
	}
	if err := updateDocsYAML(dir, ghost); err != nil {
		t.Fatalf("updateDocsYAML: %v", err)
	}
	// Deliberately never create docs/ghost.md.

	var out bytes.Buffer
	res := Run(dir, RunOptions{
		CommitHash: "ghost-hash", NoLLM: true, ForceWrite: true,
		HeadGraph: extraDemoGraph("ghost-hash", true),
		Out:       &out,
	})
	if res.Err != nil {
		t.Fatalf("Run returned critical error: %v", res.Err)
	}
	found := false
	for _, w := range res.Warnings {
		if strings.Contains(w, "docs/ghost.md") && strings.Contains(w, "does not exist") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected a missing-target-file warning for docs/ghost.md, got warnings: %v", res.Warnings)
	}
	if _, err := os.Stat(filepath.Join(dir, "docs/ghost.md")); !os.IsNotExist(err) {
		t.Errorf("Run() must not create the missing target file itself when it was never dirty (that's doc init's job)")
	}
}

func TestExtraRunComputeOnlyNoWriteNoSave(t *testing.T) {
	t.Setenv("GMB_DOC_STATE", "json")
	dir := t.TempDir() // non-git → unknown branch → compute-only under main-only
	extraDocRepo(t, dir)
	before, err := os.ReadFile(filepath.Join(dir, "docs/demo.md"))
	if err != nil {
		t.Fatal(err)
	}
	base := extraDemoGraph("base", false)
	head := extraDemoGraph("head", true)
	var out bytes.Buffer
	res := Run(dir, RunOptions{
		CommitHash: "compute-only-hash",
		NoLLM:      true,
		BaseGraph:  base,
		HeadGraph:  head,
		Out:        &out,
	})
	if res.Err != nil {
		t.Fatalf("Run returned critical error: %v", res.Err)
	}
	if res.SectionsUpdated == 0 {
		t.Error("fixture graph delta should mark sections dirty (would-update)")
	}
	if res.DocsUpdated != 0 {
		t.Errorf("compute-only run must write no docs, got %d", res.DocsUpdated)
	}
	if !strings.Contains(out.String(), "would update") {
		t.Errorf("compute-only run must report would-update lines, got:\n%s", out.String())
	}
	after, err := os.ReadFile(filepath.Join(dir, "docs/demo.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Error("compute-only run must not modify the target file")
	}
	// No state file may be created: compute-only returns before every Save.
	if _, err := os.Stat(filepath.Join(docconfig.StorageDirPath(dir), "docs_state.json")); !os.IsNotExist(err) {
		t.Error("compute-only run must not create docs_state.json")
	}
	// Ledger is post-write observability: nothing may be recorded either.
	if _, err := os.Stat(filepath.Join(dir, ".glassmarble", "runs", "runs.jsonl")); !os.IsNotExist(err) {
		t.Error("compute-only run must not append to the run ledger")
	}
}

// ────────────────────────────────────────────────────────────────────────────
// GlobalStyle merge precedence through the real orchestrator
// ────────────────────────────────────────────────────────────────────────────

// extraRecordingProvider is a stub LLM provider (mirroring the mockProvider
// pattern in renderer tests) that records system prompts for inspection.
type extraRecordingProvider struct {
	text    string
	systems []string
	calls   int
}

func (m *extraRecordingProvider) Name() string { return "extra-stub" }

func (m *extraRecordingProvider) Complete(_ context.Context, req provider.Request) (*provider.Response, error) {
	m.calls++
	m.systems = append(m.systems, req.System)
	return &provider.Response{
		Text:  m.text,
		Usage: provider.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
	}, nil
}

func (m *extraRecordingProvider) Ping(_ context.Context, _ string) error { return nil }

func TestExtraGlobalStyleMergePrecedence(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "docs/demo.md")
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		t.Fatal(err)
	}
	md := "# Demo\n\n<!-- gmb:begin:overview -->\nOld demo body.\n<!-- gmb:end:overview -->\n"
	if err := os.WriteFile(target, []byte(md), 0644); err != nil {
		t.Fatal(err)
	}
	doc := &docconfig.DocSpec{
		ID:         "demo",
		TargetPath: "docs/demo.md",
		Title:      "Demo",
		Purpose:    "Style precedence fixture",
		Audience:   "Developers",
		Scope:      docconfig.ScopeRule{Paths: []string{"internal/demo/**"}},
		Sections: []docconfig.SectionSpec{
			{ID: "overview", Title: "Overview", Instruction: "Explain the demo module", Managed: true},
		},
		// Per-doc overrides Voice + JargonBlacklist but leaves Tone empty:
		// Tone must fall back to the global style.
		Style: &docconfig.StyleSpec{Voice: "per-doc voice", JargonBlacklist: []string{"per-doc-jargon"}},
	}
	stub := &extraRecordingProvider{text: "Fresh demo prose without backticks."}
	orch := renderer.NewOrchestrator(renderer.OrchestratorOptions{
		Provider: stub,
		Model:    "extra-test",
		Out:      io.Discard,
		GlobalStyle: docconfig.StyleSpec{
			Voice:           "global voice",
			Tone:            "global tone",
			JargonBlacklist: []string{"global-jargon"},
		},
	})
	sm := storage.NewStateManager(docconfig.StorageDirPath(dir))
	changed, _, _, err := orch.ProcessDocument(context.Background(), dir, doc, nil, nil, nil, sm, "style-hash")
	if err != nil {
		t.Fatalf("ProcessDocument: %v", err)
	}
	if !changed {
		t.Fatal("expected the stub LLM render to change the document")
	}
	if stub.calls == 0 {
		t.Fatal("expected at least one stub LLM call")
	}
	sys := strings.Join(stub.systems, "\n")
	if !strings.Contains(sys, "per-doc voice") {
		t.Errorf("per-doc Voice must win over global, system prompt was:\n%s", sys)
	}
	if strings.Contains(sys, "global voice") {
		t.Errorf("global Voice must not leak through, system prompt was:\n%s", sys)
	}
	if !strings.Contains(sys, "per-doc-jargon") {
		t.Errorf("per-doc JargonBlacklist must win over global, system prompt was:\n%s", sys)
	}
	if strings.Contains(sys, "global-jargon") {
		t.Errorf("global jargon must not leak through, system prompt was:\n%s", sys)
	}
	if !strings.Contains(sys, "global tone") {
		t.Errorf("empty per-doc Tone must fall back to global, system prompt was:\n%s", sys)
	}
}

// ────────────────────────────────────────────────────────────────────────────
// Freshness recompute paths
// ────────────────────────────────────────────────────────────────────────────

func TestExtraFreshnessGitFailureYieldsZero(t *testing.T) {
	doc := docconfig.DocSpec{
		ID:         "fresh",
		TargetPath: "docs/fresh.md",
		Scope:      docconfig.ScopeRule{Paths: []string{"scope/**"}},
	}
	// Non-git directory: every git probe fails → (0, 0), never a panic.
	score, behind := ComputeFreshnessScore(t.TempDir(), doc, "deadbeef")
	if score != 0 || behind != 0 {
		t.Errorf("non-git ComputeFreshnessScore = (%d, %d), want (0, 0)", score, behind)
	}
	score, behind = ComputeFreshnessScoreWithArchEvents(t.TempDir(), doc, "", []string{"CYCLE_INTRODUCED"})
	if score != 0 || behind != 0 {
		t.Errorf("non-git ComputeFreshnessScoreWithArchEvents = (%d, %d), want (0, 0)", score, behind)
	}
}

func TestExtraWeightForCommitSubjectMatrix(t *testing.T) {
	for _, tc := range []struct {
		subject string
		want    int
	}{
		{"refactor the auth layer", 15},
		{"feat: add login endpoint", 15},
		{"fix crash on empty token", 8},
		{"perf: speed up cold start", 5},
		{"secur: rotate signing keys", 5},
		{"test: cover retry backoff", 2},
		{"docs: rewrite the runbook", 2},
		{"chore: bump linter version", 1},
		{"depend: update yaml library", 1},
		{"whgru qzxv mblep kword", 5}, // unknown intent → keyword default
	} {
		if got := keywordWeightForSubject(tc.subject); got != tc.want {
			t.Errorf("keywordWeightForSubject(%q) = %d, want %d", tc.subject, got, tc.want)
		}
	}
	// Intent-classified subjects agree with the keyword fallback on the
	// canonical verbs, so scoring is stable whichever path answers.
	for _, tc := range []struct {
		subject string
		want    int
	}{
		{"refactor the auth layer", 15},
		{"fix crash on empty token", 8},
		{"docs: rewrite the runbook", 2},
	} {
		if got := weightForCommitSubject(tc.subject); got != tc.want {
			t.Errorf("weightForCommitSubject(%q) = %d, want %d", tc.subject, got, tc.want)
		}
	}
	if got := weightForCommitSubject("whgru qzxv mblep kword"); got != 5 {
		t.Errorf("weightForCommitSubject(gibberish) = %d, want keyword default 5", got)
	}
}

// ────────────────────────────────────────────────────────────────────────────
// Misc internals: filtering, hashing helpers, assert closure
// ────────────────────────────────────────────────────────────────────────────

func TestExtraFilterDocuments(t *testing.T) {
	docs := []docconfig.DocSpec{
		{ID: "a", Tags: []string{"api"}},
		{ID: "b", Tags: []string{"api", "security"}},
		{ID: "c", Tags: []string{"ops"}},
	}
	if got := filterDocuments(docs, "", ""); len(got) != 3 {
		t.Errorf("empty filter must return all docs, got %d", len(got))
	}
	if got := filterDocuments(docs, "b", ""); len(got) != 1 || got[0].ID != "b" {
		t.Errorf("id filter failed: %+v", got)
	}
	if got := filterDocuments(docs, "", "api"); len(got) != 2 {
		t.Errorf("tag filter failed: %+v", got)
	}
	if got := filterDocuments(docs, "b", "ops"); len(got) != 0 {
		t.Errorf("combined id+tag filter must intersect, got %+v", got)
	}
	if got := filterDocuments(docs, "missing", ""); len(got) != 0 {
		t.Errorf("unknown id must yield empty, got %+v", got)
	}
	if shortHash("abcdef1234567890") != "abcdef12" {
		t.Errorf("shortHash must keep 8 chars, got %q", shortHash("abcdef1234567890"))
	}
	if shortHash("abc") != "abc" {
		t.Errorf("shortHash must not pad short input, got %q", shortHash("abc"))
	}
}

func TestExtraCatalogCacheKey(t *testing.T) {
	dir := t.TempDir()
	a := docsConfigCacheKey(dir, "", "")
	b := docsConfigCacheKey(dir, "", "")
	if a == "" || a != b {
		t.Errorf("cache key must be stable, got %q vs %q", a, b)
	}
	if got := docsConfigCacheKey(dir, "auth", ""); got == a {
		t.Error("doc filter must participate in the cache key")
	}
	if got := docsConfigCacheKey(dir, "", "api"); got == a {
		t.Error("tag filter must participate in the cache key")
	}
}

func TestExtraBuildAssertSymbolExists(t *testing.T) {
	// No graph and no repo symbols: permissive, never fails blindly.
	allow := buildAssertSymbolExists(nil, t.TempDir())
	if !allow("AnythingAtAll") {
		t.Error("empty-source closure must report every symbol known")
	}
	// Graph-backed: exact IDs, short names after :: and ., unknown rejected.
	g := akg.NewCodePropertyGraph("assert-test")
	g.Nodes = g.Nodes.Set("internal/auth/jwt.go::ValidateToken", &link.ResolvedNode{
		ID:   "internal/auth/jwt.go::ValidateToken",
		Name: "ValidateToken",
		Kind: "FUNCTION",
		FileSpec: link.LocationMeta{
			Path:      "internal/auth/jwt.go",
			LineStart: 1,
			LineEnd:   5,
		},
	})
	known := buildAssertSymbolExists(g, t.TempDir())
	for _, sym := range []string{
		"internal/auth/jwt.go::ValidateToken",
		"ValidateToken",
	} {
		if !known(sym) {
			t.Errorf("graph-backed closure must know %q", sym)
		}
	}
	if known("HallucinatedSymbol") {
		t.Error("graph-backed closure must reject unknown symbols")
	}
}

func TestExtraNormalizeBranchPolicy(t *testing.T) {
	if normalizeBranchPolicy("") != "main-only" {
		t.Errorf("empty policy must default to main-only, got %q", normalizeBranchPolicy(""))
	}
	if normalizeBranchPolicy("any") != "any" {
		t.Errorf("explicit policy must pass through, got %q", normalizeBranchPolicy("any"))
	}
	if displayBranch("") != "unknown" {
		t.Errorf("empty branch must display as unknown, got %q", displayBranch(""))
	}
}

func TestExtraHasNonExecutableCodeBlocks(t *testing.T) {
	if hasNonExecutableCodeBlocks("# Plain prose, no fences.") {
		t.Error("prose without fences must not flag")
	}
	// Executability rides on the opening fence info string, not the body.
	if hasNonExecutableCodeBlocks("```go:exec\nfmt.Println(1)\n```\n") {
		t.Error("exec-tagged fence must not flag")
	}
	if !hasNonExecutableCodeBlocks("```go\nfmt.Println(\"exec\")\n```\n") {
		t.Error("the word exec in the body does not make a fence executable")
	}
	if hasNonExecutableCodeBlocks("```go no_run\ncode\n```\n") {
		t.Error("no_run fence must not flag")
	}
	if !hasNonExecutableCodeBlocks("```go\nfmt.Println(1)\n```\n") {
		t.Error("plain fence must flag as non-executable")
	}
	if !hasNonExecutableCodeBlocks("```go\r\nfmt.Println(1)\r\n```\r\n") {
		t.Error("CRLF fences must flag like LF fences")
	}
	// A document-level exec directive excuses every block.
	if hasNonExecutableCodeBlocks("<!-- gmb:snippet:exec -->\n```go\ncode\n```\n") {
		t.Error("document exec directive must excuse plain fences")
	}
}

func TestExtraExportStateJSONViaEngine(t *testing.T) {
	t.Setenv("GMB_DOC_STATE", "json")
	dir := t.TempDir()
	extraDocRepo(t, dir)
	sm := storage.NewStateManager(docconfig.StorageDirPath(dir))
	state, err := sm.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	ds := storage.GetOrCreateDocState(state, "docs/demo.md")
	ds.FreshnessScore = 77
	if err := sm.Save(state); err != nil {
		t.Fatalf("Save: %v", err)
	}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		SchemaVersion int `json:"schema_version"`
		Documents     map[string]struct {
			FreshnessScore int `json:"freshness_score"`
		} `json:"documents"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("state must round-trip as JSON: %v", err)
	}
	if decoded.Documents["docs/demo.md"].FreshnessScore != 77 {
		t.Errorf("state JSON lost the freshness score: %+v", decoded.Documents)
	}
}
