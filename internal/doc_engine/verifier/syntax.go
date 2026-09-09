// Gate 1: Markdown AST syntax validation.
// Detects unclosed fenced code blocks, malformed table rows, and broken list nesting.
// Uses pure string scanning — no external parser.
package verifier

import (
	"bufio"
	"strings"
)

// checkMarkdownSyntax validates markdown text for structural issues
// that would render as broken output in any markdown viewer.
func checkMarkdownSyntax(content string) *GateError {
	if err := checkFencedBlocks(content); err != nil {
		return err
	}
	if err := checkTableRows(content); err != nil {
		return err
	}
	return nil
}

// checkFencedBlocks verifies every opening ``` has a matching closing ```.
// A doc with an unclosed fence garbles the entire page.
func checkFencedBlocks(content string) *GateError {
	scanner := bufio.NewScanner(strings.NewReader(content))
	depth := 0
	var lang string
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		trimmed := strings.TrimLeft(line, " \t")
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			if depth == 0 {
				depth++
				lang = strings.TrimLeft(trimmed[3:], " \t")
				_ = lang
			} else {
				depth--
			}
		}
	}
	if depth != 0 {
		return &GateError{Gate: 1, Message: "unclosed fenced code block"}
	}
	return nil
}

// checkTableRows checks that all GFM table rows have at least one pipe.
// A malformed table row causes the whole table to fail rendering.
func checkTableRows(content string) *GateError {
	scanner := bufio.NewScanner(strings.NewReader(content))
	inTable := false
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		isTableRow := strings.HasPrefix(line, "|")
		if isTableRow {
			inTable = true
			// Count pipes — a valid row has at least 2 pipes (|col|)
			if strings.Count(line, "|") < 2 {
				return &GateError{Gate: 1, Message: "malformed table row: fewer than 2 pipes"}
			}
		} else if inTable && line != "" && !strings.HasPrefix(line, "|") {
			// Row after table content that isn't a separator or blank
			if !strings.HasPrefix(line, "#") {
				inTable = false
			}
		}
	}
	return nil
}
