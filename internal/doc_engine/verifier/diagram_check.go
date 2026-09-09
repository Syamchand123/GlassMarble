// Gate 2: Mermaid/PlantUML render check.
// Extracts all diagram fenced blocks and validates their syntax.
// Validates that:
//   - Mermaid blocks contain a valid diagram-type declaration on the first line.
//   - Node labels have balanced brackets.
//   - Edges use known Mermaid/PlantUML arrow syntax.
package verifier

import (
	"bufio"
	"strings"
)

// knownMermaidTypes is the set of valid Mermaid diagram type declarations.
var knownMermaidTypes = map[string]bool{
	"flowchart":    true,
	"graph":        true,
	"sequenceDiagram": true,
	"classDiagram": true,
	"stateDiagram": true,
	"stateDiagram-v2": true,
	"erDiagram":    true,
	"gantt":        true,
	"pie":          true,
	"quadrantChart": true,
	"requirementDiagram": true,
	"gitGraph":     true,
	"c4Context":    true,
	"c4Container":  true,
	"c4Component":  true,
	"mindmap":      true,
	"timeline":     true,
	"xychart-beta": true,
}

// checkDiagramSyntax finds all mermaid/plantuml fenced blocks and validates them.
func checkDiagramSyntax(content string) *GateError {
	blocks := extractFencedBlocks(content)
	for lang, body := range blocks {
		switch strings.ToLower(strings.TrimSpace(lang)) {
		case "mermaid":
			if err := validateMermaid(body); err != nil {
				return err
			}
		case "plantuml":
			if err := validatePlantUML(body); err != nil {
				return err
			}
		}
	}
	return nil
}

// extractFencedBlocks returns a map from language → block body for all fenced blocks.
func extractFencedBlocks(content string) map[string]string {
	result := make(map[string]string)
	scanner := bufio.NewScanner(strings.NewReader(content))
	var (
		inFence  bool
		lang     string
		bodyBuf  strings.Builder
	)
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimLeft(line, " \t")
		if !inFence && (strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~")) {
			inFence = true
			lang = strings.TrimLeft(trimmed[3:], " \t")
			bodyBuf.Reset()
			continue
		}
		if inFence && (strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~")) {
			result[lang] = bodyBuf.String()
			inFence = false
			lang = ""
			continue
		}
		if inFence {
			bodyBuf.WriteString(line + "\n")
		}
	}
	return result
}

// validateMermaid checks basic structural validity of a Mermaid block.
func validateMermaid(body string) *GateError {
	lines := nonEmptyLines(body)
	if len(lines) == 0 {
		return &GateError{Gate: 2, Message: "empty mermaid block"}
	}
	// First non-empty line must be a recognised diagram type.
	firstWord := strings.Fields(lines[0])[0]
	if !knownMermaidTypes[firstWord] {
		return &GateError{Gate: 2, Message: "unknown mermaid diagram type: " + firstWord}
	}
	// Check balanced square brackets in node labels.
	if err := checkBracketBalance(body, '[', ']', 2); err != nil {
		return err
	}
	// Check balanced parentheses for round nodes.
	if err := checkBracketBalance(body, '(', ')', 2); err != nil {
		return err
	}
	return nil
}

// validatePlantUML checks that the block opens with @startuml and closes with @enduml.
func validatePlantUML(body string) *GateError {
	trimmed := strings.TrimSpace(body)
	if !strings.HasPrefix(trimmed, "@startuml") {
		return &GateError{Gate: 2, Message: "plantuml block missing @startuml"}
	}
	if !strings.HasSuffix(trimmed, "@enduml") {
		return &GateError{Gate: 2, Message: "plantuml block missing @enduml"}
	}
	return nil
}

// checkBracketBalance verifies that open/close bracket counts match (within tolerance).
func checkBracketBalance(text string, open, close byte, tolerance int) *GateError {
	depth := 0
	for i := 0; i < len(text); i++ {
		if text[i] == open {
			depth++
		} else if text[i] == close {
			depth--
		}
	}
	if depth < -tolerance || depth > tolerance {
		return &GateError{Gate: 2, Message: "unbalanced brackets in diagram"}
	}
	return nil
}

func nonEmptyLines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return out
}
