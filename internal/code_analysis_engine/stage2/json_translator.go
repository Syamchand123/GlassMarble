package stage2

import (
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage1"
)

type JSONTranslator struct{}

func (t *JSONTranslator) CoerceToken(tok stage1.RawToken, fileRelPath string) *GASTNode {
	node := baseNode(tok, fileRelPath)
	extractGenericTypesAndDecorators(node, tok.Content)
	switch tok.Kind {
	case stage1.TokenImport:
		node.Type = GASTImport
		node.Kind = "ref"
		node.Name = cleanImportPath(tok.Content)
	case stage1.TokenDeclaration:
		node.Type = GASTTypeDeclaration
		node.Kind = "object"
		if strings.Contains(tok.Type, "array") {
			node.Kind = "array"
		} else if strings.Contains(tok.Type, "pair") {
			node.Kind = "property"
		}
		node.Visibility = "public"
		setDeclarationFQN(node, fileRelPath, tok.Name)
	case stage1.TokenCall:
		node.Type = GASTCallExpression
		node.Kind = "call"
	}
	return node
}
