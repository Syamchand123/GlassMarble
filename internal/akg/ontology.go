package akg

import (
	_ "embed"
	"regexp"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/product/ont"
)

// EmbeddedOntology is the full GlassMarble AKG ontology, embedded at build
// time from ontology.ttl (AUDIT Issue 3 Phase 3A-1). It is the single source
// of truth for the serializer vocabulary: every predicate emitted by
// mapEdgeTypeToPredicate, every gm:<key> property emitted by the serializer,
// and every class emitted by mapKindToClass MUST be declared here.
// The ontology_conformance_test.go suite enforces this invariant.
//
//go:embed ontology.ttl
var EmbeddedOntology string

// OntologyEmbedded reports whether the ontology was successfully embedded at
// build time. If false, the package was built without the TTL asset and the
// conformance suite should fail loudly.
func OntologyEmbedded() bool {
	return strings.Contains(EmbeddedOntology, "@prefix "+ont.PrefixGM)
}

var ontologyDeclLine = regexp.MustCompile(`(?m)^` + ont.PrefixGM + `([A-Za-z0-9_]+) a (rdf:Property|rdfs:Class|owl:Ontology)\b`)

// ontologyDeclaredTerms returns the set of gm: terms (without prefix) that are
// formally declared in the embedded ontology as a property, class, or ontology.
func ontologyDeclaredTerms() map[string]bool {
	terms := make(map[string]bool)
	for _, m := range ontologyDeclLine.FindAllStringSubmatch(EmbeddedOntology, -1) {
		terms[m[1]] = true
	}
	return terms
}

// isOntologyTermDeclared reports whether the given gm: term (without prefix)
// is declared in the embedded ontology. rdfs: terms are always considered
// declared (they are part of the fixed RDFS vocabulary).
func isOntologyTermDeclared(term string) bool {
	if strings.HasPrefix(term, "rdfs:") || strings.HasPrefix(term, "rdf:") {
		return true
	}
	return ontologyDeclaredTerms()[strings.TrimPrefix(term, ont.PrefixGM)]
}
