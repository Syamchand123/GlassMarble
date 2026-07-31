package stage2

import (
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage1"
)

type JavaTranslator struct{}

func (t *JavaTranslator) CoerceToken(tok stage1.RawToken, fileRelPath string) *GASTNode {
	node := baseNode(tok, fileRelPath)
	extractGenericTypesAndDecorators(node, tok.Content)

	switch tok.Kind {
	case stage1.TokenImport:
		node.Type = GASTImport
		node.Kind = "import"
		node.Name = cleanImportPath(tok.Content)
	case stage1.TokenDeclaration:
		switch tok.Type {
		case "package_declaration":
			node.Type = GASTNamespace
			node.Kind = "namespace"
		case "class_declaration":
			node.Type = GASTTypeDeclaration
			node.Kind = "class"
		case "interface_declaration":
			node.Type = GASTTypeDeclaration
			node.Kind = "interface"
		case "enum_declaration":
			node.Type = GASTTypeDeclaration
			node.Kind = "enum"
		case "record_declaration":
			node.Type = GASTTypeDeclaration
			node.Kind = "record"
		case "field_declaration":
			node.Type = GASTField
			node.Kind = "field"
		case "formal_parameter":
			node.Type = GASTParameter
			node.Kind = "parameter"
		case "constructor_declaration":
			node.Type = GASTFunction
			node.Kind = "constructor"
		default:
			node.Type = GASTFunction
			node.Kind = "method"
		}
		node.Visibility = resolveJavaVisibility(tok.Content)
		if tok.Type != "package_declaration" && tok.Type != "field_declaration" && tok.Type != "formal_parameter" {
			setDeclarationFQN(node, fileRelPath, tok.Name)
		}
	case stage1.TokenCall:
		node.Type = GASTCallExpression
		node.Kind = "call"
	}
	return node
}

func resolveJavaVisibility(content string) string {
	switch {
	case strings.Contains(content, "public"):
		return "public"
	case strings.Contains(content, "private"):
		return "private"
	case strings.Contains(content, "protected"):
		return "protected"
	default:
		return "internal"
	}
}
