package visualizer

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"net/http"
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

// Start boots the HTTP server on an available port and optionally launches the browser.
func (s *VisualizerServer) Start(ctx context.Context) (int, error) {
	mux := http.NewServeMux()

	// API Handlers
	mux.HandleFunc("/api/health", s.handleHealth)
	mux.HandleFunc("/api/graph", s.handleGraph)
	mux.HandleFunc("/api/status", s.handleStatus)
	mux.HandleFunc("/api/impact", s.handleImpact)
	mux.HandleFunc("/api/search", s.handleSearch)
	mux.HandleFunc("/api/smells", s.handleSmells)
	mux.HandleFunc("/api/layers", s.handleLayers)
	mux.HandleFunc("/api/paths", s.handlePaths)

	// Static Assets
	assetSub, err := fs.Sub(assetsFS, "assets")
	if err != nil {
		return 0, fmt.Errorf("failed to load embedded assets: %w", err)
	}

	fileServer := http.FileServer(http.FS(assetSub))
	mux.Handle("/assets/", http.StripPrefix("/assets/", fileServer))
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
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(data)
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
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
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

// Stop gracefully shuts down the visualizer server.
func (s *VisualizerServer) Stop() error {
	if s.server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		return s.server.Shutdown(ctx)
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
			edgeCount = s.graph.OutboundEdges.Len()
		}
		if s.graph.FileNodeIndex != nil {
			filesCount = s.graph.FileNodeIndex.Len()
		}
	}

	res := map[string]any{
		"status":         "healthy",
		"nodes_count":    nodeCount,
		"edges_count":    edgeCount,
		"files_count":    filesCount,
		"smells_count":   0,
		"schema_version": s.graph.SchemaVersion,
		"commit_hash":    s.graph.CommitHash,
	}
	json.NewEncoder(w).Encode(res)
}

func (s *VisualizerServer) handleGraph(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Cytoscape element structures
	type CyNodeData struct {
		ID         string  `json:"id"`
		Label      string  `json:"label"`
		Kind       string  `json:"kind"`
		File       string  `json:"file,omitempty"`
		Line       int     `json:"line,omitempty"`
		Parent     string  `json:"parent,omitempty"`
		Layer      string  `json:"layer,omitempty"`
		PageRank   float64 `json:"pagerank,omitempty"`
		InDegree   int     `json:"in_degree"`
		OutDegree  int     `json:"out_degree"`
		IsEntry    bool    `json:"is_entry"`
		IsTest     bool    `json:"is_test"`
		IsCompound bool    `json:"is_compound"`
	}

	type CyEdgeData struct {
		ID       string `json:"id"`
		Source   string `json:"source"`
		Target   string `json:"target"`
		Label    string `json:"label"`
		Type     string `json:"type"`
		IsCycle  bool   `json:"is_cycle"`
		Line     int    `json:"line,omitempty"`
	}

	type CyElement struct {
		Group string      `json:"group"` // "nodes" or "edges"
		Data  any         `json:"data"`
	}

	elements := make([]CyElement, 0)
	parentPackages := make(map[string]bool)

	// Collect degrees
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

	nodeLimit := 500 // Snappy graph limit
	includedNodeIDs := make(map[string]bool)

	if s.graph != nil && s.graph.Nodes != nil {
		s.graph.Nodes.Iterate(func(id string, n *link.ResolvedNode) {
			if len(includedNodeIDs) >= nodeLimit || n == nil {
				return
			}

			// Filter out internal DFG/CFG synthetic nodes for clean architecture view
			if strings.HasPrefix(n.Kind, "CFG_") || strings.HasPrefix(n.Kind, "DFG_") {
				return
			}

			filePath := filepath.ToSlash(n.FileSpec.Path)
			pkgDir := ""
			if filePath != "" {
				pkgDir = filepath.ToSlash(filepath.Dir(filePath))
				if pkgDir != "." && pkgDir != "" {
					parentPackages[pkgDir] = true
				}
			}

			pr := 0.0
			if val, ok := n.Properties["gm:pagerank"]; ok {
				pr, _ = strconv.ParseFloat(val, 64)
			}

			layer := detectLayer(filePath, n.Kind)
			isEntry := n.Kind == "ENTRYPOINT" || n.Name == "main"
			isTest := strings.HasSuffix(filePath, "_test.go") || strings.HasSuffix(filePath, ".test.ts")

			nodeData := CyNodeData{
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
			}

			elements = append(elements, CyElement{
				Group: "nodes",
				Data:  nodeData,
			})
			includedNodeIDs[n.ID] = true
		})
	}

	// Add compound parent nodes (packages / directories)
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
	}

	// Add edges
	edgeLimit := 1000
	edgeCount := 0
	if s.graph != nil && s.graph.OutboundEdges != nil {
		s.graph.OutboundEdges.Iterate(func(srcID string, es []link.ResolvedEdge) {
			if !includedNodeIDs[srcID] {
				return
			}
			for _, e := range es {
				if edgeCount >= edgeLimit {
					return
				}
				if !includedNodeIDs[e.TargetID] || e.TargetID == srcID {
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
				edgeCount++
			}
		})
	}

	json.NewEncoder(w).Encode(elements)
}

func (s *VisualizerServer) handleSmells(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
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
	layers := []map[string]string{
		{"id": "presentation", "name": "Presentation / CLI / API", "color": "#06b6d4"},
		{"id": "application", "name": "Application / Service", "color": "#8b5cf6"},
		{"id": "domain", "name": "Domain / Model / Entities", "color": "#10b981"},
		{"id": "infrastructure", "name": "Infrastructure / DB / Network", "color": "#d946ef"},
		{"id": "common", "name": "Common / Utilities", "color": "#64748b"},
	}
	json.NewEncoder(w).Encode(layers)
}

func (s *VisualizerServer) handlePaths(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	src := r.URL.Query().Get("source")
	tgt := r.URL.Query().Get("target")

	if src == "" || tgt == "" {
		http.Error(w, `{"error":"source and target parameters required"}`, http.StatusBadRequest)
		return
	}

	// BFS path finding
	pathNodes := s.findShortestPath(src, tgt)
	json.NewEncoder(w).Encode(map[string]any{
		"source": src,
		"target": tgt,
		"path":   pathNodes,
		"found":  len(pathNodes) > 0,
	})
}

func (s *VisualizerServer) findShortestPath(srcQuery, tgtQuery string) []string {
	if s.graph == nil || s.graph.Nodes == nil {
		return nil
	}

	srcNode := s.matchNode(srcQuery)
	tgtNode := s.matchNode(tgtQuery)
	if srcNode == nil || tgtNode == nil {
		return nil
	}

	queue := [][]string{{srcNode.ID}}
	visited := make(map[string]bool)
	visited[srcNode.ID] = true

	for len(queue) > 0 {
		currPath := queue[0]
		queue = queue[1:]

		tail := currPath[len(currPath)-1]
		if tail == tgtNode.ID {
			return currPath
		}

		outEdges, ok := s.graph.OutboundEdges.Get(tail)
		if !ok {
			continue
		}

		for _, edge := range outEdges {
			nextID := edge.TargetID
			if !visited[nextID] {
				visited[nextID] = true
				newPath := append(append([]string(nil), currPath...), nextID)
				queue = append(queue, newPath)
			}
		}
	}

	return nil
}

func (s *VisualizerServer) matchNode(query string) *link.ResolvedNode {
	clean := strings.TrimSpace(query)
	if n, ok := s.graph.Nodes.Get(clean); ok && n != nil {
		return n
	}

	var match *link.ResolvedNode
	s.graph.Nodes.Iterate(func(id string, n *link.ResolvedNode) {
		if match != nil || n == nil {
			return
		}
		if strings.EqualFold(n.Name, clean) || strings.EqualFold(n.ID, clean) {
			match = n
		}
	})
	return match
}

func (s *VisualizerServer) handleImpact(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	symbol := r.URL.Query().Get("symbol")
	if symbol == "" {
		http.Error(w, `{"error":"symbol parameter required"}`, http.StatusBadRequest)
		return
	}

	depth := 10
	if dStr := r.URL.Query().Get("depth"); dStr != "" {
		if d, err := strconv.Atoi(dStr); err == nil && d > 0 {
			depth = d
		}
	}

	rep, err := impact_analyzer.AnalyzeImpact(s.graph, symbol, impact_analyzer.ImpactOptions{MaxDepth: depth})
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusNotFound)
		return
	}

	json.NewEncoder(w).Encode(rep)
}

func (s *VisualizerServer) handleSearch(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	q := strings.ToLower(r.URL.Query().Get("q"))

	var matches []map[string]any
	if s.graph != nil && s.graph.Nodes != nil && q != "" {
		s.graph.Nodes.Iterate(func(id string, n *link.ResolvedNode) {
			if len(matches) < 20 && n != nil {
				if strings.Contains(strings.ToLower(n.Name), q) || strings.Contains(strings.ToLower(n.FileSpec.Path), q) {
					matches = append(matches, map[string]any{
						"id":   n.ID,
						"name": n.Name,
						"kind": n.Kind,
						"file": n.FileSpec.Path,
						"line": n.FileSpec.LineStart,
					})
				}
			}
		})
	}

	// Sort matches by exact prefix relevance
	sort.Slice(matches, func(i, j int) bool {
		nameI := strings.ToLower(matches[i]["name"].(string))
		nameJ := strings.ToLower(matches[j]["name"].(string))
		prefI := strings.HasPrefix(nameI, q)
		prefJ := strings.HasPrefix(nameJ, q)
		if prefI != prefJ {
			return prefI
		}
		return nameI < nameJ
	})

	json.NewEncoder(w).Encode(matches)
}

func detectLayer(path, kind string) string {
	lower := strings.ToLower(path)
	switch {
	case strings.Contains(lower, "/cmd") || strings.Contains(lower, "/api") || strings.Contains(lower, "/controller") || strings.Contains(lower, "/handler") || strings.Contains(lower, "/ui") || strings.Contains(lower, "/tui"):
		return "presentation"
	case strings.Contains(lower, "/service") || strings.Contains(lower, "/usecase") || strings.Contains(lower, "/app"):
		return "application"
	case strings.Contains(lower, "/domain") || strings.Contains(lower, "/model") || strings.Contains(lower, "/entity") || strings.Contains(lower, "/types"):
		return "domain"
	case strings.Contains(lower, "/db") || strings.Contains(lower, "/repo") || strings.Contains(lower, "/storage") || strings.Contains(lower, "/infra") || strings.Contains(lower, "/client") || kind == "DATABASE":
		return "infrastructure"
	default:
		return "common"
	}
}

// OpenBrowser launches the default browser pointing to url.
func OpenBrowser(url string) error {
	var cmd string
	var args []string

	switch runtime.GOOS {
	case "windows":
		cmd = "rundll32"
		args = []string{"url.dll,FileProtocolHandler", url}
	case "darwin":
		cmd = "open"
		args = []string{url}
	default:
		cmd = "xdg-open"
		args = []string{url}
	}

	return exec.Command(cmd, args...).Start()
}
