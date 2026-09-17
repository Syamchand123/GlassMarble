package e2e_test

// BLACKBOX end-to-end + system coverage for the docs-engine feature
// (internal/doc_engine + `gmb doc*` CLI).
//
// Rules for this file:
//   - Blackbox only: drive the CLI in-process via the harness (gmb/gmbErr)
//     or, where real process exit codes are required, via the built binary.
//     No production code is imported or modified.
//   - No real network: every Track A (LLM) test uses harness.NewMockLLM.
//     The single live-provider smoke test skips unless
//     GLASSMARBLE_TEST_LIVE_LLM=1.
//   - Fast: tiny fixtures (1-3 Go files), no sleeps, one binary build shared
//     by all exit-code cases, a single `gmb analyze` in the whole file.
//   - No t.Parallel anywhere (the harness runner mutates os.Stdout + CWD).

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Syamchand123/GlassMarble/tests/harness"
)

const (
	docShopGo = `package shop

// Greet returns a friendly greeting for name.
func Greet(name string) string {
	return "hello " + name
}
`

	docShopCallerGo = `package shop

// Fetch looks up an item by id.
func Fetch(id string) (string, error) {
	if id == "" {
		return "", ErrItemNotFound
	}
	return id, nil
}
`

	docShopErrorsGo = `package shop

import "errors"

// ErrItemNotFound is returned when a catalog lookup misses the requested item.
var ErrItemNotFound = errors.New("item not found")
`

	docGoMod = `module example.com/shop

go 1.21
`
)

// docShopSandbox creates a sandbox git repo (branch main) with a minimal Go
// package and returns the sandbox plus HEAD.
func docShopSandbox(t *testing.T, withSentinel bool) (*harness.Sandbox, string) {
	t.Helper()
	sb := harness.NewSandbox(t)
	sb.RequireGit()
	sb.WriteFile("go.mod", docGoMod)
	sb.WriteFile("pkg/shop/shop.go", docShopGo)
	if withSentinel {
		sb.WriteFile("pkg/shop/errors.go", docShopErrorsGo)
	}
	sb.GitInit()
	return sb, sb.GitHead()
}

// docWriteConfig writes a minimal single-document docs.yaml for the shop
// fixture. failThreshold <0 means "no constraints block".
// NOTE: no archetype is set on purpose — archetype defaults render
// double-wrapped mermaid fences that leave an unclosed fence swallowing the
// end anchor under the goldmark parser; the archetype-free shape keeps
// managed zones machine-readable (see `doc init` journey for archetypes).
func docWriteConfig(t *testing.T, sb *harness.Sandbox, failThreshold int) {
	t.Helper()
	var b strings.Builder
	b.WriteString("version: 1\ndocs_dir: docs\ntarget_platform: github_flat\n")
	if failThreshold >= 0 {
		b.WriteString("constraints:\n")
		b.WriteString("  min_freshness_threshold: 0\n")
		b.WriteString("  min_freshness_fail: " + strconv.Itoa(failThreshold) + "\n")
	}
	b.WriteString(`documents:
  - id: guide
    target: docs/guide.md
    title: Shop Guide
    purpose: Reference for the shop package.
    audience: Developers
    scope:
      paths:
        - pkg/shop/**
    sections:
      - id: overview
        title: Overview
        instruction: Describe the exported helpers in this package.
        managed: true
`)
	sb.WriteFile(".glassmarble/docs.yaml", b.String())
}

// docRunWrite runs the standard deterministic sync used across tests.
func docRunWrite(t *testing.T, sb *harness.Sandbox, commit string, extra ...string) string {
	t.Helper()
	args := []string{"doc", "--commit", commit, "--write", "--force", "--no-llm"}
	args = append(args, extra...)
	return gmb(t, sb, args...)
}

// mustContain fails the test when any fragment is missing from out.
func mustContain(t *testing.T, out string, fragments ...string) {
	t.Helper()
	for _, f := range fragments {
		if !strings.Contains(out, f) {
			t.Errorf("output missing %q\n--- output ---\n%s", f, out)
		}
	}
}

// mustNotContain fails the test when any fragment is present in out.
func mustNotContain(t *testing.T, out string, fragments ...string) {
	t.Helper()
	for _, f := range fragments {
		if strings.Contains(out, f) {
			t.Errorf("output unexpectedly contains %q\n--- output ---\n%s", f, out)
		}
	}
}

// parseJSONObject extracts the first {...} block from combined CLI output
// (stdout+stderr share one capture in-process) and unmarshals it.
func parseJSONObject(t *testing.T, out string) map[string]any {
	t.Helper()
	start := strings.Index(out, "{")
	end := strings.LastIndex(out, "}")
	if start < 0 || end <= start {
		t.Fatalf("no JSON object in output\n--- output ---\n%s", out)
	}
	var v map[string]any
	if err := json.Unmarshal([]byte(out[start:end+1]), &v); err != nil {
		t.Fatalf("output JSON does not parse: %v\n--- output ---\n%s", err, out)
	}
	return v
}

func jsonKeys(v map[string]any) map[string]bool {
	out := make(map[string]bool, len(v))
	for k := range v {
		out[k] = true
	}
	return out
}

func requireKeys(t *testing.T, what string, v map[string]any, keys ...string) {
	t.Helper()
	have := jsonKeys(v)
	for _, k := range keys {
		if !have[k] {
			t.Errorf("%s JSON missing key %q (have %v)", what, k, v)
		}
	}
}

// ---------------------------------------------------------------------------
// 1. Full journey: init -> write -> identical rerun -> check -> drift ->
//    resync -> fresh.
// ---------------------------------------------------------------------------

func TestDocEngineJourney(t *testing.T) {
	sb, _ := docShopSandbox(t, false)

	// init docs.yaml via the CLI (non-interactive: scope + archetype given).
	out := gmb(t, sb, "doc", "init", "docs/guide.md",
		"--scope", "pkg/shop/**", "--archetype", "module", "--title", "Shop Guide")
	mustContain(t, out, "Scaffolded docs/guide.md")
	if !sb.Exists("docs/guide.md") || !sb.Exists(".glassmarble/docs.yaml") {
		t.Fatalf("doc init did not create docs/guide.md + docs.yaml")
	}
	// Fail-fast freshness so a later in-scope commit is observable drift.
	cfg := sb.ReadFile(".glassmarble/docs.yaml")
	sb.WriteFile(".glassmarble/docs.yaml", cfg+"constraints:\n  min_freshness_fail: 100\n")
	head := sb.GitCommit("add docs scaffold")

	// write: deterministic full sync.
	out = docRunWrite(t, sb, head)
	before := sb.ReadFile("docs/guide.md")
	// NOTE: the `doc` CLI path is graph-less by design (no AKG is loaded),
	// so deterministic zones carry structural markers, not symbol tables.
	mustContain(t, before, "gmb:begin:", "gmb:mode:deterministic")
	_ = out

	// rerun byte-identical (zero churn) even with --force.
	out = docRunWrite(t, sb, head)
	after := sb.ReadFile("docs/guide.md")
	if before != after {
		t.Errorf("forced rerun was not byte-identical (%d vs %d bytes)", len(before), len(after))
	}

	// check fresh.
	gmbWant(t, sb, []string{"fresh"}, "doc", "check")

	// break code: rename the exported func, commit, check must report drift.
	sb.WriteFile("pkg/shop/shop.go", strings.Replace(docShopGo, "func Greet(", "func GreetV2(", 1))
	brokenHead := sb.GitCommit("refactor: rename Greet to GreetV2")
	driftOut, err := gmbErr(t, sb, "doc", "check")
	if err == nil {
		t.Fatalf("expected doc check drift after rename, got success:\n%s", driftOut)
	}
	mustContain(t, driftOut, "docs/guide.md")

	// sync, then fresh again. The sync advances engine state to the new HEAD
	// (content is graph-less deterministic output, so state is the signal).
	docRunWrite(t, sb, brokenHead)
	stateOut := gmb(t, sb, "doc", "export", "--format", "state")
	mustContain(t, stateOut, brokenHead)
	gmbWant(t, sb, []string{"fresh"}, "doc", "check")
}

// ---------------------------------------------------------------------------
// 2. All 8 pipeline stages observable blackbox.
// ---------------------------------------------------------------------------

func TestDocEngineEightStages(t *testing.T) {
	sb, _ := docShopSandbox(t, false)
	docWriteConfig(t, sb, -1)
	head := sb.GitCommit("add docs config")

	// Stages 2-8: first verbose write (dossier, dirty discovery, grounding,
	// anchors, render, gates, atomic write + state).
	out := docRunWrite(t, sb, head, "--verbose")
	mustContain(t, out, "processing 1 document(s)") // stage 2: dossier built
	mustContain(t, out, "dirty section(s)")         // stage 3: dirty discovery
	body := sb.ReadFile("docs/guide.md")
	// Stage 4 (grounding): the deterministic renderer only emits its mode
	// tag after the grounding + render + gate pipeline ran for the section;
	// the zone echoes the section instruction it was grounded with.
	mustContain(t, body, "gmb:mode:deterministic", "Describe the exported helpers")
	mustContain(t, body, "gmb:begin:overview") // stage 5: markdown anchors

	// Stage 6: deterministic render — forced rerun is byte-identical.
	again := docRunWrite(t, sb, head)
	_ = again
	if second := sb.ReadFile("docs/guide.md"); second != body {
		t.Errorf("stage 6 (deterministic render): forced rerun changed bytes")
	}

	// Stage 7: 5 gates — fresh check (reference integrity + asserts) passes
	// and emitted markdown has balanced fences (gate 1 evidence).
	gmbWant(t, sb, []string{"fresh"}, "doc", "check")
	if n := strings.Count(body, "```"); n%2 != 0 {
		t.Errorf("stage 7 (gates): unbalanced code fences (%d)", n)
	}

	// Stage 1: fast-bail — a docs-only commit touches no scope, so the next
	// run must bail without writing.
	sb.WriteFile("README.md", "# shop\n\nDocs-only touch.\n")
	docsHead := sb.GitCommit("update readme docs")
	start := time.Now()
	bailOut := gmb(t, sb, "doc", "--commit", docsHead, "--write", "--no-llm", "--verbose")
	elapsed := time.Since(start)
	mustContain(t, bailOut, "fast-bail")
	if got := sb.ReadFile("docs/guide.md"); got != body {
		t.Errorf("stage 1 (fast-bail): guide.md changed during no-op run")
	}
	t.Logf("fast-bail no-op took %s", elapsed)
	if elapsed > 30*time.Second {
		t.Errorf("stage 1 (fast-bail): no-op took too long: %s", elapsed)
	}

	// Stage 8: atomic write + state — state still points at the last real
	// run and the ledger recorded it.
	stateOut := gmb(t, sb, "doc", "export", "--format", "state")
	mustContain(t, stateOut, `"last_commit"`, head)
	ledgerOut := gmb(t, sb, "doc", "ledger", "--json")
	requireKeys(t, "ledger", parseJSONObject(t, ledgerOut), "runs", "total_tokens", "tracks_used")
}

// ---------------------------------------------------------------------------
// 3. Every subcommand on one shared sandbox.
// ---------------------------------------------------------------------------

func TestDocEngineSubcommands(t *testing.T) {
	sb, _ := docShopSandbox(t, false)
	docWriteConfig(t, sb, -1)
	head := sb.GitCommit("add docs config")
	docRunWrite(t, sb, head)

	t.Run("check", func(t *testing.T) {
		gmbWant(t, sb, []string{"fresh"}, "doc", "check")
	})

	t.Run("status", func(t *testing.T) {
		gmbWant(t, sb, []string{"docs/guide.md"}, "doc", "status")
	})

	t.Run("view-cats-noninteractive", func(t *testing.T) {
		// Under `go test` tui.IsInteractive is always false, so view cats.
		out := gmb(t, sb, "doc", "view", "docs/guide.md")
		mustContain(t, out, "gmb:begin:overview")
	})

	t.Run("release", func(t *testing.T) {
		gmbWant(t, sb, []string{"Migration Guide"}, "doc", "release", "HEAD..HEAD")
	})

	t.Run("export-rag", func(t *testing.T) {
		gmbWant(t, sb, []string{"RAG chunk"}, "doc", "export", "--format", "rag")
		if !sb.Exists(".glassmarble/rag/manifest.json") {
			t.Errorf("rag export did not write manifest.json")
		}
	})

	t.Run("export-state", func(t *testing.T) {
		gmbWant(t, sb, []string{`"schema_version"`, `"documents"`}, "doc", "export", "--format", "state")
	})

	t.Run("eval", func(t *testing.T) {
		gmbWant(t, sb, []string{"faithfulness"}, "doc", "eval")
	})

	t.Run("ledger", func(t *testing.T) {
		gmbWant(t, sb, []string{"run(s)"}, "doc", "ledger")
		gmbWant(t, sb, []string{"run(s)"}, "doc", "ledger", "--last", "1")
	})

	t.Run("review-empty", func(t *testing.T) {
		gmbWant(t, sb, []string{"no pending items"}, "doc", "review")
		gmbWant(t, sb, []string{"0 pending"}, "doc", "review", "--stats")
	})

	t.Run("langmatrix", func(t *testing.T) {
		gmbWant(t, sb, []string{"go"}, "doc", "langmatrix")
	})

	t.Run("record-revert", func(t *testing.T) {
		gmbWant(t, sb, []string{"revert recorded"}, "doc", "review", "record-revert",
			"--doc", "docs/guide.md", "--section", "overview", "--reason", "human rewrote the table")
		gmbWant(t, sb, []string{"1 observed"}, "doc", "review", "--stats")
	})

	t.Run("tuning", func(t *testing.T) {
		gmbWant(t, sb, []string{"tuning suggestion"}, "doc", "review", "--tuning")
	})

	t.Run("tuning-apply", func(t *testing.T) {
		// Observed reverts map to "doc corrections" (no safe auto-fix):
		// the loop reports manual action rather than failing.
		gmb(t, sb, "doc", "review", "record-revert",
			"--doc", "docs/guide.md", "--section", "overview", "--reason", `overuses "synergize" in intro`)
		out := gmb(t, sb, "doc", "review", "--tuning", "--apply")
		mustContain(t, out, "tuning apply:", "manual")
	})

	t.Run("tuning-apply-safe", func(t *testing.T) {
		// A RESOLVED revert whose reason names a quoted jargon term maps
		// to "style/prompts" and is safe-applied to the global
		// style.jargon_blacklist. Seeded as a queue fixture (the same
		// shape `doc review approve` writes); the CLI consumes it.
		sb.WriteFile(".glassmarble/review.json", `[{"id":"rdeadbeef1","kind":"revert","doc_path":"docs/guide.md","section_id":"overview","summary":"Revert of docs/guide.md#overview","detail":"human rewrote the table","status":"approved","created_at":"2026-01-01T00:00:00Z","resolved_at":"2026-01-01T00:00:00Z","reason":"overuses \"synergize\" in intro"}]`)
		out := gmb(t, sb, "doc", "review", "--tuning", "--apply")
		mustContain(t, out, "tuning apply:", "[applied]")
		mustContain(t, sb.ReadFile(".glassmarble/docs.yaml"), "synergize")
	})

	t.Run("help", func(t *testing.T) {
		gmbWant(t, sb, []string{"check", "diff", "status", "release", "export", "eval", "ledger", "review", "langmatrix"},
			"doc", "--help")
		gmbWant(t, sb, []string{"docserve"}, "docserve", "--help")
	})
}

// ---------------------------------------------------------------------------
// 3b. Exit codes via the real binary (separate process: os.Exit is safe).
// ---------------------------------------------------------------------------

func TestDocEngineExitCodesBinary(t *testing.T) {
	bin := harness.BuildBinary(t)

	newGitSandbox := func(t *testing.T) *harness.Sandbox {
		sb := harness.NewSandbox(t)
		sb.RequireGit()
		sb.WriteFile("go.mod", docGoMod)
		sb.WriteFile("pkg/shop/shop.go", docShopGo)
		sb.GitInit()
		return sb
	}

	t.Run("check-0-fresh", func(t *testing.T) {
		sb := newGitSandbox(t)
		docWriteConfig(t, sb, -1)
		head := sb.GitCommit("add docs config")
		gmb(t, sb, "doc", "--commit", head, "--write", "--force", "--no-llm")
		_, _, code := harness.RunBinary(t, bin, sb.Root, nil, "doc", "check")
		if code != 0 {
			t.Errorf("doc check fresh: want exit 0, got %d", code)
		}
	})

	t.Run("check-1-drift", func(t *testing.T) {
		sb := newGitSandbox(t)
		sb.WriteFile(".glassmarble/docs.yaml", "version: 1\ndocs_dir: docs\ndocuments:\n  - id: ghost\n    target: docs/ghost.md\n    title: Ghost\n    scope:\n      paths:\n        - pkg/shop/**\n    sections:\n      - id: main\n        title: Main\n        instruction: Document the state.\n")
		_, _, code := harness.RunBinary(t, bin, sb.Root, nil, "doc", "check")
		if code != 1 {
			t.Errorf("doc check drift: want exit 1, got %d", code)
		}
	})

	t.Run("check-2-hard-error", func(t *testing.T) {
		sb := newGitSandbox(t)
		sb.WriteFile(".glassmarble/docs.yaml", "version: 99\ndocuments: []\n")
		_, _, code := harness.RunBinary(t, bin, sb.Root, nil, "doc", "check")
		if code != 2 {
			t.Errorf("doc check bad config: want exit 2, got %d", code)
		}
	})

	t.Run("diff-0-clean", func(t *testing.T) {
		sb := newGitSandbox(t)
		_, _, code := harness.RunBinary(t, bin, sb.Root, nil, "doc", "diff")
		if code != 0 {
			t.Errorf("doc diff clean: want exit 0, got %d", code)
		}
	})

	t.Run("diff-1-pending", func(t *testing.T) {
		sb := newGitSandbox(t)
		sb.WriteFile(".glassmarble/docs.yaml", "version: 1\ndocs_dir: docs\ndocuments:\n  - id: guide\n    target: docs/guide.md\n    title: Guide\n    scope:\n      paths:\n        - internal/**\n    sections:\n      - id: overview\n        title: Overview\n        instruction: Describe the subsystem.\n")
		sb.WriteFile("docs/guide.md", "# Guide\n")
		_, _, code := harness.RunBinary(t, bin, sb.Root, nil, "doc", "diff")
		if code != 1 {
			t.Errorf("doc diff pending: want exit 1, got %d", code)
		}
	})

	t.Run("docserve-help-0", func(t *testing.T) {
		sb := newGitSandbox(t)
		stdout, _, code := harness.RunBinary(t, bin, sb.Root, nil, "docserve", "--help")
		if code != 0 {
			t.Errorf("docserve --help: want exit 0, got %d", code)
		}
		mustContain(t, stdout, "docserve")
	})
}

// ---------------------------------------------------------------------------
// 4. Branch policy: feature = compute-only, --write overrides,
//    draft/wip always compute-only.
// ---------------------------------------------------------------------------

func TestDocEngineBranchPolicy(t *testing.T) {
	sb, _ := docShopSandbox(t, false)
	docWriteConfig(t, sb, -1)
	head := sb.GitCommit("add docs config")
	docRunWrite(t, sb, head)
	synced := sb.ReadFile("docs/guide.md")

	sb.MustGit("checkout", "-b", "feature/shop-tweak")
	sb.WriteFile("pkg/shop/extra.go", "package shop\n\n// Wave waves hello.\nfunc Wave() string { return \"wave\" }\n")
	featHead := sb.GitCommit("add wave helper")

	// Compute-only on a feature branch: files + state untouched.
	out := gmb(t, sb, "doc", "--commit", featHead, "--force", "--no-llm")
	mustContain(t, out, "compute-only")
	if got := sb.ReadFile("docs/guide.md"); got != synced {
		t.Errorf("feature branch without --write modified docs")
	}
	stateOut := gmb(t, sb, "doc", "export", "--format", "state")
	mustContain(t, stateOut, head)
	mustNotContain(t, stateOut, featHead)

	// --write overrides the branch policy (state advances to the new HEAD).
	out = gmb(t, sb, "doc", "--commit", featHead, "--force", "--no-llm", "--write")
	mustContain(t, out, "ForceWrite override")
	stateOut = gmb(t, sb, "doc", "export", "--format", "state")
	mustContain(t, stateOut, featHead)
	overridden := sb.ReadFile("docs/guide.md")

	// Draft branches are always compute-only, even with --write.
	sb.MustGit("checkout", "-b", "draft/refresh")
	sb.WriteFile("pkg/shop/extra.go", "package shop\n\n// Wave waves hello.\nfunc Wave() string { return \"wave v2\" }\n")
	draftHead := sb.GitCommit("draft wave tweak")
	out = gmb(t, sb, "doc", "--commit", draftHead, "--force", "--no-llm", "--write")
	mustContain(t, out, "compute-only")
	if got := sb.ReadFile("docs/guide.md"); got != overridden {
		t.Errorf("draft branch with --write modified docs")
	}
	stateOut = gmb(t, sb, "doc", "export", "--format", "state")
	mustContain(t, stateOut, featHead)
	mustNotContain(t, stateOut, draftHead)
}

// ---------------------------------------------------------------------------
// 5. Mock-LLM Track A: prose render, gate-3 repair, gate-4 hard fail.
// ---------------------------------------------------------------------------

func TestDocEngineMockLLMProse(t *testing.T) {
	sb, _ := docShopSandbox(t, false)
	docWriteConfig(t, sb, -1)
	head := sb.GitCommit("add docs config")

	mock := harness.NewMockLLM(t)
	defer mock.Close()
	url := mock.Start()
	mock.DefaultText("The shop package provides greeting helpers for the storefront. New contributors should start with the exported helpers.")
	sb.SeedAIConfig(url)

	out := gmb(t, sb, "doc", "--commit", head, "--write", "--force")
	_ = out
	mustContain(t, sb.ReadFile("docs/guide.md"), "storefront")
	if mock.Count() < 1 {
		t.Errorf("Track A did not call the mock LLM (count=%d)", mock.Count())
	}

	ledgerOut := gmb(t, sb, "doc", "ledger", "--json")
	parsed := parseJSONObject(t, ledgerOut)
	tracks, ok := parsed["tracks_used"].(map[string]any)
	if !ok || tracks["llm"] == nil || tracks["llm"].(float64) < 1 {
		t.Errorf("ledger missing llm track usage: %v", parsed["tracks_used"])
	}
}

func TestDocEngineMockLLMGate3Repair(t *testing.T) {
	sb, _ := docShopSandbox(t, false)
	docWriteConfig(t, sb, -1)
	head := sb.GitCommit("add docs config")

	mock := harness.NewMockLLM(t)
	defer mock.Close()
	url := mock.Start()
	mock.Script(
		// gmb doc's mandatory-LLM gate (ensureLLMReady) runs a live
		// connectivity ping through ai_engine.Doctor before any document
		// is touched — it hits the same /chat/completions endpoint as the
		// real render calls, so it consumes the first scripted response.
		harness.MockResponse{Text: "OK"},
		harness.MockResponse{Text: "Use `HallucinatedSymbolZZZ` to greet everyone."},
		harness.MockResponse{Text: "Use the shop helpers to greet visitors in the storefront."},
	)
	sb.SeedAIConfig(url)

	out := gmb(t, sb, "doc", "--commit", head, "--write", "--force", "--verbose")
	_ = out
	body := sb.ReadFile("docs/guide.md")
	mustContain(t, body, "storefront")
	mustNotContain(t, body, "HallucinatedSymbolZZZ")
	if got := mock.Count(); got != 3 {
		t.Errorf("gate 3 repair: want exactly 3 LLM calls (connectivity ping + initial + repair), got %d", got)
	}
	ledgerOut := gmb(t, sb, "doc", "ledger", "--json")
	parsed := parseJSONObject(t, ledgerOut)
	if repairs, _ := parsed["total_repairs"].(float64); repairs < 1 {
		t.Errorf("ledger missing gate-3 repair record: %v", parsed)
	}
}

func TestDocEngineMockLLMSecretHardFail(t *testing.T) {
	sb, _ := docShopSandbox(t, false)
	docWriteConfig(t, sb, -1)
	head := sb.GitCommit("add docs config")

	mock := harness.NewMockLLM(t)
	defer mock.Close()
	url := mock.Start()
	mock.Script(
		// See TestDocEngineMockLLMGate3Repair: the mandatory-LLM
		// connectivity ping consumes the first scripted response.
		harness.MockResponse{Text: "OK"},
		harness.MockResponse{Text: "Set api_key: hunter2supersecretvalue to enable greetings."},
	)
	sb.SeedAIConfig(url)

	// A gate-4 hard failure aborts the section outright (no content, no
	// deterministic fallback — see renderTrackA) and, since cmd/doc.go
	// exits non-zero on any SectionsFailed, this run itself fails.
	out, err := gmbErr(t, sb, "doc", "--commit", head, "--write", "--force", "--verbose")
	if err == nil {
		t.Fatalf("gate 4 hard fail: want non-zero exit for a failed section\n--- output ---\n%s", out)
	}
	mustContain(t, out, "gate 4")
	if sb.Exists("docs/guide.md") {
		mustNotContain(t, sb.ReadFile("docs/guide.md"), "hunter2supersecretvalue")
	}
	if got := mock.Count(); got != 2 {
		t.Errorf("gate 4 hard fail: want exactly 2 LLM calls (connectivity ping + initial, no repair retry), got %d", got)
	}
	ledgerOut := gmb(t, sb, "doc", "ledger", "--json")
	parsed := parseJSONObject(t, ledgerOut)
	if w, _ := parsed["total_docs_updated"].(float64); w != 0 {
		t.Errorf("ledger shows a doc update despite the only section hard-failing gate 4: %v", parsed)
	}
}

// ---------------------------------------------------------------------------
// 6. Pillar spot-checks.
// ---------------------------------------------------------------------------

func TestDocEnginePillarADRAndReviewApprove(t *testing.T) {
	sb, _ := docShopSandbox(t, false)
	docWriteConfig(t, sb, -1)
	head := sb.GitCommit("add docs config")
	docRunWrite(t, sb, head)

	// Arch-event commit triggers autonomous ADR generation on the next run.
	sb.WriteFile("pkg/store/store.go", "package store\n\n// Put persists a value.\nfunc Put(k, v string) {}\n")
	archHead := sb.GitCommit("add new database storage layer with schema migration")
	docRunWrite(t, sb, archHead)

	entries, err := os.ReadDir(sb.Path("docs", "adr"))
	if err != nil || len(entries) == 0 {
		t.Fatalf("ADR auto-gen produced no docs/adr files: %v", err)
	}
	adrBody := sb.ReadFile(filepath.Join("docs", "adr", entries[0].Name()))
	mustContain(t, adrBody, "NEW_DATABASE_LAYER")

	// The new ADR(s) join the D6 review queue as drafts; approve them all.
	// (Message scan + structural dossier events can queue more than one.)
	reviewOut := gmb(t, sb, "doc", "review")
	mustContain(t, reviewOut, "adr-draft")
	idRe := regexp.MustCompile(`(?m)^\s+(\S+)\s+\[adr-draft\]`)
	matches := idRe.FindAllStringSubmatch(reviewOut, -1)
	if len(matches) == 0 {
		t.Fatalf("could not parse review ids from:\n%s", reviewOut)
	}
	for _, m := range matches {
		gmbWant(t, sb, []string{"approved"}, "doc", "review", "approve", m[1], "--reason", "looks accurate")
	}
	statsOut := gmb(t, sb, "doc", "review", "--stats")
	mustContain(t, statsOut, "0 pending")
	mustContain(t, statsOut, "approved")
}

func TestDocEnginePillarErrorCatalog(t *testing.T) {
	// Single-commit repo: code + docs config land in the analyzed HEAD so
	// FastBail sees in-scope files and the run proceeds to P33.
	sb := harness.NewSandbox(t)
	sb.RequireGit()
	sb.WriteFile("go.mod", docGoMod)
	sb.WriteFile("pkg/shop/shop.go", docShopGo)
	sb.WriteFile("pkg/shop/fetch.go", docShopCallerGo)
	sb.WriteFile("pkg/shop/errors.go", docShopErrorsGo)
	gmb(t, sb, "init")
	docWriteConfig(t, sb, -1)
	sb.GitInit()

	// The living error catalog regenerates during analyze --docs
	// (CPG-aware P33 path: graph edges resolve callers).
	out := gmb(t, sb, "analyze", "--docs")
	_ = out
	if !sb.Exists("docs/errors.md") {
		t.Fatalf("analyze did not regenerate docs/errors.md")
	}
	catalog := sb.ReadFile("docs/errors.md")
	// The sentinel row is extracted from code with triage playbook, origin
	// file#line link, and the Callers column. (Graph-authoritative caller
	// lists stay empty for plain value references — only call edges link —
	// per the specified "unknown sentinels keep empty caller lists" rule.)
	mustContain(t, catalog, "ErrItemNotFound", "Callers", "pkg/shop/errors.go#L")
}

func TestDocEnginePillarMigration(t *testing.T) {
	sb, _ := docShopSandbox(t, false)
	sb.WriteFile("pkg/shop/farewell.go", "package shop\n\n// Farewell says goodbye.\nfunc Farewell(name string) string { return \"bye \" + name }\n")
	sb.GitCommit("add farewell helper")
	sb.MustGit("tag", "v0.1.0")
	sb.MustGit("rm", "-q", "pkg/shop/farewell.go")
	sb.GitCommit("remove farewell helper")
	sb.MustGit("tag", "v0.2.0")

	out := gmb(t, sb, "doc", "release", "v0.1.0..v0.2.0")
	mustContain(t, out, "Migration Guide", "Farewell")

	out = gmb(t, sb, "doc", "release", "v0.1.0..v0.2.0", "--out", "docs/mig.md")
	mustContain(t, out, "migration guide written")
	mustContain(t, sb.ReadFile("docs/mig.md"), "Farewell")
}

func TestDocEnginePillarRAGExport(t *testing.T) {
	sb, _ := docShopSandbox(t, false)
	docWriteConfig(t, sb, -1)
	head := sb.GitCommit("add docs config")
	docRunWrite(t, sb, head)

	out := gmb(t, sb, "doc", "export", "--format", "rag", "--out", ".glassmarble/rag")
	mustContain(t, out, "RAG chunk")
	manifestRaw := sb.ReadFile(".glassmarble/rag/manifest.json")
	var manifest struct {
		ChunksCount int `json:"chunks_count"`
	}
	if err := json.Unmarshal([]byte(manifestRaw), &manifest); err != nil {
		t.Fatalf("rag manifest.json does not parse: %v", err)
	}
	if manifest.ChunksCount < 1 {
		t.Errorf("rag manifest reports no chunks: %s", manifestRaw)
	}
	chunks, err := filepath.Glob(sb.Path(".glassmarble", "rag", "guide_*.json"))
	if err != nil || len(chunks) == 0 {
		t.Errorf("rag export wrote no section chunk files")
	} else {
		chunkBody, _ := os.ReadFile(chunks[0])
		mustContain(t, string(chunkBody), `"doc_id"`, `"chunk_id"`)
	}
}

func TestDocEnginePillarSnippetFix(t *testing.T) {
	sb, _ := docShopSandbox(t, false)
	docWriteConfig(t, sb, -1)
	sb.GitCommit("add docs config")
	sb.WriteFile("docs/guide.md", "# Shop Guide\n\n<!-- gmb:snippet:example -->\n```go\nGreet(\"a\", \"b\")\n```\n")

	// Detection: arity mismatch against the real declaration.
	detectOut, err := gmbErr(t, sb, "doc", "check", "--verify-snippets")
	if err == nil {
		t.Fatalf("expected snippet verification failure, got success:\n%s", detectOut)
	}
	mustContain(t, detectOut, "SNIPPET ERROR")

	// Explicit --fix rewrites the call line deterministically.
	before := sb.ReadFile("docs/guide.md")
	fixOut, _ := gmbErr(t, sb, "doc", "check", "--verify-snippets", "--fix")
	mustContain(t, fixOut, "SNIPPET FIXED")
	fixed := sb.ReadFile("docs/guide.md")
	if fixed == before {
		t.Errorf("--fix did not modify the doc")
	}
	mustContain(t, fixed, "Greet(name string)")
}

// ---------------------------------------------------------------------------
// 7. JSON output schema assertions.
// ---------------------------------------------------------------------------

func TestDocEngineJSONSchemas(t *testing.T) {
	sb, _ := docShopSandbox(t, false)
	docWriteConfig(t, sb, -1)
	head := sb.GitCommit("add docs config")

	runOut := gmb(t, sb, "doc", "--commit", head, "--write", "--force", "--no-llm", "--json")
	requireKeys(t, "doc run", parseJSONObject(t, runOut),
		"commit", "docs_updated", "sections_processed", "sections_updated", "tokens_used", "duration_ms")

	checkOut := gmb(t, sb, "doc", "check", "--json")
	requireKeys(t, "doc check", parseJSONObject(t, checkOut),
		"AllFresh", "Documents", "GlobalFreshness")

	statusOut := gmb(t, sb, "doc", "status", "--json")
	requireKeys(t, "doc status", parseJSONObject(t, statusOut),
		"AllFresh", "Documents", "GlobalFreshness")

	evalOut := gmb(t, sb, "doc", "eval", "--json")
	evalParsed := parseJSONObject(t, evalOut)
	requireKeys(t, "doc eval", evalParsed, "global_score", "samples", "docs")
	if samples, _ := evalParsed["samples"].(float64); samples < 1 {
		t.Errorf("doc eval scored no sections: %v", evalParsed)
	}

	ledgerOut := gmb(t, sb, "doc", "ledger", "--json")
	ledgerParsed := parseJSONObject(t, ledgerOut)
	requireKeys(t, "doc ledger", ledgerParsed, "runs", "total_tokens", "tracks_used")
	if runs, _ := ledgerParsed["runs"].(float64); runs < 1 {
		t.Errorf("ledger reports no runs after a write: %v", ledgerParsed)
	}

	// Diff with zero documents configured is the only in-process-safe diff
	// JSON path (a pending diff calls os.Exit in-process; exit codes for
	// pending diffs are pinned via the real binary instead).
	clean := harness.NewSandbox(t)
	diffOut := gmb(t, clean, "doc", "diff", "--json")
	diffParsed := parseJSONObject(t, diffOut)
	requireKeys(t, "doc diff", diffParsed, "has_changes", "sections")
	if has, _ := diffParsed["has_changes"].(bool); has {
		t.Errorf("empty-config diff reports pending changes: %v", diffParsed)
	}
}

// ---------------------------------------------------------------------------
// Live-provider smoke (gated; never runs without explicit opt-in).
// ---------------------------------------------------------------------------

func TestDocEngineLiveLLMSmoke(t *testing.T) {
	if os.Getenv("GLASSMARBLE_TEST_LIVE_LLM") != "1" {
		t.Skip("live LLM smoke test (set GLASSMARBLE_TEST_LIVE_LLM=1 to enable)")
	}
	baseURL := os.Getenv("GLASSMARBLE_TEST_LIVE_BASE_URL")
	if baseURL == "" {
		t.Skip("live LLM smoke test needs GLASSMARBLE_TEST_LIVE_BASE_URL")
	}
	sb, _ := docShopSandbox(t, false)
	docWriteConfig(t, sb, -1)
	head := sb.GitCommit("add docs config")
	sb.SeedAIConfig(baseURL)

	out := gmb(t, sb, "doc", "--commit", head, "--write", "--force")
	mustContain(t, sb.ReadFile("docs/guide.md"), "gmb:begin:overview")
	t.Logf("live smoke output: %s", out)
}
