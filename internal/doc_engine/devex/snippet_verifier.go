// Package devex implements Phase 6 Developer Experience & RAG pillars:
// - Pillar 14: Executable Code Snippets & Snippet Verification
// - Pillar 20: Doc-Code Feedback Loop via gmb ai Gap Discovery
// - Pillar 24: Migration Guide Generator
// - Pillar 37: RAG & AI-Agent Ready Knowledge Base
package devex

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"strings"
)

// SnippetError represents an obsolete or invalid symbol inside an executable snippet.
type SnippetError struct {
	LineNumber    int    `json:"line_number"`
	SnippetLine   int    `json:"snippet_line"`
	Symbol        string `json:"symbol"`
	ErrorMessage  string `json:"error_message"`
	SuggestedFix  string `json:"suggested_fix,omitempty"`
}

var snippetTagRe = regexp.MustCompile(`<!--\s*gmb:snippet:example\s*-->\s*` + "```(?:go)?\\n([\\s\\S]*?)```")

// VerifyCodeSnippets scans markdown content for executable snippet blocks, parses them,
// and validates their syntactic validity and symbol contracts against known symbols.
func VerifyCodeSnippets(markdown string, knownSymbols map[string]bool) ([]SnippetError, error) {
	var errs []SnippetError

	matches := snippetTagRe.FindAllStringSubmatchIndex(markdown, -1)
	for _, m := range matches {
		if len(m) < 4 {
			continue
		}
		snippetCode := markdown[m[2]:m[3]]
		byteOffset := m[2]
		lineNum := strings.Count(markdown[:byteOffset], "\n") + 1

		// Verify snippet syntax
		fset := token.NewFileSet()
		wrapped := snippetCode
		if !strings.Contains(snippetCode, "package ") {
			wrapped = fmt.Sprintf("package snippet\nfunc _() {\n%s\n}\n", snippetCode)
		}
		node, err := parser.ParseFile(fset, "snippet.go", wrapped, 0)
		if err != nil {
			errs = append(errs, SnippetError{
				LineNumber:   lineNum,
				ErrorMessage: fmt.Sprintf("Syntax error in executable code snippet: %v", err),
			})
			continue
		}

		// Verify referenced symbols if knownSymbols map is provided
		if len(knownSymbols) > 0 {
			ast.Inspect(node, func(n ast.Node) bool {
				sel, ok := n.(*ast.SelectorExpr)
				if !ok {
					return true
				}

				symName := sel.Sel.Name
				if ast.IsExported(symName) && !knownSymbols[symName] {
					// Disregard standard library or local packages
					if isStdlibSymbol(symName) {
						return true
					}
					pos := fset.Position(sel.Pos())
					errs = append(errs, SnippetError{
						LineNumber:   lineNum + pos.Line - 3,
						SnippetLine:  pos.Line - 2,
						Symbol:       symName,
						ErrorMessage: fmt.Sprintf("Snippet references missing or outdated symbol %q", symName),
						SuggestedFix: "Verify symbol existence in codebase or update snippet to current API.",
					})
				}
				return true
			})
		}
	}

	return errs, nil
}

func isStdlibSymbol(s string) bool {
	switch s {
	case "Println", "Printf", "Sprintf", "Errorf", "New", "Open", "Close", "Read", "Write",
		"Join", "Split", "Contains", "TrimSpace", "Now", "Date", "Duration", "Second", "Minute":
		return true
	default:
		return false
	}
}
