package cmd_test

import (
	"strings"
	"testing"
)

// inspectLazyFixture has two gm:Function nodes in different files, one
// entrypoint, and one cross-file call edge with an RDF-star line number.
const inspectLazyFixture = `@prefix gm: <http://glassmarble.org/schema#> .

<http://glassmarble.org/node/app/main.go::Main> a gm:Function ;
    gm:name "Main" ;
    gm:belongsToFile <http://glassmarble.org/file/app/main.go> ;
    gm:lineStart 10 ;
    gm:lineEnd 30 ;
    gm:isEntrypoint true ;
    .
<http://glassmarble.org/node/app/util.go::Helper> a gm:Function ;
    gm:name "Helper" ;
    gm:belongsToFile <http://glassmarble.org/file/app/util.go> ;
    gm:lineStart 5 ;
    gm:lineEnd 12 ;
    .
<http://glassmarble.org/node/app/main.go::Main> gm:calls <http://glassmarble.org/node/app/util.go::Helper> .
<< <http://glassmarble.org/node/app/main.go::Main> gm:calls <http://glassmarble.org/node/app/util.go::Helper> >> gm:lineNumber 14 .
`

// TestInspectNodeDetailLazy: node details come from the lazy streaming
// lookup, including incident edges with their line numbers
// (AUDIT Issue 4 Phase 4A-2).
func TestInspectNodeDetailLazy(t *testing.T) {
	tempDir := t.TempDir()
	writeDoctorState(t, tempDir, inspectLazyFixture)

	output, err := runGmbCommand(t, "inspect", "app/main.go::Main", "--dir", tempDir)
	if err != nil {
		t.Fatalf("inspect failed: %v\n%s", err, output)
	}
	for _, want := range []string{
		"=== Node Details: app/main.go::Main ===",
		"Name:      Main",
		"Kind:      FUNCTION",
		"File Path: app/main.go (L10 - L30)",
		"Outbound Edges (1):",
		"-> app/util.go::Helper [CALLS] (L14)",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("inspect detail output missing %q:\n%s", want, output)
		}
	}
}

// TestInspectNodeNotFoundLazy: a missing node reports the same error as the
// restored-graph path.
func TestInspectNodeNotFoundLazy(t *testing.T) {
	tempDir := t.TempDir()
	writeDoctorState(t, tempDir, inspectLazyFixture)

	_, err := runGmbCommand(t, "inspect", "ghost", "--dir", tempDir)
	if err == nil {
		t.Fatal("expected error for missing node")
	}
	if !strings.Contains(err.Error(), "node ID 'ghost' not found in AKG") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestInspectListLazy: --list streams FUNCTION/METHOD nodes only.
func TestInspectListLazy(t *testing.T) {
	tempDir := t.TempDir()
	writeDoctorState(t, tempDir, inspectLazyFixture)

	output, err := runGmbCommand(t, "inspect", "--list", "--dir", tempDir)
	if err != nil {
		t.Fatalf("inspect --list failed: %v\n%s", err, output)
	}
	for _, want := range []string{
		"=== Entry Points & Callable Symbols ===",
		"- [FUNCTION] app/main.go::Main (app/main.go:L10)",
		"- [FUNCTION] app/util.go::Helper (app/util.go:L5)",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("inspect --list output missing %q:\n%s", want, output)
		}
	}
}

// TestInspectSearchLazy: --search matches by ID or name.
func TestInspectSearchLazy(t *testing.T) {
	tempDir := t.TempDir()
	writeDoctorState(t, tempDir, inspectLazyFixture)

	// --list=false: bound flag vars persist across invocations in one test
	// process, so every mode must be stated explicitly.
	output, err := runGmbCommand(t, "inspect", "--search", "Helper", "--list=false", "--file=", "--line=0", "--dir", tempDir)
	if err != nil {
		t.Fatalf("inspect --search failed: %v\n%s", err, output)
	}
	if !strings.Contains(output, "ID: app/util.go::Helper") {
		t.Errorf("search output missing Helper node:\n%s", output)
	}
	if strings.Contains(output, "ID: app/main.go::Main") {
		t.Errorf("search 'Helper' must not match the Main node:\n%s", output)
	}
}

// TestInspectFileLineLazy: --file/--line resolves the covering node through
// the streaming scan and prints its details.
func TestInspectFileLineLazy(t *testing.T) {
	tempDir := t.TempDir()
	writeDoctorState(t, tempDir, inspectLazyFixture)

	output, err := runGmbCommand(t, "inspect", "--file", "app/main.go", "--line", "12", "--list=false", "--search=", "--dir", tempDir)
	if err != nil {
		t.Fatalf("inspect --file/--line failed: %v\n%s", err, output)
	}
	if !strings.Contains(output, "=== Node Details: app/main.go::Main ===") {
		t.Errorf("expected Main details for app/main.go:12:\n%s", output)
	}
}

// TestInspectUninitializedLazy: no state file -> the friendly empty-DB error.
func TestInspectUninitializedLazy(t *testing.T) {
	_, err := runGmbCommand(t, "inspect", "x", "--dir", t.TempDir())
	if err == nil {
		t.Fatal("expected error for missing database")
	}
	if !strings.Contains(err.Error(), "AKG database is empty") {
		t.Errorf("unexpected error: %v", err)
	}
}
