package web

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"gorouter/internal/domain/provider"
	"gorouter/internal/engine/media"
	"gorouter/internal/shared"
)

func testAccount() *provider.Account {
	return &provider.Account{
		ID:            uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		AuthType:      "api_key",
		CredentialRef: "sk-test-credential",
	}
}

func TestDecodeSearchRequest(t *testing.T) {
	req, err := DecodeSearchRequest([]byte(`{"query":"golang","max_results":3}`))
	if err != nil {
		t.Fatal(err)
	}
	if req.Query != "golang" || req.MaxResults != 3 {
		t.Errorf("req = %+v", req)
	}
}

func TestDecodeSearchRequestErrors(t *testing.T) {
	for _, body := range [][]byte{nil, []byte(`bad`), []byte(`{}`)} {
		if _, err := DecodeSearchRequest(body); !errors.Is(err, media.ErrMalformedRequest) {
			t.Errorf("DecodeSearchRequest(%q) err = %v", body, err)
		}
	}
}

func TestDecodeFetchRequestErrors(t *testing.T) {
	for _, body := range [][]byte{nil, []byte(`bad`), []byte(`{}`)} {
		if _, err := DecodeFetchRequest(body); !errors.Is(err, media.ErrMalformedRequest) {
			t.Errorf("DecodeFetchRequest(%q) err = %v", body, err)
		}
	}
}

func TestSearchResultClamp(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req SearchRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode err = %v", err)
		}
		t.Logf("max_results received = %d", req.MaxResults)
		_, _ = w.Write([]byte(`{"query":"q","results":[{"title":"t","url":"https://x.test","snippet":"s"}]}`))
	}))
	defer srv.Close()

	client := media.NewClient(media.WithBaseURL(srv.URL))
	ex := NewExecutor(client)

	// brave-search: proven maxMaxResults 20 (brave-search.js:31) — 99 clamps to 20.
	req := &SearchRequest{Query: "q", MaxResults: 99}
	if _, err := ex.Search(context.Background(), req, "brave-search", testAccount()); err != nil {
		t.Fatal(err)
	}
	if req.MaxResults != 20 {
		t.Errorf("clamped max_results = %d, want 20 (brave-search.js:31)", req.MaxResults)
	}

	// google-pse: proven maxMaxResults 10 (google-pse.js:31).
	req = &SearchRequest{Query: "q", MaxResults: 50}
	if _, err := ex.Search(context.Background(), req, "google-pse", testAccount()); err != nil {
		t.Fatal(err)
	}
	if req.MaxResults != 10 {
		t.Errorf("clamped max_results = %d, want 10 (google-pse.js:31)", req.MaxResults)
	}
}

func TestSearchDefaultMaxResults(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req SearchRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.MaxResults != 5 {
			t.Errorf("default max_results = %d, want 5 (registry defaultMaxResults)", req.MaxResults)
		}
		_, _ = w.Write([]byte(`{"query":"q","results":[]}`))
	}))
	defer srv.Close()
	client := media.NewClient(media.WithBaseURL(srv.URL))
	_, err := NewExecutor(client).Search(context.Background(), &SearchRequest{Query: "q"}, "serper", testAccount())
	if err != nil {
		t.Fatal(err)
	}
}

func TestSearchHandlerFallbackConfig(t *testing.T) {
	// openai has no registry search-config block; handler clamps apply
	// (search/index.js:75).
	cfg, ok := SearchConfigFor("openai")
	if !ok {
		t.Fatal("SearchConfigFor(openai) not ok")
	}
	if cfg.DefaultMaxResults != 5 || cfg.MaxMaxResults != 100 {
		t.Errorf("fallback cfg = %+v, want 5/100", cfg)
	}
}

func TestSearchDecodeResponse(t *testing.T) {
	resp, err := DecodeSearchResponse([]byte(`{"query":"q","results":[{"title":"T","url":"https://x.test","snippet":"S"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Results) != 1 || resp.Results[0].Title != "T" {
		t.Errorf("resp = %+v", resp)
	}
	if _, err := DecodeSearchResponse([]byte(`bad`)); !errors.Is(err, media.ErrMalformedRequest) {
		t.Errorf("bad resp err = %v", err)
	}
}

func TestFetchSuccessAndClamp(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/web/fetch" {
			t.Errorf("path = %q", r.URL.Path)
		}
		var req FetchRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode err = %v", err)
		}
		if req.MaxCharacters != MaxFetchCharacters {
			t.Errorf("max_characters = %d, want %d (firecrawl.js:31)", req.MaxCharacters, MaxFetchCharacters)
		}
		_, _ = w.Write([]byte(`{"url":"https://x.test","content":"extracted","media_type":"text/markdown"}`))
	}))
	defer srv.Close()
	client := media.NewClient(media.WithBaseURL(srv.URL))
	req := &FetchRequest{URL: "https://x.test", MaxCharacters: 999999}
	resp, err := NewExecutor(client).Fetch(context.Background(), req, testAccount())
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "extracted" {
		t.Errorf("content = %q", resp.Content)
	}
}

func TestWebErrorClassification(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"bad key sk-proj-HUSH"}`))
	}))
	defer srv.Close()
	client := media.NewClient(media.WithBaseURL(srv.URL))
	_, err := NewExecutor(client).Search(context.Background(), &SearchRequest{Query: "q"}, "serper", testAccount())
	var ae *shared.AppError
	if !errors.As(err, &ae) || ae.Code != shared.ErrUnauthorized {
		t.Fatalf("err = %v, want ErrUnauthorized", err)
	}
	if strings.Contains(err.Error(), "sk-proj-HUSH") {
		t.Errorf("credential leaked: %v", err)
	}
}

func TestWebCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()
	client := media.NewClient(media.WithBaseURL(srv.URL))
	_, err := NewExecutor(client).Search(ctx, &SearchRequest{Query: "q"}, "serper", testAccount())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

// FuzzDecodeRequest fuzzes the web codec parsers (P3-T12 parser fuzz target;
// must never panic).
func FuzzDecodeRequest(f *testing.F) {
	f.Add([]byte(`{"query":"q","max_results":5}`))
	f.Add([]byte(`{"url":"https://x.test"}`))
	f.Add([]byte(`{}`))
	f.Add([]byte(`bad`))
	f.Fuzz(func(t *testing.T, body []byte) {
		_, _ = DecodeSearchRequest(body)
		_, _ = DecodeFetchRequest(body)
		_, _ = DecodeSearchResponse(body)
		_, _ = DecodeFetchResponse(body)
	})
}
