package normalize

import (
	"regexp"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/ingest"
)

var rustImplRegex = regexp.MustCompile(`impl(?:\s*<[^>]+>)?\s+(?:(\w+)\s+for\s+)?(\w+)`)

type RustTranslator struct{}

func (t *RustTranslator) CoerceToken(tok ingest.RichToken, parent *ingest.RichToken, fileRelPath string) *GASTNode {
	node := baseNode(tok, fileRelPath)
	extractGenericTypesAndDecorators(node, tok.Content)
	switch tok.Kind {
	case ingest.TokenImport:
		node.Type = GASTImport
		node.Kind = "use"
		node.Name = cleanImportPath(tok.Content)
	case ingest.TokenDeclaration:
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
				if match[1] != "" {
					node.Implemented = append(node.Implemented, match[1])
				}
			}
		case "field_declaration":
			node.Type = GASTField
			node.Kind = "field"
		case "parameter":
			node.Type = GASTParameter
			node.Kind = "parameter"
		default:
			if isControlFlowType(tok.Type) {
				node.Type = GASTControlFlow
				node.Kind = tok.Type
			} else {
				node.Type = GASTFunction
				node.Kind = "function"
			}
		}

		if node.Type == GASTControlFlow {
			node.Visibility = "internal"
		} else if strings.HasPrefix(tok.Content, "pub") {
			node.Visibility = "public"
		} else {
			node.Visibility = "internal"
		}
	case ingest.TokenCall:
		node.Type = GASTCallExpression
		node.Kind = "call"
	}
	return node
}
