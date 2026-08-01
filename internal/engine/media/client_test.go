package media

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gorouter/internal/domain/provider"
)

func testAccount(authType string) *provider.Account {
	return &provider.Account{
		ID:            uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		AuthType:      authType,
		CredentialRef: "sk-test-credential",
	}
}

func TestBuildRequestBaseURLRequired(t *testing.T) {
	c := NewClient()
	_, err := c.BuildRequest(context.Background(), "POST", "/v1/embeddings", nil, "application/json", testAccount("api_key"))
	if !errors.Is(err, ErrBaseURLUnconfigured) {
		t.Fatalf("err = %v, want ErrBaseURLUnconfigured (fail closed on unproven host)", err)
	}
}

func TestBuildRequestAuthInjection(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "{}")
	}))
	defer srv.Close()

	c := NewClient(WithBaseURL(srv.URL))

	cases := []struct {
		authType string
		wantHdr  string
		wantVal  string
	}{
		{"api_key", "Authorization", "Bearer sk-test-credential"},
		{"oauth", "Authorization", "Bearer sk-test-credential"},
		{"", "Authorization", "Bearer sk-test-credential"},
		{"anthropic_api_key", "x-api-key", "sk-test-credential"},
	}
	for _, tc := range cases {
		req, err := c.BuildRequest(context.Background(), "GET", "/v1/test", nil, "", testAccount(tc.authType))
		if err != nil {
			t.Fatalf("BuildRequest(%s) err = %v", tc.authType, err)
		}
		got := req.Header.Get(tc.wantHdr)
		if got != tc.wantVal {
			t.Errorf("BuildRequest(%s) %s = %q, want %q", tc.authType, tc.wantHdr, got, tc.wantVal)
		}
	}
}

func TestBuildRequestAnthropicVersion(t *testing.T) {
	req, err := NewClient(WithBaseURL("https://example.test")).BuildRequest(
		context.Background(), "POST", "/v1/messages", nil, "application/json", testAccount("anthropic_api_key"))
	if err != nil {
		t.Fatal(err)
	}
	if got := req.Header.Get("anthropic-version"); got != "2023-06-01" {
		t.Errorf("anthropic-version = %q, want 2023-06-01", got)
	}
}

func TestBuildRequestContentTypeAndBody(t *testing.T) {
	c := NewClient(WithBaseURL("https://example.test"))
	req, err := c.BuildRequest(context.Background(), "POST", "/v1/embeddings", strings.NewReader(`{"input":"x"}`), "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := req.Header.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q", got)
	}
	body, _ := io.ReadAll(req.Body)
	if string(body) != `{"input":"x"}` {
		t.Errorf("body = %q", body)
	}
}

func TestClientDoWithTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
	}))
	defer srv.Close()
	c := NewClient(WithBaseURL(srv.URL), WithTimeout(100*time.Millisecond))
	req, err := c.BuildRequest(context.Background(), "GET", "/slow", nil, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	_, err = c.Do(req)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if time.Since(start) > time.Second {
		t.Errorf("timeout took %v, want ~100ms", time.Since(start))
	}
}

func TestWithBaseURLCopySharesTransport(t *testing.T) {
	base := NewClient(WithBaseURL("https://a.test"))
	copy := base.WithBaseURL("https://b.test")
	if copy.BaseURL() != "https://b.test" {
		t.Errorf("copy BaseURL = %q", copy.BaseURL())
	}
	if base.BaseURL() != "https://a.test" {
		t.Errorf("original BaseURL mutated = %q", base.BaseURL())
	}
}

func TestWithBaseURLTrimTrailingSlash(t *testing.T) {
	c := NewClient(WithBaseURL("https://a.test/"))
	if c.BaseURL() != "https://a.test" {
		t.Errorf("BaseURL = %q, want trailing slash trimmed", c.BaseURL())
	}
}
