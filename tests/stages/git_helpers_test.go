package stages_test

import (
	"regexp"
	"testing"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/git"
)

var hex40 = regexp.MustCompile(`^[0-9a-f]{40}$`)

func TestGitHEADHashAndTimestamp(t *testing.T) {
	sb := newSampleSandbox(t)
	sb.RequireGit()
	sb.GitInit()
	sb.GitCommit("feat: sample project")

	hash, err := git.GetHEADCommitHash(sb.Root)
	if err != nil {
		t.Fatalf("git.GetHEADCommitHash: %v", err)
	}
	if !hex40.MatchString(hash) {
		t.Errorf("HEAD hash = %q, want 40 hex chars", hash)
	}

	ts, err := git.GetCommitTimestamp(sb.Root, "HEAD")
	if err != nil {
		t.Fatalf("git.GetCommitTimestamp(HEAD): %v", err)
	}
	if ts.IsZero() {
		t.Error("HEAD timestamp is zero")
	}
	if ts.After(time.Now().Add(time.Hour)) || ts.Before(time.Now().Add(-24*365*time.Hour)) {
		t.Errorf("HEAD timestamp %v outside plausible range", ts)
	}

	resolved, err := git.ResolveRef(sb.Root, "HEAD")
	if err != nil {
		t.Fatalf("git.ResolveRef(HEAD): %v", err)
	}
	if resolved != hash {
		t.Errorf("ResolveRef(HEAD) = %q, want %q", resolved, hash)
	}
}

func TestGitChangedFilesBetweenCommits(t *testing.T) {
	sb := newSampleSandbox(t)
	sb.RequireGit()
	sb.GitInit()
	first := sb.GitCommit("feat: sample project")

	sb.WriteFile("internal/cache/cache.go", sb.ReadFile("internal/cache/cache.go")+"// tweak\n")
	second := sb.GitCommit("feat: cache tweak")

	files, err := git.GetChangedFiles(sb.Root, first, second)
	if err != nil {
		t.Fatalf("git.GetChangedFiles: %v", err)
	}
	if len(files) != 1 || files[0] != "internal/cache/cache.go" {
		t.Errorf("GetChangedFiles(%s, %s) = %v, want [internal/cache/cache.go]", first[:8], second[:8], files)
	}

	// Same-commit diff is empty.
	none, err := git.GetChangedFiles(sb.Root, second, second)
	if err != nil {
		t.Fatalf("git.GetChangedFiles(same, same): %v", err)
	}
	if len(none) != 0 {
		t.Errorf("GetChangedFiles(same, same) = %v, want empty", none)
	}

	// Short prefixes resolve via ResolveRef.
	short, err := git.ResolveRef(sb.Root, first[:8])
	if err != nil {
		t.Fatalf("git.ResolveRef(short prefix): %v", err)
	}
	if short != first {
		t.Errorf("ResolveRef(%s) = %q, want %q", first[:8], short, first)
	}
}
