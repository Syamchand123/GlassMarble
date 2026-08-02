package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var glassmarbleEnvVars = []string{
	"GLASSMARBLE_ROOT_DIR",
	"GLASSMARBLE_WORKER_COUNT",
	"GLASSMARBLE_MAX_FILE_BYTES",
	"GLASSMARBLE_DEBUG",
	"GLASSMARBLE_STORAGE_DIR",
	"GLASSMARBLE_OUTPUT_FORMAT",
	"GLASSMARBLE_INCLUDE_HIDDEN",
}

// clearEnv empties every GLASSMARBLE_* env var so an ambient value on the
// developer machine cannot leak into a test. t.Setenv auto-restores.
func clearEnv(t *testing.T) {
	t.Helper()
	for _, k := range glassmarbleEnvVars {
		t.Setenv(k, "")
	}
}

// isolateHome redirects os.UserHomeDir to an empty temp dir. os.UserHomeDir
// reads %USERPROFILE% on Windows and $HOME on unix, so both are redirected.
func isolateHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	return home
}

func writeLocalConfig(t *testing.T, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(".glassmarble", 0o755))
	require.NoError(t, os.WriteFile(".glassmarble/config.yaml", []byte(content), 0o644))
}

func TestLoadDefaults(t *testing.T) {
	clearEnv(t)
	isolateHome(t)

	cfg, err := Load(Config{})
	require.NoError(t, err)
	assert.Equal(t, 4, cfg.WorkerCount)
	assert.Equal(t, int64(10*1024*1024), cfg.MaxFileBytes)
	assert.Equal(t, "mermaid", cfg.OutputFormat)
	assert.Equal(t, ".glassmarble", cfg.StorageDir)
	assert.Equal(t, ".", cfg.RootDir)
	assert.False(t, cfg.Debug)
	assert.False(t, cfg.IncludeHidden)
}

func TestLoadLocalYAMLOverridesDefaults(t *testing.T) {
	clearEnv(t)
	isolateHome(t)
	t.Chdir(t.TempDir())
	writeLocalConfig(t, "root_dir: yamlroot\nworker_count: 8\nmax_file_bytes: 5000\nstorage_dir: yamlstorage\noutput_format: dot\ninclude_hidden: true\ndebug: true\n")

	cfg, err := Load(Config{})
	require.NoError(t, err)
	assert.Equal(t, "yamlroot", cfg.RootDir)
	assert.Equal(t, 8, cfg.WorkerCount)
	assert.Equal(t, int64(5000), cfg.MaxFileBytes)
	assert.Equal(t, "yamlstorage", cfg.StorageDir)
	assert.Equal(t, "dot", cfg.OutputFormat)
	assert.True(t, cfg.IncludeHidden)
	assert.True(t, cfg.Debug)

	// A second Load with a non-empty flagConfig overrides the yaml values.
	cfg2, err := Load(Config{RootDir: "flagroot", WorkerCount: 16, OutputFormat: "plantuml"})
	require.NoError(t, err)
	assert.Equal(t, "flagroot", cfg2.RootDir)
	assert.Equal(t, 16, cfg2.WorkerCount)
	assert.Equal(t, "plantuml", cfg2.OutputFormat)
	// Unset flags still fall back to the yaml-loaded value.
	assert.Equal(t, "yamlstorage", cfg2.StorageDir)
}

func TestLoadEnvOverrides(t *testing.T) {
	clearEnv(t)
	isolateHome(t)

	t.Setenv("GLASSMARBLE_WORKER_COUNT", "6")
	t.Setenv("GLASSMARBLE_MAX_FILE_BYTES", "12345")
	t.Setenv("GLASSMARBLE_DEBUG", "true")
	t.Setenv("GLASSMARBLE_ROOT_DIR", "envroot")
	t.Setenv("GLASSMARBLE_STORAGE_DIR", "envstorage")
	t.Setenv("GLASSMARBLE_OUTPUT_FORMAT", "plantuml")
	t.Setenv("GLASSMARBLE_INCLUDE_HIDDEN", "true")

	cfg, err := Load(Config{})
	require.NoError(t, err)
	assert.Equal(t, 6, cfg.WorkerCount)
	assert.Equal(t, int64(12345), cfg.MaxFileBytes)
	assert.True(t, cfg.Debug)
	assert.Equal(t, "envroot", cfg.RootDir)
	assert.Equal(t, "envstorage", cfg.StorageDir)
	assert.Equal(t, "plantuml", cfg.OutputFormat)
	assert.True(t, cfg.IncludeHidden)
}

func TestEnvParseFailureIgnored(t *testing.T) {
	clearEnv(t)
	isolateHome(t)

	t.Setenv("GLASSMARBLE_WORKER_COUNT", "abc")
	t.Setenv("GLASSMARBLE_MAX_FILE_BYTES", "not-a-number")
	t.Setenv("GLASSMARBLE_DEBUG", "notabool")

	cfg, err := Load(Config{})
	require.NoError(t, err)
	assert.Equal(t, 4, cfg.WorkerCount)
	assert.Equal(t, int64(10*1024*1024), cfg.MaxFileBytes)
	assert.False(t, cfg.Debug)
}

func TestFlagsOverrideEnv(t *testing.T) {
	clearEnv(t)
	isolateHome(t)

	t.Setenv("GLASSMARBLE_ROOT_DIR", "envdir")

	cfg, err := Load(Config{RootDir: "flagdir"})
	require.NoError(t, err)
	assert.Equal(t, "flagdir", cfg.RootDir)
}

func TestBoolFlagCannotDisableYAMLTrue(t *testing.T) {
	clearEnv(t)
	isolateHome(t)
	t.Chdir(t.TempDir())
	writeLocalConfig(t, "debug: true\n")

	// Flags only merge booleans when true, so Debug:false cannot override
	// a true value already loaded from yaml.
	cfg, err := Load(Config{Debug: false})
	require.NoError(t, err)
	assert.True(t, cfg.Debug)
}

func TestLoadGlobalConfigApplied(t *testing.T) {
	clearEnv(t)
	home := isolateHome(t)

	require.NoError(t, os.MkdirAll(filepath.Join(home, ".glassmarble"), 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(home, ".glassmarble", "config.yaml"),
		[]byte("output_format: plantuml\nworker_count: 9\n"),
		0o644,
	))

	cfg, err := Load(Config{})
	require.NoError(t, err)
	assert.Equal(t, "plantuml", cfg.OutputFormat)
	assert.Equal(t, 9, cfg.WorkerCount)
}

func TestRootDirGuardFallsBackToDot(t *testing.T) {
	clearEnv(t)
	isolateHome(t)

	cfg, err := Load(Config{})
	require.NoError(t, err)
	assert.Equal(t, ".", cfg.RootDir)
}

func TestMergeYAMLSilentlyIgnoresCorruptFile(t *testing.T) {
	clearEnv(t)
	isolateHome(t)
	t.Chdir(t.TempDir())
	// If this corrupt yaml were (incorrectly) accepted, worker_count would
	// become 8; mergeYAML swallows the decode error and keeps the default.
	writeLocalConfig(t, "worker_count: 8\nbad: [unclosed")

	cfg, err := Load(Config{})
	require.NoError(t, err)
	assert.Equal(t, 4, cfg.WorkerCount)
	assert.Equal(t, ".", cfg.RootDir)
}

func TestMergeYAMLMissingFileIgnored(t *testing.T) {
	clearEnv(t)
	isolateHome(t)
	t.Chdir(t.TempDir())

	cfg, err := Load(Config{})
	require.NoError(t, err)
	assert.Equal(t, 4, cfg.WorkerCount)
	assert.Equal(t, ".", cfg.RootDir)
}

func TestYAMLZeroValueDoesNotOverride(t *testing.T) {
	clearEnv(t)
	isolateHome(t)
	t.Chdir(t.TempDir())
	writeLocalConfig(t, "worker_count: 0\nmax_file_bytes: 0\nroot_dir: ''\n")

	cfg, err := Load(Config{})
	require.NoError(t, err)
	assert.Equal(t, 4, cfg.WorkerCount)
	assert.Equal(t, int64(10*1024*1024), cfg.MaxFileBytes)
	assert.Equal(t, ".", cfg.RootDir)
}

// TestDriftConfigFromYAML verifies the drift block parses and merges from the
// project-local config file.
func TestDriftConfigFromYAML(t *testing.T) {
	clearEnv(t)
	isolateHome(t)
	t.Chdir(t.TempDir())
	writeLocalConfig(t, `
drift:
  layers:
    - name: web
      paths: ["cmd/web/**"]
    - name: db
      paths: ["internal/db/**"]
  forbidden_deps:
    - source: web
      target: db
    - source: web
      target: cli
      reason: keep web off the CLI
  cycle_budget: 5
`)

	cfg, err := Load(Config{})
	require.NoError(t, err)
	require.Equal(t, 2, len(cfg.Drift.Layers))
	assert.Equal(t, "web", cfg.Drift.Layers[0].Name)
	assert.Equal(t, []string{"cmd/web/**"}, cfg.Drift.Layers[0].Paths)
	assert.Equal(t, 2, len(cfg.Drift.ForbiddenDeps))
	assert.Equal(t, "db", cfg.Drift.ForbiddenDeps[0].Target)
	assert.Equal(t, "keep web off the CLI", cfg.Drift.ForbiddenDeps[1].Reason)
	assert.Equal(t, 5, cfg.Drift.CycleBudget)
}

// TestDriftConfigAbsentDefaults verifies absent drift blocks leave the config
// untouched (no panic, zero-value defaults).
func TestDriftConfigAbsentDefaults(t *testing.T) {
	clearEnv(t)
	isolateHome(t)
	t.Chdir(t.TempDir())
	writeLocalConfig(t, "worker_count: 2\n")

	cfg, err := Load(Config{})
	require.NoError(t, err)
	assert.Equal(t, 0, len(cfg.Drift.Layers))
	assert.Equal(t, 0, len(cfg.Drift.ForbiddenDeps))
	assert.Equal(t, 0, cfg.Drift.CycleBudget)
}
