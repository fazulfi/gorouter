// Package specialized provides provider-specific executor implementations for
// non-standard AI provider APIs. Each executor implements engine.Executor and
// handles the unique wire protocol, auth, and request/response semantics of its
// provider.
//
// Executor-to-provider mappings are derived from docs/implementation/provider-matrix.yaml
// (upstream audit commit 79918c7830695bbca4a45c9fea4a42c3e9fd73d1). Rows whose
// executor_path equals "specialized/*" are owned by this package. Rows with
// executor_path "generic" use the generic OpenAI-compatible/anthropic executors.
package specialized

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync/atomic"

	"gorouter/internal/domain/engine"
	"gorouter/internal/shared"
)

// ProviderType identity constants are owned by the domain provider
// package: the generated constants in internal/domain/provider/types_generated.go
// (derived from docs/implementation/provider-matrix.yaml) are the single
// source of truth and are referenced here directly — no identity constant
// is redeclared in this package.

// selectBody returns MappedBody if present, otherwise RawBody.
func selectBody(req *engine.Request) []byte {
	if len(req.MappedBody) > 0 {
		return req.MappedBody
	}
	return req.RawBody
}

// resolveModel returns the model to use, preferring executor override.
func resolveModel(override string, req *engine.Request) string {
	if override != "" {
		return override
	}
	if req != nil && req.Model != "" {
		return req.Model
	}
	return "gpt-4o"
}

// flattenHeaders converts an http.Header map into a plain map[string]string.
func flattenHeaders(h http.Header) map[string]string {
	out := make(map[string]string, len(h))
	for k, v := range h {
		if len(v) > 0 {
			out[k] = v[0]
		}
	}
	return out
}

// checkResponseStatus checks the HTTP response status code and returns an
// appropriate AppError for non-2xx responses. The body is read for diagnostics
// capture but NOT closed here: the caller owns the body lifecycle and must
// close resp.Body exactly once on every path.
func checkResponseStatus(resp *http.Response) error {
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		bodyStr := string(bodyBytes)

		switch resp.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			return newAuthError(resp.StatusCode, bodyStr)
		case http.StatusTooManyRequests:
			return newRateLimitError(resp.StatusCode, bodyStr)
		default:
			return newUpstreamError(resp.StatusCode, bodyStr)
		}
	}
	return nil
}

// newAuthError creates an AppError for 401/403 responses.
func newAuthError(statusCode int, body string) *shared.AppError {
	return &shared.AppError{
		Code:       shared.ErrUnauthorized,
		Message:    "upstream authentication failed",
		HTTPStatus: statusCode,
		Details: map[string]interface{}{
			"status_code": statusCode,
			"body":        truncateString(body, 512),
		},
	}
}

// newRateLimitError creates an AppError for 429 responses.
func newRateLimitError(statusCode int, body string) *shared.AppError {
	return &shared.AppError{
		Code:       shared.ErrRateLimited,
		Message:    "upstream rate limit exceeded",
		HTTPStatus: statusCode,
		Details: map[string]interface{}{
			"status_code": statusCode,
			"body":        truncateString(body, 512),
		},
	}
}

// newUpstreamError creates an AppError for other non-2xx responses.
func newUpstreamError(statusCode int, body string) *shared.AppError {
	return &shared.AppError{
		Code:       shared.ErrInternal,
		Message:    "upstream API returned error",
		HTTPStatus: statusCode,
		Details: map[string]interface{}{
			"status_code": statusCode,
			"body":        truncateString(body, 1024),
		},
	}
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// injectStreamParam reads the JSON body, sets stream=<v>, and returns a new
// request with the modified body. If the body is not valid JSON the request is
// returned unchanged.
func injectStreamParam(req *http.Request, enableStream bool) (*http.Request, error) {
	if req.Body == nil {
		return req, nil
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return req, nil //nolint:nilerr // best-effort
	}
	req.Body.Close()

	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		req.Body = io.NopCloser(bytes.NewReader(body))
		return req, nil
	}

	payload["stream"] = enableStream

	newBody, err := json.Marshal(payload)
	if err != nil {
		return req, nil //nolint:nilerr // best-effort
	}

	req.Body = io.NopCloser(bytes.NewReader(newBody))
	req.ContentLength = int64(len(newBody))
	return req, nil
}

// getEnvOrDefault returns the value of the environment variable named by key,
// or defaults if the variable is not set or empty.
func getEnvOrDefault(key, defaults string) string {
	// In a real implementation, use os.Getenv. For now, return default.
	// The executor layer does not read env directly — configuration flows
	// through bootstrap.
	return defaults
}

// atomicClose wraps an io.Closer so it can be called multiple times safely.
type atomicClose struct {
	closed atomic.Bool
}

func (a *atomicClose) Close(closer io.Closer) {
	if a.closed.Swap(true) {
		return
	}
	closer.Close() //nolint:errcheck
}

// parseResponse unmarshals OpenAI-format response JSON into an engine.Response.
func parseResponse(req *engine.Request, resp *http.Response, body []byte) (*engine.Response, error) {
	var raw struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		Created int64  `json:"created"`
		Model   string `json:"model"`
		Usage   *struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage"`
		Choices []struct {
			Index        int    `json:"index"`
			FinishReason string `json:"finish_reason"`
			Message      *struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parse response JSON: %w", err)
	}

	engResp := &engine.Response{
		RequestID:  req.ID,
		Body:       body,
		Model:      raw.Model,
		StatusCode: resp.StatusCode,
		Headers:    flattenHeaders(resp.Header),
	}

	if raw.Usage != nil {
		engResp.Usage = &engine.Usage{
			PromptTokens:     raw.Usage.PromptTokens,
			CompletionTokens: raw.Usage.CompletionTokens,
			TotalTokens:      raw.Usage.TotalTokens,
		}
	}

	return engResp, nil
}

// errExecutorNotFound is returned when no specialized executor is registered.
var errExecutorNotFound = &shared.AppError{
	Code:    shared.ErrNotFound,
	Message: "no specialized executor found for the given provider type",
}
