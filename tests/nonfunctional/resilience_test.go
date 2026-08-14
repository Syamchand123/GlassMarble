package nonfunctional_test

import (
	"bytes"
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/developer_memory"
	"github.com/Syamchand123/GlassMarble/internal/learning"
	"github.com/Syamchand123/GlassMarble/tests/harness"
)

// TestCorruptStateRefusedOnLoad verifies that a corrupt or truncated
// akg.json surfaces a loud load error instead of a silent empty graph, and
// that `doctor` flags the same database as broken.
func TestCorruptStateRefusedOnLoad(t *testing.T) {
	tiny, err := json.Marshal(harness.TinyGraph())
	if err != nil {
		t.Fatalf("marshal TinyGraph: %v", err)
	}
	contents := map[string]string{
		"garbage":   "{ not valid json",
		"truncated": string(tiny[:len(tiny)/2]),
	}
	for name, content := range contents {
		t.Run(name, func(t *testing.T) {
			sb := harness.NewSandbox(t)
			sb.WriteFile(".glassmarble/akg.json", content)

			if _, err := akg.NewAKGTransactionManager(sb.GmDir); err == nil {
				t.Fatal("expected load error for corrupt state")
			} else if !strings.Contains(err.Error(), "failed to restore AKG state from disk") {
				t.Errorf("load error = %v, want restore-failure message", err)
			}

			out, err := harness.RunGmb(t, sb, "doctor")
			if err == nil {
				t.Fatalf("doctor must fail on corrupt state:\n%s", out)
			}
			if !strings.Contains(err.Error(), "integrity check failed") {
				t.Errorf("doctor error = %v, want integrity check failure", err)
			}
		})
	}
}

// TestEmptyStateDocumentLoads verifies the empty-document contract: `{}` is
// a valid v0 state producing an empty graph, and doctor/status stay healthy.
func TestEmptyStateDocumentLoads(t *testing.T) {
	sb := harness.NewSandbox(t)
	sb.WriteFile(".glassmarble/akg.json", "{}")

	tm, err := akg.NewAKGTransactionManager(sb.GmDir)
	if err != nil {
		t.Fatalf("New with {} state: %v", err)
	}
	defer tm.Close()
	if n := tm.GetActiveGraph().Nodes.Len(); n != 0 {
		t.Errorf("empty state loaded %d nodes, want 0", n)
	}

	out, err := harness.RunGmb(t, sb, "doctor")
	if err != nil {
		t.Fatalf("doctor on empty state: %v\n%s", err, out)
	}
	out, err = harness.RunGmb(t, sb, "status")
	if err != nil {
		t.Fatalf("status on empty state: %v\n%s", err, out)
	}
}

// TestNoBakRecoveryForCanonicalStore documents that akg.json.bak is a
// schema-migration backup only: the canonical store does NOT self-heal from
// it, so deleting akg.json yields an empty graph while the backup is left
// untouched.
func TestNoBakRecoveryForCanonicalStore(t *testing.T) {
	sb := harness.NewSandbox(t)
	sb.WriteAKGState(harness.TinyGraph())

	if err := os.Rename(sb.Path(".glassmarble/akg.json"), sb.Path(".glassmarble/akg.json.bak")); err != nil {
		t.Fatalf("rename akg.json: %v", err)
	}

	tm, err := akg.NewAKGTransactionManager(sb.GmDir)
	if err != nil {
		t.Fatalf("New without state: %v", err)
	}
	defer tm.Close()
	if n := tm.GetActiveGraph().Nodes.Len(); n != 0 {
		t.Errorf("expected empty graph after state deletion, got %d nodes", n)
	}
	if !sb.Exists(".glassmarble/akg.json.bak") {
		t.Error("bak file must be left untouched")
	}
}

// TestStaleLockReclaimed verifies crash-recovery: a db.lock older than the
// stale threshold is stolen and rewritten with the current pid.
func TestStaleLockReclaimed(t *testing.T) {
	sb := harness.NewSandbox(t)
	lock := sb.Path(".glassmarble/db.lock")
	old := time.Now().Add(-10 * time.Minute)

	writeStaleLock := func() {
		sb.WriteFile(".glassmarble/db.lock", "99999\n12345\n")
		if err := os.Chtimes(lock, old, old); err != nil {
			t.Fatalf("Chtimes: %v", err)
		}
	}

	writeStaleLock()
	tm, err := akg.NewAKGTransactionManager(sb.GmDir)
	if err != nil {
		t.Fatalf("New with stale lock present: %v", err)
	}
	defer tm.Close()

	tm.ReleaseLock()
	writeStaleLock()
	if err := tm.AcquireLock(); err != nil {
		t.Fatalf("stale lock was not reclaimed: %v", err)
	}
	defer tm.ReleaseLock()

	if data := sb.ReadFile(".glassmarble/db.lock"); !strings.Contains(data, strconv.Itoa(os.Getpid())) {
		t.Errorf("lock content %q does not carry the current pid", data)
	}
}

// TestCorruptEventWALSelfHeals verifies the events WAL survives a garbage
// line: scans skip corrupt records and rebuilds from the valid remainder.
func TestCorruptEventWALSelfHeals(t *testing.T) {
	sb := harness.NewSandbox(t)
	store := developer_memory.NewStoreForRepo(sb.Root)
	if err := store.AppendEvent(memEvent("evt_a", time.Now())); err != nil {
		t.Fatalf("AppendEvent a: %v", err)
	}
	if err := store.AppendEvent(memEvent("evt_b", time.Now())); err != nil {
		t.Fatalf("AppendEvent b: %v", err)
	}
	f, err := os.OpenFile(sb.Path(".glassmarble/memory/events.jsonl"), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open events.jsonl: %v", err)
	}
	if _, err := f.WriteString("this is not json\n"); err != nil {
		t.Fatalf("append garbage: %v", err)
	}
	f.Close()

	events, err := store.LoadEvents()
	if err != nil {
		t.Fatalf("LoadEvents with corrupt line: %v", err)
	}
	if len(events) != 2 {
		t.Errorf("LoadEvents returned %d events, want 2 (corrupt line skipped)", len(events))
	}
	mem, err := store.Rebuild()
	if err != nil {
		t.Fatalf("Rebuild with corrupt line: %v", err)
	}
	if mem.TotalEvents != 2 {
		t.Errorf("Rebuild total_events = %d, want 2", mem.TotalEvents)
	}
}

// TestCorruptCorrectionsWALLenient verifies the corrections WAL is scanned
// leniently: a garbage line does not break listing valid corrections.
func TestCorruptCorrectionsWALLenient(t *testing.T) {
	sb := harness.NewSandbox(t)
	sb.SeedCorrections()
	f, err := os.OpenFile(sb.Path(".glassmarble/memory/corrections.jsonl"), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open corrections.jsonl: %v", err)
	}
	if _, err := f.WriteString("{ corrupt\n"); err != nil {
		t.Fatalf("append garbage: %v", err)
	}
	f.Close()

	learner := learning.NewLearnerForRepo(sb.Root)
	corrs, err := learner.List()
	if err != nil {
		t.Fatalf("List with corrupt line: %v", err)
	}
	if len(corrs) != 1 {
		t.Errorf("List returned %d corrections, want 1 (corrupt line skipped)", len(corrs))
	}
}

// TestCorruptLatestIntelligenceIgnored documents a discrepancy: `patterns`
// runs architecture intelligence fresh from the graph and never reads intelligence/latest.json,
// so a corrupt artifact cannot crash it.
func TestCorruptLatestIntelligenceIgnored(t *testing.T) {
	sb := harness.NewSandbox(t)
	sb.WriteAKGState(harness.TinyGraph())
	sb.WriteFile(".glassmarble/intelligence/latest.json", "{ broken artifact")

	out, err := harness.RunGmb(t, sb, "patterns")
	if err != nil {
		t.Fatalf("patterns with corrupt latest.json must still succeed: %v\n%s", err, out)
	}
	if strings.TrimSpace(out) == "" {
		t.Error("patterns produced no output")
	}
}

// TestAtomicCommitLeavesNoTempFiles verifies the tmp+rename commit path:
// no temporary files survive and the persisted state parses cleanly with the
// committed node/edge counts.
func TestAtomicCommitLeavesNoTempFiles(t *testing.T) {
	sb := harness.NewSandbox(t)
	tm, err := akg.NewAKGTransactionManager(sb.GmDir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer tm.Close()

	if err := tm.ExecuteDeltaTransaction(commitPayload("c1", "pkg/a.go::A", "pkg/b.go::B"), []string{"pkg/a.go"}); err != nil {
		t.Fatalf("commit: %v", err)
	}

	entries, err := os.ReadDir(sb.GmDir)
	if err != nil {
		t.Fatalf("readdir %s: %v", sb.GmDir, err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp") {
			t.Errorf("temporary file left behind: %s", e.Name())
		}
	}

	graph, err := akg.ImportGraphJSON(bytes.NewReader([]byte(sb.ReadFile(".glassmarble/akg.json"))))
	if err != nil {
		t.Fatalf("committed state does not parse: %v", err)
	}
	if graph.Version != 1 {
		t.Errorf("committed graph version = %d, want 1", graph.Version)
	}
	if graph.Nodes.Len() != 2 {
		t.Errorf("committed graph has %d nodes, want 2", graph.Nodes.Len())
	}
}

// TestMissingEverythingCommandsGraceful verifies every state-dependent
// command stays graceful on a pristine workspace: human commands report
// uninitialized/empty states, machine commands fail with actionable errors.
func TestMissingEverythingCommandsGraceful(t *testing.T) {
	sb := harness.NewSandbox(t)
	cases := []struct {
		name    string
		args    []string
		wantOut string
		wantErr string
	}{
		{"status", []string{"status"}, "No active AKG database found at", ""},
		{"doctor", []string{"doctor"}, "Uninitialized", ""},
		{"stats", []string{"stats"}, "No telemetry found", ""},
		{"memory", []string{"memory"}, "Developer memory is empty", ""},
		{"timeline", []string{"timeline"}, "Developer memory is empty", ""},
		{"visualize", []string{"visualize", "callgraph"}, "", "active AKG database not found at"},
		{"patterns", []string{"patterns"}, "", "AKG database is empty"},
		{"drift", []string{"drift"}, "", "AKG database is empty"},
		{"export", []string{"export", "--output", "out.json"}, "", "AKG database is empty"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := harness.RunGmb(t, sb, tc.args...)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("expected success: %v\n%s", err, out)
				}
				if !strings.Contains(out, tc.wantOut) {
					t.Errorf("output missing %q:\n%s", tc.wantOut, out)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q:\n%s", tc.wantErr, out)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error %q missing %q", err.Error(), tc.wantErr)
			}
		})
	}
}
