package arch_intelligence

import (
	"github.com/Syamchand123/GlassMarble/internal/config"
	"github.com/Syamchand123/GlassMarble/internal/drift"
)

// LayerAssigner assigns nodes to architectural layers using the drift
// package's LayerIndex, extended with layer ordering and forbidden pairs so
// pattern PR-01 and smell SD-04 can score consistency and violations.
type LayerAssigner struct {
	index     *drift.LayerIndex
	order     map[string]int
	forbidden map[string]bool // "src\x00tgt"
}

// NewLayerAssigner builds a LayerAssigner from layer definitions. When no
// layers are defined, Configured() reports false and Assign returns "".
func NewLayerAssigner(layers []config.DriftLayer) *LayerAssigner {
	a := &LayerAssigner{
		index:     &drift.LayerIndex{Layers: layers},
		order:     make(map[string]int, len(layers)),
		forbidden: make(map[string]bool),
	}
	for i, l := range layers {
		a.order[l.Name] = i
	}
	return a
}

// Configured reports whether any layers are declared.
func (a *LayerAssigner) Configured() bool {
	return a != nil && a.index != nil && len(a.index.Layers) > 0
}

// Assign returns the layer name for a file path ("" when unlayered).
func (a *LayerAssigner) Assign(path string) string {
	if a == nil || a.index == nil {
		return ""
	}
	return a.index.AssignLayer(path)
}

// WithForbidden sets the forbidden dependency pairs used by IsForbidden.
func (a *LayerAssigner) WithForbidden(rules []config.ForbiddenDepRule) *LayerAssigner {
	if a == nil {
		return nil
	}
	for _, rule := range rules {
		if rule.Source != "" && rule.Target != "" {
			a.forbidden[rule.Source+"\x00"+rule.Target] = true
		}
	}
	return a
}

// IsForbidden reports whether the src->tgt pair is declared forbidden.
func (a *LayerAssigner) IsForbidden(srcLayer, tgtLayer string) bool {
	return a != nil && a.forbidden[srcLayer+"\x00"+tgtLayer]
}

// IsUpward reports whether srcLayer depends on tgtLayer against the declared
// order: layers listed earlier are outer; an outer layer may depend on inner
// ones, so an edge from a deeper layer to a shallower one is a violation.
func (a *LayerAssigner) IsUpward(srcLayer, tgtLayer string) bool {
	si, ok1 := a.order[srcLayer]
	ti, ok2 := a.order[tgtLayer]
	return ok1 && ok2 && si > ti
}
