package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Syamchand123/GlassMarble/internal/ai_engine"
	"github.com/Syamchand123/GlassMarble/internal/ai_engine/aiconfig"
	"github.com/Syamchand123/GlassMarble/internal/ai_engine/akgbridge"
	"github.com/Syamchand123/GlassMarble/internal/ai_engine/provider"
	"github.com/Syamchand123/GlassMarble/internal/developer_memory"
)

var whyCmd = &cobra.Command{
	Use:   "why [question]",
	Short: "Ask a grounded architecture question (e.g. 'why is Redis used?')",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		question := args[0]
		rootDir := "." // Defaulting to current dir for CLI

		cfg, err := aiconfig.Load(aiconfig.Config{})
		if err != nil {
			return fmt.Errorf("failed to load AI config: %w", err)
		}
		if cfg == nil {
			return fmt.Errorf("AI is not configured. Run 'gmb ai configure'")
		}

		engine, err := ai_engine.New(cfg, rootDir)
		if err != nil {
			return err
		}

		bridge := akgbridge.New(rootDir)
		snap, err := bridge.Snapshot()
		if err != nil {
			return fmt.Errorf("no AKG snapshot available: %w", err)
		}

		memStore := developer_memory.NewMemoryStore(rootDir)
		mem, _ := memStore.LoadMemory() // It's okay if memory is partial or missing
		if mem == nil {
			mem = &developer_memory.DeveloperMemory{}
		}

		// 1. Retrieve Evidence
		fmt.Println("Retrieving architectural evidence...")
		retriever := ai_engine.NewEvidenceRetriever(snap, mem)
		ctxData := retriever.RetrieveForQuestion(question, 10)

		// 2. Build Grounded Prompt
		groundedPrompt := ctxData.BuildPrompt()

		// 3. Query LLM
		fmt.Println("Querying AI Architect...")
		resp, err := engine.Provider.Complete(context.Background(), provider.Request{
			Model:           cfg.Model,
			System:          ai_engine.SystemPrompt,
			Messages:        []provider.Message{{Role: provider.RoleUser, Content: groundedPrompt}},
			Temperature:     cfg.Temperature,
			MaxOutputTokens: cfg.MaxOutputTokens,
		})
		if err != nil {
			return fmt.Errorf("AI request failed: %w", err)
		}

		fmt.Println("\n=== GlassMarble Architect ===")
		fmt.Println(resp.Text)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(whyCmd)
}
