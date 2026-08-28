package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/ai_engine"
	"github.com/Syamchand123/GlassMarble/internal/ai_engine/agent"
	"github.com/Syamchand123/GlassMarble/internal/ai_engine/aiconfig"
	"github.com/Syamchand123/GlassMarble/internal/ai_engine/provider"
	"github.com/Syamchand123/GlassMarble/internal/ai_engine/session"
	producterrs "github.com/Syamchand123/GlassMarble/internal/product/errors"
	"github.com/Syamchand123/GlassMarble/internal/tui"
	"github.com/Syamchand123/GlassMarble/internal/tui/programs/ai_query"
	"github.com/Syamchand123/GlassMarble/internal/tui/programs/chat"
	"github.com/Syamchand123/GlassMarble/internal/tui/programs/configure"
	"github.com/Syamchand123/GlassMarble/internal/tui/programs/sessions"
	"github.com/Syamchand123/GlassMarble/internal/tui/views"
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
// Temperature is a *float64 sentinel: nil means unset (provider default), 0.0
// means explicit 0. The flag is only set when the user explicitly passed
// --temperature (including 0).
func aiFlagConfig(cmd *cobra.Command) aiconfig.Config {
	cfg := aiconfig.Config{
		Provider:           aiProviderFlag,
		Model:              aiModelFlag,
		APIKey:             aiAPIKeyFlag,
		BaseURL:            aiBaseURLFlag,
		MaxTurns:           aiMaxTurnsFlag,
		TimeoutSec:         aiTimeoutFlag,
		MaxTotalTokens:     aiMaxTotalTokensFlag,
		MaxCostUSD:         aiMaxCostFlag,
		MaxSessionMessages: aiMaxSessionMsgFlag,
	}
	if isTempFlagChanged(cmd) {
		v := aiTemperatureFlag
		cfg.Temperature = &v
	}
	return cfg
}

func isTempFlagChanged(cmd *cobra.Command) bool {
	if cmd == nil {
		return false
	}
	if f := cmd.Flags().Lookup("temperature"); f != nil && f.Changed {
		return true
	}
	if f := cmd.PersistentFlags().Lookup("temperature"); f != nil && f.Changed {
		return true
	}
	for c := cmd.Parent(); c != nil; c = c.Parent() {
		if f := c.PersistentFlags().Lookup("temperature"); f != nil && f.Changed {
			return true
		}
	}
	if f := cmd.Root().PersistentFlags().Lookup("temperature"); f != nil && f.Changed {
		return true
	}
	return false
}

// aiRootDir resolves the repository root from the persistent --dir or --root-dir flag.
func aiRootDir(cmd *cobra.Command) string {
	return resolveDir(cmd)
}

// newAIEngine loads the effective configuration and constructs the engine.
func newAIEngine(cmd *cobra.Command) (*ai_engine.Engine, error) {
	cfg, err := aiconfig.LoadForDir(aiRootDir(cmd), aiFlagConfig(cmd))
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
	Use:     "ai [question]",
	GroupID: GroupAI.ID,
	Short:   "Ask GlassMarble AI Architect anything about your codebase",
	Long: `Ask the GlassMarble AI Architect a question about your repository.

The AI engine is Bring-Your-Own-Key (BYOK): configure your provider and model
with "gmb ai configure", or via GLASSMARBLE_AI_* environment variables.

The agent answers by calling repository tools: AKG knowledge-graph queries
(akg_*), source readers (code_*), diagram generation (diagram_*), and system
status (system_*).`,
	Example: `  # Ask a question about the repository architecture
  gmb ai "explain the architecture of this repository"

  # Ask a dependency question
  gmb ai "which services depend on the payment module"

  # Ask AI to generate a C4 container diagram and save it
  gmb ai "generate a C4 container diagram" --save c4.md

  # Restrict tool usage to code and AKG tools only
  gmb ai --tools akg,code "where is user authentication implemented?"

  # Start an interactive chat session
  gmb ai chat

  # Diagnose AI engine setup and connectivity
  gmb ai doctor`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return producterrs.Tagged(fmt.Sprintf("no question provided — try: gmb ai \"explain this repository\", or run `gmb ai --help`"), producterrs.ErrValidation)
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

		if tui.IsInteractive(cmd.InOrStdin(), cmd.OutOrStdout()) {
			verbose := false
			if v, _ := cmd.Flags().GetBool("verbose"); v {
				verbose = true
			}
			var save func(text string) error
			if aiSaveFlag != "" {
				save = func(text string) error { return saveArtifact(cmd, aiRootDir(cmd), aiSaveFlag, text) }
			}
			return ai_query.Run(cmd.Context(), engine, args[0], opts, streaming, verbose, cmd.InOrStdin(), cmd.OutOrStdout(), save)
		}

		start := time.Now()
		if streaming {
			res, err := engine.AskAgent(cmd.Context(), args[0], opts)
			if err != nil {
				return fmt.Errorf("AI request failed: %w", err)
			}
			if aiSaveFlag != "" {
				// C6-D4: sink.buf duplicates res.Text after streaming via
				// answer event flush; prefer the canonical res.Text.
				text := res.Text
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

		fmt.Fprintf(cmd.ErrOrStderr(), "Consulting %s (%s)...\n", engine.Config.Model, engine.Config.Provider)
		res, err := engine.AskAgent(cmd.Context(), args[0], opts)
		fmt.Fprintf(cmd.ErrOrStderr(), "Done in %.1fs\n", time.Since(start).Seconds())
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
	// C6-D1: use path-segment check, not substring ".." (which rejects
	// legitimate names like "my..notes.md"); also catch "a/b/../c" via
	// segment walk. Delegates to filepath.Clean semantics.
	clean := filepath.ToSlash(filepath.Clean(filename))
	if filename == "" || filename == "." || filename == ".." ||
		strings.ContainsAny(filename, `/\`) || clean != filename {
		return producterrs.Tagged(fmt.Sprintf("invalid artifact filename %q — use a plain file name without paths", filename), producterrs.ErrValidation)
	}
	for _, seg := range strings.Split(clean, "/") {
		if seg == ".." {
			return producterrs.Tagged(fmt.Sprintf("invalid artifact filename %q — use a plain file name without paths", filename), producterrs.ErrValidation)
		}
	}
	if text == "" {
		return producterrs.Tagged(fmt.Sprintf("the model produced no answer — nothing to save"), producterrs.ErrEmptySubgraph)
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

// saveChatAnswer writes the last chat turn to .glassmarble/ai/ and returns
// the written path (Ctrl+S in the interactive chat program).
func saveChatAnswer(cmd *cobra.Command, rootDir, sessID, text string) (string, error) {
	if text == "" {
		return "", fmt.Errorf("nothing to save")
	}
	dir := filepath.Join(rootDir, ".glassmarble", "ai")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("cannot create artifact directory: %w", err)
	}
	path := filepath.Join(dir, "chat-"+sessID+".md")
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		return "", fmt.Errorf("cannot write artifact: %w", err)
	}
	return path, nil
}

// looksLikeDiagram heuristically detects diagram markup in a model answer:
// mermaid/plantuml/dot fenced blocks, PlantUML @startuml directives, or
// Mermaid/DOT graph declarations. C6-D3: also detects unfenced raw mermaid
// returned by some models (e.g. "graph TD; A-->B" without fences) so it
// routes to .glassmarble/marbles/ instead of .glassmarble/ai/.
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
	// C6-D3: unfenced raw detection — check whole text for graph keywords
	// or Mermaid edge syntax without requiring fence or line-start context.
	lower := strings.ToLower(text)
	if strings.Contains(lower, "graph ") || strings.Contains(lower, "flowchart ") ||
		strings.Contains(lower, "sequencediagram") || strings.Contains(lower, "classdiagram") ||
		strings.Contains(lower, "statediagram") || strings.Contains(lower, "erdiagram") ||
		strings.Contains(lower, "@startuml") || strings.Contains(lower, "digraph ") {
		return true
	}
	if strings.Contains(text, "-->") || strings.Contains(text, "---") {
		if strings.Contains(lower, "graph") || strings.Contains(lower, "flowchart") {
			return true
		}
	}
	return false
}

// printVerboseTrace prints token/cost accounting with --verbose.
func printVerboseTrace(cmd *cobra.Command, res *agent.Result) {
	if verbose, _ := cmd.Flags().GetBool("verbose"); verbose {
		if res.Usage.TotalTokens > 0 {
			fmt.Fprintf(cmd.ErrOrStderr(), "Tokens: prompt=%d completion=%d total=%d | cost=%s | turns=%d tool-calls=%d\n",
				res.Usage.PromptTokens, res.Usage.CompletionTokens, res.Usage.TotalTokens,
				formatCost(res.CostUSD, res.CostEstimated), res.Turns, len(res.ToolCalls))
		} else {
			fmt.Fprintf(cmd.ErrOrStderr(), "cost=%s | turns=%d tool-calls=%d\n",
				formatCost(res.CostUSD, res.CostEstimated), res.Turns, len(res.ToolCalls))
		}
	}
}

var aiChatCmd = &cobra.Command{
	Use:   "chat",
	Short: "Start an interactive conversation with the AI Architect",
	Long: `Start a multi-turn conversation with session memory: the transcript is
saved to .glassmarble/ai/sessions/ and the next "gmb ai chat" resumes it.
Use --new to start fresh, --session <id> to resume a specific conversation,
and "exit", "quit", or Ctrl+D to leave.`,
	Example: `  # Resume latest conversation or start a new one
  gmb ai chat

  # Force a fresh chat session
  gmb ai chat --new

  # Resume a specific session by ID
  gmb ai chat --session 2026-08-24-10-00-00`,
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
				return producterrs.Tagged("--session and --new are mutually exclusive — choose one", producterrs.ErrValidation)
			}
			sess, err = session.Open(sessDir, aiChatSessionFlag)
			if err != nil {
				return producterrs.Annotate(fmt.Errorf("cannot resume session %q: %v — run `gmb ai sessions` to list saved sessions", aiChatSessionFlag, err), producterrs.ErrValidation)
			}
		case aiChatNewFlag:
			sess = session.Create(sessDir, engine.Config.Provider, engine.Config.Model)
		default:
			sess, err = session.Latest(sessDir)
			if err != nil {
				sess = session.Create(sessDir, engine.Config.Provider, engine.Config.Model)
			}
		}

		if tui.IsInteractive(cmd.InOrStdin(), cmd.OutOrStdout()) {
			opts := agentOptions(cmd, &aiStreamSink{out: bufio.NewWriter(cmd.OutOrStdout())})
			save := func(text string) (string, error) {
				return saveChatAnswer(cmd, rootDir, sess.ID, text)
			}
			return chat.Run(cmd.Context(), engine, sess, sessDir, opts, applyToSession, save, cmd.InOrStdin(), cmd.OutOrStdout())
		}

		reader := bufio.NewReader(cmd.InOrStdin())
		isTTY := tui.IsInteractive(cmd.InOrStdin(), cmd.OutOrStdout())

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

			fmt.Fprintf(cmd.ErrOrStderr(), "Consulting %s...\n", engine.Config.Model)
			res, err := engine.AskAgent(cmd.Context(), line, opts)
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
			fmt.Fprintf(cmd.ErrOrStderr(), "Session %s: %d turns, %d messages, cost %s (resume with `gmb ai chat --session %s`)\n",
				sess.ID, sess.Turns, len(sess.Messages),
				formatCost(sess.CostUSD, sess.CostUSD > 0), sess.ID)
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
"gmb ai chat --session <id>".`,
	Example: `  # List all saved AI chat sessions
  gmb ai sessions

  # Delete a specific saved session
  gmb ai sessions --delete 2026-08-24-10-00-00

  # Output session history list as JSON
  gmb ai sessions --json`,
	RunE: func(cmd *cobra.Command, args []string) error {
		asJSON, _ := cmd.Flags().GetBool("json")
		dir := session.Dir(aiRootDir(cmd))
		if aiSessionsDeleteFlag != "" {
			if err := session.Delete(dir, aiSessionsDeleteFlag); err != nil {
				return err
			}
			if asJSON {
				out, _ := json.MarshalIndent(map[string]string{"status": "deleted", "session_id": aiSessionsDeleteFlag}, "", "  ")
				fmt.Fprintln(cmd.OutOrStdout(), string(out))
				return nil
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Deleted session %s\n", aiSessionsDeleteFlag)
			return nil
		}

		if !asJSON && tui.IsInteractive(cmd.InOrStdin(), cmd.OutOrStdout()) {
			del := func(id string) error { return session.Delete(dir, id) }
			return sessions.Run(dir, cmd.InOrStdin(), cmd.OutOrStdout(), del)
		}

		list, err := session.List(dir)
		if err != nil {
			return err
		}
		if asJSON {
			out, _ := json.MarshalIndent(list, "", "  ")
			fmt.Fprintln(cmd.OutOrStdout(), string(out))
			return nil
		}
		if len(list) == 0 {
			fmt.Fprintln(cmd.OutOrStdout(), views.RenderSessions(list))
			return nil
		}
		fmt.Fprintln(cmd.OutOrStdout(), views.RenderSessions(list))
		return nil
	},
}

var aiConfigureCmd = &cobra.Command{
	Use:   "configure",
	Short: "Configure the AI provider, model, and API key (BYOK)",
	Long: `Configure the GlassMarble AI engine.

Without flags, runs an interactive setup. With flags, updates the target
configuration file. Keys are stored in the config file with 0600 permissions
and are never logged.`,
	Example: `  # Configure OpenAI with GPT-4o
  gmb ai configure --provider openai --model gpt-4o --key sk-...

  # Configure Gemini in project scope
  gmb ai configure --scope project --provider gemini --model gemini-2.5-flash

  # Run interactive terminal setup wizard
  gmb ai configure`,
	RunE: func(cmd *cobra.Command, args []string) error {
		changed := cmd.Flags().Changed("provider") || cmd.Flags().Changed("model") ||
			cmd.Flags().Changed("key") || cmd.Flags().Changed("base-url") ||
			cmd.Flags().Changed("temperature") || cmd.Flags().Changed("max-turns") ||
			cmd.Flags().Changed("timeout") || cmd.Flags().Changed("max-total-tokens") ||
			cmd.Flags().Changed("max-cost") || cmd.Flags().Changed("max-session-messages")

		scope := aiScopeFlag
		if scope != "global" && scope != "project" {
			return producterrs.Tagged(fmt.Sprintf("invalid scope %q — use 'global' or 'project'", scope), producterrs.ErrValidation)
		}

		target := aiconfig.GlobalPath()
		if scope == "project" {
			target = filepath.Join(aiDirFlag, aiconfig.ProjectConfigPath)
		}
		if target == "" {
			return producterrs.Tagged("cannot locate the global config file (home directory unavailable)", producterrs.ErrValidation)
		}

		// Load any existing settings from the target file.
		cfg, err := aiconfig.LoadFile(target)
		if err != nil {
			return err
		}

		if !changed {
			if tui.IsInteractive(cmd.InOrStdin(), cmd.OutOrStdout()) {
				res, err := configure.Run(provider.Registry, cfg, cmd.InOrStdin(), cmd.OutOrStdout())
				if err != nil {
					return err
				}
				cfg.Provider = res.Provider
				cfg.APIKey = res.APIKey
				cfg.Model = res.Model
				cfg.BaseURL = res.BaseURL
			} else {
				return producterrs.Tagged("interactive configuration requires a terminal — run `gmb ai configure --provider NAME --model MODEL --key KEY`, or set GLASSMARBLE_AI_* environment variables", producterrs.ErrValidation)
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
				v := aiTemperatureFlag
				cfg.Temperature = &v
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
			return producterrs.Tagged(fmt.Sprintf("unknown provider %q — run `gmb ai models` for the supported list", cfg.Provider), producterrs.ErrValidation)
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
	Example: `  # List all available AI providers and models
  gmb ai models

  # Output supported models and provider configuration status as JSON
  gmb ai models --json`,
	RunE: func(cmd *cobra.Command, args []string) error {
		asJSON, _ := cmd.Flags().GetBool("json")
		cfg, err := aiconfig.LoadForDir(aiRootDir(cmd), aiconfig.Config{})
		if err != nil {
			return err
		}

		if asJSON {
			type modelInfoJSON struct {
				Provider     string   `json:"provider"`
				DisplayName  string   `json:"display_name"`
				DefaultModel string   `json:"default_model"`
				HasAPIKey    bool     `json:"has_api_key"`
				Models       []string `json:"models"`
			}
			var list []modelInfoJSON
			for _, p := range provider.Registry {
				hasKey := aiconfig.EffectiveAPIKey(cfg, p.KeyEnvVar) != ""
				defModel := ""
				if len(p.Models) > 0 {
					defModel = p.Models[0]
				}
				list = append(list, modelInfoJSON{
					Provider:     p.Name,
					DisplayName:  p.DisplayName,
					DefaultModel: defModel,
					HasAPIKey:    hasKey,
					Models:       p.Models,
				})
			}
			out, _ := json.MarshalIndent(list, "", "  ")
			fmt.Fprintln(cmd.OutOrStdout(), string(out))
			return nil
		}

		fmt.Fprintln(cmd.OutOrStdout(), views.RenderModels(provider.Registry, cfg.Provider, func(envVar string) bool {
			return aiconfig.EffectiveAPIKey(cfg, envVar) != ""
		}))
		return nil
	},
}

var aiDoctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Diagnose the AI engine setup",
	Long: `Validate the AI configuration, test provider connectivity, and check
the state of the Architecture Knowledge Graph.`,
	Example: `  # Run full AI health and connectivity check
  gmb ai doctor

  # Output AI doctor diagnostic report as JSON
  gmb ai doctor --json`,
	RunE: func(cmd *cobra.Command, args []string) error {
		asJSON, _ := cmd.Flags().GetBool("json")
		cfg, err := aiconfig.LoadForDir(aiRootDir(cmd), aiFlagConfig(cmd))
		if err != nil {
			return err
		}

		rep := ai_engine.Doctor(cmd.Context(), cfg, aiRootDir(cmd))

		if asJSON {
			type doctorRepJSON struct {
				Provider     string   `json:"provider"`
				Model        string   `json:"model"`
				HasAPIKey    bool     `json:"has_api_key"`
				PingStatus   string   `json:"ping_status"`
				PingDuration float64  `json:"ping_duration_sec"`
				AKGExists    bool     `json:"akg_exists"`
				AKGPath      string   `json:"akg_path"`
				AKGSize      int64    `json:"akg_size_bytes"`
				Problems     []string `json:"problems"`
			}
			dj := doctorRepJSON{
				Provider:     rep.Provider,
				Model:        rep.Model,
				HasAPIKey:    rep.KeySet,
				PingStatus:   rep.PingStatus,
				PingDuration: rep.PingDuration.Seconds(),
				AKGExists:    rep.AKGExists,
				AKGPath:      rep.AKGPath,
				AKGSize:      rep.AKGSize,
				Problems:     rep.Problems,
			}
			out, _ := json.MarshalIndent(dj, "", "  ")
			fmt.Fprintln(cmd.OutOrStdout(), string(out))
			if len(rep.Problems) > 0 {
				return fmt.Errorf("doctor found %d problem(s)", len(rep.Problems))
			}
			return nil
		}

		w := cmd.OutOrStdout()
		fmt.Fprintln(w, views.RenderAIDoctor(rep, ai_engine.MaskAPIKey(cfg.APIKey)))

		if len(rep.Problems) > 0 {
			return fmt.Errorf("doctor found %d problem(s)", len(rep.Problems))
		}
		return nil
	},
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
	aiSessionsCmd.Flags().Bool("json", false, "Emit machine-readable JSON session list")

	aiModelsCmd.Flags().Bool("json", false, "Emit machine-readable JSON provider and model list")
	aiDoctorCmd.Flags().Bool("json", false, "Emit machine-readable JSON diagnostic report")

	_ = aiCmd.RegisterFlagCompletionFunc("tools", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return []string{"system", "akg", "code", "diagram"}, cobra.ShellCompDirectiveNoFileComp
	})
	_ = aiConfigureCmd.RegisterFlagCompletionFunc("scope", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return []string{"global", "project"}, cobra.ShellCompDirectiveNoFileComp
	})

	aiCmd.AddCommand(aiChatCmd)
	aiCmd.AddCommand(aiConfigureCmd)
	aiCmd.AddCommand(aiModelsCmd)
	aiCmd.AddCommand(aiDoctorCmd)
	aiCmd.AddCommand(aiSessionsCmd)

	rootCmd.AddCommand(aiCmd)
}
