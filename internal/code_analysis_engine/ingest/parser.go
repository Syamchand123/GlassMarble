package ingest

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"

	sitter "github.com/tree-sitter/go-tree-sitter"
)

// parser is a per-goroutine tree-sitter binding. Each Ingestion worker owns
// exactly one of these because the upstream Parser object is not safe for
// concurrent re-use.
type parser struct {
	mu     sync.Mutex
	parser *sitter.Parser
}

var (
	globalLangCache sync.Map // map[SupportedLang]*sitter.Language
)

func newParser() *parser {
	return &parser{
		parser: sitter.NewParser(),
	}
}

func (p *parser) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.parser != nil {
		p.parser.Close()
		p.parser = nil
	}
}

// bind loads the language binding for this worker. By pulling from a global
// cache, workers share the *sitter.Language singletons.
func (p *parser) bind(spec *LanguageSpec) (*sitter.Language, error) {
	if p.parser == nil {
		return nil, errors.New("ingest: parser already closed")
	}

	var lang *sitter.Language
	if cached, ok := globalLangCache.Load(spec.Lang); ok {
		lang = cached.(*sitter.Language)
	} else {
		lang = spec.NewLanguage()
		globalLangCache.Store(spec.Lang, lang)
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.parser.SetLanguage(lang); err != nil {
		return nil, fmt.Errorf("ingest: set language %s: %w", spec.Lang, err)
	}
	return lang, nil
}

// processFile is the per-task entry point: read source, parse it through
// tree-sitter, extract a RichToken slice, and free the C-allocated tree
// before returning. Tree-sitter is fault-tolerant so the worst case for a
// malformed file is HasErrors=true, never a panic.
func processFile(p *parser, task FileTask, spec *LanguageSpec) *IngestionResult {
	res := &IngestionResult{
		FilePath: task.FilePath,
		RelPath:  task.RelPath,
		Language: task.Language,
		Change:   task.Change,
		Commit:   task.Commit,
		Author:   task.Author,
		Time:     task.Time,
	}

	src, readErr := os.ReadFile(task.FilePath)
	if readErr != nil {
		res.Error = fmt.Errorf("ingest: read %s: %w", task.FilePath, readErr)
		return res
	}
	res.Bytes = len(src)

	if _, err := p.bind(spec); err != nil {
		res.Error = err
		return res
	}

	// We avoid ParseCtx because go-tree-sitter@v0.25.0 has a known race condition
	// in its internal context cancellation goroutine that causes a segfault.
	// We rely on the MaxFileBytes filter during discovery to prevent infinite parsing.
	tree := p.parser.Parse(src, nil)
	if tree == nil {
		res.Error = errors.New("ingest: parser returned nil tree")
		return res
	}
	defer tree.Close()

	tokens, hasError := extractTokens(tree.RootNode(), src, spec)
	res.RichTokens = tokens
	res.HasErrors = hasError
	return res
}

// extractTokens walks the tree-sitter CST once, classifying each node into
// the universal declaration / import / call taxonomy. The walker is iterative
// (no Go recursion) so even huge trees do not blow the goroutine stack.
func extractTokens(root *sitter.Node, source []byte, spec *LanguageSpec) ([]RichToken, bool) {
	if root == nil {
		return nil, false
	}
	tokens := make([]RichToken, 0, 64)
	var hasError bool

	cursor := root.Walk()
	defer cursor.Close()

	// parentStack tracks the active token index at each depth level (-1 if no parent token)
	parentStack := []int{-1}

	for {
		node := cursor.Node()
		if node == nil {
			break
		}

		if node.HasError() {
			hasError = true
		}

		currentParent := parentStack[len(parentStack)-1]
		depth := len(parentStack) - 1

		var createdTokenIdx = -1
		if kind := classifyKind(node.Kind(), spec); kind != "" {
			createdTokenIdx = len(tokens)
			tok := makeToken(node, source, kind, currentParent, depth)
			if kind == TokenDeclaration {
				enrichDeclaration(&tok, node, source, spec)
			} else if kind == TokenImport && spec.Lang == LangRuby && isRubyIncludeCall(node, source) {
				// Ruby mixins (`include Foo`, `prepend Bar`) classify as
				// imports; flag them so phase 2 can model the embedding
				// relationship (GAP-L-05 / §5.1.2).
				tok.IsEmbedded = true
			}
			tokens = append(tokens, tok)
		}

		if cursor.GotoFirstChild() {
			if createdTokenIdx != -1 {
				parentStack = append(parentStack, createdTokenIdx)
			} else {
				parentStack = append(parentStack, currentParent)
			}
			continue
		}

		for {
			if cursor.GotoNextSibling() {
				break
			}
			if !cursor.GotoParent() {
				return tokens, hasError
			}
			if len(parentStack) > 1 {
				parentStack = parentStack[:len(parentStack)-1]
			}
		}
	}
	return tokens, hasError
}

// classifyKind maps a tree-sitter node kind string to the universal RichToken
// taxonomy. Imports take precedence over declarations because some
// grammars (Python, Java) declare both module and import nodes as
// "declarations" internally.
func classifyKind(kind string, spec *LanguageSpec) TokenKind {
	for _, k := range spec.Imports {
		if kind == k {
			return TokenImport
		}
	}
	for _, k := range spec.Calls {
		if kind == k {
			return TokenCall
		}
	}
	for _, k := range spec.Declarations {
		if kind == k {
			return TokenDeclaration
		}
	}
	// Check for control-flow / structural node kinds using exact matches only
	switch kind {
	case "if_statement", "if", "for_statement", "for",
		"while_statement", "while", "do_statement", "do",
		"switch_statement", "switch", "return_statement", "return",
		"try_statement", "try", "catch_clause", "catch",
		"defer", "go", "go_statement",
		"throw_statement", "throw", "raise", "for_each",
		"for_in_statement", "for_of_statement", "foreach":
		return TokenDeclaration
	}
	return ""
}

// makeToken snapshots a node's structural fingerprint into a RichToken.
// EndByte / EndLine are clamped to source bounds so downstream phases can
// slice the source buffer without bounds-checking.
func makeToken(node *sitter.Node, source []byte, kind TokenKind, parentIdx int, depth int) RichToken {
	srcLen := uint(len(source))
	start := node.StartByte()
	end := node.EndByte()
	if start > srcLen {
		start = srcLen
	}
	if end > srcLen {
		end = srcLen
	}
	if start > end {
		start = end
	}
	startPt := node.StartPosition()
	endPt := node.EndPosition()

	contentSlice := source[start:end]
	const maxSnippetBytes = 2048
	if len(contentSlice) > maxSnippetBytes {
		contentSlice = contentSlice[:maxSnippetBytes]
	}
	content := string(contentSlice)
	if parent := node.Parent(); parent != nil {
		pKind := parent.Kind()
		if pKind == "export_statement" || pKind == "export_declaration" {
			content = "export " + content
		}
	}

	return RichToken{
		Kind:       kind,
		Type:       node.Kind(),
		Content:    content,
		Name:       nodeName(node, source),
		DocComment: extractDocComment(node, source),
		ParentIdx:  parentIdx,
		Depth:      depth,
		StartLine:  uint32(startPt.Row),
		EndLine:    uint32(endPt.Row),
		StartByte:  uint32(start),
		EndByte:    uint32(end),
		HasError:   node.HasError(),
	}
}

// enrichDeclaration fills the RichToken superset fields for declaration
// nodes: tree-sitter field roles (field name → child text), declaration-
// relevant named children, annotation text, and the IsFieldDecl /
// IsMethodSpec / IsEmbedded flags (master_overhaul_plan.md §5.1.1). Normalization
// translators can then read members, parameters, receivers, base types and
// annotations structurally instead of content-regex parsing (fixes A-07,
// A-08, A-18 at the source).
func enrichDeclaration(tok *RichToken, node *sitter.Node, source []byte, spec *LanguageSpec) {
	kind := node.Kind()
	tok.IsFieldDecl = spec.isFieldDeclKind(kind)
	// Java/C# model interface methods as plain method_declaration nodes
	// nested under interface_body, so interface membership cannot be read
	// from the kind alone — the ancestor refinement below covers it
	// (GAP-L-05 / master_overhaul_plan.md §5.1.2).
	tok.IsMethodSpec = spec.isMethodSpecKind(kind) || isInsideInterfaceBody(node)
	tok.IsEmbedded = spec.isEmbeddedKind(kind) ||
		// Go (tree-sitter-go@v0.25.0): anonymous embedding is a
		// field_declaration without a name field. Go-only: other grammars
		// (e.g. Java) also lack a direct name child on field_declaration
		// because the name lives in a variable_declarator.
		(spec.Lang == LangGo && kind == "field_declaration" && node.ChildByFieldName("name") == nil)

	cursor := node.Walk()
	defer cursor.Close()
	children := node.NamedChildren(cursor)

	var annotations []string
	for i, child := range children {
		field := node.FieldNameForNamedChild(uint32(i))
		text := strings.TrimSpace(child.Utf8Text(source))
		if field != "" && text != "" {
			if tok.FieldRoles == nil {
				tok.FieldRoles = make(map[string]string)
			}
			tok.FieldRoles[field] = text
		}
		childKind := child.Kind()
		if isAnnotationKind(childKind) || strings.HasPrefix(text, "@") || strings.HasPrefix(text, "#[") {
			annotations = append(annotations, text)
		}
		if classifyKind(childKind, spec) != "" {
			c := makeToken(&child, source, classifyKind(childKind, spec), -1, -1)
			tok.NamedChildren = append(tok.NamedChildren, &c)
		}
	}
	if len(annotations) > 0 {
		if tok.FieldRoles == nil {
			tok.FieldRoles = make(map[string]string)
		}
		tok.FieldRoles["annotations"] = strings.Join(annotations, " ")
	}
}

// isInsideInterfaceBody reports whether a declaration node sits inside an
// interface body. Java and C# model interface methods as plain
// method_declaration nodes nested under interface_body — the kind alone
// cannot distinguish them from class methods, so the ancestry decides
// (GAP-L-05). Concrete class bodies short-circuit the walk so a class
// implementing an interface never flags its methods as specs.
func isInsideInterfaceBody(node *sitter.Node) bool {
	for p := node.Parent(); p != nil; p = p.Parent() {
		switch p.Kind() {
		case "interface_body", "interface_declaration", "interface_type":
			return true
		case "class_body", "struct_body", "enum_body",
			"function_body", "block", "declaration_list":
			return false
		}
	}
	return false
}

// isRubyIncludeCall reports whether a Ruby call node is a mixin statement
// (`include Foo`, `prepend Bar`). tree-sitter-ruby models both as call
// nodes whose method field is the keyword, so kind lists cannot express
// them (GAP-L-05 / master_overhaul_plan.md §5.1.2).
func isRubyIncludeCall(node *sitter.Node, source []byte) bool {
	m := node.ChildByFieldName("method")
	if m == nil {
		return false
	}
	switch strings.TrimSpace(m.Utf8Text(source)) {
	case "include", "prepend":
		return true
	}
	return false
}

// isAnnotationKind matches annotation-bearing child node kinds across the
// supported grammars (Python decorators, Java/C# attributes, etc.).
func isAnnotationKind(kind string) bool {
	switch kind {
	case "attribute_decorator", "decorator", "annotation", "annotation_declaration",
		"attribute", "attribute_list", "decorator_list":
		return true
	}
	return false
}

// nodeName pulls a human-readable identifier (function name, type name,
// callee function) out of common field-name conventions used across
// tree-sitter grammars. Returns "" when no recognisable name field exists.
func nodeName(node *sitter.Node, source []byte) string {
	for _, field := range []string{
		"name", "function", "declarator", "type", "module", "path",
		"method", "class", "struct", "interface", "enum", "trait",
		"alias", "variable", "property", "attribute", "tag",
		"operator", "selector", "namespace", "package",
	} {
		if n := node.ChildByFieldName(field); n != nil {
			if text := resolveIdentifierText(n, source); text != "" {
				return text
			}
		}
	}

	for i := uint(0); i < node.NamedChildCount(); i++ {
		child := node.NamedChild(i)
		if child == nil {
			continue
		}
		kind := child.Kind()
		if isIdentifierKind(kind) {
			return child.Utf8Text(source)
		}
	}

	text := node.Utf8Text(source)
	if len(text) > 64 || strings.ContainsAny(text, "\n\r{};()") {
		return ""
	}
	return text
}

func isIdentifierKind(kind string) bool {
	switch kind {
	case "name", "field_identifier", "type_identifier",
		"identifier", "property_identifier", "shorthand_property_identifier",
		"shorthand_property_identifier_pattern", "variable_name",
		"class_name", "method_name", "function_name",
		"namespace_name", "module_name", "enum_name",
		"type_name", "alias_name", "attribute_name",
		"tag_name", "label_name", "selector":
		return true
	}
	return false
}

func resolveIdentifierText(n *sitter.Node, source []byte) string {
	if n == nil {
		return ""
	}
	kind := n.Kind()
	if isIdentifierKind(kind) {
		return n.Utf8Text(source)
	}
	// Recurse through common field names for nested declaration structures
	for _, field := range []string{"declarator", "name", "value", "type", "body"} {
		if child := n.ChildByFieldName(field); child != nil {
			if text := resolveIdentifierText(child, source); text != "" {
				return text
			}
		}
	}
	for i := uint(0); i < n.NamedChildCount(); i++ {
		child := n.NamedChild(i)
		if child != nil && isIdentifierKind(child.Kind()) {
			return child.Utf8Text(source)
		}
	}
	return n.Utf8Text(source)
}

// extractDocComment extracts preceding comment nodes (such as docstrings or
// inline comments) attached directly above a structural declaration.
func extractDocComment(node *sitter.Node, source []byte) string {
	var comments []string
	curr := node.PrevSibling()
	for curr != nil {
		kind := curr.Kind()
		if isCommentKind(kind) {
			comments = append([]string{curr.Utf8Text(source)}, comments...)
			curr = curr.PrevSibling()
		} else if isBlankOrWhitespaceNode(curr, source) {
			curr = curr.PrevSibling()
		} else {
			break
		}
	}
	if len(comments) == 0 {
		return ""
	}
	return strings.Join(comments, "\n")
}

func isCommentKind(kind string) bool {
	switch kind {
	case "comment", "line_comment", "block_comment", "multiline_comment", "doc_comment", "documentation_comment":
		return true
	default:
		return false
	}
}

func isBlankOrWhitespaceNode(node *sitter.Node, source []byte) bool {
	if node == nil {
		return true
	}
	start := node.StartByte()
	end := node.EndByte()
	if start >= uint(len(source)) || end > uint(len(source)) || start >= end {
		return true
	}
	txt := strings.TrimSpace(string(source[start:end]))
	return txt == ""
}
