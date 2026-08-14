package normalize

import (
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/ingest"
)

type CSSTranslator struct{}

func (t *CSSTranslator) CoerceToken(tok ingest.RichToken, parent *ingest.RichToken, fileRelPath string) *GASTNode {
	node := baseNode(tok, fileRelPath)
	extractGenericTypesAndDecorators(node, tok.Content)
	switch tok.Kind {
	case ingest.TokenImport:
		node.Type = GASTImport
		node.Kind = "import"
		node.Name = cleanImportPath(tok.Content)
	case ingest.TokenDeclaration:
		node.Type = GASTTypeDeclaration
		node.Kind = "rule_set"
		if strings.Contains(tok.Type, "media") {
			node.Kind = "media"
		} else if strings.Contains(tok.Type, "keyframe") {
			node.Kind = "keyframes"
		}
		node.Visibility = "public"
		setDeclarationFQN(node, fileRelPath, tok.Name)
	case ingest.TokenCall:
		node.Type = GASTCallExpression
		node.Kind = "call"
	}
	return node
}
