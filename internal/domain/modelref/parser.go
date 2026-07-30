package modelref

import "strings"

// parseProvider extracts the provider prefix from a raw model reference
// string. The provider ends at the first colon (":") or slash ("/") delimiter.
// When no delimiter is found both return values are empty.
func parseProvider(raw string) (provider string, remainder string) {
	if idx := strings.Index(raw, ":"); idx != -1 {
		return raw[:idx], raw[idx+1:]
	}
	if idx := strings.Index(raw, "/"); idx != -1 {
		return raw[:idx], raw[idx+1:]
	}
	return "", raw
}

// parseAccount checks whether segment is a likely account selector rather than
// a model name. Account selectors are short (1-32 chars) alphanumeric strings.
//
// If the segment looks like an account selector it is returned as *string and
// the second return value is empty. Otherwise nil is returned and the segment
// is returned verbatim as the "model" part.
func parseAccount(segment string) (*string, string) {
	if len(segment) == 0 || len(segment) > 32 {
		return nil, segment
	}
	for _, r := range segment {
		if !isAlphaNum(r) {
			return nil, segment
		}
	}
	return &segment, ""
}

// extractModelCapability splits a model segment on the last colon (":") to
// separate the model name from an optional capability suffix.
//
// Examples:
//
//	"gpt-4"           → model="gpt-4",          capability=nil
//	"gpt-4:instruct"  → model="gpt-4",          capability=*"instruct"
//	"a:b:c"           → model="a:b",            capability=*"c"
func extractModelCapability(segment string) (model string, capability *string) {
	if idx := strings.LastIndex(segment, ":"); idx != -1 {
		cap := segment[idx+1:]
		return segment[:idx], &cap
	}
	return segment, nil
}

// isAlphaNum reports whether r is an ASCII letter or digit.
func isAlphaNum(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
}
