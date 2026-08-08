package knowledge_aging

import (
	"time"

	"github.com/Syamchand123/GlassMarble/internal/archmodel"
	"github.com/Syamchand123/GlassMarble/internal/developer_memory"
)

// Ager is responsible for applying freshness decay and state transitions to the memory.
type Ager struct {
	memoryStore *developer_memory.DeveloperMemory
}

// NewAger creates a new Ager.
func NewAger(mem *developer_memory.DeveloperMemory) *Ager {
	return &Ager{memoryStore: mem}
}

// TransitionResult captures the result of evaluating transitions for a component.
type TransitionResult struct {
	Component string
	OldState  developer_memory.KnowledgeState
	NewState  developer_memory.KnowledgeState
	Reason    string
	Event     *archmodel.ArchEvent
}

// Age runs the aging process: updates freshness scores and evaluates state transitions.
func (a *Ager) Age(currentSnap *archmodel.ArchSnapshot, now time.Time) []TransitionResult {
	var transitions []TransitionResult

	if a.memoryStore == nil {
		return transitions
	}

	// 1. Apply freshness decay to all global claims
	for i, claim := range a.memoryStore.GlobalMemory {
		a.memoryStore.GlobalMemory[i].FreshnessScore = FreshnessScore(claim, now)
	}

	// Apply freshness decay to all component claims
	for compName, history := range a.memoryStore.ComponentMemory {
		for i, claim := range history.Claims {
			a.memoryStore.ComponentMemory[compName].Claims[i].FreshnessScore = FreshnessScore(claim, now)
		}
	}

	// 2. Evaluate State Transitions
	staleEntities := DetectStaleEntities(currentSnap, a.memoryStore)
	staleMap := make(map[string]StaleEntity)
	for _, se := range staleEntities {
		staleMap[se.Name] = se
	}

	for compName, history := range a.memoryStore.ComponentMemory {
		oldState := history.State

		newState, reason := determineNextState(compName, history, staleMap, currentSnap, now)

		if newState != oldState && newState != "" {
			// Actually mutate the memory State
			history.State = newState
			a.memoryStore.ComponentMemory[compName] = history

			transitions = append(transitions, TransitionResult{
				Component: compName,
				OldState:  oldState,
				NewState:  newState,
				Reason:    reason,
				Event: &archmodel.ArchEvent{
					ID:          "trans-" + compName + "-" + now.Format(time.RFC3339),
					Timestamp:   now,
					Kind:        archmodel.EventKind("STATE_CHANGE"),
					Title:       "State Transition: " + string(newState),
					Description: reason,
					Components:  []string{compName},
				},
			})
		}
	}

	return transitions
}

func determineNextState(
	compName string,
	history developer_memory.ComponentHistory,
	staleMap map[string]StaleEntity,
	currentSnap *archmodel.ArchSnapshot,
	now time.Time,
) (developer_memory.KnowledgeState, string) {

	isStale := false
	if _, ok := staleMap[compName]; ok {
		isStale = true
	}

	switch history.State {
	case developer_memory.StateActive:
		if isStale {
			// CURRENT -> DEPRECATED if still referenced, else CURRENT -> REMOVED
			if hasReferences(compName, currentSnap, history) {
				return developer_memory.StateDeprecated, "Component removed from graph but still referenced in memory"
			}
			return developer_memory.StateRemoved, "Component removed from graph with no references"
		}

		// CURRENT -> HISTORICAL if FreshnessScore < 0.2 (approximation based on claims)
		avgFreshness := 1.0
		if len(history.Claims) > 0 {
			total := 0.0
			for _, claim := range history.Claims {
				total += FreshnessScore(claim, now)
			}
			avgFreshness = total / float64(len(history.Claims))
		}
		if avgFreshness < 0.2 {
			return developer_memory.KnowledgeState("HISTORICAL"), "Knowledge freshness is very low (< 0.2)"
		}

	case developer_memory.StateExperimental:
		// EXPERIMENTAL -> CURRENT (Active) when present for > 3 consecutive commits
		// Without full commit history, we use event count
		if len(history.Events) >= 3 {
			return developer_memory.StateActive, "Promoted to Active after observing multiple confirming events"
		}

	case developer_memory.StateDeprecated:
		// DEPRECATED -> HISTORICAL when marked deprecated for > 6 months
		if now.Sub(history.LastSeen).Hours() > 24*180 {
			return developer_memory.KnowledgeState("HISTORICAL"), "Deprecated for > 6 months"
		}
	}

	return "", ""
}

func hasReferences(compName string, snap *archmodel.ArchSnapshot, history developer_memory.ComponentHistory) bool {
	for _, claim := range history.Claims {
		if claim.Object == compName && claim.State == developer_memory.StateActive {
			return true
		}
	}
	if snap != nil {
		for _, pat := range snap.Patterns {
			for _, pc := range pat.Components {
				if pc == compName {
					return true
				}
			}
		}
	}
	return false
}
