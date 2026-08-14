package normalize

import (
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/ingest"
)

// Signature is a normalized function/method signature
// (master_overhaul_plan.md §5.2.3). Consumers:
//   - EdgeReturns / EdgeHasParam producers (5.4),
//   - sequence/timing diagrams (return values),
//   - interface matching (interface_linker).
type Signature struct {
	Name       string
	Params     []string // parameter NAMES (empty for unnamed params)
	ParamTypes []string // parameter TYPE text (unnamed params keep full text)
	ReturnType string
	Text       string // normalized `name(paramTypes) ret`
}

// BuildSignature derives a Signature from a RichToken's field roles
// (name / parameters / result), falling back to content parsing for
// translators that lack role capture.
func BuildSignature(tok ingest.RichToken) Signature {
	return BuildSignatureWithRoles(tok.FieldRoles["name"], tok.FieldRoles["parameters"], tok.FieldRoles["result"], tok.Name, tok.Content)
}

// BuildSignatureWithRoles derives a Signature from explicit role texts.
// resultRole names the role holding the return type (Go: "result",
// Java/C#/C++: "type"); paramsRole holds the `(a int, b string)` clause.
func BuildSignatureWithRoles(nameRole, paramsRole, resultRole, fallbackName, content string) Signature {
	sig := Signature{
		Name:       firstNonEmpty(nameRole, fallbackName),
		ReturnType: cleanTypeText(resultRole),
	}

	paramsText := paramsRole
	if paramsText == "" {
		paramsText = extractParamsTextFromContent(content)
	}
	sig.Params, sig.ParamTypes = splitParams(paramsText)
	sig.Text = normalizeSignature(sig)
	return sig
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func cleanTypeText(s string) string {
	return strings.TrimSpace(s)
}

// extractParamsTextFromContent recovers the `(a, b, c)` parameter clause
// from a function-type content string like `func(ctx context.Context) error`.
// Returns "" when no balanced parenthesized clause exists.
func extractParamsTextFromContent(content string) string {
	open := strings.IndexByte(content, '(')
	if open == -1 {
		return ""
	}
	depth := 0
	for i := open; i < len(content); i++ {
		switch content[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return content[open : i+1]
			}
		}
	}
	return ""
}

// splitParams parses a `(a int, b string)` clause into parameter names and
// type texts. Splitting respects nested <> [] {} () so generic types and
// function-type params do not produce phantom parameters.
func splitParams(clause string) (names, types []string) {
	clause = strings.TrimSpace(clause)
	if strings.HasPrefix(clause, "(") && strings.HasSuffix(clause, ")") {
		clause = clause[1 : len(clause)-1]
	}
	if strings.TrimSpace(clause) == "" {
		return nil, nil
	}

	var parts []string
	depth := 0
	var cur strings.Builder
	flush := func() {
		if s := strings.TrimSpace(cur.String()); s != "" {
			parts = append(parts, s)
		}
		cur.Reset()
	}
	for i := 0; i < len(clause); i++ {
		c := clause[i]
		switch c {
		case '<', '[', '{', '(':
			depth++
		case '>', ']', '}', ')':
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

	for _, part := range parts {
		name, typ := splitParam(part)
		names = append(names, name)
		types = append(types, typ)
	}
	return names, types
}

// splitParam splits a single parameter like `ctx context.Context` or
// `a ...string` into name + type text. Unnamed parameters keep their full
// text as the type.
func splitParam(part string) (name, typ string) {
	part = strings.TrimSpace(part)
	part = strings.TrimPrefix(part, "...")
	part = strings.TrimSpace(part)
	if i := strings.IndexAny(part, " \t"); i != -1 {
		head, tail := part[:i], strings.TrimSpace(part[i+1:])
		if isSimpleIdentifier(head) {
			return head, tail
		}
	}
	if isSimpleIdentifier(part) {
		// Name-only parameter (role fallback without a type).
		return part, ""
	}
	return "", part
}

func isSimpleIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if i == 0 {
			if !(r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') {
				return false
			}
			continue
		}
		if !(r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

// normalizeSignature renders the canonical `name(paramTypes) ret` text.
func normalizeSignature(sig Signature) string {
	var b strings.Builder
	b.WriteString(sig.Name)
	b.WriteByte('(')
	for i, t := range sig.ParamTypes {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(t)
	}
	b.WriteByte(')')
	if sig.ReturnType != "" {
		b.WriteByte(' ')
		b.WriteString(sig.ReturnType)
	}
	return b.String()
}
