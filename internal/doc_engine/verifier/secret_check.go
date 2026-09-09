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
	regexp.MustCompile(`(?i)(api[_\-]?key|secret|token|password|bearer|private[_\-]?key)\s*[:=]\s*\S{6,}`),
	regexp.MustCompile(`AKIA[0-9A-Z]{16}`),                          // AWS access key
	regexp.MustCompile(`ghp_[a-zA-Z0-9]{36}`),                       // GitHub personal token
	regexp.MustCompile(`ghs_[a-zA-Z0-9]{36}`),                       // GitHub server token
	regexp.MustCompile(`-----BEGIN (RSA|EC|OPENSSH) PRIVATE KEY-----`),
	regexp.MustCompile(`(?i)postgres(ql)?://[^@]+:[^@]+@`),          // DB connection string with creds
	regexp.MustCompile(`(?i)mongodb://[^@]+:[^@]+@`),
	regexp.MustCompile(`[a-f0-9]{64}`),                              // 256-bit hex digest (likely key)
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
