// Package grounding — permalink.go
// Implements self-healing code permalinks (file.go#L42-L89) grounded in the AKG.
package grounding

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/link"
	docconfig "github.com/Syamchand123/GlassMarble/internal/doc_engine/config"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/patcher"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/storage"
)

// CodeLocation represents the current physical file coordinate of a symbol.
type CodeLocation struct {
	File      string
	LineStart int
	LineEnd   int
}

// FormatPermalink formats a file path and line span into a GitHub-compatible anchor link.
func FormatPermalink(filePath string, lineStart, lineEnd int) string {
	if lineStart <= 0 {
		return filePath
	}
	if lineEnd <= lineStart {
		return fmt.Sprintf("%s#L%d", filePath, lineStart)
	}
	return fmt.Sprintf("%s#L%d-L%d", filePath, lineStart, lineEnd)
}

// ResolvePermalink looks up a symbol FQN in the AKG and returns its current
// physical file and line range.
func ResolvePermalink(fqn string, graph *akg.CodePropertyGraph) (file string, startLine, endLine int, found bool) {
	if graph == nil || graph.Nodes == nil || fqn == "" {
		return "", 0, 0, false
	}

	// 1. Direct node ID lookup
	if node, ok := graph.Nodes.Get(fqn); ok && node != nil {
		return node.FileSpec.Path, node.FileSpec.LineStart, node.FileSpec.LineEnd, true
	}

	// 2. Lookup by suffix (e.g. "ValidateToken" or "pkg.ValidateToken")
	var match *link.ResolvedNode
	graph.Nodes.Iterate(func(id string, n *link.ResolvedNode) {
		if match != nil || n == nil {
			return
		}
		if id == fqn || strings.HasSuffix(id, "::"+fqn) || n.Name == fqn {
			match = n
		}
	})

	if match != nil {
		return match.FileSpec.Path, match.FileSpec.LineStart, match.FileSpec.LineEnd, true
	}

	return "", 0, 0, false
}

var markdownPermalinkRegex = regexp.MustCompile(`\[([^\]]+)\]\(([^)#]+\.[a-zA-Z0-9]+)#L([0-9]+)(?:-L([0-9]+))?\)`)

// UpdatePermalinksInMarkdown scans markdown text for existing file permalink links
// (e.g. `[ValidateToken](internal/auth/jwt.go#L42-L89)`) and updates the line
// numbers to reflect their current location in the AKG without invoking any LLM.
func UpdatePermalinksInMarkdown(markdown string, graph *akg.CodePropertyGraph) string {
	if graph == nil || graph.Nodes == nil || markdown == "" {
		return markdown
	}

	// Build a lookup index of (FilePath, SymbolName) -> (LineStart, LineEnd)
	symbolLocations := make(map[string]CodeLocation)
	graph.Nodes.Iterate(func(id string, n *link.ResolvedNode) {
		if n == nil || n.FileSpec.Path == "" {
			return
		}
		loc := CodeLocation{
			File:      n.FileSpec.Path,
			LineStart: n.FileSpec.LineStart,
			LineEnd:   n.FileSpec.LineEnd,
		}
		// Key by file + "::" + name
		key := n.FileSpec.Path + "::" + n.Name
		symbolLocations[key] = loc

		// Also key by just symbol name if unique
		if _, exists := symbolLocations[n.Name]; !exists {
			symbolLocations[n.Name] = loc
		}
	})

	return markdownPermalinkRegex.ReplaceAllStringFunc(markdown, func(match string) string {
		submatches := markdownPermalinkRegex.FindStringSubmatch(match)
		if len(submatches) < 4 {
			return match
		}
		text := submatches[1]
		file := submatches[2]
		oldStart, _ := strconv.Atoi(submatches[3])
		oldEnd := oldStart
		if len(submatches) > 4 && submatches[4] != "" {
			oldEnd, _ = strconv.Atoi(submatches[4])
		}

		// Try to find matching symbol by file + text
		key := file + "::" + text
		if loc, ok := symbolLocations[key]; ok && loc.LineStart > 0 {
			newLink := FormatPermalink(file, loc.LineStart, loc.LineEnd)
			return fmt.Sprintf("[%s](%s)", text, newLink)
		}

		// Try by text alone
		if loc, ok := symbolLocations[text]; ok && loc.LineStart > 0 && loc.File == file {
			newLink := FormatPermalink(file, loc.LineStart, loc.LineEnd)
			return fmt.Sprintf("[%s](%s)", text, newLink)
		}

		// If no symbol found, preserve original
		_ = oldEnd
		return match
	})
}

// HealAllManagedDocs recomputes every file#L permalink in each managed
// section of every doc in docs against a best-effort symbol table built by
// scanning the repo's Go AST once (exported idents → current line).
// Files are rewritten ONLY when healing changed something (atomic write via
// storage.AtomicWriteFile). It returns the total healed-link count.
func HealAllManagedDocs(repoRoot string, docs []docconfig.DocSpec) (int, error) {
	graph, err := buildASTSymbolGraph(repoRoot)
	if err != nil {
		return 0, err
	}
	total := 0
	for _, doc := range docs {
		if doc.TargetPath == "" {
			continue
		}
		abs := filepath.Join(repoRoot, doc.TargetPath)
		data, readErr := os.ReadFile(abs)
		if readErr != nil {
			// Doc file does not exist yet — nothing to heal.
			continue
		}
		managed := managedSectionIDs(doc)
		parsed := patcher.ParseMarkdown(string(data))
		changed := false
		zones := make([]patcher.Zone, len(parsed.Zones))
		copy(zones, parsed.Zones)
		for i := range zones {
			z := &zones[i]
			if z.SectionID == "" || z.Kind != patcher.ZoneManaged {
				continue
			}
			if managed != nil {
				if _, ok := managed[z.SectionID]; !ok {
					continue
				}
			}
			healed, n := healMarkdownWithCount(z.Content, graph)
			if n > 0 {
				z.Content = healed
				total += n
				changed = true
			}
		}
		if !changed {
			continue
		}
		if _, wErr := storage.AtomicWriteFile(abs, []byte(patcher.Reconstruct(zones))); wErr != nil {
			return total, wErr
		}
	}
	return total, nil
}

// managedSectionIDs returns the set of managed, non-frozen section IDs for a
// doc, or nil when the doc declares no sections (heal every managed zone).
func managedSectionIDs(doc docconfig.DocSpec) map[string]bool {
	if len(doc.Sections) == 0 {
		return nil
	}
	out := make(map[string]bool)
	for _, sec := range doc.Sections {
		if sec.Managed && !sec.Freeze {
			out[sec.ID] = true
		}
	}
	return out
}

// healMarkdownWithCount rewrites file#L permalink links via ResolvePermalink
// against graph, returning the healed markdown and the healed-link count.
func healMarkdownWithCount(markdown string, graph *akg.CodePropertyGraph) (string, int) {
	if graph == nil || graph.Nodes == nil || markdown == "" {
		return markdown, 0
	}
	count := 0
	healed := markdownPermalinkRegex.ReplaceAllStringFunc(markdown, func(match string) string {
		submatches := markdownPermalinkRegex.FindStringSubmatch(match)
		if len(submatches) < 4 {
			return match
		}
		text := submatches[1]
		file := submatches[2]
		// Prefer the file-scoped FQN, fall back to the bare symbol name.
		candidates := []string{file + "::" + text, text}
		for _, fqn := range candidates {
			if f, start, end, found := ResolvePermalink(fqn, graph); found && start > 0 && f == file {
				newLink := FormatPermalink(file, start, end)
				fixed := fmt.Sprintf("[%s](%s)", text, newLink)
				if fixed != match {
					count++
				}
				return fixed
			}
		}
		return match
	})
	return healed, count
}

// buildASTSymbolGraph scans repoRoot's Go sources once and builds a
// best-effort symbol table (exported idents → current line) as an AKG graph
// so ResolvePermalink can locate current file#line coordinates.
func buildASTSymbolGraph(repoRoot string) (*akg.CodePropertyGraph, error) {
	graph := akg.NewCodePropertyGraph("heal-scan")
	if repoRoot == "" {
		return graph, nil
	}
	fset := token.NewFileSet()
	walkErr := filepath.WalkDir(repoRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // best-effort: skip unreadable entries
		}
		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == "vendor" || name == "node_modules" || name == ".glassmarble" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(d.Name()), ".go") {
			return nil
		}
		rel, relErr := filepath.Rel(repoRoot, path)
		if relErr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		f, parseErr := parser.ParseFile(fset, path, nil, 0)
		if parseErr != nil || f == nil {
			return nil
		}
		record := func(name string, pos token.Pos, kind string) {
			if !ast.IsExported(name) {
				return
			}
			line := fset.Position(pos).Line
			if line <= 0 {
				return
			}
			id := rel + "::" + name
			if _, exists := graph.Nodes.Get(id); exists {
				return
			}
			graph.Nodes = graph.Nodes.Set(id, &link.ResolvedNode{
				ID:   id,
				Name: name,
				Kind: kind,
				FileSpec: link.LocationMeta{
					Path:      rel,
					LineStart: line,
					LineEnd:   line,
				},
			})
		}
		for _, decl := range f.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				if d.Name != nil {
					kind := "FUNCTION"
					if d.Recv != nil {
						kind = "METHOD"
					}
					record(d.Name.Name, d.Name.Pos(), kind)
				}
			case *ast.GenDecl:
				kind := "CONSTANT"
				if d.Tok == token.VAR {
					kind = "VARIABLE"
				} else if d.Tok == token.TYPE {
					kind = "TYPE"
				}
				for _, spec := range d.Specs {
					switch s := spec.(type) {
					case *ast.TypeSpec:
						if s.Name != nil {
							k := kind
							if k == "TYPE" {
								k = "STRUCT"
							}
							record(s.Name.Name, s.Name.Pos(), k)
						}
					case *ast.ValueSpec:
						for _, name := range s.Names {
							if name != nil {
								record(name.Name, name.Pos(), kind)
							}
						}
					}
				}
			}
		}
		return nil
	})
	if walkErr != nil {
		return graph, walkErr
	}
	return graph, nil
}
