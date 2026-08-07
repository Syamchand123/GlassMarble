package akg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const doctorFixtureJSON = `{
  "schema_version": 3,
  "commit_hash": "abcdef1234567890",
  "version": 7,
  "entrypoints": ["cmd/app/main.go::Main"],
  "nodes": [
    {
      "id": "cmd/app/main.go::Main",
      "kind": "FUNCTION",
      "name": "Main",
      "file_spec": { "path": "cmd/app/main.go" }
    },
    {
      "id": "internal/db/db.go::Connect",
      "kind": "FUNCTION",
      "name": "Connect",
      "file_spec": { "path": "internal/db/db.go" }
    },
    {
      "id": "internal/db/db.go::Connect",
      "kind": "FUNCTION",
      "name": "Connect duplicate",
      "file_spec": { "path": "internal/db/db.go" }
    }
  ],
  "edges": [
    {
      "source_id": "cmd/app/main.go::Main",
      "target_id": "internal/db/db.go::Connect",
      "type": "CALLS"
    },
    {
      "source_id": "cmd/app/main.go::Main",
      "target_id": "cmd/app/ghost.go::Missing",
      "type": "CALLS"
    },
    {
      "source_id": "cmd/app/main.go::Main",
      "target_id": "internal/db/db.go::Connect",
      "type": "GHOSTPREDICATE"
    }
  ]
}
`

func writeDoctorFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".glassmarble")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "akg.json"), []byte(doctorFixtureJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	return stateDir
}

func TestRunDoctorHealthyState(t *testing.T) {
	stateDir := writeDoctorFixture(t)
	rep, err := RunDoctor(stateDir)
	if err != nil {
		t.Fatalf("RunDoctor failed: %v", err)
	}
	if !rep.Initialized {
		t.Fatal("expected initialized=true with state present")
	}
	if !rep.LoadOK {
		t.Fatalf("expected parse-back to succeed, got: %s", rep.LoadError)
	}
	if rep.SchemaVersion != 3 {
		t.Errorf("schema version = %d, want 3", rep.SchemaVersion)
	}
	if rep.GraphVersion != 7 {
		t.Errorf("graph version = %d, want 7", rep.GraphVersion)
	}
	if rep.CommitHash != "abcdef1234567890" {
		t.Errorf("commit hash = %q", rep.CommitHash)
	}
	if rep.NodeCount != 2 {
		t.Errorf("node count = %d, want 2 (duplicate ID collapses on restore)", rep.NodeCount)
	}
	if rep.EdgeCount != 3 {
		t.Errorf("edge count = %d, want 3", rep.EdgeCount)
	}
	if rep.Dangling != 1 {
		t.Errorf("dangling = %d, want 1 (ghost.go::Missing)", rep.Dangling)
	}
	if len(rep.DuplicateIDs) != 1 || !strings.HasSuffix(rep.DuplicateIDs[0], "Connect") {
		t.Errorf("duplicate IDs = %v, want 1 duplicate for Connect", rep.DuplicateIDs)
	}
}

func TestRunDoctorMissingState(t *testing.T) {
	rep, err := RunDoctor(t.TempDir())
	if err != nil {
		t.Fatalf("RunDoctor failed: %v", err)
	}
	if rep.Initialized {
		t.Error("expected initialized=false without a state file")
	}
}

func TestRunDoctorCorruptState(t *testing.T) {
	stateDir := writeDoctorFixture(t)
	corrupt := `{
  "schema_version": 3,
  "commit_hash": "abcdef1234567890",
  "version": 7,
  "nodes": [
    {
      "id": "cmd/app/main.go::Main",
      "kind": "FUNCTION",
      "name": "Main",
      "file_spec": { "path": "cmd/app/main.go" }
  ]
}
`
	// Malformed document: the doctor must flag the corruption instead of
	// silently serving a partial graph.
	if err := os.WriteFile(filepath.Join(stateDir, "akg.json"), []byte(corrupt), 0o644); err != nil {
		t.Fatal(err)
	}
	rep, err := RunDoctor(stateDir)
	if err != nil {
		t.Fatalf("RunDoctor failed: %v", err)
	}
	if rep.LoadOK {
		t.Fatal("expected parse-back to fail on corrupt state")
	}
	if rep.LoadError == "" {
		t.Error("expected a LoadError message")
	}
}
