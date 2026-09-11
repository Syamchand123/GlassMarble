package verifier

import (
	"strings"
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/doc_engine/config"
)

// stubAKG is a minimal AKGSymbolIndex for mutation tests (distinct from
// gate_test.go's mockAKG to avoid redeclaration).
type stubAKG struct {
	known map[string]bool
}

func (s *stubAKG) HasSymbol(id string) bool {
	return s.known[id]
}

func TestMutationHallucinatedIdentifierFailsGate3(t *testing.T) {
	akg := &stubAKG{known: map[string]bool{}}
	newContent := "Use `HallucinatedFuncXYZ` to do the thing."
	result := RunGates("", newContent, akg)
	if result.Pass {
		t.Fatal("expected Gate 3 to fail on hallucinated backtick identifier")
	}
	if result.FailedGate != 3 {
		t.Fatalf("expected gate 3, got gate %d: %+v", result.FailedGate, result.Error)
	}
}

func TestMutationSecretFailsGate4(t *testing.T) {
	akg := &stubAKG{known: map[string]bool{}}
	newContent := "Access key: AKIAIOSFODNN7EXAMPLE."
	result := RunGates("", newContent, akg)
	if result.Pass {
		t.Fatal("expected Gate 4 to fail on AWS key")
	}
	if result.FailedGate != 4 {
		t.Fatalf("expected gate 4, got gate %d: %+v", result.FailedGate, result.Error)
	}
}

func TestMutationParaphraseIsSemanticNoOp(t *testing.T) {
	// "returns"/"return" is a synonym pair in diff_compressor.go.
	old := "The function returns an error."
	new := "The function return an error."
	akg := &stubAKG{known: map[string]bool{}}
	result := RunGates(old, new, akg)
	if !result.Pass {
		t.Fatalf("expected pass, gate %d failed: %v", result.FailedGate, result.Error)
	}
	if !result.SemanticNoOp {
		t.Error("expected SemanticNoOp true for paraphrase-only rewrite")
	}
}

func TestMutationBrokenMermaidFailsGate2(t *testing.T) {
	akg := &stubAKG{known: map[string]bool{}}
	newContent := "```mermaid\nbloblogram\n  A --> B\n```\n"
	result := RunGates("", newContent, akg)
	if result.Pass {
		t.Fatal("expected Gate 2 to fail on broken mermaid")
	}
	if result.FailedGate != 2 {
		t.Fatalf("expected gate 2, got gate %d: %+v", result.FailedGate, result.Error)
	}
}

func TestMutationUnclosedFenceFailsGate1(t *testing.T) {
	akg := &stubAKG{known: map[string]bool{}}
	newContent := "```go\nfunc Foo() {}\n"
	result := RunGates("", newContent, akg)
	if result.Pass {
		t.Fatal("expected Gate 1 to fail on unclosed fence")
	}
	if result.FailedGate != 1 {
		t.Fatalf("expected gate 1, got gate %d: %+v", result.FailedGate, result.Error)
	}
}

func TestMutationProsePassiveJargonStrictness(t *testing.T) {
	style := config.StyleSpec{JargonBlacklist: []string{"leverage"}}
	content := "The task was executed. We leverage the cache."

	pass, failures := CheckProseGate(content, style, false)
	if !pass {
		t.Errorf("non-strict must pass, got failures %v", failures)
	}
	if len(failures) == 0 {
		t.Error("non-strict must still report failures")
	}

	passStrict, failuresStrict := CheckProseGate(content, style, true)
	if passStrict {
		t.Errorf("strict must fail on jargon error, got failures %v", failuresStrict)
	}
	if len(failuresStrict) == 0 {
		t.Error("strict failure must return failure strings")
	}
}

func TestMutationProseTypoAndDoubledWord(t *testing.T) {
	pass, failures := CheckProseGate("Fix teh bug and the the docs.", config.StyleSpec{}, false)
	if !pass {
		t.Errorf("non-strict must pass even with errors, got %v", failures)
	}
	joined := strings.Join(failures, "\n")
	if !strings.Contains(joined, "typo") {
		t.Errorf("expected typo report, got %v", failures)
	}
	if !strings.Contains(joined, "doubled-word") {
		t.Errorf("expected doubled-word report, got %v", failures)
	}
}
