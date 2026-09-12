package edgecases_test

// Edge-case suite for the docs-engine feature (internal/doc_engine + `gmb doc`).
//
// Covers hostile inputs through the PUBLIC CLI surface only (`gmb doc`);
// production code is untouched. The `doc` update path is non-fatal by design
// (P5): engine failures surface as warnings with exit 0, so most tests assert
// graceful success rather than hard errors. No t.Parallel() anywhere: the
// harness mutates process-global state (os.Stdout, CWD) per invocation.
//
// Heavy variants are OUT of scope here (nightly owns them): 50k-file repos,
// real `kill -9` crash recovery, ENOSPC fault injection, and networked
// (--check-external) paths. See TestDocEngineEdgeDiskFull for the documented
// substitution.

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	docengine "github.com/Syamchand123/GlassMarble/internal/doc_engine"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/storage"
	"github.com/Syamchand123/GlassMarble/tests/harness"
)

// docEngSpec renders a minimal valid docs.yaml with one document and one
// managed section scoped to scopeGlob.
func docEngSpec(scopeGlob string) string {
	return `version: 1
docs_dir: docs
documents:
  - id: guide
    target: docs/guide.md
    title: Guide
    purpose: Edge-case fixture document.
    audience: Developers
    scope:
      paths: ["` + scopeGlob + `"]
    sections:
      - id: overview
        title: Overview
        instruction: Document the current state.
`
}

// docEngWriteConfig writes docs.yaml with the given scope glob.
func docEngWriteConfig(t *testing.T, sb *harness.Sandbox, scopeGlob string) {
	t.Helper()
	sb.WriteFile(".glassmarble/docs.yaml", docEngSpec(scopeGlob))
}

// docEngInitGit creates a git repo on main with an initial commit.
func docEngInitGit(t *testing.T, sb *harness.Sandbox) {
	t.Helper()
	sb.RequireGit()
	sb.GitInit()
}

// docEngRun executes the deterministic offline update path. --force bypasses
// fast-bail and the comment-only skip; --branch-policy any permits writes on
// any branch; --no-llm keeps the run offline and deterministic.
func docEngRun(t *testing.T, sb *harness.Sandbox, extra ...string) (string, error) {
	t.Helper()
	args := []string{"doc", "--no-llm", "--force", "--branch-policy", "any"}
	args = append(args, extra...)
	return harness.RunGmb(t, sb, args...)
}

// docEngMustRun asserts the update path exits 0 (P5 non-fatal contract).
func docEngMustRun(t *testing.T, sb *harness.Sandbox, extra ...string) string {
	t.Helper()
	out, err := docEngRun(t, sb, extra...)
	if err != nil {
		t.Fatalf("gmb doc update path failed (must be non-fatal, exit 0): %v\n--- output ---\n%s", err, out)
	}
	return out
}

// TestDocEngineEdgeEmptyRepo: a committed repo with no .go files must not
// error; the engine scaffolds the target doc deterministically.
func TestDocEngineEdgeEmptyRepo(t *testing.T) {
	sb := harness.NewSandbox(t)
	docEngInitGit(t, sb)
	sb.GitCommitFiles("docs: empty fixture", map[string]string{
		"README.md": "# Nothing here yet\n",
	})
	docEngWriteConfig(t, sb, "**")
	docEngMustRun(t, sb)
	if !sb.Exists("docs/guide.md") {
		t.Errorf("expected docs/guide.md to be scaffolded on empty repo")
	}
}

// TestDocEngineEdgeNoGitDir: without a git work tree the engine degrades
// (unknown branch, zero freshness) but still exits 0.
func TestDocEngineEdgeNoGitDir(t *testing.T) {
	sb := harness.NewSandbox(t)
	sb.WriteFile("main.go", "package main\n\nfunc main() {}\n")
	docEngWriteConfig(t, sb, "**")
	docEngMustRun(t, sb)
}

// TestDocEngineEdgeCRLF: CRLF line endings in both source and seed markdown
// must not break parsing (the patcher normalizes CRLF to LF).
func TestDocEngineEdgeCRLF(t *testing.T) {
	sb := harness.NewSandbox(t)
	docEngInitGit(t, sb)
	sb.GitCommitFiles("feat: crlf fixture", map[string]string{
		"main.go": "package main\r\n\r\nfunc main() {\r\n}\r\n",
	})
	docEngWriteConfig(t, sb, "**")
	sb.WriteFile("docs/guide.md", "# Guide\r\n\r\nSome intro.\r\n")
	docEngMustRun(t, sb)
}

// TestDocEngineEdgeUnicodeEmoji: unicode identifiers and emoji prose must
// survive the pipeline byte-intact (no crash, no mojibake error).
func TestDocEngineEdgeUnicodeEmoji(t *testing.T) {
	sb := harness.NewSandbox(t)
	docEngInitGit(t, sb)
	sb.GitCommitFiles("feat: unicode fixture", map[string]string{
		"café.go": "package main\n\n// Café greets with feeling ☕🎉\nfunc Café() string { return \"héllo 🌍\" }\n",
	})
	docEngWriteConfig(t, sb, "**")
	sb.WriteFile("docs/guide.md", "# Guide\n\nHuman intro with emoji 🚀 and accents héllo.\n")
	out := docEngMustRun(t, sb)
	_ = out
	body := sb.ReadFile("docs/guide.md")
	if !strings.Contains(body, "🚀") {
		t.Errorf("human emoji prose was destroyed by the run:\n%s", body)
	}
}

// TestDocEngineEdgeOneMBFile: a single ~1MB source file must be tolerated
// (parsed or skipped, but never a crash and exit 0).
func TestDocEngineEdgeOneMBFile(t *testing.T) {
	sb := harness.NewSandbox(t)
	docEngInitGit(t, sb)
	var b strings.Builder
	b.WriteString("package big\n\n")
	// ~1MB of small valid funcs; deterministic content.
	for i := 0; i < 22000; i++ {
		b.WriteString("func F000000() string { return \"ok\" }\n")
		if b.Len() >= 1<<20 {
			break
		}
	}
	sb.GitCommitFiles("feat: big fixture", map[string]string{"big/big.go": b.String()})
	docEngWriteConfig(t, sb, "big/**")
	docEngMustRun(t, sb)
}

// TestDocEngineEdgeBinaryInScope: a binary blob inside the doc scope must be
// skipped gracefully (exit 0, target still rendered).
func TestDocEngineEdgeBinaryInScope(t *testing.T) {
	sb := harness.NewSandbox(t)
	docEngInitGit(t, sb)
	raw := make([]byte, 4096)
	for i := range raw {
		raw[i] = byte((i * 31) % 256)
	}
	sb.WriteFile("assets/blob.bin", string(raw))
	sb.WriteFile("main.go", "package main\n\nfunc main() {}\n")
	sb.MustGit("add", "-A")
	sb.MustGit("commit", "-q", "-m", "feat: binary fixture")
	docEngWriteConfig(t, sb, "**")
	docEngMustRun(t, sb)
	if !sb.Exists("docs/guide.md") {
		t.Errorf("expected docs/guide.md despite binary file in scope")
	}
}

// TestDocEngineEdgeSymlinkLoop: a symlink cycle inside the scope must not
// hang or crash the run. Skipped where symlinks cannot be created.
func TestDocEngineEdgeSymlinkLoop(t *testing.T) {
	sb := harness.NewSandbox(t)
	docEngInitGit(t, sb)
	sb.GitCommitFiles("feat: loop fixture", map[string]string{
		"loop/main.go": "package main\n\nfunc main() {}\n",
	})
	a := filepath.Join(sb.Root, "loop", "a.link")
	b := filepath.Join(sb.Root, "loop", "b.link")
	if err := os.Symlink(b, a); err != nil {
		t.Skipf("symlinks not supported on this platform: %v", err)
	}
	if err := os.Symlink(a, b); err != nil {
		t.Skipf("symlinks not supported on this platform: %v", err)
	}
	docEngWriteConfig(t, sb, "loop/**")
	docEngMustRun(t, sb)
}

// TestDocEngineEdgeUnclosedAnchor: a begin marker without an end marker is
// recovered as human territory (never modified, never an error).
func TestDocEngineEdgeUnclosedAnchor(t *testing.T) {
	sb := harness.NewSandbox(t)
	docEngInitGit(t, sb)
	sb.GitCommitFiles("feat: anchor fixture", map[string]string{
		"main.go": "package main\n\nfunc main() {}\n",
	})
	docEngWriteConfig(t, sb, "**")
	const human = "# Guide\n\n<!-- gmb:begin:overview -->\n\nHuman draft with no end marker.\n"
	sb.WriteFile("docs/guide.md", human)
	docEngMustRun(t, sb)
	body := sb.ReadFile("docs/guide.md")
	if !strings.Contains(body, "Human draft with no end marker.") {
		t.Errorf("unclosed-anchor human content was modified:\n%s", body)
	}
}

// TestDocEngineEdgeNestedDuplicatedAnchors: nested begin markers and
// duplicated ids resolve without crashing. Managed-zone BODIES are engine
// territory and are re-rendered (so text inside them is legitimately
// replaced); human text OUTSIDE zones must survive byte-intact and the
// anchor structure must stay well-formed.
func TestDocEngineEdgeNestedDuplicatedAnchors(t *testing.T) {
	sb := harness.NewSandbox(t)
	docEngInitGit(t, sb)
	sb.GitCommitFiles("feat: nested fixture", map[string]string{
		"main.go": "package main\n\nfunc main() {}\n",
	})
	docEngWriteConfig(t, sb, "**")
	sb.WriteFile("docs/guide.md", "# Guide\n\nHUMAN-SENTINEL-KEEP-ME\n\n"+
		"<!-- gmb:begin:overview -->\n\nFirst zone.\n\n"+
		"<!-- gmb:begin:overview -->\n\nNested impostor (content, not a zone).\n\n"+
		"<!-- gmb:end:overview -->\n\n"+
		"<!-- gmb:begin:overview -->\n\nDuplicate zone.\n\n<!-- gmb:end:overview -->\n")
	docEngMustRun(t, sb)
	body := sb.ReadFile("docs/guide.md")
	if !strings.Contains(body, "HUMAN-SENTINEL-KEEP-ME") {
		t.Errorf("human content outside zones was destroyed:\n%s", body)
	}
	if strings.Count(body, "<!-- gmb:begin:overview -->") < 2 {
		t.Errorf("anchor structure collapsed (want both zone markers intact):\n%s", body)
	}
}

// TestDocEngineEdgeAnchorInFencedCode: gmb:-looking text inside a fenced code
// block is structurally invisible to the parser and must survive verbatim.
func TestDocEngineEdgeAnchorInFencedCode(t *testing.T) {
	sb := harness.NewSandbox(t)
	docEngInitGit(t, sb)
	sb.GitCommitFiles("feat: fence fixture", map[string]string{
		"main.go": "package main\n\nfunc main() {}\n",
	})
	docEngWriteConfig(t, sb, "**")
	const fence = "```go\n// <!-- gmb:begin:overview -->\nfunc Example() {}\n// <!-- gmb:end:overview -->\n```\n"
	sb.WriteFile("docs/guide.md", "# Guide\n\n"+fence)
	docEngMustRun(t, sb)
	body := sb.ReadFile("docs/guide.md")
	if !strings.Contains(body, "// <!-- gmb:begin:overview -->") {
		t.Errorf("fenced code anchor impostor was treated as a real zone:\n%s", body)
	}
}

// TestDocEngineEdgeDocsYAMLHostile: malformed, empty, and unknown-field
// configs must stay on the non-fatal update path (exit 0). Malformed/empty
// surface as warnings; unknown fields are ignored by the loader.
func TestDocEngineEdgeDocsYAMLHostile(t *testing.T) {
	t.Run("malformed", func(t *testing.T) {
		sb := harness.NewSandbox(t)
		docEngInitGit(t, sb)
		sb.GitCommitFiles("feat: yaml fixture", map[string]string{
			"main.go": "package main\n\nfunc main() {}\n",
		})
		sb.WriteFile(".glassmarble/docs.yaml", "version: [unclosed\n  documents: {{{{\n")
		out, err := docEngRun(t, sb)
		if err != nil {
			t.Fatalf("malformed docs.yaml must be non-fatal (exit 0): %v\n%s", err, out)
		}
		if !strings.Contains(out, "warning") {
			t.Errorf("expected a warning for malformed docs.yaml:\n%s", out)
		}
	})
	t.Run("empty", func(t *testing.T) {
		sb := harness.NewSandbox(t)
		docEngInitGit(t, sb)
		sb.GitCommitFiles("feat: yaml fixture", map[string]string{
			"main.go": "package main\n\nfunc main() {}\n",
		})
		sb.WriteFile(".glassmarble/docs.yaml", "")
		out, err := docEngRun(t, sb)
		if err != nil {
			t.Fatalf("empty docs.yaml must be non-fatal (exit 0): %v\n%s", err, out)
		}
		if !strings.Contains(out, "warning") {
			t.Errorf("expected a warning for empty docs.yaml:\n%s", out)
		}
	})
	t.Run("unknown-fields", func(t *testing.T) {
		sb := harness.NewSandbox(t)
		docEngInitGit(t, sb)
		sb.GitCommitFiles("feat: yaml fixture", map[string]string{
			"main.go": "package main\n\nfunc main() {}\n",
		})
		sb.WriteFile(".glassmarble/docs.yaml",
			"bogus_top_level: 123\nfuture_stuff:\n  whatever: true\n"+docEngSpec("**"))
		docEngMustRun(t, sb)
		if !sb.Exists("docs/guide.md") {
			t.Errorf("unknown fields must be ignored, but no doc was rendered")
		}
	})
}

// TestDocEngineEdgeWeirdSectionID: a section id outside the URL-safe alphabet
// is rejected by the config validator; the update path still exits 0 and
// writes nothing.
func TestDocEngineEdgeWeirdSectionID(t *testing.T) {
	sb := harness.NewSandbox(t)
	docEngInitGit(t, sb)
	sb.GitCommitFiles("feat: id fixture", map[string]string{
		"main.go": "package main\n\nfunc main() {}\n",
	})
	sb.WriteFile(".glassmarble/docs.yaml", `version: 1
docs_dir: docs
documents:
  - id: guide
    target: docs/guide.md
    title: Guide
    purpose: Fixture.
    audience: Developers
    scope:
      paths: ["**"]
    sections:
      - id: "Bad_ID! with spaces"
        title: Bad
        instruction: Document the state.
`)
	out, err := docEngRun(t, sb)
	if err != nil {
		t.Fatalf("invalid section id must be non-fatal (exit 0): %v\n%s", err, out)
	}
	if !strings.Contains(out, "warning") {
		t.Errorf("expected a warning for invalid section id:\n%s", out)
	}
	if sb.Exists("docs/guide.md") {
		t.Errorf("no document may be written when config validation fails")
	}
}

// TestDocEngineEdgeConcurrentRuns: four parallel `doc` runs over the same
// repo must all complete, converge to identical bytes, and leave a loadable
// state (advisory locking / SQLite WAL absorb the contention; the engine is
// non-fatal so contention degrades to warnings, never corruption).
//
// NOTE: this drives doc_engine.Run directly (not the in-process CLI runner)
// because the CLI runner swaps process-global os.Stdout/CWD and is unsafe
// under goroutines; the engine API itself is the public surface under test.
func TestDocEngineEdgeConcurrentRuns(t *testing.T) {
	sb := harness.NewSandbox(t)
	docEngInitGit(t, sb)
	head := sb.GitCommitFiles("feat: concurrent fixture", map[string]string{
		"main.go": "package main\n\nfunc main() {}\n",
	})
	docEngWriteConfig(t, sb, "**")
	docEngMustRun(t, sb, "--commit", head)
	ref := sb.ReadFile("docs/guide.md")

	const workers = 4
	results := make([]docengine.RunResult, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i] = docengine.Run(sb.Root, docengine.RunOptions{
				CommitHash:   head,
				NoLLM:        true,
				Force:        true,
				BranchPolicy: "any",
				Out:          io.Discard,
			})
		}(i)
	}
	wg.Wait()
	// Contention handling (SQLite open retry + busy_timeout + flock): all
	// workers must fully succeed — losers no longer surface SQLITE_BUSY as
	// a critical error. Every worker must complete without hanging or
	// panicking, converge to identical bytes, and leave a loadable state.
	succeeded := 0
	for i, r := range results {
		if r.Err != nil {
			t.Errorf("concurrent run %d failed despite contention retry: %v", i, r.Err)
			continue
		}
		succeeded++
	}
	if succeeded == 0 {
		t.Errorf("all %d concurrent runs failed; at least one must succeed", workers)
	}
	if got := sb.ReadFile("docs/guide.md"); got != ref {
		t.Errorf("concurrent runs diverged from sequential reference:\n--- ref ---\n%s\n--- got ---\n%s", ref, got)
	}
	sm := storage.NewStateManager(sb.GmDir)
	if _, err := sm.Load(); err != nil {
		t.Fatalf("state corrupted by concurrent runs: %v", err)
	}
}

// TestDocEngineEdgeReadOnlyDocsDir: an unwritable docs tree must degrade to a
// warning on the update path (P5 non-fatal), never a hard failure. On
// platforms where chmod is advisory the write may simply succeed; either way
// exit 0 is the contract under test.
func TestDocEngineEdgeReadOnlyDocsDir(t *testing.T) {
	sb := harness.NewSandbox(t)
	docEngInitGit(t, sb)
	head := sb.GitCommitFiles("feat: readonly fixture", map[string]string{
		"main.go": "package main\n\nfunc main() {}\n",
	})
	docEngWriteConfig(t, sb, "**")
	docEngMustRun(t, sb, "--commit", head)

	target := sb.Path("docs", "guide.md")
	docsDir := sb.Path("docs")
	t.Cleanup(func() {
		_ = os.Chmod(target, 0o644)
		_ = os.Chmod(docsDir, 0o755)
	})
	_ = os.Chmod(target, 0o444)
	_ = os.Chmod(docsDir, 0o555)

	out, err := docEngRun(t, sb, "--commit", head)
	if err != nil {
		t.Fatalf("read-only docs dir must be non-fatal (exit 0): %v\n%s", err, out)
	}
	if !sb.Exists("docs/guide.md") {
		t.Errorf("target doc vanished under read-only run")
	}
}

// TestDocEngineEdgeTwoThousandFiles: a 2000-file synthetic repo exercises the
// walker/vocabulary caps on a fast, generated corpus (no network, no sleeps).
func TestDocEngineEdgeTwoThousandFiles(t *testing.T) {
	sb := harness.NewSandbox(t)
	docEngInitGit(t, sb)
	files := make(map[string]string, 2000)
	for i := 0; i < 2000; i++ {
		dir := "bench/d" + twoDigits(i/100)
		files[dir+"/f"+fourDigits(i)+".go"] = "package d" + twoDigits(i/100) +
			"\n\nfunc F" + fourDigits(i) + "() string { return \"ok\" }\n"
	}
	sb.GitCommitFiles("feat: 2000 generated stubs", files)
	docEngWriteConfig(t, sb, "bench/**")
	docEngMustRun(t, sb)
	if !sb.Exists("docs/guide.md") {
		t.Errorf("expected docs/guide.md on 2000-file repo")
	}
}

// TestDocEngineEdgeDeeplyNestedScopes: 12 levels of nesting with a scoped doc
// must resolve and render.
func TestDocEngineEdgeDeeplyNestedScopes(t *testing.T) {
	sb := harness.NewSandbox(t)
	docEngInitGit(t, sb)
	deep := "deep/l1/l2/l3/l4/l5/l6/l7/l8/l9/l10/l11/main.go"
	sb.GitCommitFiles("feat: deep fixture", map[string]string{
		deep: "package main\n\nfunc Deep() {}\n",
	})
	docEngWriteConfig(t, sb, "deep/**")
	docEngMustRun(t, sb)
	if !sb.Exists("docs/guide.md") {
		t.Errorf("expected docs/guide.md for deeply nested scope")
	}
}

// TestDocEngineEdgeCorruptedState: a garbage docs_state.json (legacy JSON
// backend) must be a graceful warning + exit 0, and after an operator repair
// (valid JSON) the next run succeeds — the documented recovery path.
func TestDocEngineEdgeCorruptedState(t *testing.T) {
	t.Setenv("GMB_DOC_STATE", "json")
	sb := harness.NewSandbox(t)
	docEngInitGit(t, sb)
	head := sb.GitCommitFiles("feat: state fixture", map[string]string{
		"main.go": "package main\n\nfunc main() {}\n",
	})
	docEngWriteConfig(t, sb, "**")
	docEngMustRun(t, sb, "--commit", head)

	sb.WriteFile(".glassmarble/docs_state.json", "{ this is not json \x00\x01")
	out, err := docEngRun(t, sb, "--commit", head)
	if err != nil {
		t.Fatalf("corrupted docs_state.json must be non-fatal (exit 0): %v\n%s", err, out)
	}
	if !strings.Contains(out, "docs_state.json") {
		t.Errorf("expected the warning to name docs_state.json:\n%s", out)
	}

	// Operator repair: replace garbage with a valid empty state.
	sb.WriteFile(".glassmarble/docs_state.json", `{"schema_version":1,"documents":{}}`)
	docEngMustRun(t, sb, "--commit", head)
	after := sb.ReadFile(".glassmarble/docs_state.json")
	if !strings.Contains(after, `"schema_version"`) {
		t.Errorf("state did not recover after repair:\n%s", after)
	}
}

// TestDocEngineEdgeDiskFull documents the EXCLUDED true-ENOSPC fault
// injection (nightly owns real disk-full simulation via filesystem fault
// hooks) and instead pins the adjacent graceful-error contract: an
// unwritable atomic-write target returns an error instead of panicking, and
// the existing file is left untouched.
func TestDocEngineEdgeDiskFull(t *testing.T) {
	// EXCLUDED (nightly): real ENOSPC / quota-exceeded injection and power-loss
	// simulation. Rationale: requires privileged filesystem fault hooks, is
	// timing-sensitive, and cannot run hermetically on CI.
	sb := harness.NewSandbox(t)
	blocker := sb.WriteFile("blocker", "I am a file, not a directory\n")
	target := filepath.Join(blocker, "guide.md") // parent is a file: unwritable
	changed, err := storage.AtomicWriteFile(target, []byte("# Guide\n"))
	if err == nil {
		t.Fatalf("expected a graceful error for an unwritable target, got changed=%v", changed)
	}
	if changed {
		t.Errorf("failed write must report changed=false")
	}
	if sb.Exists("blocker/guide.md") {
		t.Errorf("failed write must not create partial output")
	}
}

func twoDigits(n int) string {
	s := "00" + itoa(n)
	return s[len(s)-2:]
}

func fourDigits(n int) string {
	s := "0000" + itoa(n)
	return s[len(s)-4:]
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [8]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
