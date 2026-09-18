// Package catalog manages the in-memory document registry, scope pattern
// matching, and entry-point validation for the Documentation Intelligence Engine.
package catalog

import (
	"path"
	"path/filepath"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/link"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/config"
)

// Catalog is the in-memory registry of all managed living documents.
type Catalog struct {
	docs []config.DocSpec
	byId map[string]*config.DocSpec
	byTarget map[string]*config.DocSpec
}

// New constructs a Catalog from a slice of DocSpecs.
func New(docs []config.DocSpec) *Catalog {
	c := &Catalog{
		docs:     make([]config.DocSpec, len(docs)),
		byId:     make(map[string]*config.DocSpec, len(docs)),
		byTarget: make(map[string]*config.DocSpec, len(docs)),
	}
	copy(c.docs, docs)

	for i := range c.docs {
		d := &c.docs[i]
		c.byId[d.ID] = d
		normTarget := normalizePath(d.TargetPath)
		c.byTarget[normTarget] = d
	}
	return c
}

// Docs returns all registered documents.
func (c *Catalog) Docs() []config.DocSpec {
	out := make([]config.DocSpec, len(c.docs))
	copy(out, c.docs)
	return out
}

// Get retrieves a document by its ID.
func (c *Catalog) Get(id string) *config.DocSpec {
	return c.byId[id]
}

// GetByTarget retrieves a document by its TargetPath.
func (c *Catalog) GetByTarget(targetPath string) *config.DocSpec {
	return c.byTarget[normalizePath(targetPath)]
}

// MatchesPath reports whether the given repository-relative file path falls
// within the scope of the document identified by docID.
// It checks scope.paths globs and excludes scope.exclude_paths globs.
func (c *Catalog) MatchesPath(docID string, filePath string) bool {
	doc := c.byId[docID]
	if doc == nil {
		return false
	}
	return matchesScope(&doc.Scope, filePath)
}

// MatchingDocs returns all DocSpecs whose scope includes filePath.
func (c *Catalog) MatchingDocs(filePath string) []*config.DocSpec {
	var matches []*config.DocSpec
	for i := range c.docs {
		d := &c.docs[i]
		if matchesScope(&d.Scope, filePath) {
			matches = append(matches, d)
		}
	}
	return matches
}

// ValidateEntryPoints verifies that all entry points declared in the catalog's
// documents exist in the provided AKG CodePropertyGraph.
// Returns a list of warning strings for any unresolvable entry points.
func (c *Catalog) ValidateEntryPoints(graph *akg.CodePropertyGraph) []string {
	if graph == nil || graph.Nodes == nil {
		return nil
	}

	var warnings []string
	for _, doc := range c.docs {
		for _, ep := range doc.Scope.EntryPoints {
			found := false
			graph.Nodes.Iterate(func(id string, _ *link.ResolvedNode) {
				if id == ep {
					found = true
				}
			})
			if !found {
				warnings = append(warnings, "entry point "+ep+" (in doc "+doc.ID+") not found in AKG")
			}
		}
	}
	return warnings
}

// MatchesScope reports whether path matches any pattern in scope.Paths,
// while not matching any pattern in scope.ExcludePaths.
func MatchesScope(scope *config.ScopeRule, filePath string) bool {
	if scope == nil {
		return false
	}
	normPath := normalizePath(filePath)

	// Check exclusion first.
	for _, excl := range scope.ExcludePaths {
		if GlobMatch(normalizePath(excl), normPath) {
			return false
		}
	}

	// Check inclusion.
	for _, incl := range scope.Paths {
		if GlobMatch(normalizePath(incl), normPath) {
			return true
		}
	}

	return false
}

func matchesScope(scope *config.ScopeRule, filePath string) bool {
	return MatchesScope(scope, filePath)
}

// GlobMatch matches pattern against path. Supports standard globs plus "**/"
// for recursive directory matching.
func GlobMatch(pattern, targetPath string) bool {
	pattern = filepath.ToSlash(pattern)
	targetPath = filepath.ToSlash(targetPath)

	// Direct match
	if pattern == targetPath {
		return true
	}

	// Wildcard all
	if pattern == "*" || pattern == "**" || pattern == "**/*" {
		return true
	}

	// Fast path: if pattern has a literal prefix before '*', check it first.
	if starIdx := strings.IndexByte(pattern, '*'); starIdx > 0 {
		prefix := pattern[:starIdx]
		if strings.HasSuffix(prefix, "/") && !strings.HasPrefix(targetPath, prefix) {
			return false
		}
	}

	// Pattern like "internal/**" -> matches anything starting with "internal/"
	if strings.HasSuffix(pattern, "/**") {
		prefix := strings.TrimSuffix(pattern, "/**")
		if targetPath == prefix || strings.HasPrefix(targetPath, prefix+"/") {
			return true
		}
	}

	// Pattern like "**/testutil/**" -> matches path containing "/testutil/" or starting with "testutil/"
	if strings.HasPrefix(pattern, "**/") && strings.HasSuffix(pattern, "/**") {
		segment := strings.TrimSuffix(strings.TrimPrefix(pattern, "**/"), "/**")
		if strings.Contains("/"+targetPath+"/", "/"+segment+"/") {
			return true
		}
	}

	// Standard path.Match (handles forward slashes portably across OSes)
	matched, err := path.Match(pattern, targetPath)
	if err == nil && matched {
		return true
	}

	// Check basename match if pattern is a file name like "*.go"
	if !strings.Contains(pattern, "/") {
		baseMatched, err := path.Match(pattern, path.Base(targetPath))
		if err == nil && baseMatched {
			return true
		}
	}

	return false
}

func normalizePath(p string) string {
	// Normalize Windows-style separators to forward slashes regardless of the
	// host OS, then clean. Using path.Clean (not filepath.Clean) keeps the
	// result slash-based and portable across platforms.
	return path.Clean(strings.ReplaceAll(p, "\\", "/"))
}
