package arch_intelligence

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/archmodel"
	"github.com/Syamchand123/GlassMarble/internal/config"
)

// Stage5Result holds all insights derived from a single graph snapshot.
type Stage5Result struct {
	// GraphHash is a stable fingerprint of the analyzed topology
	// (sorted node ids + edge signatures), used to detect unchanged graphs.
	GraphHash string                `json:"graph_hash"`
	Metrics   archmodel.ArchMetrics `json:"metrics"`

	Components        []archmodel.DetectedComponent `json:"components"`
	ComponentCoupling []ComponentCoupling           `json:"component_coupling"`

	Patterns []archmodel.DetectedPattern `json:"patterns"`
	Smells   []archmodel.ArchSmell       `json:"smells"`
}

// Engine coordinates the execution of graph analytics, pattern detection,
// and component inference (Stage 5A-5D). It is safe for concurrent Run calls:
// graph reads happen on a cached immutable snapshot, never on the live AKG.
type Engine struct {
	graph *akg.CodePropertyGraph
	cfg   *config.IntelligenceConfig
	clock func() time.Time
	logf  func(format string, args ...any)

	// forbiddenPairs are the drift-level forbidden dependencies injected by
	// the caller (drift config), used by layer-violation detection.
	forbiddenPairs []config.ForbiddenDepRule

	mu     sync.Mutex
	snap   *GraphSnapshot
	snapAt time.Time
}

// NewEngine creates a new Stage 5 engine with default config and clock.
func NewEngine(graph *akg.CodePropertyGraph) *Engine {
	return &Engine{
		graph: graph,
		cfg:   config.DefaultIntelligenceConfig(),
		clock: analysisClock(graph),
		logf:  func(string, ...any) {},
	}
}

// analysisClock returns a deterministic clock for the given graph. Evidence
// timestamps are anchored to the analyzed commit (stable bytes of the commit
// hash), so analyzing an identical graph produces byte-identical output —
// the reproducibility contract Stage 5 artifacts must satisfy. The derived
// instant is kept inside a sane window (a hash cannot be a real date). A
// graph without a commit hash anchors to the zero time. The wall clock is
// never used implicitly: callers that need real observation times inject
// WithClock.
func analysisClock(graph *akg.CodePropertyGraph) func() time.Time {
	if graph == nil {
		return func() time.Time { return time.Time{} }
	}
	hash := graph.CommitHash
	return func() time.Time {
		if hash == "" {
			return time.Time{}
		}
		sum := sha256.Sum256([]byte(hash))
		secs := binary.BigEndian.Uint64(sum[:8])
		// Anchor inside [2020-01-01, 2051-10-09]: far enough in the past to
		// read as a normal analysis time, far enough out to stay valid.
		const window = uint64(1_000_000_000)
		return time.Unix(int64(1_577_836_800+secs%window), 0).UTC()
	}
}

// EngineOption customizes an Engine.
type EngineOption func(*Engine)

// WithConfig sets the intelligence config (nil keeps defaults).
func WithConfig(cfg *config.IntelligenceConfig) EngineOption {
	return func(e *Engine) {
		if cfg != nil {
			e.cfg = cfg
		}
	}
}

// WithClock sets the clock used for evidence timestamps (deterministic in tests).
func WithClock(clock func() time.Time) EngineOption {
	return func(e *Engine) {
		if clock != nil {
			e.clock = clock
		}
	}
}

// WithLogger sets a sink for stage progress lines (nil disables logging).
func WithLogger(logf func(format string, args ...any)) EngineOption {
	return func(e *Engine) {
		if logf != nil {
			e.logf = logf
		}
	}
}

// WithLayerForbidden injects drift-level forbidden dependency pairs used by
// layer-violation detection (SD-04).
func WithLayerForbidden(rules []config.ForbiddenDepRule) EngineOption {
	return func(e *Engine) {
		e.forbiddenPairs = rules
	}
}

// NewEngineWithOptions builds an engine with explicit options.
func NewEngineWithOptions(graph *akg.CodePropertyGraph, opts ...EngineOption) *Engine {
	e := NewEngine(graph)
	for _, o := range opts {
		o(e)
	}
	return e
}

// Graph returns the underlying graph (read-only usage recommended).
func (e *Engine) Graph() *akg.CodePropertyGraph {
	return e.graph
}

// InvalidateSnapshot drops the cached snapshot so the next Run rebuilds it.
func (e *Engine) InvalidateSnapshot() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.snap = nil
}

// Snapshot returns the cached graph snapshot, rebuilding it when it is stale
// (older than cfg.SnapshotTTLSeconds) or when the graph was nil at engine
// creation. The snapshot is immutable; callers may cache it freely.
func (e *Engine) Snapshot() *GraphSnapshot {
	e.mu.Lock()
	defer e.mu.Unlock()
	ttl := time.Duration(e.cfg.SnapshotTTLSeconds) * time.Second
	if e.snap != nil && !e.cfg.SnapshotNoGraph && (ttl <= 0 || time.Since(e.snapAt) < ttl) {
		return e.snap
	}
	nodeCount := 0
	if e.graph != nil && e.graph.Nodes != nil {
		nodeCount = e.graph.Nodes.Len()
	}
	e.logf("Stage 5: capturing graph snapshot (%d nodes)", nodeCount)
	e.snap = NewGraphSnapshot(e.graph)
	e.snapAt = e.clock()
	return e.snap
}

// Run executes the full Stage 5 pipeline with a background context.
func (e *Engine) Run() Stage5Result {
	return e.RunContext(context.Background())
}

// rulesEnabled reports whether the given rule family is enabled by
// cfg.RunRules (empty means all enabled).
func rulesEnabled(cfg *config.IntelligenceConfig, family string) bool {
	if len(cfg.RunRules) == 0 {
		return true
	}
	for _, r := range cfg.RunRules {
		if r == family || r == "all" {
			return true
		}
	}
	return false
}

// RunContext executes the full Stage 5 pipeline: snapshot capture, metrics,
// component inference, coupling, pattern detection, smell detection. Phases
// check ctx cancellation between each stage.
func (e *Engine) RunContext(ctx context.Context) Stage5Result {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			e.logf("Stage 5: cancelled before start (%v)", err)
		}
	}
	cfg := e.cfg
	snap := e.Snapshot()
	result := Stage5Result{GraphHash: topologyHash(snap)}

	// 5A: quantitative metrics.
	if ctx != nil && ctx.Err() != nil {
		return result
	}
	e.logf("Stage 5A: computing metrics (%d nodes, %d edges)", snap.Len(), snap.EdgeCount)
	metrics := CalculateMetricsFromSnapshot(snap)
	assigner := NewLayerAssigner(cfg.ArchLayers)
	assigner.WithForbidden(e.forbiddenPairs)
	if assigner.Configured() {
		metrics.LayerViolationCount = countLayerViolations(snap, assigner)
	}

	// 5D: component inference + coupling.
	components := InferComponentsFromSnapshot(snap, cfg, e.clock)
	couplings, ca, ce, inst := ComputeComponentCoupling(snap, components)
	applyComponentMetrics(&metrics, ca, ce, inst)
	for i := range components {
		for _, cc := range couplings {
			if cc.ComponentID == components[i].ID {
				components[i].Ca = cc.Ca
				components[i].Ce = cc.Ce
				components[i].Instability = cc.Instability
				break
			}
		}
	}
	result.Metrics = metrics
	result.Components = components
	result.ComponentCoupling = couplings

	rctx := &RuleContext{
		Graph:             snap,
		Metrics:           metrics,
		Components:        components,
		ComponentCoupling: couplings,
		LayerAssigner:     assigner,
		Cfg:               cfg,
		Clock:             e.clock,
	}

	// 5B: pattern detection.
	if rulesEnabled(cfg, "patterns") {
		if ctx != nil && ctx.Err() != nil {
			return result
		}
		e.logf("Stage 5B: running %d pattern rules", 7)
		result.Patterns = RunPatternDetectionContext(rctx)
	}

	// 5C: smell detection.
	if rulesEnabled(cfg, "smells") {
		if ctx != nil && ctx.Err() != nil {
			return result
		}
		e.logf("Stage 5C: running %d smell rules", 7)
		result.Smells = RunSmellDetectionContext(rctx)
	}

	e.logf("Stage 5: done — %d components, %d patterns, %d smells",
		len(result.Components), len(result.Patterns), len(result.Smells))
	return result
}

// topologyHash fingerprints the snapshot topology: sorted edge signatures
// (src\x00tgt\x00type) plus the sorted node id list.
func topologyHash(snap *GraphSnapshot) string {
	if snap == nil || snap.Len() == 0 {
		return ""
	}
	edges := make([]string, 0, snap.EdgeCount)
	for _, id := range snap.NodeIDs {
		for _, e := range snap.Outbound[id] {
			if !isStructuralEdge(e.Type) {
				continue
			}
			edges = append(edges, id+"\x00"+e.TargetID+"\x00"+string(e.Type))
		}
	}
	sort.Strings(edges)
	sum := sha256.New()
	for _, id := range snap.NodeIDs {
		sum.Write([]byte(id))
		sum.Write([]byte{0})
	}
	for _, e := range edges {
		sum.Write([]byte(e))
		sum.Write([]byte{0})
	}
	return strings.ToLower(hex.EncodeToString(sum.Sum(nil))[:16])
}
