package e2e_test

// TestAnalyzeStoresWholePipelineAndSecondCommitDrivesMemory is the master-plan
// end-to-end journey for Phases 2-7 persistence:
//
//	init → sample project committed → analyze (full pipeline) → second commit
//	that ADDS a component → analyze again → assert every persistence artifact
//	exists on disk and that the second analysis wrote real architectural
//	events into developer memory (the intelligence-memory wiring).
//
// The second commit deterministically produces events because the new
// internal/audit package appears as a brand-new component in the diff between
// the two snapshots (EventServiceAdded), which the memory builder appends to
// its append-only WAL. All commands run IN PROCESS via the harness, so this
// test must not call t.Parallel().

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Syamchand123/GlassMarble/tests/harness"
)

func TestAnalyzeStoresWholePipelineAndSecondCommitDrivesMemory(t *testing.T) {
	sb := harness.NewSandbox(t)
	sb.RequireGit()

	// --- 1. init creates the V2 workspace skeleton ------------------------
	gmb(t, sb, "init")
	for _, rel := range []string{
		".glassmarble/akg.json",
		".glassmarble/config.yaml",
		".glassmarble/marbles",
		".glassmarble/snapshots",
		".glassmarble/memory",
	} {
		if !sb.Exists(rel) {
			t.Errorf("init did not create %s", rel)
		}
	}

	// --- 2. seed the project and commit it ----------------------------------
	sb.SampleProject()
	sb.GitInit()
	if status := sb.GitStatusPorcelain(); status != "" {
		t.Fatalf("expected clean working tree after seed, got:\n%s", status)
	}

	// --- 3. first analysis: full pipeline -----------------------------------
	commit1 := sb.GitHead()
	gmbWant(t, sb, []string{"Analyzed", "Intelligence:"}, "analyze")
	if !sb.Exists(".glassmarble/telemetry.json") {
		t.Errorf("analyze did not write telemetry.json")
	}
	if !sb.Exists(".glassmarble/intelligence/latest.json") {
		t.Errorf("analyze did not persist the intelligence result")
	}
	if want, got := akgCommitHash(t, sb), commit1; got != want {
		t.Errorf("akg.json commit_hash = %s, want commit1 %s", got, want)
	}

	// --- 4. second commit that adds a brand-new component --------------------
	auditFile := "internal/audit/audit.go"
	commit2 := sb.GitCommitFiles("add audit component", map[string]string{
		auditFile: `package audit

import "example.com/shop/internal/cache"

type Auditor struct {
	cache *cache.Cache
}

func New() *Auditor {
	return &Auditor{cache: cache.New()}
}

func (a *Auditor) State() string {
	return a.cache.Get("state")
}
`,
	})
	if commit2 == commit1 {
		t.Fatalf("second commit did not advance HEAD")
	}

	// --- 5. second analysis: publishes events into developer memory ----------
	out := gmbWant(t, sb, []string{"Analyzed", "Intelligence:"}, "analyze")
	if !strings.Contains(out, "audit") {
		t.Logf("second analyze output (informational):\n%s", out)
	}
	if want, got := akgCommitHash(t, sb), commit2; got != want {
		t.Errorf("akg.json commit_hash = %s, want commit2 %s", got, want)
	}

	// Snapshot per analysis: two content-addressed snap_*.json files + index.
	matches, err := filepath.Glob(sb.Path(".glassmarble", "snapshots", "snap_*.json"))
	if err != nil {
		t.Fatalf("glob snapshots: %v", err)
	}
	if len(matches) != 2 {
		t.Errorf("snapshots/snap_*.json count = %d, want 2 (one per analysis)", len(matches))
	}
	if !sb.Exists(".glassmarble/snapshots/index.json") {
		t.Errorf("snapshots/index.json missing after two analyses")
	}

	// --- 6. the developer memory is populated from the WAL -----------
	assertMemoryWAL(t, sb)
	assertTimeline(t, sb)
}

// assertMemoryWAL checks the append-only event WAL and the derived
// memory.json aggregate produced by the second analysis. (Knowledge claims
// are derived inside MemoryStore.Rebuild and live in the memory.json
// aggregate — the pipeline does not maintain a separate claims WAL.)
func assertMemoryWAL(t *testing.T, sb *harness.Sandbox) {
	t.Helper()
	eventsPath := sb.Path(".glassmarble", "memory", "events.jsonl")
	if !sb.Exists(".glassmarble/memory/events.jsonl") {
		t.Fatalf("events.jsonl not written by analyze")
	}

	events, err := os.ReadFile(eventsPath)
	if err != nil {
		t.Fatalf("read events.jsonl: %v", err)
	}
	if n := strings.Count(string(events), "\n"); n < 1 {
		t.Errorf("events.jsonl has %d event line(s), want >= 1 (the audit component addition)", n)
	}
	if !strings.Contains(string(events), "internal/audit") {
		t.Errorf("events.jsonl contains no references to the new audit component:\n%s", events)
	}

	mem := sb.ReadFile(".glassmarble/memory/memory.json")
	var agg struct {
		TotalEvents int    `json:"total_events"`
		ProjectID   string `json:"project_id"`
	}
	if err := json.Unmarshal([]byte(mem), &agg); err != nil {
		t.Fatalf("memory.json is not valid JSON: %v", err)
	}
	if agg.TotalEvents < 1 {
		t.Errorf("memory.json total_events = %d, want >= 1", agg.TotalEvents)
	}
	if agg.ProjectID == "" {
		t.Errorf("memory.json project_id is empty")
	}
	if !strings.Contains(mem, "audit") {
		t.Errorf("memory.json aggregate contains no trace of the audit component:\n%s", mem)
	}
}

// assertTimeline checks the derived timeline artifact and the `gmb timeline`
// command both expose the deployed architecture change.
func assertTimeline(t *testing.T, sb *harness.Sandbox) {
	t.Helper()
	if !sb.Exists(".glassmarble/memory/timeline.json") {
		t.Fatalf("timeline.json not written by analyze")
	}
	tl := sb.ReadFile(".glassmarble/memory/timeline.json")
	var entries []json.RawMessage
	if err := json.Unmarshal([]byte(tl), &entries); err != nil {
		t.Fatalf("timeline.json is not valid JSON: %v", err)
	}
	if len(entries) < 1 {
		t.Fatalf("timeline.json has %d entries, want >= 1", len(entries))
	}

	gmbWant(t, sb, []string{"audit"}, "timeline")
}

// akgCommitHash reads commit_hash from .glassmarble/akg.json.
func akgCommitHash(t *testing.T, sb *harness.Sandbox) string {
	t.Helper()
	data := sb.ReadFile(".glassmarble/akg.json")
	var state struct {
		CommitHash string `json:"commit_hash"`
	}
	if err := json.Unmarshal([]byte(data), &state); err != nil {
		t.Fatalf("akg.json is not valid JSON: %v", err)
	}
	return state.CommitHash
}
