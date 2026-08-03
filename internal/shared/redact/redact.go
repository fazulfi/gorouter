// Package redact masks credential-shaped values in operator console log
// lines before they are persisted or surfaced: API key values, personal
// access tokens, session hashes, OAuth tokens, cookies and passwords.
// The console-log projection stores only the redacted output of Redact,
// so raw credentials and secrets never reach the database.
package redact

import "regexp"

// Mask is the replacement marker for every redacted segment.
const Mask = "[REDACTED]"

// labeledValue matches the value of a labeled assignment: a double-quoted
// string (JSON objects and %q-style logging) with escaped-quote support, a
// single-quoted string with escaped-quote support, or an unquoted run of
// non-whitespace characters. The quoted alternatives come first so quoted
// values that contain spaces, JSON punctuation or escaped quotes are
// masked as one segment; the unquoted run includes semicolons, so
// delimiter-joined fragments (password=a;b) cannot leave a tail of the
// secret behind after the delimiter.
const labeledValue = `(?:"(?:[^"\\]|\\.)*"|'(?:[^'\\]|\\.)*'|[^\s]+)`

// redactions is the ordered set of credential-shape matchers. The
// Authorization header with an unquoted Bearer/Basic scheme is matched
// before the labeled set so the raw credential is never left after its
// scheme. Labeled assignments (key=value with a credential-ish name) are
// matched next, before bare prefixed tokens, so a labeled value is masked
// as one segment.
var redactions = []*regexp.Regexp{
	// Authorization headers in canonical unquoted form:
	// authorization: Bearer <jwt> / Basic <base64>. The scheme plus the
	// full credential is masked as one segment so the JWT or base64
	// credential cannot survive after its Bearer/Basic scheme.
	regexp.MustCompile(`(?i)\bauthorization\s*[:=]\s*(?:bearer|basic)\s+[A-Za-z0-9._~+/=-]{8,}`),
	// API key values: labeled assignments and Authorization headers. The
	// optional quote between the label and the separator covers JSON
	// object keys ("api_key_value":"sk-...").
	regexp.MustCompile(`(?i)\b(?:api[_-]?key(?:_value)?|apikey|x-api-key|x-goog-api-key|authorization)\s*["']?\s*[:=]\s*` + labeledValue),
	// Bare provider-style keys (sk-proj-..., sk-ant-..., sk-...). The value
	// class is mixed-case because real provider keys are base64url.
	regexp.MustCompile(`\bsk-(?:proj|ant|test|live)?-?[A-Za-z0-9._-]{6,}`),
	// Personal access tokens: GitHub (ghp_/gho_/ghu_/ghs_/github_pat_),
	// GitLab (glpat-/glptt-) and generic pat_ prefixed tokens.
	regexp.MustCompile(`\b(?:gh[opustu]_|github_pat_|glp[a-z][a-z]-|pat_)[A-Za-z0-9_-]{6,}`),
	// Session hashes and session identifiers: labeled assignments and
	// sess_ prefixed opaque values (mixed-case).
	regexp.MustCompile(`(?i)\b(?:session[_-]?hash|session[_-]?id|sessionid|sid)\s*["']?\s*[:=]\s*` + labeledValue),
	regexp.MustCompile(`\bsess_[A-Za-z0-9]{8,}`),
	// OAuth tokens: labeled assignments, Bearer headers and Google
	// ya29.-style tokens (mixed-case).
	regexp.MustCompile(`(?i)\b(?:access[_-]?token|refresh[_-]?token|oauth[_-]?token|id[_-]?token|token)\s*["']?\s*[:=]\s*` + labeledValue),
	regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9._~+/=-]{8,}`),
	regexp.MustCompile(`\bya29\.[A-Za-z0-9._-]+`),
	// Cookies: Cookie/Set-Cookie headers and labeled assignments.
	regexp.MustCompile(`(?i)\b(?:cookie|set-cookie|cookie[_-]?header)\s*["']?\s*[:=]\s*` + labeledValue),
	// Passwords: labeled assignments.
	regexp.MustCompile(`(?i)\b(?:password|passwd|pwd)\s*["']?\s*[:=]\s*` + labeledValue),
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
