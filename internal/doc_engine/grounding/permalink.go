// Package grounding — permalink.go
// Implements self-healing code permalinks (file.go#L42-L89) grounded in the AKG.
package grounding

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/link"
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
