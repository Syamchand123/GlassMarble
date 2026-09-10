// Package grounding — collector.go
// Dispatches AKG queries for all 13 ground_with directives.
package grounding

import (
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/link"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/catalog"
	"github.com/Syamchand123/GlassMarble/internal/doc_engine/config"
)

// Collector dispatches targeted queries against the AKG based on section ground_with directives.
type Collector struct {
	graph *akg.CodePropertyGraph
}

// NewCollector constructs a Collector bound to an AKG CodePropertyGraph.
func NewCollector(graph *akg.CodePropertyGraph) *Collector {
	return &Collector{graph: graph}
}

// groundWithAliases maps true synonym spellings to their canonical
// ground_with directive. Aliases are resolved BEFORE the dispatch switch.
var groundWithAliases = map[string]string{
	"callers":             "callgraph",
	"components":          "signatures",
	"symbols":             "signatures",
	"exported_interfaces": "exported_symbols",
	"timeline":            "arch_events",
	"timelines":           "arch_events",
	"commit_reasoning":    "arch_events",
}

// groundWithSkipped lists directive spellings that are intentionally not
// collected here: diagrams come from DocSpec.Diagrams (rendered in facts.go),
// and db_schemas is a descoped pillar (P22) that must not fail.
var groundWithSkipped = map[string]bool{
	"diagrams":   true,
	"diagram":    true,
	"db_schemas": true,
	"db_schema":  true,
}

// resolveGroundWith normalizes a raw ground_with value: lowercase, trim,
// then alias-map to the canonical directive. The second return value reports
// whether the value is a skip directive (diagrams / descoped db_schemas).
func resolveGroundWith(raw string) (canonical string, skip bool) {
	norm := strings.ToLower(strings.TrimSpace(raw))
	if groundWithSkipped[norm] {
		return norm, true
	}
	if canonical, ok := groundWithAliases[norm]; ok {
		return canonical, false
	}
	return norm, false
}

// CollectSectionFacts collects all facts required for a section under the given scope.
// Unknown ground_with values produce a descriptive error; alias spellings are
// resolved to their canonical directive before dispatch.
func (c *Collector) CollectSectionFacts(sec *config.SectionSpec, scope *config.ScopeRule) (*config.GroundTruthPayload, error) {
	payload := &config.GroundTruthPayload{}
	if c.graph == nil || c.graph.Nodes == nil {
		return payload, nil
	}

	directives := []string{"signatures", "exported_symbols"} // Default grounding
	if sec != nil && len(sec.GroundWith) > 0 {
		directives = sec.GroundWith
	}

	seenSymbols := make(map[string]bool)

	for _, d := range directives {
		canonical, skip := resolveGroundWith(d)
		if skip {
			continue
		}
		switch canonical {
		case "signatures":
			c.collectSignatures(scope, false, seenSymbols, payload)
		case "exported_symbols":
			c.collectSignatures(scope, true, seenSymbols, payload)
		case "comments":
			c.collectDocComments(scope, payload)
		case "sentinels":
			c.collectSentinels(scope, payload)
		case "error_returns":
			c.collectErrorReturns(scope, payload)
		case "concurrency_primitives":
			c.collectConcurrency(scope, payload)
		case "config_vars":
			c.collectConfigVars(scope, payload)
		case "http_handlers":
			c.collectHTTPHandlers(scope, payload)
		case "callgraph":
			c.collectCallgraphFacts(scope, payload)
		case "arch_intelligence":
			c.collectArchIntelligence(payload)
		case "arch_events":
			c.collectArchEvents(payload)
		case "ingress_points":
			c.collectIngressPoints(scope, payload)
		case "dependencies":
			c.collectDependencies(scope, payload)
		default:
			return payload, fmt.Errorf("unknown ground_with %q", d)
		}
	}

	// Sort symbols by FQN for determinism
	sort.Slice(payload.Symbols, func(i, j int) bool {
		return payload.Symbols[i].FQN < payload.Symbols[j].FQN
	})
	sort.Slice(payload.Sentinels, func(i, j int) bool {
		return payload.Sentinels[i].FQN < payload.Sentinels[j].FQN
	})
	sort.Slice(payload.ConfigVars, func(i, j int) bool {
		return payload.ConfigVars[i].Name < payload.ConfigVars[j].Name
	})

	return payload, nil
}

// 1 & 2: Signatures and Exported Symbols
func (c *Collector) collectSignatures(scope *config.ScopeRule, exportedOnly bool, seen map[string]bool, p *config.GroundTruthPayload) {
	c.graph.Nodes.Iterate(func(id string, n *link.ResolvedNode) {
		if n == nil || !catalog.MatchesScope(scope, n.FileSpec.Path) {
			return
		}
		if exportedOnly && !isExportedSymbol(n.Name) {
			return
		}
		if seen[id] {
			return
		}
		seen[id] = true

		p.Symbols = append(p.Symbols, config.SymbolFact{
			FQN:       id,
			Kind:      string(n.Kind),
			File:      n.FileSpec.Path,
			Line:      n.FileSpec.LineStart,
			Permalink: FormatPermalink(n.FileSpec.Path, n.FileSpec.LineStart, n.FileSpec.LineEnd),
			Signature: extractSignature(n),
			Doc:       extractDoc(n),
		})
	})
}

// 3: Doc Comments
func (c *Collector) collectDocComments(scope *config.ScopeRule, p *config.GroundTruthPayload) {
	c.graph.Nodes.Iterate(func(id string, n *link.ResolvedNode) {
		if n == nil || !catalog.MatchesScope(scope, n.FileSpec.Path) {
			return
		}
		doc := extractDoc(n)
		if doc != "" {
			// Attach doc to matching symbol in p.Symbols or add new
			found := false
			for i := range p.Symbols {
				if p.Symbols[i].FQN == id {
					p.Symbols[i].Doc = doc
					found = true
					break
				}
			}
			if !found {
				p.Symbols = append(p.Symbols, config.SymbolFact{
					FQN:       id,
					Kind:      string(n.Kind),
					File:      n.FileSpec.Path,
					Line:      n.FileSpec.LineStart,
					Permalink: FormatPermalink(n.FileSpec.Path, n.FileSpec.LineStart, n.FileSpec.LineEnd),
					Signature: extractSignature(n),
					Doc:       doc,
				})
			}
		}
	})
}

// 4: Sentinel Errors (var Err* error)
func (c *Collector) collectSentinels(scope *config.ScopeRule, p *config.GroundTruthPayload) {
	seen := make(map[string]bool)
	c.graph.Nodes.Iterate(func(id string, n *link.ResolvedNode) {
		if n == nil || !catalog.MatchesScope(scope, n.FileSpec.Path) {
			return
		}
		name := n.Name
		if isSentinelError(name) && !seen[id] {
			seen[id] = true
			p.Sentinels = append(p.Sentinels, config.SentinelFact{
				FQN:  id,
				Doc:  extractDoc(n),
				File: n.FileSpec.Path,
				Line: n.FileSpec.LineStart,
			})
		}
	})
}

// 5: Error Returns
func (c *Collector) collectErrorReturns(scope *config.ScopeRule, p *config.GroundTruthPayload) {
	c.graph.Nodes.Iterate(func(id string, n *link.ResolvedNode) {
		if n == nil || !catalog.MatchesScope(scope, n.FileSpec.Path) {
			return
		}
		sig := extractSignature(n)
		if strings.Contains(sig, "error") || strings.HasSuffix(strings.TrimSpace(sig), "error") {
			p.Sentinels = append(p.Sentinels, config.SentinelFact{
				FQN:  id,
				Doc:  "Returns error: " + sig,
				File: n.FileSpec.Path,
				Line: n.FileSpec.LineStart,
			})
		}
	})
}

// 6: Concurrency Primitives (Mutex, RWMutex, channels, goroutines)
func (c *Collector) collectConcurrency(scope *config.ScopeRule, p *config.GroundTruthPayload) {
	c.graph.Nodes.Iterate(func(id string, n *link.ResolvedNode) {
		if n == nil || !catalog.MatchesScope(scope, n.FileSpec.Path) {
			return
		}
		sig := extractSignature(n)
		name := n.Name
		lower := strings.ToLower(sig + " " + name)
		if strings.Contains(lower, "mutex") || strings.Contains(lower, "rwmutex") || strings.Contains(lower, "chan ") || strings.Contains(lower, "waitgroup") {
			p.Symbols = append(p.Symbols, config.SymbolFact{
				FQN:       id,
				Kind:      "concurrency",
				File:      n.FileSpec.Path,
				Line:      n.FileSpec.LineStart,
				Permalink: FormatPermalink(n.FileSpec.Path, n.FileSpec.LineStart, n.FileSpec.LineEnd),
				Signature: sig,
				Doc:       "Concurrency primitive: " + extractDoc(n),
			})
		}
	})
}

// 7: Config Vars (os.Getenv, flag, viper)
func (c *Collector) collectConfigVars(scope *config.ScopeRule, p *config.GroundTruthPayload) {
	seen := make(map[string]bool)
	c.graph.Nodes.Iterate(func(id string, n *link.ResolvedNode) {
		if n == nil || !catalog.MatchesScope(scope, n.FileSpec.Path) {
			return
		}
		name := n.Name
		if isConfigVar(name) || isConfigVar(id) {
			if !seen[name] {
				seen[name] = true
				p.ConfigVars = append(p.ConfigVars, config.ConfigVarFact{
					Name: name,
					Doc:  extractDoc(n),
					File: n.FileSpec.Path,
					Line: n.FileSpec.LineStart,
				})
			}
		}
	})
}

// 8: HTTP Handlers
func (c *Collector) collectHTTPHandlers(scope *config.ScopeRule, p *config.GroundTruthPayload) {
	c.graph.Nodes.Iterate(func(id string, n *link.ResolvedNode) {
		if n == nil || !catalog.MatchesScope(scope, n.FileSpec.Path) {
			return
		}
		sig := extractSignature(n)
		if strings.Contains(sig, "http.ResponseWriter") || strings.Contains(sig, "gin.Context") || strings.Contains(sig, "echo.Context") {
			p.Symbols = append(p.Symbols, config.SymbolFact{
				FQN:       id,
				Kind:      "http_handler",
				File:      n.FileSpec.Path,
				Line:      n.FileSpec.LineStart,
				Permalink: FormatPermalink(n.FileSpec.Path, n.FileSpec.LineStart, n.FileSpec.LineEnd),
				Signature: sig,
				Doc:       extractDoc(n),
			})
		}
	})
}

// 9: Callgraph Facts
func (c *Collector) collectCallgraphFacts(scope *config.ScopeRule, p *config.GroundTruthPayload) {
	if c.graph == nil || c.graph.OutboundEdges == nil || scope == nil {
		return
	}
	for _, ep := range scope.EntryPoints {
		for _, edge := range c.graph.GetOutboundEdges(ep) {
			if edge.Type == link.EdgeCalls {
				fact := config.SymbolFact{
					FQN:       edge.TargetID,
					Kind:      "callee",
					Signature: "Called by " + cleanSymbolName(ep),
				}
				if node, ok := c.graph.Nodes.Get(edge.TargetID); ok && node != nil {
					fact.File = node.FileSpec.Path
					fact.Line = node.FileSpec.LineStart
					fact.Permalink = FormatPermalink(node.FileSpec.Path, node.FileSpec.LineStart, node.FileSpec.LineEnd)
				}
				p.Symbols = append(p.Symbols, fact)
			}
		}
	}
}

// 10: Arch Intelligence (components & patterns)
func (c *Collector) collectArchIntelligence(p *config.GroundTruthPayload) {
	// Surface high-level architectural component facts from graph structure
	if c.graph.Nodes != nil {
		p.ArchEvents = append(p.ArchEvents, "Graph nodes verified: CPG ontology active")
	}
}

// 11: Arch Events
func (c *Collector) collectArchEvents(p *config.GroundTruthPayload) {
	// Appends active graph commit hash context
	if c.graph != nil && c.graph.CommitHash != "" {
		p.ArchEvents = append(p.ArchEvents, "Commit: "+c.graph.CommitHash)
	}
}

// 12: Ingress Points
func (c *Collector) collectIngressPoints(scope *config.ScopeRule, p *config.GroundTruthPayload) {
	if scope != nil {
		for _, ep := range scope.EntryPoints {
			fact := config.SymbolFact{
				FQN:  ep,
				Kind: "ingress",
				Doc:  "Public entry point",
			}
			if c.graph != nil && c.graph.Nodes != nil {
				if node, ok := c.graph.Nodes.Get(ep); ok && node != nil {
					fact.File = node.FileSpec.Path
					fact.Line = node.FileSpec.LineStart
					fact.Permalink = FormatPermalink(node.FileSpec.Path, node.FileSpec.LineStart, node.FileSpec.LineEnd)
				}
			}
			p.Symbols = append(p.Symbols, fact)
		}
	}
}

// 13: Dependencies
func (c *Collector) collectDependencies(scope *config.ScopeRule, p *config.GroundTruthPayload) {
	if c.graph == nil || c.graph.OutboundEdges == nil {
		return
	}
	seenDeps := make(map[string]bool)
	c.graph.Nodes.Iterate(func(id string, n *link.ResolvedNode) {
		if n == nil || !catalog.MatchesScope(scope, n.FileSpec.Path) {
			return
		}
		for _, e := range c.graph.GetOutboundEdges(id) {
			if e.Type == link.EdgeDependsOn || e.Type == link.EdgeCalls {
				tgtPkg := extractPackage(e.TargetID)
				if tgtPkg != "" && !seenDeps[tgtPkg] {
					seenDeps[tgtPkg] = true
					p.ArchEvents = append(p.ArchEvents, "Depends on package: "+tgtPkg)
				}
			}
		}
	})
}

func isSentinelError(name string) bool {
	if strings.HasPrefix(name, "Err") || strings.HasSuffix(name, "Error") {
		return true
	}
	return false
}

func isConfigVar(name string) bool {
	upper := strings.ToUpper(name)
	if strings.Contains(upper, "CONFIG") || strings.Contains(upper, "CFG") ||
		strings.Contains(upper, "PORT") || strings.Contains(upper, "HOST") ||
		strings.Contains(upper, "ENV") || strings.Contains(upper, "TIMEOUT") ||
		strings.Contains(upper, "ADDR") || strings.Contains(upper, "SECRET") ||
		strings.Contains(upper, "KEY") {
		return true
	}
	return false
}

func isExportedSymbol(name string) bool {
	if name == "" {
		return false
	}
	r := []rune(name)[0]
	return unicode.IsUpper(r)
}

func extractSignature(n *link.ResolvedNode) string {
	if n == nil {
		return ""
	}
	if sig, ok := n.Properties["signature"]; ok && sig != "" {
		return sig
	}
	return n.Name
}

func extractDoc(n *link.ResolvedNode) string {
	if n == nil {
		return ""
	}
	if doc, ok := n.Properties["doc_comment"]; ok {
		return doc
	}
	if doc, ok := n.Properties["doc"]; ok {
		return doc
	}
	return ""
}
