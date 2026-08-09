package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultLearningConfig(t *testing.T) {
	cfg := DefaultLearningConfig()
	assert.True(t, cfg.CorrectionsApplyOnQuery())
	assert.True(t, cfg.LearnConventionsEnabled())
	assert.Equal(t, 2, cfg.ConventionEvidenceThreshold())
}

func TestLearningConfigApplyDefaults(t *testing.T) {
	cfg := &LearningConfig{}
	cfg.ApplyDefaults()
	assert.True(t, cfg.CorrectionsApplyOnQuery())
	assert.True(t, cfg.LearnConventionsEnabled())
	assert.Equal(t, 2, cfg.ConventionEvidenceThreshold())
}

func TestLearningConfigExplicitFalsePreserved(t *testing.T) {
	// Tri-state pointers: an explicit apply_on_query: false must survive
	// ApplyDefaults — the user deliberately turned the overlay off.
	f := false
	cfg := &LearningConfig{ApplyOnQuery: &f}
	cfg.ApplyDefaults()
	assert.False(t, cfg.CorrectionsApplyOnQuery())
	assert.True(t, cfg.LearnConventionsEnabled())
}

func TestLearningConfigYAML(t *testing.T) {
	clearEnv(t)
	isolateHome(t)
	t.Chdir(t.TempDir())
	require.NoError(t, os.MkdirAll(".glassmarble", 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(".glassmarble", "config.yaml"),
		[]byte("learning:\n  apply_on_query: false\n  conventions_enabled: true\n  min_convention_evidence: 5\n"), 0o644))

	cfg, err := Load(Config{})
	require.NoError(t, err)
	require.NotNil(t, cfg.Learning)
	cfg.Learning.ApplyDefaults()

	assert.False(t, cfg.Learning.CorrectionsApplyOnQuery())
	assert.True(t, cfg.Learning.LearnConventionsEnabled())
	assert.Equal(t, 5, cfg.Learning.ConventionEvidenceThreshold())
}

func TestNilLearningConfigHelpers(t *testing.T) {
	// The tri-state accessors must be nil-safe: consumers that skip config
	// entirely get the defaults.
	var cfg *LearningConfig
	assert.True(t, cfg.CorrectionsApplyOnQuery())
	assert.True(t, cfg.LearnConventionsEnabled())
	assert.Equal(t, 2, cfg.ConventionEvidenceThreshold())
}
