package cmd_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCompareCommandTwoFiles diffs two exported snapshots.
func TestCompareCommandTwoFiles(t *testing.T) {
	tempDir := t.TempDir()
	writeDoctorState(t, tempDir, doctorFixtureGraph())

	// Export the base fixture, then add a node via import to build a head.
	basePath := filepath.Join(tempDir, "base.json")
	if _, err := runGmbCommand(t, "export", "--dir", tempDir, "--output", basePath); err != nil {
		t.Fatalf("base export failed: %v", err)
	}

	// Build a head JSON with an extra node and edge by importing the base,
	// then re-exporting from a second database? Simpler: hand-craft a head
	// GraphJSON document derived from the fixture.
	headPath := filepath.Join(tempDir, "head.json")
	headDoc := `{
  "schema_version": 3,
  "commit_hash": "headhash",
  "version": 8,
  "nodes": [
    {"id": "cmd/app/main.go::Main", "kind": "FUNCTION", "name": "Main", "file_spec": {"path": "cmd/app/main.go"}},
    {"id": "internal/db/db.go::Connect", "kind": "FUNCTION", "name": "Connect", "file_spec": {"path": "internal/db/db.go"}},
    {"id": "internal/db/db.go::Pool", "kind": "STRUCT", "name": "Pool", "file_spec": {"path": "internal/db/db.go"}}
  ],
  "edges": [
    {"source_id": "cmd/app/main.go::Main", "target_id": "internal/db/db.go::Connect", "type": "CALLS", "line_number": 5},
    {"source_id": "cmd/app/main.go::Main", "target_id": "internal/db/db.go::Pool", "type": "INSTANTIATES_GENERIC", "line_number": 7}
  ]
}`
	if err := os.WriteFile(headPath, []byte(headDoc), 0o644); err != nil {
		t.Fatal(err)
	}

	output, err := runGmbCommand(t, "compare", basePath, headPath)
	if err != nil {
		t.Fatalf("compare failed: %v\n%s", err, output)
	}
	for _, want := range []string{"AKG Architecture Diff", "Nodes added:   1", "Edges added:   1", "Pool"} {
		if !strings.Contains(output, want) {
			t.Errorf("compare output missing %q:\n%s", want, output)
		}
	}
}

// TestCompareCommandJSON emits machine-readable diff JSON.
func TestCompareCommandJSON(t *testing.T) {
	tempDir := t.TempDir()
	writeDoctorState(t, tempDir, doctorFixtureGraph())

	basePath := filepath.Join(tempDir, "base.json")
	if _, err := runGmbCommand(t, "export", "--dir", tempDir, "--output", basePath); err != nil {
		t.Fatalf("base export failed: %v", err)
	}
	headPath := filepath.Join(tempDir, "head.json")
	if err := os.WriteFile(headPath, []byte(`{"schema_version":3,"commit_hash":"h","version":1,"nodes":[],"edges":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}

	output, err := runGmbCommand(t, "compare", basePath, headPath, "--json")
	if err != nil {
		t.Fatalf("compare failed: %v\n%s", err, output)
	}
	for _, want := range []string{`"nodes_removed"`, `"edges_removed"`, `"files_changed"`} {
		if !strings.Contains(output, want) {
			t.Errorf("compare JSON missing %s:\n%s", want, output)
		}
	}
}

// TestCompareCommandMissingFiles errors on a bad argument count.
func TestCompareCommandMissingFiles(t *testing.T) {
	_, err := runGmbCommand(t, "compare", "only-one.json")
	if err == nil {
		t.Fatal("expected error for a single file argument")
	}
}
