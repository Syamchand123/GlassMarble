package edgecases_test

// Edge cases around configuration, the akg.json database itself, and the
// GraphJSON import gateway. Deviations from the documented expectations,
// verified against cmd/init.go, cmd/status.go, cmd/snapshot.go, cmd/import.go,
// internal/akg/graph_json.go:
//
//  1. Missing config.yaml loads defaults silently — `gmb status` uses v3.0.0
//     (the current default schema) instead of erroring.
//  2. `gmb init` writes an empty-but-valid v3 state: nodes/edges are empty
//     ARRAYS (never JSON null), so `gmb status` streams it cleanly. This
//     regression (serialized null arrays previously broke the streaming
//     stats reader with "expected a JSON array") is pinned here.
//  3. `gmb snapshot --list` on a fresh state prints the empty hint and
//     succeeds — no node-types-file requirement anywhere in the path.
//  4. `gmb snapshot --create` on an empty graph fails with the empty-database
//     hint instead of snapshotting a vacuum.
//  5. `gmb import` is the only schema-version gateway: schema v1 documents
//     and documents above the current schema version are rejected with the
//     unsupported-schema errors; dangling references are rejected on replace.

import (
	"strings"
	"testing"

	"github.com/Syamchand123/GlassMarble/tests/harness"
)

// TestMissingConfigTolerated: no config.yaml — status renders with the
// default schema version rather than complaining.
func TestMissingConfigTolerated(t *testing.T) {
	sb := singleGoRepo(t)
	mustRunContains(t, sb, []string{"GlassMarble Workspace Initialized"}, "init")
	out := mustRunContains(t, sb, []string{"GlassMarble AKG Status"}, "status")
	if strings.Contains(out, "unsupported database version") {
		t.Errorf("default schema was rejected:\n%s", out)
	}
}

// TestInitWritesEmptyArraysNotNull: the state written by init must stream
// cleanly — nodes/edges must never serialize as JSON null (regression guard
// for the "expected a JSON array" failure of `gmb status` after `gmb init`).
func TestInitWritesEmptyArraysNotNull(t *testing.T) {
	sb := singleGoRepo(t)
	mustRun(t, sb, "init")
	raw := sb.ReadFile(".glassmarble/akg.json")
	if strings.Contains(raw, `"nodes": null`) || strings.Contains(raw, `"edges": null`) {
		t.Errorf("empty state serialized null arrays:\n%s", raw)
	}
	mustRunContains(t, sb, []string{"GlassMarble AKG Status", "Nodes Count:   0"}, "status")
}

// TestSnapshotListEmpty: a fresh state has no snapshots — list prints the
// empty hint and succeeds.
func TestSnapshotListEmpty(t *testing.T) {
	sb := singleGoRepo(t)
	mustRun(t, sb, "init")
	mustRunContains(t, sb, []string{"No snapshots yet.", "gmb snapshot --create"}, "snapshot", "--list")
}

// TestUnsupportedLegacyVersion: a schema v1 document is rejected by import
// with the pre-overhaul error.
func TestUnsupportedLegacyVersion(t *testing.T) {
	sb := harness.NewSandbox(t)
	sb.WriteFile("v1.json", `{"schema_version":1,"nodes":[],"edges":[]}`)
	mustFailContains(t, sb, []string{"cannot import schema v1 graph document"}, "import", "v1.json")
}

// TestUnsupportedFutureVersion: a schema above the current version is
// rejected by import with the exceeds-maximum error.
func TestUnsupportedFutureVersion(t *testing.T) {
	sb := harness.NewSandbox(t)
	sb.WriteFile("v99.json", `{"schema_version":99,"nodes":[],"edges":[]}`)
	mustFailContains(t, sb, []string{"exceeds maximum supported schema version"}, "import", "v99.json")
}

// TestImportRejectsDangling: a document whose edge references a node that is
// not part of the document fails the dangling-reference gate on replace.
func TestImportRejectsDangling(t *testing.T) {
	sb := harness.NewSandbox(t)
	sb.WriteFile("bad.json", `{"schema_version":3,"nodes":[{"id":"a","kind":"FUNCTION","name":"a","file_spec":{"path":"a.go"}}],"edges":[{"source_id":"a","target_id":"missing"}]}`)
	out, err := harness.RunGmb(t, sb, "import", "bad.json")
	if err == nil {
		t.Fatalf("import of a dangling document unexpectedly succeeded:\n%s", out)
	}
	combined := out + "\n" + err.Error()
	for _, w := range []string{"import rejected", "dangling"} {
		if !strings.Contains(combined, w) {
			t.Errorf("import error missing %q (err=%v)\n--- output ---\n%s", w, err, out)
		}
	}
}

// TestImportValidDoc: a schema v2 document with a node imports cleanly and
// status reflects the imported node.
func TestImportValidDoc(t *testing.T) {
	sb := harness.NewSandbox(t)
	sb.WriteFile("ok.json", `{"schema_version":2,"commit_hash":"imported","version":1,"nodes":[{"id":"main","kind":"FUNCTION","name":"main","file_spec":{"path":"main.go"}}],"edges":[]}`)
	mustRunContains(t, sb, []string{"Imported AKG snapshot", "Nodes"}, "import", "ok.json")
	mustRunContains(t, sb, []string{"GlassMarble AKG Status", "Nodes Count:   1"}, "status")
}