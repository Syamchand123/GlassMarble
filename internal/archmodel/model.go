// Package archmodel is the central type registry for all GlassMarble V2 cross-phase types.
//
// WHY THIS PACKAGE EXISTS (execution_plan.md §2, Problem P1):
//
//	The original design placed ArchEvent in arch_intelligence, ArchSnapshot in
//	arch_timeline, and TimelineEntry in developer_memory, creating a compile-blocking
//	circular import cycle:
//	    arch_intelligence → arch_timeline → developer_memory → arch_intelligence
//	The corrected execution plan resolves this by extracting ALL shared types into this
//	single leaf package. No phase package imports another — they all import archmodel.
//
// WHAT IS IN HERE:
//   - ArchEvent + EventKind enum          — one architectural change event
//   - ArchSnapshot + AKGJSON embed        — point-in-time architecture state
//   - SnapshotDelta                       — diff between two snapshots
//   - TimelineEntry                       — human-readable timeline row
//   - ArchMetrics + MetricDelta           — quantitative architecture measurements
//   - DetectedComponent + ComponentKind   — inferred architectural unit
//   - DetectedPattern + PatternKind       — recognised architecture pattern
//   - ArchSmell + SmellKind + Severity    — anti-pattern / architectural problem
//   - StaleEntity                         — component present in memory but gone from graph
//
// DEPENDENCY DIRECTION (strict, cycle-free):
//
//	evidence (leaf) → archmodel (leaf) → phase packages → cmd/
package archmodel

import (
	"github.com/Syamchand123/GlassMarble/internal/evidence"
	"time"
)

// EventKind describes what type of architectural change occurred.
// Values are stored as strings in JSON — MUST NOT be renamed after first release.
type EventKind string

const (
	EventServiceAdded      EventKind = "SERVICE_ADDED"
	EventServiceRemoved    EventKind = "SERVICE_REMOVED"
	EventServiceSplit      EventKind = "SERVICE_SPLIT"
	EventServiceMerged     EventKind = "SERVICE_MERGED"
	EventDependencyAdded   EventKind = "DEPENDENCY_ADDED"
	EventDependencyRemoved EventKind = "DEPENDENCY_REMOVED"
	EventPatternDetected   EventKind = "PATTERN_DETECTED"
	EventPatternLost       EventKind = "PATTERN_REMOVED"
	EventSmellDetected     EventKind = "SMELL_DETECTED"
	EventSmellResolved     EventKind = "SMELL_RESOLVED"
	EventBoundaryCreated   EventKind = "BOUNDARY_CREATED"
	EventAsyncIntroduced   EventKind = "ASYNC_PATTERN_INTRODUCED"
	EventCachingAdded      EventKind = "CACHING_ADDED"
	EventSecurityAdded     EventKind = "SECURITY_LAYER_ADDED"
	EventAPIAdded          EventKind = "API_ENDPOINT_ADDED"
	EventDataStoreAdded    EventKind = "DATA_STORE_ADDED"
	EventCouplingIncreased EventKind = "COUPLING_INCREASED"
	EventCouplingDecreased EventKind = "COUPLING_DECREASED"
	EventDeadCodeDetected  EventKind = "DEAD_CODE_DETECTED"
	EventCycleIntroduced   EventKind = "CYCLE_INTRODUCED"
	EventCycleResolved     EventKind = "CYCLE_RESOLVED"
	EventLayerViolation    EventKind = "LAYER_VIOLATION"
	// EventStateChanged marks a knowledge-state transition performed by
	// knowledge aging — e.g. CURRENT → DEPRECATED. The new
	// state is carried machine-readably in a single well-known tag of the
	// form "state=<STATE>" (see StateTag / StateFromTags); the description
	// carries the human-readable reason. Transition events are appended to
	// events.jsonl so a memory rebuild from the WAL reproduces aging
	// states exactly (reproducibility principle, master plan §9.4).
	EventStateChanged EventKind = "STATE_CHANGE"
)

// ArchEvent is one architectural change event — the fundamental atom of memory.
// MUST carry non-empty Evidence.Bundle. ID is deterministic sha256[0:16].
type ArchEvent struct {
	ID   string    `json:"id"`
	Kind EventKind `json:"kind"`
	// CommitHash is the commit the analysis ran AT, which is not necessarily
	// the commit that caused the change. Structural events come from diffing
	// two snapshots, and when analysis has not run for a while that diff spans
	// every commit since BaseCommitHash. Attributing the whole span to HEAD is
	// unavoidable without re-analysing each commit, but silently presenting it
	// as a single commit's work is not: read the pair, not CommitHash alone.
	CommitHash string `json:"commit_hash"`
	// BaseCommitHash is the commit of the snapshot this event was diffed
	// against. Empty when there was no baseline (the first analysis). When it
	// differs from the previous commit of CommitHash, this event describes a
	// range rather than one commit.
	BaseCommitHash string          `json:"base_commit_hash,omitempty"`
	Timestamp      time.Time       `json:"timestamp"`
	Title          string          `json:"title"`
	Description    string          `json:"description"`
	AffectedIDs    []string        `json:"affected_ids"`
	Components     []string        `json:"components"`
	Evidence       evidence.Bundle `json:"evidence"`
	Intent         string          `json:"intent"`
	IntentSrc      evidence.Source `json:"intent_src"`
	Tags           []string        `json:"tags"`
	RelatedPRs     []string        `json:"related_prs"`
	RelatedIssues  []string        `json:"related_issues"`
	ValidFrom      time.Time       `json:"valid_from"`
	ValidUntil     *time.Time      `json:"valid_until,omitempty"`
}

// StateTagPrefix is the well-known tag prefix STATE_CHANGE events use to
// carry the new knowledge state machine-readably ("state=<STATE>"). The
// state string is the persisted KnowledgeState value ("CURRENT",
// "DEPRECATED", "REMOVED", "HISTORICAL", "EXPERIMENTAL", "UNKNOWN").
const StateTagPrefix = "state="

// StateTag builds the well-known state tag for a STATE_CHANGE event.
func StateTag(state string) string {
	return StateTagPrefix + state
}

// StateFromTags extracts the knowledge state carried by a STATE_CHANGE
// event's tags. Returns "" when no state tag is present.
func StateFromTags(tags []string) string {
	for _, t := range tags {
		if len(t) > len(StateTagPrefix) && t[:len(StateTagPrefix)] == StateTagPrefix {
			return t[len(StateTagPrefix):]
		}
	}
	return ""
}

// ComponentKind classifies a detected architectural unit.
type ComponentKind string

const (
	ComponentService        ComponentKind = "SERVICE"
	ComponentModule         ComponentKind = "MODULE"
	ComponentBoundedContext ComponentKind = "BOUNDED_CONTEXT"
	ComponentLayer          ComponentKind = "LAYER"
	ComponentFeature        ComponentKind = "FEATURE"
	ComponentExternal       ComponentKind = "EXTERNAL_DEPENDENCY"
)

// DetectedComponent is a logical architectural unit inferred from graph topology.
// component inference: Louvain community detection + directory prefix analysis. No LLM.
type DetectedComponent struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Kind        ComponentKind   `json:"kind"`
	NodeIDs     []string        `json:"node_ids"`
	Directories []string        `json:"directories"`
	Evidence    evidence.Bundle `json:"evidence"`
	Confidence  float64         `json:"confidence"`
	// Dependencies lists the IDs of the components this one depends on
	// (distinct structural edges). Filled by component inference; used by event
	// generation to detect component-level dependency changes.
	Dependencies []string `json:"dependencies,omitempty"`
	// Ca and Ce are the component's afferent/efferent coupling in the
	// component graph; Instability = Ce/(Ca+Ce). Filled by intelligence metrics/component inference.
	Ca          int     `json:"ca,omitempty"`
	Ce          int     `json:"ce,omitempty"`
	Instability float64 `json:"instability,omitempty"`
}

// PatternKind identifies a recognised architectural pattern.
type PatternKind string

const (
	PatternCleanArchitecture PatternKind = "CLEAN_ARCHITECTURE"
	PatternHexagonal         PatternKind = "HEXAGONAL"
	PatternMicroservices     PatternKind = "MICROSERVICES"
	PatternMonolith          PatternKind = "MONOLITH"
	PatternLayered           PatternKind = "LAYERED"
	PatternCQRS              PatternKind = "CQRS"
	PatternDDD               PatternKind = "DDD"
	PatternEventDriven       PatternKind = "EVENT_DRIVEN"
	PatternRepository        PatternKind = "REPOSITORY_PATTERN"
	PatternCRUD              PatternKind = "CRUD"
	PatternGateway           PatternKind = "API_GATEWAY"
	PatternSaga              PatternKind = "SAGA"
)

// DetectedPattern is an architectural pattern matched by pattern detection.
// Must carry non-empty Evidence.Bundle.
type DetectedPattern struct {
	Kind        PatternKind     `json:"kind"`
	Name        string          `json:"name"`
	Components  []string        `json:"components"`
	Confidence  float64         `json:"confidence"`
	Evidence    evidence.Bundle `json:"evidence"`
	Description string          `json:"description"`
}

// SmellKind identifies an architectural anti-pattern.
type SmellKind string

const (
	SmellGodService          SmellKind = "GOD_SERVICE"
	SmellGodObject           SmellKind = "GOD_OBJECT"
	SmellCyclicDependency    SmellKind = "CYCLIC_DEPENDENCY"
	SmellDeadCode            SmellKind = "DEAD_CODE"
	SmellLayerViolation      SmellKind = "LAYER_VIOLATION"
	SmellTightCoupling       SmellKind = "TIGHT_COUPLING"
	SmellLowCohesion         SmellKind = "LOW_COHESION"
	SmellGodPackage          SmellKind = "GOD_PACKAGE"
	SmellUnstableAbstraction SmellKind = "UNSTABLE_ABSTRACTION"
	SmellFeatureEnvy         SmellKind = "FEATURE_ENVY"
	SmellChattyService       SmellKind = "CHATTY_SERVICE"
)

// Severity classifies an ArchSmell's urgency.
type Severity string

const (
	SeverityLow      Severity = "LOW"
	SeverityMedium   Severity = "MEDIUM"
	SeverityHigh     Severity = "HIGH"
	SeverityCritical Severity = "CRITICAL"
)

// ArchSmell is an architectural anti-pattern detected in smell detection.
// Must carry non-empty Evidence.Bundle.
type ArchSmell struct {
	Kind        SmellKind       `json:"kind"`
	Title       string          `json:"title"`
	Severity    Severity        `json:"severity"`
	AffectedIDs []string        `json:"affected_ids"`
	Evidence    evidence.Bundle `json:"evidence"`
	Suggestion  string          `json:"suggestion"`
}

// HotspotEntry is one high-centrality node from PageRank analysis.
type HotspotEntry struct {
	NodeID   string  `json:"node_id"`
	Name     string  `json:"name"`
	PageRank float64 `json:"page_rank"`
	FanIn    int     `json:"fan_in"`
	FanOut   int     `json:"fan_out"`
}

// ArchMetrics holds quantitative architecture quality measurements from architecture intelligenceA.
// Derived entirely from the CodePropertyGraph — no LLM.
type ArchMetrics struct {
	TotalNodes                  int     `json:"total_nodes"`
	TotalEdges                  int     `json:"total_edges"`
	GraphDensity                float64 `json:"graph_density"`
	MaxFanIn                    int     `json:"max_fan_in"`
	MaxFanOut                   int     `json:"max_fan_out"`
	AvgFanIn                    float64 `json:"avg_fan_in"`
	AvgFanOut                   float64 `json:"avg_fan_out"`
	AfferentCoupling            float64 `json:"afferent_coupling"`
	EfferentCoupling            float64 `json:"efferent_coupling"`
	Instability                 float64 `json:"instability"`
	LCOM4                       float64 `json:"lcom4"`
	CyclomaticMax               int     `json:"cyclomatic_max"`
	CyclomaticAvg               float64 `json:"cyclomatic_avg"`
	StronglyConnectedComponents int     `json:"sccs"`
	CycleCount                  int     `json:"cycle_count"`
	MaxCycleLength              int     `json:"max_cycle_length"`
	DeadCodeNodeCount           int     `json:"dead_code_node_count"`
	// EntrypointCount is how many entrypoints the reachability sweep found
	// in the graph. When it is 0, ReachableFromEntrypoints and
	// DeadCodeNodeCount are undefined, not zero: with no roots to walk from
	// there is no evidence of deadness either way.
	EntrypointCount int `json:"entrypoint_count"`
	// ReachableFromEntrypoints is the fraction of code units (functions,
	// methods, types, modules outside excluded paths) reachable from an
	// entrypoint or exposed as library API surface. Read it only when
	// EntrypointCount > 0.
	ReachableFromEntrypoints float64        `json:"reachable_from_entrypoints"`
	TopHotspots              []HotspotEntry `json:"top_hotspots"`
	LayerViolationCount      int            `json:"layer_violation_count"`
}

// MetricDelta is the signed diff between two ArchMetrics. Stored inside SnapshotDelta.
type MetricDelta struct {
	DensityDelta   float64 `json:"density_delta"`
	CycleDelta     int     `json:"cycle_delta"`
	ViolationDelta int     `json:"violation_delta"`
	CouplingTrend  string  `json:"coupling_trend"`
	CohesionTrend  string  `json:"cohesion_trend"`
	SummaryLine    string  `json:"summary_line"`
}

// ArchSnapshot captures the full architecture state at one commit.
// Written to .glassmarble/snapshots/snap_{id[0:8]}.json by arch_timeline.SnapshotStore.
//
// CRITICAL: AKGJSON embeds the FULL CodePropertyGraph (exec_plan §3 P2 fix) so
// --diff and --replay compare real graph state, not summaries.
// Skip-write: if TopologyHash matches the previous snapshot, no file is written.
type ArchSnapshot struct {
	ID         string    `json:"id"`
	CommitHash string    `json:"commit_hash"`
	Version    string    `json:"version,omitempty"`
	Timestamp  time.Time `json:"timestamp"`
	// Order is the commit's position in git history (git rev-list --count),
	// used by the snapshot store to order snapshots when several commits
	// share the same author timestamp (sub-second commit bursts). 0 means
	// unknown (e.g. uncommitted watch-mode states).
	Order        int64               `json:"order,omitempty"`
	NodeCount    int                 `json:"node_count"`
	EdgeCount    int                 `json:"edge_count"`
	Components   []DetectedComponent `json:"components"`
	Patterns     []DetectedPattern   `json:"patterns"`
	Smells       []ArchSmell         `json:"smells"`
	Metrics      ArchMetrics         `json:"metrics"`
	TopologyHash string              `json:"topology_hash"`
	AKGJSON      []byte              `json:"akg_json,omitempty"`
}

// SnapshotDelta is the architectural diff between two ArchSnapshots.
type SnapshotDelta struct {
	BaseSnapshot      string      `json:"base_snapshot"`
	HeadSnapshot      string      `json:"head_snapshot"`
	CommitsInRange    []string    `json:"commits_in_range"`
	Events            []ArchEvent `json:"events"`
	AddedComponents   []string    `json:"added_components"`
	RemovedComponents []string    `json:"removed_components"`
	PatternChanges    []string    `json:"pattern_changes"`
	SmellChanges      []string    `json:"smell_changes"`
	MetricDelta       MetricDelta `json:"metric_delta"`
}

// TimelineEntry is one human-readable row in the architecture evolution timeline.
// Built by developer_memory.Builder.ProcessEvents(), read by arch_timeline.RenderTimeline().
type TimelineEntry struct {
	Timestamp   time.Time `json:"timestamp"`
	CommitHash  string    `json:"commit_hash"`
	Version     string    `json:"version,omitempty"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	EventKind   EventKind `json:"event_kind"`
	Components  []string  `json:"components"`
	Intent      string    `json:"intent,omitempty"`
	Tags        []string  `json:"tags"`
}

// StaleEntity represents a component in memory but no longer in the graph.
// Used by knowledge_aging (deferred to v2.1); type defined here for future use.
type StaleEntity struct {
	Name     string    `json:"name"`
	LastSeen time.Time `json:"last_seen"`
	Reason   string    `json:"reason"`
}
