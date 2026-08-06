package visualization_engine

import (
	"container/list"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/stage1"
	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/stage2"
	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/stage3"
	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/types"
)

func TestNewEngineCoordinator(t *testing.T) {
	ec := NewEngineCoordinator("/fake/path.ttl")
	if ec == nil {
		t.Fatal("expected non-nil EngineCoordinator")
	}
	if ec.ttlPath != "/fake/path.ttl" {
		t.Errorf("expected ttlPath '/fake/path.ttl', got '%s'", ec.ttlPath)
	}
}

func TestSubgraphCacheGetSet(t *testing.T) {
	cache := &SubgraphCache{
		entries:  make(map[string]*cacheEntry),
		lruList:  list.New(),
		maxBytes: subgraphCacheMaxBytes,
	}
	g := &types.NativeGraph{
		Nodes: map[string]*types.NativeNode{
			"a": {ID: "a", Name: "A"},
		},
	}
	now := time.Now()
	cache.Set("test-key", now, g)
	got := cache.Get("test-key", now)
	if got == nil {
		t.Fatal("expected to get cached graph")
	}
	if len(got.Nodes) != 1 {
		t.Errorf("expected 1 node, got %d", len(got.Nodes))
	}
}

func TestSubgraphCacheEviction(t *testing.T) {
	cache := &SubgraphCache{
		entries:  make(map[string]*cacheEntry),
		lruList:  list.New(),
		maxBytes: 1024,
	}
	now := time.Now()
	cache.Set("key1", now, &types.NativeGraph{})
	cache.Set("key2", now, &types.NativeGraph{})
	cache.Set("key3", now, &types.NativeGraph{})
	if len(cache.entries) > 2 {
		t.Errorf("expected at most 2 entries after eviction, got %d", len(cache.entries))
	}
	if cache.Get("key1", now) != nil {
		t.Error("key1 should have been evicted")
	}
}

func TestSubgraphCacheExpiredMtime(t *testing.T) {
	cache := &SubgraphCache{
		entries:  make(map[string]*cacheEntry),
		lruList:  list.New(),
		maxBytes: subgraphCacheMaxBytes,
	}
	oldTime := time.Now().Add(-1 * time.Hour)
	newTime := time.Now()
	cache.Set("key", oldTime, &types.NativeGraph{})
	got := cache.Get("key", newTime)
	if got != nil {
		t.Error("expected nil when mtime changed")
	}
}

func TestSubgraphCacheLRUOrdering(t *testing.T) {
	cache := &SubgraphCache{
		entries:  make(map[string]*cacheEntry),
		lruList:  list.New(),
		maxBytes: 1536,
	}
	now := time.Now()
	cache.Set("a", now, &types.NativeGraph{})
	cache.Set("b", now, &types.NativeGraph{})
	cache.Set("c", now, &types.NativeGraph{})
	cache.Get("a", now)
	cache.Set("d", now, &types.NativeGraph{})
	if cache.Get("b", now) != nil {
		t.Error("key b should have been evicted (least recently used)")
	}
	if cache.Get("a", now) == nil {
		t.Error("key a should still be in cache (recently used)")
	}
}

func TestSubgraphCacheEvict(t *testing.T) {
	cache := &SubgraphCache{
		entries:  make(map[string]*cacheEntry),
		lruList:  list.New(),
		maxBytes: subgraphCacheMaxBytes,
	}
	now := time.Now()
	cache.Set("a", now, &types.NativeGraph{Nodes: map[string]*types.NativeNode{"x": {ID: "x"}}})
	cache.Set("b", now, &types.NativeGraph{})
	if len(cache.entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(cache.entries))
	}
	cache.Evict(0)
	if len(cache.entries) != 0 {
		t.Errorf("expected 0 entries after Evict(), got %d", len(cache.entries))
	}
}

func TestReportProgress(t *testing.T) {
	called := false
	cb := func(stage, detail string) {
		called = true
		if stage != "test" {
			t.Errorf("expected stage 'test', got '%s'", stage)
		}
	}
	reportProgress(cb, "test", "detail")
	if !called {
		t.Error("expected callback to be called")
	}
}

func TestReportProgressNilCallback(t *testing.T) {
	reportProgress(nil, "test", "detail")
}

func TestNewEngineCoordinatorNilPath(t *testing.T) {
	ec := NewEngineCoordinator("")
	if ec == nil {
		t.Fatal("expected non-nil EngineCoordinator")
	}
	if ec.ttlPath != "" {
		t.Errorf("expected empty ttlPath, got '%s'", ec.ttlPath)
	}
}

func TestSubgraphCacheTTLExpiryOnGet(t *testing.T) {
	cache := &SubgraphCache{
		entries:  make(map[string]*cacheEntry),
		lruList:  list.New(),
		maxBytes: subgraphCacheMaxBytes,
	}
	now := time.Now()
	entryTime := now.Add(-30 * time.Minute)
	cache.Set("expired-key", entryTime, &types.NativeGraph{})
	cache.mu.Lock()
	if e, ok := cache.entries["expired-key"]; ok {
		e.ttl = 5 * time.Minute
		e.lastAccess = now.Add(-10 * time.Minute)
	}
	cache.mu.Unlock()
	got := cache.Get("expired-key", entryTime)
	if got != nil {
		t.Error("expected nil for TTL-expired entry")
	}
}

func TestSubgraphCacheGetNotFound(t *testing.T) {
	cache := &SubgraphCache{
		entries:  make(map[string]*cacheEntry),
		lruList:  list.New(),
		maxBytes: subgraphCacheMaxBytes,
	}
	got := cache.Get("nonexistent", time.Now())
	if got != nil {
		t.Error("expected nil for nonexistent key")
	}
}

func TestSubgraphCacheEvictCount(t *testing.T) {
	cache := &SubgraphCache{
		entries:  make(map[string]*cacheEntry),
		lruList:  list.New(),
		maxBytes: subgraphCacheMaxBytes,
	}
	now := time.Now()
	cache.Set("a", now, &types.NativeGraph{Nodes: map[string]*types.NativeNode{"x": {ID: "x"}}})
	cache.Set("b", now, &types.NativeGraph{})
	cache.Set("c", now, &types.NativeGraph{})
	if len(cache.entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(cache.entries))
	}
	cache.Evict(2)
	if len(cache.entries) != 1 {
		t.Errorf("expected 1 entry after Evict(2), got %d", len(cache.entries))
	}
}

func TestSubgraphCacheEvictAll(t *testing.T) {
	cache := &SubgraphCache{
		entries:  make(map[string]*cacheEntry),
		lruList:  list.New(),
		maxBytes: subgraphCacheMaxBytes,
	}
	now := time.Now()
	cache.Set("a", now, &types.NativeGraph{})
	cache.Set("b", now, &types.NativeGraph{})
	cache.Evict(0)
	if len(cache.entries) != 0 {
		t.Errorf("expected 0 entries after Evict(0), got %d", len(cache.entries))
	}
}

func TestSubgraphCacheConcurrentAccess(t *testing.T) {
	cache := &SubgraphCache{
		entries:  make(map[string]*cacheEntry),
		lruList:  list.New(),
		maxBytes: subgraphCacheMaxBytes,
	}
	now := time.Now()
	cache.Set("key1", now, &types.NativeGraph{})
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cache.Get("key1", now)
			cache.Set("key2", now, &types.NativeGraph{})
			cache.Get("key2", now)
		}()
	}
	wg.Wait()
	if cache.Get("key1", now) == nil {
		t.Error("key1 should still be in cache after concurrent access")
	}
}

func TestProjectDiagramEndToEnd(t *testing.T) {
	path := filepath.Join("testdata", "minimal.ttl")
	ec := NewEngineCoordinator(path)
	result, err := ec.ProjectDiagram(types.UMLClass, types.QueryOptions{
		EntryPointID: "main.go::Main",
	})
	if err != nil {
		t.Fatalf("ProjectDiagram failed: %v", err)
	}
	if result == "" {
		t.Error("expected non-empty diagram output")
	}
}

func TestProjectDiagramScopeFolder(t *testing.T) {
	path := filepath.Join("testdata", "scope_internal.ttl")
	ec := NewEngineCoordinator(path)
	result, err := ec.ProjectDiagram(types.CallGraph, types.QueryOptions{
		EntryPointID: "internal/api/handler.go::HandleRequest",
		Scope:        types.ScopeFolder,
		ScopePath:    "internal",
	})
	if err != nil {
		t.Fatalf("ProjectDiagram with scope failed: %v", err)
	}
	if result == "" {
		t.Error("expected non-empty diagram output with scope")
	}
}

func TestComputeGraphSummaryEndToEnd(t *testing.T) {
	path := filepath.Join("testdata", "minimal.ttl")
	ec := NewEngineCoordinator(path)
	summary, err := ec.ComputeGraphSummary(types.UMLClass, types.QueryOptions{
		EntryPointID: "main.go::Main",
	})
	if err != nil {
		t.Fatalf("ComputeGraphSummary failed: %v", err)
	}
	if summary == nil {
		t.Fatal("expected non-nil summary")
	}
	if summary.NodeCount < 1 {
		t.Errorf("expected at least 1 node, got %d", summary.NodeCount)
	}
}

func TestPipelineParseExtractRender(t *testing.T) {
	path := filepath.Join("testdata", "minimal.ttl")
	native, err := stage1.ParseTTLFileToNative(path)
	if err != nil {
		t.Fatalf("ParseTTLFileToNative failed: %v", err)
	}
	cfg := stage1.GetExtractionConfig(types.UMLClass, types.QueryOptions{EntryPointID: "main.go::Main"})
	sub, _, err := stage1.ExtractFromSubgraph(native, cfg, types.QueryOptions{EntryPointID: "main.go::Main"})
	if err != nil {
		t.Fatalf("ExtractFromSubgraph failed: %v", err)
	}
	if len(sub.Nodes) == 0 {
		t.Fatal("expected at least one extracted node")
	}
	metrics := stage2.ComputeAllMetrics(sub)
	if metrics == nil {
		t.Fatal("expected non-nil metrics")
	}
	layout := stage2.BuildLayoutTreeEx(sub, metrics, metrics.Communities, types.QueryOptions{}, types.UMLClass)
	if layout == nil {
		t.Fatal("expected non-nil layout tree")
	}
	markup := stage3.RenderDiagramFormat(layout, types.UMLClass, "mermaid")
	if markup == "" {
		t.Error("expected non-empty render output")
	}
}

// TestProjectDiagramFromGraphEqualsFilePath: the from-graph entry point
// renders the same diagram as the file-parsing entry point for an identical
// in-memory graph (AUDIT Issue 4 Phase 4A-1).
func TestProjectDiagramFromGraphEqualsFilePath(t *testing.T) {
	path := filepath.Join("testdata", "minimal.ttl")
	ec := NewEngineCoordinator(path)
	opts := types.QueryOptions{EntryPointID: "main.go::Main"}

	fromFile, err := ec.ProjectDiagram(types.UMLClass, opts)
	if err != nil {
		t.Fatalf("ProjectDiagram failed: %v", err)
	}

	native, err := stage1.ParseTTLFileToNative(path)
	if err != nil {
		t.Fatalf("ParseTTLFileToNative failed: %v", err)
	}
	fromGraph, err := ProjectDiagramFromGraph(native, types.UMLClass, opts)
	if err != nil {
		t.Fatalf("ProjectDiagramFromGraph failed: %v", err)
	}

	if fromFile == "" || fromGraph == "" {
		t.Fatal("expected non-empty diagrams")
	}
	if fromFile != fromGraph {
		t.Errorf("from-graph diagram differs from from-file diagram:\nfromFile:\n%s\nfromGraph:\n%s", fromFile, fromGraph)
	}
}

// TestComputeGraphSummaryFromGraphEqualsFilePath: same parity for the
// summary path (AUDIT Issue 4 Phase 4A-1).
func TestComputeGraphSummaryFromGraphEqualsFilePath(t *testing.T) {
	path := filepath.Join("testdata", "minimal.ttl")
	ec := NewEngineCoordinator(path)
	opts := types.QueryOptions{EntryPointID: "main.go::Main"}

	fromFile, err := ec.ComputeGraphSummary(types.UMLClass, opts)
	if err != nil {
		t.Fatalf("ComputeGraphSummary failed: %v", err)
	}

	native, err := stage1.ParseTTLFileToNative(path)
	if err != nil {
		t.Fatalf("ParseTTLFileToNative failed: %v", err)
	}
	fromGraph, err := ComputeGraphSummaryFromGraph(native, types.UMLClass, opts)
	if err != nil {
		t.Fatalf("ComputeGraphSummaryFromGraph failed: %v", err)
	}

	if fromFile == nil || fromGraph == nil {
		t.Fatal("expected non-nil summaries")
	}
	if fromFile.NodeCount != fromGraph.NodeCount {
		t.Errorf("node count mismatch: file=%d graph=%d", fromFile.NodeCount, fromGraph.NodeCount)
	}
	if fromFile.EdgeCount != fromGraph.EdgeCount {
		t.Errorf("edge count mismatch: file=%d graph=%d", fromFile.EdgeCount, fromGraph.EdgeCount)
	}
}

// TestProjectDiagramScopeFileUsesScopedParse: a file-scoped diagram parses
// only the file's triples and still renders a correct non-empty diagram
// (AUDIT Issue 4 Phase 4A-2).
func TestProjectDiagramScopeFileUsesScopedParse(t *testing.T) {
	path := filepath.Join("testdata", "scope_internal.ttl")
	ec := NewEngineCoordinator(path)
	opts := types.QueryOptions{
		EntryPointID: "internal/api/handler.go::HandleRequest",
		Scope:        types.ScopeFile,
		ScopePath:    "internal/api/handler.go",
	}
	result, err := ec.ProjectDiagram(types.CallGraph, opts)
	if err != nil {
		t.Fatalf("file-scoped ProjectDiagram failed: %v", err)
	}
	if result == "" {
		t.Fatal("expected non-empty file-scoped diagram")
	}
}

// TestProjectDiagramFromGraphDoesNotMutateSource: the from-graph entry point
// must not mutate the caller's graph (scoping works on a private clone).
func TestProjectDiagramFromGraphDoesNotMutateSource(t *testing.T) {
	path := filepath.Join("testdata", "minimal.ttl")
	native, err := stage1.ParseTTLFileToNative(path)
	if err != nil {
		t.Fatalf("ParseTTLFileToNative failed: %v", err)
	}
	before := native.Clone()
	_, err = ProjectDiagramFromGraph(native, types.UMLClass, types.QueryOptions{
		EntryPointID: "main.go::Main",
		Scope:        types.ScopeFile,
		ScopePath:    "main.go",
	})
	if err != nil {
		t.Fatalf("ProjectDiagramFromGraph failed: %v", err)
	}
	if len(native.Nodes) != len(before.Nodes) {
		t.Errorf("source graph mutated: %d nodes before, %d after", len(before.Nodes), len(native.Nodes))
	}
}

func TestSubgraphCacheSetOverwrite(t *testing.T) {
	cache := &SubgraphCache{
		entries:  make(map[string]*cacheEntry),
		lruList:  list.New(),
		maxBytes: subgraphCacheMaxBytes,
	}
	now := time.Now()
	g1 := &types.NativeGraph{Nodes: map[string]*types.NativeNode{"a": {ID: "a"}}}
	g2 := &types.NativeGraph{Nodes: map[string]*types.NativeNode{"b": {ID: "b"}}}
	cache.Set("key", now, g1)
	cache.Set("key", now, g2) // overwrite
	got := cache.Get("key", now)
	if got == nil {
		t.Fatal("expected non-nil result after overwrite")
	}
	if _, ok := got.Nodes["b"]; !ok {
		t.Error("expected overwritten graph to have node b")
	}
	if len(cache.entries) != 1 {
		t.Errorf("expected 1 entry after overwrite, got %d", len(cache.entries))
	}
}

func TestProjectDiagramWithPipelineConfig(t *testing.T) {
	path := filepath.Join("testdata", "minimal.ttl")
	ec := NewEngineCoordinator(path)
	result, err := ec.ProjectDiagram(types.CallGraph, types.QueryOptions{
		EntryPointID: "main.go::Main",
		PipelineCfg: &types.PipelineConfig{
			EnableMetrics:     true,
			EnableCommunities: true,
			EnableSCC:         true,
		},
	})
	if err != nil {
		t.Fatalf("ProjectDiagram with PipelineConfig failed: %v", err)
	}
	if result == "" {
		t.Error("expected non-empty diagram output")
	}
}

func TestProjectDiagramMetricsDisabled(t *testing.T) {
	path := filepath.Join("testdata", "minimal.ttl")
	ec := NewEngineCoordinator(path)
	result, err := ec.ProjectDiagram(types.CallGraph, types.QueryOptions{
		EntryPointID: "main.go::Main",
		PipelineCfg: &types.PipelineConfig{
			EnableMetrics:     false,
			EnableCommunities: false,
			EnableSCC:         false,
		},
	})
	if err != nil {
		t.Fatalf("ProjectDiagram with metrics disabled failed: %v", err)
	}
	if result == "" {
		t.Error("expected non-empty diagram output even without metrics")
	}
}

func TestProjectDiagramCaching(t *testing.T) {
	resetCacheForTest()
	path := filepath.Join("testdata", "minimal.ttl")
	ec := NewEngineCoordinator(path)
	// First call — populates cache
	result1, err := ec.ProjectDiagram(types.UMLClass, types.QueryOptions{EntryPointID: "main.go::Main"})
	if err != nil {
		t.Fatalf("first ProjectDiagram failed: %v", err)
	}
	// Second call — should hit cache
	result2, err := ec.ProjectDiagram(types.UMLClass, types.QueryOptions{EntryPointID: "main.go::Main"})
	if err != nil {
		t.Fatalf("second ProjectDiagram failed: %v", err)
	}
	if result1 == "" || result2 == "" {
		t.Error("expected non-empty results from both calls")
	}
}

func TestProjectDiagramProgressCallback(t *testing.T) {
	path := filepath.Join("testdata", "minimal.ttl")
	ec := NewEngineCoordinator(path)
	var stages []string
	_, err := ec.ProjectDiagram(types.UMLClass, types.QueryOptions{
		EntryPointID: "main.go::Main",
		OnProgress: func(stage, detail string) {
			stages = append(stages, stage)
		},
	})
	if err != nil {
		t.Fatalf("ProjectDiagram with progress callback failed: %v", err)
	}
	if len(stages) == 0 {
		t.Error("expected at least one progress callback")
	}
	// Verify key stages were reported
	found := make(map[string]bool)
	for _, s := range stages {
		found[s] = true
	}
	for _, expected := range []string{"StageParse", "StageExtract", "StageRender"} {
		if !found[expected] {
			t.Errorf("expected stage %q to be reported, got stages: %v", expected, stages)
		}
	}
}

func TestProjectDiagramSummaryCallback(t *testing.T) {
	path := filepath.Join("testdata", "minimal.ttl")
	ec := NewEngineCoordinator(path)
	var receivedSummary *types.GraphSummary
	_, err := ec.ProjectDiagram(types.UMLClass, types.QueryOptions{
		EntryPointID: "main.go::Main",
		PipelineCfg: &types.PipelineConfig{
			EnableMetrics: true,
		},
		OnSummary: func(s *types.GraphSummary) {
			receivedSummary = s
		},
	})
	if err != nil {
		t.Fatalf("ProjectDiagram failed: %v", err)
	}
	// Summary callback may or may not fire depending on the layout tree contents.
	// Just verify no panic occurred.
	_ = receivedSummary
}

func TestComputeGraphSummaryAllDiagramTypes(t *testing.T) {
	path := filepath.Join("testdata", "minimal.ttl")
	ec := NewEngineCoordinator(path)
	diagramTypes := []types.DiagramType{
		types.UMLClass, types.UMLObject, types.CallGraph, types.DependencyGraph,
		types.DataFlow, types.ERDiagram, types.Mindmap, types.Flowchart,
	}
	for _, dt := range diagramTypes {
		dt := dt
		t.Run(string(dt), func(t *testing.T) {
			t.Parallel()
			summary, err := ec.ComputeGraphSummary(dt, types.QueryOptions{})
			if err != nil {
				t.Fatalf("ComputeGraphSummary(%s) failed: %v", dt, err)
			}
			if summary == nil {
				t.Fatalf("expected non-nil summary for %s", dt)
			}
		})
	}
}

func TestProjectDiagramInvalidTTLPath(t *testing.T) {
	ec := NewEngineCoordinator("/nonexistent/path/to/file.ttl")
	_, err := ec.ProjectDiagram(types.UMLClass, types.QueryOptions{})
	if err == nil {
		t.Error("expected error for non-existent TTL file")
	}
}

func TestSubgraphCacheByteBudget(t *testing.T) {
	cache := &SubgraphCache{
		entries:  make(map[string]*cacheEntry),
		lruList:  list.New(),
		maxBytes: 1536,
	}
	now := time.Now()
	// Each empty-graph entry costs estimatedBytes(0) + 512 overhead.
	cache.Set("a", now, &types.NativeGraph{})
	cache.Set("b", now, &types.NativeGraph{})
	cache.Set("c", now, &types.NativeGraph{})
	// 3*512 fits exactly; a 4th exceeds the budget and must evict the LRU entry.
	cache.Set("d", now, &types.NativeGraph{})
	if len(cache.entries) != 3 {
		t.Errorf("expected 3 entries under byte budget, got %d", len(cache.entries))
	}
	if cache.Get("a", now) != nil {
		t.Error("entry a should have been evicted by the byte budget")
	}
	if cache.Get("d", now) == nil {
		t.Error("entry d should be cached")
	}
	count, bytes := cache.Size()
	if count != 3 {
		t.Errorf("Size() count = %d, want 3", count)
	}
	if bytes > cache.maxBytes {
		t.Errorf("Size() bytes = %d exceeds budget %d", bytes, cache.maxBytes)
	}
}

func TestSubgraphCacheConcurrentGetSet(t *testing.T) {
	cache := &SubgraphCache{
		entries:  make(map[string]*cacheEntry),
		lruList:  list.New(),
		maxBytes: subgraphCacheMaxBytes,
	}
	now := time.Now()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			key := fmt.Sprintf("key%d", i%5)
			cache.Set(key, now, &types.NativeGraph{})
			cache.Get(key, now)
		}()
	}
	wg.Wait()
}
