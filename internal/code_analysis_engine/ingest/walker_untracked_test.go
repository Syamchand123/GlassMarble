package ingest

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// gitInit makes a real repository: the filter shells out to git, so a fake
// would only prove the fake works.
func gitInit(t *testing.T, dir string) {
	t.Helper()
	for _, args := range [][]string{
		{"init"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "test"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
}

func write(t *testing.T, dir, rel, body string) {
	t.Helper()
	p := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestCollectGitTrackedFiles_IncludesUntrackedButNotIgnored pins the fix.
//
// The filter exists to honour .gitignore without reimplementing it, but plain
// `git ls-files` lists only the index, so a source file written and not yet
// added was filtered out of the walk and never entered the graph — even under
// analyze --full. New code was invisible for exactly as long as it was new.
func TestCollectGitTrackedFiles_IncludesUntrackedButNotIgnored(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir)

	write(t, dir, ".gitignore", "ignored.go\nvendor/\n")
	write(t, dir, "tracked.go", "package p\n")
	write(t, dir, "brand_new.go", "package p\n") // written, never git-added
	write(t, dir, "ignored.go", "package p\n")
	write(t, dir, "vendor/dep.go", "package v\n")

	add := exec.Command("git", "add", "tracked.go", ".gitignore")
	add.Dir = dir
	if out, err := add.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v: %s", err, out)
	}

	warn := make(chan string, 4)
	got := collectGitTrackedFiles(dir, true, warn)
	if got == nil {
		t.Fatal("expected a path set, got nil (git unavailable?)")
	}

	if !got["tracked.go"] {
		t.Error("tracked.go must be scannable")
	}
	if !got["brand_new.go"] {
		t.Error("brand_new.go is untracked and not ignored — it must be scannable, " +
			"or new source files stay invisible until they are staged")
	}
	if got["ignored.go"] {
		t.Error("ignored.go is in .gitignore and must stay excluded")
	}
	if got["vendor/dep.go"] {
		t.Error("vendor/ is in .gitignore and must stay excluded")
	}
}

// TestCollectGitTrackedFiles_NonRepoFallsBack: outside a repository there is
// nothing to consult, and the caller is expected to scan unfiltered rather
// than scan nothing.
func TestCollectGitTrackedFiles_NonRepoFallsBack(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a.go", "package p\n")

	warn := make(chan string, 4)
	if got := collectGitTrackedFiles(dir, true, warn); got != nil {
		t.Errorf("outside a git repository the filter must return nil so the caller scans unfiltered, got %v", got)
	}
	select {
	case <-warn:
	default:
		t.Error("expected a warning explaining that the scan is unfiltered")
	}
}

// TestCollectGitTrackedFiles_NarrowModeStillTrackedOnly keeps the original
// contract intact: without includeUntracked the set is exactly what git
// tracks, which is what GitTrackedOnly has always promised.
func TestCollectGitTrackedFiles_NarrowModeStillTrackedOnly(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir)
	write(t, dir, "tracked.go", "package p\n")
	write(t, dir, "brand_new.go", "package p\n")

	add := exec.Command("git", "add", "tracked.go")
	add.Dir = dir
	if out, err := add.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v: %s", err, out)
	}

	warn := make(chan string, 4)
	got := collectGitTrackedFiles(dir, false, warn)
	if !got["tracked.go"] {
		t.Error("tracked.go must be present in narrow mode")
	}
	if got["brand_new.go"] {
		t.Error("narrow mode must list only tracked files")
	}
}
