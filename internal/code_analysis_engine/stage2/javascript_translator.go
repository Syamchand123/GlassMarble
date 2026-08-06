package stage2

import (
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage1"
)

type JSTranslator struct{}

func (t *JSTranslator) CoerceToken(tok stage1.RichToken, parent *stage1.RichToken, fileRelPath string) *GASTNode {
	node := baseNode(tok, fileRelPath)
	extractGenericTypesAndDecorators(node, tok.Content)
	switch tok.Kind {
	case stage1.TokenImport:
		node.Type = GASTImport
		node.Kind = "import"
		node.Name = cleanImportPath(tok.Content)
	case stage1.TokenDeclaration:
		// Detect namespace/module declarations by type name
		if tok.Type == "module" || strings.Contains(tok.Type, "namespace_declaration") {
			node.Type = GASTNamespace
			node.Kind = "module"
			node.Name = moduleFromPath(fileRelPath)
			break
		}
		if strings.Contains(tok.Type, "class") {
			node.Type = GASTTypeDeclaration
			node.Kind = "class"
		} else if strings.Contains(tok.Type, "field") || strings.Contains(tok.Type, "property") {
			node.Type = GASTField
			node.Kind = "field"
		} else if strings.Contains(tok.Type, "parameter") {
			node.Type = GASTParameter
			node.Kind = "parameter"
		} else if isControlFlowType(tok.Type) {
			node.Type = GASTControlFlow
			node.Kind = tok.Type
		} else if strings.Contains(tok.Type, "variable") || strings.Contains(tok.Type, "lexical") {
			node.Type = GASTVariable
			node.Kind = "variable"
		} else {
			node.Type = GASTFunction
			node.Kind = "function"
			if strings.Contains(tok.Type, "method") {
				node.Kind = "method"
			}
		}

		if strings.Contains(tok.Content, "export") || strings.Contains(tok.Content, "module.exports") {
			node.Visibility = "public"
		} else {
			node.Visibility = "internal"
		}
		if node.Type != GASTControlFlow {
			setDeclarationFQN(node, fileRelPath, tok.Name)
		}
	case stage1.TokenCall:
		node.Type = GASTCallExpression
		node.Kind = "call"
	}
	return node
}
