package e2e_test

// This file is the smoke/health test for the whole suite: if the harness
// itself is broken every other test fails, so verify the fundamentals first.

import (
	"strings"
	"testing"

	"github.com/Syamchand123/GlassMarble/tests/harness"
)

func TestSmokeVersionCommand(t *testing.T) {
	sb := harness.NewSandbox(t)
	out, err := harness.RunGmb(t, sb, "version")
	if err != nil {
		t.Fatalf("version failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "0.1.0") {
		t.Errorf("version output missing version number:\n%s", out)
	}
}

func TestSmokeInitCreatesWorkspace(t *testing.T) {
	sb := harness.NewSandbox(t)
	out, err := harness.RunGmb(t, sb, "init")
	if err != nil {
		t.Fatalf("init failed: %v\n%s", err, out)
	}
	for _, rel := range []string{
		".glassmarble/akg.json",
		".glassmarble/config.yaml",
		".glassmarble/marbles",
		".glassmarble/snapshots",
		".glassmarble/memory",
	} {
		if !sb.Exists(rel) {
			t.Errorf("init did not create %s:\n%s", rel, out)
		}
	}
}

func TestSmokeInitIdempotent(t *testing.T) {
	sb := harness.NewSandbox(t)
	if _, err := harness.RunGmb(t, sb, "init"); err != nil {
		t.Fatalf("first init failed: %v", err)
	}
	out, err := harness.RunGmb(t, sb, "init")
	if err != nil {
		t.Fatalf("second init failed: %v\n%s", err, out)
	}
	if strings.Contains(out, "error") && !strings.Contains(out, "already") {
		t.Errorf("second init should be idempotent (already exists is fine):\n%s", out)
	}
}
