// Package grounding — collector.go
// Dispatches AKG queries for all 13 ground_with directives.
package grounding

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
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
	// repoRoot enables the collectSentinels source-scan fallback and
	// extractDoc's Go-source fallback (see WithRepoRoot). Empty means both
	// fallbacks are skipped — AKG-only, exactly today's behavior — so every
	// existing NewCollector caller (tests included) is unaffected without
	// needing to change.
	repoRoot string
	// srcDocCache memoizes extractDoc's per-file Go-source scan (bare symbol
	// name -> doc comment), so a document referencing many symbols from the
	// same file only parses it once per Collector/CollectSectionFacts call.
	srcDocCache map[string]map[string]string
}

// NewCollector constructs a Collector bound to an AKG CodePropertyGraph.
func NewCollector(graph *akg.CodePropertyGraph) *Collector {
	return &Collector{graph: graph}
}

// WithRepoRoot enables collectSentinels' Go-source fallback scan and returns
// the same Collector for chaining. The AKG ingestion pipeline does not
// currently index package-level `var`/`const` declarations at all (only
// funcs, methods, structs, interfaces, and fields), so a real sentinel like
// `var ErrInvalidToken = errors.New(...)` is structurally invisible to the
// graph-only collectSentinels below — the Error Catalog section would
// report zero sentinels for a package that plainly declares one. Passing
// repoRoot lets the collector go straight to the .go source as a fallback,
// the same thing sre.GenerateErrorCatalog already does independently for
// the repo-wide living error catalog (docs/errors.md).
func (c *Collector) WithRepoRoot(repoRoot string) *Collector {
	c.repoRoot = repoRoot
	return c
}

// groundWithAliases maps true synonym spellings to their canonical
// ground_with directive. Aliases are resolved BEFORE the dispatch switch.
// Deltas (symbol_deltas, config_var_changes, migration_files) resolve to the
// current-state collectors because the dossier already merges per-commit
// added/modified/removed facts into every payload in facts.go.
var groundWithAliases = map[string]string{
	"callers":             "callgraph",
	"components":          "signatures",
	"symbols":             "signatures",
	"exported_interfaces": "exported_symbols",
	"timeline":            "arch_events",
	"timelines":           "arch_events",
	"commit_reasoning":    "arch_events",
	"env_getenv":          "config_vars",
	"flag_defs":           "config_vars",
	"symbol_deltas":       "signatures",
	"config_var_changes":  "config_vars",
	"migration_files":     "signatures",
}

// groundWithSkipped lists directive spellings that are intentionally not
// collected here: diagrams come from DocSpec.Diagrams (rendered in facts.go),
// c4container/layered are diagram types (not grounding), and db_schemas is
// a descoped pillar (P22) that must not fail.
var groundWithSkipped = map[string]bool{
	"diagrams":    true,
	"diagram":     true,
	"c4container": true,
	"layered":     true,
	"db_schemas":  true,
	"db_schema":   true,
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
		case "egress_calls":
			c.collectEgressCalls(scope, payload)
		case "crypto_primitives":
			c.collectCryptoPrimitives(scope, payload)
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
			Doc:       c.extractDoc(n),
		})
	})
}

// 3: Doc Comments
func (c *Collector) collectDocComments(scope *config.ScopeRule, p *config.GroundTruthPayload) {
	c.graph.Nodes.Iterate(func(id string, n *link.ResolvedNode) {
		if n == nil || !catalog.MatchesScope(scope, n.FileSpec.Path) {
			return
		}
		doc := c.extractDoc(n)
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
	seen := make(map[string]bool)      // by node id — preserves prior dedup behavior
	seenNames := make(map[string]bool) // by symbol name — for the source-scan fallback below
	c.graph.Nodes.Iterate(func(id string, n *link.ResolvedNode) {
		if n == nil || !catalog.MatchesScope(scope, n.FileSpec.Path) {
			return
		}
		name := n.Name
		if isSentinelError(name) && !seen[id] {
			seen[id] = true
			seenNames[name] = true
			p.Sentinels = append(p.Sentinels, config.SentinelFact{
				FQN:  id,
				Doc:  c.extractDoc(n),
				File: n.FileSpec.Path,
				Line: n.FileSpec.LineStart,
			})
		}
	})
	// Source-scan fallback (see WithRepoRoot): the AKG does not index
	// package-level var/const declarations at all today, so a real sentinel
	// never surfaces from the graph walk above. Only fills in names the
	// graph walk didn't already find, so a future AKG that does index them
	// is never double-reported.
	if c.repoRoot != "" {
		for _, s := range scanSentinelsFromSource(c.repoRoot, scope) {
			if seenNames[s.name] {
				continue
			}
			seenNames[s.name] = true
			p.Sentinels = append(p.Sentinels, config.SentinelFact{
				FQN:  s.file + "::" + s.name,
				Doc:  s.doc,
				File: s.file,
				Line: s.line,
			})
		}
	}
}

// sourceSentinel is one `var Err*`/`*Error` sentinel found by scanning Go
// source directly, independent of the AKG.
type sourceSentinel struct {
	name string
	doc  string
	file string // repo-relative, slash-separated
	line int
}

// scanSentinelsFromSource walks repoRoot for .go files matching scope and
// parses each with go/parser (comments enabled) for package-level var
// declarations whose name looks like a sentinel error (isSentinelError).
// Best-effort: unparseable files are skipped, not fatal. Mirrors what
// sre.GenerateErrorCatalog already does independently for the repo-wide
// living error catalog — this is the same technique, scoped to one
// section's ground_with:sentinels scope instead of the whole repo.
func scanSentinelsFromSource(repoRoot string, scope *config.ScopeRule) []sourceSentinel {
	var out []sourceSentinel
	_ = filepath.WalkDir(repoRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, relErr := filepath.Rel(repoRoot, path)
		if relErr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if !catalog.MatchesScope(scope, rel) {
			return nil
		}
		fset := token.NewFileSet()
		node, parseErr := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if parseErr != nil || node == nil {
			return nil
		}
		for _, decl := range node.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.VAR {
				continue
			}
			for _, spec := range gen.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				doc := vs.Doc
				if doc == nil {
					doc = gen.Doc
				}
				for _, name := range vs.Names {
					if !isSentinelError(name.Name) {
						continue
					}
					docText := ""
					if doc != nil {
						docText = strings.TrimSpace(doc.Text())
					}
					out = append(out, sourceSentinel{
						name: name.Name,
						doc:  docText,
						file: rel,
						line: fset.Position(name.Pos()).Line,
					})
				}
			}
		}
		return nil
	})
	return out
}

// 5: Error Returns
func (c *Collector) collectErrorReturns(scope *config.ScopeRule, p *config.GroundTruthPayload) {
	c.graph.Nodes.Iterate(func(id string, n *link.ResolvedNode) {
		if n == nil || !catalog.MatchesScope(scope, n.FileSpec.Path) {
			return
		}
		// Only actual functions/methods return anything. Without this, a
		// PARAM/return-value sub-node whose own Name happens to literally be
		// "error" (how unnamed `error`-typed parameters/returns surface in
		// this graph, e.g. "Login::param:error") was matched by the OLD
		// substring check below and miscategorized as a "sentinel," even
		// though it's not a function at all.
		if n.Kind != "FUNCTION" && n.Kind != "METHOD" {
			return
		}
		// extractSignature falls back to the bare node Name whenever no
		// "signature" property is set (which is every case today — the AKG
		// never populates one), so checking the SIGNATURE string for
		// "error" checked the function's own NAME instead ("Login" doesn't
		// contain "error", so real error-returning functions were mostly
		// missed anyway). return_type ("(string, error)", "error", ...) is
		// the reliable signal the AKG actually provides for this.
		if !returnsBuiltinError(n.Properties["return_type"]) {
			return
		}
		sig := extractSignature(n)
		p.Sentinels = append(p.Sentinels, config.SentinelFact{
			FQN: id,
			// Backtick-wrapped: this Doc renders verbatim into a markdown
			// table cell (deterministic.go's Error Catalog). A bare
			// signature there is prose to Gate 6's checker (which
			// correctly skips backtick code spans, per its own doc
			// comment), so an unwrapped multi-param signature like
			// "func Get(key string) (string, error)" reads as the
			// English word "string" appearing twice in a row — a false
			// "doubled word" warning on perfectly normal Go syntax.
			Doc:  "Returns error: `" + sig + "`",
			File: n.FileSpec.Path,
			Line: n.FileSpec.LineStart,
		})
	})
}

// returnsBuiltinError reports whether a return_type string (e.g.
// "(string, error)", "error", "(int, *MyError)") names the builtin `error`
// interface as one of its result types. Tokenizing on parens/commas/spaces/
// pointer stars avoids a plain substring match, which would also (wrongly)
// match an unrelated named type like "MyError" or "ErrorCode".
func returnsBuiltinError(returnType string) bool {
	fields := strings.FieldsFunc(returnType, func(r rune) bool {
		return r == '(' || r == ')' || r == ',' || r == ' ' || r == '\t' || r == '*'
	})
	for _, f := range fields {
		if f == "error" {
			return true
		}
	}
	return false
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
				Doc:       "Concurrency primitive: " + c.extractDoc(n),
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
					Doc:  c.extractDoc(n),
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
				Doc:       c.extractDoc(n),
			})
		}
	})
}

// 9: Callgraph Facts — outbound callees plus inbound callers.
//
// Outbound: each entry point's CALLS targets become kind "callee" facts.
// Inbound (B3): GetInboundEdges (verified on CodePropertyGraph) resolves
// callers for each entry point AND, one hop further, the inbound callers of
// every callee found above. Caller facts (kind "caller") are deduped against
// each other and against symbols already present in the payload.
func (c *Collector) collectCallgraphFacts(scope *config.ScopeRule, p *config.GroundTruthPayload) {
	if c.graph == nil || scope == nil {
		return
	}
	seen := make(map[string]bool)
	// NOTE: dedupe covers facts added by this collector only; a callee
	// that is also an in-scope signature keeps its historical duplicate
	// "callee" fact (pre-B3 behavior relied on by callers).
	appendFact := func(fqn, kind, signature string) {
		if fqn == "" || seen[fqn] {
			return
		}
		seen[fqn] = true
		fact := config.SymbolFact{
			FQN:       fqn,
			Kind:      kind,
			Signature: signature,
		}
		if node, ok := c.graph.Nodes.Get(fqn); ok && node != nil {
			fact.File = node.FileSpec.Path
			fact.Line = node.FileSpec.LineStart
			fact.Permalink = FormatPermalink(node.FileSpec.Path, node.FileSpec.LineStart, node.FileSpec.LineEnd)
		}
		p.Symbols = append(p.Symbols, fact)
	}

	// Outbound pass: entry points → callees.
	var callees []string
	calleeSeen := make(map[string]bool)
	if c.graph.OutboundEdges != nil {
		for _, ep := range scope.EntryPoints {
			for _, edge := range c.graph.GetOutboundEdges(ep) {
				if edge.Type != link.EdgeCalls {
					continue
				}
				appendFact(edge.TargetID, "callee", "Called by "+cleanSymbolName(ep))
				if edge.TargetID != "" && !calleeSeen[edge.TargetID] {
					calleeSeen[edge.TargetID] = true
					callees = append(callees, edge.TargetID)
				}
			}
		}
	}

	// Inbound pass: callers of entry points + one-hop callers of callees.
	if c.graph.InboundEdges == nil {
		return
	}
	targets := append([]string(nil), scope.EntryPoints...)
	targets = append(targets, callees...)
	for _, target := range targets {
		if target == "" {
			continue
		}
		for _, edge := range c.graph.GetInboundEdges(target) {
			if edge.Type != link.EdgeCalls {
				continue
			}
			appendFact(edge.SourceID, "caller", "Calls "+cleanSymbolName(target))
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

// 14: Egress calls — outbound CPG call edges from in-scope symbols to
// targets outside the scope (third-party APIs, DB drivers, gRPC clients).
func (c *Collector) collectEgressCalls(scope *config.ScopeRule, p *config.GroundTruthPayload) {
	if c.graph == nil || c.graph.OutboundEdges == nil {
		return
	}
	seen := make(map[string]bool)
	c.graph.Nodes.Iterate(func(id string, n *link.ResolvedNode) {
		if n == nil || !catalog.MatchesScope(scope, n.FileSpec.Path) {
			return
		}
		for _, e := range c.graph.GetOutboundEdges(id) {
			if e.Type != link.EdgeCalls || seen[e.TargetID] {
				continue
			}
			seen[e.TargetID] = true
			if tgt, ok := c.graph.Nodes.Get(e.TargetID); ok && tgt != nil {
				if catalog.MatchesScope(scope, tgt.FileSpec.Path) {
					continue // internal call, not egress
				}
			}
			p.Symbols = append(p.Symbols, config.SymbolFact{
				FQN:       e.TargetID,
				Kind:      "egress",
				Signature: "Called from " + cleanSymbolName(id),
			})
		}
	})
}

// 15: Cryptographic primitives — AKG nodes referencing crypto libraries
// (crypto/*, sha256, aes, tls, bcrypt, hmac, jwt, rsa/ecdsa).
func (c *Collector) collectCryptoPrimitives(scope *config.ScopeRule, p *config.GroundTruthPayload) {
	c.graph.Nodes.Iterate(func(id string, n *link.ResolvedNode) {
		if n == nil || !catalog.MatchesScope(scope, n.FileSpec.Path) {
			return
		}
		haystack := strings.ToLower(id + " " + n.Name + " " + extractSignature(n) + " " + n.FileSpec.Path)
		for _, kw := range []string{"crypto", "sha256", "sha512", "aes", "tls", "bcrypt", "hmac", "jwt", "rsa", "ecdsa", "x509"} {
			if strings.Contains(haystack, kw) {
				p.Symbols = append(p.Symbols, config.SymbolFact{
					FQN:       id,
					Kind:      "crypto",
					File:      n.FileSpec.Path,
					Line:      n.FileSpec.LineStart,
					Permalink: FormatPermalink(n.FileSpec.Path, n.FileSpec.LineStart, n.FileSpec.LineEnd),
					Signature: extractSignature(n),
					Doc:       "Cryptographic primitive: " + c.extractDoc(n),
				})
				return
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

// extractSignature returns a symbol's displayed signature. The AKG never
// populates an explicit "signature" property today (every node falls
// through to the second branch), so this reconstructs one from "content" —
// the raw source text the AKG DOES capture for func/type declarations, e.g.
// "func Login(username, password string) (string, error) {\n\t..." — by
// taking everything up to the opening brace. Falls back to the bare node
// name when content is absent or has no brace (e.g. a var, or a language
// where content isn't captured this way).
func extractSignature(n *link.ResolvedNode) string {
	if n == nil {
		return ""
	}
	if sig, ok := n.Properties["signature"]; ok && sig != "" {
		return sig
	}
	if content, ok := n.Properties["content"]; ok && content != "" {
		if sig := signatureFromContent(content); sig != "" {
			return sig
		}
	}
	return n.Name
}

// signatureFromContent extracts a declaration header from raw source text:
// everything up to (not including) the first "{", collapsed to a single
// whitespace-normalized line. Returns "" when there's no "{" at all, so
// callers fall back cleanly instead of returning a whole multi-line body.
func signatureFromContent(content string) string {
	idx := strings.IndexByte(content, '{')
	if idx < 0 {
		return ""
	}
	return strings.TrimSpace(strings.Join(strings.Fields(content[:idx]), " "))
}

// extractDoc returns a symbol's doc comment. The AKG never populates a
// "doc_comment"/"doc" property today, so — when the Collector has a
// repoRoot (see WithRepoRoot) — this falls back to parsing the symbol's own
// Go source file directly for its preceding doc comment, the one thing
// "content" (used by extractSignature above) does not capture at all.
// Without repoRoot (e.g. every existing test's plain NewCollector), this
// returns "" exactly as before — a strictly additive fallback.
func (c *Collector) extractDoc(n *link.ResolvedNode) string {
	if n == nil {
		return ""
	}
	if doc, ok := n.Properties["doc_comment"]; ok && doc != "" {
		return doc
	}
	if doc, ok := n.Properties["doc"]; ok && doc != "" {
		return doc
	}
	if c == nil || c.repoRoot == "" || !strings.HasSuffix(n.FileSpec.Path, ".go") {
		return ""
	}
	return c.fileDocComments(n.FileSpec.Path)[n.Name]
}

// fileDocComments returns (parsing and caching once per Collector) a
// bare-name -> doc-comment map for one repo-relative Go file: top-level
// func/method declarations, and type/var/const declarations (including
// grouped `var ( ... )` blocks, where a spec's own doc comment wins over the
// block's shared one). Best-effort: an unparseable file yields an empty,
// still-cached map — never re-attempted, never fatal.
func (c *Collector) fileDocComments(relPath string) map[string]string {
	if c.srcDocCache == nil {
		c.srcDocCache = make(map[string]map[string]string)
	}
	if m, ok := c.srcDocCache[relPath]; ok {
		return m
	}
	m := make(map[string]string)
	c.srcDocCache[relPath] = m

	full := filepath.Join(c.repoRoot, filepath.FromSlash(relPath))
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, full, nil, parser.ParseComments)
	if err != nil || node == nil {
		return m
	}
	for _, decl := range node.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if doc := strings.TrimSpace(d.Doc.Text()); doc != "" {
				m[d.Name.Name] = doc
			}
		case *ast.GenDecl:
			groupDoc := strings.TrimSpace(d.Doc.Text())
			for _, spec := range d.Specs {
				specDoc := groupDoc
				var names []*ast.Ident
				switch s := spec.(type) {
				case *ast.TypeSpec:
					if s.Doc != nil {
						specDoc = strings.TrimSpace(s.Doc.Text())
					}
					names = []*ast.Ident{s.Name}
				case *ast.ValueSpec:
					if s.Doc != nil {
						specDoc = strings.TrimSpace(s.Doc.Text())
					}
					names = s.Names
				}
				if specDoc == "" {
					continue
				}
				for _, name := range names {
					m[name.Name] = specDoc
				}
			}
		}
	}
	return m
}
