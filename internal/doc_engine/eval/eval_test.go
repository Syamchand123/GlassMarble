package eval

import (
	"strings"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/doc_engine/config"
)

func testFactSheet() *config.FactSheet {
	return &config.FactSheet{
		DocID:              "worker",
		SectionID:          "lifecycle",
		DocPurpose:         "Document the background worker pool for operators.",
		DocAudience:        "service operators",
		SectionInstruction: "Describe the worker pool lifecycle and sizing.",
		GroundTruth: config.GroundTruthPayload{
			Symbols: []config.SymbolFact{
				{
					FQN:       "pkg/worker.Pool",
					Kind:      "struct",
					Signature: "func (p *Pool) Run(ctx context.Context) error",
					Doc:       "Pool manages a set of background workers.",
					File:      "pkg/worker/pool.go",
				},
			},
			ConfigVars: []config.ConfigVarFact{
				{Name: "WORKER_COUNT", Source: "os.Getenv", File: "pkg/worker/pool.go"},
			},
		},
	}
}

const faithfulProse = "The `Pool` manages background workers. " +
	"Set `WORKER_COUNT` to size the worker pool. " +
	"The `Run` method starts the pool lifecycle."

func TestFaithfulProseScoresOne(t *testing.T) {
	rep := ScoreSection(faithfulProse, testFactSheet())
	if rep.TotalClaims != 3 {
		t.Fatalf("TotalClaims = %d, want 3", rep.TotalClaims)
	}
	if rep.SupportedClaims != 3 {
		t.Fatalf("SupportedClaims = %d, want 3", rep.SupportedClaims)
	}
	if rep.Score != 1.0 {
		t.Fatalf("Score = %v, want 1.0", rep.Score)
	}
	if len(rep.Unsupported) != 0 {
		t.Fatalf("Unsupported = %v, want empty", rep.Unsupported)
	}
}

func TestInventedBacktickedSymbolUnsupported(t *testing.T) {
	rep := ScoreSection("The `QuantumFluxCapacitor` drives the pool.", testFactSheet())
	if rep.TotalClaims != 1 {
		t.Fatalf("TotalClaims = %d, want 1", rep.TotalClaims)
	}
	if rep.Score != 0.0 {
		t.Fatalf("Score = %v, want 0.0", rep.Score)
	}
	if len(rep.Unsupported) != 1 || !strings.Contains(rep.Unsupported[0], "QuantumFluxCapacitor") {
		t.Fatalf("Unsupported = %v, want the invented-symbol claim", rep.Unsupported)
	}
}

func TestInventedNonCodeClaimUnsupported(t *testing.T) {
	rep := ScoreSection("It retries with exponential backoff.", testFactSheet())
	if rep.TotalClaims != 1 {
		t.Fatalf("TotalClaims = %d, want 1", rep.TotalClaims)
	}
	if rep.SupportedClaims != 0 {
		t.Fatalf("SupportedClaims = %d, want 0", rep.SupportedClaims)
	}
	if rep.Score != 0.0 {
		t.Fatalf("Score = %v, want 0.0", rep.Score)
	}
	if len(rep.Unsupported) != 1 {
		t.Fatalf("Unsupported = %v, want 1 claim", rep.Unsupported)
	}
}

func TestEmptyProseScoresOne(t *testing.T) {
	for _, prose := range []string{"", "   ", "\n\t\n"} {
		rep := ScoreSection(prose, testFactSheet())
		if rep.TotalClaims != 0 {
			t.Fatalf("TotalClaims = %d, want 0 for %q", rep.TotalClaims, prose)
		}
		if rep.Score != 1.0 {
			t.Fatalf("Score = %v, want 1.0 for %q", rep.Score, prose)
		}
	}
}

type stubJudge struct {
	verdict     bool
	calls       int
	lastClaim   string
	lastContext string
}

func (s *stubJudge) Supported(claim, context string) bool {
	s.calls++
	s.lastClaim = claim
	s.lastContext = context
	return s.verdict
}

func TestJudgeTrueHonored(t *testing.T) {
	j := &stubJudge{verdict: true}
	rep := ScoreSectionWithJudge("It retries with exponential backoff.", testFactSheet(), j)
	if rep.Score != 1.0 {
		t.Fatalf("Score = %v, want 1.0", rep.Score)
	}
	if j.calls != 1 {
		t.Fatalf("judge calls = %d, want 1", j.calls)
	}
	if !strings.Contains(j.lastContext, "worker") {
		t.Fatalf("judge context missing ground truth: %q", j.lastContext)
	}
}

func TestJudgeFalseHonored(t *testing.T) {
	j := &stubJudge{verdict: false}
	rep := ScoreSectionWithJudge("It retries with exponential backoff.", testFactSheet(), j)
	if rep.Score != 0.0 {
		t.Fatalf("Score = %v, want 0.0", rep.Score)
	}
	if len(rep.Unsupported) != 1 {
		t.Fatalf("Unsupported = %v, want 1 claim", rep.Unsupported)
	}
}

func TestJudgeNotConsultedOnDeterministicPass(t *testing.T) {
	j := &stubJudge{verdict: false}
	rep := ScoreSectionWithJudge(faithfulProse, testFactSheet(), j)
	if rep.Score != 1.0 {
		t.Fatalf("Score = %v, want 1.0", rep.Score)
	}
	if j.calls != 0 {
		t.Fatalf("judge calls = %d, want 0 (deterministic pass)", j.calls)
	}
}

func TestNilJudgeMatchesScoreSection(t *testing.T) {
	prose := faithfulProse + " It retries with exponential backoff."
	a := ScoreSection(prose, testFactSheet())
	b := ScoreSectionWithJudge(prose, testFactSheet(), nil)
	if a.Score != b.Score || a.TotalClaims != b.TotalClaims ||
		a.SupportedClaims != b.SupportedClaims || len(a.Unsupported) != len(b.Unsupported) {
		t.Fatalf("nil-judge mismatch: %+v vs %+v", a, b)
	}
}

func TestCRLFInput(t *testing.T) {
	crlf := strings.ReplaceAll(faithfulProse, ". ", ".\r\n")
	lf := ScoreSection(faithfulProse, testFactSheet())
	rep := ScoreSection(crlf, testFactSheet())
	if rep.TotalClaims != lf.TotalClaims || rep.Score != lf.Score {
		t.Fatalf("CRLF rep = %+v, want %+v", rep, lf)
	}
	if rep.Score != 1.0 {
		t.Fatalf("CRLF Score = %v, want 1.0", rep.Score)
	}
}

func TestHeadingsAreClaimsAndFencesSkipped(t *testing.T) {
	prose := "# Worker Pool\n\nThe `Pool` manages background workers.\n" +
		"```go\nBogusUnrealSymbolHere foo bar\n```\n"
	rep := ScoreSection(prose, testFactSheet())
	if rep.TotalClaims != 2 {
		t.Fatalf("TotalClaims = %d (%v), want 2 (heading + sentence)", rep.TotalClaims, rep.Unsupported)
	}
	if rep.Score != 1.0 {
		t.Fatalf("Score = %v, want 1.0", rep.Score)
	}
}

func TestInstructionSatisfied(t *testing.T) {
	prose := "The worker pool manages background workers for operators."
	if !InstructionSatisfied(prose, "Describe the worker pool lifecycle for operators.") {
		t.Fatal("want satisfied (>=2 shared tokens: worker, pool, operators)")
	}
	if !InstructionSatisfied(prose, "") {
		t.Fatal("want satisfied for empty instruction")
	}
	if !InstructionSatisfied(prose, "   ") {
		t.Fatal("want satisfied for blank instruction")
	}
	if InstructionSatisfied(prose, "Explain quantum tunneling in semiconductors.") {
		t.Fatal("want unsatisfied for unrelated instruction")
	}
	if InstructionSatisfied("The pool is great.", "Describe the pool configuration.") {
		t.Fatal("want unsatisfied for exactly 1 shared token")
	}
	if InstructionSatisfied("", "Describe the worker pool lifecycle.") {
		t.Fatal("want unsatisfied for empty prose")
	}
}

func TestNilFactSheet(t *testing.T) {
	rep := ScoreSection(faithfulProse, nil)
	if rep.TotalClaims == 0 {
		t.Fatal("want claims even with nil FactSheet")
	}
	if rep.Score != 0.0 {
		t.Fatalf("Score = %v, want 0.0 with empty ground truth", rep.Score)
	}
	if rep := ScoreSection("", nil); rep.Score != 1.0 {
		t.Fatalf("empty prose Score = %v, want 1.0", rep.Score)
	}
}
