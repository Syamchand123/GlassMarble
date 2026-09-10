// Plan A5 — Go native fuzzing for the grounding sanitizer.
//
// Scope: test-only. No production source changes.
//
// Invariants checked on every fuzz input (arbitrary bytes):
//  1. Never panics.
//  2. Idempotence: SanitizeText(SanitizeText(x)) == SanitizeText(x).
//  3. Sanitized output is clean per the existing contract:
//     ContainsSecret(SanitizeText(x)) == false.
//  4. A fixed adversarial list (AWS keys, GitHub/Slack tokens, PEM blocks,
//     Bearer assignments, JWTs, connection strings, high-entropy hex) is
//     redacted per the existing contract, while UUIDs and normal prose/code
//     are left untouched.
package grounding

import (
	"strings"
	"testing"
)

// sanitizerAdversarialCases pins the existing redaction contract: each raw
// sample must disappear from the output and its redaction tag must appear.
var sanitizerAdversarialCases = []struct {
	name        string
	raw         string
	mustContain string
}{
	{"aws_akia", "AKIAIOSFODNN7EXAMPLE", "[REDACTED:aws_access_key]"},
	{"aws_asia", "ASIAIOSFODNN7EXAMPLE", "[REDACTED:aws_access_key]"},
	{"github_ghp", "ghp_1234567890abcdefghijklmnopqrstuv1234", "[REDACTED:github_token]"},
	{"github_gho", "gho_1234567890abcdefghijklmnopqrstuv1234", "[REDACTED:github_token]"},
	{"slack_xoxb", "xoxb-123456789012-123456789012-AbCdEfGhIjKlMnOpQrStUv", "[REDACTED:slack_token]"},
	{"pem_rsa", "-----BEGIN RSA PRIVATE KEY-----\nMIIEowIBAAKCAQEA7bXq\n-----END RSA PRIVATE KEY-----", "[REDACTED:private_key]"},
	{"bearer_assign", `bearer = "s3cr3t_T0ken-abc123XYZ"`, "[REDACTED:api_key_assignment]"},
	{"bearer_colon", "Bearer: s3cr3t_T0ken-abc123XYZ", "[REDACTED:api_key_assignment]"},
	{"jwt", "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c", "[REDACTED:jwt_token]"},
	{"conn_postgres", "postgres://admin:superSecretPassword123@localhost:5432/mydb", "postgres://admin:[REDACTED]@"},
	{"conn_mongodb", "mongodb://root:hunter2hunter2@db:27017/shop?retryWrites=true", "mongodb://root:[REDACTED]@"},
	{"hex_high_entropy", "4a8f9c2d1e0b3a7f8e9d0c1b2a3f4e5d6c7b8a9f0e1d", "[REDACTED:high_entropy_secret]"},
	{"api_key_assignment", `apiKey = "secret_12345678abcdef"`, "[REDACTED:api_key_assignment]"},
	{"password_assignment", "password: hunter2hunter", "[REDACTED:api_key_assignment]"},
}

// sanitizerBenignSamples must pass through byte-identical: the UUID case pins
// the IsHighEntropySecret dash-exclusion, the prose/code cases pin the
// no-false-positive contract.
var sanitizerBenignSamples = []string{
	"The quick brown fox jumps over the lazy dog.",
	"func Connect(apiKey string) {}",
	"id 550e8400-e29b-41d4-a716-446655440000 end",
	"ValidateUserWithPasswordHashAndEmail",
	"nothing secret here: just prose, numbers 12345, and code `fmt.Println`.",
	"Release 1.2.0 fixed login; see docs for details.",
}

// sanitizerFuzzSeeds is the seed corpus: adversarial samples, benign
// must-not-flag samples, and binary/unicode edge cases.
var sanitizerFuzzSeeds = []string{
	"",
	"The quick brown fox jumps over the lazy dog.",
	"func Connect(apiKey string) {}",
	"Release 1.2.0 fixed login; see docs for details.",
	"id 550e8400-e29b-41d4-a716-446655440000 end",
	"ValidateUserWithPasswordHashAndEmail",
	"Deploy using AWS key AKIAIOSFODNN7EXAMPLE today",
	"temp credentials ASIAIOSFODNN7EXAMPLE rotate soon",
	"token ghp_1234567890abcdefghijklmnopqrstuv1234 done",
	"token gho_1234567890abcdefghijklmnopqrstuv1234 done",
	"alert xoxb-123456789012-123456789012-AbCdEfGhIjKlMnOpQrStUv end",
	"-----BEGIN RSA PRIVATE KEY-----\nMIIEowIBAAKCAQEA7bXq\n-----END RSA PRIVATE KEY-----",
	`bearer = "s3cr3t_T0ken-abc123XYZ"`,
	"token eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c end",
	"postgres://admin:superSecretPassword123@localhost:5432/mydb",
	"mongodb://root:hunter2hunter2@db:27017/shop?retryWrites=true",
	"Found secret token: 4a8f9c2d1e0b3a7f8e9d0c1b2a3f4e5d6c7b8a9f0e1d in memory",
	`apiKey = "secret_12345678abcdef"`,
	"password: hunter2hunter",
	"hello \x00\xff\xfe world",
	"zero-width\u200btest and emoji \U0001F600 caf\u00e9 na\u00efve",
}

func FuzzSanitize(f *testing.F) {
	for _, s := range sanitizerFuzzSeeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, input string) {
		// 1+2. Arbitrary bytes must never panic, and sanitizing is idempotent.
		once := SanitizeText(input)
		twice := SanitizeText(once)
		if once != twice {
			t.Fatalf("sanitizer not idempotent:\ninput: %q\nonce: %q\ntwice: %q", input, once, twice)
		}
		// 3. Sanitized output must be clean per the existing contract.
		if ContainsSecret(once) {
			t.Fatalf("sanitized output still contains a secret:\ninput: %q\noutput: %q", input, once)
		}
		// 4a. Fixed adversarial list is redacted per the existing contract.
		// Word-boundary-wrapped so the check is independent of fuzzer bytes
		// (raw patterns such as AWS keys require \b anchors).
		for _, adv := range sanitizerAdversarialCases {
			wrapped := "see " + adv.raw + " here"
			got := SanitizeText(wrapped)
			if strings.Contains(got, adv.raw) {
				t.Fatalf("%s: raw secret survived sanitization: %q", adv.name, got)
			}
			if !strings.Contains(got, adv.mustContain) {
				t.Fatalf("%s: expected redaction %q in %q", adv.name, adv.mustContain, got)
			}
			if ContainsSecret(got) {
				t.Fatalf("%s: sanitized output still flagged: %q", adv.name, got)
			}
		}
		// 4b. Benign samples are never flagged.
		for _, benign := range sanitizerBenignSamples {
			got := SanitizeText(benign)
			if got != benign {
				t.Fatalf("benign sample altered:\ninput:  %q\noutput: %q", benign, got)
			}
			if ContainsSecret(got) {
				t.Fatalf("benign sample flagged as secret: %q", benign)
			}
		}
	})
}

// TestSanitizeUUIDNotFlagged pins the existing exclusion: dashed 36-char
// UUIDs are structured identifiers, not high-entropy secrets.
func TestSanitizeUUIDNotFlagged(t *testing.T) {
	uuid := "550e8400-e29b-41d4-a716-446655440000"
	if IsHighEntropySecret(uuid) {
		t.Fatalf("UUID %q must not be classified as a high-entropy secret", uuid)
	}
	in := "request id " + uuid + " completed"
	if got := SanitizeText(in); got != in {
		t.Fatalf("UUID prose altered:\ninput:  %q\noutput: %q", in, got)
	}
	if ContainsSecret(in) {
		t.Fatalf("UUID prose %q must not contain a secret", in)
	}
}

// TestSanitizeBearerRedacted pins the Bearer contract: the
// api_key_assignment pattern requires a `:`/`=` delimiter after the key
// name, so `bearer = "..."` / `Bearer: ...` redact while a bare
// `Bearer <short-token>` (no delimiter) passes through unchanged.
func TestSanitizeBearerRedacted(t *testing.T) {
	for _, in := range []string{
		`bearer = "s3cr3t_T0ken-abc123XYZ"`,
		"Bearer: s3cr3t_T0ken-abc123XYZ",
	} {
		got := SanitizeText(in)
		if !strings.Contains(got, "[REDACTED:api_key_assignment]") {
			t.Fatalf("bearer assignment not redacted:\ninput:  %q\noutput: %q", in, got)
		}
		if ContainsSecret(got) {
			t.Fatalf("redacted bearer output still flagged: %q", got)
		}
	}
	bare := "Bearer s3cr3t_T0ken-abc123XYZ"
	if got := SanitizeText(bare); got != bare {
		t.Fatalf("bare short bearer token should pass through per existing contract:\ninput:  %q\noutput: %q", bare, got)
	}
}
