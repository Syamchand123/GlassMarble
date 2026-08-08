package arch_timeline

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/Syamchand123/GlassMarble/internal/archmodel"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage4"
)

type SnapshotStore struct {
	dir string
	mu  sync.RWMutex
}

func NewSnapshotStore(dir string) *SnapshotStore {
	os.MkdirAll(dir, 0755)
	return &SnapshotStore{dir: dir}
}

// computeTopologyHash computes the sha256 of sorted (source, edge_type, target) tuples.
func computeTopologyHash(snap *archmodel.ArchSnapshot) (string, error) {
	graph, err := Replay(snap)
	if err != nil {
		return "", fmt.Errorf("failed to replay graph to compute hash: %w", err)
	}

	var edgeTuples []string
	graph.OutboundEdges.Iterate(func(source string, edges []stage4.ResolvedEdge) {
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

func (s *SnapshotStore) Create(snap *archmodel.ArchSnapshot) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Compute topology hash if missing
	if snap.TopologyHash == "" && len(snap.AKGJSON) > 0 {
		hashStr, err := computeTopologyHash(snap)
		if err != nil {
			return err
		}
		snap.TopologyHash = hashStr
	}

	entries, _ := s.loadIndex()
	if len(entries) > 0 {
		last := entries[len(entries)-1]
		if last.TopologyHash == snap.TopologyHash {
			// No architectural change, skip writing file
			return nil
		}
	}

	hashPrefix := snap.CommitHash
	if len(hashPrefix) > 8 {
		hashPrefix = hashPrefix[:8]
	}
	filename := fmt.Sprintf("snap_%s.json", hashPrefix)
	snapPath := filepath.Join(s.dir, filename)

	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(snapPath, data, 0644); err != nil {
		return err
	}

	entry := SnapshotIndexEntry{
		CommitHash:   snap.CommitHash,
		Timestamp:    snap.Timestamp,
		TopologyHash: snap.TopologyHash,
		PatternCount: len(snap.Patterns),
		SmellCount:   len(snap.Smells),
		SnapshotFile: filename,
	}
	entries = append(entries, entry)
	return s.saveIndex(entries)
}

func (s *SnapshotStore) loadIndex() ([]SnapshotIndexEntry, error) {
	path := filepath.Join(s.dir, "index.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var entries []SnapshotIndexEntry
	err = json.Unmarshal(data, &entries)
	return entries, err
}

func (s *SnapshotStore) saveIndex(entries []SnapshotIndexEntry) error {
	path := filepath.Join(s.dir, "index.json")
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func (s *SnapshotStore) List() []SnapshotIndexEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entries, _ := s.loadIndex()
	return entries
}

func (s *SnapshotStore) Get(commitHashPrefix string) (*archmodel.ArchSnapshot, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entries, err := s.loadIndex()
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if len(e.CommitHash) >= len(commitHashPrefix) && e.CommitHash[:len(commitHashPrefix)] == commitHashPrefix {
			path := filepath.Join(s.dir, e.SnapshotFile)
			data, err := os.ReadFile(path)
			if err != nil {
				return nil, err
			}
			var snap archmodel.ArchSnapshot
			if err := json.Unmarshal(data, &snap); err != nil {
				return nil, err
			}
			return &snap, nil
		}
	}
	return nil, fmt.Errorf("snapshot not found for %s", commitHashPrefix)
}

func (s *SnapshotStore) Latest() (*archmodel.ArchSnapshot, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entries, err := s.loadIndex()
	if err != nil || len(entries) == 0 {
		return nil, fmt.Errorf("no snapshots found")
	}
	last := entries[len(entries)-1]
	path := filepath.Join(s.dir, last.SnapshotFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var snap archmodel.ArchSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return nil, err
	}
	return &snap, nil
}
