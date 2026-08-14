package akg

import (
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/link"
	"github.com/Syamchand123/GlassMarble/internal/product/ont"
	"github.com/stretchr/testify/assert"
)

// TestVocabularyConstants verifies that the core ontology/vocabulary prefixes,
// predicates, and edge taxonomy views conform to the GlassMarble spec (Phase D).
func TestVocabularyConstants(t *testing.T) {
	assert.NotEmpty(t, ont.PrefixGM)
	assert.NotEmpty(t, ont.PrefixExt)
	assert.NotEmpty(t, ont.PrefixFile)

	// Structural edge family
	assert.Equal(t, "structural", link.ViewOfEdgeType(link.EdgeContains))
	assert.Equal(t, "structural", link.ViewOfEdgeType(link.EdgeBelongsTo))
	assert.Equal(t, "structural", link.ViewOfEdgeType(link.EdgeDependsOn))
	assert.Equal(t, "structural", link.ViewOfEdgeType(link.EdgeImplements))
	assert.Equal(t, "structural", link.ViewOfEdgeType(link.EdgeExtends))

	// Dynamic edge family
	assert.Equal(t, "dynamic", link.ViewOfEdgeType(link.EdgeControlFlow))
	assert.Equal(t, "dynamic", link.ViewOfEdgeType(link.EdgeDataFlow))
	assert.Equal(t, "dynamic", link.ViewOfEdgeType(link.EdgePointsTo))

	// Security edge family
	assert.Equal(t, "security", link.ViewOfEdgeType(link.EdgeSecuritySink))
}
