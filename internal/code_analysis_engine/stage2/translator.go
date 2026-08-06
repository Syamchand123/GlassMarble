package stage2

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage1"
)

// Translator defines the interface for language-specific token coercion.
type Translator interface {
	CoerceToken(tok stage1.RichToken, parent *stage1.RichToken, fileRelPath string) *GASTNode
}

// Dispatcher returns the appropriate Translator for all 13 supported languages + generic fallback.
func Dispatcher(lang stage1.SupportedLang) Translator {
	switch lang {
	case stage1.LangGo:
		return &GoTranslator{}
	case stage1.LangC:
		return &CTranslator{}
	case stage1.LangCpp:
		return &CppTranslator{}
	case stage1.LangCSharp:
		return &CSharpTranslator{}
	case stage1.LangPython:
		return &PythonTranslator{}
	case stage1.LangJava:
		return &JavaTranslator{}
	case stage1.LangJS:
		return &JSTranslator{}
	case stage1.LangTS:
		return &TSTranslator{}
	case stage1.LangHTML:
		return &HTMLTranslator{}
	case stage1.LangCSS:
		return &CSSTranslator{}
	case stage1.LangJSON:
		return &JSONTranslator{}
	case stage1.LangRuby:
		return &RubyTranslator{}
	case stage1.LangRust:
		return &RustTranslator{}
	case stage1.LangPHP:
		return &PHPTranslator{}
	default:
		return &GenericTranslator{Lang: lang}
	}
}

func baseNode(tok stage1.RichToken, fileRelPath string) *GASTNode {
	name := tok.Name
	if name == "" {
		name = tok.Type
	}
	// moduleName is derived from the file path (used as FQN prefix)
	modName := moduleFromPath(fileRelPath)
	id := fmt.Sprintf("%s::%s::%s::L%d", fileRelPath, tok.Kind, name, tok.StartLine)
	prims := DetectBehavioralPrimitives(tok.Content, name)
	node := &GASTNode{
		ID:         id,
		Name:       name,
		Kind:       tok.Type,
		DocComment: tok.DocComment,
		Primitives: prims,
		StartLine:  tok.StartLine,
		EndLine:    tok.EndLine,
		StartByte:  tok.StartByte,
		EndByte:    tok.EndByte,
		Properties: map[string]string{
			"file_path":   fileRelPath,
			"module_name": modName,
			"content":     tok.Content,
		},
	}

	lowerType := strings.ToLower(tok.Type)
	switch lowerType {
	case "if_statement", "if", "for_statement", "for",
		"switch_statement", "switch", "while_statement", "while",
		"do_statement", "do", "for_each", "foreach":
		cond := strings.TrimSpace(tok.Content)
		// Remove leading keyword
		for _, kw := range []string{"if", "switch", "for", "while", "do", "foreach"} {
			if strings.HasPrefix(strings.ToLower(cond), kw) {
				cond = cond[len(kw):]
				break
			}
		}
		// Cut off at block delimiter
		if idx := strings.IndexAny(cond, "{:"); idx != -1 {
			cond = cond[:idx]
		}
		cond = strings.TrimSpace(cond)
		// If it is fully enclosed in parenthesis (e.g. C/Java/JS), strip them
		if strings.HasPrefix(cond, "(") && strings.HasSuffix(cond, ")") {
			cond = cond[1 : len(cond)-1]
		}
		node.Properties["condition"] = strings.TrimSpace(cond)
	}

	return node
}

// moduleFromPath derives a hierarchical module/package namespace from a file relative path.
// e.g. "internal/auth/service.go" -> "internal.auth.service"
func moduleFromPath(relPath string) string {
	clean := filepath.ToSlash(filepath.Clean(relPath))
	// Strip file extension
	if idx := strings.LastIndex(clean, "."); idx > 0 {
		clean = clean[:idx]
	}
	clean = strings.Trim(clean, "/")
	if clean == "" || clean == "." {
		return "main"
	}
	// Convert directory slashes to dot notation
	ns := strings.ReplaceAll(clean, "/", ".")
	ns = strings.ReplaceAll(ns, "-", "_")
	return ns
}

// setDeclarationFQN sets node.ID and node.Name to a fully-qualified dot-notation FQN
func setDeclarationFQN(node *GASTNode, fileRelPath, name string) {
	mod := moduleFromPath(fileRelPath)
	fqn := mod + "." + name
	node.ID = fqn
	node.Name = fqn
	node.Properties["fully_qualified_name"] = fqn
}

func cleanImportPath(raw string) string {
	raw = strings.TrimSpace(raw)
	// Remove language-specific import keywords and delimiters
	prefixes := []string{
		"import", "include", "#include", "from", "using",
		"require", "require_relative", "use ",
		"extern crate", "extern crate ",
		"import \"", "import '",
	}
	for _, p := range prefixes {
		if strings.HasPrefix(raw, p) {
			raw = raw[len(p):]
		}
	}
	// Strip surrounding quotes, angle brackets, and semicolons
	raw = strings.Trim(raw, " \t\r\n;\"'<>")
	// Normalize backslash to forward slash
	raw = filepath.ToSlash(raw)
	// Collapse whitespace
	raw = strings.Join(strings.Fields(raw), " ")
	return raw
}

var globalGenericsRegex = regexp.MustCompile(`<([^>]+)>|\[([^\]]+)\]`)
var globalAnnotationRegex = regexp.MustCompile(`(@[\w\.]+|\[[\w\.]+\]|#\[[\w\.]+\])`)

// extractGenericTypesAndDecorators extracts generic parameters, annotations/attributes, and async metadata across all languages.
func extractGenericTypesAndDecorators(node *GASTNode, content string) {
	if node == nil || content == "" {
		return
	}
	if node.Properties == nil {
		node.Properties = make(map[string]string)
	}

	// Extract Generics (<T>, [T])
	if matches := globalGenericsRegex.FindStringSubmatch(content); len(matches) > 1 {
		tParam := matches[1]
		if tParam == "" && len(matches) > 2 {
			tParam = matches[2]
		}
		node.Properties["type_params"] = tParam
	}

	// Extract Annotations / Decorators (@RestController, [HttpGet], #[Route])
	if annos := globalAnnotationRegex.FindAllString(content, -1); len(annos) > 0 {
		node.Annotations = annos
	}

	// Extract Async / Future markers
	lower := strings.ToLower(content)
	if strings.Contains(lower, "async ") || strings.Contains(lower, "promise") || strings.Contains(lower, "task<") || strings.Contains(lower, "future<") {
		node.Properties["is_async"] = "true"
	}
}
