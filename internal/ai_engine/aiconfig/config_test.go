package aiconfig

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestDefaults(t *testing.T) {
	cfg := Default()
	if cfg.Provider != "openai" {
		t.Errorf("Default provider = %q, want openai", cfg.Provider)
	}
	if cfg.Model != "gpt-4o" {
		t.Errorf("Default model = %q, want gpt-4o", cfg.Model)
	}
	if cfg.Temperature == nil || *cfg.Temperature != 0.2 {
		t.Errorf("Default temperature = %v, want 0.2", cfg.Temperature)
	}
	if cfg.MaxTurns != 15 {
		t.Errorf("Default max_turns = %d, want 15", cfg.MaxTurns)
	}
	if cfg.MaxToolResultBytes != 8192 {
		t.Errorf("Default max_tool_result_bytes = %d, want 8192", cfg.MaxToolResultBytes)
	}
	if cfg.MaxOutputTokens != 8192 {
		t.Errorf("Default max_output_tokens = %d, want 8192", cfg.MaxOutputTokens)
	}
	if cfg.TimeoutSec != 180 {
		t.Errorf("Default timeout_sec = %d, want 180", cfg.TimeoutSec)
	}
	if !cfg.Stream {
		t.Error("Default stream = false, want true")
	}
	if cfg.MaxSessionMessages != 40 {
		t.Errorf("Default max_session_messages = %d, want 40", cfg.MaxSessionMessages)
	}
	if cfg.MaxTotalTokens != 0 || cfg.MaxCostUSD != 0 {
		t.Errorf("Default budgets = %d/%v, want 0/0", cfg.MaxTotalTokens, cfg.MaxCostUSD)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ai.yaml")
	tmp := 0.5
	cfg := &Config{
		Provider:           "gemini",
		Model:              "gemini-2.5-flash",
		APIKey:             "secret-key-123",
		BaseURL:            "https://example.com/v1beta",
		Temperature:        &tmp,
		MaxTurns:           7,
		MaxToolResultBytes: 4096,
		MaxOutputTokens:    2048,
		TimeoutSec:         60,
	}
	if err := Save(path, cfg); err != nil {
		t.Fatalf("Save() failed: %v", err)
	}

	loaded, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile() failed: %v", err)
	}
	if loaded.Provider != cfg.Provider || loaded.Model != cfg.Model || loaded.APIKey != cfg.APIKey {
		t.Errorf("round trip mismatch: got %+v", loaded)
	}
	if loaded.BaseURL != cfg.BaseURL || (loaded.Temperature == nil) != (cfg.Temperature == nil) || (loaded.Temperature != nil && *loaded.Temperature != *cfg.Temperature) {
		t.Errorf("round trip mismatch (base/temp): got %+v want %+v", loaded, cfg)
	}
	if loaded.MaxTurns != cfg.MaxTurns || loaded.MaxToolResultBytes != cfg.MaxToolResultBytes {
		t.Errorf("round trip mismatch (limits): got %+v", loaded)
	}
	if loaded.MaxOutputTokens != cfg.MaxOutputTokens || loaded.TimeoutSec != cfg.TimeoutSec {
		t.Errorf("round trip mismatch (tokens/timeout): got %+v", loaded)
	}

	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("saved file missing: %v", err)
	}
	if runtime.GOOS != "windows" && fi.Mode().Perm()&0o077 != 0 {
		t.Errorf("saved file permissions %o — want no group/other access", fi.Mode().Perm())
	}
}

func TestLoadEnvPrecedence(t *testing.T) {
	t.Setenv("GLASSMARBLE_AI_PROVIDER", "gemini")
	t.Setenv("GLASSMARBLE_AI_MODEL", "gemini-2.5-flash")
	t.Setenv("GLASSMARBLE_AI_API_KEY", "env-key")
	t.Setenv("GLASSMARBLE_AI_BASE_URL", "https://env.example.com")
	t.Setenv("GLASSMARBLE_AI_TEMPERATURE", "0.9")
	t.Setenv("GLASSMARBLE_AI_MAX_TURNS", "3")
	t.Setenv("GLASSMARBLE_AI_MAX_OUTPUT_TOKENS", "1024")
	t.Setenv("GLASSMARBLE_AI_TIMEOUT_SEC", "42")
	t.Setenv("GLASSMARBLE_AI_STREAM", "0")
	t.Setenv("GLASSMARBLE_AI_MAX_TOTAL_TOKENS", "999")
	t.Setenv("GLASSMARBLE_AI_MAX_COST", "0.25")
	t.Setenv("GLASSMARBLE_AI_MAX_SESSION_MESSAGES", "12")

	cfg, err := Load(Config{})
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}
	if cfg.Provider != "gemini" || cfg.Model != "gemini-2.5-flash" {
		t.Errorf("env not applied: %+v", cfg)
	}
	if cfg.APIKey != "env-key" || cfg.BaseURL != "https://env.example.com" {
		t.Errorf("env not applied (key/base): %+v", cfg)
	}
	if cfg.Temperature == nil || *cfg.Temperature != 0.9 || cfg.MaxTurns != 3 {
		t.Errorf("env not applied (temp/turns): %+v", cfg)
	}
	if cfg.MaxOutputTokens != 1024 || cfg.TimeoutSec != 42 {
		t.Errorf("env not applied (tokens/timeout): %+v", cfg)
	}
	if cfg.Stream {
		t.Errorf("GLASSMARBLE_AI_STREAM=0 must disable streaming: %+v", cfg)
	}
	if cfg.MaxTotalTokens != 999 || cfg.MaxCostUSD != 0.25 {
		t.Errorf("env not applied (budgets): %+v", cfg)
	}
	if cfg.MaxSessionMessages != 12 {
		t.Errorf("env not applied (session messages): %+v", cfg)
	}
}

func TestLoadStreamEnvTrue(t *testing.T) {
	t.Setenv("GLASSMARBLE_AI_STREAM", "true")
	cfg, err := Load(Config{})
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}
	if !cfg.Stream {
		t.Errorf("GLASSMARBLE_AI_STREAM=true must enable streaming: %+v", cfg)
	}
}

// TestLoadYAMLProjectOverridesGlobal exercises the full precedence chain with
// isolated home and project directories: global yaml < project yaml < env,
// with defaults preserved underneath.
func TestLoadYAMLProjectOverridesGlobal(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	project := filepath.Join(root, "repo")
	if err := os.MkdirAll(filepath.Join(home, ".glassmarble"), 0o755); err != nil {
		t.Fatalf("mkdir home: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(project, ".glassmarble"), 0o755); err != nil {
		t.Fatalf("mkdir project: %v", err)
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Chdir(project)

	// Config files are written from the default state, so stream: true is
	// present (a bare stream: false would mean the user explicitly disabled it).
	globalCfg := Default()
	globalCfg.Provider, globalCfg.Model, globalCfg.MaxTurns = "anthropic", "claude-sonnet-4-5", 3
	projectCfg := Default()
	projectCfg.Provider, projectCfg.Model, projectCfg.MaxTurns = "gemini", "gemini-2.5-flash", 9
	if err := Save(filepath.Join(home, ".glassmarble", GlobalConfigFile), globalCfg); err != nil {
		t.Fatalf("save global: %v", err)
	}
	if err := Save(filepath.Join(project, ProjectConfigPath), projectCfg); err != nil {
		t.Fatalf("save project: %v", err)
	}

	cfg, err := Load(Config{})
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}
	// Project wins over global.
	if cfg.Provider != "gemini" || cfg.Model != "gemini-2.5-flash" {
		t.Errorf("project config not applied: %+v", cfg)
	}
	if cfg.MaxTurns != 9 {
		t.Errorf("project max_turns = %d, want 9", cfg.MaxTurns)
	}
	// Defaults preserved under both files.
	if cfg.Temperature == nil || *cfg.Temperature != 0.2 || cfg.Stream != true || cfg.MaxSessionMessages != 40 {
		t.Errorf("defaults not preserved: %+v", cfg)
	}

	// Env beats project.
	t.Setenv("GLASSMARBLE_AI_PROVIDER", "deepseek")
	cfg, err = Load(Config{})
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}
	if cfg.Provider != "deepseek" {
		t.Errorf("env provider = %q, want deepseek", cfg.Provider)
	}
	if cfg.Model != "gemini-2.5-flash" {
		t.Errorf("project model should survive env provider override: %q", cfg.Model)
	}

	// Flags beat env.
	cfg, err = Load(Config{Provider: "openai", Model: "gpt-4o"})
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}
	if cfg.Provider != "openai" || cfg.Model != "gpt-4o" {
		t.Errorf("flag config not applied: %+v", cfg)
	}
}

func TestLoadFlagPrecedenceOverEnv(t *testing.T) {
	t.Setenv("GLASSMARBLE_AI_PROVIDER", "gemini")
	t.Setenv("GLASSMARBLE_AI_MODEL", "gemini-2.5-flash")

	cfg, err := Load(Config{Provider: "anthropic", Model: "claude-sonnet-4-5"})
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}
	if cfg.Provider != "anthropic" {
		t.Errorf("flag provider = %q, want anthropic", cfg.Provider)
	}
	if cfg.Model != "claude-sonnet-4-5" {
		t.Errorf("flag model = %q, want claude-sonnet-4-5", cfg.Model)
	}
}

func TestLoadFileMergeKeepsDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ai.yaml")
	if err := Save(path, &Config{Provider: "deepseek", Model: "deepseek-chat", APIKey: "k"}); err != nil {
		t.Fatalf("Save() failed: %v", err)
	}
	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile() failed: %v", err)
	}
	if cfg.Provider != "deepseek" || cfg.Model != "deepseek-chat" {
		t.Errorf("file values not applied: %+v", cfg)
	}
	// Unset fields fall back to defaults.
	if cfg.MaxTurns != 15 || cfg.TimeoutSec != 180 {
		t.Errorf("defaults not preserved: %+v", cfg)
	}
}

func TestLoadFileMissingIsDefaults(t *testing.T) {
	cfg, err := LoadFile(filepath.Join(t.TempDir(), "does-not-exist.yaml"))
	if err != nil {
		t.Fatalf("LoadFile() on missing file should not error: %v", err)
	}
	if cfg.Provider != "openai" {
		t.Errorf("provider = %q, want default openai", cfg.Provider)
	}
}

func TestEffectiveAPIKey(t *testing.T) {
	// 1. Config value wins.
	cfg := &Config{APIKey: "config-key"}
	if got := EffectiveAPIKey(cfg, "GLASSMARBLE_OPENAI_API_KEY"); got != "config-key" {
		t.Errorf("config key = %q, want config-key", got)
	}

	// 2. Provider env var.
	cfg = &Config{}
	t.Setenv("GLASSMARBLE_OPENAI_API_KEY", "env-key")
	if got := EffectiveAPIKey(cfg, "GLASSMARBLE_OPENAI_API_KEY"); got != "env-key" {
		t.Errorf("env key = %q, want env-key", got)
	}

	// 3. Generic env var fallback.
	t.Setenv("GLASSMARBLE_OPENAI_API_KEY", "")
	t.Setenv("GLASSMARBLE_AI_API_KEY", "generic-key")
	if got := EffectiveAPIKey(cfg, "GLASSMARBLE_OPENAI_API_KEY"); got != "generic-key" {
		t.Errorf("generic key = %q, want generic-key", got)
	}

	// 4. Nothing set.
	t.Setenv("GLASSMARBLE_AI_API_KEY", "")
	if got := EffectiveAPIKey(cfg, "GLASSMARBLE_OPENAI_API_KEY"); got != "" {
		t.Errorf("empty key = %q, want empty", got)
	}
}

func TestEffectiveBaseURL(t *testing.T) {
	cfg := &Config{BaseURL: "https://a.example.com"}
	if got := EffectiveBaseURL(cfg); got != "https://a.example.com" {
		t.Errorf("base url = %q", got)
	}
	t.Setenv("GLASSMARBLE_AI_BASE_URL", "https://b.example.com")
	cfg = &Config{}
	if got := EffectiveBaseURL(cfg); got != "https://b.example.com" {
		t.Errorf("env base url = %q", got)
	}
	t.Setenv("GLASSMARBLE_AI_BASE_URL", "")
	if got := EffectiveBaseURL(cfg); got != "" {
		t.Errorf("empty base url = %q", got)
	}
}
