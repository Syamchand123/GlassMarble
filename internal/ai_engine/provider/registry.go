package provider

import "strings"

// AdapterKind identifies the wire-format adapter a provider uses.
type AdapterKind string

const (
	// AdapterOpenAICompat is the OpenAI chat-completions wire format used by
	// OpenAI, DeepSeek, Mistral, GLM, NVIDIA, OpenRouter, Groq, Ollama, and
	// any custom endpoint.
	AdapterOpenAICompat AdapterKind = "openai_compat"
	// AdapterAnthropic is the native Anthropic Messages API wire format.
	AdapterAnthropic AdapterKind = "anthropic"
	// AdapterGemini is the native Google Gemini generateContent wire format.
	AdapterGemini AdapterKind = "gemini"
)

// Meta describes a provider offered by the BYOK registry.
type Meta struct {
	// Name is the canonical CLI identifier, e.g. "openai", "gemini".
	Name string
	// DisplayName is the human-readable label.
	DisplayName string
	// Description is a one-line summary.
	Description string
	// Adapter selects the wire-format adapter.
	Adapter AdapterKind
	// DefaultBaseURL is the default API endpoint; empty if the user must supply one.
	DefaultBaseURL string
	// KeyEnvVar is the canonical environment variable for this provider's key.
	KeyEnvVar string
	// RequiresKey indicates whether an API key is mandatory.
	RequiresKey bool
	// Models lists well-known model identifiers for this provider.
	Models []string
}

// Registry is the ordered list of supported providers.
var Registry = []Meta{
	{
		Name:           "openai",
		DisplayName:    "OpenAI",
		Description:    "GPT series models from OpenAI",
		Adapter:        AdapterOpenAICompat,
		DefaultBaseURL: "https://api.openai.com/v1",
		KeyEnvVar:      "GLASSMARBLE_OPENAI_API_KEY",
		RequiresKey:    true,
		Models:         []string{"gpt-5", "gpt-5-mini", "gpt-4o", "gpt-4o-mini", "o3", "o3-mini"},
	},
	{
		Name:           "anthropic",
		DisplayName:    "Anthropic (Claude)",
		Description:    "Claude models from Anthropic",
		Adapter:        AdapterAnthropic,
		DefaultBaseURL: "https://api.anthropic.com/v1",
		KeyEnvVar:      "GLASSMARBLE_ANTHROPIC_API_KEY",
		RequiresKey:    true,
		Models:         []string{"claude-opus-4-1", "claude-sonnet-4-5", "claude-haiku-4-5"},
	},
	{
		Name:           "gemini",
		DisplayName:    "Google Gemini",
		Description:    "Gemini models from Google",
		Adapter:        AdapterGemini,
		DefaultBaseURL: "https://generativelanguage.googleapis.com/v1beta",
		KeyEnvVar:      "GLASSMARBLE_GEMINI_API_KEY",
		RequiresKey:    true,
		Models:         []string{"gemini-2.5-pro", "gemini-2.5-flash", "gemini-2.5-flash-lite", "gemini-2.0-flash"},
	},
	{
		Name:           "deepseek",
		DisplayName:    "DeepSeek",
		Description:    "DeepSeek chat and reasoning models",
		Adapter:        AdapterOpenAICompat,
		DefaultBaseURL: "https://api.deepseek.com/v1",
		KeyEnvVar:      "GLASSMARBLE_DEEPSEEK_API_KEY",
		RequiresKey:    true,
		Models:         []string{"deepseek-chat", "deepseek-reasoner"},
	},
	{
		Name:           "mistral",
		DisplayName:    "Mistral AI",
		Description:    "Mistral and Codestral models",
		Adapter:        AdapterOpenAICompat,
		DefaultBaseURL: "https://api.mistral.ai/v1",
		KeyEnvVar:      "GLASSMARBLE_MISTRAL_API_KEY",
		RequiresKey:    true,
		Models:         []string{"mistral-large-latest", "mistral-small-latest", "codestral-latest"},
	},
	{
		Name:           "glm",
		DisplayName:    "Zhipu GLM",
		Description:    "GLM models from Zhipu AI",
		Adapter:        AdapterOpenAICompat,
		DefaultBaseURL: "https://open.bigmodel.cn/api/paas/v4",
		KeyEnvVar:      "GLASSMARBLE_GLM_API_KEY",
		RequiresKey:    true,
		Models:         []string{"glm-4.6", "glm-4.5", "glm-4.5-air"},
	},
	{
		Name:           "nvidia",
		DisplayName:    "NVIDIA NIM",
		Description:    "Accelerated open models via NVIDIA NIM",
		Adapter:        AdapterOpenAICompat,
		DefaultBaseURL: "https://integrate.api.nvidia.com/v1",
		KeyEnvVar:      "GLASSMARBLE_NVIDIA_API_KEY",
		RequiresKey:    true,
		Models:         []string{"deepseek-ai/deepseek-r1", "nvidia/llama-3.1-nemotron-70b-instruct", "meta/llama-3.3-70b-instruct"},
	},
	{
		Name:           "openrouter",
		DisplayName:    "OpenRouter",
		Description:    "Aggregated access to hundreds of models from one API",
		Adapter:        AdapterOpenAICompat,
		DefaultBaseURL: "https://openrouter.ai/api/v1",
		KeyEnvVar:      "GLASSMARBLE_OPENROUTER_API_KEY",
		RequiresKey:    true,
		Models:         []string{"openai/gpt-5", "anthropic/claude-sonnet-4-5", "google/gemini-2.5-pro", "deepseek/deepseek-chat", "qwen/qwen3-235b-a22b"},
	},
	{
		Name:           "groq",
		DisplayName:    "Groq",
		Description:    "Fast inference for open models",
		Adapter:        AdapterOpenAICompat,
		DefaultBaseURL: "https://api.groq.com/openai/v1",
		KeyEnvVar:      "GLASSMARBLE_GROQ_API_KEY",
		RequiresKey:    true,
		Models:         []string{"llama-3.3-70b-versatile", "llama-3.1-8b-instant"},
	},
	{
		Name:           "ollama",
		DisplayName:    "Ollama (local)",
		Description:    "Local models served by Ollama; no API key required",
		Adapter:        AdapterOpenAICompat,
		DefaultBaseURL: "http://localhost:11434/v1",
		KeyEnvVar:      "GLASSMARBLE_OLLAMA_BASE_URL",
		RequiresKey:    false,
		Models:         []string{"llama3.3", "qwen3", "deepseek-r1", "mistral"},
	},
	{
		Name:           "custom",
		DisplayName:    "Custom (OpenAI-compatible)",
		Description:    "Any OpenAI-compatible endpoint via a custom base URL",
		Adapter:        AdapterOpenAICompat,
		DefaultBaseURL: "",
		KeyEnvVar:      "GLASSMARBLE_AI_API_KEY",
		RequiresKey:    false,
		Models:         nil,
	},
}

// Get returns the provider metadata for the given name.
func Get(name string) (Meta, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	for _, m := range Registry {
		if m.Name == name {
			return m, true
		}
	}
	return Meta{}, false
}

// Names returns all registered provider names in registry order.
func Names() []string {
	names := make([]string, 0, len(Registry))
	for _, m := range Registry {
		names = append(names, m.Name)
	}
	return names
}
