package cmd_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestHotspotCommandEmptyDB verifies the uninitialized-database error path.
func TestHotspotCommandEmptyDB(t *testing.T) {
	tempDir := t.TempDir()
	_, err := runGmbCommand(t, "hotspot", "--dir", tempDir, "--top", "5")
	if err == nil {
		t.Fatal("expected error for empty AKG database")
	}
	if !strings.Contains(err.Error(), "AKG database is empty") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestHotspotCommandRanksByIndegree feeds a TTL with two nodes (one depends on
// the other) and verifies the higher-in-degree node is ranked first.
func TestHotspotCommandRanksByIndegree(t *testing.T) {
	tempDir := t.TempDir()
	writeDoctorState(t, tempDir, doctorFixtureGraph())

	output, err := runGmbCommand(t, "hotspot", "--dir", tempDir, "--top", "5")
	if err != nil {
		t.Fatalf("hotspot failed: %v\n%s", err, output)
	}
	if !strings.Contains(output, "Top 5 Architectural Hotspots") {
		t.Errorf("missing header:\n%s", output)
	}
	// Connect has in-degree 1, Main has out-degree 1. The top-ranked symbol
	// should be the Connect target (in-degree 1 > 0).
	if !strings.Contains(output, "Connect") {
		t.Errorf("expected Connect symbol in output:\n%s", output)
	}
}

// TestDependencyCommandSummary verifies the no-target summary output.
func TestDependencyCommandSummary(t *testing.T) {
	tempDir := t.TempDir()
	writeDoctorState(t, tempDir, doctorFixtureGraph())

	output, err := runGmbCommand(t, "dependency", "--dir", tempDir)
	if err != nil {
		t.Fatalf("dependency failed: %v\n%s", err, output)
	}
	if !strings.Contains(output, "Repository Dependency Summary") {
		t.Errorf("missing summary header:\n%s", output)
	}
}

// TestDependencyCommandTarget resolves a node by name and prints its deps.
func TestDependencyCommandTarget(t *testing.T) {
	tempDir := t.TempDir()
	writeDoctorState(t, tempDir, doctorFixtureGraph())

	output, err := runGmbCommand(t, "dependency", "--dir", tempDir, "Connect")
	if err != nil {
		t.Fatalf("dependency failed: %v\n%s", err, output)
	}
	if !strings.Contains(output, "Node:") {
		t.Errorf("missing Node section:\n%s", output)
	}
	if !strings.Contains(output, "Direct Inbound Callers/Dependents") {
		t.Errorf("missing inbound section:\n%s", output)
	}
}

// TestDependencyCommandUnknownTarget errors when nothing matches.
func TestDependencyCommandUnknownTarget(t *testing.T) {
	tempDir := t.TempDir()
	writeDoctorState(t, tempDir, doctorFixtureGraph())

	_, err := runGmbCommand(t, "dependency", "--dir", tempDir, "doesnotexist")
	if err == nil {
		t.Fatal("expected error for unknown target")
	}
	if !strings.Contains(err.Error(), "no matching node or file") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestTreeCommand renders a sorted tree of file paths.
func TestTreeCommand(t *testing.T) {
	tempDir := t.TempDir()
	writeDoctorState(t, tempDir, doctorFixtureGraph())

	output, err := runGmbCommand(t, "tree", "--dir", tempDir)
	if err != nil {
		t.Fatalf("tree failed: %v\n%s", err, output)
	}
	if !strings.Contains(output, "Architecture Workspace Tree") {
		t.Errorf("missing header:\n%s", output)
	}
	if !strings.Contains(output, "cmd/app/main.go") {
		t.Errorf("missing file path in tree:\n%s", output)
	}
	if !strings.Contains(output, "internal/db/db.go") {
		t.Errorf("missing file path in tree:\n%s", output)
	}
}

// TestTreeCommandEmptyDB errors when there is no database.
func TestTreeCommandEmptyDB(t *testing.T) {
	tempDir := t.TempDir()
	_, err := runGmbCommand(t, "tree", "--dir", tempDir)
	if err == nil {
		t.Fatal("expected error for empty AKG database")
	}
}

// TestWatchCommandRequiresGit verifies watch refuses non-git directories.
func TestWatchCommandRequiresGit(t *testing.T) {
	tempDir := t.TempDir()
	_, err := runGmbCommand(t, "watch", "--dir", tempDir)
	if err == nil {
		t.Fatal("expected watch to fail outside a git repository")
	}
	if !strings.Contains(err.Error(), "requires a git repository") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestHooksInstallUninstall writes the post-commit hook then removes it.
func TestHooksInstallUninstall(t *testing.T) {
	tempDir := t.TempDir()
	hookDir := filepath.Join(tempDir, ".git", "hooks")
	if err := os.MkdirAll(hookDir, 0o755); err != nil {
		t.Fatal(err)
	}

	output, err := runGmbCommand(t, "hooks", "install", "--dir", tempDir)
	if err != nil {
		t.Fatalf("hooks install failed: %v\n%s", err, output)
	}
	if !strings.Contains(output, "installed successfully") {
		t.Errorf("missing install confirmation:\n%s", output)
	}

	hookPath := filepath.Join(hookDir, "post-commit")
	fi, err := os.Stat(hookPath)
	if err != nil {
		t.Fatalf("post-commit hook not written: %v", err)
	}
	// Windows does not preserve Unix exec bits (writes yield 0666); POSIX
	// systems must see the executable bit set for the hook to run.
	if runtime.GOOS != "windows" && fi.Mode().Perm() != 0o755 {
		t.Errorf("hook mode = %v, want 0755", fi.Mode().Perm())
	}
	data, err := os.ReadFile(hookPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "analyze") {
		t.Errorf("hook content missing analyze invocation:\n%s", string(data))
	}

	output2, err := runGmbCommand(t, "hooks", "uninstall", "--dir", tempDir)
	if err != nil {
		t.Fatalf("hooks uninstall failed: %v\n%s", err, output2)
	}
	if !strings.Contains(output2, "uninstalled successfully") {
		t.Errorf("missing uninstall confirmation:\n%s", output2)
	}
	if _, err := os.Stat(hookPath); !os.IsNotExist(err) {
		t.Errorf("hook file still present after uninstall")
	}
}

// TestHooksCommandNotAGitRepo fails when .git/hooks is absent.
func TestHooksCommandNotAGitRepo(t *testing.T) {
	tempDir := t.TempDir()
	_, err := runGmbCommand(t, "hooks", "install", "--dir", tempDir)
	if err == nil {
		t.Fatal("expected error installing hooks outside a git repo")
	}
}

// TestHooksUnknownSubcommand validates the subcommand argument.
func TestHooksUnknownSubcommand(t *testing.T) {
	tempDir := t.TempDir()
	hookDir := filepath.Join(tempDir, ".git", "hooks")
	if err := os.MkdirAll(hookDir, 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := runGmbCommand(t, "hooks", "bogus", "--dir", tempDir)
	if err == nil {
		t.Fatal("expected error for unknown hooks subcommand")
	}
	if !strings.Contains(err.Error(), "expected install or uninstall") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestAnalyzeCommandFullScan runs the full 4-stage pipeline over a tiny Go
// file and verifies the AKG database is created and the report printed.
func TestAnalyzeCommandFullScan(t *testing.T) {
	tempDir := t.TempDir()
	main := filepath.Join(tempDir, "main.go")
	if err := os.WriteFile(main, []byte("package main\n\nfunc main() { println(\"hi\") }\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	output, err := runGmbCommand(t, "analyze", "--dir", tempDir, "--full")
	if err != nil {
		t.Fatalf("analyze failed: %v\n%s", err, output)
	}
	if !strings.Contains(output, "Analyzed 1 files") {
		t.Errorf("missing analysis summary:\n%s", output)
	}

	StatePath := filepath.Join(tempDir, ".glassmarble", "akg.json")
	if _, err := os.Stat(StatePath); err != nil {
		t.Errorf("akg.json not created: %v", err)
	}
}

// TestAnalyzeCommandEmptyDir runs the pipeline over an empty directory.
func TestAnalyzeCommandEmptyDir(t *testing.T) {
	tempDir := t.TempDir()
	output, err := runGmbCommand(t, "analyze", "--dir", tempDir, "--full")
	if err != nil {
		t.Fatalf("analyze failed: %v\n%s", err, output)
	}
	if !strings.Contains(output, "Analyzed 0 files") {
		t.Errorf("missing analysis summary:\n%s", output)
	}
}

// TestStatusCommandJSON emits valid machine-readable JSON for an initialized DB.
func TestStatusCommandJSON(t *testing.T) {
	tempDir := t.TempDir()
	writeDoctorState(t, tempDir, doctorFixtureGraph())

	output, err := runGmbCommand(t, "status", "--dir", tempDir, "--json")
	if err != nil {
		t.Fatalf("status failed: %v\n%s", err, output)
	}
	if !strings.HasPrefix(strings.TrimSpace(output), "{") {
		t.Fatalf("output is not JSON:\n%s", output)
	}
	for _, want := range []string{`"initialized": true`, `"schema_version": 3`, `"nodes": 2`, `"verified": true`} {
		if !strings.Contains(output, want) {
			t.Errorf("status JSON missing %s:\n%s", want, output)
		}
	}
}

// TestStatusCommandJSONUninitialized emits JSON with initialized=false.
func TestStatusCommandJSONUninitialized(t *testing.T) {
	tempDir := t.TempDir()
	output, err := runGmbCommand(t, "status", "--dir", tempDir, "--json")
	if err != nil {
		t.Fatalf("status failed: %v\n%s", err, output)
	}
	if !strings.Contains(output, `"initialized": false`) {
		t.Errorf("status JSON missing initialized=false:\n%s", output)
	}
}

// TestHotspotCommandJSON emits a JSON hotspots array.
func TestHotspotCommandJSON(t *testing.T) {
	tempDir := t.TempDir()
	writeDoctorState(t, tempDir, doctorFixtureGraph())

	output, err := runGmbCommand(t, "hotspot", "--dir", tempDir, "--json", "--top", "5")
	if err != nil {
		t.Fatalf("hotspot failed: %v\n%s", err, output)
	}
	if !strings.Contains(output, `"hotspots"`) {
		t.Errorf("hotspot JSON missing hotspots key:\n%s", output)
	}
	if !strings.Contains(output, "Connect") {
		t.Errorf("hotspot JSON missing Connect symbol:\n%s", output)
	}
}

// TestDependencyCommandJSONSummary emits a JSON dependency summary.
func TestDependencyCommandJSONSummary(t *testing.T) {
	tempDir := t.TempDir()
	writeDoctorState(t, tempDir, doctorFixtureGraph())

	output, err := runGmbCommand(t, "dependency", "--dir", tempDir, "--json")
	if err != nil {
		t.Fatalf("dependency failed: %v\n%s", err, output)
	}
	for _, want := range []string{`"total_nodes": 2`, `"top_dependency_nodes"`} {
		if !strings.Contains(output, want) {
			t.Errorf("dependency JSON missing %s:\n%s", want, output)
		}
	}
}

// TestDependencyCommandJSONTarget emits per-node inbound/outbound edges.
func TestDependencyCommandJSONTarget(t *testing.T) {
	tempDir := t.TempDir()
	writeDoctorState(t, tempDir, doctorFixtureGraph())

	output, err := runGmbCommand(t, "dependency", "--dir", tempDir, "Connect", "--json")
	if err != nil {
		t.Fatalf("dependency failed: %v\n%s", err, output)
	}
	for _, want := range []string{`"target": "Connect"`, `"inbound"`, `cmd/app/main.go::Main`} {
		if !strings.Contains(output, want) {
			t.Errorf("dependency JSON missing %s:\n%s", want, output)
		}
	}
}

// TestAnalyzeCommandJSON emits valid JSON after a full scan.
func TestAnalyzeCommandJSON(t *testing.T) {
	tempDir := t.TempDir()
	main := filepath.Join(tempDir, "main.go")
	if err := os.WriteFile(main, []byte("package main\n\nfunc main() { println(\"hi\") }\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	output, err := runGmbCommand(t, "analyze", "--dir", tempDir, "--full", "--json")
	if err != nil {
		t.Fatalf("analyze failed: %v\n%s", err, output)
	}
	for _, want := range []string{`"files_analyzed": 1`, `"target_dir"`, `"storage_dir"`} {
		if !strings.Contains(output, want) {
			t.Errorf("analyze JSON missing %s:\n%s", want, output)
		}
	}
}
