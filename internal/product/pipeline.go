package product

import (
	"context"
	"fmt"
	"os"
	"strings"

	producterrs "github.com/Syamchand123/GlassMarble/internal/product/errors"
	"github.com/Syamchand123/GlassMarble/internal/visualization_engine"
	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/types"
)

// DiagramOptions holds option parameters for diagram queries (11.1).
type DiagramOptions struct {
	Scope         types.ScopeLevel
	ScopePath     string
	Entry         string
	Depth         int
	MaxNodes      int
	LinkLevel     string // "architecture" | "standard" | "full"
	IncludeUnused bool
	ChangedFiles  []string
	Relative      bool
	Theme         string
	Direction     string
}

// BuildDiagramRequest holds all configuration parameters for generating an architecture diagram (11.1 / §11.1).
type BuildDiagramRequest struct {
	StatePath   string
	DiagramType types.DiagramType
	Format      string // "mermaid" | "plantuml" | "dot"
	Options     DiagramOptions
	OnProgress  func(step, detail string)
	OnWarning   func(msg string)
	OnSummary   func(s *types.GraphSummary)

	// ParseFn overrides the state-file parser used by the engine
	// coordinator. Callers serving the canonical akg.json GraphJSON store
	// pass akg.ParseGraphForQuery; nil keeps the coordinator's built-in
	// legacy Turtle reader (pre-migration state files only).
	ParseFn visualization_engine.ParseFn

	// IncludeAST, when true, builds the typed DiagramAST (second full
	// extract→layout pass) for callers that need programmatic access.
	// Default false avoids roughly doubling generation time (C6-8).
	IncludeAST bool

	// Legacy flat fields for backward compatibility
	Type          types.DiagramType
	Scope         types.ScopeLevel
	ScopePath     string
	Entry         string
	Depth         int
	IncludeUnused bool
	MaxNodes      int
	RelativePath  bool
}

// DiagramRequest is an alias for BuildDiagramRequest (back-compat).
type DiagramRequest = BuildDiagramRequest

// BuildDiagramResult holds the output markup, graph summary, layout graph, and renderer metadata (11.1 / §11.1).
type BuildDiagramResult struct {
	Markup    string
	Summary   *types.GraphSummary
	Graph     *types.LayoutTree
	AST       *types.DiagramAST
	Renderer  visualization_engine.Renderer
	NodeCount int
	EdgeCount int
}

// BuildDiagram is the single unified entry point for generating architecture diagrams
// across CLI (`gmb visualize`), TUI, and AI engine tools (V-11 / W3-09 / W7-01).
func BuildDiagram(req BuildDiagramRequest) (string, *types.GraphSummary, error) {
	res, err := BuildDiagramEx(req)
	if res == nil {
		return "", nil, err
	}
	return res.Markup, res.Summary, err
}

// BuildDiagramEx returns a structured BuildDiagramResult containing markup, graph, summary, and renderer.
func BuildDiagramEx(req BuildDiagramRequest) (*BuildDiagramResult, error) {
	return BuildDiagramWithContext(context.Background(), req)
}

// BuildDiagramWithContext accepts a Context for context cancellation and telemetry span tracking.
func BuildDiagramWithContext(ctx context.Context, req BuildDiagramRequest) (*BuildDiagramResult, error) {
	if req.StatePath == "" {
		return nil, producterrs.Annotate(fmt.Errorf("StatePath is required"), producterrs.ErrValidation)
	}

	// Normalize flat legacy fields into Options if Options is empty
	diagType := req.DiagramType
	if diagType == "" {
		diagType = req.Type
	}

	scope := req.Options.Scope
	if scope == types.ScopeGlobal && req.Scope != types.ScopeGlobal {
		scope = req.Scope
	}
	scopePath := req.Options.ScopePath
	if scopePath == "" {
		scopePath = req.ScopePath
	}
	entry := req.Options.Entry
	if entry == "" {
		entry = req.Entry
	}
	depth := req.Options.Depth
	if depth <= 0 {
		depth = req.Depth
	}
	maxNodes := req.Options.MaxNodes
	if maxNodes <= 0 {
		maxNodes = req.MaxNodes
	}
	includeUnused := req.Options.IncludeUnused || req.IncludeUnused
	relative := req.Options.Relative || req.RelativePath
	changedFiles := req.Options.ChangedFiles
	linkLevel := req.Options.LinkLevel
	if linkLevel == "" {
		linkLevel = "architecture"
	}

	if req.Format == "" {
		req.Format = "mermaid"
	}
	format := strings.ToLower(req.Format)
	switch format {
	case "mermaid", "plantuml", "dot", "graphviz", "html":
		// valid
	default:
		return nil, producterrs.Annotate(fmt.Errorf("unsupported diagram format %q (valid: mermaid, plantuml, dot, html)", req.Format), producterrs.ErrValidation)
	}

	if scope == types.ScopeFolder || scope == types.ScopeFile {
		if scopePath != "" {
			if _, err := os.Stat(scopePath); err != nil && !strings.Contains(scopePath, "::") {
				if req.OnWarning != nil {
					req.OnWarning(fmt.Sprintf("scope path %q not found on disk", scopePath))
				}
			}
		}
	}

	if depth <= 0 {
		switch diagType {
		case types.UMLComposite, types.UMLObject:
			depth = 3
		case types.C4Dynamic, types.ChangeImpact:
			depth = 5
		case types.UMLSequence, types.UMLCommunication, types.UMLInteractionOverview, types.UMLTiming:
			depth = 7
		case types.DataFlow, types.Flowchart, types.UMLActivity:
			depth = 10
		default:
			depth = 99
		}
	}

	theme := req.Options.Theme
	if theme == "" {
		theme = "modern"
	}
	direction := req.Options.Direction
	if direction == "" {
		direction = "auto"
	}

	opts := types.QueryOptions{
		EntryPointID:  entry,
		Scope:         scope,
		ScopePath:     scopePath,
		RelativePath:  relative,
		MaxDepth:      depth,
		IncludeUnused: includeUnused,
		MaxNodes:      maxNodes,
		Format:        format,
		ChangedFiles:  changedFiles,
		LinkLevel:     linkLevel,
		Theme:         theme,
		Direction:     direction,
		OnProgress:    req.OnProgress,
		OnSummary:     req.OnSummary,
	}

	// Record Telemetry Spans (11.4 / W7-02) — C6-10: rename misleading labels.
	// "extract" previously covered only coordinator construction; real
	// extraction/layout happens inside BuildLayoutTree.
	doneCoordinator := StartSpan("coordinator")
	ec := visualization_engine.NewEngineCoordinator(req.StatePath)
	// The request layer installs the canonical GraphJSON reader (Phase C);
	// without it the coordinator keeps its built-in legacy Turtle fallback.
	if req.ParseFn != nil {
		ec.SetParseFn(req.ParseFn)
	}
	doneCoordinator()

	donePipeline := StartSpan("pipeline")
	tree, err := ec.BuildLayoutTree(diagType, opts)
	donePipeline()

	doneRender := StartSpan("render")
	var renderer visualization_engine.Renderer
	switch format {
	case "plantuml":
		renderer = &visualization_engine.PlantUMLRenderer{}
	case "dot", "graphviz":
		renderer = &visualization_engine.DOTRenderer{}
	default:
		renderer = &visualization_engine.MermaidRenderer{}
	}

	var markup string
	if err == nil && tree != nil {
		markup, err = renderer.Render(tree, diagType)
	}
	if err != nil {
		// C6-9: do not discard the original error and re-run the same
		// deterministic pipeline via ProjectDiagram (doubling latency for
		// the same failure). Return the original error directly.
		doneRender()
		return nil, err
	}

	// Summary comes from the already-built layout tree — never a second
	// parse + extract pass (GAP-H-04 / W3-09 single-pass pipeline).
	summary := (*types.GraphSummary)(nil)
	if tree != nil {
		summary = tree.Summary
	}
	if summary == nil {
		summary = &types.GraphSummary{}
	}
	if req.OnSummary != nil {
		req.OnSummary(summary)
	}
	doneRender()

	nodeCount := 0
	edgeCount := 0
	if summary != nil {
		nodeCount = summary.NodeCount
		edgeCount = summary.EdgeCount
	}

	// Prepend header comment if missing (11.5)
	markup = injectHeaderComment(markup, diagType, scope, entry, nodeCount, edgeCount, format)

	// C6-8: build AST lazily only when caller explicitly needs it; the
	// second full pipeline pass roughly doubles generation time and almost
	// no caller reads AST (visualize uses Markup/Summary only).
	var ast *types.DiagramAST
	if req.IncludeAST && tree != nil {
		ast, _ = ec.BuildDiagramAST(diagType, opts)
	}

	// Print debug output if GMB_DEBUG=1 (11.4)
	if os.Getenv("GMB_DEBUG") == "1" {
		fmt.Fprintf(os.Stderr, "[GMB_DEBUG] Pipeline: type=%s scope=%s entry=%s format=%s nodes=%d edges=%d linkLevel=%s\n",
			diagType, scopeString(scope), entry, format, nodeCount, edgeCount, linkLevel)
	}

	return &BuildDiagramResult{
		Markup:    markup,
		Summary:   summary,
		Graph:     tree,
		AST:       ast,
		Renderer:  renderer,
		NodeCount: nodeCount,
		EdgeCount: edgeCount,
	}, nil
}

func scopeString(s types.ScopeLevel) string {
	switch s {
	case types.ScopeFolder:
		return "folder"
	case types.ScopeFile:
		return "file"
	default:
		return "global"
	}
}

// Version is the GlassMarble release version surfaced by `gmb version` and
// stamped into generated diagram headers. cmd/root.go references it so the
// CLI and the diagrams never drift apart.
// It is a var so it can be overridden at build time via
// `go build -ldflags "-X github.com/Syamchand123/GlassMarble/internal/product.Version=vX.Y.Z"` (C6-D29/D30).
var Version = "0.1.0"

// injectHeaderComment adds a standardized header comment if not already present (11.5).
// Header format: % <type> · <scope> · entry=<resolved> · nodes=N edges=M · generated by gmb <version>
func injectHeaderComment(markup string, diagType types.DiagramType, scope types.ScopeLevel, entry string, nodes, edges int, format string) string {
	if diagType == types.Mindmap && format != "plantuml" && format != "dot" && format != "graphviz" {
		return markup
	}
	resolvedEntry := entry
	if resolvedEntry == "" {
		resolvedEntry = "auto"
	}
	scopeStr := scopeString(scope)

	var commentLeader string
	switch format {
	case "plantuml":
		commentLeader = "'"
	case "dot", "graphviz":
		commentLeader = "//"
	default:
		commentLeader = "%%"
	}

	header := fmt.Sprintf("%s %s · %s · entry=%s · nodes=%d edges=%d · generated by gmb %s\n",
		commentLeader, diagType, scopeStr, resolvedEntry, nodes, edges, Version)
	if format == "mermaid" || format == "" {
		header += "%%{init: {\"theme\": \"base\", \"maxTextSize\": 1000000}}%%\n"
	}

	// Skip injection only when a previously-inserted gmb header is actually
	// present; a leading comment character on its own is not proof (PlantUML
	// markup may legitimately start with other content) (GAP-L-01). The
	// marker check is version-agnostic so headers written by older builds
	// are not duplicated.
	if hasGeneratedByHeader(markup) {
		return markup
	}
	return header + markup
}

// hasGeneratedByHeader reports whether the first non-empty line of markup
// carries the gmb header marker.
func hasGeneratedByHeader(markup string) bool {
	for _, line := range strings.Split(markup, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		return strings.Contains(trimmed, "generated by gmb")
	}
	return false
}
