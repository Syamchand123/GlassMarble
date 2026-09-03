package arch_intelligence

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Syamchand123/GlassMarble/internal/archmodel"
)

// latestFile is the "current state" contract file: runMemoryPipeline (cmd/)
// writes the intelligence result here after every analysis, including topology-
// unchanged watch-mode saves.
const latestFile = "intelligence/latest.json"

// LoadLatestResult reads the persisted intelligence result written to
// <storageDir>/intelligence/latest.json by the analysis pipeline. storageDir
// is the .glassmarble directory. A missing file returns os.ErrNotExist so
// callers can fall back to the latest architecture snapshot.
func LoadLatestResult(storageDir string) (*IntelligenceResult, error) {
	if storageDir == "" {
		return nil, fmt.Errorf("arch_intelligence: LoadLatestResult requires a storage directory")
	}
	path := filepath.Join(storageDir, latestFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var res IntelligenceResult
	if err := json.Unmarshal(data, &res); err != nil {
		return nil, fmt.Errorf("arch_intelligence: parse %s: %w", path, err)
	}
	return &res, nil
}

// MetricSummary renders a one-line human-readable architectural metrics
// summary from a intelligence metrics measurement. Used by the evidence retrieval evidence context
// and the pattern tool so both share one rendering.
func MetricSummary(m archmodel.ArchMetrics) string {
	// With no entrypoints the sweep returns no dead nodes, which the old
	// phrasing rendered as "100% reachable from entrypoints, 0 dead-code
	// nodes" -- a perfect score reported for a measurement that never ran.
	// This string is fed to the AI evidence context and the pattern tool, so
	// say the measurement is unavailable instead of inventing a result.
	reach := fmt.Sprintf("%.0f%% reachable from entrypoints, %d dead-code nodes",
		m.ReachableFromEntrypoints*100, m.DeadCodeNodeCount)
	if m.EntrypointCount == 0 {
		reach = "reachability not measured (no entrypoints detected)"
	}
	return fmt.Sprintf("%d nodes, %d edges, density %.3f, %d strongly-connected components, %d cycles, %s, %d layer violations",
		m.TotalNodes, m.TotalEdges, m.GraphDensity,
		m.StronglyConnectedComponents, m.CycleCount,
		reach, m.LayerViolationCount)
}
