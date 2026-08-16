package nonfunctional_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Syamchand123/GlassMarble/tests/harness"
)

// TestSnapshotListEmptyFresh verifies the snapshot index degrades to a clear
// message on a pristine workspace instead of failing.
func TestSnapshotListEmptyFresh(t *testing.T) {
	sb := harness.NewSandbox(t)
	out, err := harness.RunGmb(t, sb, "snapshot", "--list")
	if err != nil {
		t.Fatalf("snapshot --list: %v", err)
	}
	if !strings.Contains(out, "No snapshots yet") {
		t.Errorf("output missing empty-index message:\n%s", out)
	}
}

// TestSnapshotAtUnknownRef verifies an unresolvable ref yields an actionable
// "no snapshot" error rather than a panic.
func TestSnapshotAtUnknownRef(t *testing.T) {
	sb := harness.NewSandbox(t)
	out, err := harness.RunGmb(t, sb, "snapshot", "--at", "0000000000000000000000000000000000000000")
	if err == nil {
		t.Fatalf("expected resolution failure:\n%s", out)
	}
	if !strings.Contains(err.Error(), "no snapshot for") {
		t.Errorf("error %q missing no-snapshot message", err.Error())
	}
}

// TestPatternsFreshWithoutIntelligence verifies Architecture Intelligence runs fresh from the
// graph when no intelligence artifact exists — intelligence/latest.json is
// an output, never an input.
func TestPatternsFreshWithoutIntelligence(t *testing.T) {
	sb := harness.NewSandbox(t)
	sb.WriteAKGState(harness.TinyGraph())

	out, err := harness.RunGmb(t, sb, "patterns")
	if err != nil {
		t.Fatalf("patterns without latest.json: %v", err)
	}
	if strings.TrimSpace(out) == "" {
		t.Error("patterns produced no output")
	}
}

// TestMemoryAskEmptyMemory verifies `memory --ask` degrades to a helpful
// message on an empty memory store.
func TestMemoryAskEmptyMemory(t *testing.T) {
	sb := harness.NewSandbox(t)
	out, err := harness.RunGmb(t, sb, "memory", "--ask", "cache")
	if err != nil {
		t.Fatalf("memory --ask: %v", err)
	}
	if !strings.Contains(out, "The memory holds nothing about") {
		t.Errorf("output missing empty-memory message:\n%s", out)
	}
}

// TestAnalyzeWithoutGitSucceeds documents a discrepancy: git is optional.
// `analyze` on a non-git directory falls back to a full non-git scan and
// succeeds, instead of failing as a missing-git expectation might suggest.
func TestAnalyzeWithoutGitSucceeds(t *testing.T) {
	sb := harness.NewSandbox(t)
	sb.SampleProject()

	out, err := harness.RunGmb(t, sb, "analyze")
	if err != nil {
		t.Fatalf("analyze without git: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Analyzed") {
		t.Errorf("output missing analysis summary:\n%s", out)
	}
	if n := statusNodeCount(t, sb); n == 0 {
		t.Error("non-git analysis produced no nodes")
	}
}

// TestWatchRequiresGit verifies `watch` refuses to start without a git
// repository (its delta pipeline needs HEAD).
func TestWatchRequiresGit(t *testing.T) {
	sb := harness.NewSandbox(t)
	sb.SampleProject()

	out, err := harness.RunGmb(t, sb, "watch")
	if err == nil {
		t.Fatalf("watch without git must fail:\n%s", out)
	}
	if !strings.Contains(err.Error(), "watch requires a git repository") {
		t.Errorf("error %q missing requirement message", err.Error())
	}
}

// TestAIConfigFallback verifies the AI subcommands degrade cleanly with no
// configuration: doctor reports problems without making any network call,
// and a question fails with an actionable no-key error.
//
// Note: the engine may also read a user-level ~/.glassmarble/ai.yaml; when a
// real config exists there, this test's no-key expectation cannot hold.
func TestAIConfigFallback(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("GLASSMARBLE_AI_API_KEY", "")
	t.Setenv("GLASSMARBLE_OPENAI_API_KEY", "")
	t.Setenv("GEMINI_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	sb := harness.NewSandbox(t)

	out, err := harness.RunGmb(t, sb, "ai", "doctor")
	if err == nil || !strings.Contains(err.Error(), "doctor found") {
		t.Fatalf("ai doctor = (%v), want problem-report error:\n%s", err, out)
	}
	if !strings.Contains(out, "not found at") {
		t.Errorf("doctor output missing missing-AKG problem:\n%s", out)
	}

	out, err = harness.RunGmb(t, sb, "ai", "what does the architecture look like?")
	if err == nil {
		t.Fatalf("ai without a key must fail:\n%s", out)
	}
	if !strings.Contains(err.Error(), "no API key") {
		t.Errorf("error %q missing no-key message", err.Error())
	}
}

// TestCompareEmptyDatabase verifies the working-tree compare path fails
// clearly when no database exists.
func TestCompareEmptyDatabase(t *testing.T) {
	sb := harness.NewSandbox(t)
	out, err := harness.RunGmb(t, sb, "compare")
	if err == nil {
		t.Fatalf("compare without state must fail:\n%s", out)
	}
	if !strings.Contains(err.Error(), "AKG database is empty") {
		t.Errorf("error %q missing empty-database message", err.Error())
	}
}

// TestHousekeepingMissingDirs verifies housekeeping reports zero sizes for
// missing working-set areas instead of failing.
func TestHousekeepingMissingDirs(t *testing.T) {
	sb := harness.NewSandbox(t)
	out, err := harness.RunGmb(t, sb, "housekeeping")
	if err != nil {
		t.Fatalf("housekeeping: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Working Set") {
		t.Errorf("output missing report header:\n%s", out)
	}
	if !strings.Contains(out, "Total") {
		t.Errorf("output missing total row:\n%s", out)
	}
}

// TestExportUnwritableOutput verifies export fails with an actionable
// message when the output path cannot be created.
func TestExportUnwritableOutput(t *testing.T) {
	sb := harness.NewSandbox(t)
	sb.WriteAKGState(harness.TinyGraph())

	bad := filepath.Join(sb.Root, "no_such_dir", "out.json")
	out, err := harness.RunGmb(t, sb, "export", "--output", bad)
	if err == nil {
		t.Fatalf("export to unwritable path must fail:\n%s", out)
	}
	if !strings.Contains(err.Error(), "failed to create output file") {
		t.Errorf("error %q missing create-failure message", err.Error())
	}
}

// TestImportSchemaV1Rejected verifies legacy schema-v1 documents are refused
// at import with a clear message (minimum supported schema is v2).
func TestImportSchemaV1Rejected(t *testing.T) {
	sb := harness.NewSandbox(t)
	sb.WriteFile("v1.json", `{"schema_version":1,"commit_hash":"deadbeef","version":0,"nodes":[],"edges":[]}`)

	out, err := harness.RunGmb(t, sb, "import", "v1.json")
	if err == nil {
		t.Fatalf("import of schema v1 must fail:\n%s", out)
	}
	if !strings.Contains(err.Error(), "schema v1") {
		t.Errorf("error %q missing schema-v1 message", err.Error())
	}
}

// TestVisualizeExitCodes exercises the real binary and asserts the exit
// contract: success exits 0, every failure mode exits non-zero with a
// diagnostic on stderr, and the failure classes map to the exit codes
// documented in cmd/visualize.go (1 validation, 2 entry missing, 3 empty
// subgraph, 4 render limit — main.go dispatches via errors.Is on the
// producterrs taxonomy).
//
// The stderr pins match case-insensitively: Fang's styled error skin
// capitalizes the first letter and appends a trailing period.
func TestVisualizeExitCodes(t *testing.T) {
	bin := harness.BuildBinary(t)
	sb := harness.NewSandbox(t)
	sb.WriteAKGState(harness.TinyGraph())

	_, stderr, code := harness.RunBinary(t, bin, sb.Root, nil, "visualize", "callgraph", "--dir", ".")
	if code != 0 {
		t.Fatalf("callgraph exit = %d, want 0\n%s", code, stderr)
	}

	cases := []struct {
		name     string
		args     []string
		want     string
		wantCode int
	}{
		{"unsupported diagram", []string{"visualize", "bogus", "--dir", "."}, "unsupported diagram type", 1},
		{"sequence without entry", []string{"visualize", "sequence", "--dir", "."}, "entry point ID (--entry) is mandatory", 2},
		{"package empty subgraph", []string{"visualize", "package", "--dir", "."}, "empty subgraph", 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, stderr, code := harness.RunBinary(t, bin, sb.Root, nil, tc.args...)
			if code == 0 {
				t.Fatalf("expected non-zero exit\n%s", stderr)
			}
			if code != tc.wantCode {
				t.Errorf("exit = %d, want %d (documented in cmd/visualize.go)\n%s", code, tc.wantCode, stderr)
			}
			if !strings.Contains(strings.ToLower(stderr), strings.ToLower(tc.want)) {
				t.Errorf("stderr missing %q:\n%s", tc.want, stderr)
			}
		})
	}
}
