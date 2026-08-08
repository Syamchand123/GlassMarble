package cmd_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/archmodel"
	"github.com/Syamchand123/GlassMarble/internal/developer_memory"
	"github.com/Syamchand123/GlassMarble/internal/evidence"
)

// TestMemoryStage_EndToEnd verifies the Stage 5 + Stage 6 wiring: `gmb
// analyze` must create the V2 storage layout (intelligence/, snapshots/,
// memory/) and a valid memory aggregate, and re-analyzing the same tree must
// not duplicate events (idempotency at the pipeline level).
func TestMemoryStage_EndToEnd(t *testing.T) {
	root := setupAnalyzeGitRepo(t)

	output, err := runGmbCommand(t, "analyze", "--dir", root, "--stage5")
	if err != nil {
		t.Fatalf("analyze failed: %v\n%s", err, output)
	}

	gmDir := filepath.Join(root, ".glassmarble")
	// events.jsonl is append-only and created on first ingestion, so it is
	// NOT asserted here (first run has no previous snapshot to diff).
	for _, rel := range []string{
		filepath.Join("intelligence", "latest.json"),
		filepath.Join("snapshots", "index.json"),
		filepath.Join("memory", "memory.json"),
		filepath.Join("memory", "timeline.json"),
	} {
		if _, err := os.Stat(filepath.Join(gmDir, rel)); err != nil {
			t.Errorf("expected %s to exist after analyze: %v", rel, err)
		}
	}

	// latest.json must be valid JSON with stage 5 output.
	latestData, err := os.ReadFile(filepath.Join(gmDir, "intelligence", "latest.json"))
	if err != nil {
		t.Fatalf("read latest.json: %v", err)
	}
	var latest map[string]any
	if err := json.Unmarshal(latestData, &latest); err != nil {
		t.Fatalf("latest.json is not valid JSON: %v", err)
	}

	// memory.json must be a valid DeveloperMemory aggregate.
	memData, err := os.ReadFile(filepath.Join(gmDir, "memory", "memory.json"))
	if err != nil {
		t.Fatalf("read memory.json: %v", err)
	}
	var mem map[string]any
	if err := json.Unmarshal(memData, &mem); err != nil {
		t.Fatalf("memory.json is not valid JSON: %v", err)
	}
	if mem["project_id"] == nil || mem["project_id"] == "" {
		t.Errorf("project_id not set in memory.json")
	}
	if _, ok := mem["total_events"]; !ok {
		t.Errorf("total_events missing from memory.json")
	}

	// Second analyze on the identical tree: the events WAL must not grow.
	eventsPath := filepath.Join(gmDir, "memory", "events.jsonl")
	lines1 := countFileLines(t, eventsPath)

	if _, err := runGmbCommand(t, "analyze", "--dir", root, "--stage5"); err != nil {
		t.Fatalf("second analyze failed: %v", err)
	}
	lines2 := countFileLines(t, eventsPath)
	if lines2 != lines1 {
		t.Errorf("events.jsonl grew from %d to %d lines after re-analyzing the same tree (idempotency violated)", lines1, lines2)
	}

	// The aggregate must be byte-identical after the idempotent re-run.
	memData2, err := os.ReadFile(filepath.Join(gmDir, "memory", "memory.json"))
	if err != nil {
		t.Fatalf("read memory.json after re-run: %v", err)
	}
	if string(memData) != string(memData2) {
		t.Errorf("memory.json changed after re-analyzing the same tree (rebuild not deterministic)")
	}

	// gmb memory overview must render and not error.
	memOut, err := runGmbCommand(t, "memory", "--dir", root)
	if err != nil {
		t.Fatalf("gmb memory failed: %v\n%s", err, memOut)
	}
	if !strings.Contains(memOut, "Developer memory") {
		t.Errorf("gmb memory overview did not render:\n%s", memOut)
	}

	// gmb memory --ask on empty memory must answer gracefully.
	askOut, err := runGmbCommand(t, "memory", "--dir", root, "--ask", "what do we know about services?")
	if err != nil {
		t.Fatalf("gmb memory --ask failed: %v\n%s", err, askOut)
	}
	if !strings.Contains(askOut, "holds nothing") {
		t.Errorf("gmb memory --ask on empty memory should say it holds nothing:\n%s", askOut)
	}

	// gmb memory --json must emit a valid document.
	jsonOut, err := runGmbCommand(t, "memory", "--dir", root, "--json")
	if err != nil {
		t.Fatalf("gmb memory --json failed: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(jsonOut), &doc); err != nil {
		t.Fatalf("gmb memory --json output is not valid JSON: %v\n%s", err, jsonOut)
	}
}

// TestMemoryStage_CLIWithSeededMemory drives the `gmb memory` CLI against a
// real on-disk memory seeded through the developer_memory builder (the same
// store the pipeline writes to).
func TestMemoryStage_CLIWithSeededMemory(t *testing.T) {
	root := setupAnalyzeGitRepo(t)
	if _, err := runGmbCommand(t, "init", "--dir", root); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	ts := time.Date(2025, 6, 1, 10, 0, 0, 0, time.UTC)
	ev := archmodel.ArchEvent{
		ID:         "evt_seed1",
		Kind:       archmodel.EventServiceAdded,
		CommitHash: "aaaa0000",
		Timestamp:  ts,
		Title:      "PaymentService added",
		Components: []string{"PaymentService"},
		Intent:     "added because the legacy gateway was slow",
		IntentSrc:  evidence.SourceGit,
		Evidence: evidence.NewBundle(evidence.EvidenceItem{
			Source: evidence.SourceGit, Reference: "aaaa0000", Confidence: 0.9, Timestamp: ts,
		}),
	}
	store := developer_memory.NewStoreForRepo(root)
	builder := developer_memory.NewMemoryBuilderWithOptions(store, developer_memory.WithProjectID("proj_test"))
	if _, err := builder.ProcessEvents([]archmodel.ArchEvent{ev}); err != nil {
		t.Fatalf("seed memory: %v", err)
	}

	// Overview lists the component.
	out, err := runGmbCommand(t, "memory", "--dir", root)
	if err != nil {
		t.Fatalf("gmb memory failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "PaymentService") || !strings.Contains(out, "current") {
		t.Errorf("overview did not list the component:\n%s", out)
	}

	// Ask returns the component, the fact claim AND the explicit reason.
	ask, err := runGmbCommand(t, "memory", "--dir", root, "--ask", "why was PaymentService added?")
	if err != nil {
		t.Fatalf("gmb memory --ask failed: %v\n%s", err, ask)
	}
	if !strings.Contains(ask, "PaymentService") {
		t.Errorf("ask did not surface the component:\n%s", ask)
	}
	if !strings.Contains(ask, "was_added") {
		t.Errorf("ask did not surface the fact claim:\n%s", ask)
	}
	if !strings.Contains(ask, "EXPLICIT_REASON") || !strings.Contains(ask, "legacy gateway was slow") {
		t.Errorf("ask did not surface the explicit reason claim:\n%s", ask)
	}

	// Component view shows the history plus the timeline row.
	comp, err := runGmbCommand(t, "memory", "--dir", root, "--component", "payment")
	if err != nil {
		t.Fatalf("gmb memory --component failed: %v\n%s", err, comp)
	}
	if !strings.Contains(comp, "Component PaymentService") || !strings.Contains(comp, "SERVICE_ADDED") {
		t.Errorf("component view incomplete:\n%s", comp)
	}

	// Unknown component is a graceful miss, not an error.
	miss, err := runGmbCommand(t, "memory", "--dir", root, "--component", "nope")
	if err != nil {
		t.Fatalf("gmb memory --component miss failed: %v", err)
	}
	if !strings.Contains(miss, "No component matching") {
		t.Errorf("component miss should be graceful:\n%s", miss)
	}

	// JSON mode emits the seeded event.
	j, err := runGmbCommand(t, "memory", "--dir", root, "--ask", "payment", "--json")
	if err != nil {
		t.Fatalf("gmb memory --json failed: %v", err)
	}
	var doc struct {
		Events []struct {
			ID string `json:"id"`
		} `json:"events"`
	}
	if err := json.Unmarshal([]byte(j), &doc); err != nil {
		t.Fatalf("ask --json output is not valid JSON: %v\n%s", err, j)
	}
	if len(doc.Events) != 1 || doc.Events[0].ID != "evt_seed1" {
		t.Errorf("ask --json events = %+v, want [evt_seed1]", doc.Events)
	}
}

// TestMemoryStage_InitCreatesStageDirs verifies `gmb init` provisions the V2
// stage directories.
func TestMemoryStage_InitCreatesStageDirs(t *testing.T) {
	root := t.TempDir()
	if _, err := runGmbCommand(t, "init", "--dir", root); err != nil {
		t.Fatalf("init failed: %v", err)
	}
	gmDir := filepath.Join(root, ".glassmarble")
	for _, sub := range []string{"intelligence", "snapshots", "memory"} {
		st, err := os.Stat(filepath.Join(gmDir, sub))
		if err != nil || !st.IsDir() {
			t.Errorf("expected %s/ to exist after init: %v", sub, err)
		}
	}
}

func countFileLines(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0
		}
		t.Fatalf("read %s: %v", path, err)
	}
	n := 0
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) != "" {
			n++
		}
	}
	return n
}
