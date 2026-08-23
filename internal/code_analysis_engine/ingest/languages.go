package ingest

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

// LanguageSpec pairs a language identifier with the grammar binding, the language
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
	// FieldDeclKinds marks field-bearing declaration kinds so Ingestion can
	// set RichToken.IsFieldDecl (master_overhaul_plan.md §5.1.1/§5.1.2).
	FieldDeclKinds []string
	// MethodSpecKinds marks interface method declaration kinds so Ingestion
	// can set RichToken.IsMethodSpec (fixes A-04 at the source).
	MethodSpecKinds []string
	// EmbeddedKinds marks anonymous-embedding / base-class kinds so Ingestion
	// can set RichToken.IsEmbedded (Go embedded_field, C++ base_class_specifier).
	EmbeddedKinds []string
}

func (s *LanguageSpec) isFieldDeclKind(kind string) bool {
	for _, k := range s.FieldDeclKinds {
		if kind == k {
			return true
		}
	}
	return false
}

func (s *LanguageSpec) isMethodSpecKind(kind string) bool {
	for _, k := range s.MethodSpecKinds {
		if kind == k {
			return true
		}
	}
	return false
}

func (s *LanguageSpec) isEmbeddedKind(kind string) bool {
	for _, k := range s.EmbeddedKinds {
		if kind == k {
			return true
		}
	}
	return false
}

// asLang wraps a grammar package's Language() unsafe.Pointer factory into
// the *sitter.Language constructor expected by the Registry.
func asLang(fn func() unsafe.Pointer) func() *sitter.Language {
	return func() *sitter.Language { return sitter.NewLanguage(fn()) }
}

// Registry returns the full set of grammars wired into Ingestion.  Order
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
				"field_declaration", "where_clause",
			},
			FieldDeclKinds: []string{"field_declaration"},
			Imports:        []string{"use_declaration", "extern_crate_declaration"},
			Calls:          []string{"call_expression", "macro_invocation"},
		},
		{
			Lang:        LangGo,
			Extensions:  []string{".go"},
			NewLanguage: asLang(tree_sitter_go.Language),
			Declarations: []string{
				"function_declaration", "method_declaration",
				"type_spec", "function_type",
				// §5.1.2: structural members of declarations surface as
				// declaration tokens so GAST v2 can model fields, parameters,
				// interface methods and embedded types (fixes A-03/A-04/A-17).
				// NB: tree-sitter-go@v0.25.0 names interface methods method_elem
				// (NOT method_spec) and models anonymous embedding as a
				// field_declaration without a name field (no embedded_field
				// kind in this grammar). type_elem = embedded interface inside
				// interface_type.
				"field_declaration", "method_elem",
				"parameter_declaration", "type_elem",
			},
			FieldDeclKinds:  []string{"field_declaration"},
			MethodSpecKinds: []string{"method_elem"},
			EmbeddedKinds:   []string{"type_elem"},
			// type_declaration is a container node (`type Foo struct{...}` wraps a
			// type_spec, `type ( ... )` wraps several) that carries no name of its
			// own — classifying it produced an empty-ID GASTTypeDeclaration node
			// alongside the real type_spec node. Only the leaf type_spec is a
			// declaration. Same story for import_declaration below (AUDIT Issue 1).
			// import_declaration is the parenthesized block container (e.g.
			// `import ( "fmt" "time" )`); only the leaf import_spec /
			// import_path_spec nodes carry a single import path. Classifying
			// the container too produced one garbage import string holding
			// the whole block (AUDIT Issue 1 — fabricated ext: nodes).
			Imports: []string{"import_spec", "import_path_spec"},
			Calls:   []string{"call_expression"},
		},
		{
			Lang:        LangPython,
			Extensions:  []string{".py", ".pyi"},
			NewLanguage: asLang(tree_sitter_python.Language),
			Declarations: []string{
				"function_definition", "class_definition",
				"decorated_definition",
				// §5.1.2: typed parameters carry name + type structurally.
				// expression_statement is deliberately NOT declared: every
				// class-body attribute assignment would become a declaration
				// node, flooding the graph; class attributes surface through
				// the class_definition FieldRoles instead.
				"typed_parameter",
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
				// §5.1.2. superclass / super_interfaces are grammar FIELD
				// names, not node kinds — inheritance surfaces through the
				// class_declaration FieldRoles instead of Declarations.
				"field_declaration", "formal_parameter",
			},
			FieldDeclKinds: []string{"field_declaration"},
			// MethodSpecKinds / EmbeddedKinds stay empty for Java: the
			// grammar has no dedicated node kinds for interface methods
			// (they are method_declaration inside interface_body) or base
			// classes (extends/implements are FIELD roles on the class
			// node). The parser flags both structurally instead:
			// isInsideInterfaceBody sets IsMethodSpec, and phase 2's
			// java_translator reads superclass/interfaces field roles for
			// inheritance (GAP-L-05 / §5.1.2).
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
				// §5.1.2: class members (incl. public-field shorthand) become
				// declaration tokens for GAST v2 field modeling.
				"property_definition", "field_definition",
			},
			FieldDeclKinds: []string{"property_definition", "field_definition"},
			Imports:        []string{"import_statement"},
			Calls:          []string{"call_expression", "new_expression"},
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
				// §5.1.2: interface members (property_signature,
				// method_signature) and heritage clause carry name/type/base
				// structurally (fixes A-03/A-04/A-07/A-17).
				"property_signature", "method_signature",
				"heritage_clause",
			},
			FieldDeclKinds:  []string{"property_signature"},
			MethodSpecKinds: []string{"method_signature"},
			Imports:         []string{"import_statement"},
			Calls:           []string{"call_expression", "new_expression"},
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
				// §5.1.2: fields, parameters and base classes become
				// declaration tokens (base_class_specifier marks IsEmbedded).
				"field_declaration", "parameter_declaration",
				"base_class_clause", "base_class_specifier",
			},
			FieldDeclKinds: []string{"field_declaration"},
			EmbeddedKinds:  []string{"base_class_specifier"},
			Imports:        []string{"preproc_include"},
			Calls:          []string{"call_expression"},
		},
		{
			Lang:             LangC,
			Extensions:       []string{".c"},
			HeaderExtensions: []string{".h"},
			NewLanguage:      asLang(tree_sitter_c.Language),
			Declarations: []string{
				"function_definition", "struct_specifier",
				"union_specifier", "enum_specifier", "declaration",
				// §5.1.2: fields and parameters become declaration tokens.
				"field_declaration", "parameter_declaration",
			},
			FieldDeclKinds: []string{"field_declaration"},
			Imports:        []string{"preproc_include"},
			Calls:          []string{"call_expression"},
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
				// §5.1.2: fields and base_list carry members/base types
				// structurally (fixes A-03/A-07/A-17).
				"field_declaration", "base_list", "parameter",
			},
			FieldDeclKinds: []string{"field_declaration"},
			Imports:        []string{"using_directive", "namespace_declaration"},
			Calls:          []string{"invocation_expression", "object_creation_expression"},
		},
		{
			Lang:        LangRuby,
			Extensions:  []string{".rb"},
			Filenames:   []string{"Rakefile", "Gemfile", "config.ru"},
			NewLanguage: asLang(tree_sitter_ruby.Language),
			Declarations: []string{
				"function", "method", "class", "module",
				"singleton_method", "singleton_class",
				// §5.1.2: instance variables become declaration tokens so
				// Ruby fields surface structurally instead of by regex.
				"ivar", "module_function",
			},
			FieldDeclKinds: []string{"ivar"},
			// EmbeddedKinds stays empty for Ruby: mixins (`include Foo`,
			// `prepend Bar`) are call nodes whose method field is the
			// keyword — no distinct kind exists. The parser's
			// isRubyIncludeCall flags them as IsEmbedded (GAP-L-05 / §5.1.2).
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
				// §5.1.2: properties, parameters and inheritance clauses become
				// declaration tokens (fixes A-03/A-07/A-17).
				"property_declaration", "formal_parameters",
				"extends_clause", "implements_clause",
			},
			FieldDeclKinds: []string{"property_declaration"},
			Imports:        []string{"namespace_use_declaration", "namespace_use_clause"},
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
			// Kotlin: declaration-only pending — no tree-sitter grammar wired yet.
			// Extensions disabled to avoid nil NewLanguage panic; re-enable when binding added.
			Lang:       LangKotlin,
			Extensions: []string{},
			Declarations: []string{
				"class_declaration", "function_declaration", "object_declaration",
			},
		},
		{
			// Swift: declaration-only pending — no tree-sitter grammar wired yet.
			Lang:       LangSwift,
			Extensions: []string{},
			Declarations: []string{
				"class_declaration", "function_declaration", "protocol_declaration",
			},
		},
		{
			// Scala: declaration-only pending — no tree-sitter grammar wired yet.
			Lang:       LangScala,
			Extensions: []string{},
			Declarations: []string{
				"class_definition", "function_definition", "object_definition",
			},
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
