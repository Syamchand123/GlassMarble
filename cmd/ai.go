package cmd

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/ai_engine"
	"github.com/Syamchand123/GlassMarble/internal/ai_engine/agent"
	"github.com/Syamchand123/GlassMarble/internal/ai_engine/aiconfig"
	"github.com/Syamchand123/GlassMarble/internal/ai_engine/provider"
	"github.com/Syamchand123/GlassMarble/internal/ai_engine/session"
	"github.com/Syamchand123/GlassMarble/internal/terminal"
	"github.com/spf13/cobra"
)

var (
	aiProviderFlag       string
	aiModelFlag          string
	aiAPIKeyFlag         string
	aiBaseURLFlag        string
	aiTemperatureFlag    float64
	aiMaxTurnsFlag       int
	aiTimeoutFlag        int
	aiScopeFlag          string
	aiDirFlag            string
	aiToolsFlag          string
	aiNoToolsFlag        bool
	aiNoStreamFlag       bool
	aiMaxTotalTokensFlag int
	aiMaxCostFlag        float64
	aiMaxSessionMsgFlag  int
	aiSaveFlag           string
	aiChatSessionFlag    string
	aiChatNewFlag        bool
	aiSessionsDeleteFlag string
)

// aiFlagConfig builds a flag-level aiconfig.Config from the AI command flags.
func aiFlagConfig() aiconfig.Config {
	return aiconfig.Config{
		Provider:           aiProviderFlag,
		Model:              aiModelFlag,
		APIKey:             aiAPIKeyFlag,
		BaseURL:            aiBaseURLFlag,
		Temperature:        aiTemperatureFlag,
		MaxTurns:           aiMaxTurnsFlag,
		TimeoutSec:         aiTimeoutFlag,
		MaxTotalTokens:     aiMaxTotalTokensFlag,
		MaxCostUSD:         aiMaxCostFlag,
		MaxSessionMessages: aiMaxSessionMsgFlag,
	}
}

// aiRootDir resolves the repository root from the persistent --root-dir flag.
func aiRootDir(cmd *cobra.Command) string {
	rootDir, _ := cmd.Root().Flags().GetString("root-dir")
	if rootDir == "" {
		return "."
	}
	return rootDir
}

// newAIEngine loads the effective configuration and constructs the engine.
func newAIEngine(cmd *cobra.Command) (*ai_engine.Engine, error) {
	cfg, err := aiconfig.Load(aiFlagConfig())
	if err != nil {
		return nil, err
	}
	if aiNoStreamFlag {
		cfg.Stream = false
	}
	return ai_engine.New(cfg, aiRootDir(cmd))
}

// aiStreamSink buffers streamed answer deltas for the current turn. Tool
// rounds clear the buffer (their text is preamble the model discarded); the
// final answer flushes it once, on the "answer" event. In capture mode
// (--save) nothing is printed: the buffer accumulates the full final answer
// so it can be written to an artifact file.
type aiStreamSink struct {
	buf     strings.Builder
	out     *bufio.Writer
	capture bool
}

func (s *aiStreamSink) write(delta string) {
	s.buf.WriteString(delta)
}

func (s *aiStreamSink) reset() {
	s.buf.Reset()
}

func (s *aiStreamSink) flush() {
	if s.capture || s.buf.Len() == 0 {
		return
	}
	fmt.Fprintln(s.out, s.buf.String())
	s.out.Flush()
	s.buf.Reset()
}

func (s *aiStreamSink) empty() bool { return s.buf.Len() == 0 }

// agentOptions builds the AskAgent options from flags.
func agentOptions(cmd *cobra.Command, sink *aiStreamSink) ai_engine.AgentOptions {
	opts := ai_engine.AgentOptions{EnableTools: !aiNoToolsFlag}
	if aiToolsFlag != "" {
		opts.Tools = strings.Split(aiToolsFlag, ",")
	}
	if opts.EnableTools {
		opts.OnEvent = func(ev agent.Event) {
			switch ev.Type {
			case "tool_call":
				sink.reset()
				args := strings.TrimSpace(ev.ToolArgs)
				if len(args) > 120 {
					args = args[:120] + "…"
				}
				fmt.Fprintf(cmd.ErrOrStderr(), "→ %s(%s)\n", ev.ToolName, args)
			case "tool_result":
				status := "ok"
				if !ev.OK {
					status = "error"
				}
				fmt.Fprintf(cmd.ErrOrStderr(), "← %s: %s (%d bytes)\n", ev.ToolName, status, ev.ResultBytes)
			case "answer":
				sink.flush()
			}
		}
		opts.OnStream = sink.write
	}
	return opts
}

// printStoppedNote explains why an agent run ended early.
func printStoppedNote(w io.Writer, res *agent.Result) {
	switch res.StoppedReason {
	case "turn_limit":
		fmt.Fprintf(w, "Note: stopped after %d tool rounds (--max-turns limit).\n", res.Turns)
	case "token_budget":
		fmt.Fprintf(w, "Note: stopped after %d turns — total token budget exceeded.\n", res.Turns)
	case "cost_budget":
		fmt.Fprintf(w, "Note: stopped after %d turns — estimated cost budget exceeded ($%.4f).\n", res.Turns, res.CostUSD)
	}
}

// formatCost renders a cost estimate for display.
func formatCost(usd float64, estimated bool) string {
	if usd <= 0 {
		return "n/a"
	}
	return fmt.Sprintf("$%.4f", usd)
}

var aiCmd = &cobra.Command{
	Use:   "ai [question]",
	Short: "Ask GlassMarble AI Architect anything about your codebase",
	Long: `Ask the GlassMarble AI Architect a question about your repository.

The AI engine is Bring-Your-Own-Key (BYOK): configure your provider and model
with "gmb ai configure", or via GLASSMARBLE_AI_* environment variables.

The agent answers by calling repository tools: AKG knowledge-graph queries
(akg_*), source readers (code_*), diagram generation (diagram_*), and system
status (system_*).

Examples:
  gmb ai "explain the architecture of this repository"
  gmb ai "which services depend on the payment module"
  gmb ai "generate a C4 container diagram"
  gmb ai "generate a C4 container diagram" --save c4.md   # write markup to .glassmarble/marbles/
  gmb ai "write architecture notes" --save notes.md       # write the answer to .glassmarble/ai/
  gmb ai --no-tools "opinion question"   # plain chat, no tool calling
  gmb ai --tools akg,code "question"     # restrict the tool set
  gmb ai --max-cost 0.5 "question"       # stop when spend is estimated above $0.50
  gmb ai --no-stream "question"          # buffered output instead of streaming
  gmb ai chat                 # interactive conversation (session memory)
  gmb ai chat --new           # fresh session instead of resuming the latest
  gmb ai sessions             # list saved chat sessions
  gmb ai doctor               # diagnose the AI setup`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return fmt.Errorf("no question provided — try: gmb ai \"explain this repository\", or run `gmb ai --help`")
		}

		engine, err := newAIEngine(cmd)
		if err != nil {
			return err
		}

		sink := &aiStreamSink{out: bufio.NewWriter(cmd.OutOrStdout())}
		if aiSaveFlag != "" {
			sink.capture = true
		}
		opts := agentOptions(cmd, sink)
		streaming := engine.Config.Stream

		start := time.Now()
		if streaming {
			res, err := engine.AskAgent(cmd.Context(), args[0], opts)
			if err != nil {
				return fmt.Errorf("AI request failed: %w", err)
			}
			if aiSaveFlag != "" {
				text := res.Text
				if s := sink.buf.String(); s != "" {
					text = s
				}
				if err := saveArtifact(cmd, aiRootDir(cmd), aiSaveFlag, text); err != nil {
					return err
				}
			} else if sink.empty() {
				if res.Text != "" {
					fmt.Fprintln(cmd.OutOrStdout(), res.Text)
				} else if res.StoppedReason != "" {
					fmt.Fprintln(cmd.OutOrStdout(), "(no answer)")
				}
			}
			printStoppedNote(cmd.ErrOrStderr(), res)
			printVerboseTrace(cmd, res)
			return nil
		}

		spinner := terminal.NewSpinner()
		spinner.Start(fmt.Sprintf("Consulting %s (%s)...", engine.Config.Model, engine.Config.Provider))
		res, err := engine.AskAgent(cmd.Context(), args[0], opts)
		spinner.Stop(fmt.Sprintf("Done in %.1fs", time.Since(start).Seconds()))
		if err != nil {
			return fmt.Errorf("AI request failed: %w", err)
		}

		printStoppedNote(cmd.ErrOrStderr(), res)
		if aiSaveFlag != "" {
			if err := saveArtifact(cmd, aiRootDir(cmd), aiSaveFlag, res.Text); err != nil {
				return err
			}
		} else if res.Text == "" {
			fmt.Fprintln(cmd.OutOrStdout(), "(no response)")
		} else {
			fmt.Fprintln(cmd.OutOrStdout(), res.Text)
		}
		printVerboseTrace(cmd, res)
		return nil
	},
}

// saveArtifact writes the final answer to the workspace artifacts:
// diagram markup (fenced mermaid/plantuml/dot blocks) goes to
// .glassmarble/marbles/, everything else to .glassmarble/ai/. The answer is
// not echoed to the terminal — a path receipt is printed instead.
func saveArtifact(cmd *cobra.Command, rootDir, filename, text string) error {
	if filename == "" || filename == "." || filename == ".." ||
		strings.ContainsAny(filename, `/\`) || strings.Contains(filename, "..") {
		return fmt.Errorf("invalid artifact filename %q — use a plain file name without paths", filename)
	}
	if text == "" {
		return fmt.Errorf("the model produced no answer — nothing to save")
	}

	dir := filepath.Join(rootDir, ".glassmarble", "ai")
	if looksLikeDiagram(text) {
		dir = filepath.Join(rootDir, ".glassmarble", "marbles")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("cannot create artifact directory: %w", err)
	}
	path := filepath.Join(dir, filename)
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		return fmt.Errorf("cannot write artifact: %w", err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Artifact saved to %s\n", path)
	return nil
}

// looksLikeDiagram heuristically detects diagram markup in a model answer:
// mermaid/plantuml/dot fenced blocks, PlantUML @startuml directives, or
// Mermaid/DOT graph declarations.
func looksLikeDiagram(text string) bool {
	lines := strings.Split(text, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(strings.ToLower(line))
		switch {
		case strings.HasPrefix(trimmed, "```mermaid"),
			strings.HasPrefix(trimmed, "```plantuml"),
			strings.HasPrefix(trimmed, "```dot"),
			strings.HasPrefix(trimmed, "```graphviz"):
			return true
		case strings.HasPrefix(trimmed, "@startuml"), strings.HasPrefix(trimmed, "@enduml"):
			return true
		case strings.HasPrefix(trimmed, "graph "),
			strings.HasPrefix(trimmed, "flowchart "),
			strings.HasPrefix(trimmed, "sequenceDiagram"),
			strings.HasPrefix(trimmed, "classDiagram"),
			strings.HasPrefix(trimmed, "stateDiagram"),
			strings.HasPrefix(trimmed, "erDiagram"),
			strings.HasPrefix(trimmed, "mindmap"),
			strings.HasPrefix(trimmed, "timeline"),
			strings.HasPrefix(trimmed, "digraph "):
			return true
		}
	}
	return false
}

// printVerboseTrace prints token/cost accounting with --verbose.
func printVerboseTrace(cmd *cobra.Command, res *agent.Result) {
	if verbose, _ := cmd.Flags().GetBool("verbose"); verbose {
		fmt.Fprintf(cmd.ErrOrStderr(), "Tokens: prompt=%d completion=%d total=%d | cost=%s | turns=%d tool-calls=%d\n",
			res.Usage.PromptTokens, res.Usage.CompletionTokens, res.Usage.TotalTokens,
			formatCost(res.CostUSD, res.CostEstimated), res.Turns, len(res.ToolCalls))
	}
}

var aiChatCmd = &cobra.Command{
	Use:   "chat",
	Short: "Start an interactive conversation with the AI Architect",
	Long: `Start a multi-turn conversation with session memory: the transcript is
saved to .glassmarble/ai/sessions/ and the next "gmb ai chat" resumes it.
Use --new to start fresh, --session <id> to resume a specific conversation,
and "exit", "quit", or Ctrl+D to leave.

  gmb ai chat                  # resume the latest session (or start one)
  gmb ai chat --new            # force a fresh session
  gmb ai chat --session 2026... # resume a specific session (see gmb ai sessions)`,
	RunE: func(cmd *cobra.Command, args []string) error {
		engine, err := newAIEngine(cmd)
		if err != nil {
			return err
		}

		rootDir := aiRootDir(cmd)
		sessDir := session.Dir(rootDir)
		var sess *session.Session
		switch {
		case aiChatSessionFlag != "":
			if aiChatNewFlag {
				return fmt.Errorf("--session and --new are mutually exclusive")
			}
			sess, err = session.Open(sessDir, aiChatSessionFlag)
			if err != nil {
				return fmt.Errorf("cannot resume session %q: %v — run `gmb ai sessions` to list saved sessions", aiChatSessionFlag, err)
			}
		case aiChatNewFlag:
			sess = session.Create(sessDir, engine.Config.Provider, engine.Config.Model)
		default:
			sess, err = session.Latest(sessDir)
			if err != nil {
				sess = session.Create(sessDir, engine.Config.Provider, engine.Config.Model)
			}
		}

		reader := bufio.NewReader(cmd.InOrStdin())
		isTTY := terminal.IsTTY()

		if isTTY {
			fmt.Fprintf(cmd.OutOrStdout(), "GlassMarble AI Architect — %s/%s\n", engine.Config.Provider, engine.Config.Model)
			fmt.Fprintf(cmd.OutOrStdout(), "Session %s (%d prior messages). Type your question; 'exit' or 'quit' to leave.\n",
				sess.ID, len(sess.Messages))
		}

		sink := &aiStreamSink{out: bufio.NewWriter(cmd.OutOrStdout())}
		streaming := engine.Config.Stream

		for {
			if isTTY {
				fmt.Fprint(cmd.OutOrStdout(), "gmb ai> ")
			}
			line, err := reader.ReadString('\n')
			if err != nil {
				break // EOF
			}
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			lower := strings.ToLower(line)
			if lower == "exit" || lower == "quit" || lower == "bye" {
				break
			}

			sess.Trim(engine.Config.MaxSessionMessages)
			opts := agentOptions(cmd, sink)
			opts.History = sess.Messages

			if streaming {
				res, err := engine.AskAgent(cmd.Context(), line, opts)
				if err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "Error: %v\n", err)
					continue
				}
				if sink.empty() {
					if res.Text != "" {
						fmt.Fprintln(cmd.OutOrStdout(), res.Text)
					} else if res.StoppedReason != "" {
						fmt.Fprintln(cmd.OutOrStdout(), "(no answer)")
					}
				}
				printStoppedNote(cmd.ErrOrStderr(), res)
				applyToSession(sess, res, sessDir)
				continue
			}

			spinner := terminal.NewSpinner()
			spinner.Start(fmt.Sprintf("Consulting %s...", engine.Config.Model))
			res, err := engine.AskAgent(cmd.Context(), line, opts)
			spinner.Stop("")
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "Error: %v\n", err)
				continue
			}

			if res.Text != "" {
				fmt.Fprintln(cmd.OutOrStdout(), res.Text)
			} else if res.StoppedReason != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "(no answer — %s)\n", reasonLabel(res.StoppedReason))
			}
			printStoppedNote(cmd.ErrOrStderr(), res)
			applyToSession(sess, res, sessDir)
		}

		if sess.Turns > 0 {
			fmt.Fprintf(cmd.ErrOrStderr(), "Session %s: %d turns, %d messages, %d tokens, cost %s (resume with `gmb ai chat --session %s`)\n",
				sess.ID, sess.Turns, len(sess.Messages), sess.Usage.TotalTokens,
				formatCost(sess.CostUSD, sess.Usage.TotalTokens > 0), sess.ID)
		}
		return nil
	},
}

// applyToSession folds an agent run into the persistent session and saves it.
func applyToSession(sess *session.Session, res *agent.Result, dir string) {
	sess.Messages = res.Messages
	sess.Usage.PromptTokens += res.Usage.PromptTokens
	sess.Usage.CompletionTokens += res.Usage.CompletionTokens
	sess.Usage.TotalTokens += res.Usage.TotalTokens
	sess.CostUSD += res.CostUSD
	sess.Turns += res.Turns
	sess.ToolCalls += len(res.ToolCalls)
	sess.Touch()
	if err := sess.Save(dir); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not save session: %v\n", err)
	}
}

func reasonLabel(reason string) string {
	switch reason {
	case "turn_limit":
		return "tool-round limit reached"
	case "token_budget":
		return "token budget exceeded"
	case "cost_budget":
		return "cost budget exceeded"
	}
	return reason
}

var aiSessionsCmd = &cobra.Command{
	Use:   "sessions",
	Short: "List saved AI chat sessions",
	Long: `List the conversation sessions saved under .glassmarble/ai/sessions/.
Sessions are written by "gmb ai chat" and can be resumed with
"gmb ai chat --session <id>".

  gmb ai sessions              # list sessions, newest first
  gmb ai sessions --delete <id> # remove one session`,
	RunE: func(cmd *cobra.Command, args []string) error {
		dir := session.Dir(aiRootDir(cmd))
		if aiSessionsDeleteFlag != "" {
			if err := session.Delete(dir, aiSessionsDeleteFlag); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Deleted session %s\n", aiSessionsDeleteFlag)
			return nil
		}

		list, err := session.List(dir)
		if err != nil {
			return err
		}
		if len(list) == 0 {
			fmt.Fprintln(cmd.OutOrStdout(), "No saved sessions. Start one with `gmb ai chat`.")
			return nil
		}
		fmt.Fprintf(cmd.OutOrStdout(), "%-22s %-19s %-24s %5s %6s %8s %7s %9s\n",
			"ID", "UPDATED", "PROVIDER/MODEL", "MSGS", "TURNS", "TOKENS", "COST", "TOOLS")
		for _, s := range list {
			fmt.Fprintf(cmd.OutOrStdout(), "%-22s %-19s %-24s %5d %6d %8d %7s %9d\n",
				s.ID, s.Updated.Format("2006-01-02 15:04"), s.Provider+"/"+s.Model,
				s.Messages, s.Turns, s.Tokens, formatCost(s.CostUSD, s.Tokens > 0), s.ToolCalls)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "\n%d session(s). Resume with: gmb ai chat --session <id>\n", len(list))
		return nil
	},
}

var aiConfigureCmd = &cobra.Command{
	Use:   "configure",
	Short: "Configure the AI provider, model, and API key (BYOK)",
	Long: `Configure the GlassMarble AI engine.

Without flags, runs an interactive setup. With flags, updates the target
configuration file. Keys are stored in the config file with 0600 permissions
and are never logged.

  gmb ai configure --provider openai --model gpt-4o --key sk-...
  gmb ai configure --scope project --provider gemini --model gemini-2.5-flash`,
	RunE: func(cmd *cobra.Command, args []string) error {
		changed := cmd.Flags().Changed("provider") || cmd.Flags().Changed("model") ||
			cmd.Flags().Changed("key") || cmd.Flags().Changed("base-url") ||
			cmd.Flags().Changed("temperature") || cmd.Flags().Changed("max-turns") ||
			cmd.Flags().Changed("timeout") || cmd.Flags().Changed("max-total-tokens") ||
			cmd.Flags().Changed("max-cost") || cmd.Flags().Changed("max-session-messages")

		scope := aiScopeFlag
		if scope != "global" && scope != "project" {
			return fmt.Errorf("invalid scope %q — use 'global' or 'project'", scope)
		}

		target := aiconfig.GlobalPath()
		if scope == "project" {
			target = filepath.Join(aiDirFlag, aiconfig.ProjectConfigPath)
		}
		if target == "" {
			return fmt.Errorf("cannot locate the global config file (home directory unavailable)")
		}

		// Load any existing settings from the target file.
		cfg, err := aiconfig.LoadFile(target)
		if err != nil {
			return err
		}

		if !changed {
			if err := configureInteractive(cmd, cfg); err != nil {
				return err
			}
		} else {
			if cmd.Flags().Changed("provider") {
				cfg.Provider = aiProviderFlag
			}
			if cmd.Flags().Changed("model") {
				cfg.Model = aiModelFlag
			}
			if cmd.Flags().Changed("key") {
				cfg.APIKey = aiAPIKeyFlag
			}
			if cmd.Flags().Changed("base-url") {
				cfg.BaseURL = aiBaseURLFlag
			}
			if cmd.Flags().Changed("temperature") {
				cfg.Temperature = aiTemperatureFlag
			}
			if cmd.Flags().Changed("max-turns") {
				cfg.MaxTurns = aiMaxTurnsFlag
			}
			if cmd.Flags().Changed("timeout") {
				cfg.TimeoutSec = aiTimeoutFlag
			}
			if cmd.Flags().Changed("max-total-tokens") {
				cfg.MaxTotalTokens = aiMaxTotalTokensFlag
			}
			if cmd.Flags().Changed("max-cost") {
				cfg.MaxCostUSD = aiMaxCostFlag
			}
			if cmd.Flags().Changed("max-session-messages") {
				cfg.MaxSessionMessages = aiMaxSessionMsgFlag
			}
		}

		if _, ok := provider.Get(cfg.Provider); !ok {
			return fmt.Errorf("unknown provider %q — run `gmb ai models` for the supported list", cfg.Provider)
		}

		if err := aiconfig.Save(target, cfg); err != nil {
			return err
		}

		meta, _ := provider.Get(cfg.Provider)
		fmt.Fprintf(cmd.OutOrStdout(), "AI configuration saved to %s\n", target)
		fmt.Fprintf(cmd.OutOrStdout(), "Provider: %s (%s)\n", cfg.Provider, meta.DisplayName)
		fmt.Fprintf(cmd.OutOrStdout(), "Model:    %s\n", cfg.Model)
		fmt.Fprintf(cmd.OutOrStdout(), "API key:  %s\n", ai_engine.MaskAPIKey(cfg.APIKey))
		if cfg.BaseURL != "" {
			fmt.Fprintf(cmd.OutOrStdout(), "Base URL: %s\n", cfg.BaseURL)
		}
		return nil
	},
}

var aiModelsCmd = &cobra.Command{
	Use:   "models",
	Short: "List supported AI providers and their models",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := aiconfig.Load(aiconfig.Config{})
		if err != nil {
			return err
		}

		for _, m := range provider.Registry {
			marker := " "
			if m.Name == cfg.Provider {
				marker = "*"
			}
			keyStatus := "no key"
			if !m.RequiresKey {
				keyStatus = "no key required"
			} else if aiconfig.EffectiveAPIKey(cfg, m.KeyEnvVar) != "" {
				keyStatus = "key set"
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s %-12s %-24s [%s]\n", marker, m.Name, m.DisplayName, keyStatus)
			fmt.Fprintf(cmd.OutOrStdout(), "   adapter: %s\n", m.Adapter)
			fmt.Fprintf(cmd.OutOrStdout(), "   base URL: %s\n", defaultOrCustom(m.DefaultBaseURL))
			fmt.Fprintf(cmd.OutOrStdout(), "   key env: %s\n", m.KeyEnvVar)
			if len(m.Models) > 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "   models: %s\n", strings.Join(m.Models, ", "))
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "   models: (set any model with --model)\n")
			}
		}
		fmt.Fprintln(cmd.OutOrStdout(), "\n* = configured provider")
		return nil
	},
}

var aiDoctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Diagnose the AI engine setup",
	Long: `Validate the AI configuration, test provider connectivity, and check
the state of the Architecture Knowledge Graph.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := aiconfig.Load(aiFlagConfig())
		if err != nil {
			return err
		}

		rep := ai_engine.Doctor(cmd.Context(), cfg, aiRootDir(cmd))

		w := cmd.OutOrStdout()
		fmt.Fprintln(w, "AI Engine Doctor")
		fmt.Fprintln(w, "================")
		fmt.Fprintf(w, "Provider   : %s", rep.Provider)
		if rep.DisplayName != "" {
			fmt.Fprintf(w, " (%s)", rep.DisplayName)
		}
		fmt.Fprintln(w)
		fmt.Fprintf(w, "Adapter    : %s\n", rep.Adapter)
		fmt.Fprintf(w, "Model      : %s\n", rep.Model)
		fmt.Fprintf(w, "Base URL   : %s\n", defaultOrCustom(rep.BaseURL))
		fmt.Fprintf(w, "API key    : %s", ai_engine.MaskAPIKey(cfg.APIKey))
		if rep.KeySource != "" {
			fmt.Fprintf(w, " (%s)", rep.KeySource)
		}
		fmt.Fprintln(w)
		fmt.Fprintf(w, "Ping       : %s\n", pingStatus(rep))
		fmt.Fprintf(w, "AKG        : %s\n", akgStatus(rep))

		if len(rep.Problems) > 0 {
			fmt.Fprintf(w, "\n%d problem(s):\n", len(rep.Problems))
			for _, p := range rep.Problems {
				fmt.Fprintf(w, "  - %s\n", p)
			}
			return fmt.Errorf("doctor found %d problem(s)", len(rep.Problems))
		}
		fmt.Fprintln(w, "\nAll checks passed.")
		return nil
	},
}

func configureInteractive(cmd *cobra.Command, cfg *aiconfig.Config) error {
	reader := bufio.NewReader(cmd.InOrStdin())
	out := cmd.OutOrStdout()

	fmt.Fprintln(out, "Select an AI provider:")
	for i, m := range provider.Registry {
		fmt.Fprintf(out, "  %2d) %-24s %s\n", i+1, m.Name, m.Description)
	}
	fmt.Fprint(out, "Provider [1]: ")
	choice, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("setup aborted")
	}
	choice = strings.TrimSpace(choice)
	idx := 0
	if choice != "" {
		if n, convErr := strconv.Atoi(choice); convErr == nil && n >= 1 && n <= len(provider.Registry) {
			idx = n - 1
		} else if _, ok := provider.Get(choice); ok {
			for i, m := range provider.Registry {
				if m.Name == choice {
					idx = i
				}
			}
		} else {
			return fmt.Errorf("unknown provider %q", choice)
		}
	}
	meta := provider.Registry[idx]
	cfg.Provider = meta.Name

	if meta.RequiresKey {
		fmt.Fprint(out, "API key (paste it; it will be stored locally): ")
		key, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("setup aborted")
		}
		key = strings.TrimSpace(key)
		if key == "" {
			return fmt.Errorf("API key is required for provider %q", meta.Name)
		}
		cfg.APIKey = key
	}

	defaultModel := ""
	if len(meta.Models) > 0 {
		defaultModel = meta.Models[0]
	}
	if defaultModel == "" {
		fmt.Fprint(out, "Model: ")
	} else {
		fmt.Fprintf(out, "Model [%s]: ", defaultModel)
	}
	model, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("setup aborted")
	}
	model = strings.TrimSpace(model)
	if model == "" {
		if defaultModel == "" {
			return fmt.Errorf("model is required for provider %q", meta.Name)
		}
		model = defaultModel
	}
	cfg.Model = model

	if meta.DefaultBaseURL == "" {
		fmt.Fprint(out, "Base URL (required for this provider): ")
	} else {
		fmt.Fprintf(out, "Base URL [%s, press Enter to accept]: ", meta.DefaultBaseURL)
	}
	baseURL, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("setup aborted")
	}
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		baseURL = meta.DefaultBaseURL
	}
	if baseURL == "" {
		return fmt.Errorf("base URL is required for provider %q", meta.Name)
	}
	cfg.BaseURL = baseURL

	return nil
}

func defaultOrCustom(v string) string {
	if v == "" {
		return "(set --base-url)"
	}
	return v
}

func pingStatus(rep *ai_engine.DoctorReport) string {
	switch rep.PingStatus {
	case "ok":
		return fmt.Sprintf("ok (%.1fs)", rep.PingDuration.Seconds())
	case "failed":
		return "failed"
	default:
		return "skipped (configuration problems above)"
	}
}

func akgStatus(rep *ai_engine.DoctorReport) string {
	if !rep.AKGExists {
		return fmt.Sprintf("not found at %s — run `gmb analyze` first", rep.AKGPath)
	}
	return fmt.Sprintf("found at %s (%d bytes, modified %s)", rep.AKGPath, rep.AKGSize, rep.AKGModified.Format(time.RFC3339))
}

// ResetAIFlags restores AI command flag defaults (used by tests).
func ResetAIFlags() {
	aiProviderFlag = ""
	aiModelFlag = ""
	aiAPIKeyFlag = ""
	aiBaseURLFlag = ""
	aiTemperatureFlag = 0
	aiMaxTurnsFlag = 0
	aiTimeoutFlag = 0
	aiScopeFlag = "global"
	aiDirFlag = "."
	aiToolsFlag = ""
	aiNoToolsFlag = false
	aiNoStreamFlag = false
	aiMaxTotalTokensFlag = 0
	aiMaxCostFlag = 0
	aiMaxSessionMsgFlag = 0
	aiSaveFlag = ""
	aiChatSessionFlag = ""
	aiChatNewFlag = false
	aiSessionsDeleteFlag = ""
}

func init() {
	aiCmd.PersistentFlags().StringVar(&aiProviderFlag, "provider", "", "AI provider name (openai, anthropic, gemini, deepseek, ...)")
	aiCmd.PersistentFlags().StringVar(&aiModelFlag, "model", "", "AI model identifier")
	aiCmd.PersistentFlags().StringVar(&aiAPIKeyFlag, "key", "", "API key for the AI provider (prefer env vars or gmb ai configure)")
	aiCmd.PersistentFlags().StringVar(&aiBaseURLFlag, "base-url", "", "Override the provider API base URL")
	aiCmd.PersistentFlags().Float64Var(&aiTemperatureFlag, "temperature", 0, "Sampling temperature (0 = provider default)")
	aiCmd.PersistentFlags().IntVar(&aiMaxTurnsFlag, "max-turns", 0, "Maximum agent tool-call turns")
	aiCmd.PersistentFlags().IntVar(&aiTimeoutFlag, "timeout", 0, "HTTP timeout in seconds")
	aiCmd.PersistentFlags().StringVar(&aiToolsFlag, "tools", "", "Restrict agent tools to categories (system, akg, code, diagram) or names, comma-separated")
	aiCmd.PersistentFlags().BoolVar(&aiNoToolsFlag, "no-tools", false, "Plain chat mode without tool calling")
	aiCmd.PersistentFlags().BoolVar(&aiNoStreamFlag, "no-stream", false, "Disable token streaming (buffered output)")
	aiCmd.PersistentFlags().IntVar(&aiMaxTotalTokensFlag, "max-total-tokens", 0, "Stop a run when summed prompt+completion tokens exceed this (0 = unlimited)")
	aiCmd.PersistentFlags().Float64Var(&aiMaxCostFlag, "max-cost", 0, "Stop a run when estimated spend exceeds this USD amount (0 = unlimited)")
	aiCmd.PersistentFlags().IntVar(&aiMaxSessionMsgFlag, "max-session-messages", 0, "Chat history budget: keep at most this many session messages")
	aiCmd.PersistentFlags().StringVar(&aiSaveFlag, "save", "", "Save the final answer to .glassmarble/ai/ (diagram markup to .glassmarble/marbles/) instead of printing it (single-query mode)")

	aiConfigureCmd.Flags().StringVar(&aiScopeFlag, "scope", "global", "Where to write the config: 'global' (~/.glassmarble) or 'project' (repo)")
	aiConfigureCmd.Flags().StringVar(&aiDirFlag, "dir", ".", "Workspace directory for --scope project")
	aiConfigureCmd.Flags().IntVar(&aiMaxTotalTokensFlag, "max-total-tokens", 0, "Default per-run token budget")
	aiConfigureCmd.Flags().Float64Var(&aiMaxCostFlag, "max-cost", 0, "Default per-run cost budget (USD)")
	aiConfigureCmd.Flags().IntVar(&aiMaxSessionMsgFlag, "max-session-messages", 0, "Default chat history budget (messages)")

	aiChatCmd.Flags().StringVar(&aiChatSessionFlag, "session", "", "Resume a specific saved session by id")
	aiChatCmd.Flags().BoolVar(&aiChatNewFlag, "new", false, "Start a fresh session instead of resuming")

	aiSessionsCmd.Flags().StringVar(&aiSessionsDeleteFlag, "delete", "", "Delete a saved session by id")

	aiCmd.AddCommand(aiChatCmd)
	aiCmd.AddCommand(aiConfigureCmd)
	aiCmd.AddCommand(aiModelsCmd)
	aiCmd.AddCommand(aiDoctorCmd)
	aiCmd.AddCommand(aiSessionsCmd)

	rootCmd.AddCommand(aiCmd)
}
