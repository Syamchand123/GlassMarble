package arch_timeline

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/archmodel"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/link"
)

// SnapshotStore persists ArchSnapshots on disk and maintains an append-only
// index (index.json). Design (LOLPAL §5.2 / D2):
//
//   - Files are content-addressed: snap_<snapshot ID[0:8]>.json. The ID is a
//     hash of the commit + graph + analysis state, so the same commit always
//     maps to the same file and identical snapshots can never collide (a
//     problem the old commit-prefix naming had for empty/uncommitted hashes).
//   - The index is written atomically (temp file + fsync + rename), so a
//     crash mid-write can never corrupt it.
//   - loadIndex self-heals: a missing or corrupt index is rebuilt by scanning
//     the snapshot files in the directory, so a hand-edited or truncated
//     index.json does not lose history.
//   - Create skip-writes when the topology hash is unchanged since the most
//     recent snapshot (max timestamp, not last appended), and dedupes by
//     snapshot ID so re-analyzing a commit is always idempotent.
type SnapshotStore struct {
	dir string
	mu  sync.RWMutex
}

func NewSnapshotStore(dir string) (*SnapshotStore, error) {
	if dir == "" {
		return nil, fmt.Errorf("arch_timeline: snapshot store requires a directory")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("arch_timeline: create snapshot dir: %w", err)
	}
	return &SnapshotStore{dir: dir}, nil
}

// computeTopologyHash computes the sha256 of sorted (source, edge_type, target) tuples.
func computeTopologyHash(snap *archmodel.ArchSnapshot) (string, error) {
	graph, err := Replay(snap)
	if err != nil {
		return "", fmt.Errorf("failed to replay graph to compute hash: %w", err)
	}

	var edgeTuples []string
	graph.OutboundEdges.Iterate(func(source string, edges []link.ResolvedEdge) {
		for _, e := range edges {
			edgeTuples = append(edgeTuples, fmt.Sprintf("%s|%s|%s", source, e.Type, e.TargetID))
		}
	})

	sort.Strings(edgeTuples)

	h := sha256.New()
	for _, tuple := range edgeTuples {
		h.Write([]byte(tuple))
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

// Create persists snap and returns whether a file was actually written.
// It writes nothing (and returns false) when an identical snapshot already
// exists (same ID — idempotent re-analysis) or when the topology hash is
// unchanged since the latest snapshot. The write itself is atomic.
func (s *SnapshotStore) Create(snap *archmodel.ArchSnapshot) (bool, error) {
	if snap == nil {
		return false, fmt.Errorf("arch_timeline: Create requires a snapshot")
	}
	if snap.ID == "" {
		return false, fmt.Errorf("arch_timeline: Create requires a snapshot ID (use BuildSnapshot)")
	}
	if snap.Timestamp.IsZero() {
		return false, fmt.Errorf("arch_timeline: Create requires a non-zero timestamp")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Compute topology hash if missing and the graph is embedded.
	if snap.TopologyHash == "" && len(snap.AKGJSON) > 0 {
		hashStr, err := computeTopologyHash(snap)
		if err != nil {
			return false, err
		}
		snap.TopologyHash = hashStr
	}

	entries, err := s.loadIndexLocked()
	if err != nil {
		return false, err
	}

	// Idempotency: this exact snapshot (same commit + same analysis) is
	// already stored — re-analysis must never duplicate history.
	for _, e := range entries {
		if e.SnapshotID == snap.ID {
			return false, nil
		}
	}

	// Skip-write: topology unchanged since the most recent snapshot. Only
	// meaningful when the hash could be computed (i.e. the graph is embedded).
	if snap.TopologyHash != "" {
		if latest := latestEntry(entries); latest != nil && latest.TopologyHash == snap.TopologyHash {
			return false, nil
		}
	}

	filename := fmt.Sprintf("snap_%s.json", snapshotFileID(snap.ID))
	snapPath := filepath.Join(s.dir, filename)

	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return false, err
	}
	if err := atomicWrite(snapPath, data); err != nil {
		return false, fmt.Errorf("arch_timeline: write snapshot %s: %w", filename, err)
	}

	entry := SnapshotIndexEntry{
		CommitHash:   snap.CommitHash,
		SnapshotID:   snap.ID,
		Timestamp:    snap.Timestamp,
		Order:        snap.Order,
		TopologyHash: snap.TopologyHash,
		PatternCount: len(snap.Patterns),
		SmellCount:   len(snap.Smells),
		SnapshotFile: filename,
	}
	entries = append(entries, entry)
	if err := s.saveIndexLocked(entries); err != nil {
		return false, err
	}
	return true, nil
}

// snapshotFileID is the file-safe portion of a snapshot ID: for
// "snap_01234567" this is "01234567"; for odd/malformed IDs it falls back to
// a stable hash so two different IDs can never map to one file.
func snapshotFileID(id string) string {
	if len(id) > 5 && id[:5] == "snap_" {
		rest := id[5:]
		if len(rest) > 8 {
			rest = rest[:8]
		}
		if rest != "" {
			return rest
		}
	}
	sum := sha256.Sum256([]byte(id))
	return fmt.Sprintf("%x", sum[:4])
}

// atomicWrite writes data to a temp file in dir, fsyncs it, then renames it
// over path. Readers either see the old file or the new file, never a
// partially written one.
func atomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// loadIndexLocked returns the index entries ordered by timestamp ascending.
// It self-heals: a missing index.json is rebuilt from the snapshot files on
// disk, and a corrupt index.json is discarded and rebuilt the same way.
// Callers must hold s.mu.
func (s *SnapshotStore) loadIndexLocked() ([]SnapshotIndexEntry, error) {
	path := filepath.Join(s.dir, "index.json")
	data, err := os.ReadFile(path)
	if err == nil {
		var entries []SnapshotIndexEntry
		if uerr := json.Unmarshal(data, &entries); uerr == nil {
			// Accept only indexes whose referenced files still exist; a
			// manually deleted snapshot should not leave a dangling entry.
			valid := entries[:0]
			for _, e := range entries {
				if _, err := os.Stat(filepath.Join(s.dir, e.SnapshotFile)); err == nil {
					valid = append(valid, e)
				}
			}
			return sortedEntries(valid), nil
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("arch_timeline: read index: %w", err)
	}

	// Missing or corrupt index → rebuild from the snapshot directory.
	rebuild, rebuildErr := s.rebuildIndexLocked()
	if rebuildErr != nil {
		return nil, rebuildErr
	}
	if rebuildErr := s.saveIndexLocked(rebuild); rebuildErr != nil {
		return nil, fmt.Errorf("arch_timeline: rebuild index: %w", rebuildErr)
	}
	return rebuild, nil
}

// rebuildIndexLocked scans the store directory for snap_*.json files and
// derives index entries from the snapshots themselves. Unreadable files are
// skipped so a single corrupt snapshot cannot hide the rest of history.
// Callers must hold s.mu.
func (s *SnapshotStore) rebuildIndexLocked() ([]SnapshotIndexEntry, error) {
	matches, err := filepath.Glob(filepath.Join(s.dir, "snap_*.json"))
	if err != nil {
		return nil, fmt.Errorf("arch_timeline: scan snapshot dir: %w", err)
	}
	var entries []SnapshotIndexEntry
	for _, m := range matches {
		data, rerr := os.ReadFile(m)
		if rerr != nil {
			continue
		}
		var snap archmodel.ArchSnapshot
		if uerr := json.Unmarshal(data, &snap); uerr != nil {
			continue
		}
		if snap.ID == "" {
			continue
		}
		entries = append(entries, SnapshotIndexEntry{
			CommitHash:   snap.CommitHash,
			SnapshotID:   snap.ID,
			Timestamp:    snap.Timestamp,
			Order:        snap.Order,
			TopologyHash: snap.TopologyHash,
			PatternCount: len(snap.Patterns),
			SmellCount:   len(snap.Smells),
			SnapshotFile: filepath.Base(m),
		})
	}
	if len(entries) == 0 {
		return nil, nil
	}
	return sortedEntries(entries), nil
}

// saveIndexLocked persists the index atomically. Callers must hold s.mu.
func (s *SnapshotStore) saveIndexLocked(entries []SnapshotIndexEntry) error {
	path := filepath.Join(s.dir, "index.json")
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	if err := atomicWrite(path, data); err != nil {
		return fmt.Errorf("arch_timeline: write index: %w", err)
	}
	return nil
}

func sortedEntries(entries []SnapshotIndexEntry) []SnapshotIndexEntry {
	sort.Slice(entries, func(i, j int) bool {
		if !entries[i].Timestamp.Equal(entries[j].Timestamp) {
			return entries[i].Timestamp.Before(entries[j].Timestamp)
		}
		// Same-second commits (author-time resolution): the git-history
		// hint breaks the tie. Zero means unknown (uncommitted states) and
		// loses ties to a hint-bearing commit.
		if entries[i].Order != entries[j].Order && entries[i].Order != 0 && entries[j].Order != 0 {
			return entries[i].Order < entries[j].Order
		}
		if entries[i].CommitHash != entries[j].CommitHash {
			return entries[i].CommitHash < entries[j].CommitHash
		}
		return entries[i].SnapshotID < entries[j].SnapshotID
	})
	return entries
}

func latestEntry(entries []SnapshotIndexEntry) *SnapshotIndexEntry {
	if len(entries) == 0 {
		return nil
	}
	// entries are timestamp-ascending (loadIndexLocked guarantees it).
	return &entries[len(entries)-1]
}

// loadSnapshotLocked reads and decodes one snapshot file. Callers must hold
// the read lock.
func (s *SnapshotStore) loadSnapshotLocked(e SnapshotIndexEntry) (*archmodel.ArchSnapshot, error) {
	data, err := os.ReadFile(filepath.Join(s.dir, e.SnapshotFile))
	if err != nil {
		return nil, fmt.Errorf("arch_timeline: read snapshot %s: %w", e.SnapshotFile, err)
	}
	var snap archmodel.ArchSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return nil, fmt.Errorf("arch_timeline: parse snapshot %s: %w", e.SnapshotFile, err)
	}
	return &snap, nil
}

// List returns the index entries ordered oldest-first. Entries are cheap
// (no snapshot payloads are loaded).
func (s *SnapshotStore) List() []SnapshotIndexEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entries, err := s.loadIndexLocked()
	if err != nil {
		return nil
	}
	out := make([]SnapshotIndexEntry, len(entries))
	copy(out, entries)
	return out
}

// Count returns the number of indexed snapshots (0 on a broken store).
func (s *SnapshotStore) Count() int {
	return len(s.List())
}

// Get returns the snapshot whose commit hash starts with commitHashPrefix.
// The prefix must match at least one entry; when several entries share the
// prefix (ambiguous short prefix), the most recent one wins deterministically.
// Use NearestAt for the "closest commit" fallback semantics.
func (s *SnapshotStore) Get(commitHashPrefix string) (*archmodel.ArchSnapshot, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entries, err := s.loadIndexLocked()
	if err != nil {
		return nil, err
	}
	if commitHashPrefix == "" {
		return nil, fmt.Errorf("arch_timeline: Get requires a commit hash prefix")
	}
	var best *SnapshotIndexEntry
	for i := range entries {
		e := &entries[i]
		if strings.HasPrefix(e.CommitHash, commitHashPrefix) {
			if best == nil || e.Timestamp.After(best.Timestamp) {
				best = e
			}
		}
	}
	if best == nil {
		return nil, fmt.Errorf("arch_timeline: no snapshot found for commit prefix %q", commitHashPrefix)
	}
	return s.loadSnapshotLocked(*best)
}

// GetBySnapshotID returns the snapshot whose content-addressed ID matches.
// Accepts the full ID ("snap_01234567") or the 8-hex file portion.
func (s *SnapshotStore) GetBySnapshotID(id string) (*archmodel.ArchSnapshot, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if id == "" {
		return nil, fmt.Errorf("arch_timeline: GetBySnapshotID requires a snapshot ID")
	}
	entries, err := s.loadIndexLocked()
	if err != nil {
		return nil, err
	}
	short := strings.TrimPrefix(id, "snap_")
	for _, e := range entries {
		if e.SnapshotID == id || strings.TrimPrefix(e.SnapshotID, "snap_") == short {
			return s.loadSnapshotLocked(e)
		}
	}
	return nil, fmt.Errorf("arch_timeline: no snapshot found for ID %q", id)
}

// NearestAt returns the snapshot with the greatest timestamp not after ts —
// the snapshot that best describes the architecture "at" a given moment
// (e.g. at a commit between two snapshots). When no snapshot precedes ts,
// the oldest snapshot is returned. Errors on an empty store.
func (s *SnapshotStore) NearestAt(ts time.Time) (*archmodel.ArchSnapshot, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entries, err := s.loadIndexLocked()
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("arch_timeline: no snapshots found")
	}
	// entries are timestamp-ascending: the last entry not after ts is the
	// closest preceding snapshot.
	var best *SnapshotIndexEntry
	for i := range entries {
		if !entries[i].Timestamp.After(ts) {
			best = &entries[i]
		}
	}
	// Nothing before ts → the oldest snapshot is the best approximation.
	if best == nil {
		best = &entries[0]
	}
	return s.loadSnapshotLocked(*best)
}

// Latest returns the most recent snapshot by timestamp (not by append order).
func (s *SnapshotStore) Latest() (*archmodel.ArchSnapshot, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entries, err := s.loadIndexLocked()
	if err != nil {
		return nil, err
	}
	latest := latestEntry(entries)
	if latest == nil {
		return nil, fmt.Errorf("arch_timeline: no snapshots found")
	}
	return s.loadSnapshotLocked(*latest)
}
