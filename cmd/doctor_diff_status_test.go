package cmd_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Syamchand123/GlassMarble/cmd"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
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

<http://glassmarble.org/node/cmd/app/main.go::Main> gm:calls <http://glassmarble.org/node/internal/db/db.go::Connect> .
`

func writeDoctorState(t *testing.T, dir, ttl string) {
	t.Helper()
	gmDir := filepath.Join(dir, ".glassmarble")
	if err := os.MkdirAll(gmDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gmDir, "akg_state.ttl"), []byte(ttl), 0o644); err != nil {
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
	writeDoctorState(t, tempDir, doctorFixtureTTL)

	output, err := runGmbCommand(t, "doctor", "--dir", tempDir)
	if err != nil {
		t.Fatalf("doctor command failed: %v\n%s", err, output)
	}
	for _, want := range []string{"DOCTOR: OK", "Schema:        v1", "Parse-back:    ok", "Dangling:      0", "ontology conformant"} {
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
	writeDoctorState(t, tempDir, "@prefix gm: <http://glassmarble.org/schema#> .\n<http://glassmarble.org/node/x> a gm:Executable ;\n")

	output, err := runGmbCommand(t, "doctor", "--dir", tempDir)
	if err == nil {
		t.Fatalf("expected doctor to fail on truncated TTL, got success:\n%s", output)
	}
	if !strings.Contains(output, "Parse-back:    FAILED") {
		t.Errorf("expected parse-back failure report:\n%s", output)
	}
}

func TestDiffCommandWALTruncated(t *testing.T) {
	tempDir := t.TempDir()
	writeDoctorState(t, tempDir, doctorFixtureTTL)

	output, err := runGmbCommand(t, "diff", "--dir", tempDir)
	if err != nil {
		t.Fatalf("diff command failed: %v\n%s", err, output)
	}
	if !strings.Contains(output, "No pending transactions") {
		t.Errorf("expected truncated-WAL message:\n%s", output)
	}
	if !strings.Contains(output, "abcdef123456") {
		t.Errorf("expected current commit hash in diff output:\n%s", output)
	}
}

func TestStatusCommandExtended(t *testing.T) {
	tempDir := t.TempDir()
	writeDoctorState(t, tempDir, doctorFixtureTTL)

	output, err := runGmbCommand(t, "status", "--dir", tempDir)
	if err != nil {
		t.Fatalf("status command failed: %v\n%s", err, output)
	}
	for _, want := range []string{"Schema Version: 1", "Graph Version: 7", "abcdef1234567890", "Entrypoints:   0", "Virtual Nodes:"} {
		if !strings.Contains(output, want) {
			t.Errorf("status output missing %q:\n%s", want, output)
		}
	}
}
