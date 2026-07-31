package stage1

import (
	
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	

	sitter "github.com/tree-sitter/go-tree-sitter"
)

// parser is a per-goroutine tree-sitter binding. Each Stage 1 worker owns
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
		return nil, errors.New("stage1: parser already closed")
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
		return nil, fmt.Errorf("stage1: set language %s: %w", spec.Lang, err)
	}
	return lang, nil
}

// processFile is the per-task entry point: read source, parse it through
// tree-sitter, extract a RawToken slice, and free the C-allocated tree
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
		res.Error = fmt.Errorf("stage1: read %s: %w", task.FilePath, readErr)
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
		res.Error = errors.New("stage1: parser returned nil tree")
		return res
	}
	defer tree.Close()

	tokens, hasError := extractTokens(tree.RootNode(), src, spec)
	res.RawTokens = tokens
	res.HasErrors = hasError
	return res
}

// extractTokens walks the tree-sitter CST once, classifying each node into
// the universal declaration / import / call taxonomy. The walker is iterative
// (no Go recursion) so even huge trees do not blow the goroutine stack.
func extractTokens(root *sitter.Node, source []byte, spec *LanguageSpec) ([]RawToken, bool) {
	if root == nil {
		return nil, false
	}
	tokens := make([]RawToken, 0, 64)
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
			tokens = append(tokens, makeToken(node, source, kind, currentParent, depth))
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

// classifyKind maps a tree-sitter node kind string to the universal RawToken
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

// makeToken snapshots a node's structural fingerprint into a RawToken.
// EndByte / EndLine are clamped to source bounds so downstream stages can
// slice the source buffer without bounds-checking.
func makeToken(node *sitter.Node, source []byte, kind TokenKind, parentIdx int, depth int) RawToken {
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

	return RawToken{
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
