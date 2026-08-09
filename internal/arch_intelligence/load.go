package arch_intelligence

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Syamchand123/GlassMarble/internal/archmodel"
)

// latestFile is the "current state" contract file: runMemoryStage (cmd/)
// writes the Stage 5 result here after every analysis, including topology-
// unchanged watch-mode saves.
const latestFile = "intelligence/latest.json"

// LoadLatestResult reads the persisted Stage 5 result written to
// <storageDir>/intelligence/latest.json by the analysis pipeline. storageDir
// is the .glassmarble directory. A missing file returns os.ErrNotExist so
// callers can fall back to the latest architecture snapshot.
func LoadLatestResult(storageDir string) (*Stage5Result, error) {
	if storageDir == "" {
		return nil, fmt.Errorf("arch_intelligence: LoadLatestResult requires a storage directory")
	}
	path := filepath.Join(storageDir, latestFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var res Stage5Result
	if err := json.Unmarshal(data, &res); err != nil {
		return nil, fmt.Errorf("arch_intelligence: parse %s: %w", path, err)
	}
	return &res, nil
}

// MetricSummary renders a one-line human-readable architectural metrics
// summary from a Stage 5A measurement. Used by the Stage 12 evidence context
// and the pattern tool so both share one rendering.
func MetricSummary(m archmodel.ArchMetrics) string {
	return fmt.Sprintf("%d nodes, %d edges, density %.3f, %d strongly-connected components, %d cycles, %.0f%% reachable from entrypoints, %d dead-code nodes, %d layer violations",
		m.TotalNodes, m.TotalEdges, m.GraphDensity,
		m.StronglyConnectedComponents, m.CycleCount,
		m.ReachableFromEntrypoints*100, m.DeadCodeNodeCount, m.LayerViolationCount)
}