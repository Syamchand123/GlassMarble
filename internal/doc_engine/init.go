// Package doc_engine — init.go
// Interactive Charm Huh scaffolding for `gmb doc init`.
package doc_engine

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/doc_engine/config"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/renderer"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/storage"
	"github.com/Syamchand123/GlassMarble/internal/tui"
	"github.com/charmbracelet/huh"
	"gopkg.in/yaml.v3"
)

// Init scaffolds a new managed living document interactively or non-interactively.
func Init(repoRoot string, opts InitOptions) error {
	out := opts.Out
	if out == nil {
		out = os.Stdout
	}

	if opts.TargetPath == "" {
		return fmt.Errorf("doc_engine: target path is required")
	}

	// Clean target path relative to repoRoot
	targetPath := filepath.Clean(opts.TargetPath)
	if filepath.IsAbs(targetPath) {
		rel, err := filepath.Rel(repoRoot, targetPath)
		if err == nil {
			targetPath = rel
		}
	}
	targetPath = filepath.ToSlash(targetPath)

	docID := opts.DocID
	if docID == "" {
		docID = deriveDocID(targetPath)
	}

	title := opts.Title
	purpose := opts.Purpose
	audience := opts.Audience
	archetype := opts.Archetype
	scopePaths := opts.ScopePaths
	entryPoints := opts.EntryPoints

	// Auto-detect candidate scope from target path
	detectedCandidate := detectScopeCandidate(targetPath)
	if len(scopePaths) == 0 && detectedCandidate != "" {
		scopePaths = []string{detectedCandidate}
	}

	if opts.Interactive {
		scopeInput := strings.Join(scopePaths, ", ")
		entryPointsInput := strings.Join(entryPoints, ", ")
		if archetype == "" {
			archetype = "module"
		}
		if title == "" {
			title = strings.Title(strings.ReplaceAll(docID, "-", " "))
		}
		if audience == "" {
			audience = "Senior contributors and on-call engineers"
		}
		if purpose == "" {
			purpose = fmt.Sprintf("Technical reference and architecture guide for %s", docID)
		}

		theme := tui.HuhTheme()

		form := huh.NewForm(
			huh.NewGroup(
				huh.NewInput().
					Title("Document Title").
					Value(&title),
				huh.NewInput().
					Title("Purpose (One sentence describing intent)").
					Value(&purpose),
				huh.NewInput().
					Title("Primary Audience").
					Value(&audience),
				huh.NewSelect[string]().
					Title("Document Archetype").
					Options(
						huh.NewOption("module (package & exported interface reference)", "module"),
						huh.NewOption("architecture (system topology & data flow)", "architecture"),
						huh.NewOption("runbook (production ops & incident triage)", "runbook"),
						huh.NewOption("api (REST/RPC endpoint & schema reference)", "api"),
						huh.NewOption("security (threat model & cryptographic boundaries)", "security"),
						huh.NewOption("config (environment variables & flags directory)", "config"),
						huh.NewOption("database (schemas, relationships & migrations)", "database"),
						huh.NewOption("onboarding (new contributor code tour)", "onboarding"),
						huh.NewOption("migration (breaking changes & version upgrade guide)", "migration"),
						huh.NewOption("adr (architectural decision record)", "adr"),
					).
					Value(&archetype),
				huh.NewInput().
					Title("Code Scope Paths (comma-separated globs)").
					Value(&scopeInput),
				huh.NewInput().
					Title("Entry Points for call-graph/sequence diagrams (comma-separated FQNs, e.g. \"path/to/file.go::Symbol\" — optional, leave blank to skip diagrams)").
					Value(&entryPointsInput),
			),
		).WithTheme(theme)

		if err := form.Run(); err != nil {
			return fmt.Errorf("doc init cancelled: %w", err)
		}

		// Re-split scope
		scopePaths = nil
		for _, p := range strings.Split(scopeInput, ",") {
			p = strings.TrimSpace(p)
			if p != "" {
				scopePaths = append(scopePaths, p)
			}
		}
		entryPoints = nil
		for _, e := range strings.Split(entryPointsInput, ",") {
			e = strings.TrimSpace(e)
			if e != "" {
				entryPoints = append(entryPoints, e)
			}
		}
	}

	if title == "" {
		title = strings.Title(strings.ReplaceAll(docID, "-", " "))
	}
	if archetype == "" {
		archetype = "module"
	}
	if len(scopePaths) == 0 {
		scopePaths = []string{"internal/**"}
	}

	spec := config.DocSpec{
		ID:         docID,
		TargetPath: targetPath,
		Title:      title,
		Purpose:    purpose,
		Audience:   audience,
		Archetype:  archetype,
		Mode:       config.ModeManagedSections,
		Scope: config.ScopeRule{
			Paths:       scopePaths,
			EntryPoints: entryPoints,
		},
	}

	// Apply Archetype defaults
	if err := renderer.ApplyArchetype(&spec); err != nil {
		return fmt.Errorf("applying archetype: %w", err)
	}

	// Scaffold target markdown file
	absTarget := filepath.Join(repoRoot, targetPath)
	var scaffoldContent string
	if existingBytes, err := os.ReadFile(absTarget); err == nil && len(existingBytes) > 0 {
		existing := string(existingBytes)
		var sb strings.Builder
		sb.WriteString(existing)
		if !strings.HasSuffix(existing, "\n") {
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
		for _, sec := range spec.Sections {
			beginAnchor := fmt.Sprintf("<!-- gmb:begin:%s -->", sec.ID)
			if !strings.Contains(existing, beginAnchor) {
				sb.WriteString(fmt.Sprintf("## %s\n\n", sec.Title))
				sb.WriteString(fmt.Sprintf("<!-- gmb:begin:%s -->\n<!-- gmb:end:%s -->\n\n", sec.ID, sec.ID))
			}
		}
		scaffoldContent = sb.String()
	} else {
		scaffoldContent = generateScaffoldMarkdown(&spec)
	}

	if _, err := storage.AtomicWriteFile(absTarget, []byte(scaffoldContent)); err != nil {
		return fmt.Errorf("writing scaffolded document %s: %w", targetPath, err)
	}

	// Update .glassmarble/docs.yaml
	if err := updateDocsYAML(repoRoot, spec); err != nil {
		return fmt.Errorf("updating docs.yaml: %w", err)
	}

	fmt.Fprintf(out, "✓ Scaffolded %s with pre-populated frontmatter\n", targetPath)
	fmt.Fprintf(out, "✓ Configured archetype %q with %d managed section(s)\n", archetype, len(spec.Sections))
	fmt.Fprintf(out, "✓ Added %q specification to .glassmarble/docs.yaml\n", spec.ID)
	fmt.Fprintf(out, "✓ Run 'gmb doc --doc %s' to generate initial content\n", spec.ID)

	return nil
}

func detectScopeCandidate(targetPath string) string {
	base := filepath.Base(targetPath)
	name := strings.TrimSuffix(base, filepath.Ext(base))
	return fmt.Sprintf("internal/%s/**", name)
}

func generateScaffoldMarkdown(spec *config.DocSpec) string {
	var sb strings.Builder
	sb.WriteString("---\n")
	sb.WriteString(fmt.Sprintf("id: %s\n", spec.ID))
	sb.WriteString(fmt.Sprintf("title: %s\n", spec.Title))
	sb.WriteString(fmt.Sprintf("archetype: %s\n", spec.Archetype))
	sb.WriteString(fmt.Sprintf("purpose: %q\n", spec.Purpose))
	sb.WriteString(fmt.Sprintf("audience: %q\n", spec.Audience))
	sb.WriteString("---\n\n")

	sb.WriteString(fmt.Sprintf("# %s\n\n", spec.Title))
	if spec.Purpose != "" {
		sb.WriteString(fmt.Sprintf("%s\n\n", spec.Purpose))
	}

	for _, sec := range spec.Sections {
		sb.WriteString(fmt.Sprintf("## %s\n\n", sec.Title))
		if sec.Instruction != "" {
			sb.WriteString(fmt.Sprintf("> %s\n\n", sec.Instruction))
		}
		if sec.Managed {
			sb.WriteString(fmt.Sprintf("<!-- gmb:begin:%s -->\n", sec.ID))
			sb.WriteString(fmt.Sprintf("<!-- gmb:end:%s -->\n\n", sec.ID))
		}
	}

	return sb.String()
}

func updateDocsYAML(repoRoot string, newDoc config.DocSpec) error {
	cfgPath := config.DocsConfigPath(repoRoot)

	cfg, err := config.LoadDocsConfig(repoRoot)
	if err != nil || cfg == nil {
		// Create fresh default config if none exists
		cfg = &config.DocsConfig{
			Version:        config.CurrentSchemaVersion,
			DocsDir:        "docs",
			TargetPlatform: "github_flat",
		}
	}
	if cfg.DocsDir == "" {
		cfg.DocsDir = "docs"
	}
	if cfg.TargetPlatform == "" {
		cfg.TargetPlatform = "github_flat"
	}

	// Update existing doc or append new one
	found := false
	for i, d := range cfg.Documents {
		if d.ID == newDoc.ID || d.TargetPath == newDoc.TargetPath {
			cfg.Documents[i] = newDoc
			found = true
			break
		}
	}
	if !found {
		cfg.Documents = append(cfg.Documents, newDoc)
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}

	_, err = storage.AtomicWriteFile(cfgPath, data)
	return err
}

func deriveDocID(targetPath string) string {
	base := filepath.Base(targetPath)
	return strings.ToLower(strings.TrimSuffix(base, filepath.Ext(base)))
}
