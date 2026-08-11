package e2e_test

// Snapshot and timeline lifecycle on a real analyzed repository: one
// analysis, two commits, then every snapshot mode (--create, --list, --at,
// --diff, --replay) plus the derived timeline views. In-process runner, so
// no t.Parallel().

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Syamchand123/GlassMarble/tests/harness"
)

func TestSnapshotAndTimelineLifecycle(t *testing.T) {
	sb := harness.NewSandbox(t)
	sb.RequireGit()

	sb.SampleProject()
	sb.GitInit()
	gmb(t, sb, "analyze")
	commit1 := sb.GitHead()

	// Evolve: add a second package so the two snapshots really differ.
	commit2 := sb.GitCommitFiles("add checkout service", map[string]string{
		"internal/checkout/checkout.go": `package checkout

import "example.com/shop/internal/cache"

// Processor coordinates the checkout flow through the cache layer.
type Processor struct {
	cache *cache.Cache
}

func NewProcessor(c *cache.Cache) *Processor {
	return &Processor{cache: c}
}
`,
	})
	gmb(t, sb, "analyze")

	// --- snapshot --list ------------------------------------------------------
	listOut := gmbWant(t, sb, []string{"SNAPSHOT ID", "COMMIT", "PATS", "SMELLS"}, "snapshot", "--list")
	if got := strings.Count(listOut, "snap_"); got != 2 {
		t.Errorf("expected exactly 2 snapshots (one per analyzed commit), got %d:\n%s", got, listOut)
	}
	if !strings.Contains(listOut, commit1[:8]) || !strings.Contains(listOut, commit2[:8]) {
		t.Errorf("snapshot list should reference both commits:\n%s", listOut)
	}

	// --- snapshot --at <commit> and --at HEAD ---------------------------------
	for _, ref := range []string{commit1, commit2, "HEAD"} {
		out := gmbWant(t, sb, []string{"Snapshot snap_", "commit:", "components:"}, "snapshot", "--at", ref)
		if !strings.Contains(out, "graph:") {
			t.Errorf("snapshot --at %s missing graph line:\n%s", ref, out)
		}
	}

	// --- snapshot --at --json --------------------------------------------------
	jsonOut := gmb(t, sb, "snapshot", "--at", commit2, "--json")
	var snap map[string]any
	if err := json.Unmarshal([]byte(jsonOut), &snap); err != nil {
		t.Fatalf("snapshot --at --json invalid: %v\n%s", err, jsonOut)
	}
	if snap["commit_hash"] == nil || snap["node_count"] == nil {
		t.Errorf("snapshot json missing fields: %v", snap)
	}

	// --- snapshot --diff <base> <head> ------------------------------------------
	diffOut := gmbWant(t, sb, []string{"Architecture diff", "→"}, "snapshot", "--diff", commit1, commit2)
	if !strings.Contains(diffOut, commit1[:8]) || !strings.Contains(diffOut, commit2[:8]) {
		t.Errorf("snapshot diff should name both refs:\n%s", diffOut)
	}
	if !strings.Contains(diffOut, "checkout") {
		t.Logf("snapshot diff did not mention the new package (informational):\n%s", diffOut)
	}

	// --- snapshot --create (skip-write at unchanged topology) --------------------
	createOut := gmb(t, sb, "snapshot", "--create")
	if !strings.Contains(createOut, "Snapshot unchanged") {
		t.Errorf("expected skip-write at unchanged topology, got:\n%s", createOut)
	}

	// --- snapshot --replay --------------------------------------------------------
	gmbWant(t, sb, []string{"graph TD"}, "snapshot", "--replay", "HEAD", "--diagram", "dependency")

	// --- timeline text -------------------------------------------------------------
	timelineOut := gmb(t, sb, "timeline", "--full")
	if !strings.Contains(timelineOut, "commit:") {
		t.Errorf("timeline --full missing commit hashes:\n%s", timelineOut)
	}

	// --- timeline json --------------------------------------------------------------
	tlJSON := gmb(t, sb, "timeline", "--format", "json")
	var entries []map[string]any
	if err := json.Unmarshal([]byte(tlJSON), &entries); err != nil {
		t.Fatalf("timeline --format json invalid: %v\n%s", err, tlJSON)
	}
	if len(entries) == 0 {
		t.Errorf("expected timeline entries:\n%s", tlJSON)
	}

	// --- timeline component filter ---------------------------------------------------
	gmbWant(t, sb, []string{"checkout", "commit:"}, "timeline", "--component", "checkout", "--full")

	// --- timeline mermaid -------------------------------------------------------------
	gmbWant(t, sb, []string{"timeline", "title Architecture Evolution"}, "timeline", "--format", "mermaid")
}
