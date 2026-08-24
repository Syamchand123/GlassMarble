package cmd

import (
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/arch_timeline"
	"github.com/Syamchand123/GlassMarble/internal/archmodel"
	"github.com/Syamchand123/GlassMarble/internal/config"
	"github.com/Syamchand123/GlassMarble/internal/developer_memory"
	"github.com/Syamchand123/GlassMarble/internal/knowledge_aging"
	"github.com/Syamchand123/GlassMarble/internal/learning"
	"gopkg.in/yaml.v3"
)

// runAging is the knowledge aging wiring point in the analysis pipeline
// (master plan §13.1 — knowledge aging runs on every analysis). It:
//
//  1. loads the "aging:" config (defaults when absent),
//  2. loads the latest architecture snapshot (nil when none exists yet —
//     aging still refreshes freshness and can promote experimental
//     components),
//  3. runs one aging pass: freshness decay on every claim, deterministic
//     state transitions (STATE_CHANGE events appended to the memory WAL),
//     and an atomically-saved freshened aggregate.
//
// Non-fatal by design (§15.6): a failure here warns and continues — the
// graph is committed, memory is updated, and aging can always be re-run.
func runAging(storageDir string, verbose bool) {
	cfg, err := loadAgingConfig(storageDir)
	if err != nil && verbose {
		tuiPrintf("warning: could not load aging config: %v\n", err)
	}
	if !cfg.AgingEnabled() {
		if verbose {
			tuiPrintln("Aging: knowledge aging disabled by config (aging.enabled=false)")
		}
		return
	}

	repoDir := filepath.Dir(storageDir)
	store6 := developer_memory.NewStoreForRepo(repoDir).WithLogger(func(format string, args ...any) {
		tuiPrintf("warning: "+format+"\n", args...)
	})
	pins := agingPinsFromCorrections(repoDir, store6, verbose)
	ager := knowledge_aging.NewAger(store6,
		knowledge_aging.WithConfig(cfg),
		knowledge_aging.WithPinnedStates(pins),
		knowledge_aging.WithLogger(func(format string, args ...any) {
			tuiPrintf("warning: "+format+"\n", args...)
		}))

	var snap *archmodel.ArchSnapshot
	if store, serr := arch_timeline.NewSnapshotStore(snapshotDir(storageDir)); serr == nil {
		snap, _ = store.Latest()
	}

	now := time.Now()
	transitions, aerr := ager.Age(snap, now)
	if aerr != nil {
		tuiPrintf("warning: knowledge aging failed: %v\n", aerr)
		return
	}

	mem, err := store6.LoadMemory()
	if err != nil {
		if verbose {
			tuiPrintf("warning: could not load memory for summary: %v\n", err)
		}
		return
	}
	avg, claims := averageFreshness(mem, now)
	if len(transitions) > 0 {
		tuiPrintf("Aging: %d state transition(s) | %d claim(s) aged | average freshness %.0f%%\n",
			len(transitions), claims, avg*100)
		for _, tr := range transitions {
			tuiPrintf("  %s: %s → %s (%s)\n", tr.Component, tr.OldState, tr.NewState, tr.RuleID)
		}
	} else if verbose {
		tuiPrintf("Aging: no state transitions | %d claim(s) aged | average freshness %.0f%%\n",
			claims, avg*100)
	}
}

// agingPinsFromCorrections derives the knowledge aging pin set from the convention learning
// correction log (master plan §8.2, §13.1): every STATE correction whose
// target is a memory component pins that component to the corrected state,
// so aging can never fight — or silently revert — a developer's explicit
// state decision. Corrections about claims, events or unknown targets are
// ignored. The correction log is not required to exist: a missing or
// unreadable log yields an empty pin set and aging simply runs un-pinned.
func agingPinsFromCorrections(repoDir string, store *developer_memory.MemoryStore, verbose bool) map[string]developer_memory.KnowledgeState {
	pins := map[string]developer_memory.KnowledgeState{}
	corrections, err := learning.NewStore(repoDir).LoadAll()
	if err != nil {
		if verbose {
			tuiPrintf("warning: could not load corrections for aging pins: %v\n", err)
		}
		return pins
	}
	mem, err := store.LoadMemory()
	if err != nil || mem == nil {
		return pins
	}
	for _, c := range corrections {
		if c.Kind != learning.CorrectionKindState {
			continue
		}
		state := developer_memory.KnowledgeState(c.CorrectedValue)
		if !validAgingPinState(state) {
			continue
		}
		if _, ok := mem.ComponentMemory[c.TargetID]; ok {
			pins[c.TargetID] = state
		}
	}
	if len(pins) > 0 && verbose {
		names := make([]string, 0, len(pins))
		for name := range pins {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			tuiPrintf("Aging: component %q pinned to %s by a STATE correction (aging will not transition it)\n",
				name, pins[name])
		}
	}
	return pins
}

// validAgingPinState reports whether a corrected value is one of the six
// supported knowledge states. Defensive: corrections are validated on
// append, but an old or hand-edited log must not crash the pipeline.
func validAgingPinState(state developer_memory.KnowledgeState) bool {
	switch state {
	case developer_memory.StateActive,
		developer_memory.StateDeprecated,
		developer_memory.StateRemoved,
		developer_memory.StateHistorical,
		developer_memory.StateExperimental,
		developer_memory.StateUnknown:
		return true
	}
	return false
}

// averageFreshness computes the mean freshness score over every claim in
// the aggregate (global + component-scoped) at the given time.
func averageFreshness(mem *developer_memory.DeveloperMemory, now time.Time) (avg float64, total int) {
	if mem == nil {
		return 0, 0
	}
	sum := 0.0
	for _, c := range mem.GlobalMemory {
		sum += knowledge_aging.FreshnessScore(c, now)
		total++
	}
	for _, h := range mem.ComponentMemory {
		for _, c := range h.Claims {
			sum += knowledge_aging.FreshnessScore(c, now)
			total++
		}
	}
	if total == 0 {
		return 0, 0
	}
	return sum / float64(total), total
}

// loadAgingConfig reads the "aging:" section of .glassmarble/config.yaml,
// falling back to the built-in defaults when the file or section is absent.
// storageDir is the .glassmarble directory.
func loadAgingConfig(storageDir string) (*config.AgingConfig, error) {
	cfg := config.DefaultAgingConfig()
	data, err := os.ReadFile(filepath.Join(storageDir, "config.yaml"))
	if err != nil {
		return cfg, nil
	}
	var local config.Config
	if err := yaml.Unmarshal(data, &local); err != nil {
		return cfg, nil
	}
	if local.Aging != nil {
		cfg = local.Aging
	}
	cfg.ApplyDefaults()
	return cfg, nil
}
