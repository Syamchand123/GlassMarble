// Gate 4: Secret & PII scrub on generated output.
// Hard fail — no retry, no Track B fallback. A secret in generated markdown
// must never reach disk. Reuses the same patterns as grounding/sanitizer.go.
package verifier

import (
	"regexp"
)

// outputSecretPatterns are applied to generated markdown before any write.
// These are stricter than the input-side patterns: we fail hard here (Gate 4),
// whereas the grounding sanitizer silently redacts.
var outputSecretPatterns = []*regexp.Regexp{
	// Keyword assignment. The [A-Za-z0-9_\-]* after the keyword lets it match
	// compound identifiers (AWS_SECRET_ACCESS_KEY=, DB_PASSWORD=, secret_key:,
	// access_token=) where extra word characters sit between the sensitive
	// keyword and the "=/:" — not just the keyword immediately followed by it.
	regexp.MustCompile(`(?i)(api[_\-]?key|secret|token|password|passwd|credentials?|bearer|private[_\-]?key)[A-Za-z0-9_\-]*\s*[:=]\s*\S{6,}`),
	// "Authorization: Bearer <token>" / bare "Bearer <token>" — the common
	// HTTP-header shape, which never uses ":"/"=" between "Bearer" and the
	// token itself so the assignment pattern above cannot catch it.
	regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9\-_\.]{10,}`),
	// JWT: three dot-separated base64url segments (header.payload.signature).
	regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{2,}\.[A-Za-z0-9_-]{4,}\.[A-Za-z0-9_-]{4,}\b`),
	regexp.MustCompile(`AKIA[0-9A-Z]{16}`),    // AWS access key ID
	regexp.MustCompile(`ghp_[a-zA-Z0-9]{36}`), // GitHub personal token
	regexp.MustCompile(`ghs_[a-zA-Z0-9]{36}`), // GitHub server token
	regexp.MustCompile(`-----BEGIN (RSA|EC|OPENSSH|PGP|DSA) PRIVATE KEY-----`),
	// DB connection strings with embedded creds — covers the common scheme
	// variants (postgresql alias, mongodb+srv, TLS variants, message queues).
	regexp.MustCompile(`(?i)(postgres(ql)?|mysql|mongodb(\+srv)?|redis|rediss|amqp|amqps|mssql)://[^:@/\s]+:[^@\s]+@`),
	regexp.MustCompile(`(?i)[a-f0-9]{64}\b`), // 256-bit hex digest (likely key), case-insensitive
}

// checkSecrets applies secret patterns to content and returns a Gate 4 error
// if any pattern matches.
//
// This gate always fails hard — callers must NOT retry the LLM or fall back to
// Track B. The LLM prompt itself may have been contaminated; the section is aborted.
func checkSecrets(content string) *GateError {
	for _, re := range outputSecretPatterns {
		if re.MatchString(content) {
			return &GateError{
				Gate:    4,
				Message: "SECURITY: potential secret pattern detected in generated section output — section write aborted",
			}
		}
	}
	return nil
}
