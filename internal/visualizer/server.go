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
	"runtime"
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
	if s.graph != nil && s.graph.Nodes != nil {
		nodeCount = s.graph.Nodes.Len()
	}
	if s.graph != nil && s.graph.OutboundEdges != nil {
		edgeCount = s.graph.OutboundEdges.Len()
	}

	res := map[string]any{
		"status":         "healthy",
		"nodes_count":    nodeCount,
		"edges_count":    edgeCount,
		"schema_version": s.graph.SchemaVersion,
		"commit_hash":    s.graph.CommitHash,
	}
	json.NewEncoder(w).Encode(res)
}

func (s *VisualizerServer) handleGraph(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	type webNode struct {
		ID       string            `json:"id"`
		Name     string            `json:"name"`
		Kind     string            `json:"kind"`
		FileSpec link.LocationMeta `json:"file_spec"`
	}

	type webEdge struct {
		SourceID string `json:"source_id"`
		TargetID string `json:"target_id"`
		Type     string `json:"type"`
	}

	nodes := make([]webNode, 0)
	edges := make([]webEdge, 0)

	if s.graph != nil && s.graph.Nodes != nil {
		limit := 300 // Cap for snappy browser canvas simulation
		s.graph.Nodes.Iterate(func(id string, n *link.ResolvedNode) {
			if len(nodes) < limit && n != nil {
				nodes = append(nodes, webNode{
					ID:       n.ID,
					Name:     n.Name,
					Kind:     n.Kind,
					FileSpec: n.FileSpec,
				})
			}
		})
	}

	if s.graph != nil && s.graph.OutboundEdges != nil {
		edgeLimit := 600
		s.graph.OutboundEdges.Iterate(func(srcID string, es []link.ResolvedEdge) {
			for _, e := range es {
				if len(edges) < edgeLimit {
					edges = append(edges, webEdge{
						SourceID: e.SourceID,
						TargetID: e.TargetID,
						Type:     string(e.Type),
					})
				}
			}
		})
	}

	payload := map[string]any{
		"nodes": nodes,
		"edges": edges,
	}
	json.NewEncoder(w).Encode(payload)
}

func (s *VisualizerServer) handleImpact(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	symbol := r.URL.Query().Get("symbol")
	if symbol == "" {
		http.Error(w, `{"error":"symbol parameter required"}`, http.StatusBadRequest)
		return
	}

	rep, err := impact_analyzer.AnalyzeImpact(s.graph, symbol, impact_analyzer.ImpactOptions{MaxDepth: 10})
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusNotFound)
		return
	}

	json.NewEncoder(w).Encode(rep)
}

func (s *VisualizerServer) handleSearch(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	q := strings.ToLower(r.URL.Query().Get("q"))

	var matches []map[string]string
	if s.graph != nil && s.graph.Nodes != nil && q != "" {
		s.graph.Nodes.Iterate(func(id string, n *link.ResolvedNode) {
			if len(matches) < 15 && n != nil {
				if strings.Contains(strings.ToLower(n.Name), q) || strings.Contains(strings.ToLower(n.FileSpec.Path), q) {
					matches = append(matches, map[string]string{
						"id":   n.ID,
						"name": n.Name,
						"kind": n.Kind,
						"file": n.FileSpec.Path,
					})
				}
			}
		})
	}

	json.NewEncoder(w).Encode(matches)
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
