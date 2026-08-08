package cmd_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDriftCommandPass reports no drift for a healthy graph with no config.
func TestDriftCommandPass(t *testing.T) {
	tempDir := t.TempDir()
	writeDoctorState(t, tempDir, doctorFixtureGraph())

	output, err := runGmbCommand(t, "drift", "--dir", tempDir)
	if err != nil {
		t.Fatalf("drift failed: %v\n%s", err, output)
	}
	if !strings.Contains(output, "Architecture Drift Report") {
		t.Errorf("missing report header:\n%s", output)
	}
	if !strings.Contains(output, "RESULT: PASS") {
		t.Errorf("expected PASS result:\n%s", output)
	}
}

// TestDriftCommandJSON emits a machine-readable report.
func TestDriftCommandJSON(t *testing.T) {
	tempDir := t.TempDir()
	writeDoctorState(t, tempDir, doctorFixtureGraph())

	output, err := runGmbCommand(t, "drift", "--dir", tempDir, "--json")
	if err != nil {
		t.Fatalf("drift failed: %v\n%s", err, output)
	}
	for _, want := range []string{`"cycle_budget"`, `"forbidden_dependencies"`, `"cycle_count"`} {
		if !strings.Contains(output, want) {
			t.Errorf("drift JSON missing %s:\n%s", want, output)
		}
	}
}

// TestDriftCommandEmptyDB errors when no AKG database exists.
func TestDriftCommandEmptyDB(t *testing.T) {
	tempDir := t.TempDir()
	_, err := runGmbCommand(t, "drift", "--dir", tempDir)
	if err == nil {
		t.Fatal("expected error for empty AKG database")
	}
	if !strings.Contains(err.Error(), "AKG database is empty") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestDriftCommandForbiddenConfig writes a config.yaml declaring a forbidden
// web->db dependency and a fixture with that cross-layer edge, then verifies
// the violation is surfaced.
func TestDriftCommandForbiddenConfig(t *testing.T) {
	tempDir := t.TempDir()
	writeDoctorState(t, tempDir, doctorFixtureGraph())

	gmDir := filepath.Join(tempDir, ".glassmarble")
	cfgYAML := `drift:
  layers:
    - name: app
      paths: ["cmd/**"]
    - name: db
      paths: ["internal/**"]
  forbidden_deps:
    - source: app
      target: db
  cycle_budget: 0
`
	if err := os.WriteFile(filepath.Join(gmDir, "config.yaml"), []byte(cfgYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	output, err := runGmbCommand(t, "drift", "--dir", tempDir)
	if err == nil {
		t.Fatalf("expected drift to fail on forbidden dependency\n%s", output)
	}
	if !strings.Contains(output, "FORBIDDEN_DEPENDENCY") {
		t.Errorf("expected forbidden dependency violation:\n%s", output)
	}
	if !strings.Contains(output, "RESULT: FAIL") {
		t.Errorf("expected FAIL result:\n%s", output)
	}
}
