package normalize

import (
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/ingest"
)

type RubyTranslator struct{}

func (t *RubyTranslator) CoerceToken(tok ingest.RichToken, parent *ingest.RichToken, fileRelPath string) *GASTNode {
	node := baseNode(tok, fileRelPath)
	extractGenericTypesAndDecorators(node, tok.Content)
	switch tok.Kind {
	case ingest.TokenImport:
		if strings.HasPrefix(tok.Content, "require ") || strings.HasPrefix(tok.Content, "require(") || strings.HasPrefix(tok.Content, "require_relative ") || strings.HasPrefix(tok.Content, "require_relative(") || strings.HasPrefix(tok.Content, "require'") || strings.HasPrefix(tok.Content, "require\"") || strings.HasPrefix(tok.Content, "require_relative'") || strings.HasPrefix(tok.Content, "require_relative\"") {
			node.Type = GASTImport
			node.Kind = "require"
			if strings.Contains(tok.Content, "require_relative") {
				node.Kind = "require_relative"
			}
			node.Name = cleanImportPath(tok.Content)
		} else {
			node.Type = GASTCallExpression
			node.Kind = "call"
		}
	case ingest.TokenDeclaration:
		if strings.Contains(tok.Type, "class") {
			node.Type = GASTTypeDeclaration
			node.Kind = "class"
		} else if strings.Contains(tok.Type, "module") {
			// Ruby module = namespace/mixin scope
			node.Type = GASTNamespace
			node.Kind = "module"
			node.Name = tok.Name
		} else if strings.Contains(tok.Type, "field") || strings.Contains(tok.Type, "property") || strings.Contains(tok.Type, "attribute") {
			node.Type = GASTField
			node.Kind = "field"
		} else if strings.Contains(tok.Type, "parameter") || strings.Contains(tok.Type, "argument") {
			node.Type = GASTParameter
			node.Kind = "parameter"
		} else if isControlFlowType(tok.Type) {
			node.Type = GASTControlFlow
			node.Kind = tok.Type
			node.Visibility = "internal"
		} else {
			node.Type = GASTFunction
			node.Kind = "method"
		}
		if node.Type != GASTNamespace && node.Type != GASTControlFlow {
			node.Visibility = "public"
			setDeclarationFQN(node, fileRelPath, tok.Name)
		}
	case ingest.TokenCall:
		node.Type = GASTCallExpression
		node.Kind = "call"
	}
	return node
}
