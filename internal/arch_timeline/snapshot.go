package arch_timeline

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/archmodel"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage4"
)

// SnapshotIndexEntry represents a single entry in the snapshots index.json.
type SnapshotIndexEntry struct {
	CommitHash   string    `json:"commit_hash"`
	Timestamp    time.Time `json:"timestamp"`
	TopologyHash string    `json:"topology_hash"`
	PatternCount int       `json:"pattern_count"`
	SmellCount   int       `json:"smell_count"`
	SnapshotFile string    `json:"snapshot_file"`
}

// SnapshotInput carries the Stage 5 analysis results for one commit. It uses
// only archmodel types so arch_timeline never imports a stage package
// (LOLPAL dependency rule — arch_intelligence must not be reachable from
// here).
type SnapshotInput struct {
	Graph      *akg.CodePropertyGraph
	CommitHash string
	Version    string
	Timestamp  time.Time

	Components []archmodel.DetectedComponent
	Patterns   []archmodel.DetectedPattern
	Smells     []archmodel.ArchSmell
	Metrics    archmodel.ArchMetrics
}

// BuildSnapshot packages the Stage 5 outputs into a point-in-time
// ArchSnapshot ready for SnapshotStore.Create.
//
// The snapshot ID is deterministic: snap_ + sha256 of the commit hash and
// the graph's node/edge fingerprints, so re-building the same commit yields
// the same ID. TopologyHash is intentionally left empty — SnapshotStore
// computes it during Create (and skips the write when the topology is
// unchanged), so BuildSnapshot never needs to replay the graph.
func BuildSnapshot(in SnapshotInput) (*archmodel.ArchSnapshot, error) {
	snap := &archmodel.ArchSnapshot{
		CommitHash: in.CommitHash,
		Version:    in.Version,
		Timestamp:  in.Timestamp,
		Components: in.Components,
		Patterns:   in.Patterns,
		Smells:     in.Smells,
		Metrics:    in.Metrics,
	}

	if in.Graph != nil {
		var buf bytes.Buffer
		if err := akg.ExportGraphJSON(in.Graph, &buf); err != nil {
			return nil, fmt.Errorf("arch_timeline: export graph json: %w", err)
		}
		snap.AKGJSON = buf.Bytes()
		snap.NodeCount = in.Graph.Nodes.Len()
		in.Graph.OutboundEdges.Iterate(func(_ string, edges []stage4.ResolvedEdge) {
			snap.EdgeCount += len(edges)
		})
	}

	sum := sha256.Sum256([]byte(strings.Join([]string{
		in.CommitHash,
		string(snap.AKGJSON),
		fingerprintComponents(in.Components),
		fingerprintPatterns(in.Patterns),
		fingerprintSmells(in.Smells),
	}, "|")))
	snap.ID = "snap_" + hex.EncodeToString(sum[:8])
	return snap, nil
}

func fingerprintComponents(cs []archmodel.DetectedComponent) string {
	var parts []string
	for _, c := range cs {
		parts = append(parts, c.ID+"="+c.Name)
	}
	return strings.Join(parts, ",")
}

func fingerprintPatterns(ps []archmodel.DetectedPattern) string {
	var parts []string
	for _, p := range ps {
		parts = append(parts, string(p.Kind))
	}
	return strings.Join(parts, ",")
}

func fingerprintSmells(ss []archmodel.ArchSmell) string {
	var parts []string
	for _, s := range ss {
		parts = append(parts, string(s.Kind))
	}
	return strings.Join(parts, ",")
}
