package stage2

import (
	"regexp"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage1"
)

var goReceiverRegex = regexp.MustCompile(`func\s*\(\s*\w+\s+\*?(\w+)\s*\)`)

type GoTranslator struct{}

func (t *GoTranslator) CoerceToken(tok stage1.RawToken, fileRelPath string) *GASTNode {
	node := baseNode(tok, fileRelPath)
	extractGenericTypesAndDecorators(node, tok.Content)
	modName := moduleFromPath(fileRelPath)
	// Go uses "pkg" as the canonical module prefix for FQN
	pkgPrefix := "pkg"
	if modName != "module" {
		pkgPrefix = modName
	}

	switch tok.Kind {
	case stage1.TokenImport:
		node.Type = GASTImport
		node.Kind = "import"
		node.Name = cleanImportPath(tok.Content)

	case stage1.TokenDeclaration:
		switch tok.Type {
		case "package_clause":
			node.Type = GASTNamespace
			node.Kind = "namespace"
		case "function_declaration", "method_declaration":
			node.Type = GASTFunction
			node.Kind = "function"
			if tok.Type == "method_declaration" {
				node.Kind = "method"
				if match := goReceiverRegex.FindStringSubmatch(tok.Content); len(match) > 1 {
					node.ReceiverType = match[1]
					node.Properties["receiver_type"] = match[1]
					// FQN: pkg.Receiver.Method — fully qualified dot-notation
					node.ID = pkgPrefix + "." + match[1] + "." + tok.Name
					node.Name = node.ID
				}
			} else {
				// FQN: pkg.FunctionName
				node.ID = pkgPrefix + "." + tok.Name
				node.Name = node.ID
			}
			node.Visibility = resolveGoVisibility(tok.Name)
		case "field_declaration":
			node.Type = GASTField
			node.Kind = "field"
			node.Visibility = resolveGoVisibility(tok.Name)
		case "parameter_declaration":
			node.Type = GASTParameter
			node.Kind = "parameter"
			node.Visibility = "internal"
		case "type_declaration", "type_spec":
			node.Type = GASTTypeDeclaration
			node.Kind = "struct"
			if strings.Contains(tok.Content, "interface") {
				node.Kind = "interface"
			}
			node.Visibility = resolveGoVisibility(tok.Name)
			node.ID = pkgPrefix + "." + tok.Name
			node.Name = node.ID
		default:
			// Handle control flow statements
			switch tok.Type {
			case "if_statement", "if", "for_statement", "for",
				"switch_statement", "switch", "return_statement", "return",
				"defer", "go", "go_statement", "while_statement", "while",
				"do_statement", "do", "try_statement", "try", "catch_clause",
				"throw", "throw_statement", "for_each", "foreach":
				node.Type = GASTControlFlow
				node.Kind = tok.Type
				node.Visibility = "internal"
			default:
				node.Type = GASTTypeDeclaration
				node.Kind = tok.Type
				node.Visibility = resolveGoVisibility(tok.Name)
				node.ID = pkgPrefix + "." + tok.Name
				node.Name = node.ID
			}
		}

	case stage1.TokenCall:
		node.Type = GASTCallExpression
		node.Kind = "call"
	}
	return node
}

func resolveGoVisibility(name string) string {
	if name != "" && name[0] >= 'A' && name[0] <= 'Z' {
		return "public"
	}
	return "internal"
}
