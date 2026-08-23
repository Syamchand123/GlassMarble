// Package aiconfig handles the Bring-Your-Own-Key (BYOK) configuration for the
// GlassMarble AI engine: provider selection, model selection, API keys, and
// runtime limits.
//
// Precedence (highest wins): CLI flags > GLASSMARBLE_AI_* environment
// variables > project .glassmarble/ai.yaml > global ~/.glassmarble/ai.yaml >
// defaults.
package aiconfig

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config holds all AI engine settings.
type Config struct {
	Provider           string   `yaml:"provider"`
	Model              string   `yaml:"model"`
	APIKey             string   `yaml:"api_key"`
	BaseURL            string   `yaml:"base_url,omitempty"`
	Temperature        *float64 `yaml:"temperature,omitempty"`
	MaxTurns           int      `yaml:"max_turns"`
	MaxToolResultBytes int      `yaml:"max_tool_result_bytes"`
	MaxOutputTokens    int      `yaml:"max_output_tokens"`
	TimeoutSec         int      `yaml:"timeout_sec"`
	// Stream enables token-level streaming output when the provider supports it.
	Stream bool `yaml:"stream"`
	// MaxTotalTokens caps the summed prompt+completion tokens per run; 0 = unlimited.
	MaxTotalTokens int `yaml:"max_total_tokens"`
	// MaxCostUSD caps the estimated spend per run (priced models only); 0 = unlimited.
	MaxCostUSD float64 `yaml:"max_cost_usd"`
	// MaxSessionMessages bounds chat history kept in a session; older turns
	// are trimmed to this many messages.
	MaxSessionMessages int `yaml:"max_session_messages"`
}

const (
	// ProjectConfigPath is the repository-local configuration file,
	// relative to the workspace root.
	ProjectConfigPath = ".glassmarble/ai.yaml"
	// GlobalConfigFile is the user-global configuration file name.
	GlobalConfigFile = "ai.yaml"
)

// GlobalPath returns the user-global AI configuration file path
// (~/.glassmarble/ai.yaml), or "" if the home directory is unavailable.
func GlobalPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".glassmarble", GlobalConfigFile)
}

func floatPtr(v float64) *float64 { return &v }

// FloatPtr is exported for tests and external callers.
func FloatPtr(v float64) *float64 { return &v }

// Default returns the built-in defaults for the AI engine configuration.
func Default() *Config {
	return &Config{
		Provider:           "openai",
		Model:              "gpt-4o",
		Temperature:        floatPtr(0.2),
		MaxTurns:           15,
		MaxToolResultBytes: 8192,
		MaxOutputTokens:    8192,
		TimeoutSec:         180,
		Stream:             true,
		MaxSessionMessages: 40,
	}
}

// Load resolves the effective configuration with precedence:
// flags > GLASSMARBLE_AI_* environment variables > project ai.yaml >
// global ai.yaml > defaults.
// Project config is resolved relative to the current working directory.
// For repository-aware resolution use LoadForDir.
func Load(flagConfig Config) (*Config, error) {
	return LoadForDir(".", flagConfig)
}

// LoadForDir resolves the effective configuration with precedence:
// flags > GLASSMARBLE_AI_* environment variables > project ai.yaml >
// global ai.yaml > defaults. ProjectConfigPath is resolved against rootDir
// (the effective repository root, e.g. --root-dir). An empty rootDir is
// treated as ".".
func LoadForDir(rootDir string, flagConfig Config) (*Config, error) {
	cfg := Default()
	mergeYAML(GlobalPath(), cfg)
	effectiveRoot := rootDir
	if effectiveRoot == "" {
		effectiveRoot = "."
	}
	projectPath := filepath.Join(effectiveRoot, ProjectConfigPath)
	mergeYAML(projectPath, cfg)
	applyEnv(cfg)
	applyFlags(flagConfig, cfg)
	return cfg, nil
}

// LoadFile returns the configuration from a single YAML file merged over the
// defaults. Missing or unreadable files are not an error.
func LoadFile(path string) (*Config, error) {
	cfg := Default()
	mergeYAML(path, cfg)
	return cfg, nil
}

// Save writes cfg to path, creating parent directories. The file is written
// with 0600 permissions because it may contain an API key.
func Save(path string, cfg *Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("failed to encode config: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}
	// Ensure permissions 0600 even when the file already existed (WriteFile
	// only applies mode on creation).
	_ = os.Chmod(path, 0o600)
	return nil
}

// EffectiveAPIKey resolves the API key for a provider with precedence:
// explicit config value > provider-specific environment variable
// (e.g. GLASSMARBLE_OPENAI_API_KEY) > generic GLASSMARBLE_AI_API_KEY.
func EffectiveAPIKey(cfg *Config, keyEnvVar string) string {
	if cfg != nil && cfg.APIKey != "" {
		return cfg.APIKey
	}
	if v := os.Getenv(keyEnvVar); v != "" {
		return v
	}
	if v := os.Getenv("GLASSMARBLE_AI_API_KEY"); v != "" {
		return v
	}
	return ""
}

// EffectiveBaseURL resolves the provider endpoint: explicit value > provider
// default (handled by the caller via the registry).
func EffectiveBaseURL(cfg *Config) string {
	if cfg != nil && cfg.BaseURL != "" {
		return cfg.BaseURL
	}
	if v := os.Getenv("GLASSMARBLE_AI_BASE_URL"); v != "" {
		return v
	}
	return ""
}

// EffectiveTemperature resolves the sampling temperature.
// Nil means unset (provider default); a non-nil pointer may be 0.0 for
// explicit deterministic sampling.
func EffectiveTemperature(cfg *Config) *float64 {
	if cfg != nil {
		return cfg.Temperature
	}
	return nil
}

func applyEnv(cfg *Config) {
	if v := os.Getenv("GLASSMARBLE_AI_PROVIDER"); v != "" {
		cfg.Provider = v
	}
	if v := os.Getenv("GLASSMARBLE_AI_MODEL"); v != "" {
		cfg.Model = v
	}
	if v := os.Getenv("GLASSMARBLE_AI_API_KEY"); v != "" {
		cfg.APIKey = v
	}
	if v := os.Getenv("GLASSMARBLE_AI_BASE_URL"); v != "" {
		cfg.BaseURL = v
	}
	if v := os.Getenv("GLASSMARBLE_AI_TEMPERATURE"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			cfg.Temperature = floatPtr(f)
		}
	}
	if v := os.Getenv("GLASSMARBLE_AI_MAX_TURNS"); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			cfg.MaxTurns = i
		}
	}
	if v := os.Getenv("GLASSMARBLE_AI_MAX_TOOL_RESULT_BYTES"); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			cfg.MaxToolResultBytes = i
		}
	}
	if v := os.Getenv("GLASSMARBLE_AI_MAX_OUTPUT_TOKENS"); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			cfg.MaxOutputTokens = i
		}
	}
	if v := os.Getenv("GLASSMARBLE_AI_TIMEOUT_SEC"); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			cfg.TimeoutSec = i
		}
	}
	if v := os.Getenv("GLASSMARBLE_AI_STREAM"); v != "" {
		cfg.Stream = v != "0" && !strings.EqualFold(v, "false")
	}
	if v := os.Getenv("GLASSMARBLE_AI_MAX_TOTAL_TOKENS"); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			cfg.MaxTotalTokens = i
		}
	}
	if v := os.Getenv("GLASSMARBLE_AI_MAX_COST"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			cfg.MaxCostUSD = f
		}
	}
	if v := os.Getenv("GLASSMARBLE_AI_MAX_SESSION_MESSAGES"); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			cfg.MaxSessionMessages = i
		}
	}
}

func applyFlags(flagConfig Config, cfg *Config) {
	if flagConfig.Provider != "" {
		cfg.Provider = flagConfig.Provider
	}
	if flagConfig.Model != "" {
		cfg.Model = flagConfig.Model
	}
	if flagConfig.APIKey != "" {
		cfg.APIKey = flagConfig.APIKey
	}
	if flagConfig.BaseURL != "" {
		cfg.BaseURL = flagConfig.BaseURL
	}
	if flagConfig.Temperature != nil {
		cfg.Temperature = flagConfig.Temperature
	}
	if flagConfig.MaxTurns > 0 {
		cfg.MaxTurns = flagConfig.MaxTurns
	}
	if flagConfig.MaxToolResultBytes > 0 {
		cfg.MaxToolResultBytes = flagConfig.MaxToolResultBytes
	}
	if flagConfig.MaxOutputTokens > 0 {
		cfg.MaxOutputTokens = flagConfig.MaxOutputTokens
	}
	if flagConfig.TimeoutSec > 0 {
		cfg.TimeoutSec = flagConfig.TimeoutSec
	}
	if flagConfig.MaxTotalTokens > 0 {
		cfg.MaxTotalTokens = flagConfig.MaxTotalTokens
	}
	if flagConfig.MaxCostUSD > 0 {
		cfg.MaxCostUSD = flagConfig.MaxCostUSD
	}
	if flagConfig.MaxSessionMessages > 0 {
		cfg.MaxSessionMessages = flagConfig.MaxSessionMessages
	}
}

func mergeYAML(path string, cfg *Config) {
	if path == "" {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}

	var temp Config
	if err := yaml.Unmarshal(data, &temp); err != nil {
		return
	}

	// "stream" and "temperature" need nil-aware handling: stream defaults to
	// true (so false must be distinguished from absent), temperature may be
	// 0.0 explicitly (deterministic) vs absent (provider default).
	var probe struct {
		Stream      *bool    `yaml:"stream"`
		Temperature *float64 `yaml:"temperature"`
	}
	if err := yaml.Unmarshal(data, &probe); err == nil {
		if probe.Stream != nil {
			cfg.Stream = *probe.Stream
		}
		if probe.Temperature != nil {
			cfg.Temperature = probe.Temperature
		}
	}

	if temp.Provider != "" {
		cfg.Provider = temp.Provider
	}
	if temp.Model != "" {
		cfg.Model = temp.Model
	}
	if temp.APIKey != "" {
		cfg.APIKey = temp.APIKey
	}
	if temp.BaseURL != "" {
		cfg.BaseURL = temp.BaseURL
	}
	if temp.MaxTurns > 0 {
		cfg.MaxTurns = temp.MaxTurns
	}
	if temp.MaxToolResultBytes > 0 {
		cfg.MaxToolResultBytes = temp.MaxToolResultBytes
	}
	if temp.MaxOutputTokens > 0 {
		cfg.MaxOutputTokens = temp.MaxOutputTokens
	}
	if temp.TimeoutSec > 0 {
		cfg.TimeoutSec = temp.TimeoutSec
	}
	if temp.MaxTotalTokens > 0 {
		cfg.MaxTotalTokens = temp.MaxTotalTokens
	}
	if temp.MaxCostUSD > 0 {
		cfg.MaxCostUSD = temp.MaxCostUSD
	}
	if temp.MaxSessionMessages > 0 {
		cfg.MaxSessionMessages = temp.MaxSessionMessages
	}
}
