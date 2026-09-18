package e2e_test

// SYSTEM BEHAVIORS for the docs-engine (multi-doc coordination, cascade
// thresholds, permalink healing, ADR lifecycle, review queue, RAG export,
// daemon lifecycle, multiprocess convergence).
//
// Rules for this file:
//   - Blackbox only: drive the CLI in-process via the harness (gmb/gmbErr)
//     or, where real processes/locks are required (daemon, multiprocess),
//     via ONE shared built binary (harness.BuildBinary caches it).
//     No production code is imported or modified.
//   - Deterministic, no network: every engine run uses --no-llm and the
//     known live-provider env keys are scrubbed (coordScrubDocEnv).
//   - No t.Parallel anywhere (the harness runner mutates os.Stdout + CWD).
//   - No sleeps except deadline-based daemon polling (200ms interval with
//     an absolute deadline — never a fixed sleep).
//
// Two engine paths are exercised, and the tests document which is which:
//   - `gmb doc` (graph-less): deterministic output is code-independent, so
//     re-runs are byte-identical and engine STATE (docs_state.json) is the
//     change signal. Cascade gates are observable here as creation
//     suppression: aggregate/root docs are not even scaffolded without an
//     architectural signal in the target commit.
//   - `gmb analyze --docs` (graph-backed): real dirty discovery — content
//     tracks code symbols, so per-file byte outcomes differentiate leaf,
//     aggregate, and root tiers.

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Syamchand123/GlassMarble/tests/harness"
)

const (
	coordSharedGo = `package shared

// Format renders a value for display.
func Format(v string) string {
	return "[" + v + "]"
}
`

	coordAuthGo = `package auth

import "example.com/shop/pkg/shared"

// Greet returns a friendly greeting for name.
func Greet(name string) string {
	return shared.Format("hello " + name)
}

// helper is an unexported internal helper.
func helper(x string) string {
	return "h:" + x
}
`

	coordBillingGo = `package billing

import "example.com/shop/pkg/shared"

// Charge bills an account.
func Charge(account string) string {
	return shared.Format("charge:" + account)
}
`

	coordAPIGo = `package api

import "example.com/shop/pkg/shared"

// Serve handles one request.
func Serve(path string) string {
	return shared.Format("serve:" + path)
}
`

	coordGoMod = `module example.com/shop

go 1.21
`
)

// coordScrubDocEnv blanks every known live-provider key for the duration of
// the test so offline (--no-llm) runs can never leak to a real provider.
func coordScrubDocEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"OPENAI_API_KEY", "OPENAI_BASE_URL", "OPENAI_ORG_ID",
		"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN",
		"GOOGLE_API_KEY", "GEMINI_API_KEY",
		"AZURE_OPENAI_API_KEY", "AZURE_OPENAI_ENDPOINT",
		"GROQ_API_KEY", "TOGETHER_API_KEY", "COHERE_API_KEY",
		"MISTRAL_API_KEY", "GLASSMARBLE_API_KEY", "LLM_API_KEY",
		"GLASSMARBLE_TEST_LIVE_LLM", "GLASSMARBLE_TEST_LIVE_BASE_URL",
	} {
		t.Setenv(k, "")
	}
}

// coordMultiSandbox builds the shared multi-doc fixture: three leaf scopes
// (auth/billing/api) plus a shared package used by all three, with leaf
// docs per scope, one aggregate (architecture archetype) doc, and one root
// (README) doc. Code AND docs.yaml land in the INITIAL commit (mirroring
// the `analyze --docs` pillar fixture) so the first analyze sees in-scope
// files at HEAD and FastBail cannot skip the initial sync.
func coordMultiSandbox(t *testing.T) *harness.Sandbox {
	t.Helper()
	sb := harness.NewSandbox(t)
	sb.RequireGit()
	sb.WriteFile("go.mod", coordGoMod)
	sb.WriteFile("pkg/auth/auth.go", coordAuthGo)
	sb.WriteFile("pkg/billing/billing.go", coordBillingGo)
	sb.WriteFile("pkg/api/api.go", coordAPIGo)
	sb.WriteFile("pkg/shared/shared.go", coordSharedGo)
	gmb(t, sb, "init")
	sb.WriteFile(".glassmarble/docs.yaml", `version: 1
docs_dir: docs
target_platform: github_flat
documents:
  - id: auth
    target: docs/auth.md
    title: Auth Reference
    purpose: Reference for the auth package.
    audience: Developers
    scope:
      paths:
        - pkg/auth/**
        - pkg/shared/**
    sections:
      - id: overview
        title: Overview
        instruction: Describe the exported helpers in the auth package.
        managed: true
  - id: billing
    target: docs/billing.md
    title: Billing Reference
    purpose: Reference for the billing package.
    audience: Developers
    scope:
      paths:
        - pkg/billing/**
        - pkg/shared/**
    sections:
      - id: overview
        title: Overview
        instruction: Describe the exported helpers in the billing package.
        managed: true
  - id: api
    target: docs/api.md
    title: API Reference
    purpose: Reference for the api package.
    audience: Developers
    scope:
      paths:
        - pkg/api/**
        - pkg/shared/**
    sections:
      - id: overview
        title: Overview
        instruction: Describe the exported helpers in the api package.
        managed: true
  - id: sysarch
    target: docs/architecture.md
    title: System Architecture
    purpose: Cross-cutting system design.
    audience: Developers
    archetype: architecture
    scope:
      paths:
        - pkg/**
    sections:
      - id: overview
        title: Overview
        instruction: Describe the system architecture.
        managed: true
  - id: rootdoc
    target: README.md
    title: Readme
    purpose: Root orientation.
    audience: Developers
    scope:
      paths:
        - pkg/auth/**
        - cmd/shop/**
    sections:
      - id: overview
        title: Overview
        instruction: Orient new contributors.
        managed: true
`)
	sb.GitInit()
	return sb
}

// coordAnalyze runs the graph-backed docs-engine pass used for content
// assertions (dirty discovery needs the AKG head graph).
func coordAnalyze(t *testing.T, sb *harness.Sandbox) string {
	t.Helper()
	return gmb(t, sb, "analyze", "--docs", "--no-llm")
}

// coordSharedLinks extracts every pkg/shared/shared.go#L<n> permalink line
// number from a rendered document.
func coordSharedLinks(t *testing.T, body string) []string {
	t.Helper()
	re := regexp.MustCompile(`pkg/shared/shared\.go#L(\d+(?:-L\d+)?)`)
	var out []string
	for _, m := range re.FindAllStringSubmatch(body, -1) {
		out = append(out, m[1])
	}
	return out
}

// coordFuncLine returns the 1-based line number of the first line containing
// frag in the sandbox-relative file (0 when absent).
func coordFuncLine(t *testing.T, sb *harness.Sandbox, rel, frag string) int {
	t.Helper()
	for i, ln := range strings.Split(sb.ReadFile(rel), "\n") {
		if strings.Contains(ln, frag) {
			return i + 1
		}
	}
	return 0
}

// coordApproveAllReviews approves every pending review item and returns the
// number of items approved.
func coordApproveAllReviews(t *testing.T, sb *harness.Sandbox) int {
	t.Helper()
	reviewOut := gmb(t, sb, "doc", "review")
	idRe := regexp.MustCompile(`(?m)^\s+(\S+)\s+\[([\w-]+)\]`)
	matches := idRe.FindAllStringSubmatch(reviewOut, -1)
	for _, m := range matches {
		gmbWant(t, sb, []string{"approved"}, "doc", "review", "approve", m[1], "--reason", "looks accurate")
	}
	return len(matches)
}

// coordPollUntil polls cond every 200ms until it returns true or the
// deadline elapses (deadline-based waiting for the daemon test only).
func coordPollUntil(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	if !cond() {
		t.Fatalf("timed out after %s waiting for: %s", timeout, what)
	}
}

// syncBuffer is a mutex-guarded bytes.Buffer: the daemon child's stdout and
// stderr pipes are drained on separate goroutines while the test polls the
// accumulated output, so unsynchronized access would race under -race.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func TestDocEngineCoordination(t *testing.T) {
	coordScrubDocEnv(t)

	t.Run("multidoc_single_run_and_cascade", func(t *testing.T) {
		sb := coordMultiSandbox(t)
		coordAnalyze(t, sb)

		// A1: ONE initial commit touches all three scopes; a single analyze
		// run renders all three leaf docs consistently.
		for _, rel := range []string{"docs/auth.md", "docs/billing.md", "docs/api.md"} {
			if !sb.Exists(rel) {
				t.Fatalf("single-run multi-doc sync missing %s", rel)
			}
			mustContain(t, sb.ReadFile(rel), "gmb:begin:overview")
		}
		// Shared grounding does not contradict: the shared symbol resolves
		// to the identical permalink in all three documents.
		authLinks := coordSharedLinks(t, sb.ReadFile("docs/auth.md"))
		billingLinks := coordSharedLinks(t, sb.ReadFile("docs/billing.md"))
		apiLinks := coordSharedLinks(t, sb.ReadFile("docs/api.md"))
		if len(authLinks) == 0 || len(billingLinks) == 0 || len(apiLinks) == 0 {
			t.Fatalf("shared grounding missing (auth=%v billing=%v api=%v)", authLinks, billingLinks, apiLinks)
		}
		for _, links := range [][]string{billingLinks, apiLinks} {
			if strings.Join(links, ",") != strings.Join(authLinks, ",") {
				t.Errorf("shared grounding contradicts: auth=%v other=%v", authLinks, links)
			}
		}
		if want := coordFuncLine(t, sb, "pkg/shared/shared.go", "func Format("); want == 0 {
			t.Fatalf("shared.Format definition not found")
		} else {
			for _, rel := range []string{"docs/auth.md", "docs/billing.md", "docs/api.md"} {
				if !strings.Contains(sb.ReadFile(rel), "shared.go#L"+strconv.Itoa(want)) {
					t.Errorf("%s permalink does not point at shared.go line %d", rel, want)
				}
			}
		}
		// Cascade at initial sync: the aggregate doc is NOT written without
		// a critical architectural event (0 writes for it), root is.
		if sb.Exists("docs/architecture.md") {
			t.Errorf("aggregate doc written on a non-architectural commit (want 0 writes)")
		}
		if !sb.Exists("README.md") {
			t.Errorf("root doc missing after initial sync")
		}

		// A2: ONE commit touching all three scopes updates all three in a
		// single run; the aggregate still stays out.
		sb.WriteFile("pkg/auth/auth.go", coordAuthGo+"\n// Login authenticates a user.\nfunc Login(user string) string { return shared.Format(user) }\n")
		sb.WriteFile("pkg/billing/billing.go", coordBillingGo+"\n// Refund credits an account.\nfunc Refund(account string) string { return shared.Format(account) }\n")
		sb.WriteFile("pkg/api/api.go", coordAPIGo+"\n// Health reports status.\nfunc Health() string { return shared.Format(\"ok\") }\n")
		sb.GitCommit("add batch helpers across services")
		coordAnalyze(t, sb)
		for _, tc := range []struct{ rel, want string }{
			{"docs/auth.md", "Login"}, {"docs/billing.md", "Refund"}, {"docs/api.md", "Health"},
		} {
			mustContain(t, sb.ReadFile(tc.rel), tc.want)
		}
		authBase := sb.ReadFile("docs/auth.md")
		if sb.Exists("docs/architecture.md") {
			t.Errorf("aggregate doc written on a non-critical commit (want 0 writes)")
		}

		// A3: private-helper rename — the leaf doc tracks it, sibling
		// scopes stay byte-identical (isolation), aggregate stays out.
		billingA2 := sb.ReadFile("docs/billing.md")
		apiA2 := sb.ReadFile("docs/api.md")
		sb.WriteFile("pkg/auth/auth.go", strings.Replace(sb.ReadFile("pkg/auth/auth.go"), "func helper(", "func helperV2(", 1))
		sb.GitCommit("fix helper naming")
		coordAnalyze(t, sb)
		authAfter := sb.ReadFile("docs/auth.md")
		if authAfter == authBase {
			t.Errorf("leaf auth doc did not update on private-helper rename")
		}
		mustContain(t, authAfter, "helperV2")
		if got := sb.ReadFile("docs/billing.md"); got != billingA2 {
			t.Errorf("billing doc changed on an out-of-scope auth-only commit")
		}
		if got := sb.ReadFile("docs/api.md"); got != apiA2 {
			t.Errorf("api doc changed on an out-of-scope auth-only commit")
		}
		if sb.Exists("docs/architecture.md") {
			t.Errorf("aggregate doc written on private-helper rename (want 0 writes)")
		}

		// A4: public-surface change (exported cmd/ entry) — root picks it
		// up while scope-untouched leaves stay byte-identical.
		billingA3 := sb.ReadFile("docs/billing.md")
		apiA3 := sb.ReadFile("docs/api.md")
		sb.WriteFile("cmd/shop/main.go", "package main\n\n// Run is the CLI entry point.\nfunc Run() {}\n\nfunc main() { Run() }\n")
		sb.GitCommit("add command entry point")
		coordAnalyze(t, sb)
		mustContain(t, sb.ReadFile("README.md"), "cmd/shop/main.go")
		if got := sb.ReadFile("docs/billing.md"); got != billingA3 {
			t.Errorf("billing doc changed on an out-of-scope cmd/ commit")
		}
		if got := sb.ReadFile("docs/api.md"); got != apiA3 {
			t.Errorf("api doc changed on an out-of-scope cmd/ commit")
		}
		if sb.Exists("docs/architecture.md") {
			t.Errorf("aggregate doc written on cmd/ entry commit (want 0 writes)")
		}
	})

	t.Run("cascade_thresholds_doc_path", func(t *testing.T) {
		// Graph-less `doc` runs: gates are observable as creation
		// suppression (no --force anywhere in this subtest, so the dirty
		// list — not the force reprocess — decides what is written).
		// Code AND docs.yaml land in the INITIAL commit so the first run
		// cannot fast-bail (a config-only HEAD would bail for touching no
		// scope: FastBail rule 4).
		sb := harness.NewSandbox(t)
		sb.RequireGit()
		sb.WriteFile("go.mod", docGoMod)
		sb.WriteFile("pkg/shop/shop.go", docShopGo)
		sb.WriteFile(".glassmarble/docs.yaml", `version: 1
docs_dir: docs
target_platform: github_flat
documents:
  - id: leaf
    target: docs/leaf.md
    title: Leaf
    purpose: Leaf doc.
    audience: Developers
    scope:
      paths:
        - pkg/shop/**
    sections:
      - id: overview
        title: Overview
        instruction: Describe the exported helpers in this package.
        managed: true
  - id: sysarch
    target: docs/architecture.md
    title: System Architecture
    purpose: Cross-cutting system design.
    audience: Developers
    archetype: architecture
    scope:
      paths:
        - pkg/shop/**
    sections:
      - id: overview
        title: Overview
        instruction: Describe the system architecture.
        managed: true
  - id: rootdoc
    target: README.md
    title: Readme
    purpose: Root orientation.
    audience: Developers
    scope:
      paths:
        - pkg/shop/**
    sections:
      - id: overview
        title: Overview
        instruction: Orient new contributors.
        managed: true
`)
		// FirstSIGNAL commit: a scope touch whose message carries only the
		// non-critical "auth" keyword (SECURITY_BOUNDARY_CHANGED — not in
		// the aggregate critical set, and "auth" alone is not a SECURITY
		// intent). The event both defeats the comment-only skip and opens
		// the root gate, while the aggregate gate stays shut: leaf+root
		// render, aggregate is suppressed (0 writes for it).
		sb.GitInit()
		sb.WriteFile("pkg/shop/shop.go", docShopGo+"// Package shop provides helpers.\n")
		signalHead := sb.GitCommit("add auth helper")
		gmb(t, sb, "doc", "--commit", signalHead, "--write", "--no-llm")
		if !sb.Exists("docs/leaf.md") {
			t.Fatalf("leaf doc not written on signal commit")
		}
		if !sb.Exists("README.md") {
			t.Fatalf("root doc not written on arch-signal commit")
		}
		if sb.Exists("docs/architecture.md") {
			t.Fatalf("aggregate doc written without a critical arch event")
		}
		leafBase := sb.ReadFile("docs/leaf.md")
		rootBase := sb.ReadFile("README.md")

		// Private change, plain message: nothing moves, state advances.
		sb.WriteFile("pkg/shop/shop.go", strings.Replace(docShopGo, "func Greet(", "func greet(", 1))
		privHead := sb.GitCommit("tweak greeting helper")
		privOut := gmb(t, sb, "doc", "--commit", privHead, "--write", "--no-llm", "--json")
		if got := sb.ReadFile("docs/leaf.md"); got != leafBase {
			t.Errorf("leaf bytes changed on graph-less private rename (want state-only signal)")
		}
		if got := sb.ReadFile("README.md"); got != rootBase {
			t.Errorf("root bytes changed on a non-architectural commit")
		}
		if sb.Exists("docs/architecture.md") {
			t.Errorf("aggregate doc appeared on a non-architectural commit")
		}
		privParsed := parseJSONObject(t, privOut)
		if tokens, _ := privParsed["tokens_used"].(float64); tokens != 0 {
			t.Errorf("tokens_used = %v on a --no-llm run, want 0", tokens)
		}

		// Architectural commit: the critical event cascades to the
		// aggregate doc; leaf and root bytes hold (root was already
		// created by the earlier arch signal and stays byte-stable).
		// NOTE: the new file lives under pkg/shop/ (in-scope): an
		// out-of-scope-only commit would fast-bail before gates run.
		sb.WriteFile("pkg/shop/store.go", "package shop\n\n// Put persists a value.\nfunc Put(k, v string) {}\n")
		archHead := sb.GitCommit("add new database storage layer")
		gmb(t, sb, "doc", "--commit", archHead, "--write", "--no-llm")
		if !sb.Exists("docs/architecture.md") {
			t.Fatalf("aggregate doc not created on critical arch event")
		}
		if got := sb.ReadFile("README.md"); got != rootBase {
			t.Errorf("root bytes changed on an out-of-scope arch commit")
		}
		if got := sb.ReadFile("docs/leaf.md"); got != leafBase {
			t.Errorf("leaf bytes changed on an out-of-scope arch commit")
		}
	})

	t.Run("permalink_healing", func(t *testing.T) {
		// Permalink healing is the zero-LLM P7 sweep (HealAllManagedDocs):
		// it rewrites file#L links from worktree AST with no tokens. It
		// resolves links by SYMBOL name, so the probe link below uses
		// symbol text ([Greet], not the filename-style links the
		// deterministic tables emit). Re-renders cannot heal (section
		// hashes exclude line numbers, so shifted lines are "clean").
		sb, _ := docShopSandbox(t, false)
		docWriteConfig(t, sb, -1)
		cfgHead := sb.GitCommit("add docs config")
		docRunWrite(t, sb, cfgHead)

		greetLine := coordFuncLine(t, sb, "pkg/shop/shop.go", "func Greet(")
		if greetLine <= 0 {
			t.Fatalf("func Greet not found in fixture")
		}
		// Plant a symbol-text permalink in the managed zone. The zone is
		// otherwise untouched, so the next render merges clean (P2) and
		// the link survives for the heal sweep to maintain.
		body := sb.ReadFile("docs/guide.md")
		const endMarker = "<!-- gmb:end:overview -->"
		if !strings.Contains(body, endMarker) {
			t.Fatalf("managed zone end marker missing:\n%s", body)
		}
		probeLink := "[Greet](pkg/shop/shop.go#L" + strconv.Itoa(greetLine) + ")"
		sb.WriteFile("docs/guide.md", strings.Replace(body, endMarker,
			probeLink+"\n"+endMarker, 1))

		// Shift every symbol down by 10 lines (comments after the package
		// clause, so parsing is unaffected) and re-run with --no-llm.
		lines := strings.Split(sb.ReadFile("pkg/shop/shop.go"), "\n")
		var pad []string
		for i := 1; i <= 10; i++ {
			pad = append(pad, "// pad line")
		}
		shifted := append([]string{lines[0]}, append(pad, lines[1:]...)...)
		sb.WriteFile("pkg/shop/shop.go", strings.Join(shifted, "\n"))
		shiftHead := sb.GitCommit("chore: pad file header")
		healOut := gmb(t, sb, "doc", "--commit", shiftHead, "--write", "--no-llm", "--json")
		healParsed := parseJSONObject(t, healOut)
		if tokens, _ := healParsed["tokens_used"].(float64); tokens != 0 {
			t.Errorf("tokens_used = %v on the zero-LLM heal path, want 0", tokens)
		}

		newLine := coordFuncLine(t, sb, "pkg/shop/shop.go", "func Greet(")
		if newLine != greetLine+10 {
			t.Fatalf("line shift landed at %d, want %d", newLine, greetLine+10)
		}
		healed := sb.ReadFile("docs/guide.md")
		if !strings.Contains(healed, "[Greet](pkg/shop/shop.go#L"+strconv.Itoa(newLine)+")") {
			t.Errorf("permalink not healed to line %d\n%s", newLine, healed)
		}
		if strings.Contains(healed, "[Greet](pkg/shop/shop.go#L"+strconv.Itoa(greetLine)+")") {
			t.Errorf("stale permalink shop.go#L%d survives\n%s", greetLine, healed)
		}
		// The whole sandbox history used --no-llm only: ledger stays at 0.
		ledgerParsed := parseJSONObject(t, gmb(t, sb, "doc", "ledger", "--json"))
		if tokens, _ := ledgerParsed["total_tokens"].(float64); tokens != 0 {
			t.Errorf("total_tokens = %v across --no-llm runs, want 0", tokens)
		}
	})

	t.Run("adr_lifecycle", func(t *testing.T) {
		sb, _ := docShopSandbox(t, false)
		docWriteConfig(t, sb, -1)
		head := sb.GitCommit("add docs config")
		docRunWrite(t, sb, head)

		// Arch-event commit → ADR file + index + timeline + review draft.
		// The message yields exactly one event type (NEW_DATABASE_LAYER):
		// "layer"/"service" would add LAYER_VIOLATION/SERVICE_ADDED and a
		// second ADR, breaking the 1:1 lifecycle assertions below.
		sb.WriteFile("pkg/store/store.go", "package store\n\n// Put persists a value.\nfunc Put(k, v string) {}\n")
		archHead := sb.GitCommit("add new database persistence")
		docRunWrite(t, sb, archHead)

		entries, err := os.ReadDir(sb.Path("docs", "adr"))
		if err != nil {
			t.Fatalf("docs/adr missing after arch-event commit: %v", err)
		}
		var adrFiles []string
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") &&
				e.Name() != "index.md" && e.Name() != "template.md" {
				adrFiles = append(adrFiles, e.Name())
			}
		}
		if len(adrFiles) == 0 {
			t.Fatalf("arch-event commit produced no ADR file (entries: %v)", entries)
		}
		firstADR := filepath.Join("docs", "adr", adrFiles[0])
		mustContain(t, sb.ReadFile(firstADR), "NEW_DATABASE_LAYER")
		if !sb.Exists("docs/adr/index.md") {
			t.Fatalf("ADR index.md not regenerated")
		}
		mustContain(t, sb.ReadFile("docs/adr/index.md"), adrFiles[0])
		if !sb.Exists("docs/adr/timeline.json") {
			t.Fatalf("ADR timeline.json not emitted")
		}
		var timeline []map[string]any
		if err := json.Unmarshal([]byte(sb.ReadFile("docs/adr/timeline.json")), &timeline); err != nil {
			t.Fatalf("timeline.json does not parse: %v", err)
		}
		if len(timeline) == 0 {
			t.Fatalf("timeline.json is empty")
		}

		// Review the draft → approve → queue drains; `doc check` is clean.
		mustContain(t, gmb(t, sb, "doc", "review"), "adr-draft")
		if n := coordApproveAllReviews(t, sb); n == 0 {
			t.Fatalf("no review items approved (expected adr-draft)")
		}
		mustContain(t, gmb(t, sb, "doc", "review", "--stats"), "0 pending")
		if out, err := gmbErr(t, sb, "doc", "check"); err != nil {
			t.Errorf("doc check not clean after ADR approval:\n%s", out)
		}

		// A second related ADR supersedes the first (same event family →
		// overlapping title tokens): status flip + backlink.
		sb.WriteFile("pkg/store/cache.go", "package store\n\n// Fetch loads a value.\nfunc Fetch(k string) string { return k }\n")
		sb.GitCommit("extend database persistence follow-up")
		docRunWrite(t, sb, sb.GitHead())
		after := sb.ReadFile(firstADR)
		mustContain(t, after, "status: superseded", "Superseded by")
		mustContain(t, sb.ReadFile("docs/adr/index.md"), "Superseded by")
		var timeline2 []map[string]any
		if err := json.Unmarshal([]byte(sb.ReadFile("docs/adr/timeline.json")), &timeline2); err != nil {
			t.Fatalf("timeline.json does not parse after supersession: %v", err)
		}
		if len(timeline2) != 2 {
			t.Fatalf("timeline wants 2 entries after supersession, got %d", len(timeline2))
		}
		statuses := map[string]bool{}
		for _, e := range timeline2 {
			if s, _ := e["status"].(string); s != "" {
				statuses[s] = true
			}
		}
		if !statuses["superseded"] || !statuses["accepted"] {
			t.Errorf("timeline statuses want superseded+accepted, got %v", statuses)
		}
	})

	t.Run("review_queue_lifecycle", func(t *testing.T) {
		sb, _ := docShopSandbox(t, false)
		docWriteConfig(t, sb, -1)
		head := sb.GitCommit("add docs config")
		docRunWrite(t, sb, head)
		gmbWant(t, sb, []string{"no pending items"}, "doc", "review")

		// Conflicting human edit inside the managed zone (same line the
		// machine regenerates) + an instruction change on the next commit.
		body := sb.ReadFile("docs/guide.md")
		const echoLine = "> Describe the exported helpers in this package."
		if !strings.Contains(body, echoLine) {
			t.Fatalf("deterministic echo line not found for human edit:\n%s", body)
		}
		sb.WriteFile("docs/guide.md", strings.Replace(body, echoLine,
			echoLine+" Human note: greetings are core.", 1))
		cfg := sb.ReadFile(".glassmarble/docs.yaml")
		sb.WriteFile(".glassmarble/docs.yaml", strings.Replace(cfg,
			"instruction: Describe the exported helpers in this package.",
			"instruction: Describe the exported helpers and their error cases.", 1))
		confHead := sb.GitCommit("tweak guide wording")

		// The run reports the conflict, preserves the human text, appends
		// the machine note, and queues a conflict item. --force is
		// required: the commit touches only docs.yaml + the doc itself,
		// which FastBail would skip as out-of-scope (proven incidentally
		// by the pre-force debugging of this very test).
		confOut := gmb(t, sb, "doc", "--commit", confHead, "--write", "--force", "--no-llm")
		mustContain(t, confOut, "merge conflict")
		merged := sb.ReadFile("docs/guide.md")
		mustContain(t, merged, "Human note", "Doc Update Note", "error cases")
		reviewOut := gmb(t, sb, "doc", "review")
		mustContain(t, reviewOut, "conflict", "docs/guide.md")

		// Approve → resolved and drained.
		idRe := regexp.MustCompile(`(?m)^\s+(\S+)\s+\[conflict\]`)
		m := idRe.FindStringSubmatch(reviewOut)
		if m == nil {
			t.Fatalf("could not parse conflict id from:\n%s", reviewOut)
		}
		gmbWant(t, sb, []string{"approved"}, "doc", "review", "approve", m[1], "--reason", "human wording kept")
		statsOut := gmb(t, sb, "doc", "review", "--stats")
		mustContain(t, statsOut, "0 pending", "1 approved")
	})

	t.Run("rag_export_content", func(t *testing.T) {
		sb, _ := docShopSandbox(t, false)
		docWriteConfig(t, sb, -1)
		head := sb.GitCommit("add docs config")
		docRunWrite(t, sb, head)

		// Default output dir (no --out flag): in-process --out would
		// poison the shared command tree for later bare-state exports.
		gmbWant(t, sb, []string{"RAG chunk"}, "doc", "export", "--format", "rag")
		// NOTE: the on-disk manifest carries version/chunks_count/
		// exported_at only (no per-file list) — counts are asserted
		// against the chunk files on disk instead.
		var manifest struct {
			ChunksCount int `json:"chunks_count"`
		}
		if err := json.Unmarshal([]byte(sb.ReadFile(".glassmarble/rag/manifest.json")), &manifest); err != nil {
			t.Fatalf("rag manifest.json does not parse: %v", err)
		}
		chunkPaths, err := filepath.Glob(sb.Path(".glassmarble", "rag", "*.json"))
		var chunks []string
		for _, p := range chunkPaths {
			if filepath.Base(p) != "manifest.json" {
				chunks = append(chunks, p)
			}
		}
		if err != nil || len(chunks) == 0 {
			t.Fatalf("rag export wrote no chunk files")
		}
		if manifest.ChunksCount != len(chunks) {
			t.Errorf("manifest chunks_count=%d but %d chunk files on disk", manifest.ChunksCount, len(chunks))
		}
		// Every chunk carries the RAG contract and its file refs resolve.
		var firstIDs []string
		for _, p := range chunks {
			raw, _ := os.ReadFile(p)
			var chunk map[string]any
			if err := json.Unmarshal(raw, &chunk); err != nil {
				t.Fatalf("chunk %s does not parse: %v", p, err)
			}
			for _, k := range []string{"doc_id", "chunk_id", "symbols", "files"} {
				if _, ok := chunk[k]; !ok {
					t.Errorf("chunk %s missing key %q", filepath.Base(p), k)
				}
			}
			id, _ := chunk["doc_id"].(string)
			cid, _ := chunk["chunk_id"].(string)
			if id == "" || cid == "" {
				t.Errorf("chunk %s has empty doc_id/chunk_id", filepath.Base(p))
			}
			firstIDs = append(firstIDs, cid)
			if refs, ok := chunk["file_refs"].([]any); ok {
				for _, r := range refs {
					rs, _ := r.(string)
					filePart := rs
					if at := strings.LastIndex(rs, "@"); at >= 0 {
						filePart = rs[at+1:]
					}
					if colon := strings.Index(filePart, "#"); colon >= 0 {
						filePart = filePart[:colon]
					}
					if filePart == "" {
						continue
					}
					if !sb.Exists(filepath.FromSlash(filePart)) {
						t.Errorf("chunk %s file_ref %q does not resolve", filepath.Base(p), rs)
					}
				}
			}
		}
		// Chunk ids are content-hash stable: a second export reproduces them.
		gmb(t, sb, "doc", "export", "--format", "rag")
		chunkPaths2, _ := filepath.Glob(sb.Path(".glassmarble", "rag", "*.json"))
		var secondIDs []string
		for _, p := range chunkPaths2 {
			if filepath.Base(p) == "manifest.json" {
				continue
			}
			raw, _ := os.ReadFile(p)
			var chunk map[string]any
			_ = json.Unmarshal(raw, &chunk)
			if cid, _ := chunk["chunk_id"].(string); cid != "" {
				secondIDs = append(secondIDs, cid)
			}
		}
		if strings.Join(firstIDs, ",") != strings.Join(secondIDs, ",") {
			t.Errorf("chunk ids unstable across exports:\n%v\n%v", firstIDs, secondIDs)
		}
	})

	t.Run("daemon_lifecycle", func(t *testing.T) {
		bin := harness.BuildBinary(t)
		sb, _ := docShopSandbox(t, false)
		docWriteConfig(t, sb, -1)
		head := sb.GitCommit("add docs config")
		docRunWrite(t, sb, head)

		// Start docserve as a REAL background process (not in-process).
		// --no-llm keeps the daemon offline and deterministic (CI has no
		// live provider, and the mandatory-LLM gate would otherwise refuse
		// to start the watcher at all).
		proc := exec.Command(bin, "docserve", "--no-llm", "--debounce-ms", "500")
		proc.Dir = sb.Root
		var stdout, stderr syncBuffer
		proc.Stdout, proc.Stderr = &stdout, &stderr
		if err := proc.Start(); err != nil {
			t.Fatalf("starting docserve: %v", err)
		}
		t.Cleanup(func() {
			_ = proc.Process.Kill()
			_, _ = proc.Process.Wait()
		})
		combined := func() string { return stdout.String() + stderr.String() }
		coordPollUntil(t, 10*time.Second, "docserve watching line", func() bool {
			return strings.Contains(combined(), "watching")
		})

		// Touch a scope file AND the instruction, then commit so HEAD
		// moves. The commit message carries an arch keyword ("service"):
		// the daemon runs without --force, so the batch needs (a) an
		// in-scope file (else FastBail skips) and (b) a non-empty dossier
		// (else the comment-only skip fires) — the keyword provides (b)
		// via a SERVICE_ADDED event while the instruction edit is what
		// actually dirties the section. Then poll for the batch.
		cfg := sb.ReadFile(".glassmarble/docs.yaml")
		sb.WriteFile(".glassmarble/docs.yaml", strings.Replace(cfg,
			"instruction: Describe the exported helpers in this package.",
			"instruction: Describe the exported helpers for daemon watch.", 1))
		sb.WriteFile("pkg/shop/shop.go", docShopGo+"// daemon: keepalive touch.\n")
		daemonHead := sb.GitCommit("update guide instruction for new service")
		// Post-commit worktree touch: guarantees at least one fs event
		// strictly AFTER the commit, so the debounced batch resolves HEAD
		// to daemonHead deterministically (events that precede the commit
		// would otherwise race it and the batch would see the old HEAD).
		sb.WriteFile("pkg/shop/shop.go", docShopGo+"// daemon: keepalive touch.\n// daemon: post-commit touch.\n")
		coordPollUntil(t, 20*time.Second, "daemon-updated guide.md", func() bool {
			raw, err := os.ReadFile(sb.Path("docs", "guide.md"))
			return err == nil && strings.Contains(string(raw), "for daemon watch")
		})
		// The batch summary prints after onChange returns, i.e. after the
		// file write the poll above observed — wait for it explicitly
		// rather than asserting a flush race.
		coordPollUntil(t, 10*time.Second, "docserve batch summary", func() bool {
			return strings.Contains(combined(), "batch of")
		})

		// Kill the daemon and verify no corruption: state loads and points
		// at the new HEAD. docserve is signal-silent by design (it prints
		// the watching line + "Press Ctrl+C to stop." up front and nothing
		// on shutdown), so the corruption check is the shutdown assertion.
		mustContain(t, combined(), "Press Ctrl+C to stop.")
		if err := proc.Process.Kill(); err != nil {
			t.Fatalf("killing docserve: %v", err)
		}
		if _, err := proc.Process.Wait(); err != nil {
			t.Logf("docserve wait: %v", err)
		}
		// State via the shared binary-backed helper (in-process --out
		// would poison the shared tree for later bare-state exports).
		if lc := funcStateCommit(t, sb); lc != daemonHead {
			t.Errorf("post-daemon last_commit = %q, want %q", lc, daemonHead)
		}
	})

	t.Run("multiprocess_convergence", func(t *testing.T) {
		bin := harness.BuildBinary(t)
		sb, _ := docShopSandbox(t, false)
		docWriteConfig(t, sb, -1)
		head := sb.GitCommit("add docs config")
		docRunWrite(t, sb, head)

		// Launch 3 REAL `doc --write --force --no-llm` processes at once:
		// real lock contention (stronger than goroutine-level). All must
		// exit 0 and the bytes must converge.
		var wg sync.WaitGroup
		errs := make([]error, 3)
		outputs := make([]string, 3)
		for i := 0; i < 3; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				cmd := exec.Command(bin, "doc", "--commit", head, "--write", "--force", "--no-llm")
				cmd.Dir = sb.Root
				out, err := cmd.CombinedOutput()
				outputs[i] = string(out)
				errs[i] = err
			}(i)
		}
		wg.Wait()
		for i := range errs {
			if errs[i] != nil {
				t.Errorf("worker %d failed: %v\n%s", i, errs[i], outputs[i])
			}
		}
		body := sb.ReadFile("docs/guide.md")
		mustContain(t, body, "gmb:begin:overview", "gmb:mode:deterministic")
		if lc := funcStateCommit(t, sb); lc != head {
			t.Errorf("post-convergence last_commit = %q, want %q", lc, head)
		}
	})
}
