package e2e_test

// CHAOS / FAULT-INJECTION tests for the docs-engine feature
// (internal/doc_engine + `gmb doc*` CLI).
//
// Rules for this file:
//   - Blackbox only: drive REAL compiled binaries as separate processes for
//     every kill test (never in-process: an in-process kill would kill the
//     test runner itself). Each kill test builds gmb once into its own
//     t.TempDir() via chaosBuildBinary. Non-kill cases (corrupt AKG, git
//     lock) use the in-process harness where a real process adds nothing.
//   - No real network: every command uses --no-llm (deterministic renderer)
//     and live LLM keys are scrubbed via chaosScrubEnv at each test start
//     (lesson learned: bare analyze hits live providers when keys exist).
//   - No fixed sleeps: the killer goroutine polls every 5ms and fires on an
//     observed condition (lock files, target/state writes) or a deadline.
//     os.Process.Kill is SIGKILL on unix and TerminateProcess on Windows.
//   - Victim hygiene: baselines run on throwaway repos and victims on fresh
//     (or stale-lock-cleaned) repos, because a converged rerun is faster,
//     skips identical-byte guide writes, and leaves *.lock sidecars behind
//     — all of which would make a "mid-run" kill land at process start.
//   - No t.Parallel anywhere (in-process verification steps mutate os.Stdout
//     + CWD via the harness runner).

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Syamchand123/GlassMarble/tests/harness"
)

// chaosScrubEnv unsets every known live-provider key for the duration of the
// test so offline (--no-llm) runs can never leak to a real provider.
// Self-contained (does not depend on other test files in this package).
func chaosScrubEnv(t *testing.T) {
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

// chaosBuildBinary compiles the real gmb binary once per test into
// t.TempDir() and returns its path.
// NOTE: may exceed ~60s on a cold `go build` cache; warm-cache builds are
// a few seconds.
func chaosBuildBinary(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	root := ""
	for d := dir; ; d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			root = d
			break
		}
		if parent := filepath.Dir(d); parent == d {
			t.Fatalf("no go.mod found above %s", dir)
		}
	}
	name := "gmb"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	out := filepath.Join(t.TempDir(), name)
	cmd := exec.Command("go", "build", "-o", out, ".")
	cmd.Dir = root
	if raw, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build failed: %v\n%s", err, raw)
	}
	return out
}

const chaosGoMod = `module example.com/shop

go 1.21
`

const chaosDocsYAML = `version: 1
docs_dir: docs
documents:
  - id: guide
    target: docs/guide.md
    title: Guide
    purpose: Chaos fixture.
    audience: Developers
    scope:
      paths:
        - pkg/**
    sections:
      - id: overview
        title: Overview
        instruction: Document the current state.
        managed: true
`

// chaosSeedRepo builds a git repo with n generated stub files plus the chaos
// docs.yaml, returning the sandbox and HEAD hash.
func chaosSeedRepo(t *testing.T, n int) (*harness.Sandbox, string) {
	t.Helper()
	sb := harness.NewSandbox(t)
	sb.RequireGit()
	sb.GitInit()
	files := make(map[string]string, n)
	for i := 0; i < n; i++ {
		files["pkg/p"+pad2(i%20)+"/f"+pad4(i)+".go"] =
			"package p" + pad2(i%20) + "\n\n// F" + pad4(i) + " does work.\nfunc F" + pad4(i) + "() string { return \"ok\" }\n"
	}
	head := sb.GitCommitFiles("chaos: generated stubs", files)
	sb.WriteFile("go.mod", chaosGoMod)
	sb.WriteFile(".glassmarble/docs.yaml", chaosDocsYAML)
	head = sb.GitCommit("chaos: docs config")
	return sb, head
}

func pad2(i int) string {
	if i < 10 {
		return "0" + string(rune('0'+i))
	}
	return string(rune('0'+i/10)) + string(rune('0'+i%10))
}

func pad4(i int) string {
	s := ""
	for _, m := range []int{1000, 100, 10, 1} {
		s += string(rune('0' + (i/m)%10))
	}
	return s
}

// chaosRunBinary runs the compiled binary as a separate process with --dir
// pinned to the sandbox root. Returns combined output + error.
func chaosRunBinary(t *testing.T, bin, dir string, env []string, args ...string) (string, error) {
	t.Helper()
	full := append([]string{"doc", "--dir", dir}, args...)
	cmd := exec.Command(bin, full...)
	cmd.Dir = dir
	if env != nil {
		cmd.Env = append(os.Environ(), env...)
	}
	raw, err := cmd.CombinedOutput()
	return string(raw), err
}

// chaosKillRun starts the real binary and kills it (SIGKILL/TerminateProcess)
// once fire() reports true or the deadline passes. Polling only: the killer
// ticks every 5ms and never sleeps a fixed duration to await a condition.
// Returns killed=true only when Kill succeeded while the process was alive;
// the caller should retry with a fresh sandbox when killed=false.
func chaosKillRun(t *testing.T, bin, dir string, env []string, args []string, fire func() bool, deadline time.Duration) (killed bool, output string) {
	t.Helper()
	full := append([]string{"doc", "--dir", dir}, args...)
	cmd := exec.Command(bin, full...)
	cmd.Dir = dir
	if env != nil {
		cmd.Env = append(os.Environ(), env...)
	}
	var so, se bytes.Buffer
	cmd.Stdout, cmd.Stderr = &so, &se
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting victim process: %v", err)
	}
	procDone := make(chan struct{})
	go func() { _ = cmd.Wait(); close(procDone) }()

	var fired atomic.Bool
	deadlineCh := time.After(deadline)
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-procDone:
			return fired.Load(), so.String() + se.String()
		case <-deadlineCh:
			if kerr := cmd.Process.Kill(); kerr == nil {
				fired.Store(true)
			}
			<-procDone
			return fired.Load(), so.String() + se.String()
		case <-tick.C:
			select {
			case <-procDone:
				return fired.Load(), so.String() + se.String()
			default:
			}
			if fire() {
				if kerr := cmd.Process.Kill(); kerr == nil {
					fired.Store(true)
				}
				<-procDone
				return fired.Load(), so.String() + se.String()
			}
		}
	}
}

// chaosLocksObserved reports whether any engine lock file exists yet —
// evidence the victim reached the write/state phase. Callers must remove
// stale sidecars first (chaosRemoveStaleLocks) on reused repos.
//
// Lock sidecars live under .glassmarble/locks/ (storage.FlockForFile),
// named "<basename>-<sha256hex(path)[:16]>.lock" — not beside the target
// file or docs_state — so this globs the locks directory rather than
// checking fixed legacy paths.
func chaosLocksObserved(sb *harness.Sandbox) bool {
	matches, _ := filepath.Glob(sb.Path(".glassmarble", "locks", "*.lock"))
	return len(matches) > 0
}

// chaosGuideWrittenAfter reports whether docs/guide.md was (re)written at or
// after start — evidence the victim passed the render/write phase and is in
// the state-save window. Only meaningful when the victim actually rewrites
// the guide (fresh repos; converged reruns skip identical bytes).
func chaosGuideWrittenAfter(sb *harness.Sandbox, start time.Time) bool {
	fi, err := os.Stat(sb.Path("docs", "guide.md"))
	if err != nil {
		return false
	}
	if fi.Size() == 0 {
		return false
	}
	return !fi.ModTime().Before(start)
}

// chaosStateWrittenAfter reports whether the engine state file (SQLite or
// JSON backend) was (re)written at or after start. Unlike the guide, state
// is re-saved on every run (wall-clock timestamps change bytes), so this
// stays observable even on converged reruns.
func chaosStateWrittenAfter(sb *harness.Sandbox, start time.Time) bool {
	for _, rel := range []string{".glassmarble/docs_state.db", ".glassmarble/docs_state.json"} {
		fi, err := os.Stat(sb.Path(rel))
		if err != nil || fi.Size() == 0 {
			continue
		}
		if !fi.ModTime().Before(start) {
			return true
		}
	}
	return false
}

// chaosRemoveStaleLocks deletes best-effort flock sidecars (*.lock) left by
// a previous run so lock observation reflects the upcoming victim run only.
func chaosRemoveStaleLocks(sb *harness.Sandbox) {
	matches, _ := filepath.Glob(sb.Path(".glassmarble", "locks", "*.lock"))
	for _, m := range matches {
		_ = os.Remove(m)
	}
}

// chaosAssertConverged re-runs the sync in-process to completion and asserts
// the full convergence contract: run succeeds, anchors present, rerun is
// byte-identical, state points at head, check reports fresh.
func chaosAssertConverged(t *testing.T, sb *harness.Sandbox, head string) {
	t.Helper()
	gmb(t, sb, "doc", "--commit", head, "--write", "--force", "--no-llm")
	once := sb.ReadFile("docs/guide.md")
	mustContain(t, once, "gmb:begin:overview", "gmb:end:overview")
	gmb(t, sb, "doc", "--commit", head, "--write", "--force", "--no-llm")
	if twice := sb.ReadFile("docs/guide.md"); twice != once {
		t.Fatalf("rerun did not converge (%d vs %d bytes)", len(once), len(twice))
	}
	stateOut := gmb(t, sb, "doc", "export", "--format", "state")
	if !strings.Contains(stateOut, head) {
		t.Fatalf("state does not point at HEAD %s after rerun:\n%s", head, stateOut)
	}
	gmbWant(t, sb, []string{"fresh"}, "doc", "check")
}

// ---------------------------------------------------------------------------
// 1. SIGKILL mid-run: target file is old-or-new bytes, never torn; rerun
//    converges; state loads.
// ---------------------------------------------------------------------------

func TestDocEngineChaosSigkillMidRun(t *testing.T) {
	chaosScrubEnv(t)
	bin := chaosBuildBinary(t)

	var sb *harness.Sandbox
	var head string
	var baseline time.Duration
	var before []byte
	killed := false
	condHit := false
	// The baseline runs on a THROWAWAY repo (measures T only) and every
	// victim runs on a FRESH repo: reusing one repo would leave stale *.lock
	// sidecars (fire instantly at startup) and converged state behind, so a
	// "mid-run" kill could land at process start instead.
	for attempt := 0; attempt < 3 && !killed; attempt++ {
		mSb, mHead := chaosSeedRepo(t, 250)
		start := time.Now()
		mOut, merr := chaosRunBinary(t, bin, mSb.Root, nil, "--commit", mHead, "--write", "--force", "--no-llm")
		baseline = time.Since(start)
		if merr != nil {
			t.Fatalf("baseline run failed: %v\n%s", merr, mOut)
		}
		_ = mSb

		sb, head = chaosSeedRepo(t, 250)
		// Converge the victim repo first so pre-kill bytes exist for the
		// old-or-new check. This rerun is timed as R: forced reruns redo
		// less work than first runs, so the kill deadline scales to R.
		cStart := time.Now()
		bOut, berr := chaosRunBinary(t, bin, sb.Root, nil, "--commit", head, "--write", "--force", "--no-llm")
		rerunT := time.Since(cStart)
		if berr != nil {
			t.Fatalf("victim-repo converge run failed: %v\n%s", berr, bOut)
		}
		raw, err := os.ReadFile(sb.Path("docs", "guide.md"))
		if err != nil {
			t.Fatalf("reading pre-kill guide: %v", err)
		}
		before = raw
		chaosRemoveStaleLocks(sb)
		t.Logf("attempt %d: baseline %s, converged-rerun %s", attempt, baseline, rerunT)

		victimStart := time.Now()
		// Kill lands mid-run: past startup transients (300ms gate) and no
		// later than half the measured rerun (clamped). Lock observation is
		// a bonus trigger; the deadline is the reliable mechanism.
		dl := rerunT / 2
		if dl < 500*time.Millisecond {
			dl = 500 * time.Millisecond
		}
		if dl > 3*time.Second {
			dl = 3 * time.Second
		}
		var out2 string
		killed, out2 = chaosKillRun(t, bin, sb.Root, nil,
			[]string{"--commit", head, "--write", "--force", "--no-llm"},
			func() bool {
				if time.Since(victimStart) < 300*time.Millisecond {
					return false
				}
				if chaosLocksObserved(sb) {
					condHit = true
					return true
				}
				return false
			}, dl)
		_ = out2
		t.Logf("attempt %d: killed=%v condHit=%v (victim wall %s, deadline %s)", attempt, killed, condHit, time.Since(victimStart), dl)
	}
	if !killed {
		t.Fatalf("could not land SIGKILL mid-run in 3 attempts (baselines took %s)", baseline)
	}

	// Torn-write check: the target is updated via tmp→fsync→rename, so after
	// the kill its bytes must be exactly the old or the new revision
	// (deterministic renders make old and new comparable across runs).
	torn, err := os.ReadFile(sb.Path("docs", "guide.md"))
	if err != nil {
		t.Fatalf("reading post-kill guide: %v", err)
	}
	if !bytes.Equal(torn, before) {
		t.Logf("post-kill bytes differ from pre-kill (%d vs %d bytes); must match post-rerun bytes", len(torn), len(before))
	}
	chaosAssertConverged(t, sb, head)
	after := sb.ReadFile("docs/guide.md")
	if !bytes.Equal(torn, before) && string(torn) != after {
		t.Errorf("torn write: post-kill bytes match neither pre-kill (%dB) nor converged (%dB) revision", len(before), len(after))
	}
}

// ---------------------------------------------------------------------------
// 2. Kill during the state-save window: kill fires once the guide or state
//    was rewritten during the victim run (post-write phase); same
//    convergence contract. Blackbox timing is approximate — the enforced
//    contract is convergence after a kill in this window, and the trigger is
//    logged either way.
// ---------------------------------------------------------------------------

func TestDocEngineChaosKillDuringStateSave(t *testing.T) {
	chaosScrubEnv(t)
	bin := chaosBuildBinary(t)

	var sb *harness.Sandbox
	var head string
	var baseline time.Duration
	preMissing := false
	killed := false
	firedOnWrite := false
	// Baseline on a throwaway repo (measures T only); every victim runs on a
	// FRESH repo, so the guide write + state save genuinely happen during
	// the victim run (a converged rerun would skip the guide write when
	// bytes are identical, making the window unobservable).
	for attempt := 0; attempt < 3 && !killed; attempt++ {
		mSb, mHead := chaosSeedRepo(t, 250)
		start := time.Now()
		mOut, merr := chaosRunBinary(t, bin, mSb.Root, nil, "--commit", mHead, "--write", "--force", "--no-llm")
		baseline = time.Since(start)
		if merr != nil {
			t.Fatalf("baseline run failed: %v\n%s", merr, mOut)
		}
		_ = mSb

		sb, head = chaosSeedRepo(t, 250)
		victimStart := time.Now()
		// Fallback deadline near the tail of the measured baseline keeps the
		// kill late even if no write is ever observed.
		deadline := baseline * 9 / 10
		if deadline < 800*time.Millisecond {
			deadline = 800 * time.Millisecond
		}
		if deadline > 10*time.Second {
			deadline = 10 * time.Second
		}
		killed, _ = chaosKillRun(t, bin, sb.Root, nil,
			[]string{"--commit", head, "--write", "--force", "--no-llm"},
			func() bool {
				if time.Since(victimStart) < 300*time.Millisecond {
					return false
				}
				if chaosGuideWrittenAfter(sb, victimStart) || chaosStateWrittenAfter(sb, victimStart) {
					firedOnWrite = true
					return true
				}
				return false
			}, deadline)
		t.Logf("attempt %d: killed=%v firedOnWrite=%v (baseline %s, deadline %s)", attempt, killed, firedOnWrite, baseline, deadline)
	}
	if !killed {
		t.Fatalf("could not land a kill in the state-save window in 3 attempts")
	}
	if !firedOnWrite {
		t.Logf("kill landed on the deadline fallback, not on an observed write (approximate phase targeting)")
	}

	// On a fresh repo the kill may land before the first write: then there
	// is no old revision. Otherwise the atomic rename guarantees post-kill
	// bytes are already the complete new revision, so the converged rerun
	// must be byte-identical to them.
	torn, err := os.ReadFile(sb.Path("docs", "guide.md"))
	if err != nil {
		t.Logf("kill landed before the first guide write (no old revision to compare)")
		preMissing = true
	}
	chaosAssertConverged(t, sb, head)
	if !preMissing {
		if after := sb.ReadFile("docs/guide.md"); after != string(torn) {
			t.Errorf("post-kill bytes differ from converged bytes (%d vs %dB): torn write or nondeterministic render", len(torn), len(after))
		}
	}
}

// ---------------------------------------------------------------------------
// 3. Kill during the first JSON→SQLite migration: seed a legacy
//    docs_state.json, kill the first (migrating) run early, rerun must
//    converge; the SQLite state loads and the JSON source survives.
// ---------------------------------------------------------------------------

func TestDocEngineChaosKillDuringMigration(t *testing.T) {
	chaosScrubEnv(t)
	// Default backend (SQLite with JSON auto-migration) — ensure no override
	// leaks in from the environment.
	if v, ok := os.LookupEnv("GMB_DOC_STATE"); ok {
		_ = os.Unsetenv("GMB_DOC_STATE")
		t.Cleanup(func() { _ = os.Setenv("GMB_DOC_STATE", v) })
	}
	bin := chaosBuildBinary(t)

	var sb *harness.Sandbox
	var head string
	killed := false
	for attempt := 0; attempt < 3 && !killed; attempt++ {
		sb, head = chaosSeedRepo(t, 150)
		// Legacy v1-era JSON state: the first SQLite open must migrate it.
		sb.WriteFile(".glassmarble/docs_state.json", `{"schema_version":1,"last_commit":"0000000000000000000000000000000000000000","documents":{}}`)
		victimStart := time.Now()
		killed, _ = chaosKillRun(t, bin, sb.Root, nil,
			[]string{"--commit", head, "--write", "--force", "--no-llm"},
			func() bool {
				if time.Since(victimStart) < 200*time.Millisecond {
					return false
				}
				// Migration happens on first state open, i.e. early: any
				// state-file activity is enough to fire.
				if _, err := os.Stat(sb.Path(".glassmarble", "docs_state.db")); err == nil {
					return true
				}
				return chaosLocksObserved(sb)
			}, 2*time.Second)
		t.Logf("attempt %d: killed=%v", attempt, killed)
	}
	if !killed {
		t.Fatalf("could not land a kill during migration in 3 attempts")
	}

	chaosAssertConverged(t, sb, head)
	if _, err := os.Stat(sb.Path(".glassmarble", "docs_state.db")); err != nil {
		t.Errorf("migrated SQLite state missing after kill+rerun: %v", err)
	}
	if _, err := os.Stat(sb.Path(".glassmarble", "docs_state.json")); err != nil {
		t.Errorf("migration must preserve the JSON source file: %v", err)
	}
}

// ---------------------------------------------------------------------------
// 4. Corrupt AKG: garbage akg.json degrades gracefully — doc commands and
//    analyze either tolerate it (fallback/rebuild) or fail with a clear
//    parse error; nothing panics.
// ---------------------------------------------------------------------------

func TestDocEngineChaosCorruptAKG(t *testing.T) {
	chaosScrubEnv(t)
	t.Setenv("GMB_DOC_STATE", "json")
	sb, _ := docShopSandbox(t, false)
	docWriteConfig(t, sb, -1)
	head := sb.GitCommit("add docs config")

	sb.WriteFile(".glassmarble/akg.json", "{this is not valid json{{{{garbage\x00\x01\x02")

	// Graph-less doc path must not panic on a corrupt AKG.
	out := docRunWrite(t, sb, head)
	if strings.Contains(out, "panic:") {
		t.Fatalf("doc run panicked on corrupt AKG:\n%s", out)
	}
	mustContain(t, sb.ReadFile("docs/guide.md"), "gmb:begin:overview")
	gmbWant(t, sb, []string{"fresh"}, "doc", "check")

	// Analyze loads the AKG: observed behavior is tolerance (fallback path),
	// but a clean parse error is equally acceptable. Panics are not.
	analyzeOut, err := gmbErr(t, sb, "analyze", "--no-llm")
	if strings.Contains(analyzeOut, "panic:") {
		t.Fatalf("analyze panicked on corrupt AKG:\n%s", analyzeOut)
	}
	if err == nil {
		t.Logf("analyze tolerated the corrupt AKG (fallback path)")
		return
	}
	joined := strings.ToLower(analyzeOut)
	if !strings.Contains(joined, "akg.json") && !strings.Contains(joined, "parse") &&
		!strings.Contains(joined, "invalid character") && !strings.Contains(joined, "corrupt") &&
		!strings.Contains(joined, "restore") {
		t.Errorf("analyze on corrupt AKG gave no actionable error:\n%s", analyzeOut)
	}
}

// ---------------------------------------------------------------------------
// 5. Git lock contention: holding .git/index.lock must not crash or hang the
//    run (doc git access is read-only); contention is proven genuine by a
//    blocked writer first.
// ---------------------------------------------------------------------------

func TestDocEngineChaosGitLockContention(t *testing.T) {
	chaosScrubEnv(t)
	t.Setenv("GMB_DOC_STATE", "json")
	sb, _ := docShopSandbox(t, false)
	docWriteConfig(t, sb, -1)
	head := sb.GitCommit("add docs config")

	lockPath := sb.Path(".git", "index.lock")
	if _, err := os.Stat(lockPath); err == nil {
		t.Fatalf("pre-existing index.lock would invalidate the test")
	}
	lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatalf("holding index.lock: %v", err)
	}
	t.Cleanup(func() {
		_ = lock.Close()
		_ = os.Remove(lockPath)
	})

	// Prove the contention is real: a writer must be refused while we hold it.
	wcmd := exec.Command("git", "commit", "--allow-empty", "-m", "lock probe")
	wcmd.Dir = sb.Root
	wcmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if raw, werr := wcmd.CombinedOutput(); werr == nil {
		t.Fatalf("expected git writer to block on held index.lock, it succeeded:\n%s", raw)
	} else if !strings.Contains(strings.ToLower(string(raw)), "index.lock") {
		t.Fatalf("git writer failed for an unrelated reason (contention unproven):\n%v\n%s", werr, raw)
	}

	// The doc run must handle the locked index gracefully (exit 0 here: all
	// engine git access is read-only).
	start := time.Now()
	out := docRunWrite(t, sb, head)
	elapsed := time.Since(start)
	t.Logf("doc run under held index.lock took %s", elapsed)
	if strings.Contains(out, "panic:") {
		t.Fatalf("doc run panicked under lock contention:\n%s", out)
	}
	if elapsed > 120*time.Second {
		t.Errorf("doc run under lock contention took %s (hang tripwire 120s)", elapsed)
	}
	mustContain(t, sb.ReadFile("docs/guide.md"), "gmb:begin:overview")
	chaosAssertConverged(t, sb, head)
}

// ---------------------------------------------------------------------------
// 6. OOM-ish guard: a 500-file repo full run in a real process completes
//    with no goroutine blowup (no panic, no goroutine dump in output).
// ---------------------------------------------------------------------------

func TestDocEngineChaosOOMGuard500(t *testing.T) {
	chaosScrubEnv(t)
	bin := chaosBuildBinary(t)
	sb, head := chaosSeedRepo(t, 500)

	start := time.Now()
	out, err := chaosRunBinary(t, bin, sb.Root, nil, "--commit", head, "--write", "--force", "--no-llm")
	elapsed := time.Since(start)
	t.Logf("500-file full run took %s", elapsed)
	if err != nil {
		t.Fatalf("500-file run failed: %v\n%s", err, out)
	}
	for _, frag := range []string{"panic:", "goroutine ", "out of memory", "fatal error"} {
		if strings.Contains(out, frag) {
			t.Errorf("500-file run shows blowup marker %q:\n%s", frag, out)
		}
	}
	if elapsed > 180*time.Second {
		t.Errorf("500-file run took %s (tripwire 180s)", elapsed)
	}
	if !sb.Exists("docs/guide.md") {
		t.Fatalf("500-file run produced no docs/guide.md")
	}
	chaosAssertConverged(t, sb, head)
}

// ---------------------------------------------------------------------------
// 7. Power-loss loop: SIGKILL x3 on one repo, rerun convergence + state load
//    after every kill. Kill deadlines self-calibrate to the measured rerun
//    duration R (forced reruns redo less work than first runs).
//    NOTE: may exceed ~60s (baseline + calibration + 3 kill/rerun cycles).
// ---------------------------------------------------------------------------

func TestDocEngineChaosPowerLossLoop(t *testing.T) {
	chaosScrubEnv(t)
	bin := chaosBuildBinary(t)
	sb, head := chaosSeedRepo(t, 250)

	out, err := chaosRunBinary(t, bin, sb.Root, nil, "--commit", head, "--write", "--force", "--no-llm")
	if err != nil {
		t.Fatalf("baseline run failed: %v\n%s", err, out)
	}
	rStart := time.Now()
	rOut, rerr := chaosRunBinary(t, bin, sb.Root, nil, "--commit", head, "--write", "--force", "--no-llm")
	rerunT := time.Since(rStart)
	if rerr != nil {
		t.Fatalf("calibration rerun failed: %v\n%s", rerr, rOut)
	}
	earlyDL := rerunT / 2
	if earlyDL < 400*time.Millisecond {
		earlyDL = 400 * time.Millisecond
	}
	lateDL := rerunT * 9 / 10
	if lateDL < 500*time.Millisecond {
		lateDL = 500 * time.Millisecond
	}
	t.Logf("calibrated rerun: %s (early deadline %s, late deadline %s)", rerunT, earlyDL, lateDL)

	kills := 0
	for i := 0; i < 3; i++ {
		// Stale sidecars from the previous iteration would fire the lock
		// trigger at startup; the identical-bytes guide skip also makes the
		// late trigger depend on state (re-saved every run) rather than the
		// guide.
		chaosRemoveStaleLocks(sb)
		pre, err := os.ReadFile(sb.Path("docs", "guide.md"))
		if err != nil {
			t.Fatalf("iteration %d: reading pre-kill guide: %v", i, err)
		}
		victimStart := time.Now()
		condHit := false
		var fire func() bool
		var dl time.Duration
		if i%2 == 0 {
			dl = earlyDL
			fire = func() bool {
				if time.Since(victimStart) < 200*time.Millisecond {
					return false
				}
				if chaosLocksObserved(sb) {
					condHit = true
					return true
				}
				return false
			}
		} else {
			dl = lateDL
			fire = func() bool {
				if time.Since(victimStart) < 200*time.Millisecond {
					return false
				}
				if chaosStateWrittenAfter(sb, victimStart) {
					condHit = true
					return true
				}
				return false
			}
		}
		killed, _ := chaosKillRun(t, bin, sb.Root, nil,
			[]string{"--commit", head, "--write", "--force", "--no-llm"},
			fire, dl)
		t.Logf("iteration %d: killed=%v condHit=%v", i, killed, condHit)
		if killed {
			kills++
		} else {
			// Missed the window (run finished first): still a completed
			// sync, so convergence below must hold trivially.
			t.Logf("iteration %d: victim finished before the kill (no fault injected this round)", i)
		}
		post, err := os.ReadFile(sb.Path("docs", "guide.md"))
		if err != nil {
			t.Fatalf("iteration %d: reading post-kill guide: %v", i, err)
		}
		chaosAssertConverged(t, sb, head)
		after := sb.ReadFile("docs/guide.md")
		if !bytes.Equal(post, pre) && string(post) != after {
			t.Errorf("iteration %d: torn write (matches neither pre- nor post-rerun bytes)", i)
		}
	}
	if kills == 0 {
		t.Errorf("no SIGKILL landed in 3 power-loss iterations (all victims finished first)")
	} else {
		t.Logf("power-loss loop: %d/3 kills injected, repo converged every time", kills)
	}
}
