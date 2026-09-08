// Package grounding implements AKG query dispatching, living diagram generation,
// self-healing code permalinks, and enterprise secret/PII scrubbing.
package grounding

import (
	"math"
	"regexp"
	"strings"

	"github.com/Syamchand123/GlassMarble/internal/doc_engine/config"
)

// SecretPattern defines a regex pattern and its redaction tag.
type SecretPattern struct {
	Name    string
	Regex   *regexp.Regexp
	Replace string
}

var defaultSecretPatterns = []SecretPattern{
	{
		Name:    "api_key_assignment",
		Regex:   regexp.MustCompile(`(?i)(api[_-]?key|client[_-]?secret|auth[_-]?token|bearer|password|passwd|private[_-]?key)\s*[:=]\s*["']?([A-Za-z0-9_\-\.\/\+]{8,})["']?`),
		Replace: "$1=[REDACTED:$NAME]",
	},
	{
		Name:    "aws_access_key",
		Regex:   regexp.MustCompile(`\b(AKIA|ABIA|ACCA|ASIA)[0-9A-Z]{16}\b`),
		Replace: "[REDACTED:aws_access_key]",
	},
	{
		Name:    "github_token",
		Regex:   regexp.MustCompile(`\b(ghp_[A-Za-z0-9]{36}|gho_[A-Za-z0-9]{36}|ghu_[A-Za-z0-9]{36}|ghs_[A-Za-z0-9]{36}|github_pat_[A-Za-z0-9_]{82})\b`),
		Replace: "[REDACTED:github_token]",
	},
	{
		Name:    "slack_token",
		Regex:   regexp.MustCompile(`\bxox[baprs]-[0-9]{10,13}-[0-9]{10,13}[a-zA-Z0-9-]*\b`),
		Replace: "[REDACTED:slack_token]",
	},
	{
		Name:    "private_key_pem",
		Regex:   regexp.MustCompile(`-----BEGIN (?:RSA|EC|OPENSSH|PGP|DSA)? ?PRIVATE KEY-----[^-]+-----END (?:RSA|EC|OPENSSH|PGP|DSA)? ?PRIVATE KEY-----`),
		Replace: "[REDACTED:private_key]",
	},
	{
		Name:    "connection_string_credentials",
		Regex:   regexp.MustCompile(`(?i)(postgres|mysql|mongodb|redis|amqp)://([^:]+):([^@]+)@`),
		Replace: "$1://$2:[REDACTED]@",
	},
	{
		Name:    "jwt_token",
		Regex:   regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{10,}\.eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\b`),
		Replace: "[REDACTED:jwt_token]",
	},
}

// ShannonEntropy calculates the Shannon entropy of a string (bits per symbol).
func ShannonEntropy(s string) float64 {
	if len(s) == 0 {
		return 0
	}
	freq := make(map[rune]float64)
	for _, r := range s {
		freq[r]++
	}
	total := float64(len(s))
	var entropy float64
	for _, count := range freq {
		p := count / total
		entropy -= p * math.Log2(p)
	}
	return entropy
}

// IsHighEntropySecret reports whether token appears to be an encrypted or
// random secret token based on length, character set, and Shannon entropy.
func IsHighEntropySecret(token string) bool {
	// Avoid false positives on common code constructs
	token = strings.Trim(token, "\"'` ,;()[]{}")
	if len(token) < 32 {
		return false
	}
	// UUIDs with dashes are structured identifiers, not raw secret tokens
	if len(token) == 36 && strings.Count(token, "-") == 4 {
		return false
	}
	// Check if token consists mostly of base64 or hex characters
	validChars := true
	for _, r := range token {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == '+' || r == '/' || r == '=') {
			validChars = false
			break
		}
	}
	if !validChars {
		return false
	}

	entropy := ShannonEntropy(token)

	// If purely hex (0-9, a-f, A-F), maximum possible entropy is log2(16) = 4.0.
	// Random hex tokens (SHA/API keys) have entropy typically > 3.2.
	isHex := true
	for _, r := range token {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
			isHex = false
			break
		}
	}
	if isHex {
		return entropy >= 3.2
	}

	// For base64 / alphanumeric tokens (max entropy log2(64) = 6.0)
	if len(token) >= 40 && entropy >= 4.0 {
		return true
	}
	if len(token) >= 32 && entropy >= 4.3 {
		return true
	}
	return false
}

// SanitizeText scrubs all matching credentials, tokens, private keys, and
// high-entropy secret strings from text, replacing them with redaction tags.
func SanitizeText(text string) string {
	if text == "" {
		return ""
	}

	result := text

	// 1. Apply regex pattern replacements
	for _, p := range defaultSecretPatterns {
		rep := strings.ReplaceAll(p.Replace, "$NAME", p.Name)
		result = p.Regex.ReplaceAllString(result, rep)
	}

	// 2. Scan individual words for high-entropy secrets
	words := strings.Fields(result)
	for _, w := range words {
		cleanWord := strings.Trim(w, "\"'` ,;()[]{}")
		if IsHighEntropySecret(cleanWord) {
			result = strings.ReplaceAll(result, cleanWord, "[REDACTED:high_entropy_secret]")
		}
	}

	return result
}

// ContainsSecret reports whether the text contains any detectable secrets or PII.
func ContainsSecret(text string) bool {
	if text == "" {
		return false
	}
	for _, p := range defaultSecretPatterns {
		if p.Name == "connection_string_credentials" {
			matches := p.Regex.FindAllStringSubmatch(text, -1)
			for _, m := range matches {
				if len(m) >= 4 && !strings.HasPrefix(m[3], "[REDACTED") {
					return true
				}
			}
			continue
		}
		if p.Name == "api_key_assignment" {
			matches := p.Regex.FindAllStringSubmatch(text, -1)
			for _, m := range matches {
				if len(m) >= 3 && !strings.HasPrefix(m[2], "[REDACTED") {
					return true
				}
			}
			continue
		}
		if p.Regex.MatchString(text) {
			return true
		}
	}
	words := strings.Fields(text)
	for _, w := range words {
		cleanWord := strings.Trim(w, "\"'` ,;()[]{}")
		if strings.HasPrefix(cleanWord, "[REDACTED") {
			continue
		}
		if IsHighEntropySecret(cleanWord) {
			return true
		}
	}
	return false
}

// SanitizeFactSheet scrubs secrets across all fields in a FactSheet.
func SanitizeFactSheet(sheet *config.FactSheet) *config.FactSheet {
	if sheet == nil {
		return nil
	}

	// Symbols
	for i := range sheet.GroundTruth.Symbols {
		sheet.GroundTruth.Symbols[i].Signature = SanitizeText(sheet.GroundTruth.Symbols[i].Signature)
		sheet.GroundTruth.Symbols[i].Doc = SanitizeText(sheet.GroundTruth.Symbols[i].Doc)
	}
	for i := range sheet.GroundTruth.AddedSymbols {
		sheet.GroundTruth.AddedSymbols[i].Signature = SanitizeText(sheet.GroundTruth.AddedSymbols[i].Signature)
		sheet.GroundTruth.AddedSymbols[i].Doc = SanitizeText(sheet.GroundTruth.AddedSymbols[i].Doc)
	}
	for i := range sheet.GroundTruth.ModifiedSymbols {
		sheet.GroundTruth.ModifiedSymbols[i].Before = SanitizeText(sheet.GroundTruth.ModifiedSymbols[i].Before)
		sheet.GroundTruth.ModifiedSymbols[i].After = SanitizeText(sheet.GroundTruth.ModifiedSymbols[i].After)
		sheet.GroundTruth.ModifiedSymbols[i].DocBefore = SanitizeText(sheet.GroundTruth.ModifiedSymbols[i].DocBefore)
		sheet.GroundTruth.ModifiedSymbols[i].DocAfter = SanitizeText(sheet.GroundTruth.ModifiedSymbols[i].DocAfter)
	}

	// Config vars
	for i := range sheet.GroundTruth.ConfigVars {
		sheet.GroundTruth.ConfigVars[i].Default = SanitizeText(sheet.GroundTruth.ConfigVars[i].Default)
		sheet.GroundTruth.ConfigVars[i].Doc = SanitizeText(sheet.GroundTruth.ConfigVars[i].Doc)
	}

	// Sentinels
	for i := range sheet.GroundTruth.Sentinels {
		sheet.GroundTruth.Sentinels[i].Doc = SanitizeText(sheet.GroundTruth.Sentinels[i].Doc)
	}

	// Diagrams
	for i := range sheet.GroundTruth.Diagrams {
		sheet.GroundTruth.Diagrams[i].Content = SanitizeText(sheet.GroundTruth.Diagrams[i].Content)
	}

	// Prior markdown
	sheet.PriorSectionMarkdown = SanitizeText(sheet.PriorSectionMarkdown)

	return sheet
}
