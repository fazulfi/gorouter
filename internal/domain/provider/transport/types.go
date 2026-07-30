// Package transport provides execution and transport abstractions for
// outbound API calls through provider accounts.
package transport

import (
	"context"
	"net/http"

	"gorouter/internal/domain/provider"
)

// ExecContext carries the full context for a single API request execution.
type ExecContext struct {
	Account  *provider.Account
	BaseURL  string
	Endpoint string
	Method   string
	Body     []byte
	Headers  map[string]string
	Stream   bool
	ProxyURL string
}

// RoundTripper executes an outbound HTTP request using the supplied context.
type RoundTripper interface {
	RoundTrip(ctx context.Context, ec *ExecContext) (*http.Response, error)
}
