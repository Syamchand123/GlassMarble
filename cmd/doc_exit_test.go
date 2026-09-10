package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// exitTrap is panicked by the stubbed docExit so tests can observe the
// Section 11 exit-code contract without killing the test binary.
type exitTrap struct{ code int }

// withStubbedDocExit replaces docExit with a recorder for the test duration.
// It returns a function reporting the captured code (-1 = never called).
func withStubbedDocExit(t *testing.T) func() int {
	t.Helper()
	old := docExit
	code := -1
	docExit = func(c int) {
		code = c
		panic(exitTrap{code: c})
	}
	t.Cleanup(func() { docExit = old })
	return func() int { return code }
}

// runDocCommand executes the doc command tree in-process. A stubbed-docExit
// panic is recovered and swallowed (the code is observable via getCode).
func runDocCommand(t *testing.T, getCode func() int, args ...string) (string, error) {
	t.Helper()
	var out strings.Builder
	command := RootCmdForTesting()
	command.SetOut(&out)
	command.SetErr(&out)
	command.SetArgs(args)
	var runErr error
	func() {
		defer func() {
			if r := recover(); r != nil {
				if _, ok := r.(exitTrap); !ok {
					panic(r)
				}
			}
		}()
		runErr = command.Execute()
	}()
	return out.String(), runErr
}

func writeDocConfig(t *testing.T, dir, yaml string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, ".glassmarble"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".glassmarble", "docs.yaml"), []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestDocCheckExitCode2OnBadConfig(t *testing.T) {
	getCode := withStubbedDocExit(t)
	tempDir := t.TempDir()
	writeDocConfig(t, tempDir, "version: 99\ndocuments: []\n")
	_, _ = runDocCommand(t, getCode, "doc", "check", "--dir", tempDir)
	if got := getCode(); got != 2 {
		t.Errorf("expected exit code 2 on unreadable config, got %d", got)
	}
}

func TestDocDiffExitCode1OnPendingChanges(t *testing.T) {
	getCode := withStubbedDocExit(t)
	tempDir := t.TempDir()
	writeDocConfig(t, tempDir, "version: 1\ndocs_dir: docs\ndocuments:\n  - id: guide\n    target: docs/guide.md\n    title: Guide\n    scope:\n      paths:\n        - \"internal/**\"\n    sections:\n      - id: overview\n        title: Overview\n        instruction: Describe the subsystem.\n")
	if err := os.MkdirAll(filepath.Join(tempDir, "docs"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tempDir, "docs", "guide.md"), []byte("# Guide\n"), 0644); err != nil {
		t.Fatal(err)
	}
	_, _ = runDocCommand(t, getCode, "doc", "diff", "--dir", tempDir)
	if got := getCode(); got != 1 {
		t.Errorf("expected exit code 1 on pending diff changes, got %d", got)
	}
}

func TestDocDiffExitCode0WhenClean(t *testing.T) {
	getCode := withStubbedDocExit(t)
	tempDir := t.TempDir()
	_, _ = runDocCommand(t, getCode, "doc", "diff", "--dir", tempDir)
	if got := getCode(); got != -1 {
		t.Errorf("expected no exit call on clean diff, got code %d", got)
	}
}
