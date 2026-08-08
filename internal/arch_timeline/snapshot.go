package arch_timeline

import "time"

// SnapshotIndexEntry represents a single entry in the snapshots index.json.
type SnapshotIndexEntry struct {
	CommitHash   string    `json:"commit_hash"`
	Timestamp    time.Time `json:"timestamp"`
	TopologyHash string    `json:"topology_hash"`
	PatternCount int       `json:"pattern_count"`
	SmellCount   int       `json:"smell_count"`
	SnapshotFile string    `json:"snapshot_file"`
}
