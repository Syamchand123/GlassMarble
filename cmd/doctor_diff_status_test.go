package cmd_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Syamchand123/GlassMarble/cmd"
	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// doctorFixtureGraph returns a two-node, one-edge GraphJSON state fixture
// (schema 3) shared across the CLI test suite.
func doctorFixtureGraph() *akg.GraphJSON {
	return &akg.GraphJSON{
		SchemaVersion: akg.CurrentSchemaVersion,
		Version:       7,
		CommitHash:    "abcdef1234567890",
		Nodes: []akg.GraphNodeJSON{
			{ID: "cmd/app/main.go::Main", Kind: "EXECUTABLE", Name: "Main", FileSpec: akg.LocationMetaJSON{Path: "cmd/app/main.go", LineStart: 1, LineEnd: 5}},
			{ID: "internal/db/db.go::Connect", Kind: "EXECUTABLE", Name: "Connect", FileSpec: akg.LocationMetaJSON{Path: "internal/db/db.go", LineStart: 1, LineEnd: 3}},
		},
		Edges: []akg.GraphEdgeJSON{
			{SourceID: "cmd/app/main.go::Main", TargetID: "internal/db/db.go::Connect", Type: "CALLS", LineNumber: 3},
		},
	}
}

func writeDoctorState(t *testing.T, dir string, g *akg.GraphJSON) {
	t.Helper()
	gmDir := filepath.Join(dir, ".glassmarble")
	if err := os.MkdirAll(gmDir, 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.MarshalIndent(g, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gmDir, "akg.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func runGmbCommand(t *testing.T, args ...string) (string, error) {
	t.Helper()
	// fmt.Printf in command RunE functions writes to os.Stdout directly, so
	// capture it with a temp file instead of a pipe. A pipe deadlocks when the
	// writer blocks on a full 64KB buffer while the reader only drains after
	// Execute returns (e.g. the large `gmb completion bash` script).
	var out strings.Builder
	oldStdout := os.Stdout
	tmp, err := os.CreateTemp("", "gmb-out-*.txt")
	if err != nil {
		t.Fatal(err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	os.Stdout = tmp
	defer func() { os.Stdout = oldStdout }()
	command := cmd.RootCmdForTesting()
	// The root command is a package-level singleton; flag values set by a
	// prior test invocation persist across Execute calls (e.g. a --json=true
	// from one test would leak into the next). Walk the whole command tree and
	// reset every flag back to its declared default so each test starts clean.
	var resetFlags func(c *cobra.Command)
	resetFlags = func(c *cobra.Command) {
		c.Flags().VisitAll(func(f *pflag.Flag) {
			_ = f.Value.Set(f.DefValue)
		})
		c.InheritedFlags().VisitAll(func(f *pflag.Flag) {
			_ = f.Value.Set(f.DefValue)
		})
		for _, sub := range c.Commands() {
			resetFlags(sub)
		}
	}
	resetFlags(command)
	command.SetOut(tmp)
	command.SetErr(tmp)
	command.SetArgs(args)
	runErr := command.Execute()
	os.Stdout = oldStdout
	if err := tmp.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(tmpName)
	if err != nil {
		t.Fatal(err)
	}
	out.Write(data)
	// Cobra appends a trailing newline after help output; read the file
	// contents we captured (already includes it) and return it.
	return out.String(), runErr
}

func TestDoctorCommandHealthy(t *testing.T) {
	tempDir := t.TempDir()
	writeDoctorState(t, tempDir, doctorFixtureGraph())

	output, err := runGmbCommand(t, "doctor", "--dir", tempDir)
	if err != nil {
		t.Fatalf("doctor command failed: %v\n%s", err, output)
	}
	for _, want := range []string{"DOCTOR: OK", "Schema:        v3", "Parse-back:    ok", "Dangling:      0"} {
		if !strings.Contains(output, want) {
			t.Errorf("doctor output missing %q:\n%s", want, output)
		}
	}
}

func TestDoctorCommandUninitialized(t *testing.T) {
	tempDir := t.TempDir()
	output, err := runGmbCommand(t, "doctor", "--dir", tempDir)
	if err != nil {
		t.Fatalf("doctor command failed: %v\n%s", err, output)
	}
	if !strings.Contains(output, "Uninitialized") {
		t.Errorf("expected Uninitialized report:\n%s", output)
	}
}

func TestDoctorCommandCorruptFails(t *testing.T) {
	tempDir := t.TempDir()
	gmDir := filepath.Join(tempDir, ".glassmarble")
	if err := os.MkdirAll(gmDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gmDir, "akg.json"), []byte("{ not valid json"), 0o644); err != nil {
		t.Fatal(err)
	}

	output, err := runGmbCommand(t, "doctor", "--dir", tempDir)
	if err == nil {
		t.Fatalf("expected doctor to fail on corrupt state, got success:\n%s", output)
	}
	if !strings.Contains(output, "Parse-back:    FAILED") {
		t.Errorf("expected parse-back failure report:\n%s", output)
	}
}

func TestDiffCommandNoPendingTransactions(t *testing.T) {
	tempDir := t.TempDir()
	writeDoctorState(t, tempDir, doctorFixtureGraph())

	output, err := runGmbCommand(t, "diff", "--dir", tempDir)
	if err != nil {
		t.Fatalf("diff command failed: %v\n%s", err, output)
	}
	if !strings.Contains(output, "No pending transactions") {
		t.Errorf("expected no-pending-transactions message:\n%s", output)
	}
	if !strings.Contains(output, "abcdef123456") {
		t.Errorf("expected current commit hash in diff output:\n%s", output)
	}
}

func TestStatusCommandExtended(t *testing.T) {
	tempDir := t.TempDir()
	writeDoctorState(t, tempDir, doctorFixtureGraph())

	output, err := runGmbCommand(t, "status", "--dir", tempDir)
	if err != nil {
		t.Fatalf("status command failed: %v\n%s", err, output)
	}
	for _, want := range []string{"Schema Version: 3", "Graph Version: 7", "abcdef1234567890", "Entrypoints:   0", "Virtual Nodes:"} {
		if !strings.Contains(output, want) {
			t.Errorf("status output missing %q:\n%s", want, output)
		}
	}
}
