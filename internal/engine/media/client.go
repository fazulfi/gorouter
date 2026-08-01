package media

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"gorouter/internal/domain/provider"
)

// Client is the media transport client. It mirrors the generic provider
// client shape (internal/engine/providers/generic/client.go) but accepts a
// streaming io.Reader body so uploads never require a full-body buffer.
type Client struct {
	httpClient *http.Client
	baseURL    string
}

// ClientOption configures a Client.
type ClientOption func(*Client)

// WithTransport sets the underlying HTTP round tripper.
func WithTransport(rt http.RoundTripper) ClientOption {
	return func(c *Client) { c.httpClient.Transport = rt }
}

// WithBaseURL sets the base URL (trailing slash trimmed). The base URL is
// resolved by the caller from provider configuration; media never invents a
// default endpoint host (fail closed when unset).
func WithBaseURL(url string) ClientOption {
	return func(c *Client) { c.baseURL = strings.TrimRight(url, "/") }
}

// WithTimeout sets the per-request HTTP timeout.
func WithTimeout(d time.Duration) ClientOption {
	return func(c *Client) { c.httpClient.Timeout = d }
}

// NewClient creates a Client with default transport and 60s timeout (mirrors
// generic.NewClient defaults).
func NewClient(opts ...ClientOption) *Client {
	c := &Client{httpClient: &http.Client{}}
	for _, opt := range opts {
		opt(c)
	}
	if c.httpClient.Transport == nil {
		c.httpClient.Transport = http.DefaultTransport
	}
	if c.httpClient.Timeout == 0 {
		c.httpClient.Timeout = 60 * time.Second
	}
	return c
}

// Do performs the request with the client's transport and timeout.
func (c *Client) Do(req *http.Request) (*http.Response, error) {
	// #nosec G704 -- DECISIONS #376: custom-provider URLs intentionally permit loopback/LAN/internal/link-local targets.
	return c.httpClient.Do(req)
}

// BaseURL returns the configured base URL (may be empty).
func (c *Client) BaseURL() string {
	return c.baseURL
}

// WithBaseURL returns a copy of the client with a different base URL,
// sharing the transport and timeout (mirrors generic.Client.WithBaseURL).
func (c *Client) WithBaseURL(baseURL string) *Client {
	clone := *c
	clone.baseURL = strings.TrimRight(baseURL, "/")
	return &clone
}

// ErrBaseURLUnconfigured is returned by BuildRequest when no base URL was
// supplied — fail closed rather than guessing a provider host.
var ErrBaseURLUnconfigured = errors.New("media client: base URL not configured")

// BuildRequest constructs an authenticated request. Auth injection mirrors
// generic.Client.BuildRequest: api_key/oauth/empty AuthType → Bearer header;
// anthropic_api_key → x-api-key + anthropic-version. A nil body sends no
// body; a non-empty contentType sets Content-Type.
func (c *Client) BuildRequest(ctx context.Context, method, path string, body io.Reader, contentType string, account *provider.Account) (*http.Request, error) {
	if c.baseURL == "" {
		return nil, ErrBaseURLUnconfigured
	}
	var rdr io.Reader
	if body != nil {
		rdr = body
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, rdr)
	if err != nil {
		return nil, err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.Header.Set("Accept", "application/json")
	if account == nil {
		return req, nil
	}
	switch account.AuthType {
	case "api_key", "oauth", "":
		cred, cerr := resolveCredential(account)
		if cerr != nil {
			return nil, cerr
		}
		req.Header.Set("Authorization", "Bearer "+cred)
	case "anthropic_api_key":
		cred, cerr := resolveCredential(account)
		if cerr != nil {
			return nil, cerr
		}
		req.Header.Set("x-api-key", cred)
		req.Header.Set("anthropic-version", "2023-06-01")
	default:
		cred, cerr := resolveCredential(account)
		if cerr != nil {
			return nil, cerr
		}
		req.Header.Set("Authorization", "Bearer "+cred)
	}
	return req, nil
}

func resolveCredential(account *provider.Account) (string, error) {
	if account == nil {
		return "", nil
	}
	return account.CredentialRef, nil
}
