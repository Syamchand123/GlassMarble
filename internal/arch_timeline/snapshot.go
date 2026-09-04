package arch_timeline

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/archmodel"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/link"
)

// SnapshotIndexEntry represents a single entry in the snapshots index.json.
type SnapshotIndexEntry struct {
	CommitHash string    `json:"commit_hash"`
	SnapshotID string    `json:"snapshot_id"`
	Timestamp  time.Time `json:"timestamp"`
	// Order is the git-history position hint (rev-list --count) that breaks
	// timestamp ties between same-second commits; see ArchSnapshot.Order.
	Order        int64  `json:"order,omitempty"`
	TopologyHash string `json:"topology_hash"`
	PatternCount int    `json:"pattern_count"`
	SmellCount   int    `json:"smell_count"`
	SnapshotFile string `json:"snapshot_file"`
}

// SnapshotInput carries the Architecture Intelligence analysis results for one commit. It uses
// only archmodel types so arch_timeline never imports a phase package
// (LOLPAL dependency rule — arch_intelligence must not be reachable from
// here).
type SnapshotInput struct {
	Graph      *akg.CodePropertyGraph
	CommitHash string
	Version    string
	Timestamp  time.Time
	// Order is the commit's position in git history (git rev-list --count).
	// The store uses it to order snapshots that share the same author
	// timestamp, so "latest" and skip-write stay correct in commit bursts.
	Order int64

	Components []archmodel.DetectedComponent
	Patterns   []archmodel.DetectedPattern
	Smells     []archmodel.ArchSmell
	Metrics    archmodel.ArchMetrics

	// NoGraph skips embedding the full AKG into the snapshot. This shrinks
	// files for repos where --replay is never used, at the cost of disabling
	// structural diffs, replay, and topology-hash skip-write (the topology
	// hash cannot be computed without the graph).
	NoGraph bool
}

// BuildSnapshot packages the Architecture Intelligence outputs into a point-in-time
// ArchSnapshot ready for SnapshotStore.Create.
//
// The snapshot ID is deterministic: snap_ + sha256 of the commit hash and the
// graph/analysis fingerprints, so re-building the same commit yields the same
// ID. TopologyHash is intentionally left empty — SnapshotStore computes it
// during Create (and skips the write when the topology is unchanged), so
// BuildSnapshot never needs to replay the graph.
func BuildSnapshot(in SnapshotInput) (*archmodel.ArchSnapshot, error) {
	if in.Timestamp.IsZero() {
		return nil, fmt.Errorf("arch_timeline: BuildSnapshot requires a non-zero timestamp")
	}
	if !in.NoGraph && in.Graph == nil {
		return nil, fmt.Errorf("arch_timeline: BuildSnapshot requires a graph unless NoGraph is set")
	}
	if err := validateEvidence(in); err != nil {
		return nil, err
	}

	snap := &archmodel.ArchSnapshot{
		CommitHash: in.CommitHash,
		Version:    in.Version,
		Timestamp:  in.Timestamp.UTC(),
		Order:      in.Order,
		Components: in.Components,
		Patterns:   in.Patterns,
		Smells:     in.Smells,
		Metrics:    in.Metrics,
	}

	if !in.NoGraph && in.Graph != nil {
		var buf bytes.Buffer
		if err := akg.ExportGraphJSONCompact(in.Graph, &buf); err != nil {
			return nil, fmt.Errorf("arch_timeline: export graph json: %w", err)
		}
		snap.AKGJSON = buf.Bytes()
		snap.NodeCount = in.Graph.Nodes.Len()
		in.Graph.OutboundEdges.Iterate(func(_ string, edges []link.ResolvedEdge) {
			snap.EdgeCount += len(edges)
		})
	}

	sum := sha256.Sum256([]byte(strings.Join([]string{
		in.CommitHash,
		string(snap.AKGJSON),
		fingerprintComponents(in.Components),
		fingerprintPatterns(in.Patterns),
		fingerprintSmells(in.Smells),
		fingerprintMetrics(in.Metrics),
	}, "\x00")))
	snap.ID = "snap_" + hex.EncodeToString(sum[:8])
	return snap, nil
}

// validateEvidence enforces the evidence discipline (model.go): every
// detected component, pattern and smell must carry a non-empty evidence
// bundle. A snapshot derived from unbounded analysis is not trustworthy, so
// we fail fast with a precise diagnostic instead of persisting it.
func validateEvidence(in SnapshotInput) error {
	var missing []string
	for i, c := range in.Components {
		if c.Evidence.IsEmpty() {
			missing = append(missing, fmt.Sprintf("component #%d (%q)", i, c.Name))
		}
	}
	for i, p := range in.Patterns {
		if p.Evidence.IsEmpty() {
			missing = append(missing, fmt.Sprintf("pattern #%d (%q)", i, p.Name))
		}
	}
	for i, s := range in.Smells {
		if s.Evidence.IsEmpty() {
			missing = append(missing, fmt.Sprintf("smell #%d (%q)", i, s.Title))
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("arch_timeline: %d item(s) carry no evidence: %s",
			len(missing), strings.Join(missing, ", "))
	}
	return nil
}

// canonicalJSON hashes any value through its canonical JSON encoding, so
// keys can never collide on separators and the hash is deterministic.
func canonicalJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("unmarshalable:%s", fmt.Sprint(v))
	}
	return string(b)
}

// sortedStrings returns a sorted copy of s.
func sortedStrings(s []string) []string {
	c := append([]string(nil), s...)
	sort.Strings(c)
	return c
}

func fingerprintComponents(cs []archmodel.DetectedComponent) string {
	parts := make([]string, 0, len(cs))
	for _, c := range cs {
		parts = append(parts, canonicalJSON(struct {
			ID, Name          string
			Kind              archmodel.ComponentKind
			NodeIDs           []string
			Directories       []string
			Confidence        float64
			Dependencies      []string
			EvidenceSignature string
		}{
			ID: c.ID, Name: c.Name, Kind: c.Kind,
			NodeIDs: sortedStrings(c.NodeIDs), Directories: sortedStrings(c.Directories),
			Confidence: c.Confidence, Dependencies: sortedStrings(c.Dependencies),
			EvidenceSignature: canonicalJSON(c.Evidence),
		}))
	}
	return strings.Join(parts, "\x00")
}

func fingerprintPatterns(ps []archmodel.DetectedPattern) string {
	parts := make([]string, 0, len(ps))
	for _, p := range ps {
		parts = append(parts, canonicalJSON(struct {
			Kind              archmodel.PatternKind
			Name              string
			Components        []string
			Confidence        float64
			EvidenceSignature string
		}{
			Kind: p.Kind, Name: p.Name, Components: sortedStrings(p.Components),
			Confidence: p.Confidence, EvidenceSignature: canonicalJSON(p.Evidence),
		}))
	}
	return strings.Join(parts, "\x00")
}

func fingerprintSmells(ss []archmodel.ArchSmell) string {
	parts := make([]string, 0, len(ss))
	for _, s := range ss {
		parts = append(parts, canonicalJSON(struct {
			Kind              archmodel.SmellKind
			Title             string
			Severity          archmodel.Severity
			AffectedIDs       []string
			EvidenceSignature string
		}{
			Kind: s.Kind, Title: s.Title, Severity: s.Severity,
			AffectedIDs: sortedStrings(s.AffectedIDs), EvidenceSignature: canonicalJSON(s.Evidence),
		}))
	}
	return strings.Join(parts, "\x00")
}

// fingerprintMetrics folds the quality metrics into the fingerprint. Metrics
// are derived from the graph, so this never invalidates a stable commit's ID,
// but it keeps --no-graph snapshots (which carry no AKGJSON) distinguishable.
func fingerprintMetrics(m archmodel.ArchMetrics) string {
	return canonicalJSON(struct {
		Density         float64
		CycleCount      int
		LayerViolations int
		LCOM4           float64
		AvgFanIn        float64
		SCCs            int
		DeadCode        int
	}{
		Density: m.GraphDensity, CycleCount: m.CycleCount, LayerViolations: m.LayerViolationCount,
		LCOM4: m.LCOM4, AvgFanIn: m.AvgFanIn, SCCs: m.StronglyConnectedComponents, DeadCode: m.DeadCodeNodeCount,
	})
}
