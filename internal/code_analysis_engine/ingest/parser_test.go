package ingest

import (
	"path/filepath"
	"strings"
	"testing"
)

func specFor(t *testing.T, lang SupportedLang) *LanguageSpec {
	t.Helper()
	spec := lookupSpec(lang, Registry())
	if spec == nil {
		t.Fatalf("no spec registered for %s", lang)
	}
	return spec
}

// tokenContains reports whether any token of the given kind has Content (or
// Name) containing the needle substring.
func tokenContains(tokens []RichToken, kind TokenKind, needle string) bool {
	for _, tok := range tokens {
		if tok.Kind != kind {
			continue
		}
		if strings.Contains(tok.Content, needle) || strings.Contains(tok.Name, needle) {
			return true
		}
	}
	return false
}

func TestProcessFileGo(t *testing.T) {
	src := filepath.Join(t.TempDir(), "main.go")
	writeTestFile(t, src, `package main

import "fmt"

func greet(name string) string {
	return "hi " + name
}

func main() {
	greet("world")
	fmt.Println("done")
}
`)

	spec := specFor(t, LangGo)
	task := FileTask{
		FilePath: src,
		RelPath:  "main.go",
		Language: LangGo,
		Change:   ChangeModified,
	}

	p := newParser()
	defer p.Close()

	res := processFile(p, task, spec)
	if res == nil {
		t.Fatal("processFile returned nil result")
	}
	if res.Error != nil {
		t.Fatalf("processFile error: %v", res.Error)
	}
	if res.HasErrors {
		t.Errorf("HasErrors = true for valid Go source")
	}
	if len(res.RichTokens) == 0 {
		t.Fatal("RichTokens empty, want > 0")
	}

	if !tokenContains(res.RichTokens, TokenDeclaration, "greet") {
		t.Errorf("no declaration token mentioning 'greet'; tokens=%v", res.RichTokens)
	}
	if !tokenContains(res.RichTokens, TokenDeclaration, "main") {
		t.Errorf("no declaration token mentioning 'main'")
	}
	if !tokenContains(res.RichTokens, TokenImport, "fmt") {
		t.Errorf("no import token mentioning 'fmt'")
	}
	if !tokenContains(res.RichTokens, TokenCall, "Println") {
		t.Errorf("no call token mentioning 'Println'")
	}
}

func TestProcessFileReadError(t *testing.T) {
	spec := specFor(t, LangGo)
	missing := filepath.Join(t.TempDir(), "nope.go")
	task := FileTask{
		FilePath: missing,
		RelPath:  "nope.go",
		Language: LangGo,
		Change:   ChangeModified,
	}

	p := newParser()
	defer p.Close()

	res := processFile(p, task, spec)
	if res == nil {
		t.Fatal("processFile returned nil result")
	}
	if res.Error == nil {
		t.Fatal("expected read error, got nil")
	}
	if len(res.RichTokens) != 0 {
		t.Errorf("RichTokens = %v, want empty on read failure", res.RichTokens)
	}
}

func TestProcessFileGoSyntaxError(t *testing.T) {
	src := filepath.Join(t.TempDir(), "broken.go")
	writeTestFile(t, src, "package main\n\nfunc broken( {\n")

	spec := specFor(t, LangGo)
	task := FileTask{FilePath: src, RelPath: "broken.go", Language: LangGo, Change: ChangeAdded}

	p := newParser()
	defer p.Close()

	res := processFile(p, task, spec)
	if res == nil {
		t.Fatal("processFile returned nil result")
	}
	if res.Error != nil {
		t.Fatalf("unexpected error: %v", res.Error)
	}
	if !res.HasErrors {
		t.Error("HasErrors = false, want true for malformed source")
	}
}

func TestExtractTokensPython(t *testing.T) {
	src := filepath.Join(t.TempDir(), "util.py")
	writeTestFile(t, src, `def greet(name):
    return "hi " + name


def main():
    print(greet("world"))
`)

	spec := specFor(t, LangPython)
	task := FileTask{FilePath: src, RelPath: "util.py", Language: LangPython, Change: ChangeAdded}

	p := newParser()
	defer p.Close()

	res := processFile(p, task, spec)
	if res == nil {
		t.Fatal("processFile returned nil result")
	}
	if res.Error != nil {
		t.Skipf("python grammar unavailable in this build: %v", res.Error)
	}
	if len(res.RichTokens) == 0 {
		t.Fatal("RichTokens empty, want > 0")
	}
	if !tokenContains(res.RichTokens, TokenDeclaration, "greet") {
		t.Errorf("no declaration token mentioning 'greet'; tokens=%v", res.RichTokens)
	}
	if !tokenContains(res.RichTokens, TokenCall, "print") {
		t.Errorf("no call token mentioning 'print'")
	}
}

func TestExtractTokensJava(t *testing.T) {
	src := filepath.Join(t.TempDir(), "Hello.java")
	writeTestFile(t, src, `public class Hello {
    public static void main(String[] args) {
        System.out.println("hi");
    }
}
`)

	spec := specFor(t, LangJava)
	task := FileTask{FilePath: src, RelPath: "Hello.java", Language: LangJava, Change: ChangeAdded}

	p := newParser()
	defer p.Close()

	res := processFile(p, task, spec)
	if res == nil {
		t.Fatal("processFile returned nil result")
	}
	if res.Error != nil {
		t.Skipf("java grammar unavailable in this build: %v", res.Error)
	}
	if len(res.RichTokens) == 0 {
		t.Fatal("RichTokens empty, want > 0")
	}
	if !tokenContains(res.RichTokens, TokenDeclaration, "Hello") {
		t.Errorf("no declaration token mentioning 'Hello'; tokens=%v", res.RichTokens)
	}
	if !tokenContains(res.RichTokens, TokenCall, "println") {
		t.Errorf("no call token mentioning 'println'")
	}
}
