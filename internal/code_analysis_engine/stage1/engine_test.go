package stage1

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunIngestionFull(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "a.go"), "package main\n\nfunc Add(a, b int) int { return a + b }\n")
	writeTestFile(t, filepath.Join(root, "b.go"), "package main\n\nfunc main() { Add(1, 2) }\n")

	cfg := DefaultConfig(root)
	out, err := RunIngestion(cfg)
	if err != nil {
		t.Fatalf("RunIngestion: %v", err)
	}

	if len(out.Updated) != 2 {
		t.Fatalf("Updated len = %d, want 2", len(out.Updated))
	}
	for _, r := range out.Updated {
		if r.Language != LangGo {
			t.Errorf("Updated[%s].Language = %s, want %s", r.FilePath, r.Language, LangGo)
		}
		if r.HasErrors {
			t.Errorf("Updated[%s].HasErrors = true for valid Go code", r.FilePath)
		}
		if len(r.RawTokens) == 0 {
			t.Errorf("Updated[%s].RawTokens is empty, want > 0", r.FilePath)
		}
	}
}

func TestRunIngestionHandlesParseErrors(t *testing.T) {
	root := t.TempDir()
	// Deliberately broken Go: tree-sitter is fault tolerant and should
	// produce HasErrors instead of panicking.
	writeTestFile(t, filepath.Join(root, "broken.go"), "package main\n\nfunc broken( {\n")

	cfg := DefaultConfig(root)
	out, err := RunIngestion(cfg)
	if err != nil {
		t.Fatalf("RunIngestion: %v", err)
	}

	if len(out.Updated) != 1 {
		t.Fatalf("Updated len = %d, want 1", len(out.Updated))
	}
	if !out.Updated[0].HasErrors {
		t.Errorf("HasErrors = false, want true for malformed file")
	}
	if out.Updated[0].Error != nil {
		t.Errorf("Error = %v, want nil (parse errors surface as HasErrors)", out.Updated[0].Error)
	}
}

func TestRunIngestionForDelta(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "main.go")
	writeTestFile(t, src, "package main\n\nfunc Greet(name string) string { return \"hi \" + name }\n")

	cfg := DefaultConfig(root)

	modified := FileTask{
		FilePath: src,
		RelPath:  "main.go",
		Language: LangGo,
		Change:   ChangeModified,
	}
	out, err := RunIngestionForDelta(cfg, []FileTask{modified})
	if err != nil {
		t.Fatalf("RunIngestionForDelta (modified): %v", err)
	}
	if len(out.Updated) != 1 {
		t.Fatalf("Updated len = %d, want 1 for modified task", len(out.Updated))
	}
	if len(out.Deleted) != 0 {
		t.Errorf("Deleted len = %d, want 0 for modified task", len(out.Deleted))
	}
	if out.Updated[0].Change != ChangeModified {
		t.Errorf("Updated[0].Change = %s, want %s", out.Updated[0].Change, ChangeModified)
	}
	if len(out.Updated[0].RawTokens) == 0 {
		t.Errorf("RawTokens empty, want > 0")
	}

	deleted := FileTask{
		FilePath: src,
		RelPath:  "main.go",
		Language: LangGo,
		Change:   ChangeDeleted,
	}
	out, err = RunIngestionForDelta(cfg, []FileTask{deleted})
	if err != nil {
		t.Fatalf("RunIngestionForDelta (deleted): %v", err)
	}
	if len(out.Deleted) != 1 {
		t.Fatalf("Deleted len = %d, want 1 for deleted task", len(out.Deleted))
	}
	if len(out.Updated) != 0 {
		t.Errorf("Updated len = %d, want 0 for deleted task", len(out.Updated))
	}
	if out.Deleted[0].FilePath != src {
		t.Errorf("Deleted[0].FilePath = %s, want %s", out.Deleted[0].FilePath, src)
	}
	if out.Deleted[0].RelPath != "main.go" {
		t.Errorf("Deleted[0].RelPath = %s, want main.go", out.Deleted[0].RelPath)
	}
}

func TestRunIngestionForDeltaUnknownLanguage(t *testing.T) {
	cfg := DefaultConfig(t.TempDir())
	task := FileTask{
		FilePath: "foo.xyz",
		RelPath:  "foo.xyz",
		Change:   ChangeModified,
	}
	out, err := RunIngestionForDelta(cfg, []FileTask{task})
	if err != nil {
		t.Fatalf("RunIngestionForDelta: %v", err)
	}

	if len(out.Updated) != 0 {
		t.Errorf("Updated len = %d, want 0 for unknown language", len(out.Updated))
	}
	found := false
	for _, m := range out.Skipped {
		if strings.Contains(m, "foo.xyz") && strings.Contains(m, "no matching grammar") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected skip message containing 'foo.xyz' and 'no matching grammar'; Skipped=%v", out.Skipped)
	}
}

func TestNormalizeDefaults(t *testing.T) {
	cfg := Config{RootDir: "", WorkerCount: 0, BufferSize: 0, MaxFileBytes: 0}
	normalize(&cfg)

	if cfg.WorkerCount < 1 || cfg.WorkerCount > 8 {
		t.Errorf("WorkerCount = %d, want within [1,8]", cfg.WorkerCount)
	}
	if cfg.BufferSize != 128 {
		t.Errorf("BufferSize = %d, want 128", cfg.BufferSize)
	}
	if cfg.MaxFileBytes != 2<<20 {
		t.Errorf("MaxFileBytes = %d, want %d", cfg.MaxFileBytes, 2<<20)
	}
	if cfg.RootDir == "" {
		t.Error("RootDir empty after normalize, want abs of '.'")
	}
}

func TestRunIngestionEmptyDir(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	out, err := RunIngestion(cfg)
	if err != nil {
		t.Fatalf("RunIngestion on empty dir: %v", err)
	}
	if out == nil {
		t.Fatal("RunIngestion returned nil output")
	}
	if len(out.Updated) != 0 {
		t.Errorf("Updated len = %d, want 0", len(out.Updated))
	}
	if len(out.Deleted) != 0 {
		t.Errorf("Deleted len = %d, want 0", len(out.Deleted))
	}
}

func TestRunIngestionForDeltaRespectsCanceledContext(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "main.go")
	writeTestFile(t, src, "package main\n\nfunc main() {}\n")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	cfg := DefaultConfig(root)
	cfg.Ctx = ctx

	task := FileTask{
		FilePath: src,
		RelPath:  "main.go",
		Language: LangGo,
		Change:   ChangeModified,
	}
	out, err := RunIngestionForDelta(cfg, []FileTask{task})
	if err != nil {
		t.Fatalf("RunIngestionForDelta with canceled ctx: %v", err)
	}
	// Workers exit on ctx.Done before processing; results may be absent
	// but the run must terminate without hanging or panicking.
	if len(out.Updated) > 1 {
		t.Errorf("Updated len = %d, want 0 or 1", len(out.Updated))
	}
}
