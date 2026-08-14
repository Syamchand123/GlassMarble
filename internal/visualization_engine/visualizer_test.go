package visualization_engine

import (
	"container/list"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/types"
)

func TestNewEngineCoordinator(t *testing.T) {
	ec := NewEngineCoordinator("/fake/path.json")
	if ec == nil {
		t.Fatal("expected non-nil EngineCoordinator")
	}
	if ec.statePath != "/fake/path.json" {
		t.Errorf("expected statePath '/fake/path.json', got '%s'", ec.statePath)
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
	cb := func(step, detail string) {
		called = true
		if step != "test" {
			t.Errorf("expected step 'test', got '%s'", step)
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
	if ec.statePath != "" {
		t.Errorf("expected empty statePath, got '%s'", ec.statePath)
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

func TestProjectDiagramInvalidStatePath(t *testing.T) {
	ec := NewEngineCoordinator("/nonexistent/path/to/akg.json")
	_, err := ec.ProjectDiagram(types.UMLClass, types.QueryOptions{})
	if err == nil {
		t.Error("expected error for non-existent state file")
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
