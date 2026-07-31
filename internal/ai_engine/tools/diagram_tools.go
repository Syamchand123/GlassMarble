package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/visualization_engine"
	"github.com/Syamchand123/GlassMarble/internal/visualization_engine/types"
)

// diagramTypes maps canonical diagram-type names (matching diagramTypeCatalog
// and the visualization engine's DiagramType constants) to the enum values.
var diagramTypes = func() map[string]types.DiagramType {
	m := make(map[string]types.DiagramType, len(diagramTypeCatalog))
	for _, info := range diagramTypeCatalog {
		m[canonicalDiagramType(info.Type)] = types.DiagramType(info.Type)
	}
	return m
}()

// canonicalDiagramType normalizes a user-supplied type name so "c4 container",
// "C4_CONTAINER", and "c4-container" all resolve to the same enum value.
func canonicalDiagramType(v string) string {
	v = strings.ToUpper(strings.TrimSpace(v))
	v = strings.ReplaceAll(v, "-", "_")
	return strings.ReplaceAll(v, " ", "_")
}

// diagramTools builds the visualization-engine tools: diagram generation,
// graph summary, and the type vocabulary.
func diagramTools() []Tool {
	return []Tool{
		{
			Name:        "diagram_generate",
			Description: "Generate a diagram of the repository in Mermaid (default), PlantUML, or DOT markup via the visualization engine. Types: all 31 in diagram_types/system_diagram_types (UML, C4, ER, data flow, mindmap, flowchart, dependency, hotspot, call graph, layered, change impact, infrastructure). Set save=true to write the markup to .glassmarble/marbles/ and return a path receipt instead of the full markup.",
			Category:    CategoryDiagram,
			Parameters: Schema(map[string]Prop{
				"type":      {Type: "string", Description: "Diagram type, e.g. UML_CLASS, C4_CONTAINER, DEPENDENCY_GRAPH, CALL_GRAPH (see diagram_types for the full list)", Required: true},
				"format":    {Type: "string", Description: "Output format", Enum: []string{"mermaid", "plantuml", "dot"}, Default: "mermaid"},
				"scope":     {Type: "string", Description: "Scope of the diagram", Enum: []string{"global", "folder:<path>", "file:<path>"}, Default: "global"},
				"entry":     {Type: "string", Description: "Entry point node ID to start traversal from (mandatory for UML_SEQUENCE, recommended for CALL_GRAPH)"},
				"depth":     {Type: "integer", Description: "Maximum traversal depth from the entry point (0 = unlimited)", Default: 0},
				"unused":    {Type: "boolean", Description: "Include nodes not reachable from any entry point", Default: false},
				"pagerank":  {Type: "boolean", Description: "Enable PageRank-based metrics in the layout", Default: true},
				"community": {Type: "boolean", Description: "Enable community detection clustering", Default: true},
				"scc":       {Type: "boolean", Description: "Enable strongly-connected-component analysis", Default: true},
				"save":      {Type: "boolean", Description: "Write the markup to .glassmarble/marbles/<type>.md and return a path receipt", Default: false},
			}),
			Handler: func(ctx context.Context, env *Env, args map[string]any) (any, error) {
				dt, ok := diagramTypes[canonicalDiagramType(strArg(args, "type", ""))]
				if !ok {
					return nil, fmt.Errorf("unknown diagram type %q — see diagram_types for the 31 supported types", strArg(args, "type", ""))
				}
				if dt == types.UMLSequence && strArg(args, "entry", "") == "" {
					return nil, fmt.Errorf("entry point (--entry) is mandatory for UML_SEQUENCE diagrams")
				}
				ttlPath := filepath.Join(env.RootDir, ".glassmarble", "akg_state.ttl")
				if _, err := os.Stat(ttlPath); err != nil {
					return nil, fmt.Errorf("AKG database not found at %s — run `gmb analyze` first", ttlPath)
				}
				scope, scopePath, err := parseDiagramScope(strArg(args, "scope", "global"))
				if err != nil {
					return nil, err
				}
				opts := types.QueryOptions{
					EntryPointID:  strArg(args, "entry", ""),
					MaxDepth:      intArg(args, "depth", 0, 0, 50),
					IncludeUnused: boolArg(args, "unused", false),
					Format:        strArg(args, "format", "mermaid"),
					Scope:         scope,
					ScopePath:     scopePath,
				}
				_, hasPR := args["pagerank"]
				_, hasComm := args["community"]
				_, hasSCC := args["scc"]
				if hasPR || hasComm || hasSCC {
					opts.PipelineCfg = &types.PipelineConfig{
						EnableMetrics:     boolArg(args, "pagerank", true),
						EnableCommunities: boolArg(args, "community", true),
						EnableSCC:         boolArg(args, "scc", true),
					}
				}
				markup, err := visualization_engine.NewEngineCoordinator(ttlPath).ProjectDiagram(dt, opts)
				if err != nil {
					return nil, fmt.Errorf("diagram generation failed: %w", err)
				}
				if !boolArg(args, "save", false) {
					return Raw(markup), nil
				}
				path, err := saveDiagramMarkup(env.RootDir, dt, markup)
				if err != nil {
					return nil, err
				}
				return map[string]any{
					"saved":  path,
					"type":   string(dt),
					"bytes":  len(markup),
					"format": strArg(args, "format", "mermaid"),
				}, nil
			},
		},
		{
			Name:        "diagram_summary",
			Description: "Compute the graph summary of a diagram type's extracted subgraph: node/edge counts, density, diameter, average path length, clusters, largest SCC, god objects, bipartite score. Use this to characterize the graph before deciding what to render.",
			Category:    CategoryDiagram,
			Parameters: Schema(map[string]Prop{
				"type":   {Type: "string", Description: "Diagram type, e.g. UML_CLASS, DEPENDENCY_GRAPH, CALL_GRAPH (see diagram_types)", Required: true},
				"scope":  {Type: "string", Description: "Scope of the diagram", Enum: []string{"global", "folder:<path>", "file:<path>"}, Default: "global"},
				"entry":  {Type: "string", Description: "Entry point node ID for traversal-based types"},
				"depth":  {Type: "integer", Description: "Maximum traversal depth (0 = unlimited)", Default: 0},
				"unused": {Type: "boolean", Description: "Include unused nodes", Default: false},
			}),
			Handler: func(ctx context.Context, env *Env, args map[string]any) (any, error) {
				dt, ok := diagramTypes[canonicalDiagramType(strArg(args, "type", ""))]
				if !ok {
					return nil, fmt.Errorf("unknown diagram type %q — see diagram_types for the 31 supported types", strArg(args, "type", ""))
				}
				ttlPath := filepath.Join(env.RootDir, ".glassmarble", "akg_state.ttl")
				if _, err := os.Stat(ttlPath); err != nil {
					return nil, fmt.Errorf("AKG database not found at %s — run `gmb analyze` first", ttlPath)
				}
				scope, scopePath, err := parseDiagramScope(strArg(args, "scope", "global"))
				if err != nil {
					return nil, err
				}
				opts := types.QueryOptions{
					EntryPointID:  strArg(args, "entry", ""),
					MaxDepth:      intArg(args, "depth", 0, 0, 50),
					IncludeUnused: boolArg(args, "unused", false),
					Scope:         scope,
					ScopePath:     scopePath,
				}
				s, err := visualization_engine.NewEngineCoordinator(ttlPath).ComputeGraphSummary(dt, opts)
				if err != nil {
					return nil, fmt.Errorf("diagram summary failed: %w", err)
				}
				return map[string]any{
					"node_count":       s.NodeCount,
					"edge_count":       s.EdgeCount,
					"density":          s.Density,
					"diameter":         s.Diameter,
					"avg_path_length":  s.AvgPathLength,
					"cluster_count":    s.ClusterCount,
					"largest_scc_size": s.LargestSCCSize,
					"god_object_count": s.GodObjectCount,
					"bipartite_score":  s.BipartiteScore,
				}, nil
			},
		},
		{
			Name:        "diagram_types",
			Description: "The complete vocabulary of diagram types the engine can generate (31 types across UML, C4, and specialized/analysis families). Call this before generating a diagram if unsure of the type name.",
			Category:    CategoryDiagram,
			Parameters:  Schema(nil),
			Handler: func(ctx context.Context, env *Env, args map[string]any) (any, error) {
				return map[string]any{"count": len(diagramTypeCatalog), "types": diagramTypeCatalog}, nil
			},
		},
	}
}

// parseDiagramScope mirrors gmb visualize's --scope parsing.
func parseDiagramScope(scopeStr string) (types.ScopeLevel, string, error) {
	switch {
	case scopeStr == "" || scopeStr == "global":
		return types.ScopeGlobal, "", nil
	case strings.HasPrefix(scopeStr, "folder:"):
		path := strings.TrimPrefix(scopeStr, "folder:")
		if path == "" {
			return types.ScopeGlobal, "", fmt.Errorf("folder scope requires a non-empty path")
		}
		return types.ScopeFolder, path, nil
	case strings.HasPrefix(scopeStr, "file:"):
		path := strings.TrimPrefix(scopeStr, "file:")
		if path == "" {
			return types.ScopeGlobal, "", fmt.Errorf("file scope requires a non-empty path")
		}
		return types.ScopeFile, path, nil
	}
	return types.ScopeGlobal, "", fmt.Errorf("invalid scope %q (valid values: global, folder:path, file:path)", scopeStr)
}

// saveDiagramMarkup writes diagram markup to .glassmarble/marbles/, mirroring
// gmb visualize --save.
func saveDiagramMarkup(rootDir string, dt types.DiagramType, markup string) (string, error) {
	marblesDir := filepath.Join(rootDir, ".glassmarble", "marbles")
	if err := os.MkdirAll(marblesDir, 0o755); err != nil {
		return "", fmt.Errorf("failed to create marbles directory: %w", err)
	}
	fileName := strings.ToLower(strings.ReplaceAll(string(dt), "_", "-")) + ".md"
	filePath := filepath.Join(marblesDir, fileName)
	if err := os.WriteFile(filePath, []byte(markup), 0o644); err != nil {
		return "", fmt.Errorf("failed to write diagram: %w", err)
	}
	return filePath, nil
}
