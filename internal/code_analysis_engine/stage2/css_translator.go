package stage2

import (
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage1"
)

type CSSTranslator struct{}

func (t *CSSTranslator) CoerceToken(tok stage1.RichToken, parent *stage1.RichToken, fileRelPath string) *GASTNode {
	node := baseNode(tok, fileRelPath)
	extractGenericTypesAndDecorators(node, tok.Content)
	switch tok.Kind {
	case stage1.TokenImport:
		node.Type = GASTImport
		node.Kind = "import"
		node.Name = cleanImportPath(tok.Content)
	case stage1.TokenDeclaration:
		node.Type = GASTTypeDeclaration
		node.Kind = "rule_set"
		if strings.Contains(tok.Type, "media") {
			node.Kind = "media"
		} else if strings.Contains(tok.Type, "keyframe") {
			node.Kind = "keyframes"
		}
		node.Visibility = "public"
		setDeclarationFQN(node, fileRelPath, tok.Name)
	case stage1.TokenCall:
		node.Type = GASTCallExpression
		node.Kind = "call"
	}
	return node
}
