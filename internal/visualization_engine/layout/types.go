package normalize

import "github.com/Syamchand123/GlassMarble/internal/visualization_engine/types"

type DiagramMetrics struct {
	PageRank    map[string]float64
	Betweenness map[string]float64
	DegreeIn    map[string]int
	DegreeOut   map[string]int
	Communities map[string]string
	GodObjects  []string
	KCore       map[string]int
	SCCs        [][]string
	Summary     *types.GraphSummary
}
