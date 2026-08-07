// Package akgbridge provides the AI agent's lazy access to the AKG
// (Architecture Knowledge Graph) snapshot of a repository.
//
// The snapshot is loaded once per repository state via the AKG transaction
// manager and cached; whenever .glassmarble/akg.json changes (a new
// `gmb analyze` run), the next call transparently reloads it. This gives the
// agent tools the full graph algorithm surface (cycles, PageRank, impact
// radius, ...) without re-parsing the JSON state for every query.
package akgbridge

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/akg"
)

// Status is a cheap stat-only view of the AKG database.
type Status struct {
	Exists   bool
	Path     string
	Size     int64
	Modified time.Time
}

type fileStat struct {
	size    int64
	modTime time.Time
}

// Bridge lazily loads and caches the AKG snapshot for one repository.
// All methods are safe for concurrent use.
type Bridge struct {
	rootDir string
	akgDir  string

	mu   sync.Mutex
	stat fileStat
	snap *akg.CodePropertyGraph
}

// New returns a bridge for the repository rooted at rootDir.
func New(rootDir string) *Bridge {
	return &Bridge{
		rootDir: rootDir,
		akgDir:  filepath.Join(rootDir, ".glassmarble"),
	}
}

// RootDir returns the repository root the bridge serves.
func (b *Bridge) RootDir() string { return b.rootDir }

// Status returns stat-only information about the canonical akg.json state
// without loading the graph itself.
func (b *Bridge) Status() Status {
	st := Status{Path: filepath.Join(b.akgDir, "akg.json")}
	if fi, err := os.Stat(st.Path); err == nil {
		st.Exists = true
		st.Size = fi.Size()
		st.Modified = fi.ModTime()
	}
	return st
}

// Clear drops the cached snapshot so the next Snapshot call reloads from
// disk even if the state is unchanged.
func (b *Bridge) Clear() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.snap = nil
	b.stat = fileStat{}
}

// Snapshot returns the current AKG snapshot, reloading it whenever akg.json
// has changed since the last load. A missing database is reported with
// actionable guidance.
func (b *Bridge) Snapshot() (*akg.CodePropertyGraph, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	st := b.Status()
	if !st.Exists {
		b.snap = nil
		b.stat = fileStat{}
		return nil, errNoAKG(st.Path)
	}
	cur := fileStat{size: st.Size, modTime: st.Modified}
	if b.snap != nil && b.stat == cur {
		return b.snap, nil
	}

	// The transaction manager self-heals from the legacy TTL on open and
	// writes a fresh akg.json; Close() flushes any pending writes before we
	// hand the snapshot to concurrent readers.
	tm, err := akg.NewAKGTransactionManager(b.akgDir)
	if err != nil {
		return nil, fmt.Errorf("failed to open AKG database: %w", err)
	}
	snap := tm.GetActiveSnapshot()
	tm.Close()
	if snap == nil {
		b.snap = nil
		b.stat = fileStat{}
		return nil, fmt.Errorf("AKG database at %s is empty — run `gmb analyze` first", st.Path)
	}
	// The open itself may rewrite the state (cache repair), so re-stat before
	// caching: the next call must not spuriously reload.
	fresh := b.Status()
	repairRestoredIndexes(snap)
	b.snap = snap
	b.stat = fileStat{size: fresh.Size, modTime: fresh.Modified}
	return snap, nil
}

// repairRestoredIndexes repairs snapshot indexes that the AKG does not rebuild
// when reconstructing from the TTL. KindIndex is empty on restored graphs, so
// kind-filtered queries would silently return nothing even though nodes carry
// kinds (macro inference mutates nodes after the index was dropped). The
// repair only fills an empty index and never touches node/edge data.
func repairRestoredIndexes(snap *akg.CodePropertyGraph) {
	if snap == nil || snap.KindIndex == nil || snap.KindIndex.Len() > 0 {
		return
	}
	for _, node := range snap.Nodes.Values() {
		if node == nil || node.Kind == "" {
			continue
		}
		existing, _ := snap.KindIndex.Get(node.Kind)
		next := make(map[string]bool, len(existing)+1)
		for id := range existing {
			next[id] = true
		}
		next[node.ID] = true
		snap.KindIndex = snap.KindIndex.Set(node.Kind, next)
	}
}

// EdgeCount returns the total number of edges in the snapshot.
func EdgeCount(snap *akg.CodePropertyGraph) int {
	if snap == nil || snap.OutboundEdges == nil {
		return 0
	}
	total := 0
	for _, edges := range snap.OutboundEdges.Values() {
		total += len(edges)
	}
	return total
}

// GitCommit returns the short HEAD commit of the repository, or an error
// when it is not a git repository.
func GitCommit(ctx context.Context, rootDir string) (string, error) {
	out, err := exec.CommandContext(ctx, "git", "-C", rootDir, "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse failed: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

func errNoAKG(path string) error {
	return fmt.Errorf("AKG database not found at %s — run `gmb analyze` first", path)
}
