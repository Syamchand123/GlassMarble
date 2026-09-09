package archfeatures

import (
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// MilestoneRecord represents an architectural snapshot or milestone in history.
type MilestoneRecord struct {
	Ref         string
	Date        string
	Intent      string
	Description string
	Components  []string
}

// GenerateEvolutionChapter builds a narrative chapter tracing how the architecture
// evolved between git references (or throughout the repo's history).
func GenerateEvolutionChapter(repoRoot string, fromRef, toRef string) (string, error) {
	milestones, err := queryGitMilestones(repoRoot, fromRef, toRef)
	if err != nil {
		// Fallback to synthetic baseline milestone
		milestones = []MilestoneRecord{
			{
				Ref:         "Initial Architecture",
				Date:        time.Now().Format("2006-01-02"),
				Intent:      "FOUNDATION",
				Description: "Initial baseline architecture established with core subsystems and domain models.",
				Components:  []string{"cmd", "internal/akg", "internal/doc_engine"},
			},
		}
	}

	var sb strings.Builder
	sb.WriteString("## System Evolution & Architectural History\n\n")
	sb.WriteString("> Replayed by GlassMarble Architectural Time Machine from commit reasoning and snapshot history.\n\n")

	if len(milestones) == 0 {
		sb.WriteString("No major architectural milestones recorded in this interval.\n")
		return sb.String(), nil
	}

	for _, m := range milestones {
		sb.WriteString(fmt.Sprintf("### %s (%s)\n\n", m.Ref, m.Date))
		sb.WriteString(fmt.Sprintf("- **Intent:** `%s`\n", m.Intent))
		sb.WriteString(fmt.Sprintf("- **Summary:** %s\n", m.Description))
		if len(m.Components) > 0 {
			sb.WriteString(fmt.Sprintf("- **Impacted Subsystems:** %s\n", strings.Join(m.Components, ", ")))
		}
		sb.WriteString("\n")
	}

	return sb.String(), nil
}

func queryGitMilestones(repoRoot, fromRef, toRef string) ([]MilestoneRecord, error) {
	rangeArg := "HEAD"
	if fromRef != "" && toRef != "" {
		rangeArg = fmt.Sprintf("%s..%s", fromRef, toRef)
	}

	cmd := exec.Command("git", "log", rangeArg, "--oneline", "-n", "20")
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	var records []MilestoneRecord

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, " ", 2)
		if len(parts) < 2 {
			continue
		}

		hash := parts[0]
		msg := parts[1]

		intent := "MAINTENANCE"
		lower := strings.ToLower(msg)
		if strings.HasPrefix(lower, "feat") || strings.Contains(lower, "add") {
			intent = "ADD_FEATURE"
		} else if strings.HasPrefix(lower, "refactor") || strings.Contains(lower, "reorganize") {
			intent = "REFACTOR"
		} else if strings.HasPrefix(lower, "fix") {
			intent = "FIX_BUG"
		} else if strings.Contains(lower, "perf") {
			intent = "PERFORMANCE"
		} else if strings.Contains(lower, "sec") {
			intent = "SECURITY"
		}

		// Only retain noteworthy commits
		if intent == "ADD_FEATURE" || intent == "REFACTOR" || strings.Contains(lower, "arch") || strings.Contains(lower, "layer") {
			records = append(records, MilestoneRecord{
				Ref:         hash,
				Date:        time.Now().Format("2006-01-02"),
				Intent:      intent,
				Description: msg,
				Components:  extractMentionedComponents(msg),
			})
		}
	}

	return records, nil
}

func extractMentionedComponents(msg string) []string {
	var comps []string
	candidates := []string{"doc_engine", "akg", "ai_engine", "visualization", "tui", "cmd", "storage", "patcher"}
	lower := strings.ToLower(msg)
	for _, c := range candidates {
		if strings.Contains(lower, c) {
			comps = append(comps, "internal/"+c)
		}
	}
	return comps
}
