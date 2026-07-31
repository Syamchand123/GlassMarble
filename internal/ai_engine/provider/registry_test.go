package provider

import (
	"strings"
	"testing"
)

func TestRegistryRequiredProviders(t *testing.T) {
	required := []string{
		"openai", "anthropic", "gemini", "deepseek", "mistral", "glm",
		"nvidia", "openrouter", "groq", "ollama", "custom",
	}
	byName := make(map[string]Meta)
	for _, m := range Registry {
		byName[m.Name] = m
	}
	for _, name := range required {
		if _, ok := byName[name]; !ok {
			t.Errorf("provider %q missing from registry", name)
		}
	}
}

func TestRegistrySanity(t *testing.T) {
	seen := make(map[string]bool)
	for _, m := range Registry {
		if m.Name == "" || m.DisplayName == "" || m.Description == "" {
			t.Errorf("provider with empty identity fields: %+v", m)
		}
		if seen[m.Name] {
			t.Errorf("duplicate provider name %q", m.Name)
		}
		seen[m.Name] = true

		switch m.Adapter {
		case AdapterOpenAICompat, AdapterAnthropic, AdapterGemini:
		default:
			t.Errorf("provider %q has invalid adapter %q", m.Name, m.Adapter)
		}

		if m.RequiresKey {
			if m.KeyEnvVar == "" || !strings.HasPrefix(m.KeyEnvVar, "GLASSMARBLE_") || !strings.HasSuffix(m.KeyEnvVar, "_API_KEY") {
				t.Errorf("provider %q: bad key env var %q", m.Name, m.KeyEnvVar)
			}
			if m.DefaultBaseURL == "" {
				t.Errorf("provider %q requires a key but has no default base URL", m.Name)
			}
		}
		if m.Adapter != AdapterOpenAICompat && m.Adapter != AdapterAnthropic && m.Adapter != AdapterGemini {
			t.Errorf("provider %q: unsupported adapter %q", m.Name, m.Adapter)
		}
	}
}

func TestGet(t *testing.T) {
	meta, ok := Get("OPENAI")
	if !ok || meta.Name != "openai" {
		t.Errorf("Get(OPENAI) = %+v, %v", meta, ok)
	}
	meta, ok = Get("  Anthropic ")
	if !ok || meta.Name != "anthropic" {
		t.Errorf("Get(' Anthropic ') = %+v, %v", meta, ok)
	}
	if _, ok := Get("nonexistent"); ok {
		t.Error("Get(nonexistent) should be false")
	}
}

func TestNames(t *testing.T) {
	names := Names()
	if len(names) != len(Registry) {
		t.Errorf("Names() len = %d, registry len = %d", len(names), len(Registry))
	}
	if names[0] != "openai" || names[len(names)-1] != "custom" {
		t.Errorf("Names() = %v", names)
	}
}
