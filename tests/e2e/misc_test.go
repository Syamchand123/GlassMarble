package e2e_test

// Fast, analysis-free CLI checks: version, completion, hooks, housekeeping
// and uninitialized-workspace behavior on seeded/empty sandboxes. In-process
// runner, so no t.Parallel().

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Syamchand123/GlassMarble/tests/harness"
)

func TestVersionCommand(t *testing.T) {
	sb := harness.NewSandbox(t)
	gmbWant(t, sb, []string{"GlassMarble", "0.1.0", "AI Architecture Intelligence"}, "version")
}

func TestCompletionScripts(t *testing.T) {
	sb := harness.NewSandbox(t)
	for _, shell := range []string{"bash", "zsh", "fish", "powershell"} {
		out := gmb(t, sb, "completion", shell)
		if !strings.Contains(out, "gmb") {
			t.Errorf("completion %s missing gmb mentions", shell)
		}
	}
	bash := gmb(t, sb, "completion", "bash")
	if !strings.Contains(bash, "__start_gmb") {
		t.Errorf("bash completion missing __start_gmb:\n%s", bash)
	}
}

func TestHooksInstallUninstall(t *testing.T) {
	sb := harness.NewSandbox(t)
	sb.RequireGit()
	sb.SampleProject()
	sb.GitInit()

	// Not a git repo fails with a clear message.
	plain := harness.NewSandbox(t)
	out, err := gmbErr(t, plain, "hooks", "install")
	if err == nil {
		t.Errorf("hooks install outside a git repo should fail:\n%s", out)
	}

	gmbWant(t, sb, []string{"Git Hook Installed", "post-commit"}, "hooks", "install")
	hook := sb.ReadFile(filepath.Join(".git", "hooks", "post-commit"))
	if !strings.Contains(hook, "analyze") {
		t.Errorf("hook script does not run analyze:\n%s", hook)
	}

	// Install is idempotent (overwrites).
	gmbWant(t, sb, []string{"Git Hook Installed"}, "hooks", "install")

	gmbWant(t, sb, []string{"uninstalled successfully"}, "hooks", "uninstall")
	if sb.Exists(filepath.Join(".git", "hooks", "post-commit")) {
		t.Errorf("hook still present after uninstall")
	}

	// Uninstalling again reports "no hook".
	gmbWant(t, sb, []string{"No active GlassMarble post-commit hook"}, "hooks", "uninstall")
}

func TestHousekeepingReport(t *testing.T) {
	sb := harness.NewSandbox(t)
	sb.WriteAKGState(harness.TinyGraph())
	sb.WriteFile(".glassmarble/marbles/dep.md", "```mermaid\ngraph TD\n```\n")

	out := gmbWant(t, sb, []string{".glassmarble Working Set", "Total"}, "housekeeping")
	if !strings.Contains(out, "marbles") {
		t.Errorf("housekeeping report should list the marbles area:\n%s", out)
	}
}

func TestUninitializedWorkspaceBehavior(t *testing.T) {
	sb := harness.NewSandbox(t)

	// status/doctor report uninitialized instead of failing.
	gmbWant(t, sb, []string{"GlassMarble Status: Uninitialized"}, "status")
	gmbWant(t, sb, []string{"Uninitialized"}, "doctor")

	// Analysis-backed commands fail with a clear message.
	for _, args := range [][]string{
		{"inspect", "--list"},
		{"dependency"},
		{"hotspot", "--top", "5"},
		{"patterns"},
		{"visualize", "dependency"},
		{"export", "--output", "x.json"},
	} {
		out, err := gmbErr(t, sb, args...)
		if err == nil {
			t.Errorf("gmb %v on an empty workspace should fail:\n%s", args, out)
		}
	}

	// stats on an empty workspace says no telemetry.
	gmbWant(t, sb, []string{"No telemetry found"}, "stats")

	// housekeeping on an empty workspace is still fine.
	gmb(t, sb, "housekeeping")
}
