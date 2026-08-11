// Package qa_test holds quality-assurance suites: golden renderer checks,
// JSON schema conformance, contract spot checks and exit-code pinning.
package qa_test

// JSON schema conformance: every wire/disk JSON document the product emits
// is pinned against the REAL decoder types from internal/*. Hand-written
// sample documents (shaped after the struct json tags) must unmarshal
// cleanly and every gate field must survive the round trip. Documented
// as the "contract of record" for stage persistence formats:
//
//	akg.json            -> akg.GraphJSON            (schema v3)
//	memory.json         -> developer_memory.DeveloperMemory
//	intelligence/...    -> arch_intelligence.Stage5Result
//	snapshots/index.json -> []arch_timeline.SnapshotIndexEntry
//	snapshots/<id>.json -> archmodel.ArchSnapshot
//
// Any field rename/removal in the structs or a change in the persisted
// shape breaks these tests — the schema-drift gate for akg/diamond
// compatibility.

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/arch_intelligence"
	"github.com/Syamchand123/GlassMarble/internal/arch_timeline"
	"github.com/Syamchand123/GlassMarble/internal/archmodel"
	"github.com/Syamchand123/GlassMarble/internal/developer_memory"
)

const akgSample = `{
	"schema_version": 3,
	"commit_hash": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	"version": 9,
	"entrypoints": ["cmd/app/main.go::Main"],
	"folder_zones": {"cmd": "api", "internal": "core"},
	"nodes": [
		{"id": "cmd/app/main.go::Main", "kind": "EXECUTABLE", "name": "Main",
		 "file_spec": {"path": "cmd/app/main.go", "line_start": 12}},
		{"id": "internal/service.go::New", "kind": "FUNCTION", "name": "New",
		 "primitive": "DISK_IO",
		 "primitive_scores": {"DISK_IO": 0.8},
		 "file_spec": {"path": "internal/service.go", "line_start": 5}}
	],
	"edges": [
		{"source_id": "cmd/app/main.go::Main", "target_id": "internal/service.go::New",
		 "type": "CALLS", "line_number": 15, "confidence": 1.0}
	],
	"verified": true
}`

func TestAkgJSONSchemaV3(t *testing.T) {
	var g akg.GraphJSON
	if err := json.Unmarshal([]byte(akgSample), &g); err != nil {
		t.Fatalf("akg.json sample must unmarshal: %v", err)
	}
	if g.SchemaVersion != 3 {
		t.Errorf("schema_version = %d, want 3", g.SchemaVersion)
	}
	if g.CommitHash != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" || g.Version != 9 {
		t.Errorf("commit_hash/version mismatch: %q %d", g.CommitHash, g.Version)
	}
	if len(g.Nodes) != 2 || len(g.Edges) != 1 {
		t.Fatalf("nodes/edges = %d/%d, want 2/1", len(g.Nodes), len(g.Edges))
	}
	n0 := g.Nodes[0]
	if n0.ID != "cmd/app/main.go::Main" || n0.Kind != "EXECUTABLE" || n0.Name != "Main" {
		t.Errorf("node0 fields wrong: %+v", n0)
	}
	if n0.FileSpec.Path != "cmd/app/main.go" || n0.FileSpec.LineStart != 12 {
		t.Errorf("node0 file_spec wrong: %+v", n0.FileSpec)
	}
	n1 := g.Nodes[1]
	if n1.Primitive != "DISK_IO" || n1.PrimitiveScores["DISK_IO"] != 0.8 {
		t.Errorf("node1 primitive metadata wrong: %+v", n1)
	}
	e0 := g.Edges[0]
	if e0.SourceID != "cmd/app/main.go::Main" || e0.TargetID != "internal/service.go::New" || e0.Type != "CALLS" || e0.LineNumber != 15 {
		t.Errorf("edge0 fields wrong: %+v", e0)
	}
	if !g.Verified {
		t.Errorf("verified = false, want true")
	}
}

func TestAkgJSONEmptyArraysRegress(t *testing.T) {
	// Regression guard for the fixed graph_json bug: an empty graph must
	// serialize as "nodes":[] — never "nodes":null — or `gmb status` breaks
	// right after `gmb init`.
	var g akg.GraphJSON
	if err := json.Unmarshal([]byte(`{"schema_version":3,"nodes":[],"edges":[]}`), &g); err != nil {
		t.Fatalf("unmarshal empty graph: %v", err)
	}
	if g.Nodes == nil || g.Edges == nil {
		t.Fatalf("empty arrays decoded to nil (schema drift)")
	}
	out, err := json.Marshal(g)
	if err != nil {
		t.Fatalf("marshal empty graph: %v", err)
	}
	s := string(out)
	if strings.Contains(s, `"nodes":null`) || strings.Contains(s, `"edges":null`) {
		t.Fatalf("empty graph serialized as null: %s", s)
	}
	if !strings.Contains(s, `"nodes":[]`) || !strings.Contains(s, `"edges":[]`) {
		t.Fatalf("empty graph missing [] arrays: %s", s)
	}
}

func TestAkgJSONUnknownFieldRejected(t *testing.T) {
	dec := json.NewDecoder(strings.NewReader(strings.Replace(akgSample,
		`"verified": true`, `"verified": true, "bogus_field": 1`, 1)))
	dec.DisallowUnknownFields()
	var g akg.GraphJSON
	if err := dec.Decode(&g); err == nil {
		t.Fatalf("unknown field must be rejected by DisallowUnknownFields")
	}
}

const memorySample = `{
	"project_id": "proj_ab12cd34ef56",
	"last_updated": "2026-06-15T10:30:00Z",
	"total_events": 2,
	"timeline": [
		{"timestamp": "2026-06-15T10:30:00Z", "commit_hash": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		 "version": "v0.1.1", "title": "Add service", "description": "Introduce internal/service.go",
		 "event_kind": "SYS_ADD", "components": ["internal/service.go"], "intent": "REFACTOR"}
	],
	"component_memory": {
		"internal/service.go": {
			"name": "internal/service.go",
			"first_seen": "2026-06-15T10:30:00Z",
			"last_seen": "2026-06-15T10:30:00Z",
			"state": "CURRENT",
			"event_ids": ["evt_0001"],
			"claims": [
				{"id": "claim_1", "subject": "internal/service.go", "subject_id": "internal/service.go::New",
				 "predicate": "implements", "object": "RemoteService", "claim_kind": "STRUCTURAL",
				 "evidence": {"type": "STRUCTURAL_DEP", "source_id": "cmd/app/main.go::Main", "target_id": "internal/service.go::New", "confidence": 0.9},
				 "state": "CURRENT", "valid_from": "2026-06-15T10:30:00Z", "freshness_score": 0.95}
			]
		}
	},
	"global_memory": [
		{"id": "gclaim_1", "subject": "cmd/app/main.go", "subject_id": "cmd/app/main.go::Main",
		 "predicate": "calls", "object": "internal/service.go", "object_id": "internal/service.go::New",
		 "claim_kind": "DEPENDENCY", "evidence": {"type": "STRUCTURAL_DEP", "source_id": "cmd/app/main.go::Main", "target_id": "internal/service.go::New", "confidence": 0.9},
		 "state": "CURRENT", "valid_from": "2026-06-15T10:30:00Z", "freshness_score": 0.95}
	],
	"events": [
		{"id": "evt_0001", "kind": "comp_added", "commit_hash": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		 "timestamp": "2026-06-15T10:30:00Z", "title": "Add service", "description": "Introduce internal/service.go",
		 "affected_ids": ["internal/service.go::New"], "components": ["internal/service.go"],
		 "intent": "REFACTOR", "intent_src": "heuristic", "tags": ["add"], "valid_from": "2026-06-15T10:30:00Z"}
	]
}`

func TestMemoryJSONSchema(t *testing.T) {
	var m developer_memory.DeveloperMemory
	if err := json.Unmarshal([]byte(memorySample), &m); err != nil {
		t.Fatalf("memory.json sample must unmarshal: %v", err)
	}
	if !strings.HasPrefix(m.ProjectID, "proj_") {
		t.Errorf("project_id = %q, want proj_ prefix", m.ProjectID)
	}
	if m.TotalEvents != 2 || len(m.Events) != 1 {
		t.Errorf("total_events/events = %d/%d", m.TotalEvents, len(m.Events))
	}
	if len(m.Timeline) != 1 || m.Timeline[0].Title != "Add service" {
		t.Errorf("timeline wrong: %+v", m.Timeline)
	}
	if len(m.GlobalMemory) != 1 {
		t.Fatalf("global_memory = %d, want 1", len(m.GlobalMemory))
	}
	cl := m.GlobalMemory[0]
	if cl.ID != "gclaim_1" || cl.Predicate != "calls" || cl.ObjectID != "internal/service.go::New" {
		t.Errorf("global claim fields wrong: %+v", cl)
	}
	if string(cl.State) != "CURRENT" {
		t.Errorf("claim state = %q, want CURRENT", cl.State)
	}
	if cl.ValidFrom.IsZero() || cl.FreshnessScore == 0 {
		t.Errorf("claim validity metadata missing: %+v", cl)
	}
	ch := m.ComponentMemory["internal/service.go"]
	if ch.State != "CURRENT" || len(ch.Claims) != 1 || len(ch.Events) != 1 {
		t.Errorf("component history wrong: %+v", ch)
	}
}

func TestMemoryJSONRoundTrip(t *testing.T) {
	var m developer_memory.DeveloperMemory
	if err := json.Unmarshal([]byte(memorySample), &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	out, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back developer_memory.DeveloperMemory
	if err := json.Unmarshal(out, &back); err != nil {
		t.Fatalf("re-unmarshal: %v", err)
	}
	if back.ProjectID != m.ProjectID || len(back.GlobalMemory) != len(m.GlobalMemory) {
		t.Errorf("round trip mismatch: %+v vs %+v", back.ProjectID, m.ProjectID)
	}
}

const intelligenceSample = `{
	"graph_hash": "abc123def456",
	"metrics": {"total_nodes": 2, "total_edges": 1, "graph_density": 0.5,
	            "max_fan_in": 1, "max_fan_out": 0, "instability": 0.0,
	            "cyclomatic_max": 4},
	"components": [],
	"component_coupling": [],
	"patterns": [
		{"kind": "CPU_INTENSIVE", "name": "crawl hot path", "components": ["main.go"],
		 "confidence": 0.87, "description": "single point of CPU work"}
	],
	"smells": [
		{"kind": "GOD_OBJECT", "title": "N/A is 25% of all calls",
		 "severity": "MAJOR", "affected_ids": ["main.go::Main"], "suggestion": "split N/A"}
	]
}`

func TestIntelligenceJSONSchema(t *testing.T) {
	var r arch_intelligence.Stage5Result
	if err := json.Unmarshal([]byte(intelligenceSample), &r); err != nil {
		t.Fatalf("intelligence sample must unmarshal: %v", err)
	}
	if r.GraphHash != "abc123def456" {
		t.Errorf("graph_hash = %q", r.GraphHash)
	}
	if r.Metrics.TotalNodes != 2 || r.Metrics.TotalEdges != 1 {
		t.Errorf("metrics wrong: %+v", r.Metrics)
	}
	if len(r.Patterns) != 1 || r.Patterns[0].Confidence != 0.87 {
		t.Errorf("patterns wrong: %+v", r.Patterns)
	}
	if len(r.Smells) != 1 || string(r.Smells[0].Severity) != "MAJOR" {
		t.Errorf("smells wrong: %+v", r.Smells)
	}
}

const snapshotIndexSample = `[
	{"commit_hash": "cccccccccccccccccccccccccccccccccccccccc",
	 "snapshot_id": "snap_2026-06-15T10-31-00Z",
	 "timestamp": "2026-06-15T10:31:00Z",
	 "order": 4, "topology_hash": "abc", "pattern_count": 1, "smell_count": 0,
	 "snapshot_file": "snap_2026-06-15T10-31-00Z.json"}
]`

func TestSnapshotIndexSchema(t *testing.T) {
	var entries []arch_timeline.SnapshotIndexEntry
	if err := json.Unmarshal([]byte(snapshotIndexSample), &entries); err != nil {
		t.Fatalf("index.json sample must unmarshal: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %d", len(entries))
	}
	e := entries[0]
	if e.CommitHash != "cccccccccccccccccccccccccccccccccccccccc" || e.SnapshotID != "snap_2026-06-15T10-31-00Z" || e.Order != 4 {
		t.Errorf("entry fields wrong: %+v", e)
	}
	if e.Timestamp.IsZero() || e.SnapshotFile == "" {
		t.Errorf("entry metadata missing: %+v", e)
	}
}

func TestArchSnapshotSchema(t *testing.T) {
	sample := `{
		"id": "snap_2026-06-15T10-31-00Z",
		"commit_hash": "cccccccccccccccccccccccccccccccccccccccc",
		"version": "v0.1.1",
		"timestamp": "2026-06-15T10:31:00Z",
		"node_count": 2, "edge_count": 1,
		"components": [], "patterns": [], "smells": []
	}`
	var s archmodel.ArchSnapshot
	if err := json.Unmarshal([]byte(sample), &s); err != nil {
		t.Fatalf("snapshot sample must unmarshal: %v", err)
	}
	if s.ID != "snap_2026-06-15T10-31-00Z" || s.NodeCount != 2 || s.EdgeCount != 1 {
		t.Errorf("snapshot fields wrong: %+v", s)
	}
	if s.Timestamp.Sub(time.Now()) > 0 {
		t.Errorf("snapshot timestamp parsed wrong: %v", s.Timestamp)
	}
}