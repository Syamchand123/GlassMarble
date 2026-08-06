package product

import (
	"fmt"
	"os"
	"strings"

	producterrs "github.com/Syamchand123/GlassMarble/internal/product/errors"
	"github.com/Syamchand123/GlassMarble/internal/visualization_engine"
	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/types"
)

// DiagramRequest holds all configuration parameters for generating an architecture diagram (V-11 / §7.4).
type DiagramRequest struct {
	TTLPath       string
	Type          types.DiagramType
	Scope         types.ScopeLevel
	ScopePath     string
	Entry         string
	Depth         int
	IncludeUnused bool
	MaxNodes      int
	RelativePath  bool
	Format        string // "mermaid" | "plantuml" | "dot"
	OnProgress    func(stage, detail string)
	OnWarning     func(msg string)
	OnSummary     func(s *types.GraphSummary)
}

// BuildDiagram is the single unified entry point for generating architecture diagrams
// across CLI (`gmb visualize`), TUI, and AI engine tools (V-11 / W3-09).
func BuildDiagram(req DiagramRequest) (string, *types.GraphSummary, error) {
	if req.TTLPath == "" {
		return "", nil, producterrs.Annotate(fmt.Errorf("TTLPath is required"), producterrs.ErrValidation)
	}
	if req.Format == "" {
		req.Format = "mermaid"
	}
	switch strings.ToLower(req.Format) {
	case "mermaid", "plantuml", "dot":
		// valid
	default:
		return "", nil, producterrs.Annotate(fmt.Errorf("unsupported diagram format %q (valid: mermaid, plantuml, dot)", req.Format), producterrs.ErrValidation)
	}

	if req.Scope == types.ScopeFolder || req.Scope == types.ScopeFile {
		if req.ScopePath != "" {
			if _, err := os.Stat(req.ScopePath); err != nil && !strings.Contains(req.ScopePath, "::") {
				if req.OnWarning != nil {
					req.OnWarning(fmt.Sprintf("scope path %q not found on disk", req.ScopePath))
				}
			}
		}
	}

	depth := req.Depth
	if depth <= 0 {
		switch req.Type {
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

	opts := types.QueryOptions{
		EntryPointID:  req.Entry,
		Scope:         req.Scope,
		ScopePath:     req.ScopePath,
		RelativePath:  req.RelativePath,
		MaxDepth:      depth,
		IncludeUnused: req.IncludeUnused,
		MaxNodes:      req.MaxNodes,
		Format:        strings.ToLower(req.Format),
		OnProgress:    req.OnProgress,
		OnSummary:     req.OnSummary,
	}

	ec := visualization_engine.NewEngineCoordinator(req.TTLPath)
	markup, err := ec.ProjectDiagram(req.Type, opts)
	if err != nil {
		return "", nil, err
	}

	summary, err := ec.ComputeGraphSummary(req.Type, opts)
	if err != nil {
		return markup, nil, nil
	}

	return markup, summary, nil
}
