package cmd

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/tui"
	visualizeprog "github.com/Syamchand123/GlassMarble/internal/tui/programs/visualize"
	"github.com/Syamchand123/GlassMarble/internal/visualization_engine"
	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/types"
	"github.com/spf13/cobra"
)

var (
	entryPointID  string
	maxDepth      int
	includeUnused bool
	storagePath   string
	saveFile      string
	formatFlag    string
	scopeFlag     string
	outputFlag    string
	summaryFlag   bool
	pagerankFlag  bool
	communityFlag bool
	sccFlag       bool
	renderFlag    string
)

var visualizeCmd = &cobra.Command{
	Use:   "visualize [diagram_type]",
	Short: "Generate visual architecture diagrams (marbles) from the AKG",
	Long:  `Queries the W3C RDF Turtle database file (.ttl) and projects the graph layout into Mermaid.js format.`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		diagName := args[0]
		var diagType types.DiagramType

		switch diagName {
		// 14 UML Diagrams
		case "class":
			diagType = types.UMLClass
		case "object":
			diagType = types.UMLObject
		case "component":
			diagType = types.UMLComponent
		case "deployment":
			diagType = types.UMLDeployment
		case "package":
			diagType = types.UMLPackage
		case "composite":
			diagType = types.UMLComposite
		case "profile":
			diagType = types.UMLProfile
		case "usecase":
			diagType = types.UMLUsecase
		case "activity":
			diagType = types.UMLActivity
		case "state":
			diagType = types.UMLState
		case "sequence":
			diagType = types.UMLSequence
		case "communication":
			diagType = types.UMLCommunication
		case "interaction":
			diagType = types.UMLInteractionOverview
		case "timing":
			diagType = types.UMLTiming

		// 7 C4 Diagrams
		case "c4context":
			diagType = types.C4Context
		case "c4container":
			diagType = types.C4Container
		case "c4component":
			diagType = types.C4Component
		case "c4code":
			diagType = types.C4Code
		case "c4landscape":
			diagType = types.C4Landscape
		case "c4dynamic":
			diagType = types.C4Dynamic
		case "c4deployment":
			diagType = types.C4Deployment

		// Specialized
		case "er":
			diagType = types.ERDiagram
		case "dataflow":
			diagType = types.DataFlow
		case "mindmap":
			diagType = types.Mindmap
		case "flowchart":
			diagType = types.Flowchart

		// Track G
		case "dependency":
			diagType = types.DependencyGraph
		case "hotspot":
			diagType = types.HotspotComplexity
		case "callgraph":
			diagType = types.CallGraph
		case "layered":
			diagType = types.LayeredArchitecture
		case "impact":
			diagType = types.ChangeImpact
		case "infrastructure":
			diagType = types.Infrastructure

		default:
			return fmt.Errorf("unsupported diagram type '%s'", diagName)
		}

		// Ensure we require entry point for sequence diagrams
		if diagType == types.UMLSequence && entryPointID == "" {
			return fmt.Errorf("entry point ID (--entry) is mandatory for UML Sequence diagrams")
		}

		// Resolve .ttl path
		ttlPath := filepath.Join(storagePath, ".glassmarble", "akg_state.ttl")
		if _, err := os.Stat(ttlPath); os.IsNotExist(err) {
			return fmt.Errorf("active AKG database not found at %s. Please run analysis first", ttlPath)
		}

		// Initialize 3-Tier Visualizer
		coordinator := visualization_engine.NewEngineCoordinator(ttlPath)
		start := time.Now()

		scope, scopePath, err := parseScope(scopeFlag)
		if err != nil {
			return err
		}

		opts := types.QueryOptions{
			EntryPointID:  entryPointID,
			MaxDepth:      maxDepth,
			IncludeUnused: includeUnused,
			Format:        formatFlag,
			Scope:         scope,
			ScopePath:     scopePath,
		}

		if cmd.Flags().Changed("pagerank") || cmd.Flags().Changed("community") || cmd.Flags().Changed("scc") {
			opts.PipelineCfg = &types.PipelineConfig{
				EnableMetrics:     true,
				EnableCommunities: true,
				EnableSCC:         true,
			}
			if cmd.Flags().Changed("pagerank") {
				opts.PipelineCfg.EnableMetrics = pagerankFlag
			}
			if cmd.Flags().Changed("community") {
				opts.PipelineCfg.EnableCommunities = communityFlag
			}
			if cmd.Flags().Changed("scc") {
				opts.PipelineCfg.EnableSCC = sccFlag
			}
		}

		var graphSummary *types.GraphSummary
		if cmd.Flags().Changed("summary") && summaryFlag {
			opts.OnSummary = func(s *types.GraphSummary) {
				graphSummary = s
			}
		}

		if renderFlag == "" && tui.IsInteractive(cmd.InOrStdin(), cmd.OutOrStdout()) {
			return visualizeprog.Run(visualizeprog.Config{
				DiagType:    diagType,
				TTLPath:     ttlPath,
				Opts:        opts,
				SaveFile:    saveFile,
				OutputFlag:  outputFlag,
				FormatFlag:  formatFlag,
				StoragePath: storagePath,
				SummaryFlag: cmd.Flags().Changed("summary") && summaryFlag,
				In:          cmd.InOrStdin(),
				Out:         cmd.OutOrStdout(),
			})
		}

		// Non-interactive fallback: report pipeline stages to stderr (same
		// channel the old terminal spinner used) so buffered test writers stay
		// byte-compatible.
		opts.OnProgress = func(stage, detail string) {
			msg := stage
			if detail != "" {
				msg += " " + detail
			}
			fmt.Fprintf(os.Stderr, "%s...\n", msg)
		}

		// Generate Diagram Markup (Marble)
		markup, err := coordinator.ProjectDiagram(diagType, opts)
		fmt.Fprintf(os.Stderr, "Done in %.1fs\n", time.Since(start).Seconds())
		if err != nil {
			return fmt.Errorf("failed to generate diagram: %w", err)
		}

		// Print summary before diagram if requested
		if cmd.Flags().Changed("summary") && summaryFlag && graphSummary != nil {
			s := graphSummary
			fmt.Fprintf(cmd.OutOrStdout(), "=== Graph Summary ===\n")
			fmt.Fprintf(cmd.OutOrStdout(), "Nodes: %d | Edges: %d | Density: %.4f\n", s.NodeCount, s.EdgeCount, s.Density)
			fmt.Fprintf(cmd.OutOrStdout(), "Diameter: %d | Avg Path Length: %.2f\n", s.Diameter, s.AvgPathLength)
			fmt.Fprintf(cmd.OutOrStdout(), "Clusters: %d | Largest SCC: %d | God Objects: %d\n", s.ClusterCount, s.LargestSCCSize, s.GodObjectCount)
			fmt.Fprintf(cmd.OutOrStdout(), "========================\n")
		}

		// Render the diagram to an image (SVG or PNG) when --render is set.
		// Mermaid markup is sent to a renderer: Kroki (network) by default,
		// with mermaid-cli (`mmdc`) as a local fallback.
		if renderFlag != "" {
			if err := renderMermaidToImage(markup, renderFlag, formatFlag); err != nil {
				return err
			}
			return nil
		}

		// Stream or save diagram markup (Marble)
		if saveFile != "" {
			fileName := saveFile
			if !strings.HasSuffix(fileName, ".md") {
				fileName += ".md"
			}

			marblesDir := filepath.Join(storagePath, ".glassmarble", "marbles")
			if err := os.MkdirAll(marblesDir, 0755); err != nil {
				return fmt.Errorf("failed to create marbles directory: %w", err)
			}

			filePath := filepath.Join(marblesDir, fileName)
			langTag := "mermaid"
			if strings.EqualFold(formatFlag, "plantuml") {
				langTag = "plantuml"
			} else if strings.EqualFold(formatFlag, "dot") || strings.EqualFold(formatFlag, "graphviz") {
				langTag = "dot"
			}
			mdContent := fmt.Sprintf("```%s\n%s\n```\n", langTag, markup)
			if err := os.WriteFile(filePath, []byte(mdContent), 0644); err != nil {
				return fmt.Errorf("failed to save marble file: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Marble saved successfully to %s\n", filePath)
		} else if cmd.Flags().Changed("output") && outputFlag != "" {
			if err := os.WriteFile(outputFlag, []byte(markup), 0644); err != nil {
				return fmt.Errorf("failed to write output file: %w", err)
			}
		} else {
			// Stream to CLI output writer (supports testing redirection)
			fmt.Fprint(cmd.OutOrStdout(), markup)
		}
		return nil
	},
}

func parseScope(scopeStr string) (types.ScopeLevel, string, error) {
	if scopeStr == "" || scopeStr == "global" {
		return types.ScopeGlobal, "", nil
	}
	if strings.HasPrefix(scopeStr, "folder:") {
		path := strings.TrimPrefix(scopeStr, "folder:")
		if path == "" {
			return types.ScopeGlobal, "", fmt.Errorf("folder scope requires a non-empty path")
		}
		return types.ScopeFolder, path, nil
	}
	if strings.HasPrefix(scopeStr, "file:") {
		path := strings.TrimPrefix(scopeStr, "file:")
		if path == "" {
			return types.ScopeGlobal, "", fmt.Errorf("file scope requires a non-empty path")
		}
		return types.ScopeFile, path, nil
	}
	return types.ScopeGlobal, "", fmt.Errorf("invalid scope: %q (valid values: global, folder:path, file:path)", scopeStr)
}

// renderMermaidToImage converts diagram markup to an SVG or PNG image. The
// markup language (mermaid/plantuml/dot) is selected by formatFlag; the target
// format is derived from the --render file extension (.svg or .png). It tries
// the Kroki rendering service first, then a locally-installed mermaid-cli
// (`mmdc`). When both are unavailable the raw markup is saved next to the
// target path and a descriptive error is returned.
func renderMermaidToImage(markup, targetPath, formatFlag string) error {
	return renderMermaidToImageWithEndpoint(markup, targetPath, formatFlag, "https://kroki.io")
}

// RenderMermaidToImageForTest is the testable entry point for the render
// pipeline with an injectable Kroki base URL.
func RenderMermaidToImageForTest(markup, targetPath, formatFlag, krokiBase string) error {
	return renderMermaidToImageWithEndpoint(markup, targetPath, formatFlag, krokiBase)
}

func renderMermaidToImageWithEndpoint(markup, targetPath, formatFlag, krokiBase string) error {
	ext := strings.ToLower(filepath.Ext(targetPath))
	switch ext {
	case ".svg", ".png":
	default:
		return fmt.Errorf("unsupported render format %q (use .svg or .png)", ext)
	}
	imgFormat := strings.TrimPrefix(ext, ".")

	// Kroki diagram-type keyword per markup language.
	krokiType := "mermaid"
	switch {
	case strings.EqualFold(formatFlag, "plantuml"):
		krokiType = "plantuml"
	case strings.EqualFold(formatFlag, "dot") || strings.EqualFold(formatFlag, "graphviz"):
		krokiType = "graphviz"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	endpoint := krokiBase + "/" + krokiType + "/" + imgFormat
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(markup))
	if err != nil {
		return fmt.Errorf("failed to build render request: %w", err)
	}
	req.Header.Set("Content-Type", "text/plain; charset=utf-8")
	resp, err := http.DefaultClient.Do(req)
	if err == nil {
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			var buf bytes.Buffer
			if _, cerr := buf.ReadFrom(resp.Body); cerr != nil {
				return fmt.Errorf("failed to read rendered image: %w", cerr)
			}
			if err := os.WriteFile(targetPath, buf.Bytes(), 0o644); err != nil {
				return fmt.Errorf("failed to write rendered image: %w", err)
			}
			fmt.Fprintf(os.Stdout, "Rendered %s image to %s (via Kroki)\n", imgFormat, targetPath)
			return nil
		}
	}

	// Fallback: mermaid-cli local renderer.
	mmdc, findErr := exec.LookPath("mmdc")
	if findErr == nil {
		// mermaid-cli accepts .mmd input and --output with an extension.
		cmd := exec.CommandContext(ctx, mmdc, "-i", "-", "-o", targetPath)
		cmd.Stdin = strings.NewReader(markup)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if runErr := cmd.Run(); runErr == nil {
			fmt.Fprintf(os.Stdout, "Rendered %s image to %s (via mermaid-cli)\n", imgFormat, targetPath)
			return nil
		}
		_ = stderr
	}

	// Neither renderer succeeded: persist the markup so nothing is lost.
	markupPath := targetPath + ".txt"
	_ = os.WriteFile(markupPath, []byte(markup), 0o644)
	return fmt.Errorf("no diagram renderer available (Kroki unreachable and mermaid-cli not installed); markup written to %s", markupPath)
}

func ResetVisualizeFlags() {
	entryPointID = ""
	maxDepth = 7
	includeUnused = false
	storagePath = "."
	saveFile = ""
	formatFlag = "mermaid"
	scopeFlag = ""
	outputFlag = ""
	summaryFlag = false
	pagerankFlag = false
	communityFlag = false
	sccFlag = false
	renderFlag = ""
}

func init() {
	visualizeCmd.Flags().StringVar(&entryPointID, "entry", "", "Execution entry point symbol ID (mandatory for sequence diagrams)")
	visualizeCmd.Flags().IntVar(&maxDepth, "depth", 7, "Maximum search depth limit for reachability path walk")
	visualizeCmd.Flags().BoolVar(&includeUnused, "unused", false, "Include unreferenced dead components in the layout")
	visualizeCmd.Flags().StringVar(&storagePath, "dir", ".", "Directory path containing the .glassmarble/ database folder")
	visualizeCmd.Flags().StringVar(&saveFile, "save", "", "Save the diagram to a markdown file inside .glassmarble/marbles/")
	visualizeCmd.Flags().StringVar(&formatFlag, "format", "mermaid", "Output format: mermaid, plantuml, or dot")
	visualizeCmd.Flags().StringVar(&scopeFlag, "scope", "", "Filter layout to specific scope: global (default), folder:path, or file:path")
	visualizeCmd.Flags().StringVar(&outputFlag, "output", "", "Write diagram to file instead of stdout")
	visualizeCmd.Flags().BoolVar(&summaryFlag, "summary", false, "Print graph summary before the diagram")
	visualizeCmd.Flags().BoolVar(&pagerankFlag, "pagerank", false, "Enable PageRank computation")
	visualizeCmd.Flags().BoolVar(&communityFlag, "community", false, "Enable community detection")
	visualizeCmd.Flags().BoolVar(&sccFlag, "scc", false, "Enable strongly connected components analysis")
	visualizeCmd.Flags().StringVar(&renderFlag, "render", "", "Render the diagram to an image file (.svg or .png) via Kroki or mermaid-cli")

	rootCmd.AddCommand(visualizeCmd)
}
