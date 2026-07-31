package visualization_engine

import (
	"container/list"
	"fmt"
	"os"
	"sync"
	"time"

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
}

type SubgraphCache struct {
	mu      sync.RWMutex
	entries map[string]*cacheEntry
	lruList *list.List
	maxSize int
}

var (
	subgraphCache = &SubgraphCache{
		entries: make(map[string]*cacheEntry),
		lruList: list.New(),
		maxSize: 128,
	}
)

// resetCacheForTest clears the global cache. Used only in tests.
func resetCacheForTest() {
	subgraphCache = &SubgraphCache{
		entries: make(map[string]*cacheEntry),
		lruList: list.New(),
		maxSize: 128,
	}
}

type EngineCoordinator struct {
	ttlPath string
}

// NewEngineCoordinator creates a new EngineCoordinator with the given TTL file path.
func NewEngineCoordinator(ttlPath string) *EngineCoordinator {
	return &EngineCoordinator{ttlPath: ttlPath}
}

func reportProgress(cb func(stage, detail string), stage, detail string) {
	if cb != nil {
		cb(stage, detail)
	}
}
// ProjectDiagram runs the full 7-stage pipeline (parse, scope, extract, metrics, cluster, layout, render).
func (ec *EngineCoordinator) ProjectDiagram(t types.DiagramType, opts types.QueryOptions) (string, error) {
	opts.DiagramType = t

	enableMetrics := true
	enableCommunities := true
	enableSCC := true
	if opts.PipelineCfg != nil {
		enableMetrics = opts.PipelineCfg.EnableMetrics
		enableCommunities = opts.PipelineCfg.EnableCommunities
		enableSCC = opts.PipelineCfg.EnableSCC
	}

	reportProgress(opts.OnProgress, "StageParse", "Parsing AKG database...")
	full, err := ec.parseGraph(opts)
	if err != nil {
		return "", fmt.Errorf("parse failed: %w", err)
	}

	if opts.Scope != types.ScopeGlobal {
		reportProgress(opts.OnProgress, "StageScope", fmt.Sprintf("scoping to %v", opts.Scope))
		stage1.ApplyScope(full, opts)
	} else {
		reportProgress(opts.OnProgress, "StageScope", "global scope")
	}

	cfg := stage1.GetExtractionConfig(t, opts)
	reportProgress(opts.OnProgress, "StageExtract", fmt.Sprintf("config=%s", cfg.Name))
	subgraph := stage1.ExtractFromSubgraph(full, cfg, opts)

	reportProgress(opts.OnProgress, "StageMetrics", fmt.Sprintf("(SCC=%v, %d nodes)", enableSCC, len(subgraph.Nodes)))
	var metrics *stage2.DiagramMetrics
	if enableMetrics {
		metrics = stage2.ComputeAllMetrics(subgraph)
	}

	reportProgress(opts.OnProgress, "StageCluster", "")
	var clustering map[string]string
	if enableCommunities {
		clustering = stage2.DetectCommunities(subgraph)
	} else if metrics != nil {
		clustering = metrics.Communities
	}

	reportProgress(opts.OnProgress, "StageLayout", "")
	layout := stage2.BuildLayoutTreeEx(subgraph, metrics, clustering, opts, t)

	reportProgress(opts.OnProgress, "StageRender", fmt.Sprintf("rendering %s...", string(t)))
	markup := stage3.RenderDiagramFormat(layout, t, opts.Format)

	if opts.OnSummary != nil && layout.Summary != nil {
		opts.OnSummary(layout.Summary)
	}

	return markup, nil
}

// ComputeGraphSummary parses, scopes, and extracts the graph, then returns a summary without rendering.
func (ec *EngineCoordinator) ComputeGraphSummary(t types.DiagramType, opts types.QueryOptions) (*types.GraphSummary, error) {
	opts.DiagramType = t

	full, err := ec.parseGraph(opts)
	if err != nil {
		return nil, fmt.Errorf("parse failed: %w", err)
	}

	if opts.Scope != types.ScopeGlobal {
		stage1.ApplyScope(full, opts)
	}

	cfg := stage1.GetExtractionConfig(t, opts)
	subgraph := stage1.ExtractFromSubgraph(full, cfg, opts)

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
		delete(sc.entries, key)
		sc.lruList.Remove(entry.element)
		return nil
	}

	if time.Since(entry.lastAccess) > entry.ttl {
		delete(sc.entries, key)
		sc.lruList.Remove(entry.element)
		return nil
	}

	entry.lastAccess = time.Now()
	sc.lruList.MoveToFront(entry.element)
	return entry.graph
}

func (sc *SubgraphCache) Set(key string, mtime time.Time, graph *types.NativeGraph) {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	if existing, found := sc.entries[key]; found {
		sc.lruList.Remove(existing.element)
		delete(sc.entries, key)
	}

	if sc.lruList.Len() >= sc.maxSize {
		oldest := sc.lruList.Back()
		if oldest != nil {
			oldestKey := oldest.Value.(string)
			delete(sc.entries, oldestKey)
			sc.lruList.Remove(oldest)
		}
	}

	elem := sc.lruList.PushFront(key)
	sc.entries[key] = &cacheEntry{
		mtime:      mtime,
		graph:      graph,
		lastAccess: time.Now(),
		element:    elem,
		ttl:        10 * time.Minute,
	}
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
		delete(sc.entries, key)
		sc.lruList.Remove(oldest)
	}
}

func (ec *EngineCoordinator) parseGraph(opts types.QueryOptions) (*types.NativeGraph, error) {
	info, err := os.Stat(ec.ttlPath)
	if err != nil {
		return nil, fmt.Errorf("cannot stat TTL file: %w", err)
	}

	cacheKey := fmt.Sprintf("parse:%s:%d:%d", ec.ttlPath, info.Size(), info.ModTime().UnixNano())
	if cached := subgraphCache.Get(cacheKey, info.ModTime()); cached != nil {
		return cached, nil
	}

	native, err := stage1.ParseTTLFileToNative(ec.ttlPath)
	if err != nil {
		return nil, fmt.Errorf("failed to parse Turtle file: %w", err)
	}

	subgraphCache.Set(cacheKey, info.ModTime(), native)
	return native, nil
}
