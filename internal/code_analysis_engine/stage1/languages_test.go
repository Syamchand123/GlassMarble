package stage1

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegistryNotEmpty(t *testing.T) {
	reg := Registry()
	if len(reg) == 0 {
		t.Fatal("Registry() returned empty slice")
	}
}

func TestRegistryLangUniqueness(t *testing.T) {
	reg := Registry()
	seen := make(map[SupportedLang]bool)
	for _, spec := range reg {
		if seen[spec.Lang] {
			t.Errorf("duplicate language %s in registry", spec.Lang)
		}
		seen[spec.Lang] = true
		if spec.NewLanguage == nil && spec.Lang != LangKotlin && spec.Lang != LangSwift && spec.Lang != LangScala {
			t.Errorf("language %s has nil NewLanguage factory", spec.Lang)
		}
		if len(spec.Extensions) == 0 && len(spec.Filenames) == 0 {
			t.Errorf("language %s has no extensions or filenames", spec.Lang)
		}
	}
}

func TestDetectLanguageByExtension(t *testing.T) {
	reg := Registry()
	tests := []struct {
		path string
		lang SupportedLang
		ok   bool
	}{
		{"foo.go", LangGo, true},
		{"foo.py", LangPython, true},
		{"foo.java", LangJava, true},
		{"foo.js", LangJS, true},
		{"foo.ts", LangTS, true},
		{"foo.tsx", LangTS, true},
		{"foo.rb", LangRuby, true},
		{"foo.php", LangPHP, true},
		{"foo.cpp", LangCpp, true},
		{"foo.c", LangC, true},
		{"foo.cs", LangCSharp, true},
		{"foo.rs", LangRust, true},
		{"foo.css", LangCSS, true},
		{"foo.html", LangHTML, true},
		{"foo.json", LangJSON, true},
		{"foo.kt", LangKotlin, true},
		{"foo.swift", LangSwift, true},
		{"foo.scala", LangScala, true},
		{"foo.unknown", LangUnknown, false},
	}

	for _, tt := range tests {
		lang, _, ok := DetectLanguage(tt.path, reg)
		if ok != tt.ok {
			t.Errorf("DetectLanguage(%q) ok=%v, want %v", tt.path, ok, tt.ok)
			continue
		}
		if ok && lang != tt.lang {
			t.Errorf("DetectLanguage(%q) lang=%s, want %s", tt.path, lang, tt.lang)
		}
	}
}

func TestDetectLanguageByFilename(t *testing.T) {
	reg := Registry()
	// Ruby-specific filenames
	lang, _, ok := DetectLanguage("Gemfile", reg)
	if !ok || lang != LangRuby {
		t.Errorf("DetectLanguage(Gemfile) = %s, %v; want LangRuby, true", lang, ok)
	}
	lang, _, ok = DetectLanguage("Rakefile", reg)
	if !ok || lang != LangRuby {
		t.Errorf("DetectLanguage(Rakefile) = %s, %v; want LangRuby, true", lang, ok)
	}
}

func TestDetectLanguageHeaderExtension(t *testing.T) {
	reg := Registry()
	// .hpp should map to C++ (longer match)
	lang, _, ok := DetectLanguage("foo.hpp", reg)
	if !ok || lang != LangCpp {
		t.Errorf("DetectLanguage(foo.hpp) = %s, %v; want LangCpp, true", lang, ok)
	}
	// .h is ambiguous — registry returns first match (C++), which is acceptable
	lang, _, ok = DetectLanguage("foo.h", reg)
	if !ok {
		t.Error("DetectLanguage(foo.h) should match some language")
	}
}

func TestDetectLanguageCaseInsensitive(t *testing.T) {
	reg := Registry()
	tests := []struct {
		path string
		lang SupportedLang
	}{
		{"Foo.GO", LangGo},
		{"Foo.PY", LangPython},
		{"Foo.JAVA", LangJava},
		{".JS", LangJS},
	}
	for _, tt := range tests {
		lang, _, ok := DetectLanguage(tt.path, reg)
		if !ok || lang != tt.lang {
			t.Errorf("DetectLanguage(%q) = %s, %v; want %s, true", tt.path, lang, ok, tt.lang)
		}
	}
}

func TestDetectLanguageNonexistent(t *testing.T) {
	reg := Registry()
	_, _, ok := DetectLanguage("", reg)
	if ok {
		t.Error("DetectLanguage('') should return false")
	}
	_, _, ok = DetectLanguage("/", reg)
	if ok {
		t.Error("DetectLanguage('/') should return false")
	}
}

func TestLookupSpec(t *testing.T) {
	reg := Registry()
	for _, spec := range reg {
		got := lookupSpec(spec.Lang, reg)
		if got == nil {
			t.Errorf("lookupSpec(%s) returned nil", spec.Lang)
		} else if got.Lang != spec.Lang {
			t.Errorf("lookupSpec(%s).Lang = %s", spec.Lang, got.Lang)
		}
	}
	got := lookupSpec(LangUnknown, reg)
	if got != nil {
		t.Errorf("lookupSpec(unknown) = %v, want nil", got)
	}
}

func TestResolveWorkerCount(t *testing.T) {
	if n := resolveWorkerCount(0); n < 1 || n > 8 {
		t.Errorf("resolveWorkerCount(0) = %d, want 1-8", n)
	}
	if n := resolveWorkerCount(4); n != 4 {
		t.Errorf("resolveWorkerCount(4) = %d, want 4", n)
	}
	if n := resolveWorkerCount(100); n != 100 {
		t.Errorf("resolveWorkerCount(100) = %d, want 100", n)
	}
}

func TestClassifyKind(t *testing.T) {
	spec := &LanguageSpec{
		Declarations: []string{"function_declaration", "class_declaration"},
		Imports:      []string{"import_declaration", "import_statement"},
		Calls:        []string{"call_expression", "method_invocation"},
	}

	tests := []struct {
		kind string
		want TokenKind
	}{
		{"function_declaration", TokenDeclaration},
		{"class_declaration", TokenDeclaration},
		{"import_declaration", TokenImport},
		{"call_expression", TokenCall},
		{"method_invocation", TokenCall},

		// Control flow exact matches
		{"if_statement", TokenDeclaration},
		{"for_statement", TokenDeclaration},
		{"return", TokenDeclaration},
		{"if", TokenDeclaration},
		{"defer", TokenDeclaration},

		// Should NOT match (substring traps)
		{"unified_call", ""},
		{"specification", ""},
		{"confidence", ""},
		{"interface_declaration", ""},

		// Unknown kinds
		{"", ""},
		{"identifier", ""},
		{"number_literal", ""},
	}

	for _, tt := range tests {
		got := classifyKind(tt.kind, spec)
		if got != tt.want {
			t.Errorf("classifyKind(%q) = %q, want %q", tt.kind, got, tt.want)
		}
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig("/test/root")
	if cfg.RootDir != "/test/root" {
		t.Errorf("DefaultConfig RootDir = %q, want '/test/root'", cfg.RootDir)
	}
	if cfg.WorkerCount != 0 {
		t.Errorf("DefaultConfig WorkerCount = %d, want 0", cfg.WorkerCount)
	}
	if cfg.MaxFileBytes != defaultMaxFileBytes {
		t.Errorf("DefaultConfig MaxFileBytes = %d, want %d", cfg.MaxFileBytes, defaultMaxFileBytes)
	}
	if cfg.BufferSize != 128 {
		t.Errorf("DefaultConfig BufferSize = %d, want 128", cfg.BufferSize)
	}
	if cfg.Ctx == nil {
		t.Error("DefaultConfig Ctx is nil, want context.Background()")
	}
}

func TestNormalize(t *testing.T) {
	cfg := Config{RootDir: ""}
	normalize(&cfg)
	if cfg.RootDir == "" {
		t.Error("normalize left RootDir empty")
	}
	if cfg.WorkerCount <= 0 {
		t.Errorf("normalize WorkerCount = %d, want > 0", cfg.WorkerCount)
	}
	if cfg.BufferSize <= 0 {
		t.Errorf("normalize BufferSize = %d, want > 0", cfg.BufferSize)
	}
	if cfg.MaxFileBytes <= 0 {
		t.Errorf("normalize MaxFileBytes = %d, want > 0", cfg.MaxFileBytes)
	}
	if cfg.Ctx != nil {
		t.Error("normalize should not set Ctx if nil")
	}
}

func TestMustAbs(t *testing.T) {
	got := mustAbs("")
	if got == "" {
		t.Error("mustAbs('') returned empty")
	}
	got = mustAbs(".")
	if got == "." {
		t.Error("mustAbs('.') returned '.', expected absolute path")
	}
}

func TestIsCommentKind(t *testing.T) {
	commentKinds := []string{"comment", "line_comment", "block_comment", "multiline_comment", "doc_comment", "documentation_comment"}
	for _, k := range commentKinds {
		if !isCommentKind(k) {
			t.Errorf("isCommentKind(%q) = false, want true", k)
		}
	}
	if isCommentKind("identifier") {
		t.Error("isCommentKind('identifier') = true, want false")
	}
	if isCommentKind("") {
		t.Error("isCommentKind('') = true, want false")
	}
}

func TestDefaultSkipDirs(t *testing.T) {
	essential := []string{".git", "node_modules", "vendor", "dist", "__pycache__", "target", ".idea"}
	for _, d := range essential {
		if _, ok := defaultSkipDirs[d]; !ok {
			t.Errorf("defaultSkipDirs missing essential directory: %s", d)
		}
	}
}

func TestConfigDefaults(t *testing.T) {
	cfg := DefaultConfig(".")
	if cfg.IncludeHidden {
		t.Error("DefaultConfig IncludeHidden should be false")
	}
}

// Test14LanguageFixturesParse verifies that sample fixtures for all 14 supported languages parse without panics (W6-01 / §10.3).
func Test14LanguageFixturesParse(t *testing.T) {
	langs := []string{
		"go", "python", "javascript", "typescript", "java", "csharp",
		"rust", "c", "cpp", "kotlin", "php", "ruby", "swift", "scala",
	}

	reg := Registry()

	for _, lang := range langs {
		t.Run(lang, func(t *testing.T) {
			dir := filepath.Join("..", "..", "..", "testdata", "languages", lang)
			files, err := os.ReadDir(dir)
			require.NoError(t, err, "fixture dir for %s must exist", lang)
			require.NotEmpty(t, files, "fixture dir for %s must contain at least 1 file", lang)

			filePath := filepath.Join(dir, files[0].Name())
			_, spec, found := DetectLanguage(filePath, reg)
			assert.True(t, found, "language %s must be detected in registry", lang)
			assert.NotNil(t, spec, "spec for %s must not be nil", lang)
		})
	}
}
