package stage2

import (
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage1"
)

type GenericTranslator struct {
	Lang stage1.SupportedLang
}

func (g *GenericTranslator) CoerceToken(tok stage1.RawToken, fileRelPath string) *GASTNode {
	node := baseNode(tok, fileRelPath)
	extractGenericTypesAndDecorators(node, tok.Content)
	switch tok.Kind {
	case stage1.TokenImport:
		node.Type = GASTImport
		node.Kind = "import"
		node.Name = cleanImportPath(tok.Content)
	case stage1.TokenDeclaration:
		switch tok.Type {
		case "if_statement", "if", "for_statement", "for",
			"switch_statement", "switch", "return_statement", "return",
			"defer", "go", "go_statement", "while_statement", "while",
			"do_statement", "do", "try_statement", "try", "catch_clause",
			"throw", "throw_statement", "raise", "for_each", "foreach",
			"for_in_statement", "for_of_statement":
			node.Type = GASTControlFlow
			node.Kind = tok.Type
			node.Visibility = "internal"
		default:
			node.Type = GASTTypeDeclaration
			node.Kind = tok.Type
			node.Visibility = "public"
			setDeclarationFQN(node, fileRelPath, tok.Name)
		}
	case stage1.TokenCall:
		node.Type = GASTCallExpression
		node.Kind = "call"
	}
	return node
}
