package arch_linter

import (
	"fmt"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/link"
)

// Lint executes the ruleset against the in-memory CodePropertyGraph.
func Lint(graph *akg.CodePropertyGraph, ruleset *Ruleset) (*LintResult, error) {
	if ruleset == nil {
		return nil, fmt.Errorf("ruleset is nil")
	}

	res := &LintResult{
		RulesTotal: len(ruleset.Rules),
		Passed:     true,
		Violations: make([]Violation, 0),
	}

	if graph == nil || graph.Nodes == nil || graph.Nodes.Len() == 0 {
		res.RulesPassed = res.RulesTotal
		return res, nil
	}

	// Cache node paths and package paths
	nodePath := make(map[string]string)
	nodePackage := make(map[string]string)
	nodeName := make(map[string]string)
	nodeLine := make(map[string]int)

	graph.Nodes.Iterate(func(id string, node *link.ResolvedNode) {
		if node == nil {
			return
		}
		p := normalizePath(node.FileSpec.Path)
		nodePath[id] = p
		nodePackage[id] = path.Dir(p)
		nodeName[id] = node.Name
		nodeLine[id] = node.FileSpec.LineStart
	})

	// Pre-build outbound edges map & package-level dependencies
	outboundEdges := make(map[string][]link.ResolvedEdge)
	packageDeps := make(map[string]map[string]bool) // pkg -> set of imported pkgs

	graph.OutboundEdges.Iterate(func(srcID string, edges []link.ResolvedEdge) {
		outboundEdges[srcID] = edges
		srcPkg := nodePackage[srcID]
		if srcPkg == "" {
			return
		}

		for _, e := range edges {
			tgtPkg := nodePackage[e.TargetID]
			if tgtPkg == "" || tgtPkg == srcPkg {
				continue
			}
			if isDependencyEdge(e.Type) {
				if packageDeps[srcPkg] == nil {
					packageDeps[srcPkg] = make(map[string]bool)
				}
				packageDeps[srcPkg][tgtPkg] = true
			}
		}
	})

	// Evaluate each rule
	for _, rule := range ruleset.Rules {
		ruleViolations := evaluateRule(rule, graph, nodePath, nodeName, nodeLine, outboundEdges, packageDeps, ruleset.Exclude)
		if len(ruleViolations) == 0 {
			res.RulesPassed++
		} else {
			res.Violations = append(res.Violations, ruleViolations...)
		}
	}

	// Calculate aggregates
	res.ViolationsTotal = len(res.Violations)
	for _, v := range res.Violations {
		if v.Severity == SeverityError {
			res.ErrorsCount++
			res.Passed = false
		} else if v.Severity == SeverityWarning {
			res.WarningsCount++
		}
	}

	// Sort violations deterministically by source path, line, rule
	sort.Slice(res.Violations, func(i, j int) bool {
		if res.Violations[i].SourcePath != res.Violations[j].SourcePath {
			return res.Violations[i].SourcePath < res.Violations[j].SourcePath
		}
		if res.Violations[i].SourceLine != res.Violations[j].SourceLine {
			return res.Violations[i].SourceLine < res.Violations[j].SourceLine
		}
		return res.Violations[i].RuleID < res.Violations[j].RuleID
	})

	return res, nil
}

func evaluateRule(
	rule Rule,
	graph *akg.CodePropertyGraph,
	nodePath map[string]string,
	nodeName map[string]string,
	nodeLine map[string]int,
	outboundEdges map[string][]link.ResolvedEdge,
	packageDeps map[string]map[string]bool,
	excludes []string,
) []Violation {
	var violations []Violation

	// 1. Deny Imports / Forbidden Dependencies
	if len(rule.DenyImports) > 0 && rule.From != "" {
		graph.Nodes.Iterate(func(srcID string, node *link.ResolvedNode) {
			srcFile := nodePath[srcID]
			if srcFile == "" || isExcluded(srcFile, excludes) {
				return
			}
			if !MatchGlob(rule.From, srcFile) {
				return
			}

			for _, e := range outboundEdges[srcID] {
				tgtFile := nodePath[e.TargetID]
				if tgtFile == "" || tgtFile == srcFile || isExcluded(tgtFile, excludes) {
					continue
				}
				if !isDependencyEdge(e.Type) {
					continue
				}

				for _, denied := range rule.DenyImports {
					if MatchGlob(denied, tgtFile) {
						violations = append(violations, Violation{
							RuleID:      rule.ID,
							RuleName:    rule.Name,
							Severity:    rule.Severity,
							SourcePath:  srcFile,
							SourceNode:  nodeName[srcID],
							SourceLine:  nodeLine[srcID],
							TargetPath:  tgtFile,
							TargetNode:  nodeName[e.TargetID],
							TargetLine:  nodeLine[e.TargetID],
							EdgeKind:    string(e.Type),
							Message:     fmt.Sprintf("Forbidden dependency: %s must not import or call %s", srcFile, tgtFile),
							Suggestion:  fmt.Sprintf("Decouple %s from %s using an interface or dependency inversion", nodeName[srcID], nodeName[e.TargetID]),
						})
						break
					}
				}
			}
		})
	}

	// 2. Allow Only (Whitelist)
	if len(rule.AllowOnly) > 0 && rule.From != "" {
		graph.Nodes.Iterate(func(srcID string, node *link.ResolvedNode) {
			srcFile := nodePath[srcID]
			if srcFile == "" || isExcluded(srcFile, excludes) {
				return
			}
			if !MatchGlob(rule.From, srcFile) {
				return
			}

			srcDir := path.Dir(srcFile)
			for _, e := range outboundEdges[srcID] {
				tgtFile := nodePath[e.TargetID]
				if tgtFile == "" || tgtFile == srcFile || isExcluded(tgtFile, excludes) {
					continue
				}
				if !isDependencyEdge(e.Type) {
					continue
				}
				// Same directory/package is allowed by default
				if path.Dir(tgtFile) == srcDir {
					continue
				}

				allowed := false
				for _, allowPattern := range rule.AllowOnly {
					if MatchGlob(allowPattern, tgtFile) {
						allowed = true
						break
					}
				}

				if !allowed {
					violations = append(violations, Violation{
						RuleID:      rule.ID,
						RuleName:    rule.Name,
						Severity:    rule.Severity,
						SourcePath:  srcFile,
						SourceNode:  nodeName[srcID],
						SourceLine:  nodeLine[srcID],
						TargetPath:  tgtFile,
						TargetNode:  nodeName[e.TargetID],
						TargetLine:  nodeLine[e.TargetID],
						EdgeKind:    string(e.Type),
						Message:     fmt.Sprintf("Unapproved dependency: %s is not in allowlist for %s", tgtFile, srcFile),
						Suggestion:  fmt.Sprintf("Update allow_only rules or remove reference to %s", tgtFile),
					})
				}
			}
		})
	}

	// 3. Require Layer (Inversion of Control Enforcement)
	if rule.RequireLayer != "" && rule.From != "" {
		graph.Nodes.Iterate(func(srcID string, node *link.ResolvedNode) {
			srcFile := nodePath[srcID]
			if srcFile == "" || isExcluded(srcFile, excludes) {
				return
			}
			// Look for callers of `rule.From`
			for _, e := range outboundEdges[srcID] {
				tgtFile := nodePath[e.TargetID]
				if tgtFile == "" || tgtFile == srcFile || isExcluded(tgtFile, excludes) {
					continue
				}
				if !isDependencyEdge(e.Type) {
					continue
				}

				if MatchGlob(rule.From, tgtFile) {
					// The caller (srcFile) must match RequireLayer
					if !MatchGlob(rule.RequireLayer, srcFile) && path.Dir(srcFile) != path.Dir(tgtFile) {
						violations = append(violations, Violation{
							RuleID:      rule.ID,
							RuleName:    rule.Name,
							Severity:    rule.Severity,
							SourcePath:  srcFile,
							SourceNode:  nodeName[srcID],
							SourceLine:  nodeLine[srcID],
							TargetPath:  tgtFile,
							TargetNode:  nodeName[e.TargetID],
							TargetLine:  nodeLine[e.TargetID],
							EdgeKind:    string(e.Type),
							Message:     fmt.Sprintf("Layer violation: %s must only be accessed via %s (called directly by %s)", tgtFile, rule.RequireLayer, srcFile),
							Suggestion:  fmt.Sprintf("Route calls through %s intermediate layer", rule.RequireLayer),
						})
					}
				}
			}
		})
	}

	// 4. Prevent Cycles
	if rule.PreventCycles {
		scope := rule.Scope
		if scope == "" {
			scope = "**"
		}
		cycles := detectCycles(packageDeps, scope)
		for _, cycle := range cycles {
			violations = append(violations, Violation{
				RuleID:      rule.ID,
				RuleName:    rule.Name,
				Severity:    rule.Severity,
				SourcePath:  cycle[0],
				Message:     fmt.Sprintf("Circular dependency cycle detected: %s", strings.Join(cycle, " -> ")),
				Suggestion:  "Extract shared interface into a separate package or break cycle via dependency inversion",
			})
		}
	}

	// 5. Max Fan-Out Limit
	if rule.MaxFanOut > 0 {
		scope := rule.Scope
		if scope == "" {
			scope = "**"
		}

		for pkg, deps := range packageDeps {
			if !MatchGlob(scope, pkg) {
				continue
			}
			if len(deps) > rule.MaxFanOut {
				violations = append(violations, Violation{
					RuleID:      rule.ID,
					RuleName:    rule.Name,
					Severity:    rule.Severity,
					SourcePath:  pkg,
					Message:     fmt.Sprintf("Excessive coupling (Fan-Out = %d, Max = %d): package %s depends on too many packages", len(deps), rule.MaxFanOut, pkg),
					Suggestion:  "Decompose package into smaller cohesive units with focused responsibilities",
				})
			}
		}
	}

	return violations
}

func isDependencyEdge(t link.RelationshipType) bool {
	switch t {
	case link.EdgeDependsOn, link.EdgeCalls, link.EdgeContextCall, link.EdgeImplements,
		link.EdgeExtends, link.EdgeComposes, link.EdgeReferences, link.EdgeQueriesDB,
		link.EdgeSendsTo, link.EdgeInstantiates:
		return true
	default:
		return false
	}
}

func normalizePath(p string) string {
	return filepath.ToSlash(strings.TrimPrefix(p, "./"))
}

func isExcluded(p string, excludes []string) bool {
	for _, excl := range excludes {
		if MatchGlob(excl, p) {
			return true
		}
	}
	return false
}

// detectCycles uses DFS to find elementary cycles among packages matching scope.
func detectCycles(graph map[string]map[string]bool, scope string) [][]string {
	var cycles [][]string
	visited := make(map[string]int) // 0 = unvisited, 1 = visiting, 2 = visited
	var stack []string

	var dfs func(u string)
	dfs = func(u string) {
		visited[u] = 1
		stack = append(stack, u)

		// Deterministic iteration order
		var neighbors []string
		for v := range graph[u] {
			if MatchGlob(scope, v) {
				neighbors = append(neighbors, v)
			}
		}
		sort.Strings(neighbors)

		for _, v := range neighbors {
			if visited[v] == 1 {
				// Cycle found: extract from stack
				idx := -1
				for i, node := range stack {
					if node == v {
						idx = i
						break
					}
				}
				if idx != -1 {
					cycle := append([]string{}, stack[idx:]...)
					cycle = append(cycle, v)
					cycles = append(cycles, cycle)
				}
			} else if visited[v] == 0 {
				dfs(v)
			}
		}

		stack = stack[:len(stack)-1]
		visited[u] = 2
	}

	var allNodes []string
	for u := range graph {
		if MatchGlob(scope, u) {
			allNodes = append(allNodes, u)
		}
	}
	sort.Strings(allNodes)

	for _, u := range allNodes {
		if visited[u] == 0 {
			dfs(u)
		}
	}

	return cycles
}
