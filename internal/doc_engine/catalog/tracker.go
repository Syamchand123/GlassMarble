// Package catalog — tracker.go
// Implements an O(1) reverse-index mapping changed files and symbols
// to affected document sections.
package catalog

import (
	"strings"
	"sync"

	"github.com/Syamchand123/GlassMarble/internal/doc_engine/config"
)

// ReverseIndex provides fast lookups from modified code entities
// (file paths, symbol FQNs) to the document sections that document them.
type ReverseIndex struct {
	catalog *Catalog
	mu      sync.RWMutex

	// symbolToSections maps symbol FQN prefix or full FQN to dirty section references.
	symbolToSections map[string][]config.DirtySectionRef

	// fileToSections maps normalized file path to dirty section references.
	fileToSections map[string][]config.DirtySectionRef
}

// NewReverseIndex builds a ReverseIndex from the given Catalog.
func NewReverseIndex(cat *Catalog) *ReverseIndex {
	idx := &ReverseIndex{
		catalog:          cat,
		symbolToSections: make(map[string][]config.DirtySectionRef),
		fileToSections:   make(map[string][]config.DirtySectionRef),
	}
	idx.build()
	return idx
}

// build indexes all document sections by file path patterns and entry points.
func (idx *ReverseIndex) build() {
	if idx.catalog == nil {
		return
	}

	for _, doc := range idx.catalog.docs {
		// Index entry points
		for _, ep := range doc.Scope.EntryPoints {
			for _, sec := range doc.Sections {
				if !sec.Managed || sec.Freeze {
					continue
				}
				ref := config.DirtySectionRef{
					DocID:     doc.ID,
					DocPath:   doc.TargetPath,
					SectionID: sec.ID,
					Priority:  assignPriority(&doc, &sec),
					Reason:    "entry point match: " + ep,
				}
				idx.symbolToSections[ep] = append(idx.symbolToSections[ep], ref)
			}
		}
	}
}

// IndexSymbol associates a symbol FQN with a dirty section reference.
func (idx *ReverseIndex) IndexSymbol(fqn string, ref config.DirtySectionRef) {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	idx.symbolToSections[fqn] = append(idx.symbolToSections[fqn], ref)
}

// IndexFile associates a file path with a dirty section reference.
func (idx *ReverseIndex) IndexFile(filePath string, ref config.DirtySectionRef) {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	norm := normalizePath(filePath)
	idx.fileToSections[norm] = append(idx.fileToSections[norm], ref)
}

// FindDirtySectionsForFiles returns all managed document sections that
// track any of the provided changed file paths.
// Results are deduplicated by (DocID, SectionID).
func (idx *ReverseIndex) FindDirtySectionsForFiles(changedFiles []string) []config.DirtySectionRef {
	if idx.catalog == nil || len(changedFiles) == 0 {
		return nil
	}

	seen := make(map[string]bool)
	var dirty []config.DirtySectionRef

	for _, file := range changedFiles {
		normFile := normalizePath(file)

		// 1. Check direct cached file mapping
		idx.mu.RLock()
		cached, hasCached := idx.fileToSections[normFile]
		idx.mu.RUnlock()
		if hasCached {
			for _, ref := range cached {
				key := ref.DocID + "::" + ref.SectionID
				if !seen[key] {
					seen[key] = true
					dirty = append(dirty, ref)
				}
			}
		}

		// 2. Check catalog scope matching
		matchingDocs := idx.catalog.MatchingDocs(normFile)
		for _, doc := range matchingDocs {
			if len(doc.Sections) == 0 {
				// Whole document is managed
				key := doc.ID + "::"
				if !seen[key] {
					seen[key] = true
					dirty = append(dirty, config.DirtySectionRef{
						DocID:     doc.ID,
						DocPath:   doc.TargetPath,
						SectionID: "",
						Priority:  assignPriority(doc, nil),
						Reason:    "file changed in scope: " + normFile,
					})
				}
				continue
			}

			// Individual sections
			for _, sec := range doc.Sections {
				if !sec.Managed || sec.Freeze {
					continue
				}
				key := doc.ID + "::" + sec.ID
				if !seen[key] {
					seen[key] = true
					dirty = append(dirty, config.DirtySectionRef{
						DocID:     doc.ID,
						DocPath:   doc.TargetPath,
						SectionID: sec.ID,
						Priority:  assignPriority(doc, &sec),
						Reason:    "file changed in scope: " + normFile,
					})
				}
			}
		}
	}

	return dirty
}

// FindDirtySectionsForSymbol returns all sections affected by changes to
// a symbol with the given FQN (e.g. "internal/auth/jwt.go::ValidateToken").
func (idx *ReverseIndex) FindDirtySectionsForSymbol(fqn string) []config.DirtySectionRef {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	var results []config.DirtySectionRef
	seen := make(map[string]bool)

	// Direct match
	if refs, ok := idx.symbolToSections[fqn]; ok {
		for _, r := range refs {
			key := r.DocID + "::" + r.SectionID
			if !seen[key] {
				seen[key] = true
				results = append(results, r)
			}
		}
	}

	// File-part prefix match: "internal/auth/jwt.go::ValidateToken" -> file is "internal/auth/jwt.go"
	parts := strings.Split(fqn, "::")
	if len(parts) > 0 {
		filePart := parts[0]
		fileRefs := idx.FindDirtySectionsForFiles([]string{filePart})
		for _, r := range fileRefs {
			key := r.DocID + "::" + r.SectionID
			if !seen[key] {
				seen[key] = true
				results = append(results, r)
			}
		}
	}

	return results
}

// assignPriority calculates the dirty queue priority (1 = highest, 5 = lowest)
// based on doc tags and section properties.
func assignPriority(doc *config.DocSpec, sec *config.SectionSpec) int {
	if doc != nil {
		for _, tag := range doc.Tags {
			if tag == "security" || tag == "compliance" {
				return 1 // Security is highest priority
			}
		}
		if doc.Archetype == "security" {
			return 1
		}
	}

	if sec != nil {
		idLower := strings.ToLower(sec.ID)
		if strings.Contains(idLower, "security") || strings.Contains(idLower, "auth") {
			return 1
		}
		if strings.Contains(idLower, "api") || strings.Contains(idLower, "interface") || strings.Contains(idLower, "export") {
			return 2 // Public API interface changes
		}
		if strings.Contains(idLower, "error") || strings.Contains(idLower, "triage") {
			return 3 // Error triage
		}
		if strings.Contains(idLower, "config") || strings.Contains(idLower, "env") {
			return 5 // Config/environment reference
		}
	}

	return 4 // Default internal implementation
}
