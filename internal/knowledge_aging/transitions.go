package knowledge_aging

import (
	"fmt"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/archmodel"
	"github.com/Syamchand123/GlassMarble/internal/config"
	"github.com/Syamchand123/GlassMarble/internal/developer_memory"
)

// Rule IDs. These are stable identifiers attached to every transition
// event's evidence (SourceRule reference) so each state change is traceable
// to the deterministic rule that produced it.
const (
	ruleCurrentDeprecated        = "aging.transition.current_deprecated"
	ruleCurrentRemoved           = "aging.transition.current_removed"
	ruleExperimentalDeprecated   = "aging.transition.experimental_deprecated"
	ruleExperimentalRemoved      = "aging.transition.experimental_removed"
	ruleExperimentalPromoted     = "aging.transition.experimental_promoted"
	ruleDeprecatedHistorical     = "aging.transition.deprecated_historical"
	ruleDeprecatedRestored       = "aging.transition.deprecated_restored"
)

// transitionDecision is the outcome of evaluating the rules for one
// component: the new state ("" = no change) plus the rule and reason that
// justify it.
type transitionDecision struct {
	newState developer_memory.KnowledgeState
	ruleID   string
	reason   string
}

// determineNextState applies the knowledge aging state-transition rules to one
// component (master plan §9.4, presence-based interpretation):
//
//	CURRENT     + absent from graph + grace elapsed + referenced elsewhere → DEPRECATED
//	CURRENT     + absent from graph + grace elapsed + unreferenced         → REMOVED
//	EXPERIMENTAL + absent + grace elapsed + referenced                     → DEPRECATED
//	EXPERIMENTAL + absent + grace elapsed + unreferenced                   → REMOVED
//	EXPERIMENTAL + present + ≥N confirming events                          → CURRENT
//	DEPRECATED  + present in the graph again                               → CURRENT
//	DEPRECATED  + unobserved longer than the cooling period                → HISTORICAL
//
// The stale-grace period (cfg.StaleGraceDaysValue, default 7 days, 0 =
// immediate) is the deterministic guard against transient absence: a
// component missing from one watch-mode snapshot must not be demoted until
// it has also been unobserved in memory for the grace window (the plan's
// "two consecutive snapshots" requirement expressed in time).
//
// REMOVED / HISTORICAL / UNKNOWN are terminal: aging never silently
// reverts them — restoring knowledge is a user correction (convention learning) or a
// new SERVICE_ADDED event (developer memory). DEPRECATED is NOT terminal: a
// component that reappears in the graph is restored to CURRENT so the
// graph and the memory can never disagree.
//
// Freshness never demotes a component that is still present in the graph:
// a long-stable, still-true component stays CURRENT; freshness decays the
// RANKING of its claims instead. Every decision carries a human-readable
// reason naming the concrete facts it was computed from.
func determineNextState(
	compName string,
	history developer_memory.ComponentHistory,
	stale map[string]StaleEntity,
	mem *developer_memory.DeveloperMemory,
	snap *archmodel.ArchSnapshot,
	cfg *config.AgingConfig,
	now time.Time,
) transitionDecision {
	isStale := false
	if _, ok := stale[compName]; ok {
		isStale = true
	}

	switch history.State {
	case developer_memory.StateActive:
		if !isStale {
			return transitionDecision{} // still present → still current
		}
		if !staleGraceElapsed(history, cfg, now) {
			return transitionDecision{} // absent but within the grace period
		}
		if referencedBy(mem, snap, compName) {
			return transitionDecision{
				newState: developer_memory.StateDeprecated,
				ruleID:   ruleCurrentDeprecated,
				reason:   "component no longer detected in the current graph (beyond the stale-grace period) but still referenced by memory or detected patterns",
			}
		}
		return transitionDecision{
			newState: developer_memory.StateRemoved,
			ruleID:   ruleCurrentRemoved,
			reason:   "component no longer detected in the current graph (beyond the stale-grace period) with no remaining references",
		}

	case developer_memory.StateExperimental:
		if isStale {
			if !staleGraceElapsed(history, cfg, now) {
				return transitionDecision{} // absent but within the grace period
			}
			if referencedBy(mem, snap, compName) {
				return transitionDecision{
					newState: developer_memory.StateDeprecated,
					ruleID:   ruleExperimentalDeprecated,
					reason:   "experimental component no longer detected in the current graph (beyond the stale-grace period) but still referenced",
				}
			}
			return transitionDecision{
				newState: developer_memory.StateRemoved,
				ruleID:   ruleExperimentalRemoved,
				reason:   "experimental component no longer detected in the current graph (beyond the stale-grace period) with no remaining references",
			}
		}
		if len(history.Events) >= cfg.ExperimentalPromotionEvents {
			return transitionDecision{
				newState: developer_memory.StateActive,
				ruleID:   ruleExperimentalPromoted,
				reason:   fmt.Sprintf("promoted after %d confirming events while still present in the graph", len(history.Events)),
			}
		}

	case developer_memory.StateDeprecated:
		// Restore is keyed on real presence in the graph, NOT on "not
		// stale": DetectStaleEntities only tracks CURRENT / EXPERIMENTAL
		// components, so a DEPRECATED component that is still absent would
		// otherwise be "not stale" and flip back to CURRENT on every run
		// (DEPRECATED → CURRENT → DEPRECATED oscillation). Presence is the
		// only signal that reopens a deprecated component.
		if isPresentInGraph(snap, compName) {
			return transitionDecision{
				newState: developer_memory.StateActive,
				ruleID:   ruleDeprecatedRestored,
				reason:   "component detected again in the current graph after being deprecated",
			}
		}
		// LastSeen is the timestamp of the deprecation transition event
		// itself (the most recent observation), so the elapsed time is the
		// time spent unobserved while deprecated.
		if !history.LastSeen.IsZero() &&
			now.Sub(history.LastSeen) > time.Duration(cfg.DeprecationToHistoricalDays)*24*time.Hour {
			return transitionDecision{
				newState: developer_memory.StateHistorical,
				ruleID:   ruleDeprecatedHistorical,
				reason: fmt.Sprintf("deprecated and unobserved for %.0f days (> %d)",
					now.Sub(history.LastSeen).Hours()/24, cfg.DeprecationToHistoricalDays),
			}
		}
	}

	return transitionDecision{}
}

// isPresentInGraph reports whether the component is detected in the current
// snapshot (as a component or a pattern member). Absence of a snapshot is
// absence of information, never presence.
func isPresentInGraph(snap *archmodel.ArchSnapshot, compName string) bool {
	if snap == nil {
		return false
	}
	return indexPresentEntities(snap).hasComponent(compName)
}

// staleGraceElapsed reports whether the stale-grace period has elapsed for a
// component that is absent from the current snapshot. With a grace of 0 the
// period is always elapsed (immediate transitions); a component with no
// observation timestamp at all is treated as elapsed so it can never be
// stuck in a grace limbo.
func staleGraceElapsed(history developer_memory.ComponentHistory, cfg *config.AgingConfig, now time.Time) bool {
	if cfg.StaleGraceDaysValue() <= 0 {
		return true
	}
	if history.LastSeen.IsZero() {
		return true
	}
	return now.Sub(history.LastSeen) >= time.Duration(cfg.StaleGraceDaysValue())*24*time.Hour
}

// referencedBy reports whether anything still points at compName: a
// CURRENT claim (global or component-scoped) naming it as subject or
// object, or a detected pattern in the current snapshot that includes it.
func referencedBy(mem *developer_memory.DeveloperMemory, snap *archmodel.ArchSnapshot, compName string) bool {
	if mem != nil {
		for _, claim := range mem.GlobalMemory {
			if claim.State != developer_memory.StateActive {
				continue
			}
			if claim.Subject == compName || claim.Object == compName {
				return true
			}
		}
		for _, history := range mem.ComponentMemory {
			for _, claim := range history.Claims {
				if claim.State != developer_memory.StateActive {
					continue
				}
				if claim.Subject == compName || claim.Object == compName {
					return true
				}
			}
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
