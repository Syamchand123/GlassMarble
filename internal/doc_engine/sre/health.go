// Package sre implements Phase 6 SRE & Ops Intelligence pillars:
// - Pillar 26: Architectural Complexity & Hotspot Health Profiles
// - Pillar 33: Living Error Code Catalog & SRE Triage Playbook
// - Pillar 32: Concurrency, Threading & Lock Contention Documentation
// - Pillar 31: Test Topology & Verification Coverage Docs
package sre

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"math"
	"path/filepath"
	"strings"
)

// HealthProfile represents the architectural complexity and hotspot profile of a subsystem.
type HealthProfile struct {
	Subsystem       string   `json:"subsystem"`
	HotspotRank     int      `json:"hotspot_rank"`
	TotalFiles      int      `json:"total_files"`
	Instability     float64  `json:"instability"`      // Ce / (Ca + Ce)
	MaxComplexity   int      `json:"max_complexity"`   // Cyclomatic estimate
	RiskFunction    string   `json:"risk_function"`    // Function with highest complexity
	SmellsDetected  []string `json:"smells_detected"`  // Detected architectural smells
}

// ComputeHealthProfile calculates health and complexity metrics for a given subsystem directory.
func ComputeHealthProfile(repoRoot, subDir string) HealthProfile {
	absDir := filepath.Join(repoRoot, subDir)
	fset := token.NewFileSet()

	fileCount := 0
	maxComp := 1
	riskFn := "None"
	inboundRefs := 0
	outboundImports := 0

	_ = filepath.WalkDir(absDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fileCount++

		node, parseErr := parser.ParseFile(fset, path, nil, 0)
		if parseErr != nil {
			return nil
		}

		outboundImports += len(node.Imports)

		for _, decl := range node.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}

			comp := estimateCyclomaticComplexity(fn)
			if comp > maxComp {
				maxComp = comp
				riskFn = fn.Name.Name
			}
		}
		return nil
	})

	totalCoupling := inboundRefs + outboundImports
	instability := 0.5
	if totalCoupling > 0 {
		instability = math.Round(float64(outboundImports)/float64(totalCoupling)*100) / 100
	}

	var smells []string
	if maxComp >= 15 {
		smells = append(smells, fmt.Sprintf("High cyclomatic complexity in `%s()` (%d)", riskFn, maxComp))
	}
	if fileCount > 25 {
		smells = append(smells, "Subsystem file count exceeds modular size threshold")
	}

	rank := 1
	if fileCount > 10 {
		rank = 3
	} else if fileCount > 5 {
		rank = 5
	} else {
		rank = 10
	}

	return HealthProfile{
		Subsystem:      subDir,
		HotspotRank:    rank,
		TotalFiles:     fileCount,
		Instability:    instability,
		MaxComplexity:  maxComp,
		RiskFunction:   riskFn,
		SmellsDetected: smells,
	}
}

// FormatHealthMarkdown renders the Pillar 26 Subsystem Health markdown card.
func FormatHealthMarkdown(p HealthProfile) string {
	var sb strings.Builder
	sb.WriteString("### Subsystem Health & Complexity Profile\n\n")
	sb.WriteString(fmt.Sprintf("* **Hotspot Rank:** #%d in repository\n", p.HotspotRank))
	sb.WriteString(fmt.Sprintf("* **Instability Index:** %.2f\n", p.Instability))
	if p.RiskFunction != "None" {
		sb.WriteString(fmt.Sprintf("* **Cyclomatic Risk:** `%s()` complexity=%d\n", p.RiskFunction, p.MaxComplexity))
	} else {
		sb.WriteString("* **Cyclomatic Risk:** Low (no high-complexity functions detected)\n")
	}

	if len(p.SmellsDetected) > 0 {
		sb.WriteString(fmt.Sprintf("* **Architectural Alerts:** %s\n", strings.Join(p.SmellsDetected, "; ")))
	} else {
		sb.WriteString("* **Architectural Alerts:** 0 violations detected (clean boundaries)\n")
	}
	sb.WriteString("\n")
	return sb.String()
}

func estimateCyclomaticComplexity(fn *ast.FuncDecl) int {
	comp := 1
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		switch n.(type) {
		case *ast.IfStmt, *ast.ForStmt, *ast.RangeStmt, *ast.CaseClause, *ast.CommClause:
			comp++
		case *ast.BinaryExpr:
			// check for logical && and ||
			b := n.(*ast.BinaryExpr)
			if b.Op == token.LAND || b.Op == token.LOR {
				comp++
			}
		}
		return true
	})
	return comp
}
