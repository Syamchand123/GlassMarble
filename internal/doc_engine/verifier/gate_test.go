package verifier

import (
	"testing"
)

// ────────────────────────────────────────────────────────────────────────────
// Gate 1: Markdown syntax
// ────────────────────────────────────────────────────────────────────────────

func TestGate1_ValidMarkdown(t *testing.T) {
	content := "# Title\n\n```go\nfunc Foo() {}\n```\n\n| A | B |\n|---|---|\n| 1 | 2 |\n"
	if err := checkMarkdownSyntax(content); err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
}

func TestGate1_UnclosedFence(t *testing.T) {
	content := "```go\nfunc Foo() {}\n"
	err := checkMarkdownSyntax(content)
	if err == nil {
		t.Error("expected error for unclosed fence")
	}
}

func TestGate1_MalformedTable(t *testing.T) {
	content := "| A\n|---|\n| 1 |\n"
	// Only 1 pipe in first row → should fail.
	err := checkMarkdownSyntax(content)
	if err == nil {
		t.Error("expected error for malformed table row")
	}
}

// ────────────────────────────────────────────────────────────────────────────
// Gate 2: Diagram syntax
// ────────────────────────────────────────────────────────────────────────────

func TestGate2_ValidMermaid(t *testing.T) {
	content := "```mermaid\ngraph TD\n  A --> B\n```\n"
	if err := checkDiagramSyntax(content); err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
}

func TestGate2_UnknownMermaidType(t *testing.T) {
	content := "```mermaid\nbloblogram\n  A --> B\n```\n"
	err := checkDiagramSyntax(content)
	if err == nil {
		t.Error("expected error for unknown mermaid type")
	}
}

func TestGate2_EmptyMermaidBlock(t *testing.T) {
	content := "```mermaid\n```\n"
	err := checkDiagramSyntax(content)
	if err == nil {
		t.Error("expected error for empty mermaid block")
	}
}

func TestGate2_ValidPlantUML(t *testing.T) {
	content := "```plantuml\n@startuml\nA -> B : hello\n@enduml\n```\n"
	if err := checkDiagramSyntax(content); err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
}

func TestGate2_PlantUMLMissingEnd(t *testing.T) {
	content := "```plantuml\n@startuml\nA -> B\n```\n"
	err := checkDiagramSyntax(content)
	if err == nil {
		t.Error("expected error for plantuml missing @enduml")
	}
}

// ────────────────────────────────────────────────────────────────────────────
// Gate 3: Symbol linter
// ────────────────────────────────────────────────────────────────────────────

type mockAKG struct {
	known map[string]bool
}

func (m *mockAKG) HasSymbol(id string) bool {
	return m.known[id]
}

func TestGate3_KnownSymbol(t *testing.T) {
	content := "Use `MyFunc` to do the thing."
	akg := &mockAKG{known: map[string]bool{"MyFunc": true}}
	if err := checkSymbols(content, akg); err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
}

func TestGate3_UnknownSymbol(t *testing.T) {
	content := "Use `HallucinatedFunc` to do the thing."
	akg := &mockAKG{known: map[string]bool{}}
	err := checkSymbols(content, akg)
	if err == nil {
		t.Error("expected error for hallucinated symbol")
	}
}

func TestGate3_StdlibAllowlist(t *testing.T) {
	// context.Context is in the stdlib allowlist — should not require AKG lookup.
	content := "Pass a `context.Context` to every call."
	akg := &mockAKG{known: map[string]bool{}}
	if err := checkSymbols(content, akg); err != nil {
		t.Errorf("stdlib symbol incorrectly rejected: %v", err)
	}
}

// ────────────────────────────────────────────────────────────────────────────
// Gate 4: Secret check
// ────────────────────────────────────────────────────────────────────────────

func TestGate4_CleanContent(t *testing.T) {
	content := "The function returns an error on invalid input."
	if err := checkSecrets(content); err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
}

func TestGate4_AWSKey(t *testing.T) {
	content := "Access key: AKIAIOSFODNN7EXAMPLE."
	err := checkSecrets(content)
	if err == nil {
		t.Error("expected error for AWS access key pattern")
	}
}

func TestGate4_GitHubToken(t *testing.T) {
	content := "Token: ghp_abcdefghijklmnopqrstuvwxyzABCDEFGHIJ."
	err := checkSecrets(content)
	if err == nil {
		t.Error("expected error for GitHub token pattern")
	}
}

func TestGate4_APIKeyAssignment(t *testing.T) {
	content := "api_key = supersecretvalue123"
	err := checkSecrets(content)
	if err == nil {
		t.Error("expected error for api_key assignment")
	}
}

// ────────────────────────────────────────────────────────────────────────────
// Gate 5: Semantic diff compressor
// ────────────────────────────────────────────────────────────────────────────

func TestGate5_IdenticalContent(t *testing.T) {
	c := "The system initializes the connection pool."
	if !isSemanticNoOp(c, c) {
		t.Error("identical content should be no-op")
	}
}

func TestGate5_WhitespaceOnlyChange(t *testing.T) {
	old := "The system   initializes."
	new := "The system initializes."
	if !isSemanticNoOp(old, new) {
		t.Error("whitespace-only change should be no-op")
	}
}

func TestGate5_SynonymSubstitution(t *testing.T) {
	old := "The function returns an error."
	new := "The function return an error."
	if !isSemanticNoOp(old, new) {
		t.Error("synonym swap should be no-op")
	}
}

func TestGate5_FactualChange(t *testing.T) {
	old := "The system supports 10 connections."
	new := "The system supports 100 connections and handles backpressure."
	if isSemanticNoOp(old, new) {
		t.Error("factual change should NOT be a no-op")
	}
}

// ────────────────────────────────────────────────────────────────────────────
// RunGates integration
// ────────────────────────────────────────────────────────────────────────────

func TestRunGates_AllPass(t *testing.T) {
	old := "Old content."
	new := "New content with fresh facts added here."
	akg := &mockAKG{known: map[string]bool{}}
	result := RunGates(old, new, akg)
	if !result.Pass {
		t.Errorf("expected pass, gate %d failed: %v", result.FailedGate, result.Error)
	}
}

func TestRunGates_Gate4HardFail(t *testing.T) {
	old := ""
	new := "My aws key is AKIAIOSFODNN7EXAMPLE."
	akg := &mockAKG{known: map[string]bool{}}
	result := RunGates(old, new, akg)
	if result.Pass {
		t.Error("expected Gate 4 to fail on AWS key in output")
	}
	if result.FailedGate != 4 {
		t.Errorf("expected gate 4, got gate %d", result.FailedGate)
	}
}

func TestRunGates_SemanticNoOp(t *testing.T) {
	c := "The system utilizes the cache."
	akg := &mockAKG{known: map[string]bool{}}
	result := RunGates(c, c, akg) // old == new
	if !result.Pass {
		t.Errorf("expected pass, got gate %d: %v", result.FailedGate, result.Error)
	}
	if !result.SemanticNoOp {
		t.Error("expected SemanticNoOp for identical content")
	}
}

func TestRunGates_Gate1UnclosedFence(t *testing.T) {
	old := ""
	new := "```go\nfunc Foo() {}\n"
	akg := &mockAKG{known: map[string]bool{}}
	result := RunGates(old, new, akg)
	if result.Pass {
		t.Error("expected Gate 1 to fail on unclosed fence")
	}
	if result.FailedGate != 1 {
		t.Errorf("expected gate 1, got gate %d", result.FailedGate)
	}
}
