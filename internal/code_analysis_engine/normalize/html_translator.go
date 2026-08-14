package normalize

import (
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/ingest"
)

type HTMLTranslator struct{}

func (t *HTMLTranslator) CoerceToken(tok ingest.RichToken, parent *ingest.RichToken, fileRelPath string) *GASTNode {
	node := baseNode(tok, fileRelPath)
	extractGenericTypesAndDecorators(node, tok.Content)
	switch tok.Kind {
	case ingest.TokenImport:
		node.Type = GASTImport
		node.Kind = "script"
		node.Name = cleanImportPath(tok.Content)
	case ingest.TokenDeclaration:
		node.Type = GASTTypeDeclaration
		node.Kind = "element"
		if strings.Contains(tok.Type, "script") {
			node.Kind = "script"
		} else if strings.Contains(tok.Type, "style") {
			node.Kind = "style"
		}
		node.Visibility = "public"
		setDeclarationFQN(node, fileRelPath, tok.Name)
	case ingest.TokenCall:
		node.Type = GASTCallExpression
		node.Kind = "call"
	}
	return node
}
