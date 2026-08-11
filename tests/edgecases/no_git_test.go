package edgecases_test

// Behavior outside a git repository. Deviations from the docs, verified
// against cmd/init.go, cmd/analyze.go, cmd/status.go, cmd/snapshot.go,
// cmd/hooks.go:
//
//  1. `gmb init` succeeds without git (creates the workspace and writes an
//     empty-but-valid v3 akg.json).
//  2. `gmb analyze` succeeds without git: there is no .git directory, so
//     GitTrackedOnly is never enabled and every file is ingested (analyze.go
//     toggles GitTrackedOnly only when .git exists).
//  3. `gmb status` renders the initialized dashboard after init; only a raw
//     sandbox is "Uninitialized".
//  4. Snapshot modes that need the graph or git resolve cleanly to the
//     empty/not-resolvable errors rather than panicking.
//  5. `gmb hooks` validates the .git/hooks directory before the subcommand
//     verb, so a non-git sandbox fails with the missing-hooks error.

import (
	"strings"
	"testing"

	"github.com/Syamchand123/GlassMarble/tests/harness"
)

// TestHooksNoGit: on a plain sandbox, hooks install fails with the
// ".git/hooks missing" error — install is a no-go without git.
func TestHooksNoGit(t *testing.T) {
	sb := harness.NewSandbox(t)
	mustFailContains(t, sb, []string{"not a git repository or .git/hooks missing"}, "hooks", "install")
}

// TestGitBackedCommandsUninitializedState: snapshot modes on a raw sandbox
// fail with context-appropriate errors instead of panicking.
func TestGitBackedCommandsUninitializedState(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want []string
	}{
		{"at-head", []string{"snapshot", "--at", "HEAD"}, []string{"no snapshot for \"HEAD\"", "not a resolvable git ref"}},
		{"diff", []string{"snapshot", "--diff", "nonexistent", "other"}, []string{"cannot resolve base ref \"nonexistent\"", "no snapshot for \"nonexistent\"", "not a resolvable git ref"}},
		{"replay", []string{"snapshot", "--replay", "HEAD"}, []string{"no snapshot for \"HEAD\"", "not a resolvable git ref"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sb := harness.NewSandbox(t)
			mustFailContains(t, sb, tc.want, tc.args...)
		})
	}
}

// TestSnapshotCreateEmptyGraph: creating a snapshot without a graph errors
// with the empty-database hint (run 'gmb analyze' first).
func TestSnapshotCreateEmptyGraph(t *testing.T) {
	sb := harness.NewSandbox(t)
	mustRun(t, sb, "init")
	mustFailContains(t, sb, []string{"database is empty", "gmb analyze"}, "snapshot", "--create")
}

// TestTimelineEmptyMemory: `timeline` with no memory renders the empty-state
// notice and succeeds.
func TestTimelineEmptyMemory(t *testing.T) {
	sb := harness.NewSandbox(t)
	mustRunContains(t, sb, []string{"Developer memory is empty."}, "timeline", "--from", "2006-01-01")
}

// TestAnalyzeNoGit: analysis works on a non-git tree, ingesting every file
// (no .git means GitTrackedOnly is never enabled).
func TestAnalyzeNoGit(t *testing.T) {
	sb := harness.NewSandbox(t)
	sb.WriteFile("main.go", "package main\n\nfunc main() {}\n")
	out := mustRunContains(t, sb, []string{"Analyzed 1 files"}, "analyze")
	if strings.Contains(out, "stage 1 ingestion failed") {
		t.Errorf("analyze failed without git:\n%s", out)
	}
}

// TestInitNoGit: init succeeds without git and status afterwards renders the
// initialized dashboard.
func TestInitNoGit(t *testing.T) {
	sb := harness.NewSandbox(t)
	mustRunContains(t, sb, []string{"GlassMarble Workspace Initialized"}, "init")
	mustRunContains(t, sb, []string{"GlassMarble AKG Status"}, "status")
}