package nonfunctional_test

// SCALABILITY tripwires for the docs-engine feature (NON-FUNCTIONAL-2,
// beyond tests/nonfunctional/doc_engine_perf_test.go).
//
// The perf file already covers at 2000 files: fast-bail, invalidation,
// grounding, gates, atomic write, deterministic render, and one full run.
// Nothing here duplicates those cases; this file extends them:
//
//   - 10k-file synthetic repo: full run, fast-bail no-op, invalidation
//     growth 2k→10k (sub-linear-ish, generous ratio only).
//   - Memory bound: in-process full-run heap growth stays sane.
//   - Startup latency: `doc --help` + no-op run tripwires.
//   - File-descriptor stability: 50 sequential no-op runs keep /proc/self/fd
//     flat (linux only; TZ/locale runs are already covered by
//     TestDocEngineCompat_TZIndependence / UTF8Locale in tests/e2e).
//   - Long target path + 200-section single doc renders correctly.
//
// Convention (mirrors doc_engine_perf_test.go): every budget carries a
// GENEROUS margin (10x-100x over expected reality) so slow CI never flakes.
// Only upper bounds are asserted; no sleeps, no network (--no-llm), env keys
// scrubbed. Tests that may exceed ~60s are marked NOTE. No t.Parallel (the
// harness runner mutates os.Stdout + CWD).

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/doc_engine/catalog"
	docconfig "github.com/Syamchand123/GlassMarble/internal/doc_engine/config"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/invalidator"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/storage"
	"github.com/Syamchand123/GlassMarble/tests/harness"
)

// scaleScrubEnv unsets every known live-provider key for the duration of the
// test so offline (--no-llm) runs can never leak to a real provider.
func scaleScrubEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"OPENAI_API_KEY", "OPENAI_BASE_URL", "OPENAI_ORG_ID",
		"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN",
		"GOOGLE_API_KEY", "GEMINI_API_KEY",
		"AZURE_OPENAI_API_KEY", "AZURE_OPENAI_ENDPOINT",
		"GROQ_API_KEY", "TOGETHER_API_KEY", "COHERE_API_KEY",
		"MISTRAL_API_KEY", "GITHUB_TOKEN",
		"GLASSMARBLE_TEST_LIVE_LLM",
	} {
		if v, ok := os.LookupEnv(k); ok {
			_ = os.Unsetenv(k)
			vv, kk := v, k
			t.Cleanup(func() { _ = os.Setenv(kk, vv) })
		}
	}
}

// scaleRun executes the CLI in-process (flag state reset first, mirroring
// tests/e2e helpers) and fails the test on error.
func scaleRun(t *testing.T, sb *harness.Sandbox, args ...string) string {
	t.Helper()
	harness.ResetFlags()
	out, err := harness.RunGmb(t, sb, args...)
	if err != nil {
		t.Fatalf("gmb %v failed: %v\n--- output ---\n%s", args, err, out)
	}
	return out
}

// scaleTinyRepo builds a git repo with one Go file + the perf-shaped
// docs.yaml, returning the sandbox and HEAD hash.
func scaleTinyRepo(t *testing.T) (*harness.Sandbox, string) {
	t.Helper()
	sb := harness.NewSandbox(t)
	sb.RequireGit()
	sb.GitInit()
	files := map[string]string{
		"go.mod":                 "module example.com/shop\n\ngo 1.21\n",
		"pkg/shop/shop.go":       "package shop\n\n// Greet returns a friendly greeting.\nfunc Greet(name string) string { return \"hello \" + name }\n",
		".glassmarble/docs.yaml": scaleDocsYAML("pkg/shop/**", 1),
	}
	head := sb.GitCommitFiles("scale: tiny fixture", files)
	return sb, head
}

// scaleDocsYAML renders a single-document config over scope with n sections
// (section ids are URL-safe per the loader contract).
func scaleDocsYAML(scope string, n int) string {
	var b strings.Builder
	b.WriteString("version: 1\ndocs_dir: docs\ndocuments:\n  - id: guide\n    target: docs/guide.md\n    title: Guide\n    purpose: Scale fixture.\n    audience: Developers\n    scope:\n      paths:\n        - \"" + scope + "\"\n    sections:\n")
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "      - id: s%03d\n        title: Section %03d\n        instruction: Document the current state.\n        managed: true\n", i, i)
	}
	return b.String()
}

// scaleHeapMB returns current HeapAlloc in MiB after a GC (best-effort
// steady-state read for the in-process CLI runs).
func scaleHeapMB(t *testing.T) float64 {
	t.Helper()
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	mb := float64(m.HeapAlloc) / (1024 * 1024)
	t.Logf("heap: %.1f MiB (numGC=%d)", mb, m.NumGC)
	return mb
}

// ---------------------------------------------------------------------------
// 1. 10k-file synthetic repo: full run completes, no-op fast-bails fast.
//    NOTE: may exceed ~60s (fixture commit + full run dominate).
// ---------------------------------------------------------------------------

func TestDocEngineScaleFull10k(t *testing.T) {
	scaleScrubEnv(t)
	sb, head := docEngPerfRepo(t, 10000)

	start := time.Now()
	out, err := harness.RunGmb(t, sb, "doc", "--no-llm", "--force", "--branch-policy", "any", "--commit", head)
	elapsed := time.Since(start)
	t.Logf("full doc run on 10000-file repo: %s", elapsed)
	if err != nil {
		t.Fatalf("doc run failed: %v\n%s", err, out)
	}
	if !sb.Exists("docs/guide.md") {
		t.Fatalf("expected docs/guide.md after 10k full run")
	}
	if elapsed > 600*time.Second {
		t.Errorf("10k full run took %s, budgeted at 600s (tripwire only)", elapsed)
	}

	// No-op on the converged 10k repo: the engine short-circuits either via
	// the fast-bail path or the already-processed-commit short-circuit —
	// both are legitimate no-op bails (observed: the latter).
	before := sb.ReadFile("docs/guide.md")
	start = time.Now()
	noopOut := scaleRun(t, sb, "doc", "--no-llm", "--branch-policy", "any", "--commit", head, "--verbose")
	noopElapsed := time.Since(start)
	t.Logf("no-op rerun on 10000-file repo: %s", noopElapsed)
	if !strings.Contains(noopOut, "fast-bail") && !strings.Contains(noopOut, "already processed") {
		t.Errorf("expected a no-op bail on converged 10k repo, got:\n%s", noopOut)
	}
	if got := sb.ReadFile("docs/guide.md"); got != before {
		t.Errorf("no-op rerun changed docs/guide.md (%d vs %d bytes)", len(before), len(got))
	}
	if noopElapsed > 60*time.Second {
		t.Errorf("10k no-op took %s, budgeted at 60s (tripwire only)", noopElapsed)
	}
}

// ---------------------------------------------------------------------------
// 2. Invalidation growth 2k→10k: dossier + dirty-section discovery must scale
//    sub-linear-ish — asserted as a GENEROUS ratio only (5x files, <15x
//    time), never an exact bound.
//    NOTE: may exceed ~60s (10k dossier build dominates).
// ---------------------------------------------------------------------------

func TestDocEngineScaleInvalidationGrowth(t *testing.T) {
	scaleScrubEnv(t)
	constraints := &docconfig.GlobalConstraints{}

	measure := func(n int) time.Duration {
		t.Helper()
		sb, head := docEngPerfRepo(t, n)
		cat := catalog.New(docEngPerfDocs())
		state := &storage.DocEngineState{}
		start := time.Now()
		dossier, err := invalidator.BuildDossier(sb.Root, head, nil, nil)
		if err != nil {
			t.Fatalf("BuildDossier(%d): %v", n, err)
		}
		inv := invalidator.New(cat)
		dirty, err := inv.FindDirtySections(dossier, state, nil, constraints)
		elapsed := time.Since(start)
		t.Logf("invalidation on %d-file repo: %s (%d dirty)", n, elapsed, len(dirty))
		if len(dirty) == 0 {
			t.Errorf("expected dirty sections on initial sync (%d files)", n)
		}
		return elapsed
	}

	d2k := measure(2000)
	d10k := measure(10000)
	if d2k == 0 || d10k == 0 {
		t.Fatalf("zero-duration measurement, clock too coarse to compare")
	}
	ratio := float64(d10k) / float64(d2k)
	t.Logf("invalidation growth 2k→10k: %s → %s (ratio %.2f, budget <15x for 5x files)", d2k, d10k, ratio)
	if ratio >= 15 {
		t.Errorf("invalidation scales super-linearly: ratio %.2f >= 15 (tripwire only)", ratio)
	}
	if d10k > 300*time.Second {
		t.Errorf("10k invalidation took %s, budgeted at 300s (tripwire only)", d10k)
	}
}

// ---------------------------------------------------------------------------
// 3. Memory bound: in-process full run over a 2000-file repo keeps heap
//    growth and absolute RSS-proxy (HeapAlloc) sane (generous GiB-scale
//    tripwires — leak/guard rails only, never tuning).
// ---------------------------------------------------------------------------

func TestDocEngineScaleMemoryBound(t *testing.T) {
	scaleScrubEnv(t)
	sb, head := docEngPerfRepo(t, 2000)

	beforeMB := scaleHeapMB(t)
	out, err := harness.RunGmb(t, sb, "doc", "--no-llm", "--force", "--branch-policy", "any", "--commit", head)
	if err != nil {
		t.Fatalf("doc run failed: %v\n%s", err, out)
	}
	afterMB := scaleHeapMB(t)
	growth := afterMB - beforeMB
	t.Logf("2k full run heap: %.1f → %.1f MiB (growth %.1f MiB)", beforeMB, afterMB, growth)
	if growth > 1024 {
		t.Errorf("heap grew %.1f MiB across one 2k run, budgeted at 1024 MiB (tripwire only)", growth)
	}
	if afterMB > 2048 {
		t.Errorf("post-run heap %.1f MiB exceeds 2048 MiB (tripwire only)", afterMB)
	}
	if !sb.Exists("docs/guide.md") {
		t.Errorf("expected docs/guide.md after memory-bound run")
	}
}

// ---------------------------------------------------------------------------
// 4. Startup latency: `doc --help` and a tiny-repo no-op run answer fast.
// ---------------------------------------------------------------------------

func TestDocEngineScaleStartupLatency(t *testing.T) {
	scaleScrubEnv(t)
	sb, head := scaleTinyRepo(t)
	scaleRun(t, sb, "doc", "--no-llm", "--force", "--commit", head)

	start := time.Now()
	harness.ResetFlags()
	if _, err := harness.RunGmb(t, sb, "doc", "--help"); err != nil {
		t.Fatalf("doc --help failed: %v", err)
	}
	helpElapsed := time.Since(start)
	t.Logf("doc --help: %s", helpElapsed)
	if helpElapsed > 30*time.Second {
		t.Errorf("doc --help took %s, budgeted at 30s (tripwire only)", helpElapsed)
	}

	start = time.Now()
	scaleRun(t, sb, "doc", "--no-llm", "--commit", head)
	noopElapsed := time.Since(start)
	t.Logf("tiny-repo no-op run: %s", noopElapsed)
	if noopElapsed > 60*time.Second {
		t.Errorf("tiny-repo no-op took %s, budgeted at 60s (tripwire only)", noopElapsed)
	}
}

// ---------------------------------------------------------------------------
// 5. File-descriptor stability: 50 sequential no-op runs keep the fd count
//    flat (linux /proc/self/fd only; skipped with reason elsewhere).
// ---------------------------------------------------------------------------

func TestDocEngineScaleFDStability(t *testing.T) {
	scaleScrubEnv(t)
	if runtime.GOOS != "linux" {
		t.Skipf("fd stability needs /proc/self/fd (linux only); running on %s", runtime.GOOS)
	}
	const fdDir = "/proc/self/fd"
	if _, err := os.Stat(fdDir); err != nil {
		t.Skipf("fd stability needs /proc/self/fd: %v", err)
	}
	countFDs := func() int {
		t.Helper()
		entries, err := os.ReadDir(fdDir)
		if err != nil {
			t.Fatalf("reading %s: %v", fdDir, err)
		}
		return len(entries)
	}

	sb, head := scaleTinyRepo(t)
	scaleRun(t, sb, "doc", "--no-llm", "--force", "--commit", head)
	runtime.GC()
	base := countFDs()
	for i := 0; i < 50; i++ {
		scaleRun(t, sb, "doc", "--no-llm", "--commit", head)
	}
	runtime.GC()
	final := countFDs()
	t.Logf("fds: %d → %d across 50 sequential no-op runs", base, final)
	if final-base > 5 {
		t.Errorf("fd leak: %d → %d (+%d) across 50 runs, budgeted at +5 (tripwire only)", base, final, final-base)
	}
}

// ---------------------------------------------------------------------------
// 6. Long target path + 200-section single doc: renders correctly with all
//    anchors and full section state.
// ---------------------------------------------------------------------------

func TestDocEngineScaleLongPathManySections(t *testing.T) {
	scaleScrubEnv(t)
	t.Setenv("GMB_DOC_STATE", "json") // read section state back as JSON
	sb := harness.NewSandbox(t)
	sb.RequireGit()
	sb.GitInit()

	// Deep target path, kept under the ~200-char Windows-safe budget (the
	// compat suite pins deep REPO roots; this pins a deep DOC target).
	deep := filepath.Join("docs", "lorem", "ipsum", "dolor", "sit", "amet", "consectetur", "adipiscing", "elit", "sed", "doeiusmod", "deep.md")
	targetAbs := filepath.Join(sb.Root, filepath.FromSlash(deep))
	if len(targetAbs) > 200 {
		t.Fatalf("fixture target %d chars exceeds the 200-char budget", len(targetAbs))
	}
	t.Logf("deep doc target: %d chars", len(targetAbs))

	sb.WriteFile("go.mod", "module example.com/shop\n\ngo 1.21\n")
	sb.WriteFile("pkg/shop/shop.go", "package shop\n\n// Greet returns a friendly greeting.\nfunc Greet(name string) string { return \"hello \" + name }\n")

	var b strings.Builder
	b.WriteString("version: 1\ndocs_dir: docs\ndocuments:\n  - id: big\n    target: " + deep + "\n    title: Big\n    purpose: Scale fixture.\n    audience: Developers\n    scope:\n      paths:\n        - pkg/shop/**\n    sections:\n")
	const nSec = 200
	for i := 0; i < nSec; i++ {
		fmt.Fprintf(&b, "      - id: s%03d\n        title: Section %03d\n        instruction: Document the current state.\n        managed: true\n", i, i)
	}
	sb.WriteFile(".glassmarble/docs.yaml", b.String())
	head := sb.GitCommitFiles("scale: deep many-section fixture", map[string]string{})

	start := time.Now()
	out, err := harness.RunGmb(t, sb, "doc", "--no-llm", "--force", "--branch-policy", "any", "--commit", head)
	elapsed := time.Since(start)
	t.Logf("200-section deep-target run took %s", elapsed)
	if err != nil {
		t.Fatalf("doc run failed: %v\n%s", err, out)
	}
	if elapsed > 180*time.Second {
		t.Errorf("200-section run took %s, budgeted at 180s (tripwire only)", elapsed)
	}

	// Per-run section cap (MaxDocUpdatesPerCommit, default 10): one run
	// renders 10 zones, so converge with bounded reruns until all 200 zones
	// carry rendered content. Deadline-polled, no sleeps.
	body := sb.ReadFile(filepath.ToSlash(deep))
	markers := strings.Count(body, "gmb:mode:deterministic")
	deadline := start.Add(180 * time.Second) // overall tripwire for convergence
	for runs := 1; markers < nSec && runs < 25 && time.Now().Before(deadline); runs++ {
		scaleRun(t, sb, "doc", "--no-llm", "--force", "--branch-policy", "any", "--commit", head)
		body = sb.ReadFile(filepath.ToSlash(deep))
		markers = strings.Count(body, "gmb:mode:deterministic")
	}
	t.Logf("200-section convergence took %s (%d deterministic zones)", time.Since(start), markers)
	if time.Since(start) > 180*time.Second {
		t.Errorf("200-section convergence took %s, budgeted at 180s (tripwire only)", time.Since(start))
	}
	if markers != nSec {
		t.Fatalf("only %d/%d sections rendered after bounded convergence runs", markers, nSec)
	}
	for i := 0; i < nSec; i++ {
		id := fmt.Sprintf("s%03d", i)
		if !strings.Contains(body, "gmb:begin:"+id) || !strings.Contains(body, "gmb:end:"+id) {
			t.Errorf("section %s anchors missing in rendered doc", id)
		}
	}

	// State: the doc entry must exist with section hashes; the count must be
	// non-zero and STABLE across a final rerun (convergence), and the doc
	// bytes must be identical.
	raw := sb.ReadFile(".glassmarble/docs_state.json")
	var state struct {
		Documents map[string]struct {
			Sections map[string]any `json:"sections"`
		} `json:"documents"`
	}
	if err := json.Unmarshal([]byte(raw), &state); err != nil {
		t.Fatalf("state is not JSON: %v\n%s", err, raw)
	}
	got, ok := state.Documents[deep]
	if !ok {
		names := []string{}
		for k := range state.Documents {
			names = append(names, k)
		}
		t.Fatalf("state missing deep target %q (have %v)", deep, names)
	}
	t.Logf("state persists %d/%d section hashes for the deep doc", len(got.Sections), nSec)
	if len(got.Sections) == 0 {
		t.Errorf("state persists no section hashes for the deep doc")
	}
	scaleRun(t, sb, "doc", "--no-llm", "--force", "--branch-policy", "any", "--commit", head)
	raw2 := sb.ReadFile(".glassmarble/docs_state.json")
	var state2 struct {
		Documents map[string]struct {
			Sections map[string]any `json:"sections"`
		} `json:"documents"`
	}
	if err := json.Unmarshal([]byte(raw2), &state2); err != nil {
		t.Fatalf("rerun state is not JSON: %v", err)
	}
	if n2 := len(state2.Documents[deep].Sections); n2 != len(got.Sections) {
		t.Errorf("section-state count unstable across reruns: %d → %d", len(got.Sections), n2)
	}
	if again := sb.ReadFile(filepath.ToSlash(deep)); again != body {
		t.Errorf("200-section rerun was not byte-identical (%d vs %d bytes)", len(body), len(again))
	}
}
