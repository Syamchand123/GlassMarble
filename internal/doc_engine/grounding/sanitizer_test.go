package grounding

import (
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/doc_engine/config"
	"github.com/stretchr/testify/assert"
)

func TestSanitizeText_Patterns(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		contains string
		redacted string
	}{
		{
			name:     "AWS Access Key",
			input:    "Deploy using AWS key AKIAIOSFODNN7EXAMPLE today",
			redacted: "Deploy using AWS key [REDACTED:aws_access_key] today",
		},
		{
			name:     "GitHub Token",
			input:    "ghp_1234567890abcdefghijklmnopqrstuv1234",
			redacted: "[REDACTED:github_token]",
		},
		{
			name:     "API Key Assignment",
			input:    `apiKey = "secret_12345678abcdef"`,
			contains: "[REDACTED:api_key_assignment]",
		},
		{
			name:     "Connection String Credentials",
			input:    "postgres://admin:superSecretPassword123@localhost:5432/mydb",
			redacted: "postgres://admin:[REDACTED]@localhost:5432/mydb",
		},
		{
			name:     "Private Key Block",
			input:    "-----BEGIN RSA PRIVATE KEY-----\nMIIEowIBAAKCAQEA...\n-----END RSA PRIVATE KEY-----",
			redacted: "[REDACTED:private_key]",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			output := SanitizeText(tc.input)
			if tc.redacted != "" {
				assert.Equal(t, tc.redacted, output)
			}
			if tc.contains != "" {
				assert.Contains(t, output, tc.contains)
			}
			assert.False(t, ContainsSecret(output), "output must not contain un-redacted secrets")
		})
	}
}

func TestShannonEntropy_And_HighEntropySecrets(t *testing.T) {
	// Low entropy English text or code identifier
	normalIdent := "ValidateUserWithPasswordHashAndEmail"
	assert.False(t, IsHighEntropySecret(normalIdent), "normal code identifiers must not trigger false positives")

	// High entropy base64-like secret token (length 44)
	highEntropyToken := "4a8f9c2d1e0b3a7f8e9d0c1b2a3f4e5d6c7b8a9f0e1d"
	assert.True(t, IsHighEntropySecret(highEntropyToken))

	text := "Found secret token: " + highEntropyToken + " in memory"
	sanitized := SanitizeText(text)
	assert.Contains(t, sanitized, "[REDACTED:high_entropy_secret]")
}

func TestSanitizeFactSheet(t *testing.T) {
	sheet := &config.FactSheet{
		GroundTruth: config.GroundTruthPayload{
			Symbols: []config.SymbolFact{
				{
					FQN:       "internal/auth.Connect",
					Signature: "func Connect(apiKey string)",
					Doc:       "Connects with AKIAIOSFODNN7EXAMPLE",
				},
			},
			ConfigVars: []config.ConfigVarFact{
				{Name: "DB_PASS", Default: "postgres://user:superSecret@db:5432"},
			},
		},
		PriorSectionMarkdown: "Old text with ghp_1234567890abcdefghijklmnopqrstuv1234 token",
	}

	sanitized := SanitizeFactSheet(sheet)
	assert.Contains(t, sanitized.GroundTruth.Symbols[0].Doc, "[REDACTED:aws_access_key]")
	assert.Contains(t, sanitized.GroundTruth.ConfigVars[0].Default, "[REDACTED]")
	assert.Contains(t, sanitized.PriorSectionMarkdown, "[REDACTED:github_token]")
}
