// Package harness provides the shared test infrastructure for the
// GlassMarble end-to-end test suites under tests/.
//
// Design rules for every test in this tree:
//
//  1. Never touch a real user workspace. Every test builds its own sandbox
//     repository under a t.TempDir() and points commands at it with --dir
//     (or runs with the sandbox as the working directory).
//  2. CLI commands execute IN PROCESS via cmd.RootCmdForTesting(). This is
//     the same mechanism the cmd/ package's own tests use and it is
//     deterministic and fast. A handful of tests additionally exercise the
//     real compiled binary via BuildBinary for true end-to-end coverage.
//  3. The in-process runner swaps os.Stdout and the process working
//     directory. These are global process state: tests that use them MUST
//     NOT call t.Parallel().
//  4. LLM-backed commands (ai, why) talk to a scriptable OpenAI-compatible
//     mock server (NewMockLLM), never to a real provider.
package harness

import (
	"os"
	"path/filepath"
	"testing"
)

// Sandbox is an isolated repository workspace for one test.
type Sandbox struct {
	T *testing.T
	// Root is the absolute path of the repository under test.
	Root string
	// GmDir is <Root>/.glassmarble.
	GmDir string
}

// NewSandbox creates an empty repository directory for a test.
func NewSandbox(t *testing.T) *Sandbox {
	t.Helper()
	root := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("harness: create sandbox root: %v", err)
	}
	return &Sandbox{T: t, Root: root, GmDir: filepath.Join(root, ".glassmarble")}
}

// Path joins a relative path against the sandbox root.
func (s *Sandbox) Path(rel ...string) string {
	return filepath.Join(append([]string{s.Root}, rel...)...)
}

// WriteFile writes a file under the sandbox root, creating parents.
func (s *Sandbox) WriteFile(rel string, content string) string {
	s.T.Helper()
	full := s.Path(rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		s.T.Fatalf("harness: mkdir %s: %v", filepath.Dir(full), err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		s.T.Fatalf("harness: write %s: %v", rel, err)
	}
	return full
}

// ReadFile reads a file under the sandbox root; fails the test on error.
func (s *Sandbox) ReadFile(rel string) string {
	s.T.Helper()
	data, err := os.ReadFile(s.Path(rel))
	if err != nil {
		s.T.Fatalf("harness: read %s: %v", rel, err)
	}
	return string(data)
}

// Exists reports whether a path under the sandbox root exists.
func (s *Sandbox) Exists(rel string) bool {
	_, err := os.Stat(s.Path(rel))
	return err == nil
}

// RequireGit skips the test when the git binary is unavailable.
func (s *Sandbox) RequireGit() {
	s.T.Helper()
	if !HasGit() {
		s.T.Skip("harness: git binary not available on this machine")
	}
}

// Global test-state guard: the in-process CLI runner mutates os.Stdout and
// the working directory, so all suites using it serialize through this mutex
// and must never use t.Parallel().
var cliMutex = make(chan struct{}, 1)

func lockCLI() { cliMutex <- struct{}{} }
func unlockCLI() { <-cliMutex }
