package learning

import (
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage4"
	"github.com/Syamchand123/GlassMarble/internal/developer_memory"
)

type ProjectConventions struct {
	ServiceNamingPattern string
	LayerDirectories     []string
	TestFilePattern      string
	ADRDirectory         string
	PreferredPatterns    []string
	RejectedPatterns     []string
}

func LearnConventions(graph *akg.CodePropertyGraph, memory *developer_memory.DeveloperMemory) *ProjectConventions {
	conv := &ProjectConventions{}

	// Test File Pattern Detection
	testSuffixCounts := make(map[string]int)
	if graph.FileNodeIndex != nil {
		graph.FileNodeIndex.Iterate(func(file string, _ map[string]bool) {
			if strings.HasSuffix(file, "_test.go") {
				testSuffixCounts["*_test.go"]++
			} else if strings.HasSuffix(file, ".spec.ts") {
				testSuffixCounts["*.spec.ts"]++
			} else if strings.HasSuffix(file, ".test.js") {
				testSuffixCounts["*.test.js"]++
			}
		})
	}
	
	bestTestCount := 0
	for suffix, count := range testSuffixCounts {
		if count > bestTestCount {
			bestTestCount = count
			conv.TestFilePattern = suffix
		}
	}

	// Service Naming Pattern Detection
	suffixCounts := make(map[string]int)
	if graph.Nodes != nil {
		graph.Nodes.Iterate(func(_ string, node *stage4.ResolvedNode) {
			if node.Kind == "MODULE" || node.Kind == "STRUCT" {
				if strings.HasSuffix(node.Name, "Service") {
					suffixCounts["*Service"]++
				} else if strings.HasSuffix(node.Name, "Handler") {
					suffixCounts["*Handler"]++
				} else if strings.HasSuffix(node.Name, "Controller") {
					suffixCounts["*Controller"]++
				}
			}
		})
	}

	bestServiceCount := 0
	for suffix, count := range suffixCounts {
		if count > bestServiceCount {
			bestServiceCount = count
			conv.ServiceNamingPattern = suffix
		}
	}

	// Layer Directories
	layerDirs := make(map[string]bool)
	if graph.FileNodeIndex != nil {
		graph.FileNodeIndex.Iterate(func(file string, _ map[string]bool) {
			parts := strings.Split(file, "/")
			for _, part := range parts {
				switch part {
				case "domain", "core", "infrastructure", "api", "handlers", "controllers", "services", "repository":
					layerDirs[part] = true
				}
			}
		})
	}
	for dir := range layerDirs {
		conv.LayerDirectories = append(conv.LayerDirectories, dir)
	}

	// ADR Directory
	if graph.FileNodeIndex != nil {
		graph.FileNodeIndex.Iterate(func(file string, _ map[string]bool) {
			if strings.Contains(file, "docs/adr") || strings.Contains(file, "docs/decisions") {
				parts := strings.Split(file, "/")
				if len(parts) >= 2 {
					conv.ADRDirectory = parts[0] + "/" + parts[1]
				}
			}
		})
	}

	return conv
}
