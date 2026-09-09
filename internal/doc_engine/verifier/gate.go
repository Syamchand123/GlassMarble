// Package verifier implements Stage 7 of the Documentation Intelligence Engine:
// the Quality Firewall — 5 ordered gates that every generated section must pass
// before it reaches the atomic writer.
//
// Gate 1: Markdown AST syntax check (unclosed backticks, broken tables)
// Gate 2: Mermaid/PlantUML render check (diagram syntax validation)
// Gate 3: AKG Symbol Linter (backtick identifiers must exist in AKG or stdlib)
// Gate 4: Secret & PII scrub on output (hard fail, no retry)
// Gate 5: Semantic diff compression (discard churn-only changes)
package verifier

import (
	"strings"
)

// GateError is a structured error from the Quality Firewall.
type GateError struct {
	Gate    int
	Message string
}

func (e *GateError) Error() string {
	return strings.Join([]string{"doc_engine/gate", gateNames[e.Gate], e.Message}, ": ")
}

var gateNames = map[int]string{
	1: "markdown-syntax",
	2: "diagram-render",
	3: "symbol-linter",
	4: "secret-scan",
	5: "semantic-diff",
}

// GateResult is the verdict from running all 5 gates.
type GateResult struct {
	Pass       bool
	FailedGate int    // 0 if all pass
	Error      *GateError
	// SemanticNoOp is true when Gate 5 determines the new content is churn-only.
	// Callers should discard THEIRS and keep the existing content unchanged.
	SemanticNoOp bool
}

// AKGSymbolIndex is the minimal interface gate 3 needs from the AKG.
// Fulfilled by the grounding package's collector at runtime.
type AKGSymbolIndex interface {
	// HasSymbol returns true if the identifier is a known symbol in the AKG
	// or in the stdlib allowlist.
	HasSymbol(identifier string) bool
}

// RunGates executes all 5 gates in order for a generated section.
//
// Parameters:
//   - oldContent: current content of the managed zone (before replacement)
//   - newContent: freshly generated content from the renderer
//   - akg: AKG symbol index for Gate 3 (may be nil → Gate 3 skipped)
//
// ponytail: gates are pure stdlib — no external parser dependency.
func RunGates(oldContent, newContent string, akg AKGSymbolIndex) GateResult {
	if err := checkMarkdownSyntax(newContent); err != nil {
		return GateResult{FailedGate: 1, Error: err}
	}
	if err := checkDiagramSyntax(newContent); err != nil {
		return GateResult{FailedGate: 2, Error: err}
	}
	if akg != nil {
		if err := checkSymbols(newContent, akg); err != nil {
			return GateResult{FailedGate: 3, Error: err}
		}
	}
	if err := checkSecrets(newContent); err != nil {
		return GateResult{FailedGate: 4, Error: err}
	}
	noOp := isSemanticNoOp(oldContent, newContent)
	return GateResult{Pass: true, SemanticNoOp: noOp}
}
