// Package generic provides shared HTTP client infrastructure and model-discovery
// helpers for dashboard-defined compatible providers (OpenAI-compatible,
// Anthropic-compatible, and any provider that exposes a REST API).
//
// Custom provider URLs are intentionally unrestricted per decisions #136, #137,
// and #376 — no SSRF, loopback, LAN, private-range, or DNS-rebinding validation
// is applied.
package generic

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"gorouter/internal/domain/provider"
)

// CredentialResolver resolves a provider.Account into a bearer token or API key
// suitable for injection into outbound requests. This abstraction exists because
// Account.CredentialRef may be a reference (e.g. a secret-store key) rather than
// the plaintext credential itself.
//
// Implementations MUST redact the returned value from all error messages and
// log output. The default resolver treats CredentialRef as an opaque bearer
// token, which matches Phase 2 behaviour.
type CredentialResolver interface {
	// Resolve returns the plaintext credential for the given account.
	// Implementations must not return the credential in error strings.
	Resolve(account *provider.Account) (string, error)
}

// CredentialResolverFunc is an adapter that allows ordinary functions to serve
// as CredentialResolver.
type CredentialResolverFunc func(account *provider.Account) (string, error)

// Resolve calls the underlying function.
func (f CredentialResolverFunc) Resolve(account *provider.Account) (string, error) {
	return f(account)
}

// defaultResolver treats CredentialRef as the plaintext credential directly.
var defaultResolver CredentialResolver = CredentialResolverFunc(func(account *provider.Account) (string, error) {
	if account == nil {
		return "", nil
	}
	return account.CredentialRef, nil
})

// Client is a shared HTTP client for compatible provider executors. It supports
// dependency-injected transport, per-account credential injection, custom base
// URLs, and optional proxy selection.
type Client struct {
	httpClient *http.Client
	baseURL    string // optional; overridden by provider.BaseURL
	resolver   CredentialResolver
}

// ClientOption configures a Client.
type ClientOption func(*Client)

// WithTransport sets the HTTP round-tripper. Pass http.DefaultTransport or a
// wrapped transport for custom dial / proxy behaviour.
func WithTransport(rt http.RoundTripper) ClientOption {
	return func(c *Client) {
		c.httpClient.Transport = rt
	}
}

// WithBaseURL sets a default base URL for the client. The base URL is resolved
// from this value only; Account does not carry a parent Provider reference so
// provider-level BaseURL is resolved by the caller before constructing the
// client.
func WithBaseURL(url string) ClientOption {
	return func(c *Client) {
		c.baseURL = strings.TrimRight(url, "/")
	}
}

// WithTimeout sets the per-request timeout.
func WithTimeout(d time.Duration) ClientOption {
	return func(c *Client) {
		c.httpClient.Timeout = d
	}
}

// WithCredentialResolver sets a custom credential resolver. The default resolver
// treats Account.CredentialRef as the plaintext credential, matching Phase 2.
func WithCredentialResolver(r CredentialResolver) ClientOption {
	return func(c *Client) {
		c.resolver = r
	}
}

// NewClient creates a new Client with the given options. Defaults:
//   - http.DefaultTransport
//   - 60s timeout
//   - no base URL override
//   - default credential resolver (CredentialRef as plaintext)
func NewClient(opts ...ClientOption) *Client {
	c := &Client{
		httpClient: &http.Client{
			Transport: http.DefaultTransport,
			Timeout:   60 * time.Second,
		},
		resolver: defaultResolver,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Do sends an HTTP request and returns the response. The caller must close
// resp.Body when done.
func (c *Client) Do(req *http.Request) (*http.Response, error) {
	return c.httpClient.Do(req)
}

// Transport returns the underlying HTTP round-tripper.
func (c *Client) Transport() http.RoundTripper {
	return c.httpClient.Transport
}

// WithBaseURL returns a new Client that is a copy of the receiver with only the
// base URL replaced. The returned client shares the same HTTP transport, timeout,
// and CredentialResolver as the original. Use this when you need a per-provider
// client that differs only in base URL.
//
// This is safer than constructing a new Client from options because it
// guarantees that all runtime configuration (transport, timeout, resolver,
// and any future settings) are preserved.
func (c *Client) WithBaseURL(baseURL string) *Client {
	return &Client{
		httpClient: &http.Client{
			Transport: c.httpClient.Transport,
			Timeout:   c.httpClient.Timeout,
		},
		baseURL:  strings.TrimRight(baseURL, "/"),
		resolver: c.resolver,
	}
}

// BuildRequest constructs an *http.Request with the given method, path, body,
// and account credentials. The path is joined with the resolved base URL.
//
// Credential injection:
//   - OpenAI-compatible providers: Authorization: Bearer <token>
//   - Anthropic-compatible providers: x-api-key: <token>
//
// AuthType on the account selects the injection strategy.
func (c *Client) BuildRequest(ctx context.Context, method, path string, body []byte, account *provider.Account) (*http.Request, error) {
	baseURL := c.resolveBaseURL(account)
	url := fmt.Sprintf("%s%s", strings.TrimRight(baseURL, "/"), path)

	var bodyReader io.Reader
	if len(body) > 0 {
		bodyReader = bytes.NewReader(body)
	}

	httpReq, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")

	// Inject credentials via the resolver, which handles reference-to-plaintext
	// resolution (e.g. secret-store lookup). The default resolver treats
	// CredentialRef as the plaintext token, matching Phase 2.
	if account != nil {
		cred, err := c.resolver.Resolve(account)
		if err != nil {
			return nil, fmt.Errorf("resolve credential: %w", err)
		}
		switch account.AuthType {
		case "api_key", "oauth", "":
			httpReq.Header.Set("Authorization", "Bearer "+cred)
		case "anthropic_api_key":
			httpReq.Header.Set("x-api-key", cred)
			httpReq.Header.Set("anthropic-version", "2023-06-01")
		default:
			httpReq.Header.Set("Authorization", "Bearer "+cred)
		}
	}

	return httpReq, nil
}

// resolveBaseURL returns the base URL to use for requests.
// The base URL is taken from the client-level override if set, otherwise empty.
// Account does not carry a parent Provider reference, so provider-level BaseURL
// must be resolved by the caller and passed via WithBaseURL / WithBaseURL.
func (c *Client) resolveBaseURL(account *provider.Account) string {
	if c.baseURL != "" {
		return c.baseURL
	}
	return ""
}
