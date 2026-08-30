package impact_analyzer

import (
	"fmt"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/link"
)

// ImpactNode represents a symbol or node affected by changes to the target.
type ImpactNode struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	File      string `json:"file"`
	Line      int    `json:"line"`
	Distance  int    `json:"distance"` // 1 = Direct caller, 2..N = Transitive
	EdgeType  string `json:"edge_type,omitempty"`
	IsTest    bool   `json:"is_test"`
	IsEntry   bool   `json:"is_entrypoint"`
}

// ImpactReport contains the complete blast-radius and architectural risk assessment.
type ImpactReport struct {
	TargetQuery               string       `json:"target_query"`
	TargetNodeID              string       `json:"target_node_id"`
	TargetName                string       `json:"target_name"`
	TargetKind                string       `json:"target_kind"`
	TargetFile                string       `json:"target_file"`
	RiskScore                 int          `json:"risk_score"`  // 0 - 100
	RiskLevel                 string       `json:"risk_level"`  // LOW, MEDIUM, HIGH, CRITICAL
	DirectDependentsCount     int          `json:"direct_dependents_count"`
	TransitiveDependentsCount int          `json:"transitive_dependents_count"`
	TotalImpactedNodes        int          `json:"total_impacted_nodes"`
	TotalImpactedFiles        int          `json:"total_impacted_files"`
	ImpactedFiles             []string     `json:"impacted_files"`
	DirectDependents          []ImpactNode `json:"direct_dependents"`
	TransitiveDependents      []ImpactNode `json:"transitive_dependents"`
	ImpactedTestFiles         []string     `json:"impacted_test_files"`
	ImpactedEntrypoints       []string     `json:"impacted_entrypoints"`
	RecommendedTestCommand    string       `json:"recommended_test_command,omitempty"`
	MaxDepthReached           int          `json:"max_depth_reached"`
}

// ImpactOptions configures blast-radius calculation.
type ImpactOptions struct {
	MaxDepth      int      // Maximum reverse traversal depth (0 = unlimited)
	IncludeTests  bool     // Whether to include test nodes in general impact
	TestsOnly     bool     // Filter output exclusively to impacted test suites
	ExcludeFiles  []string // Glob patterns of files to exclude from traversal
}

// AnalyzeImpact computes the topological reverse dependency closure for targetQuery.
func AnalyzeImpact(graph *akg.CodePropertyGraph, targetQuery string, opts ImpactOptions) (*ImpactReport, error) {
	if graph == nil || graph.Nodes == nil || graph.Nodes.Len() == 0 {
		return nil, fmt.Errorf("architecture knowledge graph is empty")
	}

	targetNode := findTargetNode(graph, targetQuery)
	if targetNode == nil {
		return nil, fmt.Errorf("symbol or file %q not found in architecture knowledge graph", targetQuery)
	}

	rep := &ImpactReport{
		TargetQuery:  targetQuery,
		TargetNodeID: targetNode.ID,
		TargetName:   targetNode.Name,
		TargetKind:   targetNode.Kind,
		TargetFile:   filepath.ToSlash(targetNode.FileSpec.Path),
	}

	// Reverse BFS queue: [nodeID, distance]
	type queueItem struct {
		nodeID   string
		distance int
		edgeType string
	}

	queue := []queueItem{{nodeID: targetNode.ID, distance: 0, edgeType: ""}}
	visited := make(map[string]int) // nodeID -> shortest distance
	visited[targetNode.ID] = 0

	impactedFilesMap := make(map[string]bool)
	impactedTestsMap := make(map[string]bool)
	impactedEntriesMap := make(map[string]bool)

	if targetNode.FileSpec.Path != "" {
		impactedFilesMap[filepath.ToSlash(targetNode.FileSpec.Path)] = true
	}

	maxDepth := opts.MaxDepth
	if maxDepth <= 0 {
		maxDepth = 50 // Safe upper bound
	}

	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]

		if curr.distance >= maxDepth {
			continue
		}

		inEdges, ok := graph.InboundEdges.Get(curr.nodeID)
		if !ok || len(inEdges) == 0 {
			continue
		}

		for _, edge := range inEdges {
			srcID := edge.SourceID
			if srcID == "" || srcID == curr.nodeID {
				continue
			}

			newDist := curr.distance + 1
			prevDist, alreadyVisited := visited[srcID]
			if alreadyVisited && prevDist <= newDist {
				continue
			}
			visited[srcID] = newDist

			srcNode, exists := graph.Nodes.Get(srcID)
			if !exists || srcNode == nil {
				continue
			}

			srcFile := filepath.ToSlash(srcNode.FileSpec.Path)
			if isExcludedFile(srcFile, opts.ExcludeFiles) {
				continue
			}

			isTest := isTestFile(srcFile) || strings.HasPrefix(srcNode.Name, "Test")
			isEntry := isEntrypoint(srcNode, graph.Entrypoints)

			item := ImpactNode{
				ID:       srcNode.ID,
				Name:     srcNode.Name,
				Kind:     srcNode.Kind,
				File:     srcFile,
				Line:     srcNode.FileSpec.LineStart,
				Distance: newDist,
				EdgeType: string(edge.Type),
				IsTest:   isTest,
				IsEntry:  isEntry,
			}

			if newDist > rep.MaxDepthReached {
				rep.MaxDepthReached = newDist
			}

			if newDist == 1 {
				rep.DirectDependents = append(rep.DirectDependents, item)
			} else {
				rep.TransitiveDependents = append(rep.TransitiveDependents, item)
			}

			if srcFile != "" {
				impactedFilesMap[srcFile] = true
				if isTest {
					impactedTestsMap[srcFile] = true
				}
			}

			if isEntry {
				impactedEntriesMap[srcNode.Name+" ("+srcFile+")"] = true
			}

			queue = append(queue, queueItem{
				nodeID:   srcID,
				distance: newDist,
				edgeType: string(edge.Type),
			})
		}
	}

	rep.DirectDependentsCount = len(rep.DirectDependents)
	rep.TransitiveDependentsCount = len(rep.TransitiveDependents)
	rep.TotalImpactedNodes = rep.DirectDependentsCount + rep.TransitiveDependentsCount

	for f := range impactedFilesMap {
		rep.ImpactedFiles = append(rep.ImpactedFiles, f)
	}
	sort.Strings(rep.ImpactedFiles)
	rep.TotalImpactedFiles = len(rep.ImpactedFiles)

	for t := range impactedTestsMap {
		rep.ImpactedTestFiles = append(rep.ImpactedTestFiles, t)
	}
	sort.Strings(rep.ImpactedTestFiles)

	for e := range impactedEntriesMap {
		rep.ImpactedEntrypoints = append(rep.ImpactedEntrypoints, e)
	}
	sort.Strings(rep.ImpactedEntrypoints)

	// Sort dependents by distance, then name
	sort.Slice(rep.DirectDependents, func(i, j int) bool {
		return rep.DirectDependents[i].Name < rep.DirectDependents[j].Name
	})
	sort.Slice(rep.TransitiveDependents, func(i, j int) bool {
		if rep.TransitiveDependents[i].Distance != rep.TransitiveDependents[j].Distance {
			return rep.TransitiveDependents[i].Distance < rep.TransitiveDependents[j].Distance
		}
		return rep.TransitiveDependents[i].Name < rep.TransitiveDependents[j].Name
	})

	// Calculate Architectural Risk Score (0 - 100)
	rawScore := (rep.DirectDependentsCount * 8) +
		(rep.TransitiveDependentsCount * 2) +
		(rep.TotalImpactedFiles * 4) +
		(len(rep.ImpactedEntrypoints) * 12)

	if rawScore > 100 {
		rep.RiskScore = 100
	} else if rawScore < 5 && rep.TotalImpactedNodes > 0 {
		rep.RiskScore = 5
	} else {
		rep.RiskScore = rawScore
	}

	switch {
	case rep.RiskScore >= 75:
		rep.RiskLevel = "CRITICAL"
	case rep.RiskScore >= 50:
		rep.RiskLevel = "HIGH"
	case rep.RiskScore >= 25:
		rep.RiskLevel = "MEDIUM"
	default:
		rep.RiskLevel = "LOW"
	}

	// Recommended test command
	if len(rep.ImpactedTestFiles) > 0 {
		pkgDirs := make(map[string]bool)
		for _, tf := range rep.ImpactedTestFiles {
			pkgDirs["./"+path.Dir(tf)] = true
		}
		var dirs []string
		for d := range pkgDirs {
			dirs = append(dirs, d)
		}
		sort.Strings(dirs)
		rep.RecommendedTestCommand = "go test " + strings.Join(dirs, " ")
	}

	return rep, nil
}

func findTargetNode(graph *akg.CodePropertyGraph, query string) *link.ResolvedNode {
	cleanQuery := strings.TrimSpace(query)
	cleanPath := filepath.ToSlash(cleanQuery)

	// 1. Exact node ID match
	if n, ok := graph.Nodes.Get(cleanQuery); ok && n != nil {
		return n
	}

	var exactNameMatch *link.ResolvedNode
	var exactFileMatch *link.ResolvedNode
	var substringNameMatch *link.ResolvedNode

	graph.Nodes.Iterate(func(id string, node *link.ResolvedNode) {
		if node == nil {
			return
		}

		if strings.EqualFold(node.Name, cleanQuery) {
			exactNameMatch = node
			return
		}

		if node.FileSpec.Path != "" {
			nodeFile := filepath.ToSlash(node.FileSpec.Path)
			if nodeFile == cleanPath || strings.HasSuffix(nodeFile, "/"+cleanPath) {
				if exactFileMatch == nil {
					exactFileMatch = node
				}
			}
		}

		if substringNameMatch == nil && strings.Contains(strings.ToLower(node.Name), strings.ToLower(cleanQuery)) {
			substringNameMatch = node
		}
	})

	if exactNameMatch != nil {
		return exactNameMatch
	}
	if exactFileMatch != nil {
		return exactFileMatch
	}
	return substringNameMatch
}

func isTestFile(p string) bool {
	lower := strings.ToLower(p)
	return strings.HasSuffix(lower, "_test.go") ||
		strings.HasSuffix(lower, ".test.ts") ||
		strings.HasSuffix(lower, ".test.js") ||
		strings.HasSuffix(lower, ".spec.ts") ||
		strings.HasSuffix(lower, "_spec.rb") ||
		strings.HasPrefix(filepath.Base(lower), "test_") ||
		strings.HasSuffix(lower, "test.java")
}

func isEntrypoint(node *link.ResolvedNode, entrypoints []string) bool {
	if node.Name == "main" || node.Kind == "ENTRYPOINT" {
		return true
	}
	for _, ep := range entrypoints {
		if ep == node.ID || ep == node.Name {
			return true
		}
	}
	return false
}

func isExcludedFile(p string, excludes []string) bool {
	for _, excl := range excludes {
		if strings.Contains(p, excl) {
			return true
		}
	}
	return false
}
