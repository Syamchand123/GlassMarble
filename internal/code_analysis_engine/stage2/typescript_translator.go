package stage2

import (
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage1"
)

type TSTranslator struct{}

func (t *TSTranslator) CoerceToken(tok stage1.RichToken, parent *stage1.RichToken, fileRelPath string) *GASTNode {
	node := baseNode(tok, fileRelPath)
	extractGenericTypesAndDecorators(node, tok.Content)

	switch tok.Kind {
	case stage1.TokenImport:
		node.Type = GASTImport
		node.Kind = "import"
		node.Name = cleanImportPath(tok.Content)
	case stage1.TokenDeclaration:
		// TypeScript namespace/module declarations emit GASTNamespace
		if tok.Type == "module_declaration" || tok.Type == "namespace_declaration" || tok.Type == "module" {
			node.Type = GASTNamespace
			node.Kind = "namespace"
			node.Name = tok.Name
			if node.Name == "" {
				node.Name = moduleFromPath(fileRelPath)
			}
			break
		}
		if strings.Contains(tok.Type, "field") || strings.Contains(tok.Type, "property") {
			node.Type = GASTField
			node.Kind = "field"
		} else if strings.Contains(tok.Type, "parameter") {
			node.Type = GASTParameter
			node.Kind = "parameter"
		} else if strings.Contains(tok.Type, "class") || strings.Contains(tok.Type, "interface") || strings.Contains(tok.Type, "enum") || strings.Contains(tok.Type, "type_alias") {
			node.Type = GASTTypeDeclaration
			node.Kind = "class"
			if strings.Contains(tok.Type, "interface") {
				node.Kind = "interface"
			} else if strings.Contains(tok.Type, "enum") {
				node.Kind = "enum"
			} else if strings.Contains(tok.Type, "type_alias") {
				node.Kind = "type_alias"
			}
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

		if strings.Contains(tok.Content, "private") {
			node.Visibility = "private"
		} else if strings.Contains(tok.Content, "protected") {
			node.Visibility = "protected"
		} else if strings.Contains(tok.Content, "export") || tok.ParentIdx <= 0 {
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
