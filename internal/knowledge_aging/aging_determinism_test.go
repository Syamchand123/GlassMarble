package knowledge_aging

import (
	"testing"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/developer_memory"
)

// TestTransitionEventIDIsStableWithinADay pins the determinism the doc claims.
//
// Regression: the ID hashed now.Format(RFC3339Nano), so two aging passes over
// the same transition produced different IDs and the caller's have[ev.ID]
// dedup could never fire — a component flapping between states appended a new
// WAL record on every pass, forever.
func TestTransitionEventIDIsStableWithinADay(t *testing.T) {
	a := &Ager{}
	dec := transitionDecision{newState: developer_memory.StateDeprecated, reason: "absent from the graph"}

	morning := time.Date(2026, 3, 4, 9, 0, 0, 0, time.UTC)
	evening := time.Date(2026, 3, 4, 21, 45, 12, 987654321, time.UTC)

	first := a.transitionEvent("PaymentService", developer_memory.StateActive, dec, morning)
	second := a.transitionEvent("PaymentService", developer_memory.StateActive, dec, evening)

	if first.ID != second.ID {
		t.Errorf("same transition on the same day produced different IDs:\n %s\n %s", first.ID, second.ID)
	}
}

// TestTransitionEventIDVariesByTransition ensures the ID still distinguishes
// genuinely different transitions.
func TestTransitionEventIDVariesByTransition(t *testing.T) {
	a := &Ager{}
	now := time.Date(2026, 3, 4, 9, 0, 0, 0, time.UTC)
	base := a.transitionEvent("PaymentService", developer_memory.StateActive,
		transitionDecision{newState: developer_memory.StateDeprecated}, now)

	cases := map[string]string{
		"different component": a.transitionEvent("OrderService", developer_memory.StateActive,
			transitionDecision{newState: developer_memory.StateDeprecated}, now).ID,
		"different old state": a.transitionEvent("PaymentService", developer_memory.StateExperimental,
			transitionDecision{newState: developer_memory.StateDeprecated}, now).ID,
		"different new state": a.transitionEvent("PaymentService", developer_memory.StateActive,
			transitionDecision{newState: developer_memory.StateRemoved}, now).ID,
		"different day": a.transitionEvent("PaymentService", developer_memory.StateActive,
			transitionDecision{newState: developer_memory.StateDeprecated}, now.AddDate(0, 0, 1)).ID,
	}
	for label, id := range cases {
		if id == base.ID {
			t.Errorf("%s produced the same event ID as the base transition", label)
		}
	}
}
