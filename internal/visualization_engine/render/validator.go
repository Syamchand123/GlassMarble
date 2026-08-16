package aggregate

import (
	"fmt"

	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/types"
)

// ValidateAndOptimizeAST runs AST hygiene, validation, and pruning passes
// before passing the AST to target format visitors.
func ValidateAndOptimizeAST(ast *types.DiagramAST) *types.DiagramAST {
	if ast == nil {
		return nil
	}

	// Pass 1: Prune empty boundaries (eliminates got 'RBRACE' syntax errors)
	if ast.Root != nil {
		ast.Root = pruneEmptyBoundaries(ast.Root)
	}

	// Pass 2: Ensure unique and valid identifier strings
	sanitizeASTElementsAndBoundaries(ast)

	// Pass 3: Prune dangling edges whose endpoints were filtered out
	ast.Edges = pruneDanglingEdges(ast)

	return ast
}

// pruneEmptyBoundaries recursively removes any boundary that contains 0 elements
// and 0 non-empty children.
func pruneEmptyBoundaries(b *types.ASTBoundary) *types.ASTBoundary {
	if b == nil {
		return nil
	}

	var validChildren []*types.ASTBoundary
	for _, child := range b.Children {
		pruned := pruneEmptyBoundaries(child)
		if pruned != nil && (len(pruned.Elements) > 0 || len(pruned.Children) > 0) {
			validChildren = append(validChildren, pruned)
		}
	}
	b.Children = validChildren

	if len(b.Elements) == 0 && len(b.Children) == 0 && b.RawName != "Root" {
		return nil
	}

	return b
}

// sanitizeASTElementsAndBoundaries cleans labels and guarantees safe identifiers.
func sanitizeASTElementsAndBoundaries(ast *types.DiagramAST) {
	if ast == nil || ast.Root == nil {
		return
	}

	seenIDs := make(map[string]int)

	var walk func(b *types.ASTBoundary)
	walk = func(b *types.ASTBoundary) {
		if b == nil {
			return
		}

		b.ID = ensureUniqueID(b.ID, seenIDs)
		b.Label = sanitizeLabel(b.Label)

		for _, elem := range b.Elements {
			elem.ID = ensureUniqueID(elem.ID, seenIDs)
			elem.Name = sanitizeLabel(elem.Name)
			for i := range elem.Fields {
				elem.Fields[i].Name = sanitizeLabel(elem.Fields[i].Name)
			}
			for i := range elem.Methods {
				elem.Methods[i].Name = sanitizeLabel(elem.Methods[i].Name)
			}
		}

		for _, child := range b.Children {
			walk(child)
		}
	}

	walk(ast.Root)
}

func ensureUniqueID(id string, seen map[string]int) string {
	clean := sanitizeName(id)
	if clean == "" {
		clean = "elem"
	}
	count, exists := seen[clean]
	if !exists {
		seen[clean] = 1
		return clean
	}
	seen[clean] = count + 1
	return fmt.Sprintf("%s_%d", clean, count)
}

func sanitizeLabel(s string) string {
	return sanitizeMermaidLabel(s)
}

// pruneDanglingEdges drops edges that point to non-existent elements.
func pruneDanglingEdges(ast *types.DiagramAST) []types.ASTEdge {
	if ast == nil {
		return nil
	}

	validIDs := make(map[string]bool)
	var collectIDs func(b *types.ASTBoundary)
	collectIDs = func(b *types.ASTBoundary) {
		if b == nil {
			return
		}
		if b.ID != "" {
			validIDs[b.ID] = true
		}
		for _, elem := range b.Elements {
			validIDs[elem.ID] = true
		}
		for _, child := range b.Children {
			collectIDs(child)
		}
	}
	collectIDs(ast.Root)

	var validEdges []types.ASTEdge
	for _, edge := range ast.Edges {
		if validIDs[edge.SourceID] && validIDs[edge.TargetID] {
			validEdges = append(validEdges, edge)
		}
	}
	return validEdges
}
