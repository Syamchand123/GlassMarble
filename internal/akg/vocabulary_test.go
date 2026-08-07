package akg

import (
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage4"
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
	assert.Equal(t, "structural", stage4.ViewOfEdgeType(stage4.EdgeContains))
	assert.Equal(t, "structural", stage4.ViewOfEdgeType(stage4.EdgeBelongsTo))
	assert.Equal(t, "structural", stage4.ViewOfEdgeType(stage4.EdgeDependsOn))
	assert.Equal(t, "structural", stage4.ViewOfEdgeType(stage4.EdgeImplements))
	assert.Equal(t, "structural", stage4.ViewOfEdgeType(stage4.EdgeExtends))

	// Dynamic edge family
	assert.Equal(t, "dynamic", stage4.ViewOfEdgeType(stage4.EdgeControlFlow))
	assert.Equal(t, "dynamic", stage4.ViewOfEdgeType(stage4.EdgeDataFlow))
	assert.Equal(t, "dynamic", stage4.ViewOfEdgeType(stage4.EdgePointsTo))

	// Security edge family
	assert.Equal(t, "security", stage4.ViewOfEdgeType(stage4.EdgeSecuritySink))
}
