package config

import (
	"os"
	"path/filepath"
	"strconv"

	"gopkg.in/yaml.v3"
)

type Config struct {
	RootDir       string      `yaml:"root_dir"`
	WorkerCount   int         `yaml:"worker_count"`
	MaxFileBytes  int64       `yaml:"max_file_bytes"`
	Debug         bool        `yaml:"debug"`
	StorageDir    string      `yaml:"storage_dir"`
	OutputFormat  string      `yaml:"output_format"` // "mermaid", "plantuml", "dot"
	IncludeHidden bool        `yaml:"include_hidden"`
	Drift         DriftConfig `yaml:"drift"`
	// Intelligence holds the Stage 5 thresholds (pattern/smell detection).
	// nil means defaults (config.DefaultIntelligenceConfig()).
	Intelligence *IntelligenceConfig `yaml:"intelligence"`
	// Fusion holds the Stage 9 multi-source knowledge fusion settings
	// (docs globs, technology lexicon, source adapters). nil means defaults
	// (config.DefaultFusionConfig()). Whether fusion runs at all is decided
	// by the `gmb analyze --include-docs` flag, not by this section.
	Fusion *FusionConfig `yaml:"fusion"`
	// Learning holds the Stage 10 learning-layer settings (correction
	// overlay on queries, deterministic convention extraction). nil means
	// defaults (config.DefaultLearningConfig()).
	Learning *LearningConfig `yaml:"learning"`
}

// DriftConfig declares the architecture invariants checked by `gmb drift`.
// All are optional; when absent the drift command reports only cycles.
type DriftConfig struct {
	// Layers partitions the codebase by path glob so cross-layer edges can be
	// checked against ForbiddenDeps. Each node is assigned to the first layer
	// whose pattern matches its file path.
	Layers []DriftLayer `yaml:"layers"`
	// ForbiddenDeps lists directed source-layer -> target-layer dependencies
	// that are not allowed. Source and Target name a layer from Layers.
	ForbiddenDeps []ForbiddenDepRule `yaml:"forbidden_deps"`
	// CycleBudget is the maximum number of tolerated cycles between layers.
	// A non-positive value means "any cycles fail".
	CycleBudget int `yaml:"cycle_budget"`
}

// DriftLayer is a named path pattern used to bucket nodes for drift checks.
type DriftLayer struct {
	Name  string   `yaml:"name"`
	Paths []string `yaml:"paths"`
}

// ForbiddenDepRule forbids dependencies from Source layer to Target layer.
type ForbiddenDepRule struct {
	Source string `yaml:"source"`
	Target string `yaml:"target"`
	Reason string `yaml:"reason,omitempty"`
}

// Load loads configuration with precedence:
// flags > GLASSMARBLE_* env vars > .glassmarble/config.yaml > ~/.glassmarble/config.yaml > defaults
func Load(flagConfig Config) (*Config, error) {
	// 1. Defaults
	cfg := &Config{
		WorkerCount:  4,
		MaxFileBytes: 10 * 1024 * 1024, // 10MB
		OutputFormat: "mermaid",
		StorageDir:   ".glassmarble",
	}

	// 2. ~/.glassmarble/config.yaml
	homeDir, err := os.UserHomeDir()
	if err == nil {
		globalCfgPath := filepath.Join(homeDir, ".glassmarble", "config.yaml")
		mergeYAML(globalCfgPath, cfg)
	}

	// 3. .glassmarble/config.yaml
	localCfgPath := filepath.Join(".glassmarble", "config.yaml")
	mergeYAML(localCfgPath, cfg)

	// 4. Environment Variables
	if val := os.Getenv("GLASSMARBLE_ROOT_DIR"); val != "" {
		cfg.RootDir = val
	}
	if val := os.Getenv("GLASSMARBLE_WORKER_COUNT"); val != "" {
		if i, err := strconv.Atoi(val); err == nil {
			cfg.WorkerCount = i
		}
	}
	if val := os.Getenv("GLASSMARBLE_MAX_FILE_BYTES"); val != "" {
		if i, err := strconv.ParseInt(val, 10, 64); err == nil {
			cfg.MaxFileBytes = i
		}
	}
	if val := os.Getenv("GLASSMARBLE_DEBUG"); val != "" {
		if b, err := strconv.ParseBool(val); err == nil {
			cfg.Debug = b
		}
	}
	if val := os.Getenv("GLASSMARBLE_STORAGE_DIR"); val != "" {
		cfg.StorageDir = val
	}
	if val := os.Getenv("GLASSMARBLE_OUTPUT_FORMAT"); val != "" {
		cfg.OutputFormat = val
	}
	if val := os.Getenv("GLASSMARBLE_INCLUDE_HIDDEN"); val != "" {
		if b, err := strconv.ParseBool(val); err == nil {
			cfg.IncludeHidden = b
		}
	}

	// 5. Flags (overwrite if set)
	if flagConfig.RootDir != "" {
		cfg.RootDir = flagConfig.RootDir
	}
	if flagConfig.WorkerCount != 0 {
		cfg.WorkerCount = flagConfig.WorkerCount
	}
	if flagConfig.MaxFileBytes != 0 {
		cfg.MaxFileBytes = flagConfig.MaxFileBytes
	}
	// Booleans from flags are tricky if false is the intended override,
	// but for simplicity we merge if true
	if flagConfig.Debug {
		cfg.Debug = true
	}
	if flagConfig.StorageDir != "" {
		cfg.StorageDir = flagConfig.StorageDir
	}
	if flagConfig.OutputFormat != "" {
		cfg.OutputFormat = flagConfig.OutputFormat
	}
	if flagConfig.IncludeHidden {
		cfg.IncludeHidden = true
	}

	if cfg.RootDir == "" {
		cfg.RootDir = "."
	}

	return cfg, nil
}

func mergeYAML(path string, cfg *Config) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}

	// Decode into a temporary config to only overwrite set values
	var temp Config
	if err := yaml.Unmarshal(data, &temp); err != nil {
		return
	}

	if temp.RootDir != "" {
		cfg.RootDir = temp.RootDir
	}
	if temp.WorkerCount != 0 {
		cfg.WorkerCount = temp.WorkerCount
	}
	if temp.MaxFileBytes != 0 {
		cfg.MaxFileBytes = temp.MaxFileBytes
	}
	if temp.Debug {
		cfg.Debug = temp.Debug
	}
	if temp.StorageDir != "" {
		cfg.StorageDir = temp.StorageDir
	}
	if temp.OutputFormat != "" {
		cfg.OutputFormat = temp.OutputFormat
	}
	if temp.IncludeHidden {
		cfg.IncludeHidden = temp.IncludeHidden
	}
	if temp.Drift.Layers != nil || temp.Drift.ForbiddenDeps != nil {
		cfg.Drift = temp.Drift
	}
	if temp.Intelligence != nil {
		cfg.Intelligence = temp.Intelligence
	}
	if temp.Fusion != nil {
		cfg.Fusion = temp.Fusion
	}
	if temp.Learning != nil {
		cfg.Learning = temp.Learning
	}
}
