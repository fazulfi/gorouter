package harness

import (
	"regexp"
	"sort"
	"strings"
)

// RedactedMarker replaces every credential-like string in harness
// output.
const RedactedMarker = "[REDACTED]"

// Redactor strips exact secret values and credential-shaped patterns
// (Bearer tokens, API keys, session cookies, PATs) from harness output.
// It is immutable after construction and safe for concurrent use.
type Redactor struct {
	secrets  []string
	patterns []*regexp.Regexp
}

// NewRedactor creates a Redactor for the given exact secret values and
// patterns. A nil patterns slice selects DefaultCredentialPatterns.
func NewRedactor(secrets []string, patterns []*regexp.Regexp) *Redactor {
	if patterns == nil {
		patterns = DefaultCredentialPatterns()
	}
	seen := map[string]bool{}
	sorted := make([]string, 0, len(secrets))
	for _, s := range secrets {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		sorted = append(sorted, s)
	}
	sort.Slice(sorted, func(i, j int) bool { return len(sorted[i]) > len(sorted[j]) })
	compiled := make([]*regexp.Regexp, 0, len(patterns))
	for _, p := range patterns {
		if p != nil {
			compiled = append(compiled, p)
		}
	}
	return &Redactor{secrets: sorted, patterns: compiled}
}

// DefaultCredentialPatterns returns the built-in credential patterns
// matched by the leak-fail gate: Bearer/Basic tokens, OpenAI-style
// keys, GitHub/Slack/GitLab/AWS/Google tokens and PATs, JWTs, session
// cookies, API key headers and query parameters.
func DefaultCredentialPatterns() []*regexp.Regexp {
	return []*regexp.Regexp{
		regexp.MustCompile(`\bBearer\s+[A-Za-z0-9._~+/=-]{8,}`),
		regexp.MustCompile(`\bBasic\s+[A-Za-z0-9+/=]{12,}`),
		regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{12,}`),
		regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{12,}`),
		regexp.MustCompile(`\bglpat-[A-Za-z0-9_-]{8,}`),
		regexp.MustCompile(`\bxox[baprs]-[A-Za-z0-9-]{10,}`),
		regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{20,}`),
		regexp.MustCompile(`\bAKIA[0-9A-Z]{16}`),
		regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}`),
		regexp.MustCompile(`(?i)\b(?:x-api-key|x-goog-api-key|api-key)\s*[:=]\s*[A-Za-z0-9._~+/=-]{8,}`),
		regexp.MustCompile(`(?i)\bsession[=:]\s*[A-Za-z0-9._-]{12,}`),
		regexp.MustCompile(`(?i)\bCookie\s*:\s*[^;\r\n]+`),
		regexp.MustCompile(`[?&](?:api[_-]?key|token|access_token|key)\s*=\s*[^&\s]{8,}`),
	}
}

// Redact replaces every known secret and credential pattern in s.
func (r *Redactor) Redact(s string) string {
	for _, secret := range r.secrets {
		s = strings.ReplaceAll(s, secret, RedactedMarker)
	}
	for _, p := range r.patterns {
		s = p.ReplaceAllString(s, RedactedMarker)
	}
	return s
}

// RedactBytes is Redact for byte slices.
func (r *Redactor) RedactBytes(b []byte) []byte {
	return []byte(r.Redact(string(b)))
}

// DetectCredentialLeaks returns every credential-like substring found
// in text, combining the default patterns with the given exact secret
// values. The leak-fail gate uses it to reject any test run whose
// stdout/stderr/artifacts contain credentials.
func DetectCredentialLeaks(text string, secrets []string) []string {
	var leaks []string
	seen := map[string]bool{}
	report := func(s string) {
		if s == "" || seen[s] {
			return
		}
		seen[s] = true
		leaks = append(leaks, s)
	}
	for _, secret := range secrets {
		if secret != "" && strings.Contains(text, secret) {
			report(secret)
		}
	}
	for _, p := range DefaultCredentialPatterns() {
		for _, m := range p.FindAllString(text, -1) {
			report(m)
		}
	}
	sort.Strings(leaks)
	return leaks
}
