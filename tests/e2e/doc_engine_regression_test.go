package e2e_test

// TestDocEngineRegression* guards previously-fixed docs-engine bugs so they
// never return. Each test names the bug it guards. All commands run IN
// PROCESS via the harness (helpers_test.go conventions), so no test here may
// call t.Parallel(). No network is touched.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Syamchand123/GlassMarble/tests/harness"
)

const regressionDocsYAML = `version: 1
docs_dir: "docs"
constraints:
  min_freshness_threshold: 0
  min_freshness_fail: 0
documents:
  - id: demo
    target: "docs/demo.md"
    title: "Demo"
    purpose: "Fixture for docs-engine regression tests."
    audience: "Developers"
    scope:
      paths: ["internal/demo/**"]
    sections:
      - id: overview
        title: "Overview"
        instruction: "Explain the demo module"
        managed: true
        ground_with: ["signatures"]
`

const regressionDemoMD = `# Demo

<!-- gmb:begin:overview -->
Old demo body.
<!-- gmb:end:overview -->
`

const regressionDemoGo = `package demo

// Greet returns a greeting for name.
func Greet(name string) string {
	return "hi " + name
}

// Add returns the sum of a and b.
func Add(a, b int) int {
	return a + b
}
`

// seedDocSandbox builds a git-backed sandbox with init done, one managed
// doc, and one tiny Go package, all committed on main.
func seedDocSandbox(t *testing.T) *harness.Sandbox {
	t.Helper()
	sb := harness.NewSandbox(t)
	sb.RequireGit()
	gmb(t, sb, "init")
	sb.WriteFile("go.mod", "module example.com/demo\n\ngo 1.21\n")
	sb.WriteFile(".glassmarble/docs.yaml", regressionDocsYAML)
	sb.WriteFile("docs/demo.md", regressionDemoMD)
	sb.WriteFile("internal/demo/app.go", regressionDemoGo)
	sb.GitInit()
	return sb
}

// TestDocEngineRegression_EmptyHashRerenderLoop guards the zero-churn loop
// bug: sections whose persisted hash is empty ("") read as dirty on every
// run, so a second --force run must report no updates and leave bytes
// untouched, and the stored hash must be non-empty.
func TestDocEngineRegression_EmptyHashRerenderLoop(t *testing.T) {
	t.Setenv("GMB_DOC_STATE", "json")
	sb := seedDocSandbox(t)

	gmb(t, sb, "doc", "--no-llm", "--force")
	afterFirst := sb.ReadFile("docs/demo.md")

	out := gmb(t, sb, "doc", "--no-llm", "--force")
	if strings.Contains(out, "section(s) updated") {
		t.Errorf("BUG(empty-hash re-render loop): second --force run re-rendered:\n%s", out)
	}
	if got := sb.ReadFile("docs/demo.md"); got != afterFirst {
		t.Errorf("BUG(empty-hash re-render loop): second run rewrote bytes:\n--- first ---\n%s\n--- second ---\n%s", afterFirst, got)
	}

	raw := sb.ReadFile(".glassmarble/docs_state.json")
	var state struct {
		Documents map[string]struct {
			Sections map[string]struct {
				Hash string `json:"ast_subgraph_hash"`
			} `json:"sections"`
		} `json:"documents"`
	}
	if err := json.Unmarshal([]byte(raw), &state); err != nil {
		t.Fatalf("docs_state.json is not valid JSON: %v", err)
	}
	sec, ok := state.Documents["docs/demo.md"].Sections["overview"]
	if !ok {
		t.Fatalf("BUG(empty-hash re-render loop): no section state for docs/demo.md/overview:\n%s", raw)
	}
	if sec.Hash == "" {
		t.Errorf("BUG(empty-hash re-render loop): persisted empty hash re-dirties every run")
	}
}

// TestDocEngineRegression_StateKeySplitBrain guards the absolute/relative
// split-brain: state must be keyed by the repo-relative DocSpec target
// ("docs/demo.md"), never by the absolute write path, or hash lookups miss
// under a second document entry and every section re-renders forever.
func TestDocEngineRegression_StateKeySplitBrain(t *testing.T) {
	t.Setenv("GMB_DOC_STATE", "json")
	sb := seedDocSandbox(t)

	gmb(t, sb, "doc", "--no-llm", "--force")

	raw := sb.ReadFile(".glassmarble/docs_state.json")
	var state struct {
		Documents map[string]json.RawMessage `json:"documents"`
	}
	if err := json.Unmarshal([]byte(raw), &state); err != nil {
		t.Fatalf("docs_state.json is not valid JSON: %v", err)
	}
	if len(state.Documents) != 1 {
		t.Errorf("BUG(state-key split-brain): want exactly 1 document entry, got %d: %v",
			len(state.Documents), keysOf(state.Documents))
	}
	for key := range state.Documents {
		if filepath.IsAbs(key) {
			t.Errorf("BUG(state-key split-brain): absolute state key %q", key)
		}
		if strings.Contains(key, sb.Root) {
			t.Errorf("BUG(state-key split-brain): state key leaks workdir: %q", key)
		}
	}
	if _, ok := state.Documents["docs/demo.md"]; !ok {
		t.Errorf("BUG(state-key split-brain): missing relative key docs/demo.md, got %v", keysOf(state.Documents))
	}
}

func keysOf(m map[string]json.RawMessage) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// TestDocEngineRegression_CRLFSnippetBlindness guards snippet blindness on
// CRLF working trees: without newline normalization the fence regex finds
// zero blocks and a broken snippet passes verification silently.
func TestDocEngineRegression_CRLFSnippetBlindness(t *testing.T) {
	sb := seedDocSandbox(t)

	crlf := "# Demo\r\n\r\n<!-- gmb:begin:overview -->\r\nBody.\r\n<!-- gmb:end:overview -->\r\n\r\n" +
		"<!-- gmb:snippet:example -->\r\n```go\r\nfunc broken(((\r\n```\r\n"
	sb.WriteFile("docs/demo.md", crlf)

	out, err := gmbErr(t, sb, "doc", "check", "--verify-snippets")
	if err == nil {
		t.Fatalf("BUG(CRLF snippet blindness): broken CRLF snippet passed verification silently:\n%s", out)
	}
	if !strings.Contains(out, "SNIPPET ERROR") {
		t.Errorf("BUG(CRLF snippet blindness): expected a SNIPPET ERROR report, got:\n%s", out)
	}
}

// TestDocEngineRegression_FunctionKindRendersRows guards deterministic
// kind-filter emptiness: real graphs carry FUNCTION-kind symbols (uppercase
// AKG vocabulary) and the Functions table must render rows for them instead
// of an empty table.
func TestDocEngineRegression_FunctionKindRendersRows(t *testing.T) {
	sb := seedDocSandbox(t)

	// --docs wires the AKG head graph into the doc engine; without it the
	// grounding payload is empty and no symbol rows can render. --no-llm
	// pins the deterministic renderer: without it the run may reach a live
	// provider (when keys are present in the environment) and produce free
	// prose instead of the table this guard asserts.
	gmb(t, sb, "analyze", "--docs", "--no-llm")

	body := sb.ReadFile("docs/demo.md")
	if !strings.Contains(body, "Functions and Methods") {
		t.Errorf("BUG(kind-filter emptiness): no Functions table rendered:\n%s", body)
	}
	for _, sym := range []string{"Greet", "Add"} {
		if !strings.Contains(body, "`"+sym+"`") {
			t.Errorf("BUG(kind-filter emptiness): FUNCTION-kind symbol %q rendered no row:\n%s", sym, body)
		}
	}
}

// TestDocEngineRegression_UnknownSubcommandNoArgs guards two CLI contract
// bugs: unknown `doc` subcommands must be rejected, and fixed-arity
// subcommands (e.g. `doc check`) must reject positional args.
func TestDocEngineRegression_UnknownSubcommandNoArgs(t *testing.T) {
	sb := seedDocSandbox(t)

	if out, err := gmbErr(t, sb, "doc", "bogus-subcommand"); err == nil {
		t.Errorf("BUG(unknown-subcommand): `doc bogus-subcommand` succeeded:\n%s", out)
	}
	if out, err := gmbErr(t, sb, "doc", "check", "extra-arg"); err == nil {
		t.Errorf("BUG(NoArgs rejection): `doc check extra-arg` succeeded:\n%s", out)
	}
}

// TestDocEngineRegression_ForceWriteWarningScope guards the ForceWrite
// warning contract: --write on main is a no-op confirmation (no warning),
// compute-only on a feature branch warns, and --write there warns about the
// real override.
func TestDocEngineRegression_ForceWriteWarningScope(t *testing.T) {
	sb := seedDocSandbox(t)

	out := gmb(t, sb, "doc", "--no-llm", "--force", "--write")
	if strings.Contains(out, "ForceWrite override") {
		t.Errorf("BUG(ForceWrite warning): no-op --write on main must not warn:\n%s", out)
	}

	sb.MustGit("checkout", "-b", "feature/docs-refresh")
	out = gmb(t, sb, "doc", "--no-llm", "--force")
	if !strings.Contains(out, "compute-only") {
		t.Errorf("BUG(ForceWrite warning): feature branch without --write must report compute-only:\n%s", out)
	}

	out = gmb(t, sb, "doc", "--no-llm", "--force", "--write")
	if !strings.Contains(out, "ForceWrite override") {
		t.Errorf("BUG(ForceWrite warning): --write on a feature branch must warn about the override:\n%s", out)
	}
}

// TestDocEngineRegression_FixBeforeVerdict guards --fix-before-verdict
// ordering: with --fix, the snippet repair must land in the file BEFORE the
// audit verdict is computed, so the verdict reflects fixed bytes (here: the
// arity error becomes a syntax error at the replaced line) and the run
// reports SNIPPET FIXED.
func TestDocEngineRegression_FixBeforeVerdict(t *testing.T) {
	sb := seedDocSandbox(t)

	broken := regressionDemoMD + "\n<!-- gmb:snippet:example -->\n```go\nGreet(\"a\", \"b\")\n```\n"
	sb.WriteFile("docs/demo.md", broken)

	// Baseline without --fix: the arity verdict fires on original bytes.
	out, err := gmbErr(t, sb, "doc", "check", "--verify-snippets")
	if err == nil {
		t.Fatalf("expected the arity verdict to fail without --fix:\n%s", out)
	}
	if !strings.Contains(out, "takes 1") {
		t.Errorf("expected an arity message naming the declaration, got:\n%s", out)
	}

	// With --fix: the repair lands first (SNIPPET FIXED + file rewritten),
	// and the verdict evaluates the fixed bytes (syntax error at the
	// signature line) instead of the original arity error.
	out, _ = gmbErr(t, sb, "doc", "check", "--verify-snippets", "--fix")
	if !strings.Contains(out, "SNIPPET FIXED") {
		t.Errorf("BUG(--fix-before-verdict): fix did not run before the verdict:\n%s", out)
	}
	if strings.Contains(out, "takes 1") {
		t.Errorf("BUG(--fix-before-verdict): verdict evaluated pre-fix bytes:\n%s", out)
	}
	if !strings.Contains(out, "Syntax error") {
		t.Errorf("BUG(--fix-before-verdict): verdict must reflect the fixed bytes:\n%s", out)
	}
	if got := sb.ReadFile("docs/demo.md"); !strings.Contains(got, "Greet(name string)") {
		t.Errorf("BUG(--fix-before-verdict): fixed signature not in file:\n%s", got)
	}
}

// TestDocEngineRegression_TOCRegen guards TOC regeneration: `doc check
// --fix` must rewrite a stale `<!-- toc -->` block from actual headings,
// drop dead entries, and converge (second run is a no-op).
func TestDocEngineRegression_TOCRegen(t *testing.T) {
	sb := seedDocSandbox(t)

	sb.WriteFile("docs/demo.md", "# Demo\n\n<!-- toc -->\n\n- [Stale](#gone)\n\n# Install\n\n## Usage\n\nBody.\n")

	out := gmb(t, sb, "doc", "check", "--fix")
	if !strings.Contains(out, "TOC REGENERATED") {
		t.Errorf("BUG(TOC regen): stale TOC was not regenerated:\n%s", out)
	}
	once := sb.ReadFile("docs/demo.md")
	for _, want := range []string{"- [Install](#install)", "- [Usage](#usage)"} {
		if !strings.Contains(once, want) {
			t.Errorf("BUG(TOC regen): missing entry %q:\n%s", want, once)
		}
	}
	if strings.Contains(once, "#gone") {
		t.Errorf("BUG(TOC regen): dead entry survived:\n%s", once)
	}

	gmb(t, sb, "doc", "check", "--fix")
	if twice := sb.ReadFile("docs/demo.md"); twice != once {
		t.Errorf("BUG(TOC regen): second regeneration not idempotent:\n--- first ---\n%s\n--- second ---\n%s", once, twice)
	}
}

// TestDocEngineRegression_BloatGuardSanity guards analysis bloat: a tiny
// project must produce a tiny graph (bounded nodes/edges), so a noisy
// producer can never hide behind the docs pipeline.
func TestDocEngineRegression_BloatGuardSanity(t *testing.T) {
	sb := seedDocSandbox(t)

	// --no-llm pins the deterministic renderer: without it the run may reach
	// a live provider (when keys are present in the environment) — real cost,
	// nondeterministic prose, flaky failures. The bloat guard only asserts on
	// akg.json shape, so offline rendering is the correct mode.
	gmb(t, sb, "analyze", "--docs", "--no-llm")

	raw, err := os.ReadFile(sb.Path(".glassmarble", "akg.json"))
	if err != nil {
		t.Fatalf("reading akg.json: %v", err)
	}
	var graph struct {
		Nodes []json.RawMessage `json:"nodes"`
		Edges []json.RawMessage `json:"edges"`
	}
	if err := json.Unmarshal(raw, &graph); err != nil {
		t.Fatalf("akg.json is not valid JSON: %v", err)
	}
	if len(graph.Nodes) == 0 {
		t.Error("BUG(bloat-guard sanity): tiny project produced an empty graph (pipeline broken)")
	}
	if len(graph.Nodes) > 500 {
		t.Errorf("BUG(bloat-guard sanity): %d nodes for a 2-file project exceeds the sanity budget", len(graph.Nodes))
	}
	if len(graph.Edges) > 2000 {
		t.Errorf("BUG(bloat-guard sanity): %d edges for a 2-file project exceeds the sanity budget", len(graph.Edges))
	}
}
