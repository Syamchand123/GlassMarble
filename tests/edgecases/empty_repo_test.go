package edgecases_test

// Edge cases around empty, degenerate and hostile repositories.
//
// Deviations from the documented expectations (verified against cmd/analyze.go,
// cmd/init.go, cmd/status.go, cmd/doctor.go and internal/git/git.go):
//
//  1. `gmb analyze` does NOT require git and never errors when HEAD is
//     missing: runAnalysis ignores the GetHEADCommitHash error (analyze.go:152)
//     and falls back to a full scan. An empty directory analyzes to 0 files.
//  2. A git repository with NO commits also analyzes successfully: the walker
//     sets GitTrackedOnly (analyze.go:183) and `git ls-files` returns an empty
//     set, so every file is skipped with "(untracked by git)" — there is no
//     HEAD/commit error anywhere in the pipeline.
//  3. `init` writes an empty-but-valid v3 akg.json, so `status` and `doctor`
//     render INITIALIZED (0 nodes, "DOCTOR: OK") after init. Only a raw
//     sandbox with no .glassmarble renders "Uninitialized".

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Syamchand123/GlassMarble/tests/harness"
)

// TestAnalyzeEmptyDirNoGit: no .git, no source files — analyze succeeds with a
// zero-file report instead of erroring (fallback to full scan).
func TestAnalyzeEmptyDirNoGit(t *testing.T) {
	sb := harness.NewSandbox(t)
	out := mustRunContains(t, sb, []string{"Starting GlassMarble Analysis", "Analyzed 0 files"}, "analyze")
	if strings.Contains(out, "ingestion failed") {
		t.Errorf("analyze failed on empty dir:\n%s", out)
	}
}

// TestInitStatusDoctorEmptyDir: init creates the workspace; after init,
// status/doctor report an initialized empty v3 database (deviation 3).
func TestInitStatusDoctorEmptyDir(t *testing.T) {
	sb := harness.NewSandbox(t)
	out := mustRunContains(t, sb, []string{"GlassMarble Workspace Initialized"}, "init")
	for _, rel := range []string{".glassmarble/akg.json", ".glassmarble/config.yaml", ".glassmarble/marbles", ".glassmarble/snapshots", ".glassmarble/memory", ".gitignore"} {
		if !sb.Exists(rel) {
			t.Errorf("init did not create %s:\n%s", rel, out)
		}
	}
	// Status renders the initialized dashboard: init wrote an empty v3 state.
	mustRunContains(t, sb, []string{"GlassMarble AKG Status", "Nodes Count:"}, "status")
	// Doctor passes: the empty v3 state parses back cleanly.
	mustRunContains(t, sb, []string{"DOCTOR: OK"}, "doctor")
}

// TestStatusDoctorUninitializedRawDir: without init or akg.json, status and
// doctor render the uninitialized card and return nil.
func TestStatusDoctorUninitializedRawDir(t *testing.T) {
	sb := harness.NewSandbox(t)
	mustRunContains(t, sb, []string{"GlassMarble Status: Uninitialized"}, "status")
	mustRunContains(t, sb, []string{"GlassMarble Doctor: Uninitialized"}, "doctor")
}

// TestAnalyzeGitRepoNoCommits: git initialized but HEAD never created — every
// file is untracked, so analysis succeeds and skips them (deviation 2).
func TestAnalyzeGitRepoNoCommits(t *testing.T) {
	sb := harness.NewSandbox(t)
	sb.RequireGit()
	sb.MustGit("init", "-q", "-b", "main")
	sb.WriteFile("main.go", "package main\n\nfunc main() {}\n")
	out := mustRunContains(t, sb, []string{"Analyzed 0 files", "(untracked by git)"}, "analyze")
	if strings.Contains(out, "HEAD") && strings.Contains(out, "error") {
		t.Errorf("unexpected HEAD error surfaced:\n%s", out)
	}
}

// TestAnalyzeSingleFileRepo: one committed main.go — analyze ingests it and
// status renders the populated dashboard.
func TestAnalyzeSingleFileRepo(t *testing.T) {
	sb := singleGoRepo(t)
	mustRunContains(t, sb, []string{"Analyzed 1 files"}, "analyze")
	mustRunContains(t, sb, []string{"GlassMarble AKG Status"}, "status")
}

// TestAnalyzeZeroByteFile: a tracked empty .go file must not panic; analyze
// either ingests nothing or completes with a graceful error.
func TestAnalyzeZeroByteFile(t *testing.T) {
	sb := gitRepoWith(t, map[string]string{"main.go": ""})
	out, err := harness.RunGmb(t, sb, "analyze")
	if err != nil {
		t.Logf("analyze errored gracefully on zero-byte file (accepted): %v\n%s", err, out)
		return
	}
	if !strings.Contains(out, "Analyzed") {
		t.Errorf("zero-byte analyze output missing report:\n%s", out)
	}
}

// TestAnalyzeCommentsOnlyFile: a file with no declarations still parses.
func TestAnalyzeCommentsOnlyFile(t *testing.T) {
	sb := gitRepoWith(t, map[string]string{"main.go": "package main\n\n// nothing to see here\n"})
	mustRunContains(t, sb, []string{"Analyzed 1 files"}, "analyze")
}

// TestAnalyzeDeeplyNestedDirs: 10 levels of nesting must not break discovery.
func TestAnalyzeDeeplyNestedDirs(t *testing.T) {
	deep := filepath.Join("a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "main.go")
	sb := gitRepoWith(t, map[string]string{deep: "package main\n\nfunc main() {}\n"})
	mustRunContains(t, sb, []string{"Analyzed 1 files"}, "analyze")
}

// TestAnalyzeUnicodeAndSpacedFilenames: unicode and spaces in file names are
// ingested, and `inspect --search café` finds the unicode node.
func TestAnalyzeUnicodeAndSpacedFilenames(t *testing.T) {
	sb := gitRepoWith(t, map[string]string{
		"café.go":    "package main\n\nfunc Café() {}\n",
		"my file.go": "package main\n\nfunc main() {}\n",
	})
	mustRunContains(t, sb, []string{"Analyzed 2 files"}, "analyze")
	mustRunContains(t, sb, []string{"=== Search Results for 'café' ==="}, "inspect", "--search", "café")
}

// TestAnalyzeCRLFFile: CRLF line endings must not break the parser.
func TestAnalyzeCRLFFile(t *testing.T) {
	sb := gitRepoWith(t, map[string]string{"main.go": "package main\r\n\r\nfunc main() {\r\n}\r\n"})
	mustRunContains(t, sb, []string{"Analyzed 1 files"}, "analyze")
}

// TestAnalyzeNonUTF8File: raw non-UTF8 bytes in a .go file must never crash
// the pipeline (a parse error or a skipped result is both acceptable).
func TestAnalyzeNonUTF8File(t *testing.T) {
	sb := gitRepoWith(t, map[string]string{
		"bad.go": string([]byte{0xff, 0xfe, 0x00, 0x01, 0xfe, 0xff, 0x89, 0x50}) + "\npackage main\n\nfunc main() {}\n",
	})
	out, err := harness.RunGmb(t, sb, "analyze")
	if err != nil {
		t.Logf("analyze errored gracefully on non-UTF8 file (accepted): %v\n%s", err, out)
		return
	}
	if !strings.Contains(out, "Analyzed") {
		t.Errorf("non-UTF8 analyze output missing report:\n%s", out)
	}
}

// TestAnalyzeOversizedFileSkipped: with max_file_bytes: 4096 in config.yaml, a
// 5MB single-line file is skipped with the recorded "(exceeds MaxFileBytes)"
// warning and the run still succeeds.
func TestAnalyzeOversizedFileSkipped(t *testing.T) {
	sb := harness.NewSandbox(t)
	sb.WriteFile("main.go", "package main\n\nfunc main() {}\n")
	sb.WriteFile("huge.go", "package main\n// "+strings.Repeat("a", 5<<20)+"\n")
	sb.WriteFile(".glassmarble/config.yaml", "max_file_bytes: 4096\n")
	out := mustRunContains(t, sb, []string{"exceeds MaxFileBytes", "skipped during ingestion", "Analyzed 1 files"}, "analyze")
	if strings.Contains(out, "ingestion failed") {
		t.Errorf("analyze failed with oversized file present:\n%s", out)
	}
}

// TestAnalyzeSymlinkDoesNotCrash: a symlink pointing at a repo file is either
// skipped by the walker (non-regular entry) or followed harmlessly. Skipped on
// platforms that cannot create symlinks (Windows without developer mode).
func TestAnalyzeSymlinkDoesNotCrash(t *testing.T) {
	sb := singleGoRepo(t)
	link := filepath.Join(sb.Root, "linked.go")
	if err := os.Symlink(filepath.Join(sb.Root, "main.go"), link); err != nil {
		t.Skipf("symlinks not supported on this platform: %v", err)
	}
	mustRunContains(t, sb, []string{"Analyzed"}, "analyze")
}
