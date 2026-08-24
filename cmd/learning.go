package cmd

import (
	"os"
	"path/filepath"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/config"
	"github.com/Syamchand123/GlassMarble/internal/developer_memory"
	"github.com/Syamchand123/GlassMarble/internal/learning"
	"gopkg.in/yaml.v3"
)

// runLearning is the convention learning wiring point in the analysis pipeline
// (master plan §13.1 — learning runs continuously alongside the other
// phases). It deterministically re-extracts the repository's conventions
// from the committed graph and the developer memory, folds in the
// developer's accepted/rejected patterns from the correction log, and
// persists the result to .glassmarble/memory/conventions.json (atomic,
// derived, always recomputable).
//
// Corrections themselves are NOT applied here: they are an overlay that
// runs at query time (gmb memory), so they are reflected immediately
// without a re-analysis. This phase only refreshes the learned conventions.
//
// Non-fatal by design (§15.6): a failure here warns and continues — the
// graph is already committed and memory is already updated.
func runLearning(storageDir string, tm *akg.AKGTransactionManager, verbose bool) {
	repoDir := filepath.Dir(storageDir)

	cfg, err := loadLearningConfig(storageDir)
	if err != nil {
		if verbose {
			tuiPrintf("warning: could not load learning config: %v\n", err)
		}
	}
	if !cfg.LearnConventionsEnabled() {
		if verbose {
			tuiPrintln("Learning: convention learning disabled by config (learning.conventions_enabled=false)")
		}
		return
	}

	graph := tm.GetActiveGraph()
	if graph == nil || graph.Nodes == nil || graph.Nodes.Len() == 0 {
		return
	}

	store6 := developer_memory.NewStoreForRepo(repoDir).WithLogger(func(format string, args ...any) {
		tuiPrintf("warning: "+format+"\n", args...)
	})
	mem, err := store6.LoadMemory()
	if err != nil {
		tuiPrintf("warning: convention learning could not load developer memory: %v\n", err)
		mem = nil
	}

	corrStore := learning.NewStore(repoDir).WithLogger(func(format string, args ...any) {
		tuiPrintf("warning: "+format+"\n", args...)
	})
	learner := learning.NewLearner(corrStore, learning.WithConfig(cfg))
	preferred, rejected, perr := learner.PatternFeedback(mem)
	if perr != nil && verbose {
		tuiPrintf("warning: convention learning could not read pattern feedback: %v\n", perr)
	}

	conv := learning.LearnConventions(graph, mem,
		learning.WithMinEvidence(cfg.ConventionEvidenceThreshold()),
		learning.WithPatternFeedback(preferred, rejected))

	convStore := learning.NewConventionsStore(repoDir)
	if err := convStore.Save(conv); err != nil {
		tuiPrintf("warning: convention learning could not persist conventions: %v\n", err)
		return
	}

	if verbose {
		tuiPrintf("Learning: learned conventions (service=%q test=%q adr=%q layers=%d patterns=%d, %d preferred %d rejected)\n",
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
