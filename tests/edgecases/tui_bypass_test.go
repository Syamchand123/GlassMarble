package edgecases_test

// Non-interactive (TUI-bypass) verification of every headless path in the
// CLI contract — the whole surface must work without any terminal.
//
// Verified against cmd/ (cobra registrations, prompt guards), cmd/analyze.go
// and internal/ui/root.go: the TUI is bypassed whenever the input is not a
// terminal; flags cover the interactive modes.

import (
	"strings"
	"testing"

	"github.com/Syamchand123/GlassMarble/tests/harness"
)

// TestPlainModeAllCommands: drives every headless path end to end in one
// sandbox. Content-level assertions live in dedicated tests below; this one
// only proves none of the commands hang, prompt, or crash back to back.
func TestPlainModeAllCommands(t *testing.T) {
	sb := singleGoRepo(t)
	mustRunContains(t, sb, []string{"GlassMarble Workspace Initialized"}, "init")
	mustRunContains(t, sb, []string{"Analyzed 1 files"}, "analyze")
	mustRunContains(t, sb, []string{"GlassMarble AKG Status"}, "status")
	mustRunContains(t, sb, []string{`"initialized": true`}, "status", "--json")
	mustRunContains(t, sb, []string{"DOCTOR: OK"}, "doctor")
	mustRun(t, sb, "inspect") // no flags on a valid state: help, exit 0
	mustRunContains(t, sb, []string{"=== Entry Points & Callable Symbols ==="}, "inspect", "--list")
	mustRunContains(t, sb, []string{"=== Search Results for 'main' ==="}, "inspect", "--search", "main")
	mustRunContains(t, sb, []string{"Snapshot"}, "snapshot", "--create") // analyze snapshots eagerly; create may report "unchanged"
	mustRunContains(t, sb, []string{"SNAPSHOT ID"}, "snapshot", "--list")
	mustRunContains(t, sb, []string{"Snapshot", "commit:"}, "snapshot", "--at", "HEAD")
	mustRunContains(t, sb, []string{"Architecture diff"}, "snapshot", "--diff", "HEAD", "HEAD")
	mustRun(t, sb, "snapshot", "--replay", "HEAD", "--diagram", "dependency")
	mustRunContains(t, sb, []string{"Developer memory is empty."}, "timeline", "--from", "2006-01-01")
	mustRunContains(t, sb, []string{"The memory holds nothing about \"cache\""}, "memory", "--ask", "cache")
	mustRunContains(t, sb, []string{"Recorded correction"}, "memory", "--correct", "pcache", "--kind", "REJECT", "--reason", "test")
	mustRunContains(t, sb, []string{"1 correction(s) in the audit log"}, "memory", "--corrections")
	mustRun(t, sb, "export", "-o", "out.json")
	if !sb.Exists("out.json") {
		t.Errorf("export did not create out.json")
	}
	mustRunContains(t, sb, []string{"Supported GlassMarble Diagram Types (31 total)"}, "visualize", "list")
	mustRunContains(t, sb, []string{"stateDiagram"}, "visualize", "state")
	mustRun(t, sb, "hooks", "install")
	if !sb.Exists(".git/hooks/post-commit") {
		t.Errorf("hooks install did not create .git/hooks/post-commit")
	}
	mustRun(t, sb, "hooks", "uninstall")
	if sb.Exists(".git/hooks/post-commit") {
		t.Errorf("hooks uninstall left .git/hooks/post-commit behind")
	}
	mustRunContains(t, sb, []string{"Analyzed"}, "analyze")
}

// TestPlainModeTimelineWindow: the seeded memory.json carries exactly one
// timeline event at 2026-08-01; window filters must include or exclude it
// deterministically. JSON mode is used so timestamps are exact.
func TestPlainModeTimelineWindow(t *testing.T) {
	sb := singleGoRepo(t)
	seedTimelineMemory(t, sb)
	mustRunContains(t, sb, []string{"add cache layer"}, "timeline", "--from", "2006-01-01")
	mustRunContains(t, sb, []string{"Aug 2026", "add cache layer"}, "timeline")
	mustRunContains(t, sb, []string{"2026-08-01T10:00:00Z"}, "timeline", "--from", "2006-01-01", "--format", "json")
	out := mustRun(t, sb, "timeline", "--from", "2006-01-01", "--to", "2000-01-01", "--format", "json")
	if strings.Contains(out, "add cache layer") {
		t.Errorf("window [2006-01-01, 2000-01-01] must be empty:\n%s", out)
	}
	out = mustRun(t, sb, "timeline", "--from", "2099-01-01", "--format", "json")
	if strings.Contains(out, "add cache layer") {
		t.Errorf("window [2099-01-01, ...] must be empty:\n%s", out)
	}
	out = mustRun(t, sb, "timeline", "--to", "2006-01-01", "--format", "json")
	if strings.Contains(out, "add cache layer") {
		t.Errorf("window [.., 2006-01-01] must be empty:\n%s", out)
	}
	mustRunContains(t, sb, []string{"add cache layer"}, "timeline", "--component", "cache")
	mustRunContains(t, sb, []string{"2026"}, "timeline", "--component", "cache", "--full")
}

// TestPlainModeMemoryAsk: with the seeded component + timeline, `memory
// --ask` ranks the component and correlates the related timeline entry, while
// the empty-memory project overview stays graceful.
func TestPlainModeMemoryAsk(t *testing.T) {
	sb := singleGoRepo(t)
	seedTimelineMemory(t, sb)
	mustRunContains(t, sb, []string{"Answering \"cache\"", "Components:", "cache", "Related timeline:", "add cache layer"}, "memory", "--ask", "cache")
	mustRunContains(t, sb, []string{"edgecase-fixture", "1 event(s)", "1 component(s)"}, "memory")
}

// TestPlainModeExportCleanup: exporting a fresh graph succeeds and the file
// is a valid GraphJSON document.
func TestPlainModeExportCleanup(t *testing.T) {
	sb := singleGoRepo(t)
	mustRun(t, sb, "init")
	mustRun(t, sb, "analyze")
	mustRun(t, sb, "export", "-o", "out.json", "-f", "graphjson")
	raw := sb.ReadFile("out.json")
	if !strings.Contains(raw, `"schema_version": 3`) {
		t.Errorf("exported document missing schema version:\n%s", raw)
	}
}

// TestPlainModeCompletionShells: every supported completion shell exits 0
// with non-empty output.
func TestPlainModeCompletionShells(t *testing.T) {
	for _, shell := range []string{"bash", "fish", "powershell", "zsh"} {
		t.Run(shell, func(t *testing.T) {
			sb := harness.NewSandbox(t)
			out, err := harness.RunGmb(t, sb, "completion", shell)
			if err != nil {
				t.Fatalf("completion %s failed: %v\n%s", shell, err, out)
			}
			if strings.TrimSpace(out) == "" {
				t.Errorf("completion %s produced empty output", shell)
			}
		})
	}
}

// TestInspectNoArgsEmptyGraph: `inspect` with no flags on an initialized but
// empty graph prints help and exits 0.
func TestInspectNoArgsEmptyGraph(t *testing.T) {
	sb := harness.NewSandbox(t)
	mustRun(t, sb, "init")
	out := mustRunContains(t, sb, []string{"Usage:"}, "inspect")
	if strings.Contains(out, "panic") {
		t.Errorf("inspect without args panicked:\n%s", out)
	}
}

// TestInspectNoArgsNoState: `inspect` with no akg.json at all reports the
// empty-database hint even before the help fallback.
func TestInspectNoArgsNoState(t *testing.T) {
	sb := harness.NewSandbox(t)
	mustFailContains(t, sb, []string{"AKG database is empty", "gmb analyze"}, "inspect")
}