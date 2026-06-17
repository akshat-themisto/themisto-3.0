package dlp

import "regexp"

// RedactionPattern pairs a compiled regex with metadata for in-place redaction.
type RedactionPattern struct {
	Name string         // e.g. "ssn", "credit_card", "api_key_openai"
	Re   *regexp.Regexp // compiled pattern (shared with Scanner)
	Type string         // "pii" or "credential"
}

// Replacement is the single redaction label used for all pattern types.
const Replacement = "[REDACTED]"

// RedactionPatterns returns the default PII and credential patterns used by the
// DLP scanner, suitable for external callers that need to perform in-place
// string redaction (e.g. cleaning local chat databases).
func RedactionPatterns() []RedactionPattern {
	s := defaultScanner
	out := make([]RedactionPattern, 0, len(s.piiPatterns)+len(s.credentialPatterns))
	for _, p := range s.piiPatterns {
		out = append(out, RedactionPattern{
			Name: p.name,
			Re:   p.re,
			Type: "pii",
		})
	}
	for _, p := range s.credentialPatterns {
		out = append(out, RedactionPattern{
			Name: p.name,
			Re:   p.re,
			Type: "credential",
		})
	}
	return out
}
