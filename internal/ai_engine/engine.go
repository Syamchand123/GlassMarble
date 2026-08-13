// Package ai_engine is the GlassMarble AI engine: a Bring-Your-Own-Key (BYOK)
// interface to LLM providers. The engine wires provider configuration to the
// correct adapter and exposes a conversational facade plus the agentic
// tool-calling loop (AskAgent).
package ai_engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/ai_engine/agent"
	"github.com/Syamchand123/GlassMarble/internal/ai_engine/aiconfig"
	"github.com/Syamchand123/GlassMarble/internal/ai_engine/akgbridge"
	"github.com/Syamchand123/GlassMarble/internal/ai_engine/provider"
	"github.com/Syamchand123/GlassMarble/internal/ai_engine/tools"
)

// SystemPrompt is the GlassMarble AI Architect persona. Tool-grounded
// instructions are appended by the agent loop in later phases.
const SystemPrompt = `You are GlassMarble AI Architect, an intelligent assistant with access to:
1. A real-time Architecture Knowledge Graph (AKG) of the repository
2. Architecture memory: historical facts about how this system evolved
3. Detected patterns: Clean Architecture, microservices, CQRS, etc.
4. An architecture timeline: a chronological record of architectural changes

Working principles:
- Every answer must be grounded in the evidence provided to you.
- If you cannot find evidence in the tools, say "I don't have evidence for that."
- Always cite specific commits, PR numbers, or component names when they are available.
- Use query_architecture_memory before answering "why" questions.
- Use get_architecture_timeline before answering "how did X evolve" questions.
- Use get_architecture_patterns before answering "what patterns does this project use" questions.
- Never invent architectural history. If you don't know, say so.`

// Engine is the public facade of the AI engine.
type Engine struct {
	Config   *aiconfig.Config
	Provider provider.Provider
	RootDir  string
}

// New validates the configuration and wires it to the matching provider
// adapter. rootDir locates the repository's .glassmarble directory.
func New(cfg *aiconfig.Config, rootDir string) (*Engine, error) {
	if cfg == nil {
		return nil, fmt.Errorf("nil AI configuration")
	}

	meta, ok := provider.Get(cfg.Provider)
	if !ok {
		return nil, fmt.Errorf("unknown AI provider %q — run `gmb ai models` for the supported list", cfg.Provider)
	}
	if cfg.Model == "" {
		return nil, fmt.Errorf("no model configured for provider %q — run `gmb ai configure`", meta.Name)
	}

	apiKey := aiconfig.EffectiveAPIKey(cfg, meta.KeyEnvVar)
	if meta.RequiresKey && apiKey == "" {
		return nil, fmt.Errorf("no API key for provider %q — set it via `gmb ai configure`, %s, or GLASSMARBLE_AI_API_KEY", meta.Name, meta.KeyEnvVar)
	}

	baseURL := aiconfig.EffectiveBaseURL(cfg)
	if baseURL == "" {
		baseURL = meta.DefaultBaseURL
	}

	timeout := time.Duration(cfg.TimeoutSec) * time.Second
	var p provider.Provider
	switch meta.Adapter {
	case provider.AdapterOpenAICompat:
		p = provider.NewOpenAICompatProvider(apiKey, baseURL, timeout)
	case provider.AdapterAnthropic:
		p = provider.NewAnthropicProvider(apiKey, baseURL, timeout)
	case provider.AdapterGemini:
		p = provider.NewGeminiProvider(apiKey, baseURL, timeout)
	default:
		return nil, fmt.Errorf("unsupported adapter %q for provider %q", meta.Adapter, meta.Name)
	}

	return &Engine{Config: cfg, Provider: p, RootDir: rootDir}, nil
}

// Ask runs a single chat completion against the configured provider with the
// AI Architect system prompt. history carries prior turns of a conversation.
func (e *Engine) Ask(ctx context.Context, query string, history []provider.Message) (*provider.Response, error) {
	if query == "" {
		return nil, fmt.Errorf("empty question")
	}
	if history == nil {
		history = []provider.Message{}
	}
	msgs := append(history, provider.Message{Role: provider.RoleUser, Content: query})

	return e.Provider.Complete(ctx, provider.Request{
		Model:           e.Config.Model,
		System:          SystemPrompt,
		Messages:        msgs,
		Temperature:     e.Config.Temperature,
		MaxOutputTokens: e.Config.MaxOutputTokens,
	})
}

// AgentOptions controls an agentic AskAgent call.
type AgentOptions struct {
	// EnableTools turns on tool calling. When false, AskAgent is a plain
	// single completion (opinion questions, --no-tools).
	EnableTools bool
	// Tools restricts the tool set to the given categories (system, akg,
	// code, diagram) or exact tool names. Empty means all tools.
	Tools []string
	// MaxTurns caps the tool-call rounds; 0 means the configured value.
	MaxTurns int
	// History carries prior conversation turns.
	History []provider.Message
	// OnEvent reports loop progress (tool calls, results) to the caller.
	OnEvent func(agent.Event)
	// OnStream receives streamed answer deltas when the provider streams
	// (config.Stream must be enabled; pass nil to force buffered mode).
	OnStream func(string)
	// MaxTotalTokens caps summed tokens per run; 0 means the configured value.
	MaxTotalTokens int
	// MaxCostUSD caps the estimated run spend; 0 means the configured value.
	MaxCostUSD float64
}

// AskAgent runs the agentic loop: the model may call repository tools (AKG
// queries, code reading, system status) to ground its answer. The system
// prompt is augmented with a live repository context header.
func (e *Engine) AskAgent(ctx context.Context, query string, opts AgentOptions) (*agent.Result, error) {
	if query == "" {
		return nil, fmt.Errorf("empty question")
	}

	var selected []tools.Tool
	if opts.EnableTools {
		all := tools.All()
		if len(opts.Tools) > 0 {
			s, err := tools.Select(all, opts.Tools)
			if err != nil {
				return nil, err
			}
			selected = s
		} else {
			selected = all
		}
	}

	bridge := akgbridge.New(e.RootDir)
	rc := agent.RepoContext{Status: bridge.Status()}
	if commit, err := akgbridge.GitCommit(ctx, e.RootDir); err == nil {
		rc.GitCommit = commit
	}
	// Load the graph into the cache eagerly only when tools are enabled, so
	// opinion-only runs stay cheap.
	if len(selected) > 0 {
		if snap, err := bridge.Snapshot(); err == nil {
			rc.HasGraph = true
			rc.Nodes = snap.Nodes.Len()
			rc.Edges = akgbridge.EdgeCount(snap)
			rc.Files = snap.FileNodeIndex.Len()
			rc.Entrypoints = len(snap.Entrypoints)
			if snap.Summary != nil {
				rc.Patterns = len(snap.Summary.PrimaryPatterns)
			}
		}
	}

	maxTurns := e.Config.MaxTurns
	if opts.MaxTurns > 0 {
		maxTurns = opts.MaxTurns
	}
	if maxTurns <= 0 {
		maxTurns = 1
	}

	maxTotalTokens := e.Config.MaxTotalTokens
	if opts.MaxTotalTokens > 0 {
		maxTotalTokens = opts.MaxTotalTokens
	}
	maxCostUSD := e.Config.MaxCostUSD
	if opts.MaxCostUSD > 0 {
		maxCostUSD = opts.MaxCostUSD
	}

	var onStream func(string)
	if e.Config.Stream {
		onStream = opts.OnStream
	}

	return (&agent.Agent{
		Provider:        e.Provider,
		Model:           e.Config.Model,
		System:          agent.BuildSystemPrompt(SystemPrompt, rc, len(selected) > 0),
		Tools:           selected,
		Env:             &tools.Env{RootDir: e.RootDir, Bridge: bridge, ArtifactDir: filepath.Join(e.RootDir, ".glassmarble", "ai")},
		MaxTurns:        maxTurns,
		MaxResultBytes:  e.Config.MaxToolResultBytes,
		Temperature:     e.Config.Temperature,
		MaxOutputTokens: e.Config.MaxOutputTokens,
		OnEvent:         opts.OnEvent,
		OnStream:        onStream,
		MaxTotalTokens:  maxTotalTokens,
		MaxCostUSD:      maxCostUSD,
	}).Run(ctx, query, opts.History)
}

// DoctorReport summarizes the health of the AI engine setup.
type DoctorReport struct {
	Provider     string
	DisplayName  string
	Adapter      string
	BaseURL      string
	Model        string
	KeyRequired  bool
	KeySet       bool
	KeySource    string
	// ConfigValid distinguishes configuration problems (unknown provider,
	// missing key/model, missing base URL) from runtime failures such as a
	// connectivity ping timeout. Connectivity failures append to Problems
	// but must not mark an otherwise-valid configuration as invalid.
	ConfigValid  bool
	Problems     []string
	PingStatus   string
	PingDuration time.Duration
	AKGPath      string
	AKGExists    bool
	AKGSize      int64
	AKGModified  time.Time
}

// Doctor is a lenient diagnostic: unlike New, it never fails on configuration
// issues but records them as Problems so the CLI can print a full report.
func Doctor(ctx context.Context, cfg *aiconfig.Config, rootDir string) *DoctorReport {
	rep := &DoctorReport{
		Provider:   cfg.Provider,
		Model:      cfg.Model,
		PingStatus: "skipped",
	}

	meta, ok := provider.Get(cfg.Provider)
	if !ok {
		rep.Problems = append(rep.Problems, fmt.Sprintf("unknown provider %q — run `gmb ai models`", cfg.Provider))
		return rep
	}
	rep.DisplayName = meta.DisplayName
	rep.Adapter = string(meta.Adapter)
	rep.KeyRequired = meta.RequiresKey

	baseURL := aiconfig.EffectiveBaseURL(cfg)
	if baseURL == "" {
		baseURL = meta.DefaultBaseURL
	}
	rep.BaseURL = baseURL
	if baseURL == "" {
		rep.Problems = append(rep.Problems, fmt.Sprintf("no base URL configured and provider %q has no default — set --base-url", meta.Name))
	}

	apiKey := aiconfig.EffectiveAPIKey(cfg, meta.KeyEnvVar)
	rep.KeySet = apiKey != ""
	switch {
	case cfg.APIKey != "":
		rep.KeySource = "config"
	case os.Getenv(meta.KeyEnvVar) != "" || os.Getenv("GLASSMARBLE_AI_API_KEY") != "":
		rep.KeySource = "environment"
	}
	if meta.RequiresKey && !rep.KeySet {
		rep.Problems = append(rep.Problems, fmt.Sprintf("no API key for provider %q — set it via `gmb ai configure`, %s, or GLASSMARBLE_AI_API_KEY", meta.Name, meta.KeyEnvVar))
	}
	if cfg.Model == "" {
		rep.Problems = append(rep.Problems, "no model configured — run `gmb ai configure`")
	}
	rep.ConfigValid = len(rep.Problems) == 0

	// Connectivity ping, independent of AKG state but only when the
	// configuration itself is sane. The ping honors the configured
	// timeout (180s default): large reasoning models (e.g. NVIDIA NIM
	// 120B-class) frequently take longer than 15s to emit the first
	// token, and the historical hard 15s cap produced false "Ping
	// failed" reports on healthy setups.
	if len(rep.Problems) == 0 {
		timeout := pingTimeout(cfg)
		pingCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		var p provider.Provider
		switch meta.Adapter {
		case provider.AdapterOpenAICompat:
			p = provider.NewOpenAICompatProvider(apiKey, baseURL, timeout)
		case provider.AdapterAnthropic:
			p = provider.NewAnthropicProvider(apiKey, baseURL, timeout)
		case provider.AdapterGemini:
			p = provider.NewGeminiProvider(apiKey, baseURL, timeout)
		}

		start := time.Now()
		if err := p.Ping(pingCtx, cfg.Model); err != nil {
			rep.PingStatus = "failed"
			rep.Problems = append(rep.Problems, fmt.Sprintf("connectivity ping to %q failed: %v", cfg.Model, err))
		} else {
			rep.PingStatus = "ok"
		}
		rep.PingDuration = time.Since(start)
	}

	// AKG presence.
	akgPath := filepath.Join(rootDir, ".glassmarble", "akg.json")
	rep.AKGPath = akgPath
	if fi, err := os.Stat(akgPath); err == nil {
		rep.AKGExists = true
		rep.AKGSize = fi.Size()
		rep.AKGModified = fi.ModTime()
	} else if os.IsNotExist(err) {
		rep.Problems = append(rep.Problems, fmt.Sprintf("AKG database not found at %s — run `gmb analyze` first", akgPath))
	} else {
		rep.Problems = append(rep.Problems, fmt.Sprintf("cannot inspect AKG database: %v", err))
	}

	return rep
}

// pingTimeout returns the connectivity-ping budget: the configured request
// timeout, defaulting to 180s when unset. The historical hard 15s cap was
// removed because large reasoning models (e.g. NVIDIA NIM 120B-class) can
// routinely take longer than 15s to emit the first token, which made the
// doctor report healthy setups as "Ping failed".
func pingTimeout(cfg *aiconfig.Config) time.Duration {
	return provider.DurationFor(time.Duration(cfg.TimeoutSec) * time.Second)
}

// MaskAPIKey renders a key for display, showing only the first 4 and last 4
// characters. Short keys are fully masked.
func MaskAPIKey(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return "(not set)"
	}
	if len(key) <= 12 {
		return "********"
	}
	return key[:4] + "..." + key[len(key)-4:]
}
