package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/ai_engine/akgbridge"
)

// systemTools builds the session-level tools: repository status, the diagram
// vocabulary, and artifact saving.
func systemTools() []Tool {
	return []Tool{
		{
			Name:        "system_status",
			Description: "Repository status: git commit, AKG presence, and AKG graph counts. Call this first when asked anything about the repo.",
			Category:    CategorySystem,
			Parameters: Schema(map[string]Prop{
				"include_akg": {Type: "boolean", Description: "Also load the AKG graph and report counts (default true)", Default: true},
			}),
			Handler: func(ctx context.Context, env *Env, args map[string]any) (any, error) {
				out := map[string]any{"root_dir": env.RootDir}
				if commit, err := akgbridge.GitCommit(ctx, env.RootDir); err == nil {
					out["git_commit"] = commit
				}
				if env.Bridge != nil {
					st := env.Bridge.Status()
					akgInfo := map[string]any{
						"exists":     st.Exists,
						"path":       st.Path,
						"size_bytes": st.Size,
						"modified":   st.Modified,
					}
					if st.Exists && boolArg(args, "include_akg", true) {
						if snap, err := env.Bridge.Snapshot(); err == nil {
							akgInfo["counts"] = map[string]any{
								"nodes":       snap.Nodes.Len(),
								"edges":       akgbridge.EdgeCount(snap),
								"files":       snap.FileNodeIndex.Len(),
								"entrypoints": len(snap.Entrypoints),
								"errors":      len(snap.Errors),
							}
							akgInfo["commit"] = snap.CommitHash
						}
					}
					out["akg"] = akgInfo
				}
				return out, nil
			},
		},
		{
			Name:        "system_diagram_types",
			Description: "The complete vocabulary of diagram types the engine can generate (used with diagram tools). Call this before generating a diagram if unsure of the type name.",
			Category:    CategorySystem,
			Parameters:  Schema(nil),
			Handler: func(ctx context.Context, env *Env, args map[string]any) (any, error) {
				return map[string]any{"count": len(diagramTypeCatalog), "types": diagramTypeCatalog}, nil
			},
		},
		{
			Name:        "save_artifact",
			Description: "Save text content (an answer, report, or notes) to a file in the workspace's artifact directory (.glassmarble/ai/). Returns the saved path.",
			Category:    CategorySystem,
			Parameters: Schema(map[string]Prop{
				"filename": {Type: "string", Description: "File name, e.g. \"architecture-notes.md\" (subdirectories are not allowed)", Required: true},
				"content":  {Type: "string", Description: "The full text content to save", Required: true},
			}),
			Handler: func(ctx context.Context, env *Env, args map[string]any) (any, error) {
				filename := strArg(args, "filename", "")
				content := strArg(args, "content", "")
				if err := validateArtifactName(filename); err != nil {
					return nil, err
				}
				if len(content) > 1<<20 {
					return nil, fmt.Errorf("content too large (max 1 MiB, got %d bytes)", len(content))
				}
				dir := env.ArtifactDir
				if dir == "" {
					dir = filepath.Join(env.RootDir, ".glassmarble", "ai")
				}
				if err := os.MkdirAll(dir, 0o755); err != nil {
					return nil, fmt.Errorf("cannot create artifact directory: %v", err)
				}
				path := filepath.Join(dir, filename)
				if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
					return nil, fmt.Errorf("cannot write artifact: %v", err)
				}
				return map[string]any{"path": path, "bytes": len(content)}, nil
			},
		},
	}
}

func validateArtifactName(filename string) error {
	if filename == "" || filename == "." || filename == ".." {
		return fmt.Errorf("invalid artifact filename %q", filename)
	}
	if strings.ContainsAny(filename, `/\`) || strings.Contains(filename, "..") {
		return fmt.Errorf("artifact filenames must be plain file names without paths: %q", filename)
	}
	return nil
}

// diagramTypeInfo is one entry of the diagram vocabulary.
type diagramTypeInfo struct {
	Type        string `json:"type"`
	Family      string `json:"family"`
	Description string `json:"description"`
}

// diagramTypeCatalog mirrors the diagram types supported by the
// visualization engine (internal/visualization_engine/types).
var diagramTypeCatalog = []diagramTypeInfo{
	{Type: "UML_CLASS", Family: "UML", Description: "Class structure: types, fields, methods, relationships"},
	{Type: "UML_OBJECT", Family: "UML", Description: "Object instances at a point in time"},
	{Type: "UML_COMPONENT", Family: "UML", Description: "Software components and their interfaces"},
	{Type: "UML_DEPLOYMENT", Family: "UML", Description: "Physical deployment of artifacts on nodes"},
	{Type: "UML_PACKAGE", Family: "UML", Description: "Package organization and dependencies"},
	{Type: "UML_COMPOSITE", Family: "UML", Description: "Composite structure of a classifier"},
	{Type: "UML_PROFILE", Family: "UML", Description: "Profiles, stereotypes, and tagged values"},
	{Type: "UML_USECASE", Family: "UML", Description: "Actors, use cases, and system boundaries"},
	{Type: "UML_ACTIVITY", Family: "UML", Description: "Control flow of activities and actions"},
	{Type: "UML_STATE", Family: "UML", Description: "State machine: states and transitions"},
	{Type: "UML_SEQUENCE", Family: "UML", Description: "Message sequence between participants over time"},
	{Type: "UML_COMMUNICATION", Family: "UML", Description: "Collaboration between objects with numbered messages"},
	{Type: "UML_INTERACTION_OVERVIEW", Family: "UML", Description: "Overview of interaction fragments"},
	{Type: "UML_TIMING", Family: "UML", Description: "Timing constraints of state changes"},
	{Type: "C4_CONTEXT", Family: "C4", Description: "System context: the system and its external actors"},
	{Type: "C4_CONTAINER", Family: "C4", Description: "Containers (apps, services, databases) inside the system"},
	{Type: "C4_COMPONENT", Family: "C4", Description: "Components inside a container"},
	{Type: "C4_CODE", Family: "C4", Description: "Code-level classes and relationships"},
	{Type: "C4_LANDSCAPE", Family: "C4", Description: "Multiple systems in a landscape view"},
	{Type: "C4_DYNAMIC", Family: "C4", Description: "Runtime behavior between elements"},
	{Type: "C4_DEPLOYMENT", Family: "C4", Description: "Deployment environment of the system"},
	{Type: "DATA_FLOW", Family: "specialized", Description: "Data flow between producers, processors, and sinks"},
	{Type: "ER_DIAGRAM", Family: "specialized", Description: "Entities, attributes, and relationships (database schema)"},
	{Type: "MINDMAP", Family: "specialized", Description: "Hierarchical brainstorm view of modules and symbols"},
	{Type: "FLOWCHART", Family: "specialized", Description: "Control-flow chart of a function or process"},
	{Type: "DEPENDENCY_GRAPH", Family: "analysis", Description: "Dependencies between modules/packages"},
	{Type: "HOTSPOT_COMPLEXITY", Family: "analysis", Description: "Complexity hotspots ranked by metrics"},
	{Type: "CALL_GRAPH", Family: "analysis", Description: "Call graph from an entry point"},
	{Type: "LAYERED_ARCHITECTURE", Family: "analysis", Description: "Layered architecture with inter-layer dependencies"},
	{Type: "CHANGE_IMPACT", Family: "analysis", Description: "Blast radius of a change to a symbol"},
	{Type: "INFRASTRUCTURE", Family: "analysis", Description: "Infrastructure nodes: databases, queues, endpoints"},
}
