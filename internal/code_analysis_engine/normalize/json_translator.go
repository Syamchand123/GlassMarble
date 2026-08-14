package normalize

import (
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/ingest"
)

type JSONTranslator struct{}

func (t *JSONTranslator) CoerceToken(tok ingest.RichToken, parent *ingest.RichToken, fileRelPath string) *GASTNode {
	node := baseNode(tok, fileRelPath)
	extractGenericTypesAndDecorators(node, tok.Content)
	switch tok.Kind {
	case ingest.TokenImport:
		node.Type = GASTImport
		node.Kind = "ref"
		node.Name = cleanImportPath(tok.Content)
	case ingest.TokenDeclaration:
		node.Type = GASTTypeDeclaration
		node.Kind = "object"
		if strings.Contains(tok.Type, "array") {
			node.Kind = "array"
		} else if strings.Contains(tok.Type, "pair") {
			node.Kind = "property"
		}
		node.Visibility = "public"
		setDeclarationFQN(node, fileRelPath, tok.Name)
	case ingest.TokenCall:
		node.Type = GASTCallExpression
		node.Kind = "call"
	}
	return node
}
