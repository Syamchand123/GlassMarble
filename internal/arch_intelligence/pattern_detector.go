package arch_intelligence

import (
	"path/filepath"
	"strings"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/archmodel"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage4"
	"github.com/Syamchand123/GlassMarble/internal/evidence"
)

// PatternRule defines a deterministic rule for identifying an architectural pattern.
type PatternRule interface {
	ID() string
	Name() string
	Evaluate(graph *akg.CodePropertyGraph, metrics archmodel.ArchMetrics) *archmodel.DetectedPattern
}

// PR01LayeredArchitecture detects layered architectures by checking dependency direction
// across standard directory boundaries (e.g. cmd/ -> internal/api/ -> internal/domain/).
type PR01LayeredArchitecture struct{}

func (r *PR01LayeredArchitecture) ID() string   { return "PR-01" }
func (r *PR01LayeredArchitecture) Name() string { return "Layered Architecture" }
func (r *PR01LayeredArchitecture) Evaluate(graph *akg.CodePropertyGraph, metrics archmodel.ArchMetrics) *archmodel.DetectedPattern {
	layerNodes := make(map[string][]string) // layer name -> node IDs

	graph.Nodes.Iterate(func(id string, node *stage4.ResolvedNode) {
		if node.Kind != "FILE" && node.Kind != "PACKAGE" && node.FileSpec.Path != "" {
			dir := filepath.Dir(node.FileSpec.Path)
			dir = strings.ReplaceAll(dir, "\\", "/")

			// Simple heuristic layer bucketing based on path
			if strings.Contains(dir, "/cmd/") || strings.HasPrefix(dir, "cmd/") {
				layerNodes["UI/CLI"] = append(layerNodes["UI/CLI"], id)
			} else if strings.Contains(dir, "/api/") || strings.Contains(dir, "/handler/") {
				layerNodes["App/API"] = append(layerNodes["App/API"], id)
			} else if strings.Contains(dir, "/domain/") || strings.Contains(dir, "/core/") {
				layerNodes["Domain"] = append(layerNodes["Domain"], id)
			} else if strings.Contains(dir, "/infra/") || strings.Contains(dir, "/repository/") || strings.Contains(dir, "/db/") {
				layerNodes["Infrastructure"] = append(layerNodes["Infrastructure"], id)
			}
		}
	})

	if len(layerNodes) < 3 {
		return nil // Not enough layers for a definitive pattern
	}

	// Check dependencies between layers
	// Expected: UI -> App -> Domain -> Infra
	// Or clean arch: UI -> App -> Domain <- Infra (Handled in PR-02)
	// For basic layered, we just want to see clear unidirectional flow.

	totalEdges := 0
	violationEdges := 0

	layerOrder := map[string]int{
		"UI/CLI":         0,
		"App/API":        1,
		"Domain":         2,
		"Infrastructure": 3, // In traditional layered, domain depends on infra
	}

	nodeToLayer := make(map[string]string)
	for l, nodes := range layerNodes {
		for _, id := range nodes {
			nodeToLayer[id] = l
		}
	}

	graph.OutboundEdges.Iterate(func(sourceID string, edges []stage4.ResolvedEdge) {
		srcLayer, ok1 := nodeToLayer[sourceID]
		if !ok1 {
			return
		}
		for _, e := range edges {
			if !isStructuralEdge(e.Type) {
				continue
			}
			dstLayer, ok2 := nodeToLayer[e.TargetID]
			if !ok2 || srcLayer == dstLayer {
				continue
			}

			totalEdges++
			if layerOrder[srcLayer] > layerOrder[dstLayer] {
				violationEdges++
			}
		}
	})

	if totalEdges == 0 {
		return nil
	}

	consistency := float64(totalEdges-violationEdges) / float64(totalEdges)

	if consistency > 0.8 {
		b := evidence.Bundle{
			PrimarySource: evidence.SourceRule,
		}
		b.Add(evidence.EvidenceItem{
			Source:     evidence.SourceRule,
			Reference:  "PR-01",
			Excerpt:    "Dependencies flow from outer layers to inner layers with high consistency.",
			Confidence: consistency,
			Timestamp:  time.Now(),
		})

		return &archmodel.DetectedPattern{
			Kind:        archmodel.PatternLayered,
			Name:        "Layered Architecture",
			Confidence:  consistency,
			Evidence:    b,
			Description: "The system exhibits a layered structure where dependencies consistently flow downwards.",
		}
	}

	return nil
}

// PR02CleanArchitecture extends Layered Architecture by checking Domain dependency inversion.
type PR02CleanArchitecture struct{}

func (r *PR02CleanArchitecture) ID() string   { return "PR-02" }
func (r *PR02CleanArchitecture) Name() string { return "Clean Architecture" }
func (r *PR02CleanArchitecture) Evaluate(graph *akg.CodePropertyGraph, metrics archmodel.ArchMetrics) *archmodel.DetectedPattern {
	domainNodes := 0
	domainOutboundToInfra := 0
	infraNodes := 0

	graph.Nodes.Iterate(func(id string, node *stage4.ResolvedNode) {
		path := strings.ReplaceAll(node.FileSpec.Path, "\\", "/")
		if strings.Contains(path, "/domain/") || strings.Contains(path, "/core/") {
			domainNodes++
			// Check outbound edges
			for _, e := range graph.SafeGetOutboundEdges(id) {
				if !isStructuralEdge(e.Type) {
					continue
				}
				if target, ok := graph.SafeGetNode(e.TargetID); ok {
					targetPath := strings.ReplaceAll(target.FileSpec.Path, "\\", "/")
					if strings.Contains(targetPath, "/infra/") || strings.Contains(targetPath, "/db/") {
						domainOutboundToInfra++
					}
				}
			}
		} else if strings.Contains(path, "/infra/") || strings.Contains(path, "/db/") {
			infraNodes++
		}
	})

	if domainNodes > 0 && infraNodes > 0 && domainOutboundToInfra == 0 {
		b := evidence.Bundle{}
		b.Add(evidence.EvidenceItem{
			Source:     evidence.SourceRule,
			Reference:  "PR-02",
			Excerpt:    "Domain layer has zero outbound dependencies to infrastructure layers.",
			Confidence: 0.85,
			Timestamp:  time.Now(),
		})

		return &archmodel.DetectedPattern{
			Kind:        archmodel.PatternCleanArchitecture,
			Name:        "Clean Architecture",
			Confidence:  0.85,
			Evidence:    b,
			Description: "Domain entities and use cases are independent of infrastructure and frameworks.",
		}
	}

	return nil
}

// PR03Microservices uses Louvain community detection to find standalone services.
type PR03Microservices struct{}

func (r *PR03Microservices) ID() string   { return "PR-03" }
func (r *PR03Microservices) Name() string { return "Microservices" }
func (r *PR03Microservices) Evaluate(graph *akg.CodePropertyGraph, metrics archmodel.ArchMetrics) *archmodel.DetectedPattern {
	comms := LouvainCommunityDetection(graph)

	// Count communities that have network/DB calls inside them
	serviceCandidates := make(map[string]bool)

	graph.Nodes.Iterate(func(id string, node *stage4.ResolvedNode) {
		commID := comms[id]
		for _, e := range graph.SafeGetOutboundEdges(id) {
			if e.Type == stage4.EdgeQueriesDB || e.Type == stage4.EdgeExposesEndpoint {
				serviceCandidates[commID] = true
			}
		}
	})

	if len(serviceCandidates) >= 2 {
		b := evidence.Bundle{}
		b.Add(evidence.EvidenceItem{
			Source:     evidence.SourceRule,
			Reference:  "PR-03",
			Excerpt:    "Detected multiple loosely coupled communities with their own endpoints/databases.",
			Confidence: 0.80,
			Timestamp:  time.Now(),
		})

		var comps []string
		for k := range serviceCandidates {
			comps = append(comps, k)
		}

		return &archmodel.DetectedPattern{
			Kind:        archmodel.PatternMicroservices,
			Name:        "Microservices",
			Confidence:  0.80,
			Components:  comps,
			Evidence:    b,
			Description: "The system is composed of multiple independent services.",
		}
	}

	return nil
}

// PR05CQRS detects Command Query Responsibility Segregation.
type PR05CQRS struct{}

func (r *PR05CQRS) ID() string   { return "PR-05" }
func (r *PR05CQRS) Name() string { return "CQRS" }
func (r *PR05CQRS) Evaluate(graph *akg.CodePropertyGraph, metrics archmodel.ArchMetrics) *archmodel.DetectedPattern {
	var commands, queries, commandHandlers, queryHandlers int

	graph.Nodes.Iterate(func(id string, node *stage4.ResolvedNode) {
		name := node.Name
		if strings.HasSuffix(name, "Command") {
			commands++
		} else if strings.HasSuffix(name, "Query") {
			queries++
		} else if strings.HasSuffix(name, "CommandHandler") || strings.Contains(name, "HandleCommand") {
			commandHandlers++
		} else if strings.HasSuffix(name, "QueryHandler") || strings.Contains(name, "HandleQuery") {
			queryHandlers++
		}
	})

	if commands > 0 && queries > 0 && (commandHandlers > 0 || queryHandlers > 0) {
		b := evidence.Bundle{}
		b.Add(evidence.EvidenceItem{
			Source:     evidence.SourceRule,
			Reference:  "PR-05",
			Excerpt:    "Distinct command/query types and handlers detected in node names.",
			Confidence: 0.8,
			Timestamp:  time.Now(),
		})

		return &archmodel.DetectedPattern{
			Kind:        archmodel.PatternCQRS,
			Name:        "CQRS",
			Confidence:  0.8,
			Evidence:    b,
			Description: "Read and write operations are strictly separated using commands and queries.",
		}
	}

	return nil
}

// PR06EventDriven detects event-driven architecture based on pub/sub edges.
type PR06EventDriven struct{}

func (r *PR06EventDriven) ID() string   { return "PR-06" }
func (r *PR06EventDriven) Name() string { return "Event-Driven" }
func (r *PR06EventDriven) Evaluate(graph *akg.CodePropertyGraph, metrics archmodel.ArchMetrics) *archmodel.DetectedPattern {
	var eventEdges int
	var totalEdges int

	graph.OutboundEdges.Iterate(func(_ string, edges []stage4.ResolvedEdge) {
		for _, e := range edges {
			if isStructuralEdge(e.Type) {
				totalEdges++
			}
			if e.Type == stage4.EdgePublishes || e.Type == stage4.EdgeSubscribes || e.Type == stage4.EdgeDispatchesEvent {
				eventEdges++
			}
		}
	})

	if totalEdges == 0 {
		return nil
	}

	ratio := float64(eventEdges) / float64(totalEdges)
	if ratio > 0.10 || eventEdges > 10 { // 10% threshold or absolute count
		b := evidence.Bundle{}
		b.Add(evidence.EvidenceItem{
			Source:     evidence.SourceRule,
			Reference:  "PR-06",
			Excerpt:    "Significant number of publish/subscribe/dispatch edges detected.",
			Confidence: 0.85,
			Timestamp:  time.Now(),
		})

		return &archmodel.DetectedPattern{
			Kind:        archmodel.PatternEventDriven,
			Name:        "Event-Driven Architecture",
			Confidence:  0.85,
			Evidence:    b,
			Description: "Components communicate primarily through asynchronous events.",
		}
	}

	return nil
}

// PR07RepositoryPattern detects repository pattern usage.
type PR07RepositoryPattern struct{}

func (r *PR07RepositoryPattern) ID() string   { return "PR-07" }
func (r *PR07RepositoryPattern) Name() string { return "Repository Pattern" }
func (r *PR07RepositoryPattern) Evaluate(graph *akg.CodePropertyGraph, metrics archmodel.ArchMetrics) *archmodel.DetectedPattern {
	repoFound := false

	graph.Nodes.Iterate(func(id string, node *stage4.ResolvedNode) {
		if (node.Kind == "STRUCT" || node.Kind == "CLASS") && (strings.HasSuffix(node.Name, "Repository") || strings.HasSuffix(node.Name, "Repo")) {
			for _, e := range graph.SafeGetOutboundEdges(id) {
				if e.Type == stage4.EdgeQueriesDB {
					repoFound = true
					break
				}
			}
		}
	})

	if repoFound {
		b := evidence.Bundle{}
		b.Add(evidence.EvidenceItem{
			Source:     evidence.SourceRule,
			Reference:  "PR-07",
			Excerpt:    "Structs named Repository with database query edges detected.",
			Confidence: 0.9,
			Timestamp:  time.Now(),
		})

		return &archmodel.DetectedPattern{
			Kind:        archmodel.PatternRepository,
			Name:        "Repository Pattern",
			Confidence:  0.9,
			Evidence:    b,
			Description: "Data access is abstracted behind Repository interfaces/structs.",
		}
	}

	return nil
}

// PR04BoundedContext detects DDD bounded contexts using community detection.
type PR04BoundedContext struct{}

func (r *PR04BoundedContext) ID() string   { return "PR-04" }
func (r *PR04BoundedContext) Name() string { return "DDD Bounded Context" }
func (r *PR04BoundedContext) Evaluate(graph *akg.CodePropertyGraph, metrics archmodel.ArchMetrics) *archmodel.DetectedPattern {
	comms := LouvainCommunityDetection(graph)
	
	commSizes := make(map[string]int)
	graph.Nodes.Iterate(func(id string, _ *stage4.ResolvedNode) {
		commSizes[comms[id]]++
	})
	
	boundedContexts := 0
	var contextNames []string
	
	for commID, size := range commSizes {
		if size > 5 {
			boundedContexts++
			contextNames = append(contextNames, commID)
		}
	}
	
	if boundedContexts > 0 {
		b := evidence.Bundle{}
		b.Add(evidence.EvidenceItem{
			Source: evidence.SourceRule,
			Reference: "PR-04",
			Excerpt: "Found isolated communities acting as bounded contexts.",
			Confidence: 0.8,
			Timestamp: time.Now(),
		})
		
		return &archmodel.DetectedPattern{
			Kind: archmodel.PatternDDD,
			Name: "DDD Bounded Context",
			Components: contextNames,
			Confidence: 0.8,
			Evidence: b,
			Description: "The system contains modules that act as isolated bounded contexts.",
		}
	}
	
	return nil
}

// RunPatternDetection runs all defined rules against the graph.
func RunPatternDetection(graph *akg.CodePropertyGraph, metrics archmodel.ArchMetrics) []archmodel.DetectedPattern {
	rules := []PatternRule{
		&PR01LayeredArchitecture{},
		&PR02CleanArchitecture{},
		&PR03Microservices{},
		&PR04BoundedContext{},
		&PR05CQRS{},
		&PR06EventDriven{},
		&PR07RepositoryPattern{},
	}

	var patterns []archmodel.DetectedPattern
	for _, rule := range rules {
		p := rule.Evaluate(graph, metrics)
		if p != nil {
			patterns = append(patterns, *p)
		}
	}

	return patterns
}
