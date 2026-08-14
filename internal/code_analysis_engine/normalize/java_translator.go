package normalize

import (
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/ingest"
)

type JavaTranslator struct{}

func (t *JavaTranslator) CoerceToken(tok ingest.RichToken, parent *ingest.RichToken, fileRelPath string) *GASTNode {
	node := baseNode(tok, fileRelPath)
	extractGenericTypesAndDecorators(node, tok.Content)

	switch tok.Kind {
	case ingest.TokenImport:
		node.Type = GASTImport
		node.Kind = "import"
		node.Name = cleanImportPath(tok.Content)
	case ingest.TokenDeclaration:
		switch tok.Type {
		case "package_declaration":
			node.Type = GASTNamespace
			node.Kind = "namespace"
		case "class_declaration":
			node.Type = GASTTypeDeclaration
			node.Kind = "class"
			// §5.1.2/§5.2.2: inheritance and interface lists come from the
			// superclass / interfaces field roles, e.g. "extends A" and
			// "implements I1, I2" (fixes A-02/A-05).
			if base := stripClauseKeyword(tok.FieldRoles["superclass"]); base != "" {
				node.BaseTypes = []string{base}
			}
			if ifaces := splitTopLevelCommas(stripClauseKeyword(tok.FieldRoles["interfaces"])); len(ifaces) > 0 {
				for _, iface := range ifaces {
					if i := strings.TrimSpace(iface); i != "" {
						node.Implemented = append(node.Implemented, i)
					}
				}
			}
		case "interface_declaration":
			node.Type = GASTTypeDeclaration
			node.Kind = "interface"
			if bases := splitTopLevelCommas(stripClauseKeyword(tok.FieldRoles["interfaces"])); len(bases) > 0 {
				for _, base := range bases {
					if b := strings.TrimSpace(base); b != "" {
						node.BaseTypes = append(node.BaseTypes, b)
					}
				}
			}
		case "enum_declaration":
			node.Type = GASTTypeDeclaration
			node.Kind = "enum"
		case "record_declaration":
			node.Type = GASTTypeDeclaration
			node.Kind = "record"
		case "field_declaration":
			node.Type = GASTField
			node.Kind = "field"
			// v2 DataType population rule (fixes A-01): declared type from the
			// "type" role, parameterized types resolved from the declarator.
			node.FieldType = cleanTypeText(tok.FieldRoles["type"])
			if node.FieldType == "" {
				node.FieldType = lastTypeToken(tok.Content, tok.Name)
			}
			node.DataType = node.FieldType
		case "formal_parameter":
			node.Type = GASTParameter
			node.Kind = "parameter"
			node.DataType = cleanTypeText(tok.FieldRoles["type"])
			if node.DataType == "" {
				node.DataType = lastTypeToken(tok.Content, tok.Name)
			}
		case "constructor_declaration":
			node.Type = GASTFunction
			node.Kind = "constructor"
		case "method_declaration":
			node.Type = GASTFunction
			node.Kind = "method"
			sig := BuildSignatureWithRoles(tok.FieldRoles["name"], tok.FieldRoles["parameters"], tok.FieldRoles["type"], tok.Name, tok.Content)
			node.Signature = sig.Text
			if sig.ReturnType != "" {
				node.ReturnType = sig.ReturnType
				node.Properties["return_type"] = sig.ReturnType
			}
		default:
			if isControlFlowType(tok.Type) {
				node.Type = GASTControlFlow
				node.Kind = tok.Type
			} else {
				node.Type = GASTFunction
				node.Kind = "method"
			}
		}
		node.Visibility = resolveJavaVisibility(tok.Content)
		if node.Type != GASTControlFlow && tok.Type != "package_declaration" && tok.Type != "field_declaration" && tok.Type != "formal_parameter" {
			setDeclarationFQN(node, fileRelPath, tok.Name)
		}
	case ingest.TokenCall:
		node.Type = GASTCallExpression
		node.Kind = "call"
	}
	return node
}

// stripClauseKeyword removes the inheritance/implementation keyword prefix
// from a field-role clause text ("extends A" → "A", "implements I1, I2" → "I1, I2").
func stripClauseKeyword(clause string) string {
	clause = strings.TrimSpace(clause)
	for _, kw := range []string{"extends", "implements", "with"} {
		if strings.HasPrefix(clause, kw+" ") {
			return strings.TrimSpace(clause[len(kw):])
		}
	}
	return clause
}

func resolveJavaVisibility(content string) string {
	switch {
	case strings.Contains(content, "public"):
		return "public"
	case strings.Contains(content, "private"):
		return "private"
	case strings.Contains(content, "protected"):
		return "protected"
	default:
		return "internal"
	}
}
