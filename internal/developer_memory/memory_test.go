package developer_memory

import "testing"

// TestKnowledgeStateValues pins the persisted string values. These are part
// of the on-disk contract (master plan §1.5) — renaming them corrupts
// existing memory.
func TestKnowledgeStateValues(t *testing.T) {
	tests := map[string]KnowledgeState{
		"CURRENT":      StateActive,
		"DEPRECATED":   StateDeprecated,
		"REMOVED":      StateRemoved,
		"HISTORICAL":   StateHistorical,
		"EXPERIMENTAL": StateExperimental,
		"UNKNOWN":      StateUnknown,
	}
	if len(tests) != 6 {
		t.Fatalf("expected exactly 6 knowledge states, have %d", len(tests))
	}
	for want, state := range tests {
		if string(state) != want {
			t.Errorf("state %q persisted as %q", want, string(state))
		}
	}
}

// TestClaimKindValues pins the persisted ClaimKind strings.
func TestClaimKindValues(t *testing.T) {
	tests := map[string]ClaimKind{
		"FACT":            ClaimFact,
		"EXPLICIT_REASON": ClaimExplicitReason,
		"INFERENCE":       ClaimInference,
		"SPECULATION":     ClaimSpeculation,
	}
	if len(tests) != 4 {
		t.Fatalf("expected exactly 4 claim kinds, have %d", len(tests))
	}
	for want, kind := range tests {
		if string(kind) != want {
			t.Errorf("claim kind %q persisted as %q", want, string(kind))
		}
	}
}
