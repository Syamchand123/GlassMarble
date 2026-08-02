package stage3

import (
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage2"
	"github.com/stretchr/testify/assert"
)

func TestIndexGenerics(t *testing.T) {
	output := &Stage3Output{
		GlobalDefinitionIndex: map[string][]*stage2.GASTNode{
			"List<Item>": {
				{Name: "List", Kind: "class"},
			},
			"Map<K,V>": {
				{Name: "Map", Kind: "interface"},
			},
			"Repository[T]": {
				{Name: "Repository", Kind: "class"},
			},
			"PlainType": {
				{Name: "PlainType"},
			},
		},
	}

	IndexGenerics(output)

	assert.Equal(t, "List<Item>", output.GenericsRegistry["List"])
	assert.Equal(t, "Map<K,V>", output.GenericsRegistry["Map"])
	assert.Equal(t, "Repository[T]", output.GenericsRegistry["Repository"])

	_, exists := output.GenericsRegistry["PlainType"]
	assert.False(t, exists)
}

func TestIndexGenericsEmpty(t *testing.T) {
	output := &Stage3Output{}

	IndexGenerics(output)

	assert.NotNil(t, output.GenericsRegistry)
	assert.Empty(t, output.GenericsRegistry)
}
