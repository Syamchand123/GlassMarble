package ont

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoLiteralVocabularyOutsideOnt enforces the W0-08 invariant: no "gm:" or
// "ext:" string literal may appear in production (non-_test) Go source outside
// this package. Test files are exempt by design: they pin the exact TTL format
// so a serializer regression is caught as a literal diff. Raw TTL fixtures
// under testdata/ live in test files only.
func TestNoLiteralVocabularyOutsideOnt(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}

	var violations []string
	err = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			name := info.Name()
			if name == "ont" && filepath.Dir(path) == filepath.Join(root, "internal", "product") {
				return filepath.SkipDir
			}
			if name == ".git" || name == "node_modules" || name == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Errorf("parse %s: %v", path, err)
			return nil
		}
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			s := lit.Value
			if strings.Contains(s, `"gm:`) || strings.Contains(s, `"ext:`) {
				rel, _ := filepath.Rel(root, path)
				violations = append(violations, rel+":"+fset.Position(lit.Pos()).String())
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) > 0 {
		t.Errorf("%d production file(s) contain literal \"gm:\" or \"ext:\" vocabulary (use ont constants):\n%s",
			len(violations), strings.Join(violations, "\n"))
	}
}
