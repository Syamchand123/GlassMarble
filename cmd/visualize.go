package cmd

// GlassMarble Visualize Command (Section 11.1 / Phase 7)
// Exit codes:
//   0: Success
//   1: Validation error (ErrValidation) or invalid scope/format
//   2: Entry point missing (ErrEntryMissing) or entry symbol not found (ErrEntryNotFound)
//   3: Empty subgraph / no nodes matched (ErrEmptySubgraph)
//   4: Render limit / renderer unavailable (ErrRenderLimit) — Truncate to N highest-degree nodes (sets Truncated) — hard abort only via analyze --abort-on-limit

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/product"
	producterrs "github.com/Syamchand123/GlassMarble/internal/product/errors"
	"github.com/Syamchand123/GlassMarble/internal/tui"
	visualizeprog "github.com/Syamchand123/GlassMarble/internal/tui/programs/visualize"
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
	maxNodesFlag  int
	changedFiles  []string
	relativeFlag  bool
	linkLevelFlag string
	themeFlag        string
	directionFlag    string
	includeTestsFlag bool
)

var visualizeCmd = &cobra.Command{
	Use:     "visualize [diagram_type]",
	Aliases: []string{"viz"},
	GroupID: GroupVisualize.ID,
	Short:   "Generate visual architecture diagrams (marbles) from the AKG",
	Long: `Queries the canonical AKG state database (akg.json) and projects the graph layout
into Mermaid.js, PlantUML, or DOT markup format.

Run 'gmb visualize list' to view all 31 supported diagram types.`,
	Example: `  # Generate class diagram in Mermaid format
  gmb visualize class

  # Generate sequence diagram with an entry point symbol
  gmb visualize sequence --entry "cmd/root.go::Execute"

  # Generate C4 container diagram in PlantUML format
  gmb visualize c4container --format plantuml

  # Scope diagram to a specific package directory
  gmb visualize class --scope folder:internal/app

  # List all 31 supported diagram types
  gmb visualize list

  # Output diagram markup and graph summary as JSON
  gmb visualize dependency --json`,
	Args: cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		diagName := args[0]
		if diagName == "list" {
			printDiagramTypesList(cmd)
			return nil
		}
		if diagName == "check" {
			if len(args) < 2 {
				return producterrs.Tagged("usage: gmb visualize check <diagram_type> — try 'gmb visualize list'", producterrs.ErrValidation)
			}
			return printDiagramTypeCheck(cmd, args[1])
		}

		diagType, err := parseDiagramTypeByName(diagName)
		if err != nil {
			return producterrs.Tagged(fmt.Sprintf("unsupported diagram type '%s' — try 'gmb visualize list' to see all 31 supported types", diagName), producterrs.ErrValidation)
		}

		// Ensure we require entry point for sequence diagrams
		if diagType == types.UMLSequence && entryPointID == "" {
			return producterrs.Tagged("entry point ID (--entry) is mandatory for UML Sequence diagrams — try 'gmb visualize sequence --entry <symbol_id>'", producterrs.ErrEntryMissing)
		}

		storagePath = resolveDir(cmd)
		asJSON, _ := cmd.Flags().GetBool("json")

		// Resolve the canonical state path (Phase C: akg.json).
		statePath := filepath.Join(storagePath, ".glassmarble", "akg.json")
		if _, err := os.Stat(statePath); os.IsNotExist(err) {
			if asJSON {
				out, _ := json.MarshalIndent(map[string]string{"error": "no active AKG database"}, "", "  ")
				fmt.Fprintln(cmd.OutOrStdout(), string(out))
				return nil
			}
			return producterrs.Tagged("active AKG database not found — try 'gmb analyze' first", producterrs.ErrEmptySubgraph)
		}

		start := time.Now()

		outputFlag = strings.Trim(outputFlag, "\"'")
		storagePath = strings.Trim(storagePath, "\"'")
		saveFile = strings.Trim(saveFile, "\"'")
		scopeFlag = strings.Trim(scopeFlag, "\"'")

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
			MaxNodes:      maxNodesFlag,
			ChangedFiles:  changedFiles,
			LinkLevel:    linkLevelFlag,
			Theme:        themeFlag,
			Direction:    directionFlag,
			IncludeTests: includeTestsFlag,
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

		if !asJSON && renderFlag == "" && tui.IsInteractive(cmd.InOrStdin(), cmd.OutOrStdout()) {
			return visualizeprog.Run(visualizeprog.Config{
				DiagType:    diagType,
				StatePath:   statePath,
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

		// Non-interactive fallback: report pipeline phases to stderr (suppressed under --json)
		if !asJSON {
			opts.OnProgress = func(step, detail string) {
				msg := step
				if detail != "" {
					msg += " " + detail
				}
				fmt.Fprintf(os.Stderr, "%s...\n", msg)
			}
		}

		// Generate Diagram Markup (Marble) via unified pipeline entry (V-11 / 11.1)
		req := product.BuildDiagramRequest{
			StatePath:   statePath,
			ParseFn:     akg.ParseGraphForQuery,
			DiagramType: diagType,
			Format:      formatFlag,
			Options: product.DiagramOptions{
				Scope:         scope,
				ScopePath:     scopePath,
				Entry:         entryPointID,
				Depth:         maxDepth,
				IncludeUnused: includeUnused,
				MaxNodes:      maxNodesFlag,
				LinkLevel:     linkLevelFlag,
				ChangedFiles:  changedFiles,
				Relative:      relativeFlag,
			},
			OnProgress: opts.OnProgress,
		}
		res, err := product.BuildDiagramEx(req)
		var markup string
		if res != nil {
			markup = res.Markup
			if res.Summary != nil {
				graphSummary = res.Summary
			}
		}
		if !asJSON {
			fmt.Fprintf(os.Stderr, "Done in %.1fs\n", time.Since(start).Seconds())
		}
		if err != nil {
			// C6-6: propagate original error taxonomy; only renderer-unavailable
			// paths are classified as ErrRenderLimit (exit 4). This preserves
			// ErrEmptySubgraph (3), ErrEntryMissing (2), ErrValidation (1).
			return fmt.Errorf("failed to generate diagram: %w", err)
		}

		if asJSON {
			type visualizeJSON struct {
				DiagramType string              `json:"diagram_type"`
				Format      string              `json:"format"`
				Markup      string              `json:"markup"`
				Summary     *types.GraphSummary `json:"summary,omitempty"`
			}
			out, _ := json.MarshalIndent(visualizeJSON{
				DiagramType: string(diagType),
				Format:      formatFlag,
				Markup:      markup,
				Summary:     graphSummary,
			}, "", "  ")
			fmt.Fprintln(cmd.OutOrStdout(), string(out))
			return nil
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
			if dir := filepath.Dir(outputFlag); dir != "" && dir != "." {
				_ = os.MkdirAll(dir, 0755)
			}
			if err := os.WriteFile(outputFlag, []byte(markup), 0644); err != nil {
				return fmt.Errorf("failed to write output file: %w", err)
			}
		} else {
			// Stream to CLI output writer
			fmt.Fprint(cmd.OutOrStdout(), markup)
		}
		return nil
	},
}

func parseDiagramTypeByName(name string) (types.DiagramType, error) {
	switch strings.ToLower(name) {
	// 14 UML Diagrams
	case "class":
		return types.UMLClass, nil
	case "object":
		return types.UMLObject, nil
	case "component":
		return types.UMLComponent, nil
	case "deployment":
		return types.UMLDeployment, nil
	case "package":
		return types.UMLPackage, nil
	case "composite":
		return types.UMLComposite, nil
	case "profile":
		return types.UMLProfile, nil
	case "usecase":
		return types.UMLUsecase, nil
	case "activity":
		return types.UMLActivity, nil
	case "state":
		return types.UMLState, nil
	case "sequence":
		return types.UMLSequence, nil
	case "communication":
		return types.UMLCommunication, nil
	case "interaction":
		return types.UMLInteractionOverview, nil
	case "timing":
		return types.UMLTiming, nil

	// 7 C4 Diagrams
	case "c4context":
		return types.C4Context, nil
	case "c4container":
		return types.C4Container, nil
	case "c4component":
		return types.C4Component, nil
	case "c4code":
		return types.C4Code, nil
	case "c4landscape":
		return types.C4Landscape, nil
	case "c4dynamic":
		return types.C4Dynamic, nil
	case "c4deployment":
		return types.C4Deployment, nil

	// Specialized
	case "er":
		return types.ERDiagram, nil
	case "dataflow":
		return types.DataFlow, nil
	case "mindmap":
		return types.Mindmap, nil
	case "flowchart":
		return types.Flowchart, nil

	// Track G
	case "dependency":
		return types.DependencyGraph, nil
	case "hotspot":
		return types.HotspotComplexity, nil
	case "callgraph":
		return types.CallGraph, nil
	case "layered":
		return types.LayeredArchitecture, nil
	case "impact":
		return types.ChangeImpact, nil
	case "infrastructure":
		return types.Infrastructure, nil
	}
	return "", fmt.Errorf("unknown diagram type %q", name)
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

func renderMermaidToImage(markup, targetPath, formatFlag string) error {
	return renderMermaidToImageWithEndpoint(markup, targetPath, formatFlag, "https://kroki.io")
}

func RenderMermaidToImageForTest(markup, targetPath, formatFlag, krokiBase string) error {
	return renderMermaidToImageWithEndpoint(markup, targetPath, formatFlag, krokiBase)
}

func renderMermaidToImageWithEndpoint(markup, targetPath, formatFlag, krokiBase string) error {
	ext := strings.ToLower(filepath.Ext(targetPath))
	switch ext {
	case ".svg", ".png":
	default:
		return producterrs.Tagged(fmt.Sprintf("unsupported render format %q (use .svg or .png)", ext), producterrs.ErrValidation)
	}
	imgFormat := strings.TrimPrefix(ext, ".")

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
	// C6-5: use a client with a timeout so a hung/firewalled Kroki does not
	// block forever; DefaultClient has no timeout.
	krokiClient := &http.Client{Timeout: 10 * time.Second}
	resp, err := krokiClient.Do(req)
	var krokiErrMsg string
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
		var bodyBuf bytes.Buffer
		_, _ = bodyBuf.ReadFrom(resp.Body)
		if bodyBuf.Len() > 0 {
			krokiErrMsg = strings.TrimSpace(bodyBuf.String())
		} else {
			krokiErrMsg = resp.Status
		}
	} else {
		krokiErrMsg = err.Error()
	}

	mmdc, findErr := exec.LookPath("mmdc")
	if findErr == nil {
		mmdcCmd := exec.CommandContext(ctx, mmdc, "-i", "-", "-o", targetPath)
		mmdcCmd.Stdin = strings.NewReader(markup)
		var stderr bytes.Buffer
		mmdcCmd.Stderr = &stderr
		if runErr := mmdcCmd.Run(); runErr == nil {
			fmt.Fprintf(os.Stdout, "Rendered %s image to %s (via mermaid-cli)\n", imgFormat, targetPath)
			return nil
		}
		// C6-13: include mmdc stderr in the final error instead of discarding.
		if s := strings.TrimSpace(stderr.String()); s != "" {
			krokiErrMsg = fmt.Sprintf("%s; mermaid-cli: %s", krokiErrMsg, s)
		}
	}

	markupPath := targetPath + ".txt"
	_ = os.WriteFile(markupPath, []byte(markup), 0o644)
	if krokiErrMsg != "" {
		return producterrs.Tagged(fmt.Sprintf("no diagram renderer available (Kroki: %s; mermaid-cli not installed or failed); markup written to %s", krokiErrMsg, markupPath), producterrs.ErrRenderLimit)
	}
	return producterrs.Tagged(fmt.Sprintf("no diagram renderer available (Kroki unreachable and mermaid-cli not installed); markup written to %s", markupPath), producterrs.ErrRenderLimit)
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
	maxNodesFlag = 0
	changedFiles = nil
	linkLevelFlag = "architecture"
}

type diagTypeCatalogEntry struct {
	Name     string
	Family   string
	Tier     string
	EntryReq string
	Formats  string
}

var all31DiagramCatalog = []diagTypeCatalogEntry{
	{"class", "UML", "T1 Canonical", "optional", "mermaid, plantuml, dot"},
	{"object", "UML", "T1 Canonical", "required", "mermaid, plantuml, dot"},
	{"component", "UML", "T1 Canonical", "optional", "mermaid, plantuml, dot"},
	{"deployment", "UML", "T1 Canonical", "optional", "mermaid, plantuml, dot"},
	{"package", "UML", "T1 Canonical", "optional", "mermaid, plantuml, dot"},
	{"composite", "UML", "T1 Canonical", "required", "mermaid, plantuml, dot"},
	{"profile", "UML", "T1 Canonical", "optional", "mermaid, plantuml, dot"},
	{"usecase", "UML", "T1 Canonical", "optional", "mermaid, plantuml, dot"},
	{"activity", "UML", "T1 Canonical", "required", "mermaid, plantuml, dot"},
	{"state", "UML", "T1 Canonical", "required", "mermaid, plantuml, dot"},
	{"sequence", "UML", "T1 Canonical", "required", "mermaid, plantuml, dot"},
	{"communication", "UML", "T1 Canonical", "required", "mermaid, plantuml, dot"},
	{"interaction", "UML", "T1 Canonical", "required", "mermaid, plantuml, dot"},
	{"timing", "UML", "T1 Canonical", "required", "mermaid, plantuml, dot"},

	{"c4context", "C4", "T1 Canonical", "optional", "mermaid, plantuml, dot"},
	{"c4container", "C4", "T1 Canonical", "optional", "mermaid, plantuml, dot"},
	{"c4component", "C4", "T1 Canonical", "optional", "mermaid, plantuml, dot"},
	{"c4code", "C4", "T1 Canonical", "optional", "mermaid, plantuml, dot"},
	{"c4landscape", "C4", "T1 Canonical", "optional", "mermaid, plantuml, dot"},
	{"c4dynamic", "C4", "T1 Canonical", "required", "mermaid, plantuml, dot"},
	{"c4deployment", "C4", "T1 Canonical", "optional", "mermaid, plantuml, dot"},

	{"er", "Specialized", "T1 Canonical", "optional", "mermaid, plantuml, dot"},
	{"dataflow", "Specialized", "T1 Canonical", "optional", "mermaid, plantuml, dot"},
	{"mindmap", "Specialized", "T1 Canonical", "optional", "mermaid, plantuml, dot"},
	{"flowchart", "Specialized", "T1 Canonical", "required", "mermaid, plantuml, dot"},
	{"dependency", "Analysis", "T1 Canonical", "optional", "mermaid, plantuml, dot"},
	{"hotspot", "Analysis", "T1 Canonical", "optional", "mermaid, plantuml, dot"},
	{"callgraph", "Analysis", "T1 Canonical", "optional", "mermaid, plantuml, dot"},
	{"layered", "Analysis", "T1 Canonical", "optional", "mermaid, plantuml, dot"},
	{"impact", "Analysis", "T1 Canonical", "changed-files", "mermaid, plantuml, dot"},
	{"infrastructure", "Analysis", "T1 Canonical", "optional", "mermaid, plantuml, dot"},
}

func printDiagramTypesList(cmd *cobra.Command) {
	out := cmd.OutOrStdout()
	fmt.Fprintln(out, "Supported GlassMarble Diagram Types (31 total):")
	fmt.Fprintln(out, "")
	fmt.Fprintf(out, "%-16s %-12s %-14s %-12s %-24s\n", "Type", "Family", "Tier", "Entry Req", "Formats")
	fmt.Fprintln(out, strings.Repeat("-", 80))
	for _, entry := range all31DiagramCatalog {
		fmt.Fprintf(out, "%-16s %-12s %-14s %-12s %-24s\n", entry.Name, entry.Family, entry.Tier, entry.EntryReq, entry.Formats)
	}
}

func printDiagramTypeCheck(cmd *cobra.Command, name string) error {
	out := cmd.OutOrStdout()
	dt, err := parseDiagramTypeByName(name)
	if err != nil {
		return producterrs.Tagged(fmt.Sprintf("unknown diagram type %q. Use 'gmb visualize list' to view valid types", name), producterrs.ErrValidation)
	}

	entryReq := "optional"
	for _, entry := range all31DiagramCatalog {
		if strings.EqualFold(entry.Name, name) {
			entryReq = entry.EntryReq
			break
		}
	}

	statePath := filepath.Join(storagePath, ".glassmarble", "akg.json")
	nodeCount := 0
	edgeCount := 0
	resolvedEntry := entryPointID
	if resolvedEntry == "" {
		resolvedEntry = "auto"
	}

	var markup string
	var buildErr error
	if _, statErr := os.Stat(statePath); statErr == nil {
		req := product.BuildDiagramRequest{
			StatePath:   statePath,
			ParseFn:     akg.ParseGraphForQuery,
			DiagramType: dt,
			Format:      "mermaid",
			Options: product.DiagramOptions{
				Entry: entryPointID,
				Scope: types.ScopeGlobal,
			},
		}
		res, err := product.BuildDiagramEx(req)
		if err != nil {
			buildErr = err
		} else if res != nil {
			nodeCount = res.NodeCount
			edgeCount = res.EdgeCount
			markup = res.Markup
		}
	}

	if buildErr != nil {
		fmt.Fprintf(out, "Diagram type %q: INVALID (entry %s) — %v\n", name, entryReq, buildErr)
		fmt.Fprintf(out, "  Resolved Entry: %s\n", resolvedEntry)
		fmt.Fprintf(out, "  Node Count: %d, Edge Count: %d\n", nodeCount, edgeCount)
		fmt.Fprintf(out, "  Mermaid CLI Validation: INVALID (%v)\n", buildErr)
		return nil
	}

	fmt.Fprintf(out, "Diagram type %q: VALID (entry %s)\n", name, entryReq)
	fmt.Fprintf(out, "  Resolved Entry: %s\n", resolvedEntry)
	fmt.Fprintf(out, "  Node Count: %d, Edge Count: %d\n", nodeCount, edgeCount)

	// C6-7: do not hardcode VALID — attempt mermaid-cli validation when
	// available, otherwise report SKIP. This avoids false claims when markup
	// is syntactically broken.
	if markup != "" {
		if mmdc, err := exec.LookPath("mmdc"); err == nil {
			tmpIn, err := os.CreateTemp("", "gmb-check-*.mmd")
			if err == nil {
				_, _ = tmpIn.WriteString(markup)
				tmpIn.Close()
				tmpOut := tmpIn.Name() + ".svg"
				chkCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				chkCmd := exec.CommandContext(chkCtx, mmdc, "-i", tmpIn.Name(), "-o", tmpOut)
				var stderr bytes.Buffer
				chkCmd.Stderr = &stderr
				runErr := chkCmd.Run()
				cancel()
				_ = os.Remove(tmpIn.Name())
				_ = os.Remove(tmpOut)
				if runErr == nil {
					fmt.Fprintf(out, "  Mermaid CLI Validation: VALID\n")
				} else {
					msg := strings.TrimSpace(stderr.String())
					if msg == "" {
						msg = runErr.Error()
					}
					fmt.Fprintf(out, "  Mermaid CLI Validation: INVALID (%s)\n", msg)
				}
			} else {
				fmt.Fprintf(out, "  Mermaid CLI Validation: SKIP (temp file error)\n")
			}
		} else {
			fmt.Fprintf(out, "  Mermaid CLI Validation: SKIP (mermaid-cli not installed)\n")
		}
	} else {
		fmt.Fprintf(out, "  Mermaid CLI Validation: SKIP (no markup to validate)\n")
	}
	return nil
}

func init() {
	visualizeCmd.Flags().StringVar(&entryPointID, "entry", "", "Execution entry point symbol ID (mandatory for sequence diagrams)")
	visualizeCmd.Flags().IntVar(&maxDepth, "depth", 7, "Maximum search depth limit for reachability path walk")
	visualizeCmd.Flags().BoolVar(&includeUnused, "unused", false, "Include unreferenced dead components in the layout")
	visualizeCmd.Flags().StringVar(&saveFile, "save", "", "Save the diagram to a markdown file inside .glassmarble/marbles/")
	visualizeCmd.Flags().StringVar(&formatFlag, "format", "mermaid", "Output format: mermaid, plantuml, dot, or html")
	visualizeCmd.Flags().StringVar(&themeFlag, "theme", "modern", "Color palette theme: modern, dark, nordic, forest, or mono")
	visualizeCmd.Flags().StringVar(&directionFlag, "direction", "auto", "Layout direction: auto, TB, LR, or TD")
	visualizeCmd.Flags().StringVar(&scopeFlag, "scope", "", "Filter layout to specific scope: global (default), folder:path, or file:path")
	visualizeCmd.Flags().StringVar(&outputFlag, "output", "", "Write diagram to file instead of stdout")
	visualizeCmd.Flags().BoolVar(&summaryFlag, "summary", false, "Print graph summary before the diagram")
	visualizeCmd.Flags().BoolVar(&pagerankFlag, "pagerank", false, "Enable PageRank computation")
	visualizeCmd.Flags().BoolVar(&communityFlag, "community", false, "Enable community detection")
	visualizeCmd.Flags().BoolVar(&sccFlag, "scc", false, "Enable strongly connected components analysis")
	visualizeCmd.Flags().StringVar(&renderFlag, "render", "", "Render diagram to image file (.svg or .png) via Kroki or mermaid-cli")
	visualizeCmd.Flags().IntVar(&maxNodesFlag, "max-nodes", 0, "Maximum number of nodes to include in diagram (0 = unlimited)")
	visualizeCmd.Flags().StringSliceVar(&changedFiles, "changed-files", nil, "Comma-separated list of changed files for impact analysis")
	visualizeCmd.Flags().BoolVar(&relativeFlag, "relative", false, "Render file/symbol paths relative to folder root under folder scope")
	visualizeCmd.Flags().StringVar(&linkLevelFlag, "link-level", "architecture", "Detail level of graph linkage: architecture, standard, or full")
	visualizeCmd.Flags().BoolVar(&includeTestsFlag, "include-tests", false, "Include test files (*_test.go) and test functions in diagram")
	visualizeCmd.Flags().Bool("json", false, "Emit machine-readable JSON output containing diagram markup and summary")

	visualizeCmd.ValidArgsFunction = func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) == 0 {
			typesList := types.AllDiagramTypes()
			res := make([]string, 0, len(typesList)+2)
			res = append(res, "list", "check")
			for _, t := range typesList {
				res = append(res, string(t))
			}
			return res, cobra.ShellCompDirectiveNoFileComp
		}
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	_ = visualizeCmd.RegisterFlagCompletionFunc("format", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return []string{"mermaid", "plantuml", "dot", "html"}, cobra.ShellCompDirectiveNoFileComp
	})
	_ = visualizeCmd.RegisterFlagCompletionFunc("theme", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return []string{"modern", "dark", "nordic", "forest", "mono"}, cobra.ShellCompDirectiveNoFileComp
	})
	_ = visualizeCmd.RegisterFlagCompletionFunc("direction", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return []string{"auto", "TB", "LR", "TD"}, cobra.ShellCompDirectiveNoFileComp
	})
	_ = visualizeCmd.RegisterFlagCompletionFunc("link-level", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return []string{"architecture", "standard", "full"}, cobra.ShellCompDirectiveNoFileComp
	})

	rootCmd.AddCommand(visualizeCmd)
}
