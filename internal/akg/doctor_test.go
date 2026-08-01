package akg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const doctorFixtureTTL = `@prefix gm: <http://glassmarble.org/schema#> .

<http://glassmarble.org/node/metadata> a gm:MetaData ;
    gm:version 7 ;
    gm:schemaVersion 1 ;
    gm:commitHash "abcdef1234567890" ;
    .

<http://glassmarble.org/node/cmd/app/main.go::Main> a gm:Executable ;
    gm:name "Main" ;
    gm:belongsToFile <http://glassmarble.org/file/cmd/app/main.go> ;
    .

<http://glassmarble.org/node/internal/db/db.go::Connect> a gm:Executable ;
    gm:name "Connect" ;
    gm:belongsToFile <http://glassmarble.org/file/internal/db/db.go> ;
    .

<http://glassmarble.org/node/internal/db/db.go::Connect> a gm:Executable ;
    gm:name "Connect duplicate" ;
    .

<http://glassmarble.org/node/cmd/app/main.go::Main> gm:calls <http://glassmarble.org/node/internal/db/db.go::Connect> .
<http://glassmarble.org/node/cmd/app/main.go::Main> gm:calls <http://glassmarble.org/node/ghost.go::Missing> .
<http://glassmarble.org/node/cmd/app/main.go::Main> gm:ghostPredicate <http://glassmarble.org/node/internal/db/db.go::Connect> .
`

func writeDoctorFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".glassmarble")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "akg_state.ttl"), []byte(doctorFixtureTTL), 0o644); err != nil {
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
		t.Fatal("expected initialized=true with TTL present")
	}
	if !rep.LoadOK {
		t.Fatalf("expected parse-back to succeed, got: %s", rep.LoadError)
	}
	if rep.SchemaVersion != 1 {
		t.Errorf("schema version = %d, want 1", rep.SchemaVersion)
	}
	if rep.GraphVersion != 7 {
		t.Errorf("graph version = %d, want 7", rep.GraphVersion)
	}
	if rep.CommitHash != "abcdef1234567890" {
		t.Errorf("commit hash = %q", rep.CommitHash)
	}
	if rep.NodeCount != 2 {
		t.Errorf("node count = %d, want 2 (metadata excluded)", rep.NodeCount)
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
	if len(rep.UnknownTerms) != 1 || rep.UnknownTerms[0] != "ghostPredicate" {
		t.Errorf("unknown terms = %v, want [ghostPredicate]", rep.UnknownTerms)
	}
}

func TestRunDoctorMissingState(t *testing.T) {
	rep, err := RunDoctor(t.TempDir())
	if err != nil {
		t.Fatalf("RunDoctor failed: %v", err)
	}
	if rep.Initialized {
		t.Error("expected initialized=false without a TTL")
	}
}

func TestRunDoctorCorruptTTL(t *testing.T) {
	stateDir := writeDoctorFixture(t)
	corrupt := `@prefix gm: <http://glassmarble.org/schema#> .

<http://glassmarble.org/node/cmd/app/main.go::Main> a gm:Executable ;
    gm:name "Main" ;
`
	// Truncated mid-block: the parser must flag the unterminated statement
	// instead of silently serving a partial graph.
	if err := os.WriteFile(filepath.Join(stateDir, "akg_state.ttl"), []byte(corrupt), 0o644); err != nil {
		t.Fatal(err)
	}
	rep, err := RunDoctor(stateDir)
	if err != nil {
		t.Fatalf("RunDoctor failed: %v", err)
	}
	if rep.LoadOK {
		t.Fatal("expected parse-back to fail on truncated TTL")
	}
	if rep.LoadError == "" {
		t.Error("expected a LoadError message")
	}
}

func TestRunDoctorStaleWAL(t *testing.T) {
	stateDir := writeDoctorFixture(t)
	walDir := filepath.Join(stateDir, "wal")
	if err := os.MkdirAll(walDir, 0o755); err != nil {
		t.Fatal(err)
	}
	wal, err := NewWriteAheadLog(walDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := wal.AppendEntry(&WALEntry{
		TxID:       1,
		CommitHash: "pendinghash",
		Status:     WALStatusCommitted,
	}); err != nil {
		t.Fatal(err)
	}
	rep, err := RunDoctor(stateDir)
	if err != nil {
		t.Fatalf("RunDoctor failed: %v", err)
	}
	if !rep.StaleWAL {
		t.Error("expected StaleWAL=true when WAL is newer than TTL with entries")
	}
	if rep.WALTxCount != 1 || rep.WALCommitted != 1 {
		t.Errorf("WAL counts = %d/%d committed, want 1/1", rep.WALTxCount, rep.WALCommitted)
	}
}
