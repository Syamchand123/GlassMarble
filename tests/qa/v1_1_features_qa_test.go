package qa_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Syamchand123/GlassMarble/cmd"
	"github.com/Syamchand123/GlassMarble/tests/harness"
)

func TestCommandRegistrationV110(t *testing.T) {
	root := cmd.RootCmd()
	registered := make(map[string]bool)
	for _, c := range root.Commands() {
		registered[c.Name()] = true
	}

	for _, req := range []string{"ui", "lint", "impact"} {
		if !registered[req] {
			t.Errorf("command %q is not registered in rootCmd", req)
		}
	}
}

func TestArchitectureLinterE2E(t *testing.T) {
	sb := harness.NewSandbox(t)
	sb.PolyglotProject()
	sb.GitInit()
	harness.RunGmb(t, sb, "analyze")

	// 1. Test lint --init
	initOut, err := harness.RunGmb(t, sb, "lint", "--init")
	if err != nil {
		t.Fatalf("lint --init failed: %v\n%s", err, initOut)
	}
	if !strings.Contains(initOut, "Created starter architecture rules") {
		t.Errorf("unexpected lint --init output:\n%s", initOut)
	}

	rulesContent := sb.ReadFile(".glassmarble/rules.yaml")
	if !strings.Contains(rulesContent, "Clean Architecture") {
		t.Errorf("scaffolded rules.yaml missing expected content:\n%s", rulesContent)
	}

	// 2. Test lint execution with rules
	lintOut, err := harness.RunGmb(t, sb, "lint", "--json")
	if err != nil {
		// Non-zero exit code is allowed if violations are detected, as long as output is valid JSON
	}

	var res map[string]any
	if err := json.Unmarshal([]byte(lintOut), &res); err != nil {
		t.Fatalf("lint --json produced invalid JSON: %v\n%s", err, lintOut)
	}

	if _, ok := res["rules_total"]; !ok {
		t.Errorf("lint JSON output missing 'rules_total':\n%s", lintOut)
	}
}

func TestRefactoringBlastRadiusImpactE2E(t *testing.T) {
	sb := harness.NewSandbox(t)
	sb.PolyglotProject()
	sb.GitInit()
	harness.RunGmb(t, sb, "analyze")

	// 1. Test gmb impact with JSON output
	impactOut, err := harness.RunGmb(t, sb, "impact", "Cache", "--json")
	if err != nil {
		t.Fatalf("impact --json failed: %v\n%s", err, impactOut)
	}

	var rep map[string]any
	if err := json.Unmarshal([]byte(impactOut), &rep); err != nil {
		t.Fatalf("impact --json produced invalid JSON: %v\n%s", err, impactOut)
	}
	if _, ok := rep["risk_score"]; !ok {
		t.Errorf("impact report missing 'risk_score':\n%s", impactOut)
	}

	// 2. Test gmb impact --visualize (Mermaid format)
	vizOut, err := harness.RunGmb(t, sb, "impact", "Cache", "--visualize")
	if err != nil {
		t.Fatalf("impact --visualize failed: %v\n%s", err, vizOut)
	}
	if !strings.Contains(vizOut, "flowchart") {
		t.Errorf("expected mermaid flowchart output from impact --visualize:\n%s", vizOut)
	}
}
