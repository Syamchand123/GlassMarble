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
	"io/fs"
	"path/filepath"
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

// ────────────────────────────────────────────────────────────────────────────
// Arity verification (Pillar 14 extension)
//
// VerifySnippetsInRepo behaves like VerifyCodeSnippets but additionally checks
// call arity: it builds a map of local func declarations (name → param count)
// by parsing repo Go files, then compares each snippet call expression's
// argument count against the declaration. On mismatch the error's
// SuggestedFix carries the correct signature string so ApplySnippetFixes (or a
// human) can repair the call site.
// ────────────────────────────────────────────────────────────────────────────

// funcArity records a declaration's parameter count and printable signature.
type funcArity struct {
	Params    int
	Signature string
}

// VerifySnippetsInRepo verifies snippet syntax/symbols (via VerifyCodeSnippets)
// plus call arity against real declarations found under repoRoot.
func VerifySnippetsInRepo(repoRoot, markdown string, knownSymbols map[string]bool) ([]SnippetError, error) {
	base, err := VerifyCodeSnippets(markdown, knownSymbols)
	if err != nil {
		return base, err
	}

	arity, err := buildDeclArityMap(repoRoot)
	if err != nil || len(arity) == 0 {
		return base, err
	}

	matches := snippetTagRe.FindAllStringSubmatchIndex(markdown, -1)
	for _, m := range matches {
		if len(m) < 4 {
			continue
		}
		snippetCode := markdown[m[2]:m[3]]
		byteOffset := m[2]
		lineNum := strings.Count(markdown[:byteOffset], "\n") + 1

		wrapped := snippetCode
		if !strings.Contains(snippetCode, "package ") {
			wrapped = fmt.Sprintf("package snippet\nfunc _() {\n%s\n}\n", snippetCode)
		}
		fset := token.NewFileSet()
		node, parseErr := parser.ParseFile(fset, "snippet.go", wrapped, 0)
		if parseErr != nil {
			continue // syntax error already reported by VerifyCodeSnippets
		}

		ast.Inspect(node, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			name := callFuncName(call.Fun)
			if name == "" || isStdlibSymbol(name) {
				return true
			}
			decl, found := arity[name]
			if !found {
				return true
			}
			if len(call.Args) == decl.Params {
				return true
			}
			pos := fset.Position(call.Pos())
			base = append(base, SnippetError{
				LineNumber:   lineNum + pos.Line - 3,
				SnippetLine:  pos.Line - 2,
				Symbol:       name,
				ErrorMessage: fmt.Sprintf("Snippet calls %q with %d argument(s) but declaration takes %d", name, len(call.Args), decl.Params),
				SuggestedFix: decl.Signature,
			})
			return true
		})
	}

	return base, nil
}

// ApplySnippetFixes rewrites the offending call line of each error that
// carries a SuggestedFix. Replacement is line-based using LineNumber (1-based
// into the markdown text); leading indentation of the original line is
// preserved. Errors without a SuggestedFix or with an out-of-range
// LineNumber are skipped. The old snippet verifier is untouched.
func ApplySnippetFixes(markdown string, errs []SnippetError) string {
	lines := strings.Split(markdown, "\n")
	for _, e := range errs {
		if e.SuggestedFix == "" {
			continue
		}
		idx := e.LineNumber - 1
		if idx < 0 || idx >= len(lines) {
			continue
		}
		orig := lines[idx]
		trimmed := strings.TrimLeft(orig, " \t")
		indent := orig[:len(orig)-len(trimmed)]
		lines[idx] = indent + e.SuggestedFix
	}
	return strings.Join(lines, "\n")
}

// buildDeclArityMap parses repo Go files and maps func name → arity.
// Methods are indexed under both "Recv.Name" and bare "Name" so snippet
// call sites (pkg.Fn, obj.Method, or bare Fn) all resolve.
func buildDeclArityMap(repoRoot string) (map[string]funcArity, error) {
	result := make(map[string]funcArity)
	fset := token.NewFileSet()

	walkErr := filepath.WalkDir(repoRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == ".glassmarble" || name == "vendor" || name == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		node, parseErr := parser.ParseFile(fset, path, nil, 0)
		if parseErr != nil {
			return nil
		}
		for _, decl := range node.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Name == nil {
				continue
			}
			params := 0
			var parts []string
			if fn.Type != nil && fn.Type.Params != nil {
				for _, field := range fn.Type.Params.List {
					names := len(field.Names)
					if names == 0 {
						names = 1
					}
					params += names
					typ := exprString(field.Type)
					if len(field.Names) == 0 {
						parts = append(parts, typ)
					} else {
						for _, nm := range field.Names {
							if typ != "" {
								parts = append(parts, nm.Name+" "+typ)
							} else {
								parts = append(parts, nm.Name)
							}
						}
					}
				}
			}
			sig := fmt.Sprintf("%s(%s)", fn.Name.Name, strings.Join(parts, ", "))
			result[fn.Name.Name] = funcArity{Params: params, Signature: sig}
			if fn.Recv != nil && len(fn.Recv.List) > 0 {
				recvType := exprString(fn.Recv.List[0].Type)
				recvType = strings.TrimPrefix(recvType, "*")
				recvType = strings.TrimPrefix(recvType, "[]")
				if recvType != "" {
					result[recvType+"."+fn.Name.Name] = funcArity{Params: params, Signature: sig}
				}
			}
		}
		return nil
	})

	return result, walkErr
}

// callFuncName extracts the called function name from a call expression:
// selector calls (pkg.Fn, obj.Method) yield Sel.Name, plain calls yield the ident.
func callFuncName(fun ast.Expr) string {
	switch t := fun.(type) {
	case *ast.SelectorExpr:
		if t.Sel != nil {
			return t.Sel.Name
		}
	case *ast.Ident:
		return t.Name
	}
	return ""
}

// exprString renders a small type expression to a readable string.
func exprString(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.SelectorExpr:
		return exprString(t.X) + "." + t.Sel.Name
	case *ast.StarExpr:
		return "*" + exprString(t.X)
	case *ast.ArrayType:
		return "[]" + exprString(t.Elt)
	case *ast.Ellipsis:
		return "..." + exprString(t.Elt)
	case *ast.MapType:
		return "map[" + exprString(t.Key) + "]" + exprString(t.Value)
	case *ast.FuncType:
		return "func(...)"
	case *ast.InterfaceType:
		return "interface{}"
	default:
		return ""
	}
}
