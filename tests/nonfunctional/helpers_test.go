// Package nonfunctional_test holds NON-FUNCTIONAL tests for GlassMarble:
// performance budgets, determinism/idempotency, concurrency, corruption
// recovery, and degraded-environment fallbacks.
//
// These tests are compile-verified with `go vet ./tests/nonfunctional/...`
// and are intentionally excluded from the default test runner: several
// assertions (lock ordering, budget gates) depend on whole-repository
// behavior that would make them slow or flaky in CI. They never use
// t.Parallel() — the harness CLI runner mutates process-global state.
package nonfunctional_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/archmodel"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/link"
	"github.com/Syamchand123/GlassMarble/internal/evidence"
	"github.com/Syamchand123/GlassMarble/tests/harness"
)

// commitPayload builds a minimal valid LinkOutput for
// ExecuteDeltaTransaction: a chain of FUNCTION nodes with one CALLS edge.
func commitPayload(commitHash string, ids ...string) *link.LinkOutput {
	payload := &link.LinkOutput{
		CommitHash:         commitHash,
		GraphNodes:         map[string]*link.ResolvedNode{},
		OutboundEdges:      map[string][]link.ResolvedEdge{},
		InboundEdges:       map[string][]link.ResolvedEdge{},
		EntrypointRegistry: []string{},
	}
	for _, id := range ids {
		payload.GraphNodes[id] = &link.ResolvedNode{
			ID:       id,
			Kind:     "FUNCTION",
			Name:     id,
			FileSpec: link.LocationMeta{Path: "src/gen.go", LineStart: 1, LineEnd: 10},
		}
	}
	if len(ids) >= 2 {
		src, tgt := ids[0], ids[1]
		edge := link.ResolvedEdge{SourceID: src, TargetID: tgt, Type: link.EdgeCalls, LineNumber: 1, Confidence: 1.0}
		payload.OutboundEdges[src] = []link.ResolvedEdge{edge}
		payload.InboundEdges[tgt] = []link.ResolvedEdge{edge}
		payload.EntrypointRegistry = []string{src}
	}
	return payload
}

// memEvent builds a valid developer-memory event for AppendEvent.
func memEvent(id string, ts time.Time) archmodel.ArchEvent {
	return archmodel.ArchEvent{
		ID:         id,
		Kind:       archmodel.EventServiceAdded,
		CommitHash: "commit-" + id,
		Timestamp:  ts,
		Title:      "change " + id,
		Components: []string{"svc-" + id},
		Evidence: evidence.NewBundle(evidence.EvidenceItem{
			Source:     evidence.SourceGit,
			Reference:  "commit-" + id,
			Confidence: 0.9,
			Timestamp:  ts,
		}),
	}
}

// countJSONLLines counts non-empty lines in a JSONL WAL file (0 when absent).
func countJSONLLines(t *testing.T, path string) int {
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

// decodeJSONFile unmarshals a JSON file into v.
func decodeJSONFile(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

// gmVersion reads the persisted graph version from the AKG state file.
func gmVersion(t *testing.T, sb *harness.Sandbox) uint64 {
	t.Helper()
	_, _, v, err := akg.StateMetadata(sb.GmDir)
	if err != nil {
		t.Fatalf("StateMetadata(%s): %v", sb.GmDir, err)
	}
	return v
}

// exportNodeIDs runs `gmb export` and returns the sorted node IDs, commit
// hash, and edge count of the exported GraphJSON document.
// out must be a dot-prefixed name. The export lands inside the sandbox, which
// is also the repository under analysis, and JSON is a parsed language — so an
// export written as "graph1.json" becomes a ~100KB source file in the very
// repository a determinism test is about to re-analyze, and the second run
// legitimately sees a graph the first did not. Hidden files are skipped by the
// walker, which keeps the artifact out of the tree being measured.
//
// This only surfaced once untracked-but-not-ignored files entered the scan;
// before that the fixture was writing into the tree and being saved by a
// filter that hid it.
func exportNodeIDs(t *testing.T, sb *harness.Sandbox, out string) (ids []string, commit string, edges int) {
	t.Helper()
	if !strings.HasPrefix(out, ".") {
		t.Fatalf("export target %q must be dot-prefixed so it is not analyzed as part of the repository", out)
	}
	if _, err := harness.RunGmb(t, sb, "export", "--output", out); err != nil {
		t.Fatalf("export to %s: %v", out, err)
	}
	var doc akg.GraphJSON
	if err := json.Unmarshal([]byte(sb.ReadFile(out)), &doc); err != nil {
		t.Fatalf("decode export %s: %v", out, err)
	}
	ids = make([]string, 0, len(doc.Nodes))
	for _, n := range doc.Nodes {
		ids = append(ids, n.ID)
	}
	sort.Strings(ids)
	return ids, doc.CommitHash, len(doc.Edges)
}

// statusNodeCount decodes `gmb status --json` and returns the node count.
func statusNodeCount(t *testing.T, sb *harness.Sandbox) int {
	t.Helper()
	out, err := harness.RunGmb(t, sb, "status", "--json")
	if err != nil {
		t.Fatalf("status --json: %v", err)
	}
	var st struct {
		Nodes int `json:"nodes"`
	}
	if err := json.Unmarshal([]byte(out), &st); err != nil {
		t.Fatalf("decode status json: %v", err)
	}
	return st.Nodes
}

// fixedGitDate pins author+committer timestamps so two sandboxes with
// identical trees produce byte-identical commits (determinism tests).
const fixedGitDate = "2024-03-15T12:00:00Z"

func fixedGit(t *testing.T, sb *harness.Sandbox, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = sb.Root
	cmd.Env = append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"GIT_AUTHOR_NAME=NFR Test",
		"GIT_AUTHOR_EMAIL=nfr@glassmarble.test",
		"GIT_AUTHOR_DATE="+fixedGitDate,
		"GIT_COMMITTER_NAME=NFR Test",
		"GIT_COMMITTER_EMAIL=nfr@glassmarble.test",
		"GIT_COMMITTER_DATE="+fixedGitDate,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// gitInitFixed initializes a git repo whose commits use fixed dates/identity.
func gitInitFixed(t *testing.T, sb *harness.Sandbox) {
	t.Helper()
	sb.RequireGit()
	fixedGit(t, sb, "init", "-q", "-b", "main")
	fixedGit(t, sb, "config", "user.name", "NFR Test")
	fixedGit(t, sb, "config", "user.email", "nfr@glassmarble.test")
}

// gitCommitAllFixed stages everything and commits with the fixed identity,
// returning the resulting HEAD hash.
func gitCommitAllFixed(t *testing.T, sb *harness.Sandbox, msg string) string {
	t.Helper()
	fixedGit(t, sb, "add", "-A")
	fixedGit(t, sb, "commit", "-q", "-m", msg)
	return fixedGit(t, sb, "rev-parse", "HEAD")
}
