package stage2

import (
	"regexp"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage1"
)

var rustImplRegex = regexp.MustCompile(`impl(?:\s*<[^>]+>)?\s+(?:(\w+)\s+for\s+)?(\w+)`)

type RustTranslator struct{}

func (t *RustTranslator) CoerceToken(tok stage1.RawToken, fileRelPath string) *GASTNode {
	node := baseNode(tok, fileRelPath)
	extractGenericTypesAndDecorators(node, tok.Content)
	switch tok.Kind {
	case stage1.TokenImport:
		node.Type = GASTImport
		node.Kind = "use"
		node.Name = cleanImportPath(tok.Content)
	case stage1.TokenDeclaration:
		switch tok.Type {
		case "mod_item":
			node.Type = GASTNamespace
			node.Kind = "mod"
		case "struct_item":
			node.Type = GASTTypeDeclaration
			node.Kind = "struct"
		case "enum_item":
			node.Type = GASTTypeDeclaration
			node.Kind = "enum"
		case "trait_item":
			node.Type = GASTTypeDeclaration
			node.Kind = "trait"
		case "impl_item":
			node.Type = GASTTypeDeclaration
			node.Kind = "impl"
			if match := rustImplRegex.FindStringSubmatch(tok.Content); len(match) > 2 {
				node.ReceiverType = match[2]
				node.Properties["receiver_type"] = match[2]
			}
		case "field_declaration":
			node.Type = GASTField
			node.Kind = "field"
		case "parameter":
			node.Type = GASTParameter
			node.Kind = "parameter"
		default:
			node.Type = GASTFunction
			node.Kind = "function"
		}

		if strings.HasPrefix(tok.Content, "pub") {
			node.Visibility = "public"
		} else {
			node.Visibility = "internal"
		}
	case stage1.TokenCall:
		node.Type = GASTCallExpression
		node.Kind = "call"
	}
	return node
}
