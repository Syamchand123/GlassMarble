package e2e_test

import (
	"strings"
	"testing"

	"github.com/Syamchand123/GlassMarble/tests/harness"
)

// gmb runs one in-process CLI command against the sandbox and fails the
// test on any error. Flags are reset first so state never leaks between
// invocations (the cmd package stores flag values in package-level vars).
func gmb(t *testing.T, sb *harness.Sandbox, args ...string) string {
	t.Helper()
	harness.ResetFlags()
	out, err := harness.RunGmb(t, sb, args...)
	if err != nil {
		t.Fatalf("gmb %v failed: %v\n--- output ---\n%s", args, err, out)
	}
	return out
}

// gmbWant runs a command and asserts the combined output contains every
// fragment. Returns the output for further inspection.
func gmbWant(t *testing.T, sb *harness.Sandbox, want []string, args ...string) string {
	t.Helper()
	out := gmb(t, sb, args...)
	for _, w := range want {
		if !strings.Contains(out, w) {
			t.Errorf("gmb %v output missing %q\n--- output ---\n%s", args, w, out)
		}
	}
	return out
}

// gmbErr runs a command that is expected to fail, returning its output and
// error for inspection.
func gmbErr(t *testing.T, sb *harness.Sandbox, args ...string) (string, error) {
	t.Helper()
	harness.ResetFlags()
	return harness.RunGmb(t, sb, args...)
}
