package ingest

import (
	"encoding/csv"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

var updateGolden = flag.Bool("update-golden", false, "rewrite testdata/declarations_golden.csv from the registry")

func findToken(t *testing.T, tokens []RichToken, pred func(RichToken) bool) *RichToken {
	t.Helper()
	for i := range tokens {
		if pred(tokens[i]) {
			return &tokens[i]
		}
	}
	return nil
}

// TestJavaInterfaceMethodSpecs is the GAP-L-05 gate: Java interface methods
// (method_declaration under interface_body) must be flagged IsMethodSpec
// while ordinary class methods must not, even when the class implements an
// interface.
func TestJavaInterfaceMethodSpecs(t *testing.T) {
	const src = `package p;

interface Service {
    String ping(String in);
    void reset();
}

class ServiceImpl implements Service {
    String ping(String in) { return in; }
    void reset() {}
}
`
	res := parseSnippet(t, LangJava, "service.java", src)

	var ping, reset, implPing *RichToken
	for i := range res.RichTokens {
		tok := &res.RichTokens[i]
		if tok.Kind != TokenDeclaration || tok.Type != "method_declaration" {
			continue
		}
		switch tok.Name {
		case "ping":
			if ping == nil {
				ping = &res.RichTokens[i]
			} else {
				implPing = &res.RichTokens[i]
			}
		case "reset":
			if reset == nil {
				reset = &res.RichTokens[i]
			}
		}
	}
	if ping == nil || reset == nil {
		t.Fatalf("interface method_declarations not found; tokens=%v", res.RichTokens)
	}
	if !ping.IsMethodSpec {
		t.Errorf("interface method 'ping' should be IsMethodSpec (isInsideInterfaceBody)")
	}
	if !reset.IsMethodSpec {
		t.Errorf("interface method 'reset' should be IsMethodSpec (isInsideInterfaceBody)")
	}
	if implPing == nil {
		t.Fatal("class method 'ping' not found")
	}
	if implPing.IsMethodSpec {
		t.Errorf("class method 'ping' must NOT be IsMethodSpec; got %+v", implPing)
	}
}

// TestRubyMixinEmbedding is the GAP-L-05 gate: `include`/`prepend` call
// nodes must be flagged IsEmbedded so phase 2 can model mixin embedding.
func TestRubyMixinEmbedding(t *testing.T) {
	const src = `module Greets
end

class Widget
  include Greets
  prepend Auditable
  def greet
    puts "hi"
  end
end
`
	res := parseSnippet(t, LangRuby, "widget.rb", src)

	var includeTok, prependTok, otherCall *RichToken
	for i := range res.RichTokens {
		tok := &res.RichTokens[i]
		if tok.Kind != TokenImport {
			continue
		}
		switch tok.Name {
		case "include":
			includeTok = &res.RichTokens[i]
		case "prepend":
			prependTok = &res.RichTokens[i]
		case "puts":
			otherCall = &res.RichTokens[i]
		}
	}
	if includeTok == nil || prependTok == nil {
		t.Fatalf("include/prepend call tokens not found; tokens=%v", res.RichTokens)
	}
	if !includeTok.IsEmbedded {
		t.Errorf("`include Greets` should be IsEmbedded; got %+v", includeTok)
	}
	if !prependTok.IsEmbedded {
		t.Errorf("`prepend Auditable` should be IsEmbedded; got %+v", prependTok)
	}
	if otherCall != nil && otherCall.IsEmbedded {
		t.Errorf("plain call must not be IsEmbedded; got %+v", otherCall)
	}
}

func parseSnippet(t *testing.T, lang SupportedLang, relPath, src string) *IngestionResult {
	t.Helper()
	spec := specFor(t, lang)
	path := filepath.Join(t.TempDir(), relPath)
	writeTestFile(t, path, src)
	p := newParser()
	defer p.Close()
	res := processFile(p, FileTask{
		FilePath: path,
		RelPath:  relPath,
		Language: lang,
		Change:   ChangeModified,
	}, spec)
	if res.Error != nil {
		t.Fatalf("processFile(%s): %v", relPath, res.Error)
	}
	return res
}

// TestGoRichToken_Full is the §5.1.4 acceptance: a fixture Go file whose
// type_spec tokens expose FieldRoles["name"], whose fields are child tokens
// (linked by ParentIdx) with name/type roles, and whose interfaces expose
// method_spec children.
func TestGoRichToken_Full(t *testing.T) {
	const src = `package widget

import "fmt"

type Base struct {
	ID int
}

type Options struct {
	Name   string
	Weight float64
	Base
}

type Provider interface {
	Connect() error
	Name() string
}

func (o *Options) Rename(name string) error {
	o.Name = name
	return nil
}

func New() *Options {
	o := &Options{Name: "x"}
	fmt.Println(o.Name)
	return o
}
`
	res := parseSnippet(t, LangGo, "widget.go", src)
	if len(res.RichTokens) == 0 {
		t.Fatal("RichTokens empty")
	}

	optionsIdx := -1
	for i := range res.RichTokens {
		tok := &res.RichTokens[i]
		if tok.Kind == TokenDeclaration && tok.Type == "type_spec" && tok.Name == "Options" {
			optionsIdx = i
			if tok.FieldRoles["name"] != "Options" {
				t.Errorf("Options FieldRoles[name] = %q, want %q", tok.FieldRoles["name"], "Options")
			}
			if !strings.Contains(tok.FieldRoles["type"], "struct") {
				t.Errorf("Options FieldRoles[type] = %q, want to mention 'struct'", tok.FieldRoles["type"])
			}
			if tok.IsFieldDecl {
				t.Error("type_spec should not be flagged IsFieldDecl")
			}
		}
	}
	if optionsIdx == -1 {
		t.Fatal("no type_spec RichToken for Options")
	}

	var nameField, weightField, embedded *RichToken
	for i := range res.RichTokens {
		tok := &res.RichTokens[i]
		if tok.ParentIdx != optionsIdx {
			continue
		}
		switch {
		case tok.Type == "field_declaration" && tok.IsEmbedded:
			embedded = tok
		case tok.Type == "field_declaration" && tok.Name == "Name":
			nameField = tok
		case tok.Type == "field_declaration" && tok.Name == "Weight":
			weightField = tok
		}
	}
	if nameField == nil {
		t.Fatal("no field_declaration child (ParentIdx) named 'Name' on Options")
	}
	if !nameField.IsFieldDecl {
		t.Error("field_declaration 'Name' should be flagged IsFieldDecl")
	}
	if nameField.FieldRoles["name"] != "Name" || nameField.FieldRoles["type"] != "string" {
		t.Errorf("Name field roles = %v, want name=Name type=string", nameField.FieldRoles)
	}
	if weightField == nil || weightField.FieldRoles["type"] != "float64" {
		t.Errorf("Weight field roles = %v, want type=float64", weightField.FieldRoles)
	}
	if embedded == nil {
		t.Fatal("no embedded field_declaration child (ParentIdx) on Options")
	}
	if !embedded.IsEmbedded || !embedded.IsFieldDecl {
		t.Errorf("embedded field %+v should be IsEmbedded && IsFieldDecl", embedded)
	}
	if embedded.Name != "Base" {
		t.Errorf("embedded field Name = %q, want 'Base'", embedded.Name)
	}
	if _, hasName := embedded.FieldRoles["name"]; hasName {
		t.Errorf("embedded field should carry no name role, got %v", embedded.FieldRoles)
	}

	providerIdx := -1
	for i := range res.RichTokens {
		tok := &res.RichTokens[i]
		if tok.Kind == TokenDeclaration && tok.Type == "type_spec" && tok.Name == "Provider" {
			providerIdx = i
		}
	}
	if providerIdx == -1 {
		t.Fatal("no type_spec RichToken for Provider")
	}
	var connect, nameSpec *RichToken
	for i := range res.RichTokens {
		tok := &res.RichTokens[i]
		if tok.ParentIdx != providerIdx || !tok.IsMethodSpec {
			continue
		}
		if tok.Name == "Connect" {
			connect = tok
		}
		if tok.Name == "Name" {
			nameSpec = tok
		}
	}
	if connect == nil || nameSpec == nil {
		t.Fatalf("Provider should expose method_spec children; stream=%v", res.RichTokens)
	}
	if !connect.IsMethodSpec {
		t.Error("method_spec 'Connect' should be flagged IsMethodSpec")
	}
	if connect.FieldRoles["name"] != "Connect" {
		t.Errorf("Connect FieldRoles[name] = %q", connect.FieldRoles["name"])
	}
	if !strings.Contains(connect.FieldRoles["result"], "error") {
		t.Errorf("Connect FieldRoles[result] = %q, want to mention 'error'", connect.FieldRoles["result"])
	}

	rename := findToken(t, res.RichTokens, func(tok RichToken) bool {
		return tok.Kind == TokenDeclaration && tok.Type == "method_declaration" && tok.Name == "Rename"
	})
	if rename == nil {
		t.Fatal("no method_declaration for Rename")
	}
	if !strings.Contains(rename.FieldRoles["receiver"], "Options") {
		t.Errorf("Rename FieldRoles[receiver] = %q, want to mention 'Options'", rename.FieldRoles["receiver"])
	}
	if rename.FieldRoles["name"] != "Rename" {
		t.Errorf("Rename FieldRoles[name] = %q", rename.FieldRoles["name"])
	}
	if !strings.Contains(rename.FieldRoles["parameters"], "name") {
		t.Errorf("Rename FieldRoles[parameters] = %q, want to mention 'name'", rename.FieldRoles["parameters"])
	}
	if rename.FieldRoles["result"] != "error" {
		t.Errorf("Rename FieldRoles[result] = %q, want 'error'", rename.FieldRoles["result"])
	}

	param := findToken(t, res.RichTokens, func(tok RichToken) bool {
		return tok.Kind == TokenDeclaration && tok.Type == "parameter_declaration" && tok.Name == "name"
	})
	if param == nil {
		t.Fatal("no parameter_declaration for 'name'")
	}
	if param.FieldRoles["name"] != "name" || param.FieldRoles["type"] != "string" {
		t.Errorf("param roles = %v, want name=name type=string", param.FieldRoles)
	}
}

// TestDeclarationsCoverage is the §5.1.4 acceptance: the Declarations list
// of every registered language matches the committed golden CSV
// (testdata/declarations_golden.csv). The golden is regenerated with
// -update-golden; the committed copy is authoritative.
func TestDeclarationsCoverage(t *testing.T) {
	goldenPath := filepath.Join("testdata", "declarations_golden.csv")

	if *updateGolden {
		f, err := os.Create(goldenPath)
		if err != nil {
			t.Fatalf("create golden: %v", err)
		}
		w := csv.NewWriter(f)
		for _, spec := range Registry() {
			kinds := append([]string(nil), spec.Declarations...)
			sort.Strings(kinds)
			if err := w.Write([]string{string(spec.Lang), strings.Join(kinds, ",")}); err != nil {
				t.Fatalf("write golden: %v", err)
			}
		}
		w.Flush()
		if err := w.Error(); err != nil {
			t.Fatalf("flush golden: %v", err)
		}
		f.Close()
		t.Logf("wrote %s", goldenPath)
		return
	}

	raw, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden %s (run with -update-golden to regenerate): %v", goldenPath, err)
	}
	rows, err := csv.NewReader(strings.NewReader(string(raw))).ReadAll()
	if err != nil {
		t.Fatalf("parse golden: %v", err)
	}
	golden := make(map[string]string, len(rows))
	for _, row := range rows {
		if len(row) != 2 {
			t.Fatalf("golden row %v: want [lang, kinds]", row)
		}
		golden[row[0]] = row[1]
	}

	for _, spec := range Registry() {
		want, ok := golden[string(spec.Lang)]
		if !ok {
			t.Errorf("%s missing from golden CSV", spec.Lang)
			continue
		}
		kinds := append([]string(nil), spec.Declarations...)
		sort.Strings(kinds)
		got := strings.Join(kinds, ",")
		if got != want {
			t.Errorf("%s Declarations drifted from golden:\n  golden: %s\n  got:    %s\n  (regen: go test ./internal/code_analysis_engine/ingest -run TestDeclarationsCoverage -update-golden)",
				spec.Lang, want, got)
		}
	}
}

// TestIngestion_AllLanguages is the §5.1.4 regression gate: every registered
// grammar must still produce tokens from a minimal snippet.
func TestIngestion_AllLanguages(t *testing.T) {
	snippets := []struct {
		lang   SupportedLang
		file   string
		source string
	}{
		{LangGo, "a.go", "package p\nfunc f() {}\n"},
		{LangRust, "a.rs", "fn f() {}\n"},
		{LangPython, "a.py", "def f():\n    pass\n"},
		{LangJava, "a.java", "class A { void m() {} }\n"},
		{LangJS, "a.js", "function f() {}\n"},
		{LangTS, "a.ts", "interface I { a: number; m(): void }\n"},
		{LangCpp, "a.cpp", "struct A { int x; };\n"},
		{LangC, "a.c", "struct A { int x; };\n"},
		{LangCSharp, "a.cs", "class A { int x; }\n"},
		{LangRuby, "a.rb", "def f\nend\n"},
		{LangPHP, "a.php", "<?php function f() {}\n"},
		{LangCSS, "a.css", ".a { color: red; }\n"},
		{LangHTML, "a.html", "<div></div>\n"},
		{LangJSON, "a.json", "{\"a\": 1}\n"},
	}
	for _, s := range snippets {
		res := parseSnippet(t, s.lang, s.file, s.source)
		if len(res.RichTokens) == 0 {
			t.Errorf("%s: zero tokens from snippet", s.lang)
		}
	}
}

// TestParseThroughputNoRegression is the §5.1.4 speed guard: a 3000-line
// synthetic file must parse well under a generous budget so the field-role
// capture cannot silently regress the fast path by an order of magnitude.
func TestParseThroughputNoRegression(t *testing.T) {
	var b strings.Builder
	b.WriteString("package p\n")
	for i := 0; i < 1000; i++ {
		fmt.Fprintf(&b, "type T%d struct {\n\tName string\n\tValue int\n\tBase\n}\n\nfunc (t *T%d) M%d(x int) string { return \"\" }\n\n", i, i, i)
	}
	src := b.String()

	start := time.Now()
	res := parseSnippet(t, LangGo, "big.go", src)
	elapsed := time.Since(start)
	if len(res.RichTokens) == 0 {
		t.Fatal("no tokens")
	}
	const budget = 10 * time.Second
	if elapsed > budget {
		t.Errorf("parse of 3000-line file took %v, want < %v", elapsed, budget)
	}
}
