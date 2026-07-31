package stage2

import (
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage1"
)

type CppTranslator struct{}

func (t *CppTranslator) CoerceToken(tok stage1.RawToken, fileRelPath string) *GASTNode {
	node := baseNode(tok, fileRelPath)
	extractGenericTypesAndDecorators(node, tok.Content)
	switch tok.Kind {
	case stage1.TokenImport:
		node.Type = GASTImport
		node.Kind = "include"
		node.Name = cleanImportPath(tok.Content)
	case stage1.TokenDeclaration:
		if strings.Contains(tok.Type, "namespace") {
			node.Type = GASTNamespace
			node.Kind = "namespace"
		} else if strings.Contains(tok.Type, "class") || strings.Contains(tok.Type, "struct") || strings.Contains(tok.Type, "union") {
			node.Type = GASTTypeDeclaration
			node.Kind = "struct"
			if strings.Contains(tok.Type, "class") {
				node.Kind = "class"
			} else if strings.Contains(tok.Type, "union") {
				node.Kind = "union"
			}
		} else if strings.Contains(tok.Type, "field") || strings.Contains(tok.Type, "property") {
			node.Type = GASTField
			node.Kind = "field"
		} else if strings.Contains(tok.Type, "parameter") || strings.Contains(tok.Type, "argument") {
			node.Type = GASTParameter
			node.Kind = "parameter"
		} else {
			node.Type = GASTFunction
			node.Kind = "function"
			if strings.Contains(tok.Type, "method") {
				node.Kind = "method"
			}
		}

		if strings.Contains(tok.Content, "private:") {
			node.Visibility = "private"
		} else if strings.Contains(tok.Content, "protected:") {
			node.Visibility = "protected"
		} else {
			node.Visibility = "public"
		}
		if tok.Type != "namespace_definition" {
			setDeclarationFQN(node, fileRelPath, tok.Name)
		}
	case stage1.TokenCall:
		node.Type = GASTCallExpression
		node.Kind = "call"
	}

	if strings.HasSuffix(fileRelPath, ".h") || strings.HasSuffix(fileRelPath, ".hpp") || strings.HasSuffix(fileRelPath, ".hh") || strings.HasSuffix(fileRelPath, ".hxx") {
		node.Properties["is_header"] = "true"
		node.Properties["role"] = "interface"
	} else {
		node.Properties["is_header"] = "false"
		node.Properties["role"] = "implementation"
	}

	return node
}
