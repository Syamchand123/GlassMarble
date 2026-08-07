package visualization_engine

import (
	"container/list"
	"fmt"
	"os"
	"sync"
	"time"

	producterrs "github.com/Syamchand123/GlassMarble/internal/product/errors"
	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/stage1"
	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/stage2"
	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/stage3"
	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/types"
)

type cacheEntry struct {
	mtime      time.Time
	graph      *types.NativeGraph
	lastAccess time.Time
	element    *list.Element
	ttl        time.Duration
	bytes      int64
}

type SubgraphCache struct {
	mu           sync.RWMutex
	entries      map[string]*cacheEntry
	lruList      *list.List
	maxBytes     int64
	currentBytes int64
}

// SubgraphCache budget: the parsed AKG database is cached in full, so the
// cache is bounded in BYTES (64 MiB default) rather than by entry count
// (AUDIT Issue 4 Phase 4A-3). LRU eviction runs against the byte budget.
const subgraphCacheMaxBytes = 64 << 20

var (
	subgraphCache = &SubgraphCache{
		entries:  make(map[string]*cacheEntry),
		lruList:  list.New(),
		maxBytes: subgraphCacheMaxBytes,
	}
)

// resetCacheForTest clears the global cache. Used only in tests.
func resetCacheForTest() {
	subgraphCache = &SubgraphCache{
		entries:  make(map[string]*cacheEntry),
		lruList:  list.New(),
		maxBytes: subgraphCacheMaxBytes,
	}
}

// estimatedBytes approximates the in-memory footprint of a parsed graph:
// per-node/per-edge structural overhead plus the string contents. It is a
// lower bound used for LRU budgeting, not a runtime allocation measure.
func estimatedBytes(graph *types.NativeGraph) int64 {
	var n int64 = 0
	for id, node := range graph.Nodes {
		n += 128 + int64(len(id))
		if node == nil {
			continue
		}
		n += int64(len(node.Kind)+len(node.Name)+len(node.PrimitiveType)+len(node.FileURI)+len(node.Code)+len(node.PrimitiveZone)) + 64
		for k, v := range node.Properties {
			n += int64(len(k)+len(v)) + 64
		}
	}
	for _, e := range graph.Edges {
		n += 64 + int64(len(e.SourceID)+len(e.Predicate)+len(e.TargetID))
	}
	return n
}

// ParseFn materializes a NativeGraph from a persisted state file. The
// coordinator defaults to the legacy Turtle parser; callers serving the
// canonical GraphJSON store (Phase C) install the akg-backed adapter via
// SetParseFn. The akg package cannot be imported here (import cycle via
// stage4 → product), so the wiring happens at the request layer.
type ParseFn func(path string, opts types.QueryOptions) (*types.NativeGraph, error)

type EngineCoordinator struct {
	statePath string
	parseFn   ParseFn
}

// NewEngineCoordinator creates a new EngineCoordinator for the given state
// file path. By default the legacy Turtle parser is used; SetParseFn
// overrides it (e.g. with the akg GraphJSON reader).
func NewEngineCoordinator(statePath string) *EngineCoordinator {
	return &EngineCoordinator{statePath: statePath}
}

// SetParseFn installs a custom state-file parser, replacing the default
// legacy Turtle reader.
func (ec *EngineCoordinator) SetParseFn(fn ParseFn) {
	ec.parseFn = fn
}

func reportProgress(cb func(stage, detail string), stage, detail string) {
	if cb != nil {
		cb(stage, detail)
	}
}

// ProjectDiagram runs the full 7-stage pipeline (parse, scope, extract, metrics, cluster, layout, render).
func (ec *EngineCoordinator) ProjectDiagram(t types.DiagramType, opts types.QueryOptions) (string, error) {
	opts.DiagramType = t

	reportProgress(opts.OnProgress, "StageParse", "Parsing AKG database...")
	full, err := ec.parseGraph(opts)
	if err != nil {
		return "", fmt.Errorf("parse failed: %w", err)
	}

	return ProjectDiagramFromGraph(full, t, opts)
}

// ProjectDiagramFromGraph runs the 6-stage downstream pipeline (scope,
// extract, metrics, cluster, layout, render) over an already-loaded graph.
// Consumers that hold the AKG in memory (the AI bridge snapshot) use this
// instead of re-parsing the TTL, so the diagram engine consumes the same
// in-memory form via the AKG API (AUDIT Issue 4 Phase 4A-1).
func ProjectDiagramFromGraph(full *types.NativeGraph, t types.DiagramType, opts types.QueryOptions) (string, error) {
	opts.DiagramType = t

	enableMetrics := true
	enableCommunities := true
	enableSCC := true
	if opts.PipelineCfg != nil {
		enableMetrics = opts.PipelineCfg.EnableMetrics
		enableCommunities = opts.PipelineCfg.EnableCommunities
		enableSCC = opts.PipelineCfg.EnableSCC
	}

	// Never mutate a shared cached graph: scoping must operate on a private
	// copy (AUDIT Issue 2 Phase 2B-9).
	full = full.Clone()

	if opts.Scope != types.ScopeGlobal {
		reportProgress(opts.OnProgress, "StageScope", fmt.Sprintf("scoping to %v", opts.Scope))
		stage1.ApplyScope(full, opts)
	} else {
		reportProgress(opts.OnProgress, "StageScope", "global scope")
	}

	cfg := stage1.GetExtractionConfig(t, opts)
	reportProgress(opts.OnProgress, "StageExtract", fmt.Sprintf("config=%s", cfg.Name))
	subgraph, effectiveOpts, err := stage1.ExtractFromSubgraph(full, cfg, opts)
	if err != nil {
		return "", fmt.Errorf("extract failed: %w", err)
	}
	if len(subgraph.Nodes) == 0 {
		return "", producterrs.Annotate(fmt.Errorf("diagram %s produced an empty subgraph (no nodes match the configured node kinds; try specifying --entry or --scope)", string(t)), producterrs.ErrEmptySubgraph)
	}

	reportProgress(opts.OnProgress, "StageMetrics", fmt.Sprintf("(SCC=%v, %d nodes)", enableSCC, len(subgraph.Nodes)))
	var metrics *stage2.DiagramMetrics
	if enableMetrics {
		metrics = stage2.ComputeAllMetricsWithOptions(subgraph, enableSCC)
	}

	reportProgress(opts.OnProgress, "StageCluster", "")
	var clustering map[string]string
	if enableCommunities {
		clustering = stage2.DetectCommunities(subgraph)
	} else if metrics != nil {
		clustering = metrics.Communities
	}

	reportProgress(opts.OnProgress, "StageLayout", "")
	layout := stage2.BuildLayoutTreeEx(subgraph, metrics, clustering, effectiveOpts, t)

	reportProgress(opts.OnProgress, "StageRender", fmt.Sprintf("rendering %s...", string(t)))
	markup := stage3.RenderDiagramFormat(layout, t, opts.Format)

	if opts.OnSummary != nil && layout.Summary != nil {
		opts.OnSummary(layout.Summary)
	}

	return markup, nil
}

// BuildLayoutTree parses, scopes, extracts, metrics, clusters, and constructs the layout tree for a diagram type.
func (ec *EngineCoordinator) BuildLayoutTree(t types.DiagramType, opts types.QueryOptions) (*types.LayoutTree, error) {
	opts.DiagramType = t
	full, err := ec.parseGraph(opts)
	if err != nil {
		return nil, fmt.Errorf("parse failed: %w", err)
	}
	return BuildLayoutTreeFromGraph(full, t, opts)
}

// BuildLayoutTreeFromGraph runs the pipeline through layout tree construction over an existing graph.
func BuildLayoutTreeFromGraph(full *types.NativeGraph, t types.DiagramType, opts types.QueryOptions) (*types.LayoutTree, error) {
	opts.DiagramType = t

	enableMetrics := true
	enableCommunities := true
	enableSCC := true
	if opts.PipelineCfg != nil {
		enableMetrics = opts.PipelineCfg.EnableMetrics
		enableCommunities = opts.PipelineCfg.EnableCommunities
		enableSCC = opts.PipelineCfg.EnableSCC
	}

	full = full.Clone()
	if opts.Scope != types.ScopeGlobal {
		stage1.ApplyScope(full, opts)
	}

	cfg := stage1.GetExtractionConfig(t, opts)
	subgraph, effectiveOpts, err := stage1.ExtractFromSubgraph(full, cfg, opts)
	if err != nil {
		return nil, fmt.Errorf("extract failed: %w", err)
	}
	if len(subgraph.Nodes) == 0 {
		return nil, producterrs.Annotate(fmt.Errorf("diagram %s produced an empty subgraph", string(t)), producterrs.ErrEmptySubgraph)
	}

	var metrics *stage2.DiagramMetrics
	if enableMetrics {
		metrics = stage2.ComputeAllMetricsWithOptions(subgraph, enableSCC)
	}

	var clustering map[string]string
	if enableCommunities {
		clustering = stage2.DetectCommunities(subgraph)
	} else if metrics != nil {
		clustering = metrics.Communities
	}

	layout := stage2.BuildLayoutTreeEx(subgraph, metrics, clustering, effectiveOpts, t)
	return layout, nil
}

// ComputeGraphSummary parses, scopes, and extracts the graph, then returns a summary without rendering.
func (ec *EngineCoordinator) ComputeGraphSummary(t types.DiagramType, opts types.QueryOptions) (*types.GraphSummary, error) {
	opts.DiagramType = t

	full, err := ec.parseGraph(opts)
	if err != nil {
		return nil, fmt.Errorf("parse failed: %w", err)
	}

	return ComputeGraphSummaryFromGraph(full, t, opts)
}

// ComputeGraphSummaryFromGraph computes the summary over an already-loaded
// graph (the AI-bridge in-memory form, AUDIT Issue 4 Phase 4A-1).
func ComputeGraphSummaryFromGraph(full *types.NativeGraph, t types.DiagramType, opts types.QueryOptions) (*types.GraphSummary, error) {
	opts.DiagramType = t

	full = full.Clone()

	if opts.Scope != types.ScopeGlobal {
		stage1.ApplyScope(full, opts)
	}

	cfg := stage1.GetExtractionConfig(t, opts)
	subgraph, _, err := stage1.ExtractFromSubgraph(full, cfg, opts)
	if err != nil {
		return nil, fmt.Errorf("extract failed: %w", err)
	}

	return stage2.ComputeGraphSummary(subgraph), nil
}

func (sc *SubgraphCache) Get(key string, mtime time.Time) *types.NativeGraph {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	entry, found := sc.entries[key]
	if !found {
		return nil
	}

	if !entry.mtime.Equal(mtime) {
		sc.dropLocked(key, entry)
		return nil
	}

	if time.Since(entry.lastAccess) > entry.ttl {
		sc.dropLocked(key, entry)
		return nil
	}

	entry.lastAccess = time.Now()
	sc.lruList.MoveToFront(entry.element)
	return entry.graph
}

// dropLocked removes an entry, releasing its byte budget. Callers must hold sc.mu.
func (sc *SubgraphCache) dropLocked(key string, entry *cacheEntry) {
	delete(sc.entries, key)
	sc.lruList.Remove(entry.element)
	sc.currentBytes -= entry.bytes
	if sc.currentBytes < 0 {
		sc.currentBytes = 0
	}
}

// cacheTTL returns the SubgraphCache entry TTL, configurable via the
// GMB_CACHE_TTL environment variable (a Go duration such as "5m" or "1h").
// It defaults to 10 minutes when unset or unparseable (GAP-M-08).
func cacheTTL() time.Duration {
	if raw := os.Getenv("GMB_CACHE_TTL"); raw != "" {
		if d, err := time.ParseDuration(raw); err == nil && d > 0 {
			return d
		}
	}
	return 10 * time.Minute
}

func (sc *SubgraphCache) Set(key string, mtime time.Time, graph *types.NativeGraph) {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	if existing, found := sc.entries[key]; found {
		sc.dropLocked(key, existing)
	}

	entryBytes := estimatedBytes(graph) + 512
	for sc.lruList.Len() > 0 && sc.currentBytes+entryBytes > sc.maxBytes {
		oldest := sc.lruList.Back()
		oldestKey := oldest.Value.(string)
		sc.dropLocked(oldestKey, sc.entries[oldestKey])
	}

	elem := sc.lruList.PushFront(key)
	sc.entries[key] = &cacheEntry{
		mtime:      mtime,
		graph:      graph,
		lastAccess: time.Now(),
		element:    elem,
		ttl:        cacheTTL(),
		bytes:      entryBytes,
	}
	sc.currentBytes += entryBytes
}

func (sc *SubgraphCache) Evict(count int) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	if count <= 0 || count > sc.lruList.Len() {
		count = sc.lruList.Len()
	}
	for i := 0; i < count; i++ {
		oldest := sc.lruList.Back()
		if oldest == nil {
			break
		}
		key := oldest.Value.(string)
		sc.dropLocked(key, sc.entries[key])
	}
}

// Size reports the number of cached entries and their total estimated bytes.
func (sc *SubgraphCache) Size() (int, int64) {
	sc.mu.RLock()
	defer sc.mu.RUnlock()
	return sc.lruList.Len(), sc.currentBytes
}

func (ec *EngineCoordinator) parseGraph(opts types.QueryOptions) (*types.NativeGraph, error) {
	info, err := os.Stat(ec.statePath)
	if err != nil {
		return nil, fmt.Errorf("cannot stat state file: %w", err)
	}

	// Scope and ScopePath both participate in the cache key: a file-scoped
	// parse (which loads only the file's triples) must never collide with
	// the global parse, and two different file/folder scopes must never
	// collide with each other (AUDIT Issue 4 Phase 4A-3 / GAP-M-04).
	cacheKey := fmt.Sprintf("parse:%s:%d:%d:%d:%s", ec.statePath, info.Size(), info.ModTime().UnixNano(), opts.Scope, opts.ScopePath)
	if cached := subgraphCache.Get(cacheKey, info.ModTime()); cached != nil {
		return cached, nil
	}

	parseFn := ec.parseFn
	if parseFn == nil {
		parseFn = func(path string, opts types.QueryOptions) (*types.NativeGraph, error) {
			return stage1.ExtractSubgraph(path, opts.DiagramType, opts)
		}
	}
	native, err := parseFn(ec.statePath, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to parse state file: %w", err)
	}

	subgraphCache.Set(cacheKey, info.ModTime(), native)
	return native, nil
}
