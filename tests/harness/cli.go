package harness

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Syamchand123/GlassMarble/cmd"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)
// RunResult captures the outcome of one CLI invocation.
type RunResult struct {
	// Stdout is everything the command printed.
	Stdout string
	// Stderr is everything the command printed to stderr.
	Stderr string
	// Err is the error returned by Execute (nil on success).
	Err error
	// Dir is the working directory the command ran in.
	Dir string
}

// OK returns true when the command succeeded.
func (r RunResult) OK() bool { return r.Err == nil }

// Output returns stdout and stderr concatenated.
func (r RunResult) Output() string { return r.Stdout + r.Stderr }

// Contains asserts the combined output contains all fragments.
func (r RunResult) Contains(t *testing.T, fragments ...string) RunResult {
	t.Helper()
	out := r.Output()
	for _, f := range fragments {
		if !strings.Contains(out, f) {
			t.Errorf("output missing %q\n--- output ---\n%s", f, out)
		}
	}
	return r
}

// NotContains asserts the combined output contains none of the fragments.
func (r RunResult) NotContains(t *testing.T, fragments ...string) RunResult {
	t.Helper()
	out := r.Output()
	for _, f := range fragments {
		if strings.Contains(out, f) {
			t.Errorf("output unexpectedly contains %q\n--- output ---\n%s", f, out)
		}
	}
	return r
}

// ExpectError asserts the command failed, returning the error for inspection.
func (r RunResult) ExpectError(t *testing.T) error {
	t.Helper()
	if r.Err == nil {
		t.Errorf("expected command error, got success\n--- output ---\n%s", r.Output())
		return nil
	}
	return r.Err
}

// ExpectSuccess asserts the command succeeded.
func (r RunResult) ExpectSuccess(t *testing.T) RunResult {
	t.Helper()
	if r.Err != nil {
		t.Fatalf("command failed: %v\n--- output ---\n%s", r.Err, r.Output())
	}
	return r
}

// RunGmb executes the CLI IN PROCESS with the given args. The sandbox root
// becomes the working directory and --dir is appended automatically. Tests
// using this helper must NOT call t.Parallel().
//
// Returns (stdout, err) mirroring the shape used throughout cmd tests.
func RunGmb(t *testing.T, sb *Sandbox, args ...string) (string, error) {
	t.Helper()
	lockCLI()
	defer unlockCLI()

	out, err := runGmbLocked(t, sb.Root, args...)
	return out, err
}

// RunGmbInDir is like RunGmb but runs in an arbitrary working directory with
// an explicit --dir flag value. root is the --dir value, workdir is the
// process CWD.
func RunGmbInDir(t *testing.T, root, workdir string, args ...string) (string, error) {
	t.Helper()
	lockCLI()
	defer unlockCLI()
	return runGmbLockedInDir(t, root, workdir, args...)
}

func runGmbLocked(t *testing.T, root string, args ...string) (string, error) {
	return runGmbLockedInDir(t, root, root, args...)
}

func runGmbLockedInDir(t *testing.T, root, workdir string, args ...string) (string, error) {
	t.Helper()
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("harness: getwd: %v", err)
	}
	if err := os.Chdir(workdir); err != nil {
		t.Fatalf("harness: chdir %s: %v", workdir, err)
	}
	defer func() { _ = os.Chdir(oldWd) }()

	// fmt.Printf in command RunE functions writes to os.Stdout directly, so
	// capture it via a temp file: a pipe deadlocks when the writer blocks on
	// a full buffer while the reader only drains after Execute returns.
	oldStdout, oldStderr := os.Stdout, os.Stderr
	tmp, err := os.CreateTemp("", "gmb-out-*.txt")
	if err != nil {
		t.Fatalf("harness: temp file: %v", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	os.Stdout, os.Stderr = tmp, tmp

	command := cmd.RootCmdForTesting()
	command.SetOut(tmp)
	command.SetErr(tmp)
	fullArgs := args
	if hasDirFlag(command, args) {
		fullArgs = append([]string{"--dir", root}, args...)
	}
	command.SetArgs(fullArgs)
	// In-process invocations share one command tree: reset every flag to its
	// declared default before each run so state cannot leak between calls
	// (e.g. a previous `snapshot --create` leaving --create set true).
	resetFlags(command)
	runErr := command.Execute()

	os.Stdout, os.Stderr = oldStdout, oldStderr
	if err := tmp.Close(); err != nil {
		t.Fatalf("harness: close temp file: %v", err)
	}
	direct, err := os.ReadFile(tmpName)
	if err != nil {
		t.Fatalf("harness: read temp file: %v", err)
	}
	return string(direct), runErr
}

// ResetFlags walks the whole command tree and resets every flag to its
// declared default so flag state cannot leak between in-process test
// invocations (cmd stores flag values in package-level variables).
func ResetFlags() {
	resetFlags(cmd.RootCmdForTesting())
}

// resetFlags walks the whole command tree and resets every flag to its
// declared default so flag state cannot leak between test invocations.
func resetFlags(c *cobra.Command) {
	c.Flags().VisitAll(func(f *pflag.Flag) { _ = f.Value.Set(f.DefValue) })
	c.InheritedFlags().VisitAll(func(f *pflag.Flag) { _ = f.Value.Set(f.DefValue) })
	for _, sub := range c.Commands() {
		resetFlags(sub)
	}
}

// hasDirFlag reports whether the command resolved from args declares its own
// --dir flag (most commands do; a few like version/completion do not).
func hasDirFlag(root *cobra.Command, args []string) bool {
	c, _, err := root.Find(args)
	if err != nil || c == nil {
		return false
	}
	return c.Flags().Lookup("dir") != nil || c.InheritedFlags().Lookup("dir") != nil
}

// --- real binary execution ---

var (
	buildOnce sync.Once
	builtPath string
	buildErr  error
)

// BuildBinary compiles the real gmb binary once per test process and returns
// its path. Tests that need true end-to-end fidelity (separate process,
// real locking, real stdout) use this. CGO must be enabled for tree-sitter.
func BuildBinary(t *testing.T) string {
	t.Helper()
	buildOnce.Do(func() {
		root, err := findModuleRoot()
		if err != nil {
			buildErr = err
			return
		}
		dir, err := os.MkdirTemp("", "gmb-bin-*")
		if err != nil {
			buildErr = err
			return
		}
		builtPath = filepath.Join(dir, "gmb"+exeSuffix())
		cmd := exec.Command("go", "build", "-o", builtPath, ".")
		cmd.Dir = root
		out, err := cmd.CombinedOutput()
		if err != nil {
			buildErr = err
			t.Logf("gmb build output: %s", out)
		}
	})
	if buildErr != nil {
		t.Fatalf("harness: building gmb binary: %v", buildErr)
	}
	return builtPath
}

// findModuleRoot walks up from the test working directory (the package
// source dir) to the directory containing go.mod.
func findModuleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("harness: no go.mod found above %s", dir)
		}
		dir = parent
	}
}

func exeSuffix() string {
	if os.PathSeparator == '\\' {
		return ".exe"
	}
	return ""
}

// RunBinary executes the compiled gmb binary as a separate process. env can
// be nil. Returns stdout, stderr, exit code.
func RunBinary(t *testing.T, bin, workdir string, env []string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Dir = workdir
	cmd.Env = append(os.Environ(), env...)
	var so, se bytes.Buffer
	cmd.Stdout, cmd.Stderr = &so, &se
	err := cmd.Run()
	code = 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("harness: running binary: %v", err)
	}
	return so.String(), se.String(), code
}

// HasGit reports whether a usable git binary exists.
func HasGit() bool {
	_, err := exec.LookPath("git")
	return err == nil
}
