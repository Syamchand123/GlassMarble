package invalidator

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDeriveArchEvents_Keywords(t *testing.T) {
	events := deriveArchEvents("Split auth service into layers\n\nExtract database interface to break cycle")
	assert.Contains(t, events, "COMPONENT_SPLIT")
	assert.Contains(t, events, "CYCLE_INTRODUCED")
	assert.Contains(t, events, "NEW_DATABASE_LAYER")
	assert.Contains(t, events, "LAYER_VIOLATION")
	assert.Contains(t, events, "SERVICE_ADDED")
	assert.Contains(t, events, "INTERFACE_CHANGED")
	assert.Contains(t, events, "SECURITY_BOUNDARY_CHANGED")

	// Deterministic order: fixed keyword order regardless of message order.
	again := deriveArchEvents("auth cycle split")
	assert.Equal(t, []string{"COMPONENT_SPLIT", "CYCLE_INTRODUCED", "SECURITY_BOUNDARY_CHANGED"}, again)

	// No keywords → no events.
	assert.Empty(t, deriveArchEvents("Fix typo in README"))
	assert.Empty(t, deriveArchEvents(""))
}
