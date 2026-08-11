package stages_test

import (
	"strings"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage1"
	"github.com/Syamchand123/GlassMarble/tests/harness"
)

// Note: stage1.Config has no WithWorkers method (the API reference mentions
// one); tests set cfg.WorkerCount directly instead.

// updatedPaths returns the set of slash-normalized RelPaths in a stage 1
// output (the walker emits native separators on Windows).
func updatedPaths(out *stage1.StageOutput) map[string]bool {
	set := make(map[string]bool, len(out.Updated))
	for _, res := range out.Updated {
		set[slashed(res.RelPath)] = true
	}
	return set
}

func TestStage1RunIngestionSampleProject(t *testing.T) {
	sb := newSampleSandbox(t)
	// Second oversized file so the skipped tally is >= 2.
	sb.WriteFile("scripts/big.py", strings.Repeat("x", 3<<20))

	out, err := stage1.RunIngestion(stage1.DefaultConfig(sb.Root))
	if err != nil {
		t.Fatalf("stage1.RunIngestion: %v", err)
	}

	got := updatedPaths(out)
	for _, want := range []string{
		"cmd/api/main.go",
		"internal/service/service.go",
		"internal/repo/repo.go",
		"internal/cache/cache.go",
		"scripts/ingest.py",
		"web/app.js",
	} {
		if !got[want] {
			t.Errorf("Updated missing %q; got %v", want, got)
		}
	}

	for _, excluded := range []string{
		"api/gen.pb.go",
		"vendor/example.com/lib/lib.go",
		".secrets/keys.go",
		"docs/huge.md",
	} {
		if got[excluded] {
			t.Errorf("Updated contains excluded file %q", excluded)
		}
	}

	if len(out.Skipped) < 2 {
		t.Errorf("Skipped count = %d, want >= 2: %v", len(out.Skipped), out.Skipped)
	}
	skipped := slashed(strings.Join(out.Skipped, "\n"))
	if !strings.Contains(skipped, "docs/huge.md") || !strings.Contains(skipped, "exceeds") {
		t.Errorf("Skipped should record docs/huge.md as oversized: %v", out.Skipped)
	}

	if len(out.Warnings) == 0 {
		t.Fatalf("Warnings empty, want at least the generated-file notice")
	}
	if !strings.Contains(slashed(strings.Join(out.Warnings, "\n")), "api/gen.pb.go") {
		t.Errorf("Warnings missing api/gen.pb.go (generated file): %v", out.Warnings)
	}
}

func TestStage1ConfigTuning(t *testing.T) {
	sb := newSampleSandbox(t)
	cfg := stage1.DefaultConfig(sb.Root)
	cfg.MaxFileBytes = 1024
	cfg.WorkerCount = 2

	out, err := stage1.RunIngestion(cfg)
	if err != nil {
		t.Fatalf("stage1.RunIngestion with 2 workers: %v", err)
	}

	for _, res := range out.Updated {
		if res.Bytes > 1024 {
			t.Errorf("Updated file %q has %d bytes, exceeds MaxFileBytes=1024", res.RelPath, res.Bytes)
		}
	}
	if !strings.Contains(slashed(strings.Join(out.Skipped, "\n")), "docs/huge.md") {
		t.Errorf("docs/huge.md not skipped with MaxFileBytes=1024: %v", out.Skipped)
	}
}

func TestStage1CollectGitDiffWorkingTree(t *testing.T) {
	sb := newSampleSandbox(t)
	sb.RequireGit()
	sb.GitInit()
	sb.GitCommit("feat: sample project")

	// Stage an uncommitted addition; `git diff HEAD` must report it.
	sb.WriteFile("cmd/api/extra.go", "package main\n\nfunc extra() {}\n")
	sb.MustGit("add", "cmd/api/extra.go")

	tasks, err := stage1.CollectGitDiff(sb.Root, "")
	if err != nil {
		t.Fatalf("stage1.CollectGitDiff dirty tree: %v", err)
	}
	found := false
	for _, task := range tasks {
		if task.RelPath == "cmd/api/extra.go" {
			found = true
			if task.Change != stage1.ChangeAdded {
				t.Errorf("extra.go change = %q, want %q", task.Change, stage1.ChangeAdded)
			}
		}
	}
	if !found {
		t.Errorf("CollectGitDiff('') did not include cmd/api/extra.go: %+v", tasks)
	}
}

func TestStage1CollectGitDiffCommit(t *testing.T) {
	// Note: use a plain sandbox, NOT the sample project — GitInit already
	// commits the sandbox contents, so a subsequent "first" commit over the
	// sample would be empty (the sample files land in the initial commit).
	sb := harness.NewSandbox(t)
	sb.RequireGit()
	sb.GitInit()

	first := sb.GitCommitFiles("feat: sample project", map[string]string{
		"cmd/api/main.go":            "package main\n\nfunc main() {}\n",
		"internal/service/service.go": "package service\n\nfunc Greet() string { return \"\" }\n",
		"internal/cache/cache.go":    "package cache\n\nfunc Get() {}\n",
		"web/app.js":                 "function app() {}\n",
	})

	// Modify an existing first-commit file: the second commit must report
	// it as MODIFIED (a new path would be ADDED).
	sb.WriteFile("internal/cache/cache.go", sb.ReadFile("internal/cache/cache.go")+"// tweak\n")
	second := sb.GitCommit("feat: cache tweak")

	tasks1, err := stage1.CollectGitDiff(sb.Root, first)
	if err != nil {
		t.Fatalf("stage1.CollectGitDiff(first commit): %v", err)
	}
	rel1 := make(map[string]bool)
	for _, task := range tasks1 {
		rel1[slashed(task.RelPath)] = true
	}
	for _, want := range []string{"cmd/api/main.go", "internal/service/service.go", "web/app.js"} {
		if !rel1[want] {
			t.Errorf("diff of first commit missing %q: %v", want, rel1)
		}
	}

	tasks2, err := stage1.CollectGitDiff(sb.Root, second)
	if err != nil {
		t.Fatalf("stage1.CollectGitDiff(second commit): %v", err)
	}
	if len(tasks2) != 1 || tasks2[0].RelPath != "internal/cache/cache.go" {
		t.Errorf("diff of second commit = %+v, want exactly [internal/cache/cache.go]", tasks2)
	}
	if tasks2[0].Change != stage1.ChangeModified {
		t.Errorf("cache.go change = %q, want %q", tasks2[0].Change, stage1.ChangeModified)
	}
}

func TestStage1RunIngestionForDelta(t *testing.T) {
	sb := newSampleSandbox(t)
	sb.RequireGit()
	sb.GitInit()
	sb.GitCommit("feat: sample project")

	sb.WriteFile("internal/cache/cache.go", sb.ReadFile("internal/cache/cache.go")+"// tweak\n")
	second := sb.GitCommit("feat: cache tweak")

	diff, err := stage1.CollectGitDiff(sb.Root, second)
	if err != nil {
		t.Fatalf("stage1.CollectGitDiff: %v", err)
	}

	out, err := stage1.RunIngestionForDelta(stage1.DefaultConfig(sb.Root), diff)
	if err != nil {
		t.Fatalf("stage1.RunIngestionForDelta: %v", err)
	}
	if len(out.Updated) != 1 {
		t.Fatalf("delta Updated = %d files, want exactly 1 (only the changed file is re-parsed): %+v", len(out.Updated), out.Updated)
	}
	if got := out.Updated[0].RelPath; got != "internal/cache/cache.go" {
		t.Errorf("delta Updated[0].RelPath = %q, want internal/cache/cache.go", got)
	}
}

func TestStage1UnknownLanguageHandled(t *testing.T) {
	sb := newSampleSandbox(t)
	sb.WriteFile("notes/readme.xyz", "hello unknown world\n")

	out, err := stage1.RunIngestion(stage1.DefaultConfig(sb.Root))
	if err != nil {
		t.Fatalf("stage1.RunIngestion with unknown-language file: %v", err)
	}
	if updatedPaths(out)["notes/readme.xyz"] {
		t.Errorf("unknown-language file must not be parsed, got entry in Updated")
	}

	delta, err := stage1.RunIngestionForDelta(stage1.DefaultConfig(sb.Root), []stage1.FileTask{{
		FilePath: sb.Path("notes/readme.xyz"),
		RelPath:  "notes/readme.xyz",
		Change:   stage1.ChangeAdded,
	}})
	if err != nil {
		t.Fatalf("stage1.RunIngestionForDelta with unknown-language task: %v", err)
	}
	if !strings.Contains(strings.Join(delta.Skipped, "\n"), "no matching grammar") {
		t.Errorf("delta Skipped should mention no matching grammar for notes/readme.xyz: %v", delta.Skipped)
	}
}
