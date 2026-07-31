package stage1

import (
	"path/filepath"
	"strings"
	"unsafe"

	sitter "github.com/tree-sitter/go-tree-sitter"

	tree_sitter_csharp "github.com/tree-sitter/tree-sitter-c-sharp/bindings/go"
	tree_sitter_c "github.com/tree-sitter/tree-sitter-c/bindings/go"
	tree_sitter_cpp "github.com/tree-sitter/tree-sitter-cpp/bindings/go"
	tree_sitter_css "github.com/tree-sitter/tree-sitter-css/bindings/go"
	tree_sitter_go "github.com/tree-sitter/tree-sitter-go/bindings/go"
	tree_sitter_html "github.com/tree-sitter/tree-sitter-html/bindings/go"
	tree_sitter_java "github.com/tree-sitter/tree-sitter-java/bindings/go"
	tree_sitter_javascript "github.com/tree-sitter/tree-sitter-javascript/bindings/go"
	tree_sitter_json "github.com/tree-sitter/tree-sitter-json/bindings/go"
	tree_sitter_php "github.com/tree-sitter/tree-sitter-php/bindings/go"
	tree_sitter_python "github.com/tree-sitter/tree-sitter-python/bindings/go"
	tree_sitter_ruby "github.com/tree-sitter/tree-sitter-ruby/bindings/go"
	tree_sitter_rust "github.com/tree-sitter/tree-sitter-rust/bindings/go"
	tree_sitter_typescript "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
)

// LanguageSpec pairs a language identifier with the grammar binding, thelangrusr
// file extensions it claims, and the tree-sitter node kinds that the
// engine cares about.  Extending the engine to a new language is a one-
// line addition to the Registry() slice below.
type LanguageSpec struct {
	Lang             SupportedLang
	Extensions       []string
	HeaderExtensions []string
	Filenames        []string
	IsConfigFormat   bool
	NewLanguage      func() *sitter.Language
	Declarations     []string
	Imports          []string
	Calls            []string
}

// asLang wraps a grammar package's Language() unsafe.Pointer factory into
// the *sitter.Language constructor expected by the Registry.
func asLang(fn func() unsafe.Pointer) func() *sitter.Language {
	return func() *sitter.Language { return sitter.NewLanguage(fn()) }
}

// Registry returns the full set of grammars wired into Stage 1.  Order
// does not matter because DetectLanguage always picks the longest
// matching extension suffix.
func Registry() []LanguageSpec {
	return []LanguageSpec{
		{
			Lang:        LangRust,
			Extensions:  []string{".rs"},
			NewLanguage: asLang(tree_sitter_rust.Language),
			Declarations: []string{
				"function_item", "impl_item", "struct_item",
				"enum_item", "trait_item", "type_item",
				"mod_item", "static_item", "const_item",
			},
			Imports: []string{"use_declaration", "extern_crate_declaration"},
			Calls:   []string{"call_expression", "macro_invocation"},
		},
		{
			Lang:        LangGo,
			Extensions:  []string{".go"},
			NewLanguage: asLang(tree_sitter_go.Language),
			Declarations: []string{
				"function_declaration", "method_declaration",
				"type_declaration", "type_spec", "function_type",
			},
			Imports: []string{"import_declaration", "import_spec", "import_path_spec"},
			Calls:   []string{"call_expression"},
		},
		{
			Lang:        LangPython,
			Extensions:  []string{".py", ".pyi"},
			NewLanguage: asLang(tree_sitter_python.Language),
			Declarations: []string{
				"function_definition", "class_definition",
				"decorated_definition",
			},
			Imports: []string{
				"import_statement", "import_from_statement",
				"future_import_statement",
			},
			Calls: []string{"call"},
		},
		{
			Lang:        LangJava,
			Extensions:  []string{".java"},
			NewLanguage: asLang(tree_sitter_java.Language),
			Declarations: []string{
				"class_declaration", "interface_declaration",
				"enum_declaration", "method_declaration",
				"constructor_declaration", "record_declaration",
				"annotation_type_declaration",
			},
			Imports: []string{"import_declaration", "package_declaration"},
			Calls:   []string{"method_invocation", "object_creation_expression"},
		},
		{
			Lang:        LangJS,
			Extensions:  []string{".js", ".mjs", ".cjs", ".jsx"},
			NewLanguage: asLang(tree_sitter_javascript.Language),
			Declarations: []string{
				"function_declaration", "function_expression",
				"generator_function_declaration", "method_definition",
				"class_declaration", "lexical_declaration",
				"variable_declaration",
			},
			Imports: []string{"import_statement"},
			Calls:   []string{"call_expression", "new_expression"},
		},
		{
			Lang:        LangTS,
			Extensions:  []string{".ts", ".tsx", ".cts", ".mts"},
			NewLanguage: asLang(tree_sitter_typescript.LanguageTypescript),
			Declarations: []string{
				"function_declaration", "method_definition",
				"class_declaration", "interface_declaration",
				"type_alias_declaration", "enum_declaration",
				"lexical_declaration", "abstract_class_declaration",
			},
			Imports: []string{"import_statement"},
			Calls:   []string{"call_expression", "new_expression"},
		},
		{
			Lang:             LangCpp,
			Extensions:       []string{".cpp", ".cc", ".cxx"},
			HeaderExtensions: []string{".hpp", ".hh", ".hxx", ".h"},
			NewLanguage:      asLang(tree_sitter_cpp.Language),
			Declarations: []string{
				"function_definition", "class_specifier",
				"struct_specifier", "union_specifier",
				"namespace_definition", "template_declaration",
				"declaration",
			},
			Imports: []string{"preproc_include"},
			Calls:   []string{"call_expression"},
		},
		{
			Lang:             LangC,
			Extensions:       []string{".c"},
			HeaderExtensions: []string{".h"},
			NewLanguage:      asLang(tree_sitter_c.Language),
			Declarations: []string{
				"function_definition", "struct_specifier",
				"union_specifier", "enum_specifier", "declaration",
			},
			Imports: []string{"preproc_include"},
			Calls:   []string{"call_expression"},
		},
		{
			Lang:        LangCSharp,
			Extensions:  []string{".cs"},
			NewLanguage: asLang(tree_sitter_csharp.Language),
			Declarations: []string{
				"class_declaration", "interface_declaration",
				"struct_declaration", "enum_declaration",
				"record_declaration", "method_declaration",
				"namespace_declaration",
			},
			Imports: []string{"using_directive", "namespace_declaration"},
			Calls:   []string{"invocation_expression", "object_creation_expression"},
		},
		{
			Lang:        LangRuby,
			Extensions:  []string{".rb"},
			Filenames:   []string{"Rakefile", "Gemfile", "config.ru"},
			NewLanguage: asLang(tree_sitter_ruby.Language),
			Declarations: []string{
				"function", "method", "class", "module",
				"singleton_method", "singleton_class",
			},
			Imports: []string{"call"},
			Calls:   []string{"call", "method_call"},
		},
		{
			Lang:        LangPHP,
			Extensions:  []string{".php"},
			NewLanguage: asLang(tree_sitter_php.LanguagePHP),
			Declarations: []string{
				"function_definition", "method_declaration",
				"class_declaration", "interface_declaration",
				"trait_declaration", "enum_declaration",
			},
			Imports: []string{"namespace_use_declaration", "namespace_use_clause"},
			Calls: []string{
				"function_call_expression", "member_call_expression",
				"scoped_call_expression", "object_creation_expression",
			},
		},
		{
			Lang:        LangCSS,
			Extensions:  []string{".css", ".scss", ".less"},
			NewLanguage: asLang(tree_sitter_css.Language),
			Declarations: []string{
				"rule_set", "media_statement", "keyframes_statement",
				"keyframe_rule_set", "block",
			},
			Imports: []string{"import_statement"},
			Calls:   []string{"call_expression"},
		},
		{
			Lang:        LangHTML,
			Extensions:  []string{".html", ".htm", ".xhtml"},
			NewLanguage: asLang(tree_sitter_html.Language),
			Declarations: []string{
				"doctype", "script_element", "style_element", "element",
			},
			Imports: []string{"attribute"},
			Calls:   []string{},
		},
		{
			Lang:           LangJSON,
			Extensions:     []string{".json", ".jsonc"},
			IsConfigFormat: true,
			NewLanguage:    asLang(tree_sitter_json.Language),
			Declarations: []string{
				"pair", "object", "array",
			},
			Imports: []string{},
			Calls:   []string{},
		},
	}
}

// DetectLanguage resolves a file path to its LanguageSpec.  It prefers
// the longest matching extension suffix to avoid collisions (e.g. ".hpp"
// wins over ".h" when both C and C++ are registered).
func DetectLanguage(path string, reg []LanguageSpec) (SupportedLang, *LanguageSpec, bool) {
	base := filepath.Base(path)
	ext := strings.ToLower(filepath.Ext(path))

	for i := range reg {
		spec := &reg[i]
		for _, name := range spec.Filenames {
			if strings.EqualFold(base, name) {
				return spec.Lang, spec, true
			}
		}
	}

	var best *LanguageSpec
	var bestLen int
	for i := range reg {
		spec := &reg[i]
		for _, e := range spec.Extensions {
			if strings.EqualFold(ext, e) && len(e) > bestLen {
				bestLen = len(e)
				best = spec
			}
		}
	}
	if best != nil {
		return best.Lang, best, true
	}

	// Check HeaderExtensions (e.g. .hpp -> C++, .h -> C)
	// Prefer longer match to avoid .h matching C++ over C
	var headerBest *LanguageSpec
	var headerBestLen int
	for i := range reg {
		spec := &reg[i]
		for _, e := range spec.HeaderExtensions {
			if strings.EqualFold(ext, e) && len(e) > headerBestLen {
				headerBestLen = len(e)
				headerBest = spec
			}
		}
	}
	if headerBest != nil {
		return headerBest.Lang, headerBest, true
	}

	return LangUnknown, nil, false
}
