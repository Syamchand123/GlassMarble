package knowledge_fusion

import (
	"crypto/sha256"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Syamchand123/GlassMarble/internal/developer_memory"
	"github.com/Syamchand123/GlassMarble/internal/evidence"
)

// ParseADR parses an Architecture Decision Record markdown file.
func ParseADR(filePath string) (*developer_memory.KnowledgeClaim, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	content := string(data)
	lines := strings.Split(content, "\n")

	var title, status, context, decision string
	var currentSection string

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "# ") {
			title = strings.TrimPrefix(line, "# ")
			currentSection = ""
		} else if strings.HasPrefix(line, "## Status") {
			currentSection = "Status"
		} else if strings.HasPrefix(line, "## Context") {
			currentSection = "Context"
		} else if strings.HasPrefix(line, "## Decision") {
			currentSection = "Decision"
		} else if strings.HasPrefix(line, "## ") {
			currentSection = ""
		} else {
			if line == "" {
				continue
			}
			switch currentSection {
			case "Status":
				status += line + " "
			case "Context":
				context += line + " "
			case "Decision":
				decision += line + " "
			}
		}
	}

	title = strings.TrimSpace(title)
	status = strings.TrimSpace(status)
	context = strings.TrimSpace(context)
	decision = strings.TrimSpace(decision)

	if title == "" || decision == "" {
		return nil, fmt.Errorf("invalid ADR: missing title or decision")
	}

	claim := &developer_memory.KnowledgeClaim{
		Subject:   title,
		Predicate: "decided_to",
		Object:    decision,
		State:     developer_memory.StateActive,
		ValidFrom: time.Now(),
	}

	if strings.Contains(strings.ToLower(status), "deprecated") || strings.Contains(strings.ToLower(status), "superseded") {
		claim.State = developer_memory.StateDeprecated
	}

	b := evidence.Bundle{}
	b.Add(evidence.EvidenceItem{
		Source:     evidence.SourceDocs,
		Reference:  filePath,
		Excerpt:    fmt.Sprintf("Context: %s | Decision: %s", context, decision),
		Confidence: 0.95,
		Timestamp:  time.Now(),
	})
	claim.Evidence = b

	h := sha256.Sum256([]byte(claim.Subject + claim.Predicate + claim.Object))
	claim.ID = fmt.Sprintf("%x", h[:8])

	return claim, nil
}

// ParseReadme extracts architectural mentions from README.
func ParseReadme(filePath string) []developer_memory.KnowledgeClaim {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil
	}

	content := string(data)
	lines := strings.Split(content, "\n")

	var claims []developer_memory.KnowledgeClaim
	techKeywords := []string{"Redis", "PostgreSQL", "RabbitMQ", "Kafka", "MySQL", "MongoDB"}

	for i, line := range lines {
		for _, tech := range techKeywords {
			if strings.Contains(line, tech) {
				claim := developer_memory.KnowledgeClaim{
					Subject:   "Architecture",
					Predicate: "uses_technology",
					Object:    tech,
					State:     developer_memory.StateActive,
					ValidFrom: time.Now(),
				}

				b := evidence.Bundle{}
				excerpt := line
				if i > 0 {
					excerpt = lines[i-1] + " " + excerpt
				}
				if i < len(lines)-1 {
					excerpt = excerpt + " " + lines[i+1]
				}

				b.Add(evidence.EvidenceItem{
					Source:     evidence.SourceDocs,
					Reference:  filePath,
					Excerpt:    strings.TrimSpace(excerpt),
					Confidence: 0.70,
					Timestamp:  time.Now(),
				})
				claim.Evidence = b

				h := sha256.Sum256([]byte(claim.Subject + claim.Predicate + claim.Object))
				claim.ID = fmt.Sprintf("%x", h[:8])

				claims = append(claims, claim)
			}
		}
	}

	return claims
}
