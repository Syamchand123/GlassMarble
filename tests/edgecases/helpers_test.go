package edgecases_test

// Shared helpers for the edge-case suite. No t.Parallel() anywhere: the
// harness mutates process-global state (os.Stdout, CWD) per invocation.

import (
	"strings"
	"testing"

	"github.com/Syamchand123/GlassMarble/tests/harness"
)

// mustRun asserts the CLI invocation succeeded and returns its output.
func mustRun(t *testing.T, sb *harness.Sandbox, args ...string) string {
	t.Helper()
	out, err := harness.RunGmb(t, sb, args...)
	if err != nil {
		t.Fatalf("gmb %v failed: %v\n--- output ---\n%s", args, err, out)
	}
	return out
}

// mustRunContains asserts success and that the output contains every fragment.
func mustRunContains(t *testing.T, sb *harness.Sandbox, want []string, args ...string) string {
	t.Helper()
	out := mustRun(t, sb, args...)
	for _, w := range want {
		if !strings.Contains(out, w) {
			t.Errorf("gmb %v output missing %q\n--- output ---\n%s", args, w, out)
		}
	}
	return out
}

// mustFailContains asserts the CLI invocation failed and that the combined
// output and error text contains every fragment. Cobra usage errors and
// product-tagged errors reach the caller through the returned error rather
// than stdout, so both streams are checked.
func mustFailContains(t *testing.T, sb *harness.Sandbox, want []string, args ...string) string {
	t.Helper()
	out, err := harness.RunGmb(t, sb, args...)
	if err == nil {
		t.Fatalf("gmb %v unexpectedly succeeded\n--- output ---\n%s", args, out)
	}
	combined := out + "\n" + err.Error()
	for _, w := range want {
		if !strings.Contains(combined, w) {
			t.Errorf("gmb %v error output missing %q (err=%v)\n--- output ---\n%s", args, w, err, out)
		}
	}
	return out
}

// singleGoRepo builds a git repository containing one trivial main.go and
// returns the sandbox. HEAD exists, so analysis runs the tracked-file path.
func singleGoRepo(t *testing.T) *harness.Sandbox {
	t.Helper()
	sb := harness.NewSandbox(t)
	sb.RequireGit()
	sb.GitInit()
	sb.GitCommitFiles("fixture", map[string]string{
		"main.go": "package main\n\nfunc main() {}\n",
	})
	return sb
}

// gitRepoWith builds a git repository containing the given files (committed).
func gitRepoWith(t *testing.T, files map[string]string) *harness.Sandbox {
	t.Helper()
	sb := harness.NewSandbox(t)
	sb.RequireGit()
	sb.GitInit()
	sb.GitCommitFiles("fixture files", files)
	return sb
}

// seedTimelineMemory writes a memory.json whose aggregate carries exactly one
// timeline entry at 2026-08-01 plus a matching "cache" component, so `gmb
// timeline --from/--to` windows and `gmb memory --ask cache` can be exercised
// deterministically. Component state value follows
// developer_memory.StateActive ("CURRENT").
func seedTimelineMemory(t *testing.T, sb *harness.Sandbox) {
	t.Helper()
	const doc = `{
  "project_id": "edgecase-fixture",
  "last_updated": "2026-08-01T10:00:00Z",
  "total_events": 1,
  "timeline": [
    {"timestamp": "2026-08-01T10:00:00Z", "commit_hash": "abcdef1234567890", "title": "add cache layer", "event_kind": "COMMIT", "components": ["cache"], "tags": []}
  ],
  "component_memory": {
    "cache": {"name": "cache", "first_seen": "2026-08-01T10:00:00Z", "last_seen": "2026-08-01T10:00:00Z", "state": "CURRENT", "event_ids": [], "claims": []}
  },
  "global_memory": [],
  "events": []
}`
	sb.WriteFile(".glassmarble/memory/memory.json", doc)
}
