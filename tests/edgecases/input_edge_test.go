package edgecases_test

// Input-shape edge cases: degenerate/malformed/foreign CLI arguments that
// must be handled gracefully rather than panicking or silently corrupting
// state.
//
// Verified against cmd/ (cobra flag declarations, arg counts, mode selection)
// and internal/akg/graph_json.go:
//
//  1. `snapshot` mode selection: exactly one of --create/--list/--at/--diff/
//     --replay; a bare run, conflicting modes, an over-long --diff and
//     unknown flags are each rejected before any store work.
//  2. `why` and `import` enforce arg counts (cobra ExactArgs); `ai` rejects an
//     empty question before any model work.
//  3. `export` requires --output, checks the graph non-empty, then validates
//     the format and extension.
//  4. `visualize` rejects unknown diagram types, requires --entry for
//     sequence diagrams, and reports a missing state file.
//  5. `memory --correct` validates kinds and requires a value.
//  6. `completion` accepts bash|zsh|fish|powershell; anything else re-prints
//     the plain help (no error); zero args is a cobra arg-count error.
//  7. `hooks` validates the verb (install|uninstall) and the arg count.

import (
	"strings"
	"testing"

	"github.com/Syamchand123/GlassMarble/tests/harness"
)

// TestSnapshotBadModeSelection: impossible mode combinations are rejected
// before they consult the snapshot store.
func TestSnapshotBadModeSelection(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want []string
	}{
		{"no-mode", []string{"snapshot"}, []string{"no mode selected", "--create, --list, --at, --diff or --replay"}},
		{"conflicting-modes", []string{"snapshot", "--at", "HEAD", "--list"}, []string{"only one mode may be used at a time"}},
		{"diff-three-refs", []string{"snapshot", "--diff", "one", "two", "three"}, []string{"--diff requires exactly two refs"}},
		{"diff-one-ref", []string{"snapshot", "--diff", "one"}, []string{"--diff requires exactly two refs"}},
		{"positional-as-diff", []string{"snapshot", "list"}, []string{"--diff requires exactly two refs"}},
		{"unknown-flag", []string{"snapshot", "--create", "--bogus"}, []string{"unknown flag: --bogus"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sb := harness.NewSandbox(t)
			mustFailContains(t, sb, tc.want, tc.args...)
		})
	}
}

// TestWhyNoArgs: `why` with zero arguments is a cobra arg-count error.
func TestWhyNoArgs(t *testing.T) {
	sb := harness.NewSandbox(t)
	mustFailContains(t, sb, []string{"accepts 1 arg(s)"}, "why")
}

// TestImportNoArgs: `import` with zero arguments is a cobra arg-count error.
func TestImportNoArgs(t *testing.T) {
	sb := harness.NewSandbox(t)
	mustFailContains(t, sb, []string{"accepts 1 arg(s)"}, "import")
}

// TestImportMissingFile: a nonexistent import file is reported cleanly.
func TestImportMissingFile(t *testing.T) {
	sb := harness.NewSandbox(t)
	mustFailContains(t, sb, []string{"failed to open import file"}, "import", "nope.json")
}

// TestAIEmptyQuestion: `ai` with no question is rejected by validation.
func TestAIEmptyQuestion(t *testing.T) {
	sb := harness.NewSandbox(t)
	mustFailContains(t, sb, []string{"no question provided"}, "ai")
}

// TestExportRequiresOutput: `export` without --output prints the required
// flag message.
func TestExportRequiresOutput(t *testing.T) {
	sb := harness.NewSandbox(t)
	mustFailContains(t, sb, []string{"--output is required"}, "export")
}

// TestExportEmptyGraph: exporting an empty (init-only) state fails with the
// empty-database hint before any file is created.
func TestExportEmptyGraph(t *testing.T) {
	sb := harness.NewSandbox(t)
	mustRun(t, sb, "init")
	mustFailContains(t, sb, []string{"AKG database is empty", "glassmarble analyze"}, "export", "-o", "out.json")
	if sb.Exists("out.json") {
		t.Errorf("export created a file for an empty graph")
	}
}

// TestExportUnknownFormat: on a populated graph an unsupported format is
// rejected with the supported list (checked after the graph non-empty gate).
func TestExportUnknownFormat(t *testing.T) {
	sb := singleGoRepo(t)
	mustRun(t, sb, "init")
	mustRun(t, sb, "analyze")
	mustFailContains(t, sb, []string{"unsupported export format", "graphjson, neo4j"}, "export", "-o", "out.json", "-f", "bogus")
}

// TestExportExtensionMismatch: a default graphjson export to a non-.json
// file is rejected by the extension check.
func TestExportExtensionMismatch(t *testing.T) {
	sb := singleGoRepo(t)
	mustRun(t, sb, "init")
	mustRun(t, sb, "analyze")
	mustFailContains(t, sb, []string{"unsupported extension", "use .json"}, "export", "-o", "out.txt")
}

// TestVisualizeUnknownDiagramType: unknown diagram type names are rejected.
func TestVisualizeUnknownDiagramType(t *testing.T) {
	sb := harness.NewSandbox(t)
	mustFailContains(t, sb, []string{"unsupported diagram type 'bogus'"}, "visualize", "bogus")
}

// TestVisualizeSequenceRequiresEntry: sequence diagrams demand --entry
// (checked before any state file access).
func TestVisualizeSequenceRequiresEntry(t *testing.T) {
	sb := harness.NewSandbox(t)
	mustFailContains(t, sb, []string{"--entry", "mandatory for UML Sequence"}, "visualize", "sequence")
}

// TestVisualizeNoStateFile: with a valid type but no akg.json the missing
// state file is reported.
func TestVisualizeNoStateFile(t *testing.T) {
	sb := harness.NewSandbox(t)
	mustFailContains(t, sb, []string{"active AKG database not found"}, "visualize", "state")
}

// TestVisualizeScopeBogus: a malformed --scope value is rejected after the
// type/state gates pass.
func TestVisualizeScopeBogus(t *testing.T) {
	sb := singleGoRepo(t)
	mustRun(t, sb, "init")
	mustRun(t, sb, "analyze")
	mustFailContains(t, sb, []string{"invalid scope"}, "visualize", "state", "--scope", "bogus")
}

// TestMemoryCorrectRequiresValue: INTENT corrections demand --value.
func TestMemoryCorrectRequiresValue(t *testing.T) {
	sb := harness.NewSandbox(t)
	mustFailContains(t, sb, []string{"--value is required for INTENT corrections"}, "memory", "--correct", "foo")
}

// TestMemoryInvalidKind: unknown correction kinds are rejected.
func TestMemoryInvalidKind(t *testing.T) {
	sb := harness.NewSandbox(t)
	mustFailContains(t, sb, []string{"unknown correction kind \"BOGUS\""}, "memory", "--correct", "foo", "--kind", "BOGUS")
}

// TestCompletionBadShellShowsHelp: an unsupported shell re-prints the plain
// help and exits 0 (Fang is bypassed so the output stays ANSI-free).
func TestCompletionBadShellShowsHelp(t *testing.T) {
	sb := harness.NewSandbox(t)
	out := mustRunContains(t, sb, []string{"Shells:", "bash"}, "completion", "elvish")
	if strings.Contains(out, "\x1b[") {
		t.Errorf("completion help leaked ANSI escapes:\n%q", out)
	}
}

// TestCompletionNoArgs: `completion` with zero arguments is a cobra
// arg-count error.
func TestCompletionNoArgs(t *testing.T) {
	sb := harness.NewSandbox(t)
	mustFailContains(t, sb, []string{"accepts 1 arg(s)"}, "completion")
}

// TestHooksUnknownSubcommand: a bad verb on a git sandbox is rejected; on a
// non-git sandbox the missing-hooks check fires first.
func TestHooksUnknownSubcommand(t *testing.T) {
	t.Run("git-repo-bad-verb", func(t *testing.T) {
		sb := singleGoRepo(t)
		mustFailContains(t, sb, []string{"unknown hooks subcommand \"frobnicate\""}, "hooks", "frobnicate")
	})
	t.Run("no-git-missing-hooks", func(t *testing.T) {
		sb := harness.NewSandbox(t)
		mustFailContains(t, sb, []string{"not a git repository or .git/hooks missing"}, "hooks", "frobnicate")
	})
}

// TestHooksArgCount: `hooks` with two arguments is a cobra arg-count error.
func TestHooksArgCount(t *testing.T) {
	sb := harness.NewSandbox(t)
	mustFailContains(t, sb, []string{"accepts 1 arg(s)"}, "hooks", "install", "pre-commit")
}