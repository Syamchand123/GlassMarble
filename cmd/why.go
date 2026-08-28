package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Syamchand123/GlassMarble/internal/ai_engine"
	"github.com/Syamchand123/GlassMarble/internal/ai_engine/aiconfig"
	"github.com/Syamchand123/GlassMarble/internal/ai_engine/provider"
	producterrs "github.com/Syamchand123/GlassMarble/internal/product/errors"
	"github.com/Syamchand123/GlassMarble/internal/tui"
)

type whyJSON struct {
	Question  string   `json:"question"`
	Answer    string   `json:"answer"`
	Model     string   `json:"model"`
	Provider  string   `json:"provider"`
	Citations []string `json:"citations,omitempty"`
}

var whyCmd = &cobra.Command{
	Use:     "why [question]",
	GroupID: GroupAI.ID,
	Short:   "Ask a grounded architecture question (e.g. 'why is Redis used?')",
	Long: `Queries the GlassMarble AI Architect with strict grounding in the
Architecture Knowledge Graph and developer memory evidence.`,
	Example: `  # Ask why a specific technology or pattern is used
  gmb why "why was Redis selected for session storage?"

  # Ask why a service dependency exists
  gmb why "why does AuthService depend on UserStore?"

  # Ask why a technology or component is used in this repository
  gmb why "why is tree-sitter used for parsing?"

  # Output grounded answer as JSON
  gmb why "why was the v2 API introduced?" --json`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		question := args[0]
		rootDir := aiRootDir(cmd)
		asJSON, _ := cmd.Flags().GetBool("json")

		cfg, err := aiconfig.LoadForDir(rootDir, aiconfig.Config{})
		if err != nil {
			return fmt.Errorf("failed to load AI config: %w — try 'gmb ai configure'", err)
		}
		if cfg == nil || cfg.APIKey == "" {
			return producterrs.Tagged("AI is not configured — try 'gmb ai configure' or set GLASSMARBLE_AI_API_KEY", producterrs.ErrValidation)
		}

		engine, err := ai_engine.New(cfg, rootDir)
		if err != nil {
			return err
		}

		// 1. Retrieve Deterministic Evidence
		if !asJSON {
			fmt.Println("Retrieving architectural evidence...")
		}
		retriever := ai_engine.NewRetriever(rootDir)
		ctxData := retriever.RetrieveForQuestion(question, ai_engine.RetrieveOptions{
			TopK:      25,
			MaxTokens: 3500,
		})

		if ctxData == nil || ctxData.Empty() {
			return producterrs.Tagged(fmt.Sprintf("no architectural evidence found for %q — try 'gmb analyze' first", question), producterrs.ErrEmptySubgraph)
		}
		promptContent := ctxData.BuildPrompt()

		// 2. Query AI Architect (with tool capabilities enabled)
		if !asJSON {
			fmt.Println("Querying AI Architect...")
		}

		ctx := cmd.Context()
		if cfg.TimeoutSec > 0 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, time.Duration(cfg.TimeoutSec)*time.Second)
			defer cancel()
		}

		// Try Agentic tool execution first to allow source inspection and memory queries
		agentOpts := ai_engine.AgentOptions{
			EnableTools: true,
			MaxTurns:    4,
		}
		res, agentErr := engine.AskAgent(ctx, promptContent, agentOpts)

		var answerText string
		if agentErr == nil && res != nil && strings.TrimSpace(res.Text) != "" {
			answerText = res.Text
		} else {
			// Fallback to direct completion if agent encountered an issue
			resp, compErr := engine.Provider.Complete(ctx, provider.Request{
				Model:           cfg.Model,
				System:          ai_engine.GroundedSystemPrompt,
				Messages:        []provider.Message{{Role: provider.RoleUser, Content: promptContent}},
				Temperature:     cfg.Temperature,
				MaxOutputTokens: cfg.MaxOutputTokens,
			})
			if compErr != nil {
				if agentErr != nil {
					return fmt.Errorf("AI request failed: %w", agentErr)
				}
				return fmt.Errorf("AI request failed: %w", compErr)
			}
			answerText = resp.Text
		}

		citations := []string{}
		if ctxData != nil {
			citations = ctxData.Citations()
		}

		if asJSON {
			out, _ := json.MarshalIndent(whyJSON{
				Question:  question,
				Answer:    answerText,
				Model:     cfg.Model,
				Provider:  cfg.Provider,
				Citations: citations,
			}, "", "  ")
			fmt.Println(string(out))
			return nil
		}

		fmt.Println("\n" + tui.StyleH2.Render("=== GlassMarble Architect ==="))
		fmt.Println(answerText)

		if len(citations) > 0 {
			fmt.Println("\n" + tui.StyleMuted.Render("Evidence Citations: "+strings.Join(citations, ", ")))
		}

		return nil
	},
}

func init() {
	whyCmd.Flags().Bool("json", false, "Emit machine-readable JSON output")
	rootCmd.AddCommand(whyCmd)
}
