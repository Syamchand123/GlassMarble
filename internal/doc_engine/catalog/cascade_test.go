package catalog

import (
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/doc_engine/config"
	"github.com/stretchr/testify/assert"
)

func TestClassifyDocLevel(t *testing.T) {
	assert.Equal(t, DocLevelRoot, ClassifyDocLevel(&config.DocSpec{TargetPath: "README.md"}))
	assert.Equal(t, DocLevelRoot, ClassifyDocLevel(&config.DocSpec{TargetPath: "docs/index.md"}))

	assert.Equal(t, DocLevelAggregate, ClassifyDocLevel(&config.DocSpec{TargetPath: "docs/architecture.md"}))
	assert.Equal(t, DocLevelAggregate, ClassifyDocLevel(&config.DocSpec{
		TargetPath: "docs/system.md",
		Archetype:  "architecture",
	}))
	assert.Equal(t, DocLevelAggregate, ClassifyDocLevel(&config.DocSpec{
		TargetPath: "docs/overview.md",
		Scope:      config.ScopeRule{Paths: []string{"internal/**"}},
	}))

	assert.Equal(t, DocLevelLeaf, ClassifyDocLevel(&config.DocSpec{
		TargetPath: "docs/components/ai_engine.md",
		Scope:      config.ScopeRule{Paths: []string{"internal/ai_engine/**"}},
	}))
}

func TestShouldCascadeToAggregate(t *testing.T) {
	// Critical architectural events trigger cascade
	assert.True(t, ShouldCascadeToAggregate([]string{"SERVICE_ADDED"}, "ADD_FEATURE"))
	assert.True(t, ShouldCascadeToAggregate([]string{"CYCLE_INTRODUCED"}, "FIX_BUG"))
	assert.True(t, ShouldCascadeToAggregate([]string{"LAYER_VIOLATION"}, "REFACTOR"))

	// Non-critical event with normal intent does not cascade
	assert.False(t, ShouldCascadeToAggregate([]string{"MINOR_NODE_ADDED"}, "FIX_BUG"))
	assert.False(t, ShouldCascadeToAggregate([]string{}, "ADD_FEATURE"))
}

func TestEnforceBudgetAndSorting(t *testing.T) {
	sections := []config.DirtySectionRef{
		{DocID: "doc-internal", SectionID: "sec-4", Priority: 4},
		{DocID: "doc-sec", SectionID: "sec-1", Priority: 1},
		{DocID: "doc-api", SectionID: "sec-2", Priority: 2},
		{DocID: "doc-config", SectionID: "sec-5", Priority: 5},
		{DocID: "doc-err", SectionID: "sec-3", Priority: 3},
	}

	// Budget of 3: only top 3 priorities should survive (priorities 1, 2, 3)
	budgeted := EnforceBudget(sections, 3)
	assert.Len(t, budgeted, 3)
	assert.Equal(t, 1, budgeted[0].Priority)
	assert.Equal(t, 2, budgeted[1].Priority)
	assert.Equal(t, 3, budgeted[2].Priority)
	assert.Equal(t, "doc-sec", budgeted[0].DocID)
	assert.Equal(t, "doc-api", budgeted[1].DocID)
	assert.Equal(t, "doc-err", budgeted[2].DocID)
}
