package developer_memory

import (
	"testing"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/archmodel"
)

// TestComponentHistoryCarriesItsClaims pins the documented contract of
// ComponentHistory.Claims ("claims whose subject is this component").
//
// Regression: applyEvent appended derived claims to GlobalMemory only, so every
// ComponentHistory.Claims was empty. On this repository's own memory that was
// 0 of 24 components populated while 136 global claims existed, which silently
// emptied `gmb memory --component`, the aging pass's claim projection and
// knowledge_aging's referencedBy scan.
func TestComponentHistoryCarriesItsClaims(t *testing.T) {
	mem := &DeveloperMemory{ProjectID: "proj", ComponentMemory: make(map[string]ComponentHistory)}
	now := time.Now().UTC()

	applyEvent(mem, archmodel.ArchEvent{
		ID:         "ev-1",
		Kind:       archmodel.EventServiceAdded,
		Components: []string{"PaymentService", "OrderService"},
		Timestamp:  now,
	})

	if len(mem.GlobalMemory) == 0 {
		t.Fatal("expected global claims to be recorded")
	}
	for _, comp := range []string{"PaymentService", "OrderService"} {
		h, ok := mem.ComponentMemory[comp]
		if !ok {
			t.Fatalf("component %q missing from memory", comp)
		}
		if len(h.Claims) == 0 {
			t.Errorf("component %q has no claims; every derived claim went to GlobalMemory only", comp)
			continue
		}
		for _, c := range h.Claims {
			if c.Subject != comp {
				t.Errorf("component %q carries a claim whose subject is %q", comp, c.Subject)
			}
		}
	}
}

// TestComponentClaimsAreNotDuplicatedOnReplay guards WAL replay: rebuilding
// memory must not accumulate the same claim twice on a component.
func TestComponentClaimsAreNotDuplicatedOnReplay(t *testing.T) {
	ev := archmodel.ArchEvent{
		ID:         "ev-dup",
		Kind:       archmodel.EventServiceAdded,
		Components: []string{"Billing"},
		Timestamp:  time.Now().UTC(),
	}

	mem := &DeveloperMemory{ProjectID: "proj", ComponentMemory: make(map[string]ComponentHistory)}
	applyEvent(mem, ev)
	first := len(mem.ComponentMemory["Billing"].Claims)
	if first == 0 {
		t.Fatal("expected claims on first apply")
	}
	applyEvent(mem, ev)
	if got := len(mem.ComponentMemory["Billing"].Claims); got != first {
		t.Errorf("replaying the same event duplicated component claims: %d -> %d", first, got)
	}
}
