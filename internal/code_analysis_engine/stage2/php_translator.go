package stage2

import (
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage1"
)

type PHPTranslator struct{}

func (t *PHPTranslator) CoerceToken(tok stage1.RichToken, parent *stage1.RichToken, fileRelPath string) *GASTNode {
	node := baseNode(tok, fileRelPath)
	extractGenericTypesAndDecorators(node, tok.Content)
	switch tok.Kind {
	case stage1.TokenImport:
		node.Type = GASTImport
		node.Kind = "use"
		node.Name = cleanImportPath(tok.Content)
	case stage1.TokenDeclaration:
		if strings.Contains(tok.Type, "namespace") {
			node.Type = GASTNamespace
			node.Kind = "namespace"
		} else if strings.Contains(tok.Type, "class") || strings.Contains(tok.Type, "interface") || strings.Contains(tok.Type, "trait") || strings.Contains(tok.Type, "enum") {
			node.Type = GASTTypeDeclaration
			node.Kind = "class"
			if strings.Contains(tok.Type, "interface") {
				node.Kind = "interface"
			} else if strings.Contains(tok.Type, "trait") {
				node.Kind = "trait"
			} else if strings.Contains(tok.Type, "enum") {
				node.Kind = "enum"
			}
		} else if strings.Contains(tok.Type, "field") || strings.Contains(tok.Type, "property") {
			node.Type = GASTField
			node.Kind = "field"
		} else if strings.Contains(tok.Type, "parameter") || strings.Contains(tok.Type, "argument") {
			node.Type = GASTParameter
			node.Kind = "parameter"
		} else if isControlFlowType(tok.Type) {
			node.Type = GASTControlFlow
			node.Kind = tok.Type
		} else {
			node.Type = GASTFunction
			node.Kind = "function"
			if strings.Contains(tok.Type, "method") {
				node.Kind = "method"
			}
		}
		node.Visibility = resolveJavaVisibility(tok.Content)
		if node.Type != GASTControlFlow && tok.Type != "namespace_use_declaration" {
			setDeclarationFQN(node, fileRelPath, tok.Name)
		}
	case stage1.TokenCall:
		lowerContent := strings.ToLower(tok.Content)
		if strings.Contains(lowerContent, "include") || strings.Contains(lowerContent, "require") {
			node.Type = GASTImport
			node.Kind = "include"
			node.Name = cleanImportPath(tok.Content)
		} else {
			node.Type = GASTCallExpression
			node.Kind = "call"
		}
	}
	return node
}
