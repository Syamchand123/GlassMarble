package arch_linter

import (
	"fmt"
	"os"
	"path"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// Severity indicates the rule violation severity level.
type Severity string

const (
	SeverityError   Severity = "ERROR"
	SeverityWarning Severity = "WARN"
	SeverityInfo    Severity = "INFO"
)

// Rule represents a single architectural linting constraint.
type Rule struct {
	ID            string   `yaml:"id" json:"id"`
	Name          string   `yaml:"name" json:"name"`
	Severity      Severity `yaml:"severity" json:"severity"`
	Description   string   `yaml:"description,omitempty" json:"description,omitempty"`
	From          string   `yaml:"from,omitempty" json:"from,omitempty"`
	DenyImports   []string `yaml:"deny_imports,omitempty" json:"deny_imports,omitempty"`
	AllowOnly     []string `yaml:"allow_only,omitempty" json:"allow_only,omitempty"`
	RequireLayer  string   `yaml:"require_layer,omitempty" json:"require_layer,omitempty"`
	PreventCycles bool     `yaml:"prevent_cycles,omitempty" json:"prevent_cycles,omitempty"`
	MaxFanOut     int      `yaml:"max_fan_out,omitempty" json:"max_fan_out,omitempty"`
	MaxFanIn      int      `yaml:"max_fan_in,omitempty" json:"max_fan_in,omitempty"`
	Scope         string   `yaml:"scope,omitempty" json:"scope,omitempty"`
}

// Ruleset defines a collection of architectural rules for a project.
type Ruleset struct {
	Version     string   `yaml:"version" json:"version"`
	Name        string   `yaml:"name,omitempty" json:"name,omitempty"`
	Description string   `yaml:"description,omitempty" json:"description,omitempty"`
	Rules       []Rule   `yaml:"rules" json:"rules"`
	Exclude     []string `yaml:"exclude,omitempty" json:"exclude,omitempty"`
}

// Violation represents an identified violation of an architectural rule.
type Violation struct {
	RuleID      string   `json:"rule_id"`
	RuleName    string   `json:"rule_name"`
	Severity    Severity `json:"severity"`
	SourcePath  string   `json:"source_path"`
	SourceNode  string   `json:"source_node,omitempty"`
	SourceLine  int      `json:"source_line,omitempty"`
	TargetPath  string   `json:"target_path,omitempty"`
	TargetNode  string   `json:"target_node,omitempty"`
	TargetLine  int      `json:"target_line,omitempty"`
	EdgeKind    string   `json:"edge_kind,omitempty"`
	Message     string   `json:"message"`
	Suggestion  string   `json:"suggestion,omitempty"`
}

// LintResult contains the complete output of an architectural lint run.
type LintResult struct {
	RulesTotal      int         `json:"rules_total"`
	RulesPassed     int         `json:"rules_passed"`
	ViolationsTotal int         `json:"violations_total"`
	ErrorsCount     int         `json:"errors_count"`
	WarningsCount   int         `json:"warnings_count"`
	Passed          bool        `json:"passed"`
	Violations      []Violation `json:"violations"`
}

// LoadRules loads and parses a ruleset from a YAML file.
func LoadRules(path string) (*Ruleset, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read rules file %q: %w", path, err)
	}

	var rs Ruleset
	if err := yaml.Unmarshal(data, &rs); err != nil {
		return nil, fmt.Errorf("failed to parse rules YAML %q: %w", path, err)
	}

	if rs.Version == "" {
		rs.Version = "1"
	}

	for i := range rs.Rules {
		if rs.Rules[i].Severity == "" {
			rs.Rules[i].Severity = SeverityError
		}
		if rs.Rules[i].ID == "" {
			rs.Rules[i].ID = fmt.Sprintf("rule-%d", i+1)
		}
		if rs.Rules[i].Name == "" {
			rs.Rules[i].Name = rs.Rules[i].ID
		}
	}

	return &rs, nil
}

// MatchGlob checks if a given file path matches a glob pattern (supporting **).
func MatchGlob(pattern, filePath string) bool {
	if pattern == "" || pattern == "*" || pattern == "**" {
		return true
	}

	// Normalize slashes
	pattern = strings.ReplaceAll(pattern, "\\", "/")
	filePath = strings.ReplaceAll(filePath, "\\", "/")
	pattern = strings.TrimPrefix(pattern, "./")
	filePath = strings.TrimPrefix(filePath, "./")

	// Direct match
	if pattern == filePath {
		return true
	}

	// Suffix match for directory prefixes (e.g. "cmd/" matches "cmd/root.go")
	if strings.HasSuffix(pattern, "/") && strings.HasPrefix(filePath, pattern) {
		return true
	}

	// Handle recursive ** globbing via regex
	if strings.Contains(pattern, "**") {
		regexPattern := globToRegex(pattern)
		re, err := regexp.Compile(regexPattern)
		if err != nil {
			return false
		}
		return re.MatchString(filePath)
	}

	// Use path.Match (which always treats '/' as the directory separator)
	matched, err := path.Match(pattern, filePath)
	if err == nil && matched {
		return true
	}

	return false
}

func globToRegex(glob string) string {
	var b strings.Builder
	b.WriteString("^")
	i := 0
	for i < len(glob) {
		if i+1 < len(glob) && glob[i:i+2] == "**" {
			if i+2 < len(glob) && glob[i+2] == '/' {
				b.WriteString("(?:.*/)?")
				i += 3
			} else {
				b.WriteString(".*")
				i += 2
			}
		} else if glob[i] == '*' {
			b.WriteString("[^/]*")
			i++
		} else if glob[i] == '?' {
			b.WriteString("[^/]")
			i++
		} else if strings.ContainsRune(".+()[]{}|\\^$", rune(glob[i])) {
			b.WriteString("\\")
			b.WriteByte(glob[i])
			i++
		} else {
			b.WriteByte(glob[i])
			i++
		}
	}
	b.WriteString("$")
	return b.String()
}
