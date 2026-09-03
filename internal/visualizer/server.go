package visualizer

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/link"
	"github.com/Syamchand123/GlassMarble/internal/impact_analyzer"
	"github.com/Syamchand123/GlassMarble/internal/product/ont"
)

//go:embed assets/*
var assetsFS embed.FS

// ServerOptions defines configuration parameters for the visualizer web server.
type ServerOptions struct {
	Host     string
	Port     int
	AutoOpen bool
	RootDir  string
}

// VisualizerServer manages the HTTP listener and API endpoints.
type VisualizerServer struct {
	graph   *akg.CodePropertyGraph
	options ServerOptions
	server  *http.Server
	port    int
}

// NewServer instantiates a fresh visualizer HTTP server instance.
func NewServer(graph *akg.CodePropertyGraph, opts ServerOptions) *VisualizerServer {
	if opts.Host == "" {
		opts.Host = "127.0.0.1"
	}
	if opts.Port == 0 {
		opts.Port = 8080
	}

	return &VisualizerServer{
		graph:   graph,
		options: opts,
	}
}

func (s *VisualizerServer) getStorageDir() string {
	if s.options.RootDir != "" {
		return filepath.Join(s.options.RootDir, ".glassmarble")
	}
	if _, err := os.Stat(".glassmarble"); err == nil {
		return ".glassmarble"
	}
	return ".glassmarble"
}

// Start boots the HTTP server on an available port and optionally launches the browser.
func (s *VisualizerServer) Start(ctx context.Context) (int, error) {
	mux := http.NewServeMux()

	// Core API Handlers
	mux.HandleFunc("/api/health", s.handleHealth)
	mux.HandleFunc("/api/status", s.handleStatus)
	mux.HandleFunc("/api/graph", s.handleGraph)
	mux.HandleFunc("/api/impact", s.handleImpact)
	mux.HandleFunc("/api/search", s.handleSearch)
	mux.HandleFunc("/api/smells", s.handleSmells)
	mux.HandleFunc("/api/layers", s.handleLayers)
	mux.HandleFunc("/api/paths", s.handlePaths)

	// Extended GlassMarble Intelligence & Memory Handlers
	mux.HandleFunc("/api/intelligence", s.handleIntelligence)
	mux.HandleFunc("/api/timeline", s.handleTimeline)
	mux.HandleFunc("/api/conventions", s.handleConventions)
	mux.HandleFunc("/api/snapshots", s.handleSnapshots)
	mux.HandleFunc("/api/marbles", s.handleMarbles)

	// Built-in AKG Graph Algorithms Handlers
	mux.HandleFunc("/api/algorithms/cycles", s.handleAlgoCycles)
	mux.HandleFunc("/api/algorithms/toposort", s.handleAlgoToposort)
	mux.HandleFunc("/api/algorithms/cutvertices", s.handleAlgoCutVertices)
	mux.HandleFunc("/api/algorithms/pagerank", s.handleAlgoPageRank)
	mux.HandleFunc("/api/algorithms/similarity", s.handleAlgoSimilarity)
	mux.HandleFunc("/api/algorithms/orphans", s.handleAlgoOrphans)

	// Static Assets
	assetSub, err := fs.Sub(assetsFS, "assets")
	if err != nil {
		return 0, fmt.Errorf("failed to load embedded assets: %w", err)
	}

	fileServer := http.FileServer(http.FS(assetSub))
	mux.Handle("/assets/", http.StripPrefix("/assets/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Embedded assets only change with the binary; embed.FS has a zero
		// ModTime so no ETag/Last-Modified is emitted — without this header
		// the multi-megabyte vendored libraries re-download on every load.
		w.Header().Set("Cache-Control", "public, max-age=86400")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		fileServer.ServeHTTP(w, r)
	})))
	// Per-process asset version: index.html is always served fresh, and its
	// asset URLs carry this token — so cached JS/CSS is busted automatically
	// on every new binary/server start.
	assetVersion := strconv.FormatInt(time.Now().Unix(), 36)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		data, err := assetsFS.ReadFile("assets/index.html")
		if err != nil {
			http.Error(w, "index.html not found", http.StatusInternalServerError)
			return
		}
		page := strings.ReplaceAll(string(data), "__ASSET_V__", assetVersion)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-cache")
		w.Write([]byte(page))
	})

	// Find available port
	listener, err := net.Listen("tcp", fmt.Sprintf("%s:%d", s.options.Host, s.options.Port))
	if err != nil {
		// Fallback to random free port
		listener, err = net.Listen("tcp", fmt.Sprintf("%s:0", s.options.Host))
		if err != nil {
			return 0, fmt.Errorf("failed to bind visualizer server: %w", err)
		}
	}

	s.port = listener.Addr().(*net.TCPAddr).Port
	s.server = &http.Server{
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = s.server.Shutdown(shutdownCtx)
	}()

	if s.options.AutoOpen {
		go func() {
			time.Sleep(200 * time.Millisecond)
			_ = OpenBrowser(fmt.Sprintf("http://%s:%d", s.options.Host, s.port))
		}()
	}

	go func() {
		_ = s.server.Serve(listener)
	}()

	return s.port, nil
}

// Stop gracefully shuts down the visualizer server, then force-closes anything
// still connected. Shutdown alone waits on in-flight requests and on clients
// holding keep-alive connections, so a browser left open on the dashboard could
// stall the caller until the grace period expired.
func (s *VisualizerServer) Stop() error {
	if s.server == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := s.server.Shutdown(ctx)
	if err != nil {
		// Grace expired: drop the remaining connections rather than leaking
		// the listener and reporting failure to the caller.
		if closeErr := s.server.Close(); closeErr == nil {
			return nil
		}
		return err
	}
	return nil
}

func (s *VisualizerServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"ok","version":"1.1.0"}`))
}

func (s *VisualizerServer) handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	nodeCount := 0
	edgeCount := 0
	filesCount := 0

	if s.graph != nil {
		if s.graph.Nodes != nil {
			nodeCount = s.graph.Nodes.Len()
		}
		if s.graph.OutboundEdges != nil {
			s.graph.OutboundEdges.Iterate(func(_ string, es []link.ResolvedEdge) {
				edgeCount += len(es)
			})
		}
		if s.graph.FileNodeIndex != nil {
			filesCount = s.graph.FileNodeIndex.Len()
		}
	}

	smellsCount := 0
	// Check intelligence file for smell count
	storageDir := s.getStorageDir()
	intelPath := filepath.Join(storageDir, "intelligence", "latest.json")
	if data, err := os.ReadFile(intelPath); err == nil {
		var intel struct {
			Metrics struct {
				TotalSmells int `json:"smell_count"`
			} `json:"metrics"`
			Smells []any `json:"smells"`
		}
		if json.Unmarshal(data, &intel) == nil {
			smellsCount = len(intel.Smells)
		}
	}

	schemaVersion := 0
	commitHash := ""
	if s.graph != nil {
		schemaVersion = s.graph.SchemaVersion
		commitHash = s.graph.CommitHash
	}

	res := map[string]any{
		"status":         "healthy",
		"nodes_count":    nodeCount,
		"edges_count":    edgeCount,
		"files_count":    filesCount,
		"smells_count":   smellsCount,
		"schema_version": schemaVersion,
		"commit_hash":    commitHash,
	}
	json.NewEncoder(w).Encode(res)
}

// Cytoscape element structures
type CyNodeData struct {
	ID          string  `json:"id"`
	Label       string  `json:"label"`
	Kind        string  `json:"kind"`
	File        string  `json:"file,omitempty"`
	Line        int     `json:"line,omitempty"`
	Parent      string  `json:"parent,omitempty"`
	Layer       string  `json:"layer,omitempty"`
	PageRank    float64 `json:"pagerank,omitempty"`
	InDegree    int     `json:"in_degree"`
	OutDegree   int     `json:"out_degree"`
	Instability float64 `json:"instability,omitempty"`
	IsEntry     bool    `json:"is_entry"`
	IsTest      bool    `json:"is_test"`
	IsCompound  bool    `json:"is_compound"`
}

type CyEdgeData struct {
	ID      string `json:"id"`
	Source  string `json:"source"`
	Target  string `json:"target"`
	Label   string `json:"label"`
	Type    string `json:"type"`
	Weight  int    `json:"weight,omitempty"`
	IsCycle bool   `json:"is_cycle"`
	Line    int    `json:"line,omitempty"`
}

type CyElement struct {
	Group string `json:"group"` // "nodes" or "edges"
	Data  any    `json:"data"`
}

func (s *VisualizerServer) handleGraph(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	viewMode := r.URL.Query().Get("view") // "components", "packages", "symbols", "full"
	pkgFilter := r.URL.Query().Get("pkg")
	if viewMode == "" {
		viewMode = "components" // Default to clean, 100% complete component architecture
	}

	elements := make([]CyElement, 0)
	nodeIDSet := make(map[string]bool)

	// Collect In/Out Degrees
	inDegrees := make(map[string]int)
	outDegrees := make(map[string]int)
	if s.graph != nil && s.graph.InboundEdges != nil {
		s.graph.InboundEdges.Iterate(func(id string, edges []link.ResolvedEdge) {
			inDegrees[id] = len(edges)
		})
	}
	if s.graph != nil && s.graph.OutboundEdges != nil {
		s.graph.OutboundEdges.Iterate(func(id string, edges []link.ResolvedEdge) {
			outDegrees[id] = len(edges)
		})
	}

	switch viewMode {
	case "components":
		// Mode 1: Component Architecture Map (from intelligence/latest.json or inferred)
		elements = s.buildComponentGraph()
		json.NewEncoder(w).Encode(elements)
		return

	case "packages":
		// Mode 2: Package / Directory Dependency Map
		elements = s.buildPackageGraph()
		json.NewEncoder(w).Encode(elements)
		return

	default:
		// Mode 3: Architectural Symbols (Structs, Interfaces, Functions, Databases)
		parentPackages := make(map[string]bool)

		// Filter for top meaningful architectural symbols
		if s.graph != nil && s.graph.Nodes != nil {
			s.graph.Nodes.Iterate(func(id string, n *link.ResolvedNode) {
				if n == nil {
					return
				}

				// Skip synthetic AST / CFG nodes and anonymous parameters
				if strings.HasPrefix(n.Kind, "CFG_") || strings.HasPrefix(n.Kind, "DFG_") ||
					strings.HasPrefix(n.Name, "param:") || strings.HasPrefix(n.Name, "anonymous_") {
					return
				}

				filePath := filepath.ToSlash(n.FileSpec.Path)
				if pkgFilter != "" && !strings.HasPrefix(filePath, pkgFilter) {
					return
				}

				// Filter for architectural definitions (unless in package drill-down)
				if pkgFilter == "" {
					isArchKind := n.Kind == "STRUCT" || n.Kind == "INTERFACE" || n.Kind == "CLASS" ||
						n.Kind == "DATABASE" || n.Kind == "ENTRYPOINT" || n.Kind == "MODULE" ||
						n.Kind == "SERVICE" || inDegrees[n.ID] > 1 || outDegrees[n.ID] > 1 ||
						n.Name == "main" || strings.HasPrefix(n.Kind, "TYPE")
					if !isArchKind {
						return
					}
				}

				pkgDir := ""
				if filePath != "" {
					pkgDir = filepath.ToSlash(filepath.Dir(filePath))
					if pkgDir != "." && pkgDir != "" {
						parentPackages[pkgDir] = true
					}
				}

				pr := 0.0
				if val, ok := n.Properties[ont.PredPagerank]; ok {
					pr, _ = strconv.ParseFloat(val, 64)
				}

				layer := detectLayer(filePath, n.Kind)
				isEntry := n.Kind == "ENTRYPOINT" || n.Name == "main"
				isTest := strings.HasSuffix(filePath, "_test.go") || strings.HasSuffix(filePath, ".test.ts")

				elements = append(elements, CyElement{
					Group: "nodes",
					Data: CyNodeData{
						ID:        n.ID,
						Label:     n.Name,
						Kind:      n.Kind,
						File:      filePath,
						Line:      n.FileSpec.LineStart,
						Parent:    pkgDir,
						Layer:     layer,
						PageRank:  pr,
						InDegree:  inDegrees[n.ID],
						OutDegree: outDegrees[n.ID],
						IsEntry:   isEntry,
						IsTest:    isTest,
					},
				})
				nodeIDSet[n.ID] = true
			})
		}

		// Add compound parent packages
		for pkg := range parentPackages {
			elements = append(elements, CyElement{
				Group: "nodes",
				Data: CyNodeData{
					ID:         pkg,
					Label:      pkg,
					Kind:       "PACKAGE",
					IsCompound: true,
				},
			})
			nodeIDSet[pkg] = true
		}

		// Add all interconnecting edges between included nodes
		if s.graph != nil && s.graph.OutboundEdges != nil {
			s.graph.OutboundEdges.Iterate(func(srcID string, es []link.ResolvedEdge) {
				if !nodeIDSet[srcID] {
					return
				}
				for _, e := range es {
					if !nodeIDSet[e.TargetID] || e.TargetID == srcID {
						continue
					}
					edgeID := fmt.Sprintf("e_%s_%s_%s", e.SourceID, e.TargetID, string(e.Type))
					elements = append(elements, CyElement{
						Group: "edges",
						Data: CyEdgeData{
							ID:      edgeID,
							Source:  e.SourceID,
							Target:  e.TargetID,
							Label:   string(e.Type),
							Type:    string(e.Type),
							IsCycle: e.IsCycle,
							Line:    e.LineNumber,
						},
					})
				}
			})
		}

		json.NewEncoder(w).Encode(elements)
	}
}

// buildComponentGraph loads all 80+ inferred architectural components and their inter-component dependencies.
func (s *VisualizerServer) buildComponentGraph() []CyElement {
	elements := make([]CyElement, 0)
	nodeIDSet := make(map[string]bool)

	storageDir := s.getStorageDir()
	intelPath := filepath.Join(storageDir, "intelligence", "latest.json")

	type IntelComponent struct {
		ID           string   `json:"id"`
		Name         string   `json:"name"`
		Kind         string   `json:"kind"`
		Directories  []string `json:"directories"`
		Dependencies []string `json:"dependencies"`
		Afferent     int      `json:"ca"`
		Efferent     int      `json:"ce"`
		Instability  float64  `json:"instability"`
		NodeIDs      []string `json:"node_ids"`
	}

	type IntelData struct {
		Components []IntelComponent `json:"components"`
	}

	var intel IntelData
	if data, err := os.ReadFile(intelPath); err == nil {
		_ = json.Unmarshal(data, &intel)
	}

	if len(intel.Components) > 0 {
		for _, comp := range intel.Components {
			layer := "common"
			if len(comp.Directories) > 0 {
				layer = detectLayer(comp.Directories[0], comp.Kind)
			}

			elements = append(elements, CyElement{
				Group: "nodes",
				Data: CyNodeData{
					ID:          comp.ID,
					Label:       comp.Name,
					Kind:        "COMPONENT",
					Layer:       layer,
					InDegree:    comp.Afferent,
					OutDegree:   comp.Efferent,
					Instability: comp.Instability,
					File:        strings.Join(comp.Directories, ", "),
				},
			})
			nodeIDSet[comp.ID] = true
		}

		// Add component dependency edges
		for _, comp := range intel.Components {
			for _, depID := range comp.Dependencies {
				if nodeIDSet[depID] && depID != comp.ID {
					edgeID := fmt.Sprintf("comp_edge_%s_%s", comp.ID, depID)
					elements = append(elements, CyElement{
						Group: "edges",
						Data: CyEdgeData{
							ID:     edgeID,
							Source: comp.ID,
							Target: depID,
							Label:  "DEPENDS_ON",
							Type:   "DEPENDENCY",
						},
					})
				}
			}
		}
	} else if s.graph != nil && s.graph.Nodes != nil {
		// Fallback: Group nodes by top packages
		pkgMap := make(map[string]int)
		pkgEdges := make(map[string]map[string]int)

		s.graph.Nodes.Iterate(func(_ string, n *link.ResolvedNode) {
			if n == nil || n.FileSpec.Path == "" {
				return
			}
			pkg := filepath.ToSlash(filepath.Dir(n.FileSpec.Path))
			if pkg != "." && pkg != "" {
				pkgMap[pkg]++
			}
		})

		for pkg, count := range pkgMap {
			elements = append(elements, CyElement{
				Group: "nodes",
				Data: CyNodeData{
					ID:        pkg,
					Label:     pkg,
					Kind:      "PACKAGE",
					Layer:     detectLayer(pkg, "PACKAGE"),
					InDegree:  count,
					OutDegree: 0,
				},
			})
			nodeIDSet[pkg] = true
		}

		if s.graph.OutboundEdges != nil {
			s.graph.OutboundEdges.Iterate(func(srcID string, es []link.ResolvedEdge) {
				srcNode, ok := s.graph.Nodes.Get(srcID)
				if !ok || srcNode == nil {
					return
				}
				srcPkg := filepath.ToSlash(filepath.Dir(srcNode.FileSpec.Path))

				for _, e := range es {
					tgtNode, ok := s.graph.Nodes.Get(e.TargetID)
					if !ok || tgtNode == nil {
						return
					}
					tgtPkg := filepath.ToSlash(filepath.Dir(tgtNode.FileSpec.Path))

					if srcPkg != tgtPkg && nodeIDSet[srcPkg] && nodeIDSet[tgtPkg] {
						if pkgEdges[srcPkg] == nil {
							pkgEdges[srcPkg] = make(map[string]int)
						}
						pkgEdges[srcPkg][tgtPkg]++
					}
				}
			})

			for src, targets := range pkgEdges {
				for tgt, weight := range targets {
					elements = append(elements, CyElement{
						Group: "edges",
						Data: CyEdgeData{
							ID:     fmt.Sprintf("pkg_edge_%s_%s", src, tgt),
							Source: src,
							Target: tgt,
							Label:  fmt.Sprintf("%d calls", weight),
							Type:   "CALLS",
							Weight: weight,
						},
					})
				}
			}
		}
	}

	return elements
}

// buildPackageGraph aggregates relations at the folder/package level.
func (s *VisualizerServer) buildPackageGraph() []CyElement {
	elements := make([]CyElement, 0)
	nodeIDSet := make(map[string]bool)
	pkgSymbols := make(map[string]int)
	pkgEdges := make(map[string]map[string]int)

	if s.graph != nil && s.graph.Nodes != nil {
		s.graph.Nodes.Iterate(func(_ string, n *link.ResolvedNode) {
			if n == nil || n.FileSpec.Path == "" {
				return
			}
			pkg := filepath.ToSlash(filepath.Dir(n.FileSpec.Path))
			if pkg != "" && pkg != "." {
				pkgSymbols[pkg]++
			}
		})

		for pkg, count := range pkgSymbols {
			elements = append(elements, CyElement{
				Group: "nodes",
				Data: CyNodeData{
					ID:       pkg,
					Label:    pkg,
					Kind:     "PACKAGE",
					Layer:    detectLayer(pkg, "PACKAGE"),
					InDegree: count,
				},
			})
			nodeIDSet[pkg] = true
		}

		if s.graph.OutboundEdges != nil {
			s.graph.OutboundEdges.Iterate(func(srcID string, es []link.ResolvedEdge) {
				srcNode, ok := s.graph.Nodes.Get(srcID)
				if !ok || srcNode == nil {
					return
				}
				srcPkg := filepath.ToSlash(filepath.Dir(srcNode.FileSpec.Path))

				for _, e := range es {
					tgtNode, ok := s.graph.Nodes.Get(e.TargetID)
					if !ok || tgtNode == nil {
						return
					}
					tgtPkg := filepath.ToSlash(filepath.Dir(tgtNode.FileSpec.Path))

					if srcPkg != tgtPkg && nodeIDSet[srcPkg] && nodeIDSet[tgtPkg] {
						if pkgEdges[srcPkg] == nil {
							pkgEdges[srcPkg] = make(map[string]int)
						}
						pkgEdges[srcPkg][tgtPkg]++
					}
				}
			})

			for src, targets := range pkgEdges {
				for tgt, weight := range targets {
					elements = append(elements, CyElement{
						Group: "edges",
						Data: CyEdgeData{
							ID:     fmt.Sprintf("pkg_%s_%s", src, tgt),
							Source: src,
							Target: tgt,
							Label:  fmt.Sprintf("%d references", weight),
							Type:   "REFERENCES",
							Weight: weight,
						},
					})
				}
			}
		}
	}

	return elements
}

// handleIntelligence serves .glassmarble/intelligence/latest.json
func (s *VisualizerServer) handleIntelligence(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	intelPath := filepath.Join(s.getStorageDir(), "intelligence", "latest.json")

	data, err := os.ReadFile(intelPath)
	if err != nil {
		w.Write([]byte(`{"metrics":{},"components":[],"patterns":[],"smells":[]}`))
		return
	}
	w.Write(data)
}

// handleTimeline serves .glassmarble/memory/timeline.json or memory.json
func (s *VisualizerServer) handleTimeline(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	timePath := filepath.Join(s.getStorageDir(), "memory", "timeline.json")

	data, err := os.ReadFile(timePath)
	if err != nil {
		memPath := filepath.Join(s.getStorageDir(), "memory", "memory.json")
		data, err = os.ReadFile(memPath)
		if err != nil {
			w.Write([]byte(`{"timeline":[]}`))
			return
		}
	}
	w.Write(data)
}

// handleConventions serves .glassmarble/memory/conventions.json
func (s *VisualizerServer) handleConventions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	convPath := filepath.Join(s.getStorageDir(), "memory", "conventions.json")

	data, err := os.ReadFile(convPath)
	if err != nil {
		w.Write([]byte(`{"conventions":[]}`))
		return
	}
	w.Write(data)
}

// handleSnapshots serves .glassmarble/snapshots/index.json
func (s *VisualizerServer) handleSnapshots(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	snapPath := filepath.Join(s.getStorageDir(), "snapshots", "index.json")

	data, err := os.ReadFile(snapPath)
	if err != nil {
		w.Write([]byte(`{"snapshots":[]}`))
		return
	}
	w.Write(data)
}

// handleMarbles lists or returns Markdown/Mermaid diagrams in .glassmarble/marbles/
func (s *VisualizerServer) handleMarbles(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	name := r.URL.Query().Get("name")
	marblesDir := filepath.Join(s.getStorageDir(), "marbles")

	if name != "" {
		// Return specific diagram content
		cleanName := filepath.Clean(filepath.Base(name))
		targetFile := filepath.Join(marblesDir, cleanName)
		if !strings.HasSuffix(targetFile, ".md") {
			targetFile += ".md"
		}

		content, err := os.ReadFile(targetFile)
		if err != nil {
			http.Error(w, `{"error":"diagram not found"}`, http.StatusNotFound)
			return
		}

		// Extract raw Mermaid block
		text := string(content)
		if start := strings.Index(text, "```mermaid"); start != -1 {
			text = text[start+len("```mermaid"):]
			if end := strings.Index(text, "```"); end != -1 {
				text = text[:end]
			}
		}

		json.NewEncoder(w).Encode(map[string]any{
			"name":    name,
			"mermaid": strings.TrimSpace(text),
			"raw":     string(content),
		})
		return
	}

	// List all available marble diagrams
	type MarbleItem struct {
		Name        string `json:"name"`
		Title       string `json:"title"`
		Type        string `json:"type"`
		Description string `json:"description"`
	}

	items := make([]MarbleItem, 0)
	entries, err := os.ReadDir(marblesDir)
	if err == nil {
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".md") {
				base := strings.TrimSuffix(entry.Name(), ".md")
				title := strings.ReplaceAll(base, "_", " ")
				diagType := "DIAGRAM"
				if strings.HasPrefix(base, "c4_") {
					diagType = "C4 ARCHITECTURE"
				} else if strings.HasPrefix(base, "uml_") {
					diagType = "UML SPEC"
				} else if strings.HasPrefix(base, "callgraph") {
					diagType = "CALL GRAPH"
				} else if strings.HasPrefix(base, "dataflow") {
					diagType = "DATA FLOW"
				}

				items = append(items, MarbleItem{
					Name:        entry.Name(),
					Title:       strings.ToUpper(title),
					Type:        diagType,
					Description: fmt.Sprintf("Pre-compiled %s diagram", title),
				})
			}
		}
	}

	json.NewEncoder(w).Encode(items)
}

func (s *VisualizerServer) handleSmells(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	intelPath := filepath.Join(s.getStorageDir(), "intelligence", "latest.json")

	if data, err := os.ReadFile(intelPath); err == nil {
		var intel struct {
			Smells []map[string]any `json:"smells"`
		}
		if json.Unmarshal(data, &intel) == nil && len(intel.Smells) > 0 {
			json.NewEncoder(w).Encode(intel.Smells)
			return
		}
	}

	var smells []map[string]any
	if s.graph != nil {
		var cyclicNodes []string
		if s.graph.OutboundEdges != nil {
			s.graph.OutboundEdges.Iterate(func(srcID string, es []link.ResolvedEdge) {
				for _, e := range es {
					if e.IsCycle {
						cyclicNodes = append(cyclicNodes, srcID, e.TargetID)
					}
				}
			})
		}
		if len(cyclicNodes) > 0 {
			smells = append(smells, map[string]any{
				"title":       "Architectural Dependency Cycle",
				"severity":    "HIGH",
				"description": "Cyclic dependencies detected between components violating DAG invariants.",
				"nodes":       cyclicNodes,
			})
		}
	}
	json.NewEncoder(w).Encode(smells)
}

func (s *VisualizerServer) handleLayers(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	layers := map[string]int{
		"presentation":   0,
		"application":    0,
		"domain":         0,
		"infrastructure": 0,
		"common":         0,
	}

	if s.graph != nil && s.graph.Nodes != nil {
		s.graph.Nodes.Iterate(func(_ string, n *link.ResolvedNode) {
			if n == nil {
				return
			}
			layer := detectLayer(n.FileSpec.Path, n.Kind)
			layers[layer]++
		})
	}

	json.NewEncoder(w).Encode(layers)
}

func (s *VisualizerServer) handleImpact(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	symbol := r.URL.Query().Get("symbol")
	nodeID := r.URL.Query().Get("id")

	target := symbol
	if nodeID != "" {
		target = nodeID
	}
	if target == "" {
		http.Error(w, `{"error":"symbol or id query parameter required"}`, http.StatusBadRequest)
		return
	}

	if s.graph == nil {
		http.Error(w, `{"error":"graph not loaded"}`, http.StatusInternalServerError)
		return
	}

	report, err := impact_analyzer.AnalyzeImpact(s.graph, target, impact_analyzer.ImpactOptions{IncludeTests: true})
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusNotFound)
		return
	}

	json.NewEncoder(w).Encode(report)
}

func (s *VisualizerServer) handleSearch(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	query := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))

	type SearchResult struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Kind     string `json:"kind"`
		File     string `json:"file"`
		Line     int    `json:"line"`
		InDegree int    `json:"in_degree"`
	}

	results := make([]SearchResult, 0)
	if s.graph == nil || s.graph.Nodes == nil || query == "" {
		json.NewEncoder(w).Encode(results)
		return
	}

	s.graph.Nodes.Iterate(func(id string, n *link.ResolvedNode) {
		if len(results) >= 20 || n == nil {
			return
		}
		if strings.HasPrefix(n.Kind, "CFG_") || strings.HasPrefix(n.Kind, "DFG_") {
			return
		}
		nameLower := strings.ToLower(n.Name)
		fileLower := strings.ToLower(n.FileSpec.Path)

		if strings.Contains(nameLower, query) || strings.Contains(fileLower, query) {
			results = append(results, SearchResult{
				ID:   n.ID,
				Name: n.Name,
				Kind: n.Kind,
				File: n.FileSpec.Path,
				Line: n.FileSpec.LineStart,
			})
		}
	})

	sort.Slice(results, func(i, j int) bool {
		return len(results[i].Name) < len(results[j].Name)
	})

	json.NewEncoder(w).Encode(results)
}

func (s *VisualizerServer) handlePaths(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	source := r.URL.Query().Get("source")
	target := r.URL.Query().Get("target")

	if source == "" || target == "" {
		http.Error(w, `{"error":"source and target parameters required"}`, http.StatusBadRequest)
		return
	}

	srcID := s.resolveSymbolID(source)
	tgtID := s.resolveSymbolID(target)

	if srcID == "" || tgtID == "" {
		json.NewEncoder(w).Encode(map[string]any{"found": false, "path": []string{}})
		return
	}

	path := s.findShortestPath(srcID, tgtID)
	json.NewEncoder(w).Encode(map[string]any{
		"found": len(path) > 0,
		"path":  path,
	})
}

func (s *VisualizerServer) resolveSymbolID(query string) string {
	if s.graph == nil || s.graph.Nodes == nil {
		return ""
	}
	if _, ok := s.graph.Nodes.Get(query); ok {
		return query
	}
	var matchID string
	s.graph.Nodes.Iterate(func(id string, n *link.ResolvedNode) {
		if matchID != "" || n == nil {
			return
		}
		if n.Name == query || strings.HasSuffix(n.ID, "::"+query) {
			matchID = n.ID
		}
	})
	return matchID
}

func (s *VisualizerServer) findShortestPath(srcID, tgtID string) []string {
	if srcID == tgtID {
		return []string{srcID}
	}
	if s.graph == nil || s.graph.OutboundEdges == nil {
		return nil
	}

	visited := make(map[string]bool)
	queue := [][]string{{srcID}}
	visited[srcID] = true

	for len(queue) > 0 {
		currPath := queue[0]
		queue = queue[1:]
		last := currPath[len(currPath)-1]

		if last == tgtID {
			return currPath
		}

		edges, ok := s.graph.OutboundEdges.Get(last)
		if !ok {
			continue
		}

		for _, e := range edges {
			if !visited[e.TargetID] {
				visited[e.TargetID] = true
				newPath := make([]string, len(currPath)+1)
				copy(newPath, currPath)
				newPath[len(currPath)] = e.TargetID
				queue = append(queue, newPath)
			}
		}
	}

	return nil
}

func detectLayer(filePath, kind string) string {
	lower := strings.ToLower(filePath)
	if strings.Contains(lower, "cmd/") || strings.Contains(lower, "cli") || strings.Contains(lower, "tui") || strings.Contains(lower, "handler") || strings.Contains(lower, "controller") {
		return "presentation"
	}
	if strings.Contains(lower, "service") || strings.Contains(lower, "usecase") || strings.Contains(lower, "workflow") || strings.Contains(lower, "engine") {
		return "application"
	}
	if strings.Contains(lower, "domain") || strings.Contains(lower, "entity") || strings.Contains(lower, "model") || strings.Contains(lower, "types") {
		return "domain"
	}
	if strings.Contains(lower, "infra") || strings.Contains(lower, "db") || strings.Contains(lower, "store") || strings.Contains(lower, "repository") || strings.Contains(lower, "network") || strings.Contains(lower, "storage") {
		return "infrastructure"
	}
	return "common"
}

// OpenBrowser launches the user's default browser pointing at the visualizer URL.
func OpenBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}

func (s *VisualizerServer) handleAlgoCycles(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if s.graph == nil {
		json.NewEncoder(w).Encode(map[string]any{"cycles": [][]string{}, "count": 0})
		return
	}
	cycles := s.graph.DetectCycles()
	json.NewEncoder(w).Encode(map[string]any{
		"cycles": cycles,
		"count":  len(cycles),
	})
}

func (s *VisualizerServer) handleAlgoToposort(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if s.graph == nil {
		json.NewEncoder(w).Encode(map[string]any{"sorted": []string{}, "is_dag": false})
		return
	}
	sorted, isDAG := s.graph.GetTopologicalSort()
	limit := 200
	if len(sorted) < limit {
		limit = len(sorted)
	}
	json.NewEncoder(w).Encode(map[string]any{
		"sorted": sorted[:limit],
		"total":  len(sorted),
		"is_dag": isDAG,
	})
}

func (s *VisualizerServer) handleAlgoCutVertices(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if s.graph == nil {
		json.NewEncoder(w).Encode(map[string]any{"articulation_points": []any{}, "count": 0})
		return
	}
	aps := s.graph.FindArticulationPoints()
	type APInfo struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		File string `json:"file"`
		Kind string `json:"kind"`
	}
	var apList []APInfo
	for _, id := range aps {
		if n, ok := s.graph.Nodes.Get(id); ok && n != nil {
			apList = append(apList, APInfo{
				ID:   n.ID,
				Name: n.Name,
				File: n.FileSpec.Path,
				Kind: n.Kind,
			})
		}
	}
	json.NewEncoder(w).Encode(map[string]any{
		"articulation_points": apList,
		"count":               len(apList),
	})
}

func (s *VisualizerServer) handleAlgoPageRank(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if s.graph == nil {
		json.NewEncoder(w).Encode(map[string]any{"ranks": []any{}})
		return
	}
	ranks := s.graph.CalculatePageRank(20, 0.85)
	type RankEntry struct {
		ID    string  `json:"id"`
		Name  string  `json:"name"`
		Score float64 `json:"score"`
		File  string  `json:"file"`
		Kind  string  `json:"kind"`
	}
	var list []RankEntry
	for id, score := range ranks {
		if n, ok := s.graph.Nodes.Get(id); ok && n != nil {
			list = append(list, RankEntry{
				ID:    n.ID,
				Name:  n.Name,
				Score: score,
				File:  n.FileSpec.Path,
				Kind:  n.Kind,
			})
		}
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].Score > list[j].Score
	})
	if len(list) > 50 {
		list = list[:50]
	}
	json.NewEncoder(w).Encode(map[string]any{
		"ranks": list,
	})
}

func (s *VisualizerServer) handleAlgoSimilarity(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	nodeA := r.URL.Query().Get("nodeA")
	nodeB := r.URL.Query().Get("nodeB")
	if s.graph == nil || nodeA == "" || nodeB == "" {
		json.NewEncoder(w).Encode(map[string]any{"similarity": 0.0})
		return
	}
	sim := s.graph.GetStructuralSimilarity(nodeA, nodeB)
	json.NewEncoder(w).Encode(map[string]any{
		"nodeA":      nodeA,
		"nodeB":      nodeB,
		"similarity": sim,
	})
}

func (s *VisualizerServer) handleAlgoOrphans(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if s.graph == nil {
		json.NewEncoder(w).Encode(map[string]any{"orphans": []string{}, "count": 0})
		return
	}
	orphans := s.graph.GetOrphanNodes()
	json.NewEncoder(w).Encode(map[string]any{
		"orphans": orphans,
		"count":   len(orphans),
	})
}
