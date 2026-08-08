package cmd_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/archmodel"
)

// snapshotRoot builds a repo with two commits, each analyzed, producing two
// snapshots with different topology. Returns the repo root and the two HEAD
// commit hashes (oldest first).
func snapshotRoot(t *testing.T) (string, string, string) {
	t.Helper()
	root := setupAnalyzeGitRepo(t)

	if _, err := runGmbCommand(t, "analyze", "--dir", root, "--stage5"); err != nil {
		t.Fatalf("first analyze failed: %v", err)
	}

	// Second commit: change b.go so the topology differs.
	if err := os.WriteFile(filepath.Join(root, "b.go"), []byte("package main\n\nfunc Beep() {}\nfunc Boop() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitCmd(t, root, "add", "b.go")
	runGitCmd(t, root, "commit", "-q", "-m", "b grows")

	head1 := runGitCmd(t, root, "rev-parse", "HEAD~1")
	head2 := runGitCmd(t, root, "rev-parse", "HEAD")

	if _, err := runGmbCommand(t, "analyze", "--dir", root, "--stage5"); err != nil {
		t.Fatalf("second analyze failed: %v", err)
	}
	return root, head1, head2
}

// TestSnapshotCLI_Lifecycle drives create → list → at → create-again →
// diff across two analyzed commits.
func TestSnapshotCLI_Lifecycle(t *testing.T) {
	root, head1, head2 := snapshotRoot(t)

	// --list shows both snapshots.
	list, err := runGmbCommand(t, "snapshot", "--dir", root, "--list")
	if err != nil {
		t.Fatalf("snapshot --list failed: %v\n%s", err, list)
	}
	if !strings.Contains(list, "SNAPSHOT ID") {
		t.Errorf("list header missing:\n%s", list)
	}
	if !strings.Contains(list, "snap_") || !strings.Contains(list, head1[:8]) || !strings.Contains(list, head2[:8]) {
		t.Errorf("list missing snapshots/commits:\n%s", list)
	}

	// --at HEAD resolves to the newest snapshot.
	at, err := runGmbCommand(t, "snapshot", "--dir", root, "--at", "HEAD")
	if err != nil {
		t.Fatalf("snapshot --at HEAD failed: %v\n%s", err, at)
	}
	if !strings.Contains(at, "Snapshot snap_") || !strings.Contains(at, head2[:8]) {
		t.Errorf("--at HEAD did not show the head snapshot:\n%s", at)
	}

	// --at with the earlier commit prefix → nearest (exact) snapshot.
	at1, err := runGmbCommand(t, "snapshot", "--dir", root, "--at", head1[:8])
	if err != nil {
		t.Fatalf("snapshot --at earlier failed: %v\n%s", err, at1)
	}
	if !strings.Contains(at1, head1[:8]) {
		t.Errorf("--at earlier commit did not resolve:\n%s", at1)
	}

	// --at --json emits the full snapshot document.
	atJSON, err := runGmbCommand(t, "snapshot", "--dir", root, "--at", "HEAD", "--json")
	if err != nil {
		t.Fatalf("snapshot --at --json failed: %v", err)
	}
	var doc archmodel.ArchSnapshot
	if err := json.Unmarshal([]byte(atJSON), &doc); err != nil {
		t.Fatalf("--at --json output is not a snapshot document: %v\n%s", err, atJSON)
	}
	if doc.ID == "" || doc.CommitHash == "" || len(doc.Components) == 0 {
		t.Errorf("--at --json document incomplete: %+v", doc)
	}

	// Re-creating the same state must skip the write.
	create, err := runGmbCommand(t, "snapshot", "--dir", root, "--create")
	if err != nil {
		t.Fatalf("snapshot --create failed: %v\n%s", err, create)
	}
	if !strings.Contains(create, "Snapshot unchanged") {
		t.Errorf("re-creating the identical topology should be skipped:\n%s", create)
	}
}

// TestSnapshotCLI_Diff covers the enriched diff report across two commits,
// including the structural section and JSON mode.
func TestSnapshotCLI_Diff(t *testing.T) {
	root, head1, head2 := snapshotRoot(t)

	out, err := runGmbCommand(t, "snapshot", "--dir", root, "--diff", head1[:8], head2[:8])
	if err != nil {
		t.Fatalf("snapshot --diff failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Architecture diff") || !strings.Contains(out, "→") {
		t.Errorf("diff header missing:\n%s", out)
	}
	if !strings.Contains(out, "Components +") || !strings.Contains(out, "coupling") {
		t.Errorf("summary line missing:\n%s", out)
	}
	if !strings.Contains(out, "Structural changes:") {
		t.Errorf("structural diff section missing:\n%s", out)
	}
	// Node changes from the b.go edit must surface.
	if !strings.Contains(out, "ADDED") && !strings.Contains(out, "added") {
		t.Errorf("structural node changes missing:\n%s", out)
	}

	// JSON mode must parse and carry the delta + graph diff.
	j, err := runGmbCommand(t, "snapshot", "--dir", root, "--diff", "HEAD~1", "HEAD", "--json")
	if err != nil {
		t.Fatalf("snapshot --diff --json failed: %v\n%s", err, j)
	}
	var dr struct {
		Delta *struct {
			BaseSnapshot string `json:"base_snapshot"`
			HeadSnapshot string `json:"head_snapshot"`
			MetricDelta  struct {
				SummaryLine string `json:"summary_line"`
			} `json:"metric_delta"`
		} `json:"delta"`
		Graph *struct {
			NodesAdded []any `json:"nodes_added"`
		} `json:"graph"`
	}
	if err := json.Unmarshal([]byte(j), &dr); err != nil {
		t.Fatalf("--diff --json output is not valid JSON: %v\n%s", err, j)
	}
	if dr.Delta == nil || dr.Delta.BaseSnapshot == "" || dr.Delta.HeadSnapshot == "" {
		t.Errorf("diff JSON missing delta identity: %+v", dr.Delta)
	}
	if dr.Graph == nil {
		t.Errorf("diff JSON missing the structural graph diff")
	}
}

// TestSnapshotCLI_Replay renders a diagram from the embedded graph.
func TestSnapshotCLI_Replay(t *testing.T) {
	root, _, _ := snapshotRoot(t)

	out, err := runGmbCommand(t, "snapshot", "--dir", root, "--replay", "HEAD", "--diagram", "dependency")
	if err != nil {
		t.Fatalf("snapshot --replay failed: %v\n%s", err, out)
	}
	if strings.TrimSpace(out) == "" {
		t.Errorf("--replay produced no output")
	}
}

// TestSnapshotCLI_NoGraphFlow: --create --no-graph stores a graph-less
// snapshot, --list shows it, and --replay on it fails with a clear message.
func TestSnapshotCLI_NoGraphFlow(t *testing.T) {
	root := setupAnalyzeGitRepo(t)
	if _, err := runGmbCommand(t, "analyze", "--dir", root, "--stage5"); err != nil {
		t.Fatalf("analyze failed: %v", err)
	}

	j, err := runGmbCommand(t, "snapshot", "--dir", root, "--create", "--no-graph", "--json")
	if err != nil {
		t.Fatalf("snapshot --create --no-graph failed: %v\n%s", err, j)
	}
	var doc archmodel.ArchSnapshot
	if err := json.Unmarshal([]byte(j), &doc); err != nil {
		t.Fatalf("--create --json output is not a snapshot document: %v\n%s", err, j)
	}
	if doc.ID == "" || len(doc.AKGJSON) != 0 {
		t.Fatalf("no-graph snapshot must have an ID and no AKGJSON: %+v", doc)
	}

	replay, err := runGmbCommand(t, "snapshot", "--dir", root, "--replay", doc.ID)
	if err == nil {
		t.Fatalf("--replay on a --no-graph snapshot should fail, got:\n%s", replay)
	}
	if !strings.Contains(err.Error(), "no-graph") {
		t.Errorf("--replay error should mention --no-graph, got: %v", err)
	}
}

// TestSnapshotCLI_Validation: mode conflicts and missing state must fail
// with clear messages, never panic.
func TestSnapshotCLI_Validation(t *testing.T) {
	root := setupAnalyzeGitRepo(t)

	if _, err := runGmbCommand(t, "snapshot", "--dir", root); err == nil {
		t.Error("snapshot with no mode must error")
	} else if !strings.Contains(err.Error(), "no mode selected") {
		t.Errorf("unexpected no-mode error: %v", err)
	}

	if _, err := runGmbCommand(t, "snapshot", "--dir", root, "--list", "--at", "HEAD"); err == nil {
		t.Error("two modes at once must error")
	} else if !strings.Contains(err.Error(), "only one mode") {
		t.Errorf("unexpected multi-mode error: %v", err)
	}

	// --create on a repo with no AKG state must explain itself.
	empty := t.TempDir()
	create, err := runGmbCommand(t, "snapshot", "--dir", empty, "--create")
	if err == nil {
		t.Fatalf("--create on empty state should fail, got:\n%s", create)
	}
	if !strings.Contains(err.Error(), "AKG database is empty") {
		t.Errorf("unexpected empty-state error: %v", err)
	}

	// Unknown refs on an empty store → clear miss, not a panic.
	if _, err := runGmbCommand(t, "snapshot", "--dir", empty, "--at", "HEAD"); err == nil {
		t.Error("--at on empty store must error")
	}
}
