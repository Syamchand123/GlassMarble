package mcp

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/Syamchand123/GlassMarble/internal/ai_engine/akgbridge"
	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/arch_timeline"
	"github.com/Syamchand123/GlassMarble/internal/developer_memory"
)

// Bridge provides thread-safe, lazy-initialized access to GlassMarble's data stores.
type Bridge struct {
	mu         sync.RWMutex
	rootDir    string
	storageDir string
	maxBytes   int64

	tm        *akg.AKGTransactionManager
	akgBridge *akgbridge.Bridge
	memStore  *developer_memory.MemoryStore
	snapStore *arch_timeline.SnapshotStore
}

// NewBridge creates a new domain bridge for the given repository root.
func NewBridge(rootDir string, maxJSONMB int) *Bridge {
	absRoot, err := filepath.Abs(rootDir)
	if err != nil {
		absRoot = rootDir
	}
	var maxBytes int64
	if maxJSONMB > 0 {
		maxBytes = int64(maxJSONMB) << 20
	}
	return &Bridge{
		rootDir:    absRoot,
		storageDir: filepath.Join(absRoot, ".glassmarble"),
		maxBytes:   maxBytes,
	}
}

// RootDir returns the absolute path to the repository root.
func (b *Bridge) RootDir() string {
	return b.rootDir
}

// StorageDir returns the absolute path to the .glassmarble storage directory.
func (b *Bridge) StorageDir() string {
	return b.storageDir
}

// AKGStatePath returns the absolute path to akg.json.
func (b *Bridge) AKGStatePath() string {
	return filepath.Join(b.storageDir, "akg.json")
}

// HasAKG checks if akg.json exists on disk.
func (b *Bridge) HasAKG() bool {
	st, err := os.Stat(b.AKGStatePath())
	return err == nil && st.Size() > 0
}

// AKGBridge returns an akgbridge.Bridge instance for compatibility with internal/ai_engine/tools.
func (b *Bridge) AKGBridge() (*akgbridge.Bridge, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.akgBridge != nil {
		return b.akgBridge, nil
	}

	statePath := b.AKGStatePath()
	if _, err := os.Stat(statePath); err != nil {
		return nil, fmt.Errorf("AKG database not found at %s — run 'gmb analyze' first", statePath)
	}

	b.akgBridge = akgbridge.New(statePath)
	return b.akgBridge, nil
}

// Snapshot returns the active Architecture Knowledge Graph (CPG).
func (b *Bridge) Snapshot() (*akg.CodePropertyGraph, error) {
	br, err := b.AKGBridge()
	if err != nil {
		return nil, err
	}
	return br.Snapshot()
}

// TransactionManager returns the AKGTransactionManager, creating it on first access.
func (b *Bridge) TransactionManager() (*akg.AKGTransactionManager, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.tm != nil {
		return b.tm, nil
	}

	tm, err := akg.NewAKGTransactionManagerWithOptions(b.storageDir, b.maxBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to open AKG database: %w — run 'gmb analyze' first", err)
	}
	b.tm = tm
	return b.tm, nil
}

// MemoryStore returns the Developer Memory store instance.
func (b *Bridge) MemoryStore() (*developer_memory.MemoryStore, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.memStore != nil {
		return b.memStore, nil
	}

	store := developer_memory.NewStoreForRepo(b.rootDir)
	b.memStore = store
	return b.memStore, nil
}

// SnapshotStore returns the Architecture Snapshot store instance.
func (b *Bridge) SnapshotStore() (*arch_timeline.SnapshotStore, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.snapStore != nil {
		return b.snapStore, nil
	}

	snapDir := filepath.Join(b.storageDir, "snapshots")
	store, err := arch_timeline.NewSnapshotStore(snapDir)
	if err != nil {
		return nil, fmt.Errorf("failed to open snapshot store: %w", err)
	}
	b.snapStore = store
	return b.snapStore, nil
}

// Close releases all open database handles and resources.
func (b *Bridge) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.tm != nil {
		b.tm.Close()
		b.tm = nil
	}
	b.akgBridge = nil
	b.memStore = nil
	b.snapStore = nil
}
