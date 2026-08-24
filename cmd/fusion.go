package cmd

import (
	"context"
	"os"
	"path/filepath"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/config"
	"github.com/Syamchand123/GlassMarble/internal/developer_memory"
	"github.com/Syamchand123/GlassMarble/internal/knowledge_fusion"
	"gopkg.in/yaml.v3"
)

// runFusion is the knowledge fusion wiring point (master plan §13.1). It runs
// the knowledge fusion engine over the committed graph and the repository's
// documentation and git history, then appends the fused claims to developer
// memory — where they become queryable through `gmb memory --ask`.
//
// Wiring discipline (mirrors loadIntelligenceConfig in patterns.go):
//
//   - the "fusion:" section of .glassmarble/config.yaml drives the engine;
//     an absent section means the built-in defaults,
//   - the LocalGitAdapter is installed whenever git sources are enabled, so
//     PR/issue claims work with zero configuration,
//   - the whole phase is non-fatal (§15.6): a fusion failure warns and
//     continues — `gmb analyze` must never fail after the graph is
//     committed.
func runFusion(storageDir string, tm *akg.AKGTransactionManager, verbose bool) {
	repoDir := filepath.Dir(storageDir)
	graph := tm.GetActiveGraph()

	cfg, err := loadFusionConfig(storageDir)
	if err != nil && verbose {
		tuiPrintf("warning: could not load fusion config: %v\n", err)
	}

	store := developer_memory.NewStoreForRepo(repoDir).WithLogger(func(format string, args ...any) {
		tuiPrintf("warning: "+format+"\n", args...)
	})

	opts := []knowledge_fusion.Option{
		knowledge_fusion.WithLogger(func(format string, args ...any) {
			tuiPrintf("warning: "+format+"\n", args...)
		}),
	}
	if cfg.GitSourcesEnabled() {
		// C6-D10: share a single LocalGitAdapter so the same `git log` walk
		// is not executed twice (PR + Issue previously did double walks).
		sharedAdapter := &knowledge_fusion.LocalGitAdapter{
			RepoDir:    repoDir,
			MaxCommits: cfg.MaxCommits,
			Warnf: func(format string, args ...any) {
				tuiPrintf("warning: "+format+"\n", args...)
			},
		}
		opts = append(opts,
			knowledge_fusion.WithPRAdapter(sharedAdapter),
			knowledge_fusion.WithIssueAdapter(sharedAdapter),
		)
	}

	res, err := knowledge_fusion.NewFusionEngine(cfg, store, opts...).Run(context.Background(), repoDir, graph)
	if err != nil {
		tuiPrintf("warning: knowledge fusion failed: %v\n", err)
		return
	}
	if verbose {
		tuiPrintf("Fusion: fused %d claims from %d sources (ADR=%d README=%d PR=%d issue=%d, %d new)\n",
			res.TotalClaims, res.Sources, res.AdrFiles, res.ReadmeFiles, res.PRs, res.Issues, res.NewClaims)
	} else if res.TotalClaims > 0 {
		tuiPrintf("Fusion: fused %d knowledge claim(s) from %d source(s)\n", res.TotalClaims, res.Sources)
	}
}

// loadFusionConfig reads the "fusion:" section of .glassmarble/config.yaml,
// falling back to the built-in defaults when the file or section is absent.
func loadFusionConfig(storageDir string) (*config.FusionConfig, error) {
	cfg := config.DefaultFusionConfig()
	data, err := os.ReadFile(filepath.Join(storageDir, "config.yaml"))
	if err != nil {
		return cfg, nil
	}
	var local config.Config
	if err := yaml.Unmarshal(data, &local); err != nil {
		return cfg, nil
	}
	if local.Fusion != nil {
		cfg = local.Fusion
	}
	cfg.ApplyDefaults()
	return cfg, nil
}
