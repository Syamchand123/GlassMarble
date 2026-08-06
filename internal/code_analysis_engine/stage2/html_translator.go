package stage2

import (
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage1"
)

type HTMLTranslator struct{}

func (t *HTMLTranslator) CoerceToken(tok stage1.RichToken, parent *stage1.RichToken, fileRelPath string) *GASTNode {
	node := baseNode(tok, fileRelPath)
	extractGenericTypesAndDecorators(node, tok.Content)
	switch tok.Kind {
	case stage1.TokenImport:
		node.Type = GASTImport
		node.Kind = "script"
		node.Name = cleanImportPath(tok.Content)
	case stage1.TokenDeclaration:
		node.Type = GASTTypeDeclaration
		node.Kind = "element"
		if strings.Contains(tok.Type, "script") {
			node.Kind = "script"
		} else if strings.Contains(tok.Type, "style") {
			node.Kind = "style"
		}
		node.Visibility = "public"
		setDeclarationFQN(node, fileRelPath, tok.Name)
	case stage1.TokenCall:
		node.Type = GASTCallExpression
		node.Kind = "call"
	}
	return node
}
