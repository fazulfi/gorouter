// Package redact masks credential-shaped values in operator console log
// lines before they are persisted or surfaced: API key values, personal
// access tokens, session hashes, OAuth tokens, cookies and passwords.
// The console-log projection stores only the redacted output of Redact,
// so raw credentials and secrets never reach the database.
package redact

import "regexp"

// Mask is the replacement marker for every redacted segment.
const Mask = "[REDACTED]"

// redactions is the ordered set of credential-shape matchers. Labeled
// assignments (key=value with a credential-ish name) are matched before
// bare prefixed tokens so a labeled value is masked as one segment.
var redactions = []*regexp.Regexp{
	// API key values: labeled assignments and Authorization headers.
	regexp.MustCompile(`(?i)\b(?:api[_-]?key(?:_value)?|apikey|x-api-key|x-goog-api-key|authorization)\s*[:=]\s*[^\s,;"']+`),
	// Bare provider-style keys (sk-proj-..., sk-ant-..., sk-...).
	regexp.MustCompile(`\bsk-(?:proj|ant|test|live)?-?[a-z0-9._-]{6,}`),
	// Personal access tokens: GitHub (ghp_/gho_/ghu_/ghs_/github_pat_),
	// GitLab (glpat-/glptt-) and generic pat_ prefixed tokens.
	regexp.MustCompile(`\b(?:gh[opustu]_|github_pat_|glp[a-z][a-z]-|pat_)[A-Za-z0-9_-]{6,}`),
	// Session hashes and session identifiers: labeled assignments and
	// sess_ prefixed opaque values.
	regexp.MustCompile(`(?i)\b(?:session[_-]?hash|session[_-]?id|sessionid|sid)\s*[:=]\s*[^\s,;"']+`),
	regexp.MustCompile(`\bsess_[a-z0-9]{8,}`),
	// OAuth tokens: labeled assignments, Bearer headers and Google
	// ya29.-style tokens.
	regexp.MustCompile(`(?i)\b(?:access[_-]?token|refresh[_-]?token|oauth[_-]?token|id[_-]?token|token)\s*[:=]\s*[^\s,;"']+`),
	regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9._~+/=-]{8,}`),
	regexp.MustCompile(`\bya29\.[a-z0-9._-]+`),
	// Cookies: Cookie/Set-Cookie headers and labeled assignments.
	regexp.MustCompile(`(?i)\b(?:cookie|set-cookie|cookie[_-]?header)\s*[:=]\s*[^\s,;"']+`),
	// Passwords: labeled assignments.
	regexp.MustCompile(`(?i)\b(?:password|passwd|pwd)\s*[:=]\s*[^\s,;"']+`),
}

// Redact returns a copy of line with every credential-shaped segment
// replaced by Mask. Lines without credential shapes are returned
// unchanged.
func Redact(line string) string {
	if line == "" {
		return line
	}
	for _, re := range redactions {
		line = re.ReplaceAllString(line, Mask)
	}
	return line
}
