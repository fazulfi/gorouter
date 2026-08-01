package formats

import (
	"context"
	"encoding/json"
	"strings"

	"gorouter/internal/domain/engine"
)

// Detector identifies the wire format of an incoming request by examining
// the endpoint path first, then falling back to body shape analysis.
type Detector struct{}

// NewDetector creates a new Detector.
func NewDetector() *Detector {
	return &Detector{}
}

// endpointRules maps recognised URL path prefixes to their format constant.
// Order matters: more specific paths must appear before less specific ones.
var endpointRules = []struct {
	prefix string
	format engine.RequestFormat
}{
	{"/v1/chat/completions", engine.FormatOpenAIChat},
	{"/v1/responses", engine.FormatCodexResponses},
	{"/v1/messages", engine.FormatAnthropic},
	{"/v1beta/models", engine.FormatGemini},
	{"/v1/models", engine.FormatGemini},
	{"/v1/completions", engine.FormatOpenAICompat},
	{"/v1/embeddings", engine.FormatOpenAICompat},
	{"/v1/images", engine.FormatOpenAICompat},
	{"/v1/audio", engine.FormatOpenAICompat},
	{"/v1/moderations", engine.FormatOpenAICompat},
}

// Detect examines the endpoint path and request body to determine the wire
// protocol format. Endpoint matching takes precedence; body-shape analysis
// is used as a fallback when the endpoint is unrecognised.
//
// The endpoint parameter is the URL path portion of the request URI (e.g.
// "/v1/chat/completions"). The body parameter is the raw JSON request body,
// which may be nil or empty for non-JSON requests.
func (d *Detector) Detect(ctx context.Context, endpoint string, body json.RawMessage) (engine.RequestFormat, error) {
	if endpoint != "" {
		if f := matchEndpoint(endpoint); f != "" {
			return f, nil
		}
	}

	if len(body) > 0 {
		if f := matchBody(body); f != "" {
			return f, nil
		}
	}

	return "", ErrUnknownFormat
}

// matchEndpoint checks the endpoint path against known prefixes and returns
// the matching format constant, or empty string if no match is found.
func matchEndpoint(endpoint string) engine.RequestFormat {
	ep := strings.Split(endpoint, "?")[0]
	for _, rule := range endpointRules {
		if strings.HasPrefix(ep, rule.prefix) {
			// For Gemini, ensure the path continues past the prefix.
			if rule.format == engine.FormatGemini {
				rest := strings.TrimPrefix(ep, rule.prefix)
				if rest == "" || rest == "/" {
					continue
				}
			}
			return rule.format
		}
	}
	return ""
}

// matchBody analyses the JSON body shape to determine the wire format.
// This is the fallback path used when the endpoint is not recognised.
func matchBody(body json.RawMessage) engine.RequestFormat {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return ""
	}

	_, hasMessages := raw["messages"]
	_, hasInput := raw["input"]
	_, hasModel := raw["model"]
	_, hasContents := raw["contents"]
	_, hasAnthropicVersion := raw["anthropic_version"]

	// Format-specific discriminators take priority over generic shape analysis.
	if hasAnthropicVersion {
		return engine.FormatAnthropic
	}

	if hasContents {
		return engine.FormatGemini
	}

	if hasMessages {
		if hasModel {
			return engine.FormatOpenAIChat
		}
		// Body has "messages" but no "model". This is ambiguous: it could be
		// Anthropic (model in header/URL) or OpenAI-compatible (model in URL
		// params). Check for Anthropic-specific discriminators.
		_, hasSystem := raw["system"]
		if hasAnthropicVersion || hasSystem {
			return engine.FormatAnthropic
		}
		return engine.FormatOpenAICompat
	}

	if hasInput {
		return engine.FormatCodexResponses
	}

	if hasModel {
		return engine.FormatOpenAIChat
	}

	return ""
}
