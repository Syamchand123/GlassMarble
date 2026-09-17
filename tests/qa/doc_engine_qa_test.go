// Package qa_test holds quality-assurance suites for the docs-engine feature:
// golden renderer pins, determinism proofs, freshness-math spot checks,
// quality-firewall gate mutation, zero-churn proof, prompt snapshot, and
// JSON schema stability. Public API only (CLI + exported packages); no
// production code is modified. No t.Parallel(): the harness runner mutates
// process-global state.
package qa_test

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"

	docengine "github.com/Syamchand123/GlassMarble/internal/doc_engine"
	docconfig "github.com/Syamchand123/GlassMarble/internal/doc_engine/config"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/renderer"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/verifier"
	"github.com/Syamchand123/GlassMarble/tests/harness"
)

// docEngQAConfig is a minimal valid docs.yaml: one document, one managed
// section.
const docEngQAConfig = `version: 1
docs_dir: docs
documents:
  - id: guide
    target: docs/guide.md
    title: Guide
    purpose: QA fixture document.
    audience: Developers
    scope:
      paths: ["pkg/**"]
    sections:
      - id: overview
        title: Overview
        instruction: Document the current state.
`

// docEngQARepo builds a git repo with one scoped Go file and the QA config,
// returning the sandbox and HEAD hash.
func docEngQARepo(t *testing.T, commitMsg string) (*harness.Sandbox, string) {
	t.Helper()
	sb := harness.NewSandbox(t)
	sb.RequireGit()
	sb.GitInit()
	head := sb.GitCommitFiles(commitMsg, map[string]string{
		"pkg/alpha.go": "package alpha\n\n// Alpha does the thing.\nfunc Alpha() string { return \"ok\" }\n",
	})
	sb.WriteFile(".glassmarble/docs.yaml", docEngQAConfig)
	return sb, head
}

// docEngQADocRun runs the deterministic offline update path and requires
// exit 0.
func docEngQADocRun(t *testing.T, sb *harness.Sandbox, extra ...string) string {
	t.Helper()
	args := []string{"doc", "--no-llm", "--force", "--branch-policy", "any"}
	args = append(args, extra...)
	out, err := harness.RunGmb(t, sb, args...)
	if err != nil {
		t.Fatalf("gmb doc failed: %v\n--- output ---\n%s", err, out)
	}
	return out
}

// TestDocEngineQAGoldenDeterministicRender pins the ENTIRE deterministic
// renderer output for a fixed FactSheet. Any template drift (column order,
// mode tag, table separators) breaks this test by design.
func TestDocEngineQAGoldenDeterministicRender(t *testing.T) {
	fs := &docconfig.FactSheet{
		DocID:              "guide",
		SectionID:          "overview",
		SectionInstruction: "Table of exported types, methods, and sentinel errors",
		GroundTruth: docconfig.GroundTruthPayload{
			Symbols: []docconfig.SymbolFact{
				{
					FQN:       "internal/auth/service.go::Authenticate",
					Kind:      "func",
					Signature: "func Authenticate(user string) error",
					Doc:       "Authenticate verifies a user.",
					File:      "internal/auth/service.go",
					Line:      42,
				},
			},
		},
	}
	got, err := renderer.NewDeterministicRenderer().RenderSection(fs)
	if err != nil {
		t.Fatalf("RenderSection: %v", err)
	}
	const golden = "<!-- gmb:mode:deterministic -->\n" +
		"\n" +
		"> Table of exported types, methods, and sentinel errors\n" +
		"\n" +
		"### Functions and Methods\n" +
		"\n" +
		"| Name | Signature | Description | File |\n" +
		"| --- | --- | --- | --- |\n" +
		"| `Authenticate` | `func Authenticate(user string) error` | Authenticate verifies a user. | `service.go:42` |\n"
	if got != golden {
		t.Errorf("deterministic render drift:\n--- want ---\n%s\n--- got ---\n%s", golden, got)
	}
}

// TestDocEngineQADeterminism proves two forced runs are byte-identical,
// including engine state (modulo wall-clock timestamps, which are scrubbed
// before comparison — they are observability metadata, not content).
func TestDocEngineQADeterminism(t *testing.T) {
	t.Setenv("GMB_DOC_STATE", "json")
	sb, head := docEngQARepo(t, "docs: qa determinism fixture")
	docEngQADocRun(t, sb, "--commit", head)
	md1 := sb.ReadFile("docs/guide.md")
	st1 := docEngQAScrubbedState(t, sb)

	docEngQADocRun(t, sb, "--commit", head)
	md2 := sb.ReadFile("docs/guide.md")
	st2 := docEngQAScrubbedState(t, sb)

	if md1 != md2 {
		t.Errorf("doc run not deterministic:\n--- run1 ---\n%s\n--- run2 ---\n%s", md1, md2)
	}
	if st1 != st2 {
		t.Errorf("engine state not deterministic:\n--- run1 ---\n%s\n--- run2 ---\n%s", st1, st2)
	}
}

// docEngQAScrubbedState loads docs_state.json, deletes wall-clock keys
// recursively, and returns the canonical JSON string.
func docEngQAScrubbedState(t *testing.T, sb *harness.Sandbox) string {
	t.Helper()
	var v map[string]any
	if err := json.Unmarshal([]byte(sb.ReadFile(".glassmarble/docs_state.json")), &v); err != nil {
		t.Fatalf("state must be valid JSON: %v", err)
	}
	docEngQAScrub(v)
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("re-marshal state: %v", err)
	}
	return string(data)
}

func docEngQAScrub(v map[string]any) {
	delete(v, "generated_at")
	delete(v, "last_updated_at")
	for _, child := range v {
		switch c := child.(type) {
		case map[string]any:
			docEngQAScrub(c)
		case []any:
			for _, e := range c {
				if m, ok := e.(map[string]any); ok {
					docEngQAScrub(m)
				}
			}
		}
	}
}

// TestDocEngineQAFreshnessMath spot-checks the Appendix B scoring formula
// against engineered git history: zero-churn is exactly 100, one fresh feat
// commit lands in a tight band, structural arch events deepen the penalty
// (base 1.0 vs 0.5), and a 60-day-old commit decays to a smaller penalty.
func TestDocEngineQAFreshnessMath(t *testing.T) {
	sb := harness.NewSandbox(t)
	sb.RequireGit()
	sb.GitInit()
	doc := docconfig.DocSpec{
		ID:         "guide",
		TargetPath: "docs/guide.md",
		Scope:      docconfig.ScopeRule{Paths: []string{"pkg/**"}},
	}

	base := sb.GitCommitFiles("chore: init", map[string]string{
		"pkg/gamma.go": "package gamma\n\nfunc Gamma() {}\n",
	})
	old := docEngQADatedCommit(t, sb, "2026-05-01T12:00:00Z", "feat: add gamma widget", map[string]string{
		"pkg/gamma.go": "package gamma\n\n// Gamma handles widgets.\nfunc Gamma() string { return \"gamma\" }\n",
	})

	// Zero churn: last sync is HEAD (== old at this point).
	score, behind := docengine.ComputeFreshnessScore(sb.Root, doc, old)
	if score != 100 || behind != 0 {
		t.Errorf("zero-churn freshness = (%d, %d), want (100, 0)", score, behind)
	}

	// Decay: the 60-day-old feat commit is the only commit behind
	// (range base..HEAD == {old}): 15*0.5/sqrt(61) ~= 1.0 penalty.
	scoreOld, behindOld := docengine.ComputeFreshnessScore(sb.Root, doc, base)
	if behindOld != 1 {
		t.Errorf("old-behind commits = %d, want 1", behindOld)
	}

	head := sb.GitCommitFiles("feat: add delta widget", map[string]string{
		"pkg/delta.go": "package delta\n\n// Delta handles widgets.\nfunc Delta() string { return \"delta\" }\n",
	})

	// One fresh feat commit behind: weight 15 * base 0.5 / sqrt(0+1) = 7.5.
	scoreFresh, behindFresh := docengine.ComputeFreshnessScore(sb.Root, doc, old)
	if behindFresh != 1 {
		t.Errorf("fresh-behind commits = %d, want 1", behindFresh)
	}
	if scoreFresh < 80 || scoreFresh > 99 {
		t.Errorf("fresh feat freshness = %d, want in [80, 99]", scoreFresh)
	}
	_ = head

	// Same commit with structural arch events: base 1.0 doubles the penalty.
	scoreArch, behindArch := docengine.ComputeFreshnessScoreWithArchEvents(sb.Root, doc, old, []string{"arch:split"})
	if behindArch != 1 {
		t.Errorf("arch-behind commits = %d, want 1", behindArch)
	}
	if !(scoreArch < scoreFresh) {
		t.Errorf("arch-event score %d must be below plain score %d (base 1.0 vs 0.5)", scoreArch, scoreFresh)
	}

	// The decayed (60-day-old) penalty must be smaller than the fresh one.
	if !(scoreOld > scoreFresh) {
		t.Errorf("decayed score %d must exceed fresh score %d (recency decay)", scoreOld, scoreFresh)
	}
}

// docEngQADatedCommit stages files and commits with an explicit author /
// committer date (used to engineer recency-decay scenarios deterministically).
func docEngQADatedCommit(t *testing.T, sb *harness.Sandbox, date, msg string, files map[string]string) string {
	t.Helper()
	for rel, content := range files {
		sb.WriteFile(rel, content)
	}
	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = sb.Root
		cmd.Env = append(cmd.Environ(),
			"GIT_TERMINAL_PROMPT=0",
			"GIT_AUTHOR_NAME=QA Test",
			"GIT_AUTHOR_EMAIL=qa@glassmarble.test",
			"GIT_AUTHOR_DATE="+date,
			"GIT_COMMITTER_NAME=QA Test",
			"GIT_COMMITTER_EMAIL=qa@glassmarble.test",
			"GIT_COMMITTER_DATE="+date,
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	run("add", "-A")
	run("commit", "-q", "-m", msg)
	return run("rev-parse", "HEAD")
}

// docEngQAMockIndex is a minimal verifier.AKGSymbolIndex for gate tests.
type docEngQAMockIndex struct{ known map[string]bool }

func (m *docEngQAMockIndex) HasSymbol(id string) bool { return m.known[id] }

// TestDocEngineQAGateMutation forces every quality-firewall gate to fire
// through the public RunGates API (mutation-style: each case injects exactly
// one defect class), plus a clean pass and a churn-only no-op.
func TestDocEngineQAGateMutation(t *testing.T) {
	empty := &docEngQAMockIndex{known: map[string]bool{}}

	cases := []struct {
		name string
		old  string
		new  string
		gate int // 0 = must pass
		noop bool
	}{
		{"gate1-unclosed-fence", "", "```go\nfunc Foo() {}\n", 1, false},
		{"gate2-unknown-mermaid", "", "```mermaid\nbloblogram\nA --> B\n```\n", 2, false},
		{"gate3-hallucinated-symbol", "", "Call `NoSuchSymbolZZZ` now.", 3, false},
		{"gate4-secret", "", "api_key = supersecretvalue123", 4, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := verifier.RunGates(tc.old, tc.new, empty)
			if res.Pass {
				t.Fatalf("expected gate %d to fire, but RunGates passed", tc.gate)
			}
			if res.FailedGate != tc.gate {
				t.Errorf("failed gate = %d, want %d (err: %v)", res.FailedGate, tc.gate, res.Error)
			}
			if res.Error == nil || !strings.Contains(res.Error.Error(), "doc_engine/gate") {
				t.Errorf("gate error must carry the doc_engine/gate tag, got %v", res.Error)
			}
		})
	}

	t.Run("gate5-churn-noop", func(t *testing.T) {
		same := "The system supports 10 connections."
		res := verifier.RunGates(same, same, empty)
		if !res.Pass || !res.SemanticNoOp {
			t.Errorf("identical content must pass as SemanticNoOp, got %+v", res)
		}
	})

	t.Run("clean-pass", func(t *testing.T) {
		res := verifier.RunGates(
			"The system supports 10 connections.",
			"The system supports 100 connections and handles backpressure.",
			empty,
		)
		if !res.Pass || res.SemanticNoOp {
			t.Errorf("factual change must pass as a real update, got %+v", res)
		}
	})
}

// TestDocEngineQAZeroChurn proves the second run over an unchanged commit is
// a true no-op: docs_updated == 0 via --json and byte-identical output.
func TestDocEngineQAZeroChurn(t *testing.T) {
	sb, head := docEngQARepo(t, "docs: qa zero-churn fixture")
	docEngQADocRun(t, sb, "--commit", head)
	before := sb.ReadFile("docs/guide.md")

	out, err := harness.RunGmb(t, sb, "doc", "--no-llm", "--branch-policy", "any", "--commit", head, "--json")
	if err != nil {
		t.Fatalf("second doc run failed: %v\n%s", err, out)
	}
	var payload map[string]any
	// The runner captures combined stdout+stderr; the JSON document starts
	// at the first '{'.
	start := strings.Index(out, "{")
	if start < 0 {
		t.Fatalf("no JSON object in second-run output:\n%s", out)
	}
	if err := json.Unmarshal([]byte(out[start:]), &payload); err != nil {
		t.Fatalf("second-run --json must parse: %v\n%s", err, out)
	}
	for _, key := range []string{"commit", "docs_updated", "sections_processed", "sections_updated", "tokens_used"} {
		if _, ok := payload[key]; !ok {
			t.Errorf("run JSON missing key %q: %v", key, payload)
		}
	}
	if n, _ := payload["docs_updated"].(float64); n != 0 {
		t.Errorf("docs_updated = %v on unchanged commit, want 0 (zero churn)", payload["docs_updated"])
	}
	if after := sb.ReadFile("docs/guide.md"); after != before {
		t.Errorf("target changed on no-op run:\n--- before ---\n%s\n--- after ---\n%s", before, after)
	}
}

// TestDocEngineQAPromptSnapshot pins the Track A system prompt: 13
// sequentially numbered rules with a word cap (12 without), so prompt drift
// (dropped rules, numbering gaps) is caught here, not in production.
func TestDocEngineQAPromptSnapshot(t *testing.T) {
	capped := renderer.BuildSystemPrompt(nil, 250)
	for _, n := range []string{"\n1. ", "\n2. ", "\n3. ", "\n4. ", "\n5. ", "\n6. ", "\n7. ", "\n8. ", "\n9. ", "\n10. ", "\n11. ", "\n12. ", "\n13. "} {
		if !strings.Contains(capped, n) {
			t.Errorf("capped system prompt missing rule %q:\n%s", strings.TrimSpace(n), capped)
		}
	}
	if strings.Contains(capped, "\n14. ") {
		t.Errorf("capped system prompt must have exactly 13 rules:\n%s", capped)
	}
	for _, want := range []string{"HARD RULES", "ground_truth", "Output ONLY", "Keep this section under 250 words."} {
		if !strings.Contains(capped, want) {
			t.Errorf("capped system prompt missing %q:\n%s", want, capped)
		}
	}

	uncapped := renderer.BuildSystemPrompt(nil, 0)
	for _, n := range []string{"\n1. ", "\n2. ", "\n3. ", "\n4. ", "\n5. ", "\n6. ", "\n7. ", "\n8. ", "\n9. ", "\n10. ", "\n11. ", "\n12. "} {
		if !strings.Contains(uncapped, n) {
			t.Errorf("uncapped system prompt missing rule %q:\n%s", strings.TrimSpace(n), uncapped)
		}
	}
	if strings.Contains(uncapped, "\n13. ") {
		t.Errorf("uncapped system prompt must have exactly 12 rules (no gap 12→13):\n%s", uncapped)
	}
}

// TestDocEngineQAJSONSchemaStability unmarshals every machine-readable docs
// surface into generic maps and asserts the key contract: docs_state.json,
// `doc --json`, and `doc check --json`. A renamed/removed key breaks this
// test by design (interchange-tooling compatibility gate).
func TestDocEngineQAJSONSchemaStability(t *testing.T) {
	t.Setenv("GMB_DOC_STATE", "json")
	sb, head := docEngQARepo(t, "docs: qa schema fixture")
	docEngQADocRun(t, sb, "--commit", head)

	// 1. docs_state.json top-level + per-document keys.
	var state map[string]any
	if err := json.Unmarshal([]byte(sb.ReadFile(".glassmarble/docs_state.json")), &state); err != nil {
		t.Fatalf("docs_state.json must be valid JSON: %v", err)
	}
	for _, key := range []string{"schema_version", "last_commit", "generated_at", "documents"} {
		if _, ok := state[key]; !ok {
			t.Errorf("docs_state.json missing key %q: %v", key, state)
		}
	}
	docs, ok := state["documents"].(map[string]any)
	if !ok {
		t.Fatalf("docs_state.json documents is not an object: %T", state["documents"])
	}
	entry, ok := docs["docs/guide.md"].(map[string]any)
	if !ok {
		t.Fatalf("docs_state.json missing docs/guide.md entry: %v", docs)
	}
	for _, key := range []string{"freshness_score", "sections"} {
		if _, ok := entry[key]; !ok {
			t.Errorf("document state missing key %q: %v", key, entry)
		}
	}

	// 2. `doc --json` run payload keys.
	out, err := harness.RunGmb(t, sb, "doc", "--no-llm", "--branch-policy", "any", "--commit", head, "--json")
	if err != nil {
		t.Fatalf("doc --json failed: %v\n%s", err, out)
	}
	var runPayload map[string]any
	if err := json.Unmarshal([]byte(out[strings.Index(out, "{"):]), &runPayload); err != nil {
		t.Fatalf("doc --json must parse: %v\n%s", err, out)
	}
	for _, key := range []string{"commit", "docs_updated", "sections_processed", "sections_updated", "tokens_used", "duration_ms"} {
		if _, ok := runPayload[key]; !ok {
			t.Errorf("doc --json missing key %q: %v", key, runPayload)
		}
	}

	// 3. `doc check --json` audit keys (Go field names at top level, tagged
	// names for per-document entries).
	out, err = harness.RunGmb(t, sb, "doc", "check", "--json")
	if err != nil {
		t.Fatalf("doc check --json failed: %v\n%s", err, out)
	}
	var checkPayload map[string]any
	if err := json.Unmarshal([]byte(out[strings.Index(out, "{"):]), &checkPayload); err != nil {
		t.Fatalf("doc check --json must parse: %v\n%s", err, out)
	}
	for _, key := range []string{"AllFresh", "Documents", "GlobalFreshness"} {
		if _, ok := checkPayload[key]; !ok {
			t.Errorf("doc check --json missing key %q: %v", key, checkPayload)
		}
	}
	entries, ok := checkPayload["Documents"].([]any)
	if !ok || len(entries) == 0 {
		t.Fatalf("doc check --json Documents must be a non-empty array: %v", checkPayload["Documents"])
	}
	first, ok := entries[0].(map[string]any)
	if !ok {
		t.Fatalf("check document entry is not an object: %T", entries[0])
	}
	for _, key := range []string{"id", "target_path", "freshness", "status"} {
		if _, ok := first[key]; !ok {
			t.Errorf("check document entry missing key %q: %v", key, first)
		}
	}
}
