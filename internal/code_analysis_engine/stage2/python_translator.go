package stage2

import (
	"path/filepath"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage1"
)

type PythonTranslator struct{}

func (t *PythonTranslator) CoerceToken(tok stage1.RichToken, parent *stage1.RichToken, fileRelPath string) *GASTNode {
	node := baseNode(tok, fileRelPath)
	extractGenericTypesAndDecorators(node, tok.Content)

	switch tok.Kind {
	case stage1.TokenImport:
		node.Type = GASTImport
		node.Kind = "import"
		node.Name = cleanImportPath(tok.Content)
	case stage1.TokenDeclaration:
		// __init__.py and package-level files: emit a GASTNamespace for the Python module
		if tok.Type == "module" || (strings.Contains(fileRelPath, "__init__") && tok.Type == "decorated_definition") {
			node.Type = GASTNamespace
			node.Kind = "package"
			node.Name = pythonPackageFromPath(fileRelPath)
			break
		}
		if tok.Type == "class_definition" {
			node.Type = GASTTypeDeclaration
			node.Kind = "class"
			if strings.Contains(tok.Content, "@dataclass") || strings.Contains(tok.Content, "@attr") {
				node.Properties["is_dataclass"] = "true"
			}
		} else if strings.Contains(tok.Type, "field") || strings.Contains(tok.Type, "property") || strings.Contains(tok.Type, "attribute") || tok.Type == "typed_parameter" {
			node.Type = GASTField
			node.Kind = "field"
		} else if strings.Contains(tok.Type, "parameter") || strings.Contains(tok.Type, "argument") {
			node.Type = GASTParameter
			node.Kind = "parameter"
		} else if isControlFlowType(tok.Type) {
			node.Type = GASTControlFlow
			node.Kind = tok.Type
			node.Visibility = "internal"
		} else {
			node.Type = GASTFunction
			node.Kind = "function"
		}
		if node.Type != GASTControlFlow {
			node.Visibility = resolvePythonVisibility(tok.Name)
			setDeclarationFQN(node, fileRelPath, tok.Name)
		}
	case stage1.TokenCall:
		node.Type = GASTCallExpression
		node.Kind = "call"
	}
	return node
}

func resolvePythonVisibility(name string) string {
	if strings.HasPrefix(name, "__") && strings.HasSuffix(name, "__") {
		return "public"
	}
	if strings.HasPrefix(name, "__") {
		return "private"
	}
	if strings.HasPrefix(name, "_") {
		return "internal"
	}
	return "public"
}

// pythonPackageFromPath derives a Python package name from the file path.
// e.g. "myapp/utils/__init__.py" -> "myapp.utils", "myapp/service.py" -> "myapp.service"
func pythonPackageFromPath(relPath string) string {
	relPath = filepath.ToSlash(relPath)
	relPath = strings.TrimSuffix(relPath, "/__init__.py")
	relPath = strings.TrimSuffix(relPath, ".py")
	return strings.ReplaceAll(relPath, "/", ".")
}
