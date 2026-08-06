package stage2

import (
	"regexp"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage1"
)

var goReceiverRegex = regexp.MustCompile(`func\s*\(\s*\w+\s+(\*?\w+)[\w\[\],\s]*\)`)

// goImportAliasRegex matches Go import_spec with an explicit alias:
// `akgerrs "github.com/Syamchand123/GlassMarble/internal/errors"`.
var goImportAliasRegex = regexp.MustCompile(`^\s*([A-Za-z_][\w.]*)\s+"((?:[^"\\]|\\.)*)"`)

type GoTranslator struct{}

// CoerceToken is the reference implementation of the v2 translator contract
// (master_overhaul_plan.md §5.2.2): field roles, base types, receivers and
// return types come from RichToken FieldRoles instead of content regexes.
// Content fallbacks remain for legacy call sites and role-less fixtures.
func (t *GoTranslator) CoerceToken(tok stage1.RichToken, parent *stage1.RichToken, fileRelPath string) *GASTNode {
	node := baseNode(tok, fileRelPath)
	extractGenericTypesAndDecorators(node, tok.Content)
	modName := moduleFromPath(fileRelPath)
	// Go uses "pkg" as the canonical module prefix for FQN
	pkgPrefix := "pkg"
	if modName != "module" {
		pkgPrefix = modName
	}

	switch tok.Kind {
	case stage1.TokenImport:
		node.Type = GASTImport
		node.Kind = "import"
		node.Name = cleanImportPath(tok.Content)
		// v2 (W1-09, §5.3.5 TestExternalIDs): explicit aliases are parsed
		// out so the external indexer can record them (import_spec has no
		// field roles — enrichment runs only for declarations).
		if m := goImportAliasRegex.FindStringSubmatch(strings.TrimSpace(tok.Content)); m != nil {
			node.Name = m[2]
			if node.Properties == nil {
				node.Properties = make(map[string]string)
			}
			node.Properties["import_alias"] = m[1]
		}

	case stage1.TokenDeclaration:
		switch tok.Type {
		case "package_clause":
			node.Type = GASTNamespace
			node.Kind = "namespace"

		case "function_declaration", "method_declaration", "method_elem":
			node.Type = GASTFunction
			if tok.Type == "method_elem" {
				// Interface method (§5.2.2): Kind="method", interface-scoped
				// receiver (empty). Signature from roles; BaseTypes wiring for
				// embedded interfaces happens via type_elem tokens.
				node.Kind = "method"
				node.ID = tok.Name
				node.Name = node.ID
				node.Properties["fully_qualified_name"] = pkgPrefix + "." + node.ID
			} else {
				node.Kind = "function"
				if tok.Type == "method_declaration" {
					node.Kind = "method"
					receiver := receiverFromRole(tok.FieldRoles)
					if receiver == "" {
						// Legacy fallback: parse the receiver from content.
						if match := goReceiverRegex.FindStringSubmatch(tok.Content); len(match) > 1 {
							receiver = strings.TrimPrefix(match[1], "*")
						}
					}
					if receiver == "" {
						// Unparseable receiver (e.g. generic `func (m *CowMap[K, V]) Len()`):
						// fall back to a bare-ID method so the raw path-based baseNode ID
						// never leaks into caller IDs.
						receiver = tok.Name
					}
					node.ReceiverType = receiver
					node.Properties["receiver_type"] = receiver
					// ID/Name: bare Receiver.Method (pkg-qualified FQN kept in properties)
					node.ID = receiver + "." + tok.Name
					node.Name = node.ID
					node.Properties["fully_qualified_name"] = pkgPrefix + "." + node.ID
				} else {
					// ID/Name: bare function name (pkg-qualified FQN kept in properties)
					node.ID = tok.Name
					node.Name = node.ID
					node.Properties["fully_qualified_name"] = pkgPrefix + "." + node.ID
				}
			}
			node.Visibility = resolveGoVisibility(tok.Name)
			// Generic functions/methods (`func Map[K comparable, V any](...)`)
			// get typed TypeParams like type declarations (W1-18/A-18).
			if tp := parseGoTypeParams(tok); len(tp) > 0 {
				node.TypeParams = tp
				node.Properties["type_params"] = typeParamsText(tp)
			}
			applySignature(node, tok)

		case "field_declaration":
			node.Type = GASTField
			node.Kind = "field"
			node.Visibility = resolveGoVisibility(tok.Name)
			node.FieldType = typeFromRole(tok.FieldRoles)
			if node.FieldType == "" {
				// Legacy fallback: last token of `Name string`.
				node.FieldType = lastTypeToken(tok.Content, tok.Name)
			}
			node.DataType = node.FieldType
			if tok.IsEmbedded {
				// Anonymous embedding (tree-sitter-go@v0.25.0: field_declaration
				// without a name field). EmbeddedOf = owning type, name = embedded
				// type — normalizer wires it into the owner's BaseTypes (A-02/A-07).
				node.Kind = "embedding"
				node.FieldType = tok.Name
				node.DataType = tok.Name
				node.Properties["embedded"] = "true"
				if parent != nil {
					node.EmbeddedOf = parent.Name
					node.Properties["embedded_of"] = parent.Name
				}
			}

		case "parameter_declaration":
			node.Type = GASTParameter
			node.Kind = "parameter"
			node.Visibility = "internal"
			node.DataType = typeFromRole(tok.FieldRoles)
			if node.DataType == "" {
				node.DataType = lastTypeToken(tok.Content, tok.Name)
			}

		case "type_elem":
			// Embedded interface inside interface_type (tree-sitter-go@v0.25.0).
			node.Type = GASTField
			node.Kind = "embedding"
			node.FieldType = tok.Name
			node.DataType = tok.Name
			node.Properties["embedded"] = "true"
			if parent != nil {
				node.EmbeddedOf = parent.Name
				node.Properties["embedded_of"] = parent.Name
			}

		case "type_declaration", "type_spec":
			node.Type = GASTTypeDeclaration
			node.Visibility = resolveGoVisibility(tok.Name)
			node.ID = tok.Name
			node.Name = node.ID
			node.Properties["fully_qualified_name"] = pkgPrefix + "." + node.ID
			node.Kind = goTypeKind(tok)
			if node.Kind == "alias" {
				node.DataType = goAliasTarget(tok)
				if node.DataType == "" {
					node.DataType = "unknown"
				}
			}
			if tp := parseGoTypeParams(tok); len(tp) > 0 {
				node.TypeParams = tp
				node.Properties["type_params"] = typeParamsText(tp)
			}

		default:
			// Handle control flow statements
			switch tok.Type {
			case "if_statement", "if", "for_statement", "for",
				"switch_statement", "switch", "return_statement", "return",
				"defer", "go", "go_statement", "while_statement", "while",
				"do_statement", "do", "try_statement", "try", "catch_clause",
				"throw", "throw_statement", "for_each", "foreach":
				node.Type = GASTControlFlow
				node.Kind = tok.Type
				node.Visibility = "internal"
			default:
				node.Type = GASTTypeDeclaration
				node.Kind = tok.Type
				node.Visibility = resolveGoVisibility(tok.Name)
				node.ID = tok.Name
				node.Name = node.ID
				node.Properties["fully_qualified_name"] = pkgPrefix + "." + node.ID
			}
		}

	case stage1.TokenCall:
		node.Type = GASTCallExpression
		node.Kind = "call"
	}
	return node
}

// applySignature fills the normalized Signature text and ReturnType from the
// RichToken field roles (fixes A-09).
func applySignature(node *GASTNode, tok stage1.RichToken) {
	sig := BuildSignature(tok)
	node.Signature = sig.Text
	if sig.ReturnType != "" {
		node.ReturnType = sig.ReturnType
		node.Properties["return_type"] = sig.ReturnType
	}
}

// receiverFromRole extracts the receiver type (without `*`) from the
// "receiver" field role, e.g. "(o *Options)" → "Options".
func receiverFromRole(roles map[string]string) string {
	recv := strings.TrimSpace(roles["receiver"])
	if recv == "" {
		return ""
	}
	recv = strings.Trim(recv, "()")
	recv = strings.TrimSpace(recv)
	fields := strings.Fields(recv)
	if len(fields) == 0 {
		return ""
	}
	last := fields[len(fields)-1]
	last = strings.TrimPrefix(last, "*")
	// Generic receiver: strip any type arguments.
	if idx := strings.IndexAny(last, "[{"); idx != -1 {
		last = last[:idx]
	}
	return last
}

// typeFromRole reads the declared type from the "type" role, falling back to
// the variadic role ("...T") when present.
func typeFromRole(roles map[string]string) string {
	if t := strings.TrimSpace(roles["type"]); t != "" {
		return t
	}
	if v := strings.TrimSpace(roles["variadic_type"]); v != "" {
		return "..." + v
	}
	return ""
}

// lastTypeToken recovers the declared type of a `Name Type` snippet
// (legacy fallback when field roles are absent).
func lastTypeToken(content, name string) string {
	content = strings.TrimSpace(content)
	rest := strings.TrimSpace(strings.TrimPrefix(content, name))
	if rest == "" {
		return content
	}
	fields := strings.Fields(rest)
	if len(fields) == 0 {
		return ""
	}
	typ := fields[len(fields)-1]
	typ = strings.Trim(typ, ";,")
	if typ == "" {
		return ""
	}
	return typ
}

// goTypeKind classifies a type_spec as struct / interface / alias using the
// "type" field role first, then the content (legacy fixtures).
func goTypeKind(tok stage1.RichToken) string {
	typeText := strings.ToLower(strings.TrimSpace(tok.FieldRoles["type"]))
	switch {
	case strings.HasPrefix(typeText, "interface"):
		return "interface"
	case strings.HasPrefix(typeText, "struct"):
		return "struct"
	}
	lower := strings.ToLower(tok.Content)
	switch {
	case strings.Contains(lower, "interface"):
		return "interface"
	case strings.Contains(lower, "struct"):
		return "struct"
	case strings.Contains(tok.Content, "="):
		return "alias"
	default:
		return "alias"
	}
}

// goAliasTarget recovers the alias target from `type X = Target` or
// `type X Target` content.
func goAliasTarget(tok stage1.RichToken) string {
	if i := strings.Index(tok.Content, "="); i != -1 {
		return strings.TrimSpace(tok.Content[i+1:])
	}
	target := cleanTypeText(tok.FieldRoles["type"])
	if target == "" {
		target = strings.TrimSpace(strings.TrimPrefix(tok.Content, "type "+tok.Name))
	}
	return strings.TrimSpace(target)
}

var goTypeParamsNameRegex = regexp.MustCompile(`^type\s+([A-Za-z_]\w*)`)

// parseGoTypeParams extracts generic type parameters (`[K comparable, V any]`)
// from a type_spec or generic function content.
func parseGoTypeParams(tok stage1.RichToken) []TypeParam {
	content := tok.Content
	name := tok.Name
	if name == "" {
		if m := goTypeParamsNameRegex.FindStringSubmatch(content); len(m) > 1 {
			name = m[1]
		}
	}
	if name == "" {
		return nil
	}
	idx := strings.Index(content, name)
	if idx == -1 {
		return nil
	}
	rest := content[idx+len(name):]
	rest = strings.TrimLeft(rest, " \t")
	if !strings.HasPrefix(rest, "[") {
		return nil
	}
	depth := 0
	end := -1
	for i := 0; i < len(rest); i++ {
		switch rest[i] {
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				end = i
			}
		}
		if end != -1 {
			break
		}
	}
	if end == -1 {
		return nil
	}
	inner := rest[1:end]
	var params []TypeParam
	for _, part := range splitTopLevelCommas(inner) {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		tp := TypeParam{Name: part}
		if i := strings.IndexAny(part, " \t"); i != -1 {
			tp.Name = part[:i]
			tp.Constraint = strings.TrimSpace(part[i+1:])
		}
		params = append(params, tp)
	}
	return params
}

// splitTopLevelCommas splits on commas not nested inside brackets.
func splitTopLevelCommas(s string) []string {
	var parts []string
	depth := 0
	var cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			parts = append(parts, cur.String())
			cur.Reset()
		}
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '[', '(', '{':
			depth++
		case ']', ')', '}':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				flush()
				continue
			}
		}
		cur.WriteByte(c)
	}
	flush()
	return parts
}

func typeParamsText(params []TypeParam) string {
	var parts []string
	for _, p := range params {
		if p.Constraint != "" {
			parts = append(parts, p.Name+" "+p.Constraint)
		} else {
			parts = append(parts, p.Name)
		}
	}
	return strings.Join(parts, ", ")
}

func resolveGoVisibility(name string) string {
	if name != "" && name[0] >= 'A' && name[0] <= 'Z' {
		return "public"
	}
	return "internal"
}
