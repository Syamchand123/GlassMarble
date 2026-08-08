package arch_intelligence

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/archmodel"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage4"
	"github.com/Syamchand123/GlassMarble/internal/config"
	"github.com/Syamchand123/GlassMarble/internal/evidence"
)

// PatternRule defines a deterministic rule for identifying an architectural
// pattern. Rules run against a shared RuleContext (snapshot + metrics +
// components + config + clock) so every rule sees a consistent view.
type PatternRule interface {
	ID() string
	Name() string
	Evaluate(ctx *RuleContext) *archmodel.DetectedPattern
}

// RuleContext carries everything a rule needs: the immutable graph snapshot,
// global metrics, inferred components, the active config, and the engine
// clock (so evidence timestamps stay deterministic in tests).
type RuleContext struct {
	Graph             *GraphSnapshot
	Metrics           archmodel.ArchMetrics
	Components        []archmodel.DetectedComponent
	ComponentCoupling []ComponentCoupling
	LayerAssigner     *LayerAssigner
	Cfg               *config.IntelligenceConfig
	Clock             func() time.Time
}

// cfgOrDefault returns the active intelligence config or defaults.
func (c *RuleContext) cfgOrDefault() *config.IntelligenceConfig {
	if c.Cfg != nil {
		return c.Cfg
	}
	return config.DefaultIntelligenceConfig()
}

func (c *RuleContext) now() time.Time {
	if c.Clock != nil {
		return c.Clock()
	}
	return time.Now()
}

// newPatternEvidence builds the standard evidence bundle for a rule match.
func newPatternEvidence(ctx *RuleContext, ruleID, excerpt string, confidence float64) evidence.Bundle {
	b := evidence.Bundle{PrimarySource: evidence.SourceRule}
	b.Add(evidence.EvidenceItem{
		Source:     evidence.SourceRule,
		Reference:  ruleID,
		Excerpt:    excerpt,
		Confidence: confidence,
		Timestamp:  ctx.now(),
	})
	return b
}

// PR01LayeredArchitecture detects layered architectures: dependencies must
// flow from outer to inner layers with consistency above the configured
// threshold. Layers come from config.arch_layers when declared, otherwise
// from conventional directory buckets.
type PR01LayeredArchitecture struct{}

func (r *PR01LayeredArchitecture) ID() string   { return "PR-01" }
func (r *PR01LayeredArchitecture) Name() string { return "Layered Architecture" }
func (r *PR01LayeredArchitecture) Evaluate(ctx *RuleContext) *archmodel.DetectedPattern {
	if ctx.Graph == nil || ctx.Graph.Len() == 0 {
		return nil
	}
	cfg := ctx.cfgOrDefault()

	assigner := ctx.LayerAssigner
	if assigner == nil || !assigner.Configured() {
		assigner = defaultLayerAssigner()
	}
	layerCount := assigner.layerCount()

	nodeLayer := make(map[string]string, ctx.Graph.Len())
	for _, id := range ctx.Graph.NodeIDs {
		node := ctx.Graph.Nodes[id]
		if node != nil {
			nodeLayer[id] = assigner.Assign(node.FileSpec.Path)
		}
	}

	totalEdges := 0
	violationEdges := 0
	for _, id := range ctx.Graph.NodeIDs {
		srcLayer := nodeLayer[id]
		if srcLayer == "" {
			continue
		}
		for _, e := range ctx.Graph.structuralOutbound(id) {
			tgtLayer := nodeLayer[e.TargetID]
			if tgtLayer == "" || tgtLayer == srcLayer {
				continue
			}
			totalEdges++
			if assigner.IsUpward(srcLayer, tgtLayer) {
				violationEdges++
			}
		}
	}

	if totalEdges < 10 || layerCount < 3 {
		return nil
	}
	consistency := 1.0 - float64(violationEdges)/float64(totalEdges)
	if consistency < cfg.LayeredConsistencyThreshold {
		return nil
	}

	return &archmodel.DetectedPattern{
		Kind:       archmodel.PatternLayered,
		Name:       r.Name(),
		Components: nil,
		Confidence: consistency,
		Evidence: newPatternEvidence(ctx, r.ID(),
			fmt.Sprintf("%d cross-layer edges, %d violations, consistency %.2f", totalEdges, violationEdges, consistency),
			consistency),
		Description: "The system exhibits a layered structure where dependencies consistently flow downwards.",
	}
}

// PR02CleanArchitecture extends Layered Architecture by checking Domain
// dependency inversion: the domain layer must not depend on infrastructure,
// and infrastructure must depend on the domain (inverted dependency).
type PR02CleanArchitecture struct{}

func (r *PR02CleanArchitecture) ID() string   { return "PR-02" }
func (r *PR02CleanArchitecture) Name() string { return "Clean Architecture" }
func (r *PR02CleanArchitecture) Evaluate(ctx *RuleContext) *archmodel.DetectedPattern {
	if ctx.Graph == nil || ctx.Graph.Len() == 0 {
		return nil
	}
	domainCount, infraCount := 0, 0
	domainToInfra := 0
	infraToDomain := 0

	// Bucket nodes by conventional clean-architecture directory names.
	nodeBucket := make(map[string]string)
	for _, id := range ctx.Graph.NodeIDs {
		node := ctx.Graph.Nodes[id]
		if node == nil || node.FileSpec.Path == "" {
			continue
		}
		p := normalizeDir(node.FileSpec.Path)
		switch {
		case pathContainsAny(p, "domain", "core", "entities", "usecases", "use-cases"):
			nodeBucket[id] = "domain"
			domainCount++
		case pathContainsAny(p, "infra", "infrastructure", "repository", "repositories", "db", "database", "persistence"):
			nodeBucket[id] = "infra"
			infraCount++
		}
	}
	if domainCount < 3 || infraCount < 3 {
		return nil
	}
	for _, id := range ctx.Graph.NodeIDs {
		if nodeBucket[id] != "domain" {
			continue
		}
		for _, e := range ctx.Graph.structuralOutbound(id) {
			if nodeBucket[e.TargetID] == "infra" {
				domainToInfra++
			}
		}
	}
	for _, id := range ctx.Graph.NodeIDs {
		if nodeBucket[id] != "infra" {
			continue
		}
		for _, e := range ctx.Graph.structuralOutbound(id) {
			if nodeBucket[e.TargetID] == "domain" {
				infraToDomain++
			}
		}
	}
	// Clean architecture: domain has zero outbound dependencies to infra and
	// infra depends on domain (dependency inversion present).
	if domainToInfra > 0 || infraToDomain == 0 {
		return nil
	}
	return &archmodel.DetectedPattern{
		Kind:       archmodel.PatternCleanArchitecture,
		Name:       r.Name(),
		Confidence: 0.85,
		Evidence: newPatternEvidence(ctx, r.ID(),
			fmt.Sprintf("%d domain nodes, %d infra nodes, %d inverted dependencies (infra->domain)",
				domainCount, infraCount, infraToDomain), 0.85),
		Description: "Domain entities and use cases are independent of infrastructure and frameworks.",
	}
}

// PR03Microservices detects microservices: two or more components that own
// their endpoints/databases and share no inter-component dependencies.
type PR03Microservices struct{}

func (r *PR03Microservices) ID() string   { return "PR-03" }
func (r *PR03Microservices) Name() string { return "Microservices" }
func (r *PR03Microservices) Evaluate(ctx *RuleContext) *archmodel.DetectedPattern {
	if ctx.Graph == nil || len(ctx.Components) == 0 {
		return nil
	}
	compIndex := make(map[string]int, len(ctx.Components))
	for i, c := range ctx.Components {
		compIndex[c.ID] = i
	}
	hasOwnResource := func(c archmodel.DetectedComponent) bool {
		for _, id := range c.NodeIDs {
			for _, e := range ctx.Graph.Outbound[id] {
				if e.Type == stage4.EdgeQueriesDB || e.Type == stage4.EdgeExposesEndpoint {
					return true
				}
			}
		}
		return false
	}
	var candidates []archmodel.DetectedComponent
	for _, c := range ctx.Components {
		if hasOwnResource(c) {
			candidates = append(candidates, c)
		}
	}
	if len(candidates) < 2 {
		return nil
	}
	// Services must be independent: no dependency edges between candidates.
	names := make([]string, 0, len(candidates))
	for _, c := range candidates {
		names = append(names, c.ID)
		for _, dep := range c.Dependencies {
			if _, ok := compIndex[dep]; ok {
				return nil // candidates depend on each other — not independent services
			}
		}
	}
	sort.Strings(names)
	return &archmodel.DetectedPattern{
		Kind:       archmodel.PatternMicroservices,
		Name:       r.Name(),
		Components: names,
		Confidence: 0.8,
		Evidence: newPatternEvidence(ctx, r.ID(),
			fmt.Sprintf("%d independent components with own endpoints/databases", len(candidates)), 0.8),
		Description: "The system is composed of multiple independent services.",
	}
}

// PR04BoundedContext detects DDD bounded contexts: groups of components with
// zero inter-group dependencies, each forming a closed unit.
type PR04BoundedContext struct{}

func (r *PR04BoundedContext) ID() string   { return "PR-04" }
func (r *PR04BoundedContext) Name() string { return "DDD Bounded Context" }
func (r *PR04BoundedContext) Evaluate(ctx *RuleContext) *archmodel.DetectedPattern {
	if len(ctx.Components) < 3 {
		return nil
	}
	// Build the component dependency graph.
	index := make(map[string]int, len(ctx.Components))
	for i, c := range ctx.Components {
		index[c.ID] = i
	}
	adj := make([][]int, len(ctx.Components))
	for i, c := range ctx.Components {
		for _, dep := range c.Dependencies {
			if j, ok := index[dep]; ok && j != i {
				adj[i] = append(adj[i], j)
				adj[j] = append(adj[j], i)
			}
		}
	}
	// Weakly connected components of the component graph = candidate contexts.
	visited := make([]bool, len(ctx.Components))
	var groups [][]string
	for i := range ctx.Components {
		if visited[i] {
			continue
		}
		var group []string
		queue := []int{i}
		visited[i] = true
		for len(queue) > 0 {
			cur := queue[0]
			queue = queue[1:]
			group = append(group, ctx.Components[cur].ID)
			for _, nbr := range adj[cur] {
				if !visited[nbr] {
					visited[nbr] = true
					queue = append(queue, nbr)
				}
			}
		}
		groups = append(groups, group)
	}
	if len(groups) < 2 {
		return nil
	}
	var contexts []string
	for _, g := range groups {
		sort.Strings(g)
		contexts = append(contexts, g...)
	}
	return &archmodel.DetectedPattern{
		Kind:       archmodel.PatternDDD,
		Name:       r.Name(),
		Components: contexts,
		Confidence: 0.8,
		Evidence: newPatternEvidence(ctx, r.ID(),
			fmt.Sprintf("%d closed dependency groups over %d components", len(groups), len(ctx.Components)), 0.8),
		Description: "The system contains modules that act as isolated bounded contexts.",
	}
}

// PR05CQRS detects Command Query Responsibility Segregation from type names:
// at least two Command types, two Query types and one handler in the same
// directory family.
type PR05CQRS struct{}

func (r *PR05CQRS) ID() string   { return "PR-05" }
func (r *PR05CQRS) Name() string { return "CQRS" }
func (r *PR05CQRS) Evaluate(ctx *RuleContext) *archmodel.DetectedPattern {
	if ctx.Graph == nil {
		return nil
	}
	commands, queries, handlers := 0, 0, 0
	commandDirs := make(map[string]bool)
	queryDirs := make(map[string]bool)
	for _, id := range ctx.Graph.NodeIDs {
		node := ctx.Graph.Nodes[id]
		if node == nil {
			continue
		}
		dir := dirSlash(normalizeDir(node.FileSpec.Path))
		name := node.Name
		switch {
		case strings.HasSuffix(name, "Command") || strings.HasSuffix(name, "CommandHandler"):
			if strings.HasSuffix(name, "CommandHandler") {
				handlers++
			} else {
				commands++
				commandDirs[dir] = true
			}
		case strings.HasSuffix(name, "Query") || strings.HasSuffix(name, "QueryHandler"):
			if strings.HasSuffix(name, "QueryHandler") {
				handlers++
			} else {
				queries++
				queryDirs[dir] = true
			}
		}
	}
	// Commands and queries must coexist in the same directory family.
	coLocated := false
	for d := range commandDirs {
		for d2 := range queryDirs {
			if d != "" && d == d2 {
				coLocated = true
				break
			}
		}
	}
	if commands < 2 || queries < 2 || handlers < 1 || !coLocated {
		return nil
	}
	return &archmodel.DetectedPattern{
		Kind:       archmodel.PatternCQRS,
		Name:       r.Name(),
		Confidence: 0.8,
		Evidence: newPatternEvidence(ctx, r.ID(),
			fmt.Sprintf("%d commands, %d queries, %d handlers in %d command/query dirs",
				commands, queries, handlers, len(commandDirs)), 0.8),
		Description: "Read and write operations are strictly separated using commands and queries.",
	}
}

// PR06EventDriven detects event-driven architecture from publish/subscribe/
// dispatch edge ratios.
type PR06EventDriven struct{}

func (r *PR06EventDriven) ID() string   { return "PR-06" }
func (r *PR06EventDriven) Name() string { return "Event-Driven Architecture" }
func (r *PR06EventDriven) Evaluate(ctx *RuleContext) *archmodel.DetectedPattern {
	if ctx.Graph == nil {
		return nil
	}
	cfg := ctx.cfgOrDefault()
	eventEdges := 0
	totalEdges := 0
	for _, id := range ctx.Graph.NodeIDs {
		for _, e := range ctx.Graph.Outbound[id] {
			if isStructuralEdge(e.Type) {
				totalEdges++
			}
			if e.Type == stage4.EdgePublishes || e.Type == stage4.EdgeSubscribes ||
				e.Type == stage4.EdgeDispatchesEvent || e.Type == stage4.EdgeSendsTo ||
				e.Type == stage4.EdgeReceivesFrom {
				eventEdges++
			}
		}
	}
	if totalEdges == 0 {
		return nil
	}
	ratio := float64(eventEdges) / float64(totalEdges)
	if ratio*100 < cfg.EventEdgePct || eventEdges < 5 {
		return nil
	}
	confidence := 0.6 + 0.35*ratio
	if confidence > 0.95 {
		confidence = 0.95
	}
	return &archmodel.DetectedPattern{
		Kind:       archmodel.PatternEventDriven,
		Name:       r.Name(),
		Confidence: confidence,
		Evidence: newPatternEvidence(ctx, r.ID(),
			fmt.Sprintf("%d event edges of %d structural edges (ratio %.3f)", eventEdges, totalEdges, ratio),
			confidence),
		Description: "Components communicate primarily through asynchronous events.",
	}
}

// PR07RepositoryPattern detects Repository usage: structs/classes named
// Repository with database access (direct query edges or dependency on a
// database primitive).
type PR07RepositoryPattern struct{}

func (r *PR07RepositoryPattern) ID() string   { return "PR-07" }
func (r *PR07RepositoryPattern) Name() string { return "Repository Pattern" }
func (r *PR07RepositoryPattern) Evaluate(ctx *RuleContext) *archmodel.DetectedPattern {
	if ctx.Graph == nil {
		return nil
	}
	repos := 0
	directDB := 0
	for _, id := range ctx.Graph.NodeIDs {
		node := ctx.Graph.Nodes[id]
		if node == nil || (node.Kind != "STRUCT" && node.Kind != "CLASS") {
			continue
		}
		if !strings.HasSuffix(node.Name, "Repository") && !strings.HasSuffix(node.Name, "Repo") {
			continue
		}
		repos++
		for _, e := range ctx.Graph.Outbound[id] {
			if e.Type == stage4.EdgeQueriesDB {
				directDB++
			} else if e.Type == stage4.EdgeDependsOn {
				if tgt, ok := ctx.Graph.Nodes[e.TargetID]; ok && tgt.Primitive == "DATABASE" {
					directDB++
				}
			}
		}
	}
	if repos == 0 {
		return nil
	}
	confidence := 0.7
	if directDB > 0 {
		confidence = 0.9
	}
	return &archmodel.DetectedPattern{
		Kind:       archmodel.PatternRepository,
		Name:       r.Name(),
		Confidence: confidence,
		Evidence: newPatternEvidence(ctx, r.ID(),
			fmt.Sprintf("%d Repository types, %d with direct database access", repos, directDB), confidence),
		Description: "Data access is abstracted behind Repository interfaces/structs.",
	}
}

// defaultLayerAssigner buckets nodes into conventional layer names for
// PR-01/SD-04 when no config.arch_layers are declared.
func defaultLayerAssigner() *LayerAssigner {
	a := NewLayerAssigner([]config.DriftLayer{
		{Name: "UI/CLI", Paths: []string{"cmd/**", "**/cmd/**", "**/web/**", "**/handler/**", "**/api/**"}},
		{Name: "App", Paths: []string{"**/app/**", "**/application/**", "**/service/**", "**/services/**"}},
		{Name: "Domain", Paths: []string{"**/domain/**", "**/core/**", "**/model/**", "**/entity/**", "**/entities/**"}},
		{Name: "Infrastructure", Paths: []string{"**/infra/**", "**/infrastructure/**", "**/repository/**", "**/repositories/**", "**/db/**", "**/database/**", "**/persistence/**"}},
	})
	return a
}

// layerCount reports how many distinct layer names are populated.
func (a *LayerAssigner) layerCount() int {
	return len(a.order)
}

func pathContainsAny(p string, candidates ...string) bool {
	for _, c := range candidates {
		if strings.Contains(p, "/"+c+"/") {
			return true
		}
	}
	return false
}

// RunPatternDetection runs all defined rules against the graph.
// Compatibility wrapper: builds a snapshot-based context from the graph.
func RunPatternDetection(graph *akg.CodePropertyGraph, metrics archmodel.ArchMetrics) []archmodel.DetectedPattern {
	if graph == nil {
		return nil
	}
	ctx := &RuleContext{
		Graph:   NewGraphSnapshot(graph),
		Metrics: metrics,
		Cfg:     config.DefaultIntelligenceConfig(),
		Clock:   time.Now,
	}
	return RunPatternDetectionContext(ctx)
}

// RunPatternDetectionContext runs all pattern rules against a shared context.
func RunPatternDetectionContext(ctx *RuleContext) []archmodel.DetectedPattern {
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
		p := rule.Evaluate(ctx)
		if p != nil {
			patterns = append(patterns, *p)
		}
	}
	sort.Slice(patterns, func(i, j int) bool {
		if patterns[i].Kind != patterns[j].Kind {
			return patterns[i].Kind < patterns[j].Kind
		}
		return patterns[i].Name < patterns[j].Name
	})
	return patterns
}
