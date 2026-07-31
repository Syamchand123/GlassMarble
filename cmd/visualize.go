package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/terminal"
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

		spinner := terminal.NewSpinner()

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
			OnProgress: func(stage, detail string) {
				msg := stage
				if detail != "" {
					msg += " " + detail
				}
				spinner.Start(msg)
			},
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

		// Generate Diagram Markup (Marble)
		markup, err := coordinator.ProjectDiagram(diagType, opts)
		spinner.Stop(fmt.Sprintf("Done in %.1fs", time.Since(start).Seconds()))
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

	rootCmd.AddCommand(visualizeCmd)
}
