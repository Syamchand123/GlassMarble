package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/config"
	"github.com/Syamchand123/GlassMarble/internal/developer_memory"
	"github.com/Syamchand123/GlassMarble/internal/learning"
	"gopkg.in/yaml.v3"
)

// runLearningStage is the Stage 10 wiring point in the analysis pipeline
// (master plan §13.1 — learning runs continuously alongside the other
// stages). It deterministically re-extracts the repository's conventions
// from the committed graph and the developer memory, folds in the
// developer's accepted/rejected patterns from the correction log, and
// persists the result to .glassmarble/memory/conventions.json (atomic,
// derived, always recomputable).
//
// Corrections themselves are NOT applied here: they are an overlay that
// runs at query time (gmb memory), so they are reflected immediately
// without a re-analysis. This stage only refreshes the learned conventions.
//
// Non-fatal by design (§15.6): a failure here warns and continues — the
// graph is already committed and memory is already updated.
func runLearningStage(storageDir string, tm *akg.AKGTransactionManager, verbose bool) {
	repoDir := filepath.Dir(storageDir)

	cfg, err := loadLearningConfig(storageDir)
	if err != nil {
		if verbose {
			fmt.Printf("warning: could not load learning config: %v\n", err)
		}
	}
	if !cfg.LearnConventionsEnabled() {
		if verbose {
			fmt.Println("Stage 10: convention learning disabled by config (learning.conventions_enabled=false)")
		}
		return
	}

	graph := tm.GetActiveGraph()
	if graph == nil || graph.Nodes == nil || graph.Nodes.Len() == 0 {
		return
	}

	store6 := developer_memory.NewStoreForRepo(repoDir).WithLogger(func(format string, args ...any) {
		fmt.Printf("warning: "+format+"\n", args...)
	})
	mem, err := store6.LoadMemory()
	if err != nil {
		fmt.Printf("warning: Stage 10 could not load developer memory: %v\n", err)
		mem = nil
	}

	corrStore := learning.NewStore(repoDir).WithLogger(func(format string, args ...any) {
		fmt.Printf("warning: "+format+"\n", args...)
	})
	learner := learning.NewLearner(corrStore, learning.WithConfig(cfg))
	preferred, rejected, perr := learner.PatternFeedback(mem)
	if perr != nil && verbose {
		fmt.Printf("warning: Stage 10 could not read pattern feedback: %v\n", perr)
	}

	conv := learning.LearnConventions(graph, mem,
		learning.WithMinEvidence(cfg.ConventionEvidenceThreshold()),
		learning.WithPatternFeedback(preferred, rejected))

	convStore := learning.NewConventionsStore(repoDir)
	if err := convStore.Save(conv); err != nil {
		fmt.Printf("warning: Stage 10 could not persist conventions: %v\n", err)
		return
	}

	if verbose {
		fmt.Printf("Stage 10: learned conventions (service=%q test=%q adr=%q layers=%d patterns=%d, %d preferred %d rejected)\n",
			conv.ServiceNamingPattern.Value, conv.TestFilePattern.Value, conv.ADRDirectory.Value,
			len(conv.LayerDirectories), len(conv.LearnedPatterns),
			len(conv.PreferredPatterns), len(conv.RejectedPatterns))
	}
}

// loadLearningConfig reads the "learning:" section of
// .glassmarble/config.yaml, falling back to the built-in defaults when the
// file or section is absent. storageDir is the .glassmarble directory.
func loadLearningConfig(storageDir string) (*config.LearningConfig, error) {
	cfg := config.DefaultLearningConfig()
	data, err := os.ReadFile(filepath.Join(storageDir, "config.yaml"))
	if err != nil {
		return cfg, nil
	}
	var local config.Config
	if err := yaml.Unmarshal(data, &local); err != nil {
		return cfg, nil
	}
	if local.Learning != nil {
		cfg = local.Learning
	}
	cfg.ApplyDefaults()
	return cfg, nil
}
