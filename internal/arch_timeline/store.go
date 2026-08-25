package arch_timeline

import (
	"compress/gzip"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/archmodel"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/link"
)

// Max snapshots retained by default (P1 retention). Older snapshots are pruned
// on Create so .glassmarble/snapshots stays bounded even at 500k LOC.
const defaultMaxSnapshots = 30

// sidecarSuffix is the gzip-compressed graph sidecar for snapshots that
// embed a graph. Inline AKGJSON is omitted from the JSON file (RCA-1/RCA-2).
const sidecarSuffix = ".graph.json.gz"

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

// computeTopologyHash computes the sha256 of sorted node IDs plus sorted (source, edge_type, target) tuples.
// When the snapshot has no embedded graph (NoGraph), it falls back to the
// metrics/components fingerprint already used for ID generation (P2 — no replay needed).
func computeTopologyHash(snap *archmodel.ArchSnapshot) (string, error) {
	if len(snap.AKGJSON) == 0 {
		// No graph: use metrics/components hash so skip-write still works
		// without replaying. This is deterministic and matches fingerprintMetrics.
		h := sha256.New()
		h.Write([]byte(snap.TopologyHash))
		// Fallback to metrics hash if TopologyHash not yet set.
		if snap.TopologyHash == "" {
			// Use node/edge counts + metric fields as fallback
			fmt.Fprintf(h, "%d|%d|%d|%d", snap.NodeCount, snap.EdgeCount, snap.Metrics.CycleCount, snap.Metrics.LayerViolationCount)
			for _, c := range snap.Components {
				h.Write([]byte(c.ID))
				h.Write([]byte{0})
				h.Write([]byte(c.Name))
			}
		}
		return fmt.Sprintf("%x", h.Sum(nil)[:8]), nil
	}
	graph, err := Replay(snap)
	if err != nil {
		return "", fmt.Errorf("failed to replay graph to compute hash: %w", err)
	}

	var nodeIDs []string
	graph.Nodes.Iterate(func(id string, _ *link.ResolvedNode) {
		nodeIDs = append(nodeIDs, id)
	})
	sort.Strings(nodeIDs)

	var edgeTuples []string
	graph.OutboundEdges.Iterate(func(source string, edges []link.ResolvedEdge) {
		for _, e := range edges {
			edgeTuples = append(edgeTuples, fmt.Sprintf("%s|%s|%s", source, e.Type, e.TargetID))
		}
	})

	sort.Strings(edgeTuples)

	h := sha256.New()
	for _, id := range nodeIDs {
		h.Write([]byte(id))
		h.Write([]byte{0})
	}
	for _, tuple := range edgeTuples {
		h.Write([]byte(tuple))
		h.Write([]byte{0})
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

// writeSidecarGzip writes AKGJSON bytes as a gzipped sidecar file atomically.
func writeSidecarGzip(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-graph-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	gz := gzip.NewWriter(tmp)
	if _, err := gz.Write(data); err != nil {
		gz.Close()
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := gz.Close(); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, path)
}

// readSidecarGzip reads a gzipped sidecar file.
func readSidecarGzip(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	return io.ReadAll(gz)
}

// sidecarPath returns the sidecar file path for a snapshot file.
func sidecarPath(snapFilePath string) string {
	return snapFilePath + sidecarSuffix
}

// Create persists snap and returns whether a file was actually written.
// It writes nothing (and returns false) when an identical snapshot already
// exists (same ID — idempotent re-analysis) or when the topology hash is
// unchanged since the latest snapshot. The write itself is atomic.
// RCA-1/RCA-2: when the snapshot embeds a graph, the graph is written as a
// gzipped sidecar (.graph.json.gz) and omitted from the JSON file to avoid
// double-encoding and 5× size blow-up.
func (s *SnapshotStore) Create(snap *archmodel.ArchSnapshot) (bool, error) {
	return s.CreateWithOptions(snap, SnapshotCreateOptions{})
}

// SnapshotCreateOptions controls Create behavior.
type SnapshotCreateOptions struct {
	MaxCount int // retention cap; 0 = defaultMaxSnapshots
}

func (s *SnapshotStore) CreateWithOptions(snap *archmodel.ArchSnapshot, opts SnapshotCreateOptions) (bool, error) {
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

	// Compute topology hash if missing. For NoGraph snapshots the hash is
	// derived from metrics/components without replay (P2).
	if snap.TopologyHash == "" {
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

	// Skip-write: topology unchanged since the most recent snapshot.
	if snap.TopologyHash != "" {
		if latest := latestEntry(entries); latest != nil && latest.TopologyHash == snap.TopologyHash {
			return false, nil
		}
	}

	filename := fmt.Sprintf("snap_%s.json", snapshotFileID(snap.ID))
	snapPath := filepath.Join(s.dir, filename)

	// Sidecar handling: if the snapshot embeds a graph, write it as gzipped
	// sidecar and omit it from the JSON file (RCA-2). The JSON file then
	// contains only metadata (~few KB) instead of 50 MB escaped string.
	var sidecarData []byte
	hasGraph := len(snap.AKGJSON) > 0
	if hasGraph {
		sidecarData = snap.AKGJSON
		// Temporarily clear for JSON marshaling to avoid double-encoding.
		snap.AKGJSON = nil
		defer func() { snap.AKGJSON = sidecarData }()
	}

	data, err := json.Marshal(snap)
	if err != nil {
		return false, err
	}
	if err := atomicWrite(snapPath, data); err != nil {
		return false, fmt.Errorf("arch_timeline: write snapshot %s: %w", filename, err)
	}
	// Write sidecar after the JSON file so a crash never leaves a sidecar
	// without its index entry. Sidecar is gzipped compact GraphJSON.
	if hasGraph {
		sidecarFile := sidecarPath(snapPath)
		if err := writeSidecarGzip(sidecarFile, sidecarData); err != nil {
			// Roll back the JSON file on sidecar failure to keep atomicity.
			_ = os.Remove(snapPath)
			return false, fmt.Errorf("arch_timeline: write sidecar %s: %w", filepath.Base(sidecarFile), err)
		}
		// Restore for caller's view.
		snap.AKGJSON = sidecarData
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
	// Enforce retention cap (P1): keep at most N snapshots.
	maxCount := opts.MaxCount
	if maxCount <= 0 {
		maxCount = defaultMaxSnapshots
	}
	if len(entries) > maxCount {
		// Sort by timestamp ascending and drop oldest.
		entries = sortedEntries(entries)
		toDrop := len(entries) - maxCount
		for i := 0; i < toDrop; i++ {
			drop := entries[i]
			_ = os.Remove(filepath.Join(s.dir, drop.SnapshotFile))
			_ = os.Remove(sidecarPath(filepath.Join(s.dir, drop.SnapshotFile)))
		}
		entries = entries[toDrop:]
	}
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
// the read lock. It also loads the gzipped graph sidecar if present (RCA-2
// backward-compat: old snapshots have inline AKGJSON, new ones have sidecar).
func (s *SnapshotStore) loadSnapshotLocked(e SnapshotIndexEntry) (*archmodel.ArchSnapshot, error) {
	snapPath := filepath.Join(s.dir, e.SnapshotFile)
	data, err := os.ReadFile(snapPath)
	if err != nil {
		return nil, fmt.Errorf("arch_timeline: read snapshot %s: %w", e.SnapshotFile, err)
	}
	var snap archmodel.ArchSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return nil, fmt.Errorf("arch_timeline: parse snapshot %s: %w", e.SnapshotFile, err)
	}
	// If inline AKGJSON is empty, try sidecar (new format) or legacy
	// uncompressed sidecar.
	if len(snap.AKGJSON) == 0 {
		sidecar := sidecarPath(snapPath)
		if gzData, gerr := readSidecarGzip(sidecar); gerr == nil {
			snap.AKGJSON = gzData
		} else {
			// Legacy: check for uncompressed sidecar (migration)
			legacy := snapPath + ".graph.json"
			if ldata, lerr := os.ReadFile(legacy); lerr == nil {
				snap.AKGJSON = ldata
			}
		}
	}
	return &snap, nil
}

// DiskUsage returns total bytes and file count for the snapshot store
// (including sidecars and index). Used by status/housekeeping (RCA-4).
func (s *SnapshotStore) DiskUsage() (bytes int64, files int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entries, _ := s.loadIndexLocked()
	for _, e := range entries {
		if st, err := os.Stat(filepath.Join(s.dir, e.SnapshotFile)); err == nil {
			bytes += st.Size()
			files++
		}
		if st, err := os.Stat(sidecarPath(filepath.Join(s.dir, e.SnapshotFile))); err == nil {
			bytes += st.Size()
			files++
		}
		legacy := filepath.Join(s.dir, e.SnapshotFile) + ".graph.json"
		if st, err := os.Stat(legacy); err == nil {
			bytes += st.Size()
			files++
		}
	}
	if st, err := os.Stat(filepath.Join(s.dir, "index.json")); err == nil {
		bytes += st.Size()
		files++
	}
	return
}

// PruneKeepLast retains only the most recent keep snapshots (by timestamp).
// Returns pruned file count and reclaimed bytes.
func (s *SnapshotStore) PruneKeepLast(keep int) (prunedFiles int, prunedBytes int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := s.loadIndexLocked()
	if err != nil || len(entries) <= keep {
		return 0, 0
	}
	entries = sortedEntries(entries)
	toDrop := entries[:len(entries)-keep]
	remain := entries[len(entries)-keep:]
	for _, d := range toDrop {
		p := filepath.Join(s.dir, d.SnapshotFile)
		if st, err := os.Stat(p); err == nil {
			prunedBytes += st.Size()
			prunedFiles++
			_ = os.Remove(p)
		}
		sc := sidecarPath(p)
		if st, err := os.Stat(sc); err == nil {
			prunedBytes += st.Size()
			prunedFiles++
			_ = os.Remove(sc)
		}
		legacy := p + ".graph.json"
		if st, err := os.Stat(legacy); err == nil {
			prunedBytes += st.Size()
			prunedFiles++
			_ = os.Remove(legacy)
		}
	}
	_ = s.saveIndexLocked(remain)
	return
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
