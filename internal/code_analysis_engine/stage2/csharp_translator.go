package stage2

import (
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage1"
)

type CSharpTranslator struct{}

func (t *CSharpTranslator) CoerceToken(tok stage1.RawToken, fileRelPath string) *GASTNode {
	node := baseNode(tok, fileRelPath)
	extractGenericTypesAndDecorators(node, tok.Content)

	switch tok.Kind {
	case stage1.TokenImport:
		node.Type = GASTImport
		node.Kind = "using"
		node.Name = cleanImportPath(tok.Content)
	case stage1.TokenDeclaration:
		if strings.Contains(tok.Type, "namespace") {
			node.Type = GASTNamespace
			node.Kind = "namespace"
		} else if strings.Contains(tok.Type, "field") || strings.Contains(tok.Type, "property") {
			node.Type = GASTField
			node.Kind = "field"
		} else if strings.Contains(tok.Type, "parameter") {
			node.Type = GASTParameter
			node.Kind = "parameter"
		} else if strings.Contains(tok.Type, "class") || strings.Contains(tok.Type, "interface") || strings.Contains(tok.Type, "struct") || strings.Contains(tok.Type, "record") || strings.Contains(tok.Type, "enum") {
			node.Type = GASTTypeDeclaration
			node.Kind = "class"
			if strings.Contains(tok.Type, "interface") {
				node.Kind = "interface"
			} else if strings.Contains(tok.Type, "struct") {
				node.Kind = "struct"
			} else if strings.Contains(tok.Type, "record") {
				node.Kind = "record"
			} else if strings.Contains(tok.Type, "enum") {
				node.Kind = "enum"
			}
		} else if isControlFlowType(tok.Type) {
			node.Type = GASTControlFlow
			node.Kind = tok.Type
		} else {
			node.Type = GASTFunction
			node.Kind = "method"
		}
		node.Visibility = resolveJavaVisibility(tok.Content)
		if node.Type != GASTControlFlow && tok.Type != "namespace_declaration" && tok.Type != "field_declaration" && tok.Type != "parameter" {
			setDeclarationFQN(node, fileRelPath, tok.Name)
		}
	case stage1.TokenCall:
		node.Type = GASTCallExpression
		node.Kind = "call"
	}
	return node
}
