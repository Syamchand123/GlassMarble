package stage2

// controlFlowTypes lists statement kinds that represent control flow rather
// than definitions. stage1 classifies these as TokenDeclaration so the
// per-language translators must map them to GASTControlFlow here; any
// translator that lets them fall through to GASTFunction/GASTTypeDeclaration
// pollutes the definition index with fake functions (see AUDIT Issue 1.3).
var controlFlowTypes = map[string]bool{
	"if_statement": true, "if": true,
	"for_statement": true, "for": true,
	"while_statement": true, "while": true,
	"do_statement": true, "do": true,
	"switch_statement": true, "switch": true,
	"return_statement": true, "return": true,
	"try_statement": true, "try": true,
	"catch_clause": true, "catch": true,
	"defer": true, "go": true, "go_statement": true,
	"throw_statement": true, "throw": true, "raise": true,
	"for_each": true, "foreach": true,
	"for_in_statement": true, "for_of_statement": true,
}

// isControlFlowType reports whether a raw token type is a control-flow
// statement that must never be indexed as a definition. The list mirrors
// stage1/parser.go classifyKind and the GenericTranslator.
func isControlFlowType(tokType string) bool {
	return controlFlowTypes[tokType]
}
