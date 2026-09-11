// Package langmatrix implements improvement plan B4
// (gmb_docs_engine_improvement_plan.md, section B4): a per-language
// grounding depth matrix over the canonical GAST language list.
//
// The grader is self-contained: it does NOT use the doc engine. It parses
// each embedded fixture with the language's tree-sitter grammar (stdlib +
// already-required grammar modules only) and grades seven coverage
// dimensions structurally where possible.
//
// Grading rubric (mechanical, regression-oriented):
//
//	Dimensions: signatures, doc-comments, call-edges, errors,
//	concurrency, config, tests.
//
//	Grade A — grounded: a tree-sitter grammar for the language is wired
//	in, the fixture parses cleanly (non-nil tree, no ERROR nodes), and the
//	dimension's marker is found:
//	  - signatures: a declaration node kind is present (lists mirror the
//	    GAST ingest.Registry Declarations table).
//	  - doc-comments: a comment node ends within 2 rows above a
//	    declaration node (any comment style; Python leading docstrings
//	    count too).
//	  - call-edges: a call node kind is present (lists mirror the
//	    registry Calls table); every fixture wires a call from func A to
//	    func B.
//	  - errors / concurrency / config / tests: a language-idiomatic
//	    marker pattern matches the source (raise/throw sites,
//	    goroutine/async/thread primitives, env/config reads, test-looking
//	    functions). The clean-parse gate keeps these honest: marker text
//	    in an unparseable file is still C.
//	  - config (json only): the document itself is configuration data,
//	    so any "pair" node counts as grounded.
//
//	Grade B — declared-shallow (reserved for "parses but the marker
//	heuristic is weak"): the fixture parses cleanly but the dimension is
//	satisfied only by an explicit N/A sentinel (NOCONCURRENCY, NOCONFIG,
//	NOTEST) because the language has no idiomatic construct for it
//	(CSS/HTML). B means "correctly known-absent, not grounded": it must
//	never silently become C (sentinel lost), and reaching A needs real
//	engine work, not fixture edits.
//
//	Grade C — ungrounded: no grammar wired (kotlin/swift/scala are
//	declaration-only in the GAST registry, see
//	internal/code_analysis_engine/ingest/languages_test.go), parse
//	failure, unknown language ID, or marker absent (dimensions with no
//	applicable construct and no sentinel; JSON cannot even carry
//	sentinels because it has no comments).
//
// B is currently assigned only via sentinels; the structural-vs-heuristic
// split above is documented so future B1/B2 work knows exactly which
// dimensions need compiler-accurate signals per language.
package langmatrix

import (
	"embed"
	"regexp"
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

//go:embed testdata
var fixtures embed.FS

// DimensionGrade is the per-dimension grounding depth grade.
type DimensionGrade string

const (
	// GradeA means grounded: clean parse + marker found structurally.
	GradeA DimensionGrade = "A"
	// GradeB means declared-shallow: clean parse + N/A sentinel only.
	GradeB DimensionGrade = "B"
	// GradeC means ungrounded: no grammar, parse failure, or no marker.
	GradeC DimensionGrade = "C"
)

// Dimensions is the fixed coverage-dimension order used by reports.
var Dimensions = []string{
	"signatures",
	"doc-comments",
	"call-edges",
	"errors",
	"concurrency",
	"config",
	"tests",
}

// LanguageReport is the grading result for one language fixture.
type LanguageReport struct {
	Language string
	Grades   map[string]DimensionGrade
	// HasGrammar is false when no tree-sitter grammar module is wired
	// for the language (kotlin/swift/scala): every grade is then C.
	HasGrammar bool
	// ParseOK is true only when the fixture produced a non-nil tree
	// with no ERROR nodes.
	ParseOK bool
}

// rank orders grades for regression comparison: C < B < A.
func rank(g DimensionGrade) int {
	switch g {
	case GradeA:
		return 2
	case GradeB:
		return 1
	default:
		return 0
	}
}

// AtLeast reports whether g meets the minimum grade floor.
func AtLeast(g, floor DimensionGrade) bool {
	return rank(g) >= rank(floor)
}

// langEntry is the per-language grading configuration. Declaration and
// call node-kind lists mirror the GAST ingest.Registry tables in
// internal/code_analysis_engine/ingest/languages.go; marker patterns are
// idiomatic source-text evidence for the heuristic dimensions (gated on a
// clean parse, per the rubric).
type langEntry struct {
	id       string
	ext      string
	newLang  func() unsafe.Pointer // nil => no grammar module wired
	decls    map[string]bool
	calls    map[string]bool
	errPat   string
	concPat  string
	confPat  string
	testPat  string
	pairsCfg bool // json: the document itself is configuration data
}

func set(kinds ...string) map[string]bool {
	m := make(map[string]bool, len(kinds))
	for _, k := range kinds {
		m[k] = true
	}
	return m
}

// table lists the canonical GAST languages in registry-declaration order
// (internal/code_analysis_engine/ingest/type.go), each with its fixture
// extension, grammar factory, declaration/call node kinds, and idiomatic
// marker patterns for the heuristic dimensions. Empty pattern means the
// language has no idiomatic construct (honest C unless a sentinel applies).
var table = []langEntry{
	{
		id: "go", ext: "go",
		newLang: tree_sitter_go.Language,
		decls: set("function_declaration", "method_declaration", "type_spec", "function_type",
			"field_declaration", "method_elem", "parameter_declaration", "type_elem"),
		calls:   set("call_expression"),
		errPat:  `errors\.New|fmt\.Errorf|panic\(|\bErr\w+`,
		concPat: `\bgo func|go \w+\(|sync\.|chan |Mutex|WaitGroup`,
		confPat: `os\.Getenv|os\.LookupEnv`,
		testPat: `func Test[A-Z]\w*\(|_test\.go`,
	},
	{
		id: "java", ext: "java",
		newLang: tree_sitter_java.Language,
		decls: set("class_declaration", "interface_declaration", "enum_declaration",
			"method_declaration", "constructor_declaration", "record_declaration",
			"annotation_type_declaration", "field_declaration", "formal_parameter"),
		calls:   set("method_invocation", "object_creation_expression"),
		errPat:  `\bthrow\b|throws\b|\bcatch\b|Exception`,
		concPat: `new Thread|Executors|ExecutorService|synchronized|CompletableFuture|Future<`,
		confPat: `System\.getenv|System\.getProperty|Properties|@Value`,
		testPat: `@Test|@ParameterizedTest|\bassert\w*\(|assert `,
	},
	{
		id: "python", ext: "py",
		newLang: tree_sitter_python.Language,
		decls:   set("function_definition", "class_definition", "decorated_definition", "typed_parameter"),
		calls:   set("call"),
		errPat:  `\braise\b`,
		concPat: `asyncio|async def|\bawait\b|threading|Thread\(|concurrent\.futures`,
		confPat: `os\.environ|os\.getenv|configparser|dotenv|argparse`,
		testPat: `def test_\w*|unittest|pytest|assert `,
	},
	{
		id: "javascript", ext: "js",
		newLang: tree_sitter_javascript.Language,
		decls: set("function_declaration", "function_expression", "generator_function_declaration",
			"method_definition", "class_declaration", "lexical_declaration",
			"variable_declaration", "property_definition", "field_definition"),
		calls:   set("call_expression", "new_expression"),
		errPat:  `\bthrow\b|\bcatch\b|new Error`,
		concPat: `\basync\b|\bawait\b|Promise|Worker`,
		confPat: `process\.env|import\.meta|localStorage`,
		testPat: `\btest\(|\bit\(|describe\(|function test|assert`,
	},
	{
		id: "typescript", ext: "ts",
		newLang: tree_sitter_typescript.LanguageTypescript,
		decls: set("function_declaration", "method_definition", "class_declaration",
			"interface_declaration", "type_alias_declaration", "enum_declaration",
			"lexical_declaration", "abstract_class_declaration", "property_signature",
			"method_signature", "heritage_clause"),
		calls:   set("call_expression", "new_expression"),
		errPat:  `\bthrow\b|\bcatch\b|new Error`,
		concPat: `\basync\b|\bawait\b|Promise|Worker`,
		confPat: `process\.env|Bun\.env|Deno\.env`,
		testPat: `\btest\(|\bit\(|describe\(|function test|assert`,
	},
	{
		id: "cpp", ext: "cpp",
		newLang: tree_sitter_cpp.Language,
		decls: set("function_definition", "class_specifier", "struct_specifier",
			"union_specifier", "namespace_definition", "template_declaration",
			"declaration", "field_declaration", "parameter_declaration",
			"base_class_clause", "base_class_specifier"),
		calls:   set("call_expression"),
		errPat:  `\bthrow\b|\bcatch\b|runtime_error|logic_error|std::cerr`,
		concPat: `std::thread|std::mutex|std::async|std::jthread|co_await|std::lock_guard`,
		confPat: `std::getenv|getenv\(`,
		testPat: `void test_|TEST\(|TEST_F\(|assert|CHECK\(|REQUIRE\(`,
	},
	{
		id: "c", ext: "c",
		newLang: tree_sitter_c.Language,
		decls: set("function_definition", "struct_specifier", "union_specifier",
			"enum_specifier", "declaration", "field_declaration", "parameter_declaration"),
		calls:   set("call_expression"),
		errPat:  `perror|fprintf\(stderr|return -1|errno|exit\(|assert\(|abort\(`,
		concPat: `pthread_|thrd_|mtx_|atomic_|omp |_Atomic`,
		confPat: `getenv\(`,
		testPat: `void test_|int test_|assert\(`,
	},
	{
		id: "csharp", ext: "cs",
		newLang: tree_sitter_csharp.Language,
		decls: set("class_declaration", "interface_declaration", "struct_declaration",
			"enum_declaration", "record_declaration", "method_declaration",
			"namespace_declaration", "field_declaration", "base_list", "parameter"),
		calls:   set("invocation_expression", "object_creation_expression"),
		errPat:  `\bthrow\b|\bcatch\b|Exception`,
		concPat: `\basync\b|\bawait\b|Task<|Task\.|new Thread|lock\s*\(`,
		confPat: `GetEnvironmentVariable|IConfiguration|Configuration\[`,
		testPat: `\[Test\]|\[Fact\]|\[TestMethod\]|Assert\.`,
	},
	{
		id: "rust", ext: "rs",
		newLang: tree_sitter_rust.Language,
		decls: set("function_item", "impl_item", "struct_item", "enum_item",
			"trait_item", "type_item", "mod_item", "static_item", "const_item",
			"field_declaration", "where_clause"),
		calls:   set("call_expression", "macro_invocation"),
		errPat:  `panic!|Err\(|Result<|\.unwrap\(|\.expect\(|bail!`,
		concPat: `thread::|tokio::|async fn|\bawait\b|Mutex|Arc::|spawn`,
		confPat: `env::var|dotenv|config::`,
		testPat: `#\[test\]|#\[cfg\(test\)\]|assert`,
	},
	{
		id: "ruby", ext: "rb",
		newLang: tree_sitter_ruby.Language,
		decls:   set("function", "method", "class", "module", "singleton_method", "singleton_class", "ivar", "module_function"),
		calls:   set("call", "method_call"),
		errPat:  `\braise\b|\brescue\b`,
		concPat: `Thread\.new|Mutex|Fiber|Ractor`,
		confPat: `ENV\[|ENV\.fetch`,
		testPat: `def test_|RSpec|describe |assert_equal|assert `,
	},
	{
		id: "php", ext: "php",
		newLang: tree_sitter_php.LanguagePHP,
		decls: set("function_definition", "method_declaration", "class_declaration",
			"interface_declaration", "trait_declaration", "enum_declaration",
			"property_declaration", "formal_parameters", "extends_clause", "implements_clause"),
		calls: set("function_call_expression", "member_call_expression",
			"scoped_call_expression", "object_creation_expression"),
		errPat:  `\bthrow\b|\bcatch\b|Exception|Throwable`,
		concPat: `Fiber|parallel\\|Amp\\|React\\`,
		confPat: `getenv\(|\$_ENV|\$_SERVER`,
		testPat: `function test|->assert|PHPUnit|assert\(`,
	},
	{
		id: "kotlin", ext: "kt",
		newLang: nil, // no grammar module in go.mod: declaration-only, skipped gracefully
		decls:   set("class_declaration", "function_declaration", "object_declaration"),
		calls:   set(),
		errPat:  `\bthrow\b|\btry\b|\bcatch\b|Exception|require\(|check\(`,
		concPat: `launch\s*\{|async\s*\{|coroutine|Thread\(|Executors`,
		confPat: `System\.getenv|getenv`,
		testPat: `@Test|fun test|assertEquals|assert\(`,
	},
	{
		id: "swift", ext: "swift",
		newLang: nil, // no grammar module in go.mod: declaration-only, skipped gracefully
		decls:   set("class_declaration", "function_declaration", "protocol_declaration"),
		calls:   set(),
		errPat:  `\bthrow\b|\btry\b|\bcatch\b|Error|guard .* else`,
		concPat: `\bawait\b|async func|Task\s*\{|DispatchQueue|\bactor\b`,
		confPat: `processInfo\.environment|getenv`,
		testPat: `func test|XCTAssert|assert\(`,
	},
	{
		id: "scala", ext: "scala",
		newLang: nil, // no grammar module in go.mod: declaration-only, skipped gracefully
		decls:   set("class_definition", "function_definition", "object_definition"),
		calls:   set(),
		errPat:  `\bthrow\b|\btry\b|\bcatch\b|Failure|Left\(|require\(`,
		concPat: `Future\s*\{|Future\.|Actor|ExecutionContext|Await\.`,
		confPat: `sys\.env|sys\.props|ConfigFactory`,
		testPat: `test\(|assert\(|AnyFunSuite|AnyFlatSpec|\bshould\b`,
	},
	{
		id: "css", ext: "css",
		newLang: tree_sitter_css.Language,
		decls: set("rule_set", "media_statement", "keyframes_statement",
			"keyframe_rule_set", "block"),
		calls: set("call_expression"),
		// CSS has no error construct and no env/config reads: honest C
		// on errors, B-via-sentinel on concurrency/config/tests.
		errPat:  ``,
		concPat: ``,
		confPat: ``,
		testPat: ``,
	},
	{
		id: "html", ext: "html",
		newLang: tree_sitter_html.Language,
		decls:   set("doctype", "script_element", "style_element", "element"),
		calls:   set(), // the GAST registry wires no call kinds for HTML
		errPat:  ``,
		concPat: ``,
		confPat: ``,
		testPat: ``,
	},
	{
		id: "json", ext: "json",
		newLang:  tree_sitter_json.Language,
		decls:    set("pair", "object", "array"),
		calls:    set(), // the GAST registry wires no call kinds for JSON
		errPat:   ``,
		concPat:  ``,
		confPat:  ``, // config is structural here (pairsCfg): the doc IS config
		testPat:  ``,
		pairsCfg: true,
	},
}

// Languages returns the canonical GAST language IDs in table order.
func Languages() []string {
	out := make([]string, 0, len(table))
	for _, e := range table {
		out = append(out, e.id)
	}
	return out
}

// FixturePath returns the embedded fixture path for a language ID.
func FixturePath(langID string) string {
	for _, e := range table {
		if e.id == langID {
			return "testdata/" + e.id + "/sample." + e.ext
		}
	}
	return ""
}

func entryByID(langID string) *langEntry {
	for i := range table {
		if table[i].id == langID {
			return &table[i]
		}
	}
	return nil
}

// N/A sentinels: fixture-declared absence of an idiomatic construct.
// Any comment style counts (line/block/HTML comments); JSON cannot carry
// them, which is why its inapplicable dimensions stay C by design.
const (
	sentinelConcurrency = "NOCONCURRENCY"
	sentinelConfig      = "NOCONFIG"
	sentinelTests       = "NOTEST"
)

func blankReport(langID string) LanguageReport {
	rep := LanguageReport{Language: langID, Grades: make(map[string]DimensionGrade, len(Dimensions))}
	for _, d := range Dimensions {
		rep.Grades[d] = GradeC
	}
	return rep
}

// patCache memoizes compiled marker patterns (patterns are fixed at
// build time; MustCompile panics loudly on a bad one instead of grading
// everything C).
var patCache = make(map[string]*regexp.Regexp)

func regexpMatch(pat, text string) bool {
	re, ok := patCache[pat]
	if !ok {
		re = regexp.MustCompile(pat)
		patCache[pat] = re
	}
	return re.MatchString(text)
}

// errKindFrags are structural (node-kind substring) corroboration for the
// errors dimension; the language-specific errPat text marker is the other
// half. Either one, on a clean parse, earns A.
var errKindFrags = []string{"throw", "raise", "catch", "except", "rescue", "panic", "assert"}

type span struct {
	startRow uint
	endRow   uint
	start    uint
	end      uint
}

// GradeFixture grades one language's source bytes without the doc engine:
// tree-sitter parse with the language's grammar plus node-type walks and
// parse-gated marker heuristics, per the package rubric.
func GradeFixture(langID string, source []byte) LanguageReport {
	rep := blankReport(langID)
	e := entryByID(langID)
	if e == nil || e.newLang == nil {
		return rep // unknown language, or no grammar wired: all C
	}
	rep.HasGrammar = true

	parser := sitter.NewParser()
	defer parser.Close()
	sitterLang := sitter.NewLanguage(e.newLang())
	if sitterLang == nil {
		return rep
	}
	if err := parser.SetLanguage(sitterLang); err != nil {
		return rep
	}
	tree := parser.Parse(source, nil)
	if tree == nil {
		return rep
	}
	defer tree.Close()
	root := tree.RootNode()
	if root == nil || root.HasError() {
		return rep // fault-tolerant grammars still flag real syntax errors
	}
	rep.ParseOK = true

	kindCount := make(map[string]int)
	var comments []span
	var decls []span
	isPython := langID == "python"
	pythonDoc := false

	// Iterative cursor walk (no Go recursion): classify every node once.
	cursor := root.Walk()
	defer cursor.Close()
	for {
		node := cursor.Node()
		if node == nil {
			break
		}
		kind := node.Kind()
		kindCount[kind]++
		lower := strings.ToLower(kind)
		if strings.Contains(lower, "comment") {
			sp := node.StartPosition()
			ep := node.EndPosition()
			comments = append(comments, span{sp.Row, ep.Row, node.StartByte(), node.EndByte()})
		}
		if e.decls[kind] {
			sp := node.StartPosition()
			ep := node.EndPosition()
			decls = append(decls, span{sp.Row, ep.Row, node.StartByte(), node.EndByte()})
			if isPython && hasLeadingDocstring(node, source) {
				pythonDoc = true
			}
		}

		if cursor.GotoFirstChild() {
			continue
		}
		for {
			if cursor.GotoNextSibling() {
				break
			}
			if !cursor.GotoParent() {
				goto walked
			}
		}
	}
walked:

	hasDecl := len(decls) > 0
	hasCall := false
	for kind, n := range kindCount {
		if n > 0 && e.calls[kind] {
			hasCall = true
			break
		}
	}
	hasDoc := pythonDoc || adjacentComment(comments, decls)

	text := string(source)
	hasErrNode := false
	for kind := range kindCount {
		lower := strings.ToLower(kind)
		for _, frag := range errKindFrags {
			if strings.Contains(lower, frag) {
				hasErrNode = true
				break
			}
		}
		if hasErrNode {
			break
		}
	}
	hasErrText := e.errPat != "" && regexpMatch(e.errPat, text)
	hasConcText := e.concPat != "" && regexpMatch(e.concPat, text)
	hasConfText := e.confPat != "" && regexpMatch(e.confPat, text)
	hasTestText := e.testPat != "" && regexpMatch(e.testPat, text)

	if hasDecl {
		rep.Grades["signatures"] = GradeA
	}
	if hasDoc {
		rep.Grades["doc-comments"] = GradeA
	}
	if hasCall {
		rep.Grades["call-edges"] = GradeA
	}
	if hasErrNode || hasErrText {
		rep.Grades["errors"] = GradeA
	}
	gradeHeuristic := func(textHit bool, sentinel string) DimensionGrade {
		switch {
		case textHit:
			return GradeA
		case strings.Contains(text, sentinel):
			return GradeB
		default:
			return GradeC
		}
	}
	rep.Grades["concurrency"] = gradeHeuristic(hasConcText, sentinelConcurrency)
	rep.Grades["tests"] = gradeHeuristic(hasTestText, sentinelTests)
	if e.pairsCfg {
		if kindCount["pair"] > 0 {
			rep.Grades["config"] = GradeA
		}
	} else {
		rep.Grades["config"] = gradeHeuristic(hasConfText, sentinelConfig)
	}
	return rep
}

// adjacentComment reports whether any comment ends at most two rows above
// any declaration start (and ends before the declaration begins). The row
// window tolerates attribute/annotation lines between doc comment and
// declaration; the byte check rejects trailing same-line comments.
func adjacentComment(comments, decls []span) bool {
	for _, d := range decls {
		for _, c := range comments {
			if c.end <= d.start && c.endRow <= d.startRow && d.startRow-c.endRow <= 2 {
				return true
			}
		}
	}
	return false
}

// hasLeadingDocstring reports whether a Python function/class body opens
// with a string literal (the idiomatic docstring position).
func hasLeadingDocstring(node *sitter.Node, source []byte) bool {
	kind := node.Kind()
	if kind != "function_definition" && kind != "class_definition" {
		return false
	}
	body := node.ChildByFieldName("body")
	if body == nil {
		return false
	}
	for i := uint(0); i < body.NamedChildCount(); i++ {
		first := body.NamedChild(i)
		if first == nil {
			continue
		}
		if first.Kind() != "expression_statement" {
			return false // body opens with real code, not a docstring
		}
		for j := uint(0); j < first.NamedChildCount(); j++ {
			if kid := first.NamedChild(j); kid != nil && kid.Kind() == "string" {
				_ = source
				return true
			}
		}
		return false
	}
	return false
}

// GradeAll grades every embedded fixture and returns the reports keyed by
// language ID. A missing/unreadable fixture yields an all-C report so the
// regression gate fails loudly instead of silently skipping a language.
func GradeAll() map[string]LanguageReport {
	out := make(map[string]LanguageReport, len(table))
	for _, e := range table {
		data, err := fixtures.ReadFile(FixturePath(e.id))
		if err != nil {
			out[e.id] = blankReport(e.id)
			continue
		}
		out[e.id] = GradeFixture(e.id, data)
	}
	return out
}

// MarkdownReport renders the matrix as a GitHub-flavoured table for future
// docs/CI use. Rows follow Languages() order; languages missing from the
// input map render as "?" so absent data can never pass as grounded.
func MarkdownReport(reports map[string]LanguageReport) string {
	var b strings.Builder
	b.WriteString("| language |")
	for _, d := range Dimensions {
		b.WriteString(" " + d + " |")
	}
	b.WriteString("\n|---|")
	for range Dimensions {
		b.WriteString("---|")
	}
	for _, lang := range Languages() {
		b.WriteString("\n| " + lang + " |")
		rep, ok := reports[lang]
		for _, d := range Dimensions {
			cell := "?"
			if ok {
				if g, found := rep.Grades[d]; found {
					cell = string(g)
				} else {
					cell = string(GradeC)
				}
			}
			b.WriteString(" " + cell + " |")
		}
	}
	b.WriteString("\n")
	return b.String()
}
