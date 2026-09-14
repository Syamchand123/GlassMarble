package e2e_test

// FUNCTIONAL MATRIX for the docs-engine (`gmb doc*` CLI).
//
// Rules for this file:
//   - Blackbox only: drive the CLI in-process via the harness (gmb/gmbErr)
//     or, where real process exit codes are required, via ONE shared built
//     binary (harness.BuildBinary caches it per test process).
//     No production code is imported or modified.
//   - Deterministic, no network: every run uses --no-llm and the known
//     live-provider env keys are scrubbed (funcScrubDocEnv) so a run can
//     never reach a real provider even when keys exist in the environment.
//   - Fast: tiny fixtures (1-3 Go files), no sleeps anywhere in this file.
//   - No t.Parallel anywhere (the harness runner mutates os.Stdout + CWD).
//
// Where the CLI is lenient (exit 0 on filter misses, warning fallbacks),
// the test asserts the ACTUAL behavior and documents it inline instead of
// asserting an idealized contract.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Syamchand123/GlassMarble/tests/harness"
)

const (
	funcOtherGo = `package other

// Locate finds a widget by id.
func Locate(id string) string {
	return "w:" + id
}
`
)

// funcScrubDocEnv blanks every known live-provider key for the duration of
// the test so offline (--no-llm) runs can never leak to a real provider.
// t.Setenv restores the original values on cleanup.
func funcScrubDocEnv(t *testing.T) {
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

// funcTwoDocSandbox builds a sandbox with two code scopes (pkg/shop,
// pkg/other) and two managed documents with distinct ids AND tags:
// guide (tag team-a) and extra (tag team-b). Returns sandbox + HEAD.
func funcTwoDocSandbox(t *testing.T) (*harness.Sandbox, string) {
	t.Helper()
	sb := harness.NewSandbox(t)
	sb.RequireGit()
	sb.WriteFile("go.mod", docGoMod)
	sb.WriteFile("pkg/shop/shop.go", docShopGo)
	sb.WriteFile("pkg/other/other.go", funcOtherGo)
	sb.WriteFile(".glassmarble/docs.yaml", `version: 1
docs_dir: docs
target_platform: github_flat
documents:
  - id: guide
    target: docs/guide.md
    title: Shop Guide
    purpose: Reference for the shop package.
    audience: Developers
    tags: [team-a]
    scope:
      paths:
        - pkg/shop/**
    sections:
      - id: overview
        title: Overview
        instruction: Describe the exported helpers in this package.
        managed: true
  - id: extra
    target: docs/extra.md
    title: Other Guide
    purpose: Reference for the other package.
    audience: Developers
    tags: [team-b]
    scope:
      paths:
        - pkg/other/**
    sections:
      - id: overview
        title: Overview
        instruction: Describe the helpers in the other package.
        managed: true
`)
	sb.GitInit()
	return sb, sb.GitCommit("add two-doc config")
}

// funcThreeSectionSandbox builds a sandbox whose single document carries
// three managed sections (for --sample capping tests).
func funcThreeSectionSandbox(t *testing.T) (*harness.Sandbox, string) {
	t.Helper()
	sb := harness.NewSandbox(t)
	sb.RequireGit()
	sb.WriteFile("go.mod", docGoMod)
	sb.WriteFile("pkg/shop/shop.go", docShopGo)
	sb.WriteFile(".glassmarble/docs.yaml", `version: 1
docs_dir: docs
target_platform: github_flat
documents:
  - id: guide
    target: docs/guide.md
    title: Shop Guide
    purpose: Reference for the shop package.
    audience: Developers
    scope:
      paths:
        - pkg/shop/**
    sections:
      - id: s1
        title: First
        instruction: Describe the first aspect of this package.
        managed: true
      - id: s2
        title: Second
        instruction: Describe the second aspect of this package.
        managed: true
      - id: s3
        title: Third
        instruction: Describe the third aspect of this package.
        managed: true
`)
	sb.GitInit()
	return sb, sb.GitCommit("add three-section config")
}

// funcRequireDocTargets asserts a check/status --json payload lists exactly
// the given document target paths (filter-precision assertion).
func funcRequireDocTargets(t *testing.T, v map[string]any, want ...string) {
	t.Helper()
	raw, ok := v["Documents"].([]any)
	if !ok {
		t.Fatalf("JSON payload has no Documents array: %v", v)
	}
	var got []string
	for _, d := range raw {
		if m, ok := d.(map[string]any); ok {
			// JSON tags are lowercase ("target_path"); "id" likewise.
			if tp, _ := m["target_path"].(string); tp != "" {
				got = append(got, tp)
			} else if id, _ := m["id"].(string); id != "" {
				got = append(got, id)
			}
		}
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("filtered documents = %v, want %v", got, want)
	}
}
// funcStateCommit returns the engine's last_commit for the sandbox WITHOUT
// touching the in-process CLI: it reads .glassmarble/docs_state.json via the
// REAL binary (separate process). Rationale: the shared in-process command
// tree retains pflag Changed bits, so any in-process `export --out` (even in
// an unrelated earlier test) flips a later bare `export --format state`
// from stdout to file output — reading state through the binary (or the
// state file) is order-independent. The binary is built once per process.
func funcStateCommit(t *testing.T, sb *harness.Sandbox) string {
	t.Helper()
	bin := harness.BuildBinary(t)
	stdout, _, code := harness.RunBinary(t, bin, sb.Root, nil, "doc", "export", "--format", "state")
	if code != 0 {
		t.Fatalf("binary export state exit %d:\n%s", code, stdout)
	}
	v := parseJSONObject(t, stdout)
	lc, _ := v["last_commit"].(string)
	if lc == "" {
		t.Fatalf("exported state has no last_commit:\n%s", stdout)
	}
	return lc
}

func TestDocEngineFunctional(t *testing.T) {
	funcScrubDocEnv(t)

	t.Run("check_diff_status_eval_ledger_filters", func(t *testing.T) {
		sb, head := funcTwoDocSandbox(t)
		docRunWrite(t, sb, head)

		// --doc / --tag narrow check to the selected document(s). A fresh
		// check prints no per-doc rows, so filter precision is asserted
		// on the --json Documents array, not on text output.
		out := gmb(t, sb, "doc", "check", "--doc", "guide")
		mustContain(t, out, "all 1 document(s) fresh")
		checkGuide := parseJSONObject(t, gmb(t, sb, "doc", "check", "--doc", "guide", "--json"))
		funcRequireDocTargets(t, checkGuide, "docs/guide.md")
		out = gmb(t, sb, "doc", "check", "--tag", "team-b")
		mustContain(t, out, "all 1 document(s) fresh")
		checkTag := parseJSONObject(t, gmb(t, sb, "doc", "check", "--tag", "team-b", "--json"))
		funcRequireDocTargets(t, checkTag, "docs/extra.md")
		// --json contract for check.
		requireKeys(t, "doc check", parseJSONObject(t, gmb(t, sb, "doc", "check", "--json")),
			"AllFresh", "Documents", "GlobalFreshness")

		// diff on a synced tree is clean (in-process-safe: pending diffs
		// call os.Exit, so only the clean path runs in-process here;
		// pending-diff exit codes are pinned via the real binary in
		// TestDocEngineExitCodesBinary).
		out = gmb(t, sb, "doc", "diff", "--doc", "guide")
		mustContain(t, out, "up-to-date")
		out = gmb(t, sb, "doc", "diff", "--tag", "team-a")
		mustContain(t, out, "up-to-date")
		diffParsed := parseJSONObject(t, gmb(t, sb, "doc", "diff", "--json"))
		requireKeys(t, "doc diff", diffParsed, "has_changes", "sections")
		if has, _ := diffParsed["has_changes"].(bool); has {
			t.Errorf("synced diff reports pending changes: %v", diffParsed)
		}

		// status honors the same filters (precision via --json fields).
		out = gmb(t, sb, "doc", "status", "--doc", "extra")
		mustContain(t, out, "docs/extra.md")
		statusTag := parseJSONObject(t, gmb(t, sb, "doc", "status", "--tag", "team-a", "--json"))
		funcRequireDocTargets(t, statusTag, "docs/guide.md")
		requireKeys(t, "doc status", parseJSONObject(t, gmb(t, sb, "doc", "status", "--json")),
			"AllFresh", "Documents", "GlobalFreshness")

		// eval supports --json but has NO --doc/--tag filter (actual:
		// cobra rejects the flag). Assert the actual behavior.
		evalParsed := parseJSONObject(t, gmb(t, sb, "doc", "eval", "--json"))
		requireKeys(t, "doc eval", evalParsed, "global_score", "samples", "docs")
		if _, err := gmbErr(t, sb, "doc", "eval", "--doc", "guide"); err == nil {
			t.Errorf("doc eval --doc: expected 'unknown flag' rejection, got success")
		} else if !strings.Contains(err.Error(), "unknown flag: --doc") {
			t.Errorf("doc eval --doc: want 'unknown flag: --doc', got %v", err)
		}

		// ledger honors --last and --json.
		ledgerParsed := parseJSONObject(t, gmb(t, sb, "doc", "ledger", "--json"))
		requireKeys(t, "doc ledger", ledgerParsed, "runs", "total_tokens", "tracks_used")
		gmbWant(t, sb, []string{"run(s)"}, "doc", "ledger", "--last", "1")

		// the update pipeline honors --doc/--tag/--json.
		runParsed := parseJSONObject(t, gmb(t, sb, "doc", "--commit", head,
			"--write", "--force", "--no-llm", "--json", "--doc", "guide"))
		requireKeys(t, "doc run", runParsed,
			"commit", "docs_updated", "sections_processed", "sections_updated", "tokens_used", "duration_ms")
		runParsed = parseJSONObject(t, gmb(t, sb, "doc", "--commit", head,
			"--write", "--force", "--no-llm", "--json", "--tag", "team-b"))
		requireKeys(t, "doc run tag", runParsed,
			"commit", "docs_updated", "sections_processed", "sections_updated", "tokens_used", "duration_ms")
	})

	t.Run("doc_tag_no_match_is_lenient", func(t *testing.T) {
		sb, head := funcTwoDocSandbox(t)
		docRunWrite(t, sb, head)

		// ACTUAL: filter misses are lenient (exit 0). The machine-readable
		// signal for `doc` runs is the "error" field naming the id; the
		// read-only subcommands report an empty set. Documented here so a
		// future strict-error change will fail loudly.
		runParsed := parseJSONObject(t, gmb(t, sb, "doc", "--doc", "does-not-exist",
			"--no-llm", "--json"))
		errField, _ := runParsed["error"].(string)
		if !strings.Contains(errField, "does-not-exist") {
			t.Errorf("doc run no-match JSON error should name the id, got %v", runParsed)
		}
		mustContain(t, errField, "found in docs.yaml")

		out := gmb(t, sb, "doc", "check", "--doc", "does-not-exist")
		mustContain(t, out, "0 document(s)")
		out = gmb(t, sb, "doc", "check", "--tag", "no-such-tag")
		mustContain(t, out, "0 document(s)")
		diffParsed := parseJSONObject(t, gmb(t, sb, "doc", "diff", "--doc", "does-not-exist", "--json"))
		if has, _ := diffParsed["has_changes"].(bool); has {
			t.Errorf("diff no-match reports pending changes: %v", diffParsed)
		}
		out = gmb(t, sb, "doc", "status", "--doc", "does-not-exist")
		mustContain(t, out, "0 managed document(s)")
	})

	t.Run("force_semantics", func(t *testing.T) {
		sb, _ := docShopSandbox(t, false)
		docWriteConfig(t, sb, -1)
		head := sb.GitCommit("add docs config")

		// A config-only commit touches no scope, so the first run needs
		// --force to render at all (fast-bail would skip it otherwise).
		first := gmb(t, sb, "doc", "--commit", head, "--write", "--force", "--no-llm", "--verbose")
		mustContain(t, first, "processing 1 document(s)")
		before := sb.ReadFile("docs/guide.md")

		// Same commit without --force is skipped fast.
		second := gmb(t, sb, "doc", "--commit", head, "--write", "--no-llm", "--verbose")
		mustContain(t, second, "already processed")

		// --force reprocesses but stays byte-identical (zero churn).
		third := gmb(t, sb, "doc", "--commit", head, "--write", "--force", "--no-llm", "--verbose")
		mustContain(t, third, "processing 1 document(s)")
		if after := sb.ReadFile("docs/guide.md"); after != before {
			t.Errorf("forced rerun changed %d bytes (want byte-identical)", len(after)-len(before))
		}
	})

	t.Run("commit_targeting_older", func(t *testing.T) {
		sb, _ := docShopSandbox(t, false)
		docWriteConfig(t, sb, -1)
		oldHead := sb.GitCommit("add docs config")
		gmb(t, sb, "doc", "--commit", oldHead, "--write", "--force", "--no-llm")
		if got := funcStateCommit(t, sb); got != oldHead {
			t.Errorf("state last_commit = %q, want %q", got, oldHead)
		}

		sb.WriteFile("pkg/shop/extra.go", "package shop\n\n// Wave waves hello.\nfunc Wave() string { return \"wave\" }\n")
		newHead := sb.GitCommit("add wave helper")
		gmb(t, sb, "doc", "--commit", newHead, "--write", "--force", "--no-llm")
		if got := funcStateCommit(t, sb); got != newHead {
			t.Errorf("state last_commit = %q, want %q", got, newHead)
		}

		// Targeting the older commit rewinds engine state to it.
		gmb(t, sb, "doc", "--commit", oldHead, "--write", "--force", "--no-llm")
		if got := funcStateCommit(t, sb); got != oldHead {
			t.Errorf("rewind: state last_commit = %q, want older %q", got, oldHead)
		}
	})

	t.Run("branch_policy_tag_only", func(t *testing.T) {
		sb, _ := docShopSandbox(t, false)
		docWriteConfig(t, sb, -1)
		base := sb.GitCommit("add docs config")
		docRunWrite(t, sb, base)
		synced := sb.ReadFile("docs/guide.md")

		sb.WriteFile("pkg/shop/extra.go", "package shop\n\n// Wave waves hello.\nfunc Wave() string { return \"wave\" }\n")
		featHead := sb.GitCommit("add wave helper")

		// Off-tag HEAD under tag-only: compute-only, files + state untouched.
		out := gmb(t, sb, "doc", "--commit", featHead, "--force", "--no-llm", "--branch-policy", "tag-only")
		mustContain(t, out, "tag-only", "compute-only")
		if got := sb.ReadFile("docs/guide.md"); got != synced {
			t.Errorf("tag-only off-tag run modified docs")
		}
		if got := funcStateCommit(t, sb); got != base {
			t.Errorf("tag-only off-tag run advanced state to %q", got)
		}

		// Exact annotated tag at HEAD: writes.
		sb.MustGit("tag", "-a", "v0.1.0", "-m", "release")
		out = gmb(t, sb, "doc", "--commit", featHead, "--force", "--no-llm", "--branch-policy", "tag-only")
		mustNotContain(t, out, "compute-only")
		if got := funcStateCommit(t, sb); got != featHead {
			t.Errorf("tag-only exact-tag run did not advance state (got %q)", got)
		}

		// --write overrides tag-only off-tag on a NON-main branch (on main
		// --write is a silent no-op per resolveWritePermission, so the
		// override warning only fires off-main — asserted here).
		sb.MustGit("checkout", "-b", "feature/wave")
		sb.WriteFile("pkg/shop/extra.go", "package shop\n\n// Wave waves hello.\nfunc Wave() string { return \"wave v2\" }\n")
		overrideHead := sb.GitCommit("tweak wave helper")
		offBranch := gmb(t, sb, "doc", "--commit", overrideHead, "--force", "--no-llm",
			"--branch-policy", "tag-only")
		mustContain(t, offBranch, "tag-only", "compute-only")
		if got := funcStateCommit(t, sb); got == overrideHead {
			t.Errorf("tag-only off-tag run on feature branch advanced state (want compute-only)")
		}
		out = gmb(t, sb, "doc", "--commit", overrideHead, "--force", "--no-llm",
			"--branch-policy", "tag-only", "--write")
		mustContain(t, out, "ForceWrite override")
		if got := funcStateCommit(t, sb); got != overrideHead {
			t.Errorf("--write override did not advance state (got %q)", got)
		}
	})

	t.Run("write_on_main_is_noop", func(t *testing.T) {
		sb, _ := docShopSandbox(t, false)
		docWriteConfig(t, sb, -1)
		head := sb.GitCommit("add docs config")

		// On main/master --write changes nothing about the outcome and must
		// not emit spurious policy warnings.
		out := gmb(t, sb, "doc", "--commit", head, "--write", "--force", "--no-llm", "--verbose")
		mustNotContain(t, out, "compute-only", "ForceWrite override")
		if !sb.Exists("docs/guide.md") {
			t.Errorf("--write on main did not write docs")
		}
	})

	t.Run("invalid_branch_policy_warns", func(t *testing.T) {
		sb, _ := docShopSandbox(t, false)
		docWriteConfig(t, sb, -1)
		head := sb.GitCommit("add docs config")

		// ACTUAL: an unknown --branch-policy is a WARNING with main-only
		// fallback, not an error — the run still succeeds on main. NOTE: no
		// --write here: on main --write short-circuits policy validation
		// (ForceWrite early-return) and the warning never fires.
		out := gmb(t, sb, "doc", "--commit", head, "--force", "--no-llm",
			"--branch-policy", "bogus-policy")
		mustContain(t, out, `unknown branch-policy "bogus-policy"`, "falling back to main-only")
		if !sb.Exists("docs/guide.md") {
			t.Errorf("unknown branch-policy run should still write on main (fallback)")
		}
	})

	t.Run("verify_snippets_and_fix", func(t *testing.T) {
		sb, _ := docShopSandbox(t, false)
		docWriteConfig(t, sb, -1)
		sb.GitCommit("add docs config")
		sb.WriteFile("docs/guide.md", "# Shop Guide\n\n<!-- gmb:snippet:example -->\n```go\nGreet(\"a\", \"b\")\n```\n")

		// Detection names the file and fails the gate.
		detectOut, err := gmbErr(t, sb, "doc", "check", "--verify-snippets")
		if err == nil {
			t.Fatalf("expected snippet verification failure, got success:\n%s", detectOut)
		}
		mustContain(t, detectOut, "SNIPPET ERROR", "docs/guide.md")

		// ACTUAL: --fix WITHOUT --verify-snippets is a no-op for snippets
		// (only TOC regeneration runs): exit 0, file untouched. Documented
		// so a future coupling change will fail loudly.
		before := sb.ReadFile("docs/guide.md")
		if _, err := gmbErr(t, sb, "doc", "check", "--fix"); err != nil {
			t.Fatalf("check --fix without --verify-snippets should succeed, got: %v", err)
		}
		if after := sb.ReadFile("docs/guide.md"); after != before {
			t.Errorf("--fix without --verify-snippets modified the doc (want no-op)")
		}

		// Explicit --fix WITH --verify-snippets rewrites deterministically.
		fixOut, _ := gmbErr(t, sb, "doc", "check", "--verify-snippets", "--fix")
		mustContain(t, fixOut, "SNIPPET FIXED", "docs/guide.md")
		fixed := sb.ReadFile("docs/guide.md")
		if fixed == before {
			t.Fatalf("--fix did not modify the doc")
		}
		mustContain(t, fixed, "Greet(name string)")

		// ACTUAL: the deterministic fix inserts the raw canonical
		// signature, which is not itself a parseable call snippet — the
		// gate stays red with a NEW syntax error (documented so a future
		// fix-quality change will fail loudly). The fix makes progress
		// (different error) but still needs human follow-up.
		afterOut, err := gmbErr(t, sb, "doc", "check", "--verify-snippets")
		if err == nil {
			t.Fatalf("expected residual snippet error after --fix, got success:\n%s", afterOut)
		}
		mustContain(t, afterOut, "SNIPPET ERROR", "docs/guide.md")
		if afterOut == detectOut {
			t.Errorf("--fix did not change the snippet error (want progress to a new error)")
		}
	})

	t.Run("min_score_gating", func(t *testing.T) {
		sb, _ := docShopSandbox(t, false)
		docWriteConfig(t, sb, -1)
		head := sb.GitCommit("add docs config")
		docRunWrite(t, sb, head)

		// Trivially satisfied threshold passes.
		gmbWant(t, sb, []string{"faithfulness"}, "doc", "eval", "--min-score", "0")

		// Impossible threshold (scores max out at 1.0) fails the gate.
		// NOTE: cobra usage errors and RunE failures surface via err, not
		// the captured output — quality assertions below read err.Error().
		_, err := gmbErr(t, sb, "doc", "eval", "--min-score", "2")
		if err == nil {
			t.Fatalf("expected min-score gate failure, got success")
		}
		mustContain(t, err.Error(), "below minimum")

		// Same contract via real process exit codes (shared binary build).
		bin := harness.BuildBinary(t)
		if _, _, code := harness.RunBinary(t, bin, sb.Root, nil, "doc", "eval", "--min-score", "0"); code != 0 {
			t.Errorf("binary eval --min-score 0: want exit 0, got %d", code)
		}
		_, stderr, code := harness.RunBinary(t, bin, sb.Root, nil, "doc", "eval", "--min-score", "2")
		if code != 1 {
			t.Errorf("binary eval --min-score 2: want exit 1, got %d (stderr: %s)", code, stderr)
		}
	})

	t.Run("sample_capping", func(t *testing.T) {
		sb, head := funcThreeSectionSandbox(t)
		docRunWrite(t, sb, head)

		for _, tc := range []struct{ flag, want string }{
			{"1", `"samples": 1`},
			{"2", `"samples": 2`},
		} {
			out := gmb(t, sb, "doc", "eval", "--sample", tc.flag, "--json")
			mustContain(t, out, tc.want)
		}
		full := parseJSONObject(t, gmb(t, sb, "doc", "eval", "--json"))
		if samples, _ := full["samples"].(float64); samples != 3 {
			t.Errorf("uncapped eval samples = %v, want 3", full["samples"])
		}
	})

	t.Run("ledger_last_n", func(t *testing.T) {
		sb, _ := docShopSandbox(t, false)
		docWriteConfig(t, sb, -1)
		head := sb.GitCommit("add docs config")
		// Three effective runs on the same commit (--force bypasses the
		// already-processed skip; every run appends one ledger record).
		for i := 0; i < 3; i++ {
			docRunWrite(t, sb, head)
		}

		last1 := parseJSONObject(t, gmb(t, sb, "doc", "ledger", "--last", "1", "--json"))
		if runs, _ := last1["runs"].(float64); runs != 1 {
			t.Errorf("ledger --last 1 runs = %v, want 1", runs)
		}
		last2 := parseJSONObject(t, gmb(t, sb, "doc", "ledger", "--last", "2", "--json"))
		if runs, _ := last2["runs"].(float64); runs != 2 {
			t.Errorf("ledger --last 2 runs = %v, want 2", runs)
		}
		all := parseJSONObject(t, gmb(t, sb, "doc", "ledger", "--json"))
		if runs, _ := all["runs"].(float64); runs < 3 {
			t.Errorf("ledger uncapped runs = %v, want >= 3", runs)
		}
	})

	t.Run("out_creates_parent_dirs", func(t *testing.T) {
		sb, _ := docShopSandbox(t, false)
		docWriteConfig(t, sb, -1)
		head := sb.GitCommit("add docs config")
		docRunWrite(t, sb, head)

		out := gmb(t, sb, "doc", "release", "HEAD..HEAD", "--out", "docs/mig/nested/guide.md")
		mustContain(t, out, "migration guide written")
		if !sb.Exists("docs/mig/nested/guide.md") {
			t.Fatalf("--out did not create nested parent dirs")
		}
		mustContain(t, sb.ReadFile("docs/mig/nested/guide.md"), "Migration Guide")

		// export --out runs through the REAL binary (separate process): the
		// shared in-process tree would otherwise retain the --out Changed
		// bit and flip later bare `export --format state` calls (here and
		// in other test files) from stdout to file output.
		bin := harness.BuildBinary(t)
		stdout, stderr, code := harness.RunBinary(t, bin, sb.Root, nil,
			"doc", "export", "--format", "state", "--out", ".glassmarble/custom/dir/state.json")
		if code != 0 {
			t.Fatalf("binary export --out exit %d: %s%s", code, stdout, stderr)
		}
		if !sb.Exists(".glassmarble/custom/dir/state.json") {
			t.Fatalf("export --out did not create nested parent dirs")
		}
		var stateObj map[string]any
		if err := json.Unmarshal([]byte(sb.ReadFile(".glassmarble/custom/dir/state.json")), &stateObj); err != nil {
			t.Fatalf("exported state.json does not parse: %v", err)
		}
		for _, k := range []string{"schema_version", "documents"} {
			if _, ok := stateObj[k]; !ok {
				t.Errorf("exported state.json missing key %q", k)
			}
		}
	})

	t.Run("export_formats", func(t *testing.T) {
		sb, _ := docShopSandbox(t, false)
		docWriteConfig(t, sb, -1)
		head := sb.GitCommit("add docs config")
		docRunWrite(t, sb, head)

		// rag: manifest parses and chunk files carry the RAG contract.
		gmbWant(t, sb, []string{"RAG chunk"}, "doc", "export", "--format", "rag")
		var manifest struct {
			ChunksCount int `json:"chunks_count"`
		}
		if err := json.Unmarshal([]byte(sb.ReadFile(".glassmarble/rag/manifest.json")), &manifest); err != nil {
			t.Fatalf("rag manifest.json does not parse: %v", err)
		}
		if manifest.ChunksCount < 1 {
			t.Errorf("rag manifest reports no chunks")
		}
		chunks, err := filepath.Glob(sb.Path(".glassmarble", "rag", "guide_*.json"))
		if err != nil || len(chunks) == 0 {
			t.Fatalf("rag export wrote no section chunk files")
		}
		chunkBody, _ := os.ReadFile(chunks[0])
		mustContain(t, string(chunkBody), `"doc_id"`, `"chunk_id"`, `"symbols"`)

		// jsonl: one JSON object per line.
		gmbWant(t, sb, []string{"RAG chunk"}, "doc", "export", "--format", "jsonl")
		jsonlRaw := sb.ReadFile(".glassmarble/rag/knowledge_base.jsonl")
		lines := strings.Split(strings.TrimSpace(jsonlRaw), "\n")
		if len(lines) < 1 {
			t.Fatalf("jsonl export is empty")
		}
		for i, ln := range lines {
			var obj map[string]any
			if err := json.Unmarshal([]byte(ln), &obj); err != nil {
				t.Fatalf("jsonl line %d does not parse: %v", i, err)
			}
		}

		// state via the REAL binary stdout (in-process --out would poison
		// the shared tree's Changed bit; see funcStateCommit).
		binState := harness.BuildBinary(t)
		stateStdout, _, stateCode := harness.RunBinary(t, binState, sb.Root, nil,
			"doc", "export", "--format", "state")
		if stateCode != 0 {
			t.Fatalf("binary state export exit %d:\n%s", stateCode, stateStdout)
		}
		var stateObj map[string]any
		if err := json.Unmarshal([]byte(stateStdout), &stateObj); err != nil {
			t.Fatalf("state export does not parse: %v", err)
		}
		for _, k := range []string{"schema_version", "documents"} {
			if _, ok := stateObj[k]; !ok {
				t.Errorf("state export missing key %q", k)
			}
		}

		// ACTUAL: unknown --format values are NOT validated — the export
		// falls back to rag output with exit 0. Documented so a future
		// validation change will fail loudly.
		fallbackOut := gmb(t, sb, "doc", "export", "--format", "bogus")
		mustContain(t, fallbackOut, "RAG chunk")
	})

	t.Run("no_prune_flag", func(t *testing.T) {
		sb, _ := docShopSandbox(t, false)
		docWriteConfig(t, sb, -1)
		sb.GitCommit("add docs config")

		// ACTUAL: there is no --prune on the doc surface (pruning lives
		// under `gmb housekeeping`); cobra rejects it as unknown.
		if _, err := gmbErr(t, sb, "doc", "export", "--prune"); err == nil {
			t.Errorf("doc export --prune: expected unknown-flag rejection, got success")
		} else if !strings.Contains(err.Error(), "unknown flag: --prune") {
			t.Errorf("doc export --prune: want 'unknown flag: --prune', got %v", err)
		}
		if _, err := gmbErr(t, sb, "doc", "--prune"); err == nil {
			t.Errorf("doc --prune: expected unknown-flag rejection, got success")
		} else if !strings.Contains(err.Error(), "unknown flag: --prune") {
			t.Errorf("doc --prune: want 'unknown flag: --prune', got %v", err)
		}
	})

	t.Run("json_contract", func(t *testing.T) {
		sb, head := funcTwoDocSandbox(t)
		docRunWrite(t, sb, head)

		cases := []struct {
			name string
			args []string
			keys []string
		}{
			{"run", []string{"doc", "--commit", head, "--write", "--force", "--no-llm", "--json"},
				[]string{"commit", "docs_updated", "sections_processed", "sections_updated", "tokens_used", "duration_ms"}},
			{"check", []string{"doc", "check", "--json"},
				[]string{"AllFresh", "Documents", "GlobalFreshness"}},
			{"status", []string{"doc", "status", "--json"},
				[]string{"AllFresh", "Documents", "GlobalFreshness"}},
			{"eval", []string{"doc", "eval", "--json"},
				[]string{"global_score", "samples", "docs"}},
			{"ledger", []string{"doc", "ledger", "--json"},
				[]string{"runs", "total_tokens", "tracks_used"}},
			// Clean-tree diff is the only in-process-safe diff JSON path
			// (a pending diff calls os.Exit in-process; pending-diff exit
			// codes are pinned via the real binary instead).
			{"diff", []string{"doc", "diff", "--json"},
				[]string{"has_changes", "sections"}},
			{"export", []string{"doc", "export", "--json"},
				[]string{"chunks_count", "output_dir", "files"}},
		}
		for _, tc := range cases {
			out := gmb(t, sb, tc.args...)
			requireKeys(t, "doc "+tc.name, parseJSONObject(t, out), tc.keys...)
		}
	})

	t.Run("error_quality", func(t *testing.T) {
		sb, _ := docShopSandbox(t, false)
		// Fail-fast freshness (100) so an in-scope commit is hard drift;
		// docWriteConfig(t, sb, -1) would only warn and exit 0.
		docWriteConfig(t, sb, 100)
		head := sb.GitCommit("add docs config")
		docRunWrite(t, sb, head)

		// Drift names the doc and the breached threshold (next step: resync).
		sb.WriteFile("pkg/shop/shop.go", strings.Replace(docShopGo, "func Greet(", "func GreetV2(", 1))
		sb.GitCommit("refactor: rename Greet to GreetV2")
		driftOut, err := gmbErr(t, sb, "doc", "check")
		if err == nil {
			t.Fatalf("expected drift failure, got success:\n%s", driftOut)
		}
		mustContain(t, driftOut, "docs/guide.md", "FAIL")

		// Bad release range shows the expected shape with an example.
		// (cobra/RunE failures surface via err, not captured output.)
		_, err = gmbErr(t, sb, "doc", "release", "notarange")
		if err == nil {
			t.Fatalf("expected release range failure, got success")
		}
		mustContain(t, err.Error(), "expected <ref1>..<ref2>", "v1.1.0..v1.2.0")

		// Missing --reason names the flag.
		_, err = gmbErr(t, sb, "doc", "review", "record-revert",
			"--doc", "docs/guide.md", "--section", "overview")
		if err == nil {
			t.Fatalf("expected record-revert failure, got success")
		}
		mustContain(t, err.Error(), "--reason", "required")

		// Unknown review id echoes the id.
		_, err = gmbErr(t, sb, "doc", "review", "approve", "deadbeef01", "--reason", "x")
		if err == nil {
			t.Fatalf("expected approve failure, got success")
		}
		mustContain(t, err.Error(), "deadbeef01", "unknown id")

		// Missing managed file names its path.
		if err := os.Remove(sb.Path("docs", "guide.md")); err != nil {
			t.Fatalf("removing guide.md: %v", err)
		}
		missingOut, err := gmbErr(t, sb, "doc", "check")
		if err == nil {
			t.Fatalf("expected missing-file failure, got success:\n%s", missingOut)
		}
		mustContain(t, missingOut, "docs/guide.md", "file missing")
	})
}
