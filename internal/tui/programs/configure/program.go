// Package configure renders the interactive "gmb ai configure" wizard as a
// Huh form. It only collects values; persisting and validating them stays in
// cmd/ai.go so the flag-based path and the interactive path share one save
// routine.
package configure

import (
	"fmt"
	"io"

	"github.com/Syamchand123/GlassMarble/internal/ai_engine/aiconfig"
	"github.com/Syamchand123/GlassMarble/internal/ai_engine/provider"
	"github.com/Syamchand123/GlassMarble/internal/tui"
	"github.com/charmbracelet/huh"
)

// Result carries the values the form collected.
type Result struct {
	Provider string
	APIKey   string
	Model    string
	BaseURL  string
}

// Run renders the wizard. registry is the supported-provider list, current
// carries any existing configuration so the form pre-fills defaults. in/out
// are wired to the command's writers so the form respects Cobra redirection.
func Run(registry []provider.Meta, current *aiconfig.Config, in io.Reader, out io.Writer) (*Result, error) {
	var (
		name    string
		apiKey  string
		model   string
		baseURL string
	)

	options := make([]huh.Option[string], 0, len(registry))
	for _, m := range registry {
		label := m.Name
		if m.Description != "" {
			label = fmt.Sprintf("%s (%s)", m.Name, m.Description)
		}
		options = append(options, huh.NewOption(label, m.Name))
	}
	if current.Provider != "" {
		name = current.Provider
	} else if len(options) > 0 {
		name = options[0].Value
	}
	apiKey = current.APIKey
	model = current.Model
	baseURL = current.BaseURL

	meta, _ := provider.Get(name)
	if model == "" && len(meta.Models) > 0 {
		model = meta.Models[0]
	}
	if baseURL == "" {
		baseURL = meta.DefaultBaseURL
	}

	keyGroup := huh.NewGroup(
		huh.NewInput().
			Title("API key").
			Description("Stored in the config file with 0600 permissions; never logged.").
			EchoMode(huh.EchoModePassword).
			Value(&apiKey),
	).WithHideFunc(func() bool {
		m, ok := provider.Get(name)
		return !ok || !m.RequiresKey
	})

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Provider").
				Options(options...).
				Value(&name),
		),
		keyGroup,
		huh.NewGroup(
			huh.NewInput().
				Title("Model").
				Description("Leave empty to use the provider default.").
				Value(&model),
			huh.NewInput().
				Title("Base URL").
				Description("Leave empty to use the provider default.").
				Value(&baseURL),
		),
	).
		WithInput(in).
		WithOutput(out).
		WithTheme(tui.HuhTheme())

	if err := form.Run(); err != nil {
		return nil, err
	}

	meta, _ = provider.Get(name)
	if model == "" && len(meta.Models) > 0 {
		model = meta.Models[0]
	}
	if baseURL == "" {
		baseURL = meta.DefaultBaseURL
	}

	return &Result{Provider: name, APIKey: apiKey, Model: model, BaseURL: baseURL}, nil
}
