package e2e_test

// Export/import roundtrips and GraphJSON/Cypher interchange, driven through
// the real CLI on seeded graph state (no analysis needed). In-process
// runner, so no t.Parallel().

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Syamchand123/GlassMarble/tests/harness"
)

func TestExportImportRoundtrip(t *testing.T) {
	sb := harness.NewSandbox(t)

	// Seed a small verified graph directly (same shape `gmb analyze`
	// commits) so the commands have state without paying for analysis.
	sb.WriteAKGState(harness.TinyGraph())

	// --- export graphjson ------------------------------------------------------
	gmbWant(t, sb, []string{"Exported AKG snapshot", "2 nodes)"}, "export", "--output", "graph.json")

	var doc map[string]any
	if err := json.Unmarshal([]byte(sb.ReadFile("graph.json")), &doc); err != nil {
		t.Fatalf("graph.json invalid: %v", err)
	}
	nodes, _ := doc["nodes"].([]any)
	edges, _ := doc["edges"].([]any)
	if len(nodes) != 2 || len(edges) != 1 {
		t.Errorf("exported graph wrong shape: %d nodes, %d edges", len(nodes), len(edges))
	}
	if !strings.Contains(sb.ReadFile("graph.json"), "abcdef1234567890") {
		t.Errorf("exported graph missing commit hash")
	}

	// --- export neo4j cypher ------------------------------------------------------
	gmbWant(t, sb, []string{"Exported AKG snapshot"}, "export", "--format", "neo4j", "--output", "dump.cypher")
	cypher := sb.ReadFile("dump.cypher")
	for _, want := range []string{"Cypher Export", "CREATE", "main.go", "db.go"} {
		if !strings.Contains(cypher, want) {
			t.Errorf("cypher export missing %q", want)
		}
	}

	// --- import into a fresh workspace ---------------------------------------------
	dst := harness.NewSandbox(t)
	out := gmbWant(t, dst, []string{"Imported AKG snapshot", "Nodes", "Edges"}, "import", sb.Path("graph.json"))
	if !strings.Contains(out, "2") {
		t.Errorf("import confirmation should show the imported node count:\n%s", out)
	}
	// The imported graph is usable: doctor verifies it, status reports it.
	gmbWant(t, dst, []string{"DOCTOR: OK"}, "doctor")
	gmbWant(t, dst, []string{"GlassMarble AKG Status", "Nodes Count:   2"}, "status")

	// Export again from the imported state — full roundtrip.
	gmbWant(t, dst, []string{"Exported AKG snapshot"}, "export", "--output", "graph2.json")
	if strings.TrimSpace(sb.ReadFile("graph.json")) != strings.TrimSpace(dst.ReadFile("graph2.json")) {
		t.Errorf("roundtrip graph.json changed")
	}

	// --- import rejects malformed input ---------------------------------------------
	bad := harness.NewSandbox(t)
	bad.WriteFile("bad.json", `{"schema_version":3,"nodes":[],"edges":[{"source_id":"x","target_id":"y"}]}`)
	out, err := gmbErr(t, bad, "import", "bad.json")
	if err == nil {
		t.Errorf("import of a dangling-edge document should fail:\n%s", out)
	}
	if !strings.Contains(out, "rejected") && !strings.Contains(out, "dangling") && !strings.Contains(out, "invalid") {
		t.Logf("import rejection message (informational): %s", out)
	}

	// --- export requires an output flag ------------------------------------------------
	out, err = gmbErr(t, sb, "export")
	if err == nil {
		t.Errorf("export without --output should fail:\n%s", out)
	}
}
