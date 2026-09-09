package federation

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	docconfig "github.com/Syamchand123/GlassMarble/internal/doc_engine/config"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/storage"
)

// RottingArea documents a code subsystem with high commit velocity but low/stale documentation.
type RottingArea struct {
	Path        string `json:"path"`
	Freshness   int    `json:"freshness"`
	Velocity    int    `json:"velocity"`
	RiskScore   int    `json:"risk_score"`
	Description string `json:"description"`
}

// DebtReport summarizes repository documentation health, coverage, and ROI.
type DebtReport struct {
	TotalDocuments   int           `json:"total_documents"`
	TotalSections    int           `json:"total_sections"`
	GlobalFreshness  int           `json:"global_freshness"`
	CoverageRatio    float64       `json:"coverage_ratio"`
	TotalTokensUsed  int           `json:"total_tokens_used"`
	DriftedDocuments []string      `json:"drifted_documents"`
	RottingAreas     []RottingArea `json:"rotting_areas"`
	EstimatedHoursSaved float64    `json:"estimated_hours_saved"`
}

// GenerateDebtReport calculates documentation coverage, rotting areas, and ROI analytics.
func GenerateDebtReport(repoRoot string) (DebtReport, error) {
	cfg, err := docconfig.LoadDocsConfig(repoRoot)
	if err != nil {
		return DebtReport{}, err
	}

	sm := storage.NewStateManager(docconfig.StorageDirPath(repoRoot))
	state, _ := sm.Load()

	totalSecs := 0
	freshSum := 0
	tokens := 0
	var drifted []string

	for _, d := range cfg.Documents {
		totalSecs += len(d.Sections)
		score := 100
		if state != nil && state.Documents != nil {
			if ds, ok := state.Documents[d.TargetPath]; ok {
				if ds.FreshnessScore > 0 {
					score = ds.FreshnessScore
				}
				for _, ss := range ds.Sections {
					tokens += ss.LastTokenCost
				}
			}
		}

		if score < cfg.Constraints.MinFreshnessThreshold {
			drifted = append(drifted, d.TargetPath)
		}
		freshSum += score
	}

	globalFresh := 100
	if len(cfg.Documents) > 0 {
		globalFresh = freshSum / len(cfg.Documents)
	}

	// Calculate coverage ratio: documented packages vs total packages
	totalPkgs, documentedPkgs := countPackageCoverage(repoRoot, cfg)
	covRatio := 100.0
	if totalPkgs > 0 {
		covRatio = float64(documentedPkgs) / float64(totalPkgs) * 100.0
	}

	// Identify top rotting areas
	rotting := identifyRottingAreas(repoRoot, cfg, state)

	return DebtReport{
		TotalDocuments:   len(cfg.Documents),
		TotalSections:    totalSecs,
		GlobalFreshness:  globalFresh,
		CoverageRatio:    covRatio,
		TotalTokensUsed:  tokens,
		DriftedDocuments: drifted,
		RottingAreas:     rotting,
		EstimatedHoursSaved: float64(totalSecs) * 0.75, // average 45 mins saved per managed section
	}, nil
}

func countPackageCoverage(repoRoot string, cfg *docconfig.DocsConfig) (int, int) {
	pkgs := make(map[string]bool)
	internalDir := filepath.Join(repoRoot, "internal")

	_ = filepath.WalkDir(internalDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil
		}
		name := d.Name()
		if name == ".git" || name == "vendor" || name == "testdata" {
			return filepath.SkipDir
		}
		rel, _ := filepath.Rel(repoRoot, path)
		pkgs[filepath.ToSlash(rel)] = true
		return nil
	})

	docPkgs := 0
	for p := range pkgs {
		for _, d := range cfg.Documents {
			for _, sp := range d.Scope.Paths {
				prefix := strings.TrimSuffix(sp, "/**")
				if strings.HasPrefix(p, prefix) {
					docPkgs++
					break
				}
			}
		}
	}

	return len(pkgs), docPkgs
}

func identifyRottingAreas(repoRoot string, cfg *docconfig.DocsConfig, state *storage.DocEngineState) []RottingArea {
	var areas []RottingArea
	tracked := make(map[string]bool)
	for _, d := range cfg.Documents {
		for _, p := range d.Scope.Paths {
			tracked[strings.TrimSuffix(p, "/**")] = true
		}
	}

	// Any internal directory with Go files that lacks a doc spec is a candidate rotting area
	internalDir := filepath.Join(repoRoot, "internal")
	_ = filepath.WalkDir(internalDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(repoRoot, path)
		norm := filepath.ToSlash(rel)
		if norm == "internal" {
			return nil
		}

		if !tracked[norm] {
			// Count go files
			entries, _ := os.ReadDir(path)
			goFiles := 0
			for _, e := range entries {
				if strings.HasSuffix(e.Name(), ".go") {
					goFiles++
				}
			}

			if goFiles > 0 {
				areas = append(areas, RottingArea{
					Path:        norm,
					Freshness:   0,
					Velocity:    goFiles * 2,
					RiskScore:   goFiles * 3,
					Description: "Active code area without any managed documentation specification",
				})
			}
		}
		return nil
	})

	sort.Slice(areas, func(i, j int) bool {
		return areas[i].RiskScore > areas[j].RiskScore
	})

	if len(areas) > 5 {
		areas = areas[:5]
	}
	return areas
}
