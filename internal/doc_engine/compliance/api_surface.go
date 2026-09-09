package compliance

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
)

// BoundaryViolation records a private or unexported primitive leaking across public API boundaries.
type BoundaryViolation struct {
	PublicFunction string `json:"public_function"`
	LeakedType     string `json:"leaked_type"`
	Package        string `json:"package"`
	SourcePath     string `json:"source_path"`
	Line           int    `json:"line"`
	Severity       string `json:"severity"` // ERROR, WARN
}

// BoundaryReport summarizes public, internal mesh, and private API surface tiers.
type BoundaryReport struct {
	PublicEdgeCount  int                 `json:"public_edge_count"`
	InternalMeshCount int                `json:"internal_mesh_count"`
	PrivateCount     int                 `json:"private_count"`
	Violations       []BoundaryViolation `json:"violations"`
}

// CheckAPISurfaceBoundaries audits all packages to ensure private primitives do not leak
// across public boundaries (Pillar 34).
func CheckAPISurfaceBoundaries(repoRoot string) (BoundaryReport, error) {
	report := BoundaryReport{}
	fset := token.NewFileSet()

	err := filepath.WalkDir(repoRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == ".glassmarble" || name == "vendor" || name == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		relPath, _ := filepath.Rel(repoRoot, path)
		relPath = filepath.ToSlash(relPath)

		isPublicEdge := strings.HasPrefix(relPath, "cmd/") || (!strings.Contains(relPath, "internal/") && !strings.Contains(relPath, "test"))
		isInternalMesh := strings.Contains(relPath, "internal/")

		node, parseErr := parser.ParseFile(fset, path, nil, 0)
		if parseErr != nil {
			return nil
		}

		for _, decl := range node.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}

			if !ast.IsExported(fn.Name.Name) {
				report.PrivateCount++
				continue
			}

			if isPublicEdge {
				report.PublicEdgeCount++
			} else if isInternalMesh {
				report.InternalMeshCount++
			}

			// Check for unexported return types or parameters
			if isPublicEdge && fn.Type.Results != nil {
				for _, res := range fn.Type.Results.List {
					typeStr := exprToString(res.Type)
					if typeStr != "" && !isExportedTypeName(typeStr) {
						pos := fset.Position(fn.Pos())
						report.Violations = append(report.Violations, BoundaryViolation{
							PublicFunction: fn.Name.Name,
							LeakedType:     typeStr,
							Package:        node.Name.Name,
							SourcePath:     relPath,
							Line:           pos.Line,
							Severity:       "WARN",
						})
					}
				}
			}
		}
		return nil
	})

	sort.Slice(report.Violations, func(i, j int) bool {
		return report.Violations[i].PublicFunction < report.Violations[j].PublicFunction
	})

	return report, err
}

func isExportedTypeName(s string) bool {
	s = strings.TrimPrefix(s, "*")
	parts := strings.Split(s, ".")
	target := parts[len(parts)-1]
	if target == "" {
		return true
	}
	// stdlib types (int, string, error, bool, etc.) are fine
	switch target {
	case "int", "int64", "int32", "string", "bool", "error", "byte", "rune", "float64", "float32", "any":
		return true
	}
	r := rune(target[0])
	return r >= 'A' && r <= 'Z'
}
