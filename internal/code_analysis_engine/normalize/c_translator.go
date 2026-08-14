package normalize

import (
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/ingest"
)

type CTranslator struct{}

func (t *CTranslator) CoerceToken(tok ingest.RichToken, parent *ingest.RichToken, fileRelPath string) *GASTNode {
	node := baseNode(tok, fileRelPath)
	extractGenericTypesAndDecorators(node, tok.Content)
	switch tok.Kind {
	case ingest.TokenImport:
		node.Type = GASTImport
		node.Kind = "include"
		node.Name = cleanImportPath(tok.Content)
	case ingest.TokenDeclaration:
		if strings.Contains(tok.Type, "struct") || strings.Contains(tok.Type, "union") || strings.Contains(tok.Type, "enum") {
			node.Type = GASTTypeDeclaration
			node.Kind = "struct"
			if strings.Contains(tok.Type, "union") {
				node.Kind = "union"
			} else if strings.Contains(tok.Type, "enum") {
				node.Kind = "enum"
			}
		} else if strings.Contains(tok.Type, "field") {
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
			node.Kind = "function"
		}
		if node.Type != GASTControlFlow {
			node.Visibility = "public"
			setDeclarationFQN(node, fileRelPath, tok.Name)
		}
	case ingest.TokenCall:
		node.Type = GASTCallExpression
		node.Kind = "call"
	}

	if strings.HasSuffix(fileRelPath, ".h") {
		node.Properties["is_header"] = "true"
		node.Properties["role"] = "interface"
	} else {
		node.Properties["is_header"] = "false"
		node.Properties["role"] = "implementation"
	}

	return node
}
