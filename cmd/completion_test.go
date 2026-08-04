package cmd_test

import (
	"strings"
	"testing"
)

// TestCompletionHelpNoANSI verifies `gmb completion --help` bypasses Fang's
// styled help wrapper and emits byte-clean output that is safe to pipe into a
// shell session (§A.2). Fang prints styled help through the root command's
// help func; this test locks the ANSI-free guarantee.
func TestCompletionHelpNoANSI(t *testing.T) {
	output, err := runGmbCommand(t, "completion", "--help")
	if err != nil {
		t.Fatalf("completion --help failed: %v\n%s", err, output)
	}
	if strings.Contains(output, "\x1b[") {
		t.Errorf("completion --help leaked ANSI escapes:\n%q", output)
	}
	for _, want := range []string{"bash", "zsh", "fish", "powershell", "Usage:"} {
		if !strings.Contains(output, want) {
			t.Errorf("completion help missing %q:\n%s", want, output)
		}
	}
}

// TestCompletionBashScriptClean ensures the generated bash completion script
// itself carries no ANSI escapes (Fang must not wrap its output either).
func TestCompletionBashScriptClean(t *testing.T) {
	output, err := runGmbCommand(t, "completion", "bash")
	if err != nil {
		t.Fatalf("completion bash failed: %v\n%s", err, output)
	}
	if strings.Contains(output, "\x1b[") {
		t.Errorf("completion bash leaked ANSI escapes:\n%q", output)
	}
	if !strings.Contains(output, "__start_gmb") {
		t.Errorf("expected bash completion script, got:\n%s", output)
	}
}
