package stage1

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// collectSkippedAndWarnings flattens Skipped+Warnings so assertions can look
// for classification messages without coupling to which bucket they land in.
func collectSkippedAndWarnings(out *StageOutput) []string {
	msgs := append([]string{}, out.Skipped...)
	msgs = append(msgs, out.Warnings...)
	return msgs
}

func TestDiscoverFindsSourceFiles(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "main.go"), "package main\nfunc main() {}\n")
	writeTestFile(t, filepath.Join(root, "notes.txt"), "just some text\n")

	cfg := DefaultConfig(root)
	out, err := RunIngestion(cfg)
	if err != nil {
		t.Fatalf("RunIngestion: %v", err)
	}

	if len(out.Updated) != 1 {
		t.Fatalf("Updated len = %d, want 1 (only the .go file)", len(out.Updated))
	}
	if got := filepath.Base(out.Updated[0].FilePath); got != "main.go" {
		t.Errorf("Updated[0] = %s, want main.go", got)
	}
	if out.Updated[0].Language != LangGo {
		t.Errorf("Updated[0].Language = %s, want %s", out.Updated[0].Language, LangGo)
	}
}

func TestDiscoverSkipsHiddenAndGenerated(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "main.go"), "package main\nfunc main() {}\n")
	writeTestFile(t, filepath.Join(root, ".hidden", "file.go"), "package main\nfunc main() {}\n")
	writeTestFile(t, filepath.Join(root, ".secret.go"), "package main\nfunc main() {}\n")
	writeTestFile(t, filepath.Join(root, "foo.pb.go"), "package main\nfunc main() {}\n")

	cfg := DefaultConfig(root)
	cfg.IncludeHidden = false
	out, err := RunIngestion(cfg)
	if err != nil {
		t.Fatalf("RunIngestion (IncludeHidden=false): %v", err)
	}
	if len(out.Updated) != 1 {
		t.Fatalf("IncludeHidden=false: Updated len = %d, want 1", len(out.Updated))
	}
	if got := filepath.Base(out.Updated[0].FilePath); got != "main.go" {
		t.Errorf("IncludeHidden=false: Updated[0] = %s, want main.go", got)
	}

	cfg.IncludeHidden = true
	out, err = RunIngestion(cfg)
	if err != nil {
		t.Fatalf("RunIngestion (IncludeHidden=true): %v", err)
	}
	if len(out.Updated) != 3 {
		t.Fatalf("IncludeHidden=true: Updated len = %d, want 3 (main.go, .hidden/file.go, .secret.go)", len(out.Updated))
	}

	names := make(map[string]bool)
	for _, r := range out.Updated {
		names[filepath.Base(r.FilePath)] = true
	}
	for _, want := range []string{"main.go", "file.go", ".secret.go"} {
		if !names[want] {
			t.Errorf("IncludeHidden=true: missing %s in updated files %v", want, names)
		}
	}
}

func TestDiscoverMaxFileBytes(t *testing.T) {
	root := t.TempDir()
	big := strings.Repeat("package main\n// padding\n", 50)
	writeTestFile(t, filepath.Join(root, "big.go"), big)

	cfg := DefaultConfig(root)
	cfg.MaxFileBytes = 10
	out, err := RunIngestion(cfg)
	if err != nil {
		t.Fatalf("RunIngestion: %v", err)
	}

	if len(out.Updated) != 0 {
		t.Errorf("Updated len = %d, want 0 (file exceeds MaxFileBytes)", len(out.Updated))
	}
	found := false
	for _, m := range collectSkippedAndWarnings(out) {
		if strings.Contains(m, "exceeds") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("no 'exceeds' skip message; Skipped=%v Warnings=%v", out.Skipped, out.Warnings)
	}
}

func TestDiscoverGitTrackedOnly(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not available in PATH")
	}

	root := t.TempDir()
	runGitCmd := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, out)
		}
	}

	writeTestFile(t, filepath.Join(root, "tracked.go"), "package main\nfunc Tracked() {}\n")
	runGitCmd("init", "-q")
	runGitCmd("config", "user.email", "test@example.com")
	runGitCmd("config", "user.name", "Test User")
	runGitCmd("config", "commit.gpgsign", "false")
	runGitCmd("add", "tracked.go")
	runGitCmd("commit", "-q", "-m", "add tracked.go")

	writeTestFile(t, filepath.Join(root, "untracked.go"), "package main\nfunc Untracked() {}\n")

	cfg := DefaultConfig(root)
	cfg.GitTrackedOnly = true
	out, err := RunIngestion(cfg)
	if err != nil {
		t.Fatalf("RunIngestion: %v", err)
	}

	if len(out.Updated) != 1 {
		t.Fatalf("Updated len = %d, want 1 (only git-tracked file)", len(out.Updated))
	}
	if got := filepath.Base(out.Updated[0].FilePath); got != "tracked.go" {
		t.Errorf("Updated[0] = %s, want tracked.go", got)
	}

	found := false
	for _, m := range collectSkippedAndWarnings(out) {
		if strings.Contains(m, "untracked") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("no 'untracked' skip/warning message; Skipped=%v Warnings=%v", out.Skipped, out.Warnings)
	}
}

func TestDiscoverNonExistentRoot(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist")

	cfg := DefaultConfig(missing)
	out, err := RunIngestion(cfg)
	if err == nil {
		t.Fatal("RunIngestion on missing root: expected error, got nil")
	}
	if out != nil {
		t.Errorf("expected nil output on error, got %+v", out)
	}
}

func TestDiscoverRespectsContextCancellation(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 20; i++ {
		writeTestFile(t, filepath.Join(root, "f"+string(rune('a'+i))+".go"), "package main\nfunc F() {}\n")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	cfg := DefaultConfig(root)
	cfg.Ctx = ctx
	out, err := RunIngestion(cfg)
	if err != nil {
		t.Fatalf("RunIngestion with canceled ctx: %v", err)
	}
	if out == nil {
		t.Fatal("RunIngestion with canceled ctx returned nil output")
	}
	if len(out.Updated) != 0 {
		t.Errorf("Updated len = %d, want 0 with canceled context", len(out.Updated))
	}
}

func TestIsGeneratedFile(t *testing.T) {
	generated := []string{
		"foo.pb.go",
		"svc_grpc.pb.go",
		"api.pb.h",
		"api.pb.cc",
		"api.pb.c",
		"models_pb2.py",
		"models_pb2_grpc.py",
		"thing.generated.go",
		"thing.gen.go",
		"thing_gen.go",
		"mocks_mock.go",
		"mocks_mock_test.go",
		"docs.swagger.json",
		"bundle.min.js",
		"styles.min.css",
		"bindata.go",
		"index.d.ts.map",
		"chunk.js.map",
		"FOO.PB.GO",
	}
	for _, name := range generated {
		if !isGeneratedFile(name) {
			t.Errorf("isGeneratedFile(%q) = false, want true", name)
		}
	}

	notGenerated := []string{
		"main.go",
		"foo.go",
		"foo.gob",
		"gen.go",
		"pb.go",
		"grpc.go",
		"mock.go",
		"min.js",
		"README.md",
		"bindata.go.old",
	}
	for _, name := range notGenerated {
		if isGeneratedFile(name) {
			t.Errorf("isGeneratedFile(%q) = true, want false", name)
		}
	}
}
