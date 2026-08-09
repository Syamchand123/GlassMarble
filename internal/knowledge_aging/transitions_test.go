package knowledge_aging

import (
	"testing"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/archmodel"
	"github.com/Syamchand123/GlassMarble/internal/config"
	"github.com/Syamchand123/GlassMarble/internal/developer_memory"
	"github.com/Syamchand123/GlassMarble/internal/evidence"
)

// determineNextStateCase seeds the pure function with a crafted component
// history, an optional referencing claim, and an optional snapshot, and
// asserts the resulting decision ("" = no change).
func runDecision(
	state developer_memory.KnowledgeState,
	events []string,
	lastSeen time.Time,
	referencing bool,
	present bool,
	cfg *config.AgingConfig,
	now time.Time,
) transitionDecision {
	history := developer_memory.ComponentHistory{
		Name:     "Svc",
		State:    state,
		LastSeen: lastSeen,
		Events:   events,
	}
	var mem *developer_memory.DeveloperMemory
	if referencing {
		mem = &developer_memory.DeveloperMemory{
			GlobalMemory: []developer_memory.KnowledgeClaim{
				{
					ID:        "c-ref",
					Subject:   "Svc",
					Predicate: "depends_on",
					Object:    "Db",
					State:     developer_memory.StateActive,
					ValidFrom: baseNow.Add(-30 * 24 * time.Hour),
					Evidence:  evidence.NewBundle(evidence.EvidenceItem{Source: evidence.SourceGit, Confidence: 1.0, Timestamp: baseNow.Add(-30 * 24 * time.Hour)}),
				},
			},
		}
	}
	stale := map[string]StaleEntity{}
	snapComps := []string{"Other"}
	if present {
		snapComps = []string{"Svc"}
	} else {
		stale["Svc"] = StaleEntity{Name: "Svc", LastSeen: lastSeen, Reason: "absent"}
	}
	return determineNextState("Svc", history, stale, mem, testSnapshot(now, snapComps...), cfg, now)
}

func TestDetermineNextState(t *testing.T) {
	grace := func(days int) *config.AgingConfig {
		c := config.DefaultAgingConfig()
		c.StaleGraceDays = daysPtr(days)
		return c
	}
	longAbsent := baseNow.Add(-200 * 24 * time.Hour)

	tests := []struct {
		name       string
		state      developer_memory.KnowledgeState
		events     []string
		lastSeen   time.Time
		referencing bool
		present    bool
		cfg        *config.AgingConfig
		want       developer_memory.KnowledgeState
		wantRule   string
	}{
		{"current + present → no change", developer_memory.StateActive, []string{"e1"}, baseNow.Add(-time.Hour), false, true, grace(7), "", ""},
		{"current + absent + within grace → no change", developer_memory.StateActive, []string{"e1"}, baseNow.Add(-2 * 24 * time.Hour), false, false, grace(7), "", ""},
		{"current + absent + grace elapsed + referenced → DEPRECATED", developer_memory.StateActive, []string{"e1"}, baseNow.Add(-10 * 24 * time.Hour), true, false, grace(7), developer_memory.StateDeprecated, ruleCurrentDeprecated},
		{"current + absent + grace elapsed + unreferenced → REMOVED", developer_memory.StateActive, []string{"e1"}, baseNow.Add(-10 * 24 * time.Hour), false, false, grace(7), developer_memory.StateRemoved, ruleCurrentRemoved},
		{"current + absent + zero grace → immediate DEPRECATED", developer_memory.StateActive, []string{"e1"}, baseNow.Add(-time.Hour), true, false, grace(0), developer_memory.StateDeprecated, ruleCurrentDeprecated},
		{"experimental + absent + elapsed + referenced → DEPRECATED", developer_memory.StateExperimental, []string{"e1"}, baseNow.Add(-10 * 24 * time.Hour), true, false, grace(7), developer_memory.StateDeprecated, ruleExperimentalDeprecated},
		{"experimental + absent + elapsed + unreferenced → REMOVED", developer_memory.StateExperimental, []string{"e1"}, baseNow.Add(-10 * 24 * time.Hour), false, false, grace(7), developer_memory.StateRemoved, ruleExperimentalRemoved},
		{"experimental + present + enough events → CURRENT", developer_memory.StateExperimental, []string{"e1", "e2", "e3"}, baseNow.Add(-time.Hour), false, true, grace(7), developer_memory.StateActive, ruleExperimentalPromoted},
		{"experimental + present + too few events → no change", developer_memory.StateExperimental, []string{"e1"}, baseNow.Add(-time.Hour), false, true, grace(7), "", ""},
		{"deprecated + present → CURRENT (restore)", developer_memory.StateDeprecated, []string{"e1", "e2"}, baseNow.Add(-20 * 24 * time.Hour), false, true, grace(7), developer_memory.StateActive, ruleDeprecatedRestored},
		{"deprecated + absent + under cooling period → no change", developer_memory.StateDeprecated, []string{"e1", "e2"}, baseNow.Add(-90 * 24 * time.Hour), false, false, grace(7), "", ""},
		{"deprecated + absent + cooling period elapsed → HISTORICAL", developer_memory.StateDeprecated, []string{"e1", "e2"}, longAbsent, false, false, grace(7), developer_memory.StateHistorical, ruleDeprecatedHistorical},
		{"removed → terminal, no change", developer_memory.StateRemoved, []string{"e1"}, longAbsent, false, false, grace(7), "", ""},
		{"historical → terminal, no change", developer_memory.StateHistorical, []string{"e1"}, longAbsent, false, false, grace(7), "", ""},
		{"unknown → terminal, no change", developer_memory.StateUnknown, []string{"e1"}, longAbsent, false, false, grace(7), "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dec := runDecision(tt.state, tt.events, tt.lastSeen, tt.referencing, tt.present, tt.cfg, baseNow)
			if tt.want == "" {
				if dec.newState != "" {
					t.Fatalf("want no transition, got %s (%s)", dec.newState, dec.ruleID)
				}
				return
			}
			if dec.newState != tt.want || dec.ruleID != tt.wantRule {
				t.Fatalf("got %s via %s, want %s via %s\nreason: %s", dec.newState, dec.ruleID, tt.want, tt.wantRule, dec.reason)
			}
			if dec.reason == "" {
				t.Error("every decision must carry a human-readable reason")
			}
		})
	}
}

func TestStaleGraceElapsed(t *testing.T) {
	cfg := func(days int) *config.AgingConfig {
		c := config.DefaultAgingConfig()
		c.StaleGraceDays = daysPtr(days)
		return c
	}
	tests := []struct {
		name     string
		grace    int
		lastSeen time.Time
		want     bool
	}{
		{"zero grace is always elapsed", 0, baseNow.Add(-time.Second), true},
		{"zero lastSeen is always elapsed", 7, time.Time{}, true},
		{"exactly at the boundary is elapsed", 7, baseNow.Add(-7 * 24 * time.Hour), true},
		{"just inside the boundary is not elapsed", 7, baseNow.Add(-(7*24*time.Hour - time.Minute)), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := developer_memory.ComponentHistory{LastSeen: tt.lastSeen}
			if got := staleGraceElapsed(h, cfg(tt.grace), baseNow); got != tt.want {
				t.Errorf("staleGraceElapsed = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestReferencedBy(t *testing.T) {
	snapWithPattern := testSnapshot(baseNow, "Svc")
	snapWithPattern.Patterns = []archmodel.DetectedPattern{
		{Name: "p1", Components: []string{"Svc"}},
	}
	mem := &developer_memory.DeveloperMemory{
		GlobalMemory: []developer_memory.KnowledgeClaim{
			{
				ID:        "c1",
				Subject:   "Svc",
				Predicate: "depends_on",
				Object:    "Db",
				State:     developer_memory.StateActive,
				ValidFrom: baseNow.Add(-time.Hour),
				Evidence:  evidence.NewBundle(evidence.EvidenceItem{Source: evidence.SourceGit, Confidence: 1.0, Timestamp: baseNow.Add(-time.Hour)}),
			},
		},
		ComponentMemory: map[string]developer_memory.ComponentHistory{
			"Other": {
				Name: "Other",
				Claims: []developer_memory.KnowledgeClaim{
					{
						ID:        "c2",
						Subject:   "Svc",
						Predicate: "depends_on",
						Object:    "Db",
						State:     developer_memory.StateActive,
						ValidFrom: baseNow.Add(-time.Hour),
						Evidence:  evidence.NewBundle(evidence.EvidenceItem{Source: evidence.SourceGit, Confidence: 1.0, Timestamp: baseNow.Add(-time.Hour)}),
					},
				},
			},
		},
	}

	tests := []struct {
		name string
		mem  *developer_memory.DeveloperMemory
		snap *archmodel.ArchSnapshot
		want bool
	}{
		{"referenced by a global claim", mem, testSnapshot(baseNow), true},
		{"referenced by a component-scoped claim", mem, testSnapshot(baseNow), true},
		{"referenced by a detected pattern", nil, snapWithPattern, true},
		{"no references anywhere", nil, testSnapshot(baseNow), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := referencedBy(tt.mem, tt.snap, "Svc"); got != tt.want {
				t.Errorf("referencedBy = %v, want %v", got, tt.want)
			}
		})
	}
}
