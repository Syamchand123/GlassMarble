// Package config — validator.go
// Standalone validation entry points for DocSpec completeness.
//
// The core schema checks live in loader.go (validateDocsConfig) so that
// every LoadDocsConfig call is validated by construction. This file provides
// the explicit validator surface required by the master plan §14 package
// architecture: actionable warnings for missing audience/purpose and
// section-instruction gaps that loader treats as non-fatal.
package config

import (
	"fmt"
	"strings"
)

// ValidationWarning is a non-fatal spec completeness finding.
type ValidationWarning struct {
	DocID     string `json:"doc_id"`
	SectionID string `json:"section_id,omitempty"`
	Field     string `json:"field"`
	Message   string `json:"message"`
}

func (w ValidationWarning) Error() string {
	if w.SectionID != "" {
		return fmt.Sprintf("doc %q section %q: %s is missing (%s)", w.DocID, w.SectionID, w.Field, w.Message)
	}
	return fmt.Sprintf("doc %q: %s is missing (%s)", w.DocID, w.Field, w.Message)
}

// ValidateSpecCompleteness returns non-fatal warnings for specs that load
// successfully but lack the high-signal fields the LLM actuator needs to
// produce audience-appropriate prose (Pillar 2 / Section 7.1).
func ValidateSpecCompleteness(cfg *DocsConfig) []ValidationWarning {
	var warnings []ValidationWarning
	for _, doc := range cfg.Documents {
		if strings.TrimSpace(doc.Purpose) == "" {
			warnings = append(warnings, ValidationWarning{
				DocID:   doc.ID,
				Field:   "purpose",
				Message: "add a one-sentence intent so the renderer can shape tone; see plan §7.1 dimension 1",
			})
		}
		if strings.TrimSpace(doc.Audience) == "" {
			warnings = append(warnings, ValidationWarning{
				DocID:   doc.ID,
				Field:   "audience",
				Message: "name the primary reader (e.g. 'on-call SRE'); defaults to generic contributor voice",
			})
		}
		if len(doc.Sections) == 0 {
			warnings = append(warnings, ValidationWarning{
				DocID:   doc.ID,
				Field:   "sections",
				Message: "no sections declared; engine will treat the whole file as one managed block",
			})
		}
		for _, sec := range doc.Sections {
			if sec.Managed && !sec.Freeze && strings.TrimSpace(sec.Instruction) == "" {
				warnings = append(warnings, ValidationWarning{
					DocID:     doc.ID,
					SectionID: sec.ID,
					Field:     "instruction",
					Message:   "managed section without instruction renders as a plain symbol table",
				})
			}
			if len(sec.GroundWith) == 0 && sec.Managed && !sec.Freeze {
				warnings = append(warnings, ValidationWarning{
					DocID:     doc.ID,
					SectionID: sec.ID,
					Field:     "ground_with",
					Message:   "no grounding directives; defaults to signatures+comments",
				})
			}
		}
	}
	return warnings
}
