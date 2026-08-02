package cmd_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestExportCommandJSON writes the doctor fixture as GraphJSON.
func TestExportCommandJSON(t *testing.T) {
	tempDir := t.TempDir()
	writeDoctorState(t, tempDir, doctorFixtureTTL)

	outPath := filepath.Join(tempDir, "graph.json")
	output, err := runGmbCommand(t, "export", "--dir", tempDir, "--output", outPath)
	if err != nil {
		t.Fatalf("export failed: %v\n%s", err, output)
	}
	if !strings.Contains(output, "Exported AKG snapshot") {
		t.Errorf("missing export confirmation:\n%s", output)
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("output file not written: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if doc["nodes"] == nil {
		t.Errorf("export missing nodes array")
	}
}

// TestExportCommandMissingOutput errors when --output is absent.
func TestExportCommandMissingOutput(t *testing.T) {
	tempDir := t.TempDir()
	writeDoctorState(t, tempDir, doctorFixtureTTL)

	_, err := runGmbCommand(t, "export", "--dir", tempDir)
	if err == nil {
		t.Fatal("expected error for missing --output")
	}
	if !strings.Contains(err.Error(), "--output is required") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestExportCommandEmptyDB errors when no database exists.
func TestExportCommandEmptyDB(t *testing.T) {
	tempDir := t.TempDir()
	_, err := runGmbCommand(t, "export", "--dir", tempDir, "--output", "g.json")
	if err == nil {
		t.Fatal("expected error for empty AKG database")
	}
	if !strings.Contains(err.Error(), "AKG database is empty") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestImportCommandRoundTrip exports the fixture, clears the database, imports
// it back, and verifies the status reports the restored graph.
func TestImportCommandRoundTrip(t *testing.T) {
	tempDir := t.TempDir()
	writeDoctorState(t, tempDir, doctorFixtureTTL)

	outPath := filepath.Join(tempDir, "graph.json")
	if _, err := runGmbCommand(t, "export", "--dir", tempDir, "--output", outPath); err != nil {
		t.Fatalf("export failed: %v", err)
	}

	// Remove the on-disk state so import starts from an empty database.
	gmDir := filepath.Join(tempDir, ".glassmarble")
	if err := os.Remove(filepath.Join(gmDir, "akg_state.ttl")); err != nil {
		t.Fatalf("failed to clear state: %v", err)
	}

	output, err := runGmbCommand(t, "import", filepath.Join(tempDir, "graph.json"), "--dir", tempDir)
	if err != nil {
		t.Fatalf("import failed: %v\n%s", err, output)
	}
	if !strings.Contains(output, "Imported AKG snapshot") {
		t.Errorf("missing import confirmation:\n%s", output)
	}

	status, err := runGmbCommand(t, "status", "--dir", tempDir)
	if err != nil {
		t.Fatalf("status failed after import: %v\n%s", err, status)
	}
	if !strings.Contains(status, "Nodes Count:   2") {
		t.Errorf("status after import missing nodes:\n%s", status)
	}
}
