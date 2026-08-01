package embeddings

import (
	"context"
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

func TestDecodeRequest(t *testing.T) {
	req, err := DecodeRequest([]byte(`{"model":"text-embedding-3-small","input":"hello","encoding_format":"float"}`))
	if err != nil {
		t.Fatal(err)
	}
	if req.Model != "text-embedding-3-small" || string(req.Input) != `"hello"` {
		t.Errorf("req = %+v", req)
	}
	if req.EncodingFormat != "float" {
		t.Errorf("EncodingFormat = %q", req.EncodingFormat)
	}
}

func TestDecodeRequestArrayInput(t *testing.T) {
	req, err := DecodeRequest([]byte(`{"model":"m","input":["a","b"]}`))
	if err != nil {
		t.Fatal(err)
	}
	if string(req.Input) != `["a","b"]` {
		t.Errorf("Input = %s", req.Input)
	}
}

func TestDecodeRequestErrors(t *testing.T) {
	cases := [][]byte{
		nil,
		{},
		[]byte(`not json`),
		[]byte(`{}`),
		[]byte(`{"model":"m"}`),
		[]byte(`{"model":"m","input":null}`),
	}
	for _, body := range cases {
		if _, err := DecodeRequest(body); !errors.Is(err, media.ErrMalformedRequest) {
			t.Errorf("DecodeRequest(%q) err = %v, want ErrMalformedRequest wrap", body, err)
		}
	}
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	req := &Request{Model: "m", Input: []byte(`"x"`), Dimensions: intPtr(1536)}
	body, err := EncodeRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeRequest(body)
	if err != nil {
		t.Fatal(err)
	}
	if got.Model != "m" || got.Dimensions == nil || *got.Dimensions != 1536 {
		t.Errorf("round trip = %+v", got)
	}
}

func TestDecodeResponse(t *testing.T) {
	body := []byte(`{"object":"list","data":[{"object":"embedding","index":0,"embedding":[0.1,0.2]}],"model":"m","usage":{"prompt_tokens":3,"total_tokens":3}}`)
	resp, err := DecodeResponse(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Data) != 1 || resp.Data[0].Embedding[1] != 0.2 {
		t.Errorf("resp = %+v", resp)
	}
	if resp.Usage == nil || resp.Usage.TotalTokens != 3 {
		t.Errorf("usage = %+v", resp.Usage)
	}
}

func TestDecodeResponseErrors(t *testing.T) {
	for _, body := range [][]byte{nil, []byte(`garbage`)} {
		if _, err := DecodeResponse(body); !errors.Is(err, media.ErrMalformedRequest) {
			t.Errorf("DecodeResponse(%q) err = %v", body, err)
		}
	}
}

func intPtr(v int) *int { return &v }

func TestExecuteSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/embeddings" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("method = %q", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer sk-test-credential" {
			t.Errorf("auth = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[{"object":"embedding","index":0,"embedding":[1.5]}],"model":"m"}`))
	}))
	defer srv.Close()

	client := media.NewClient(media.WithBaseURL(srv.URL))
	ex := NewExecutor(client)
	resp, err := ex.Execute(context.Background(), &Request{Model: "m", Input: []byte(`"x"`)}, testAccount())
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Data) != 1 || resp.Data[0].Embedding[0] != 1.5 {
		t.Errorf("resp = %+v", resp)
	}
}

func TestExecuteAuthErrorRedaction(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid api key sk-proj-TOP-SECRET"}`))
	}))
	defer srv.Close()

	client := media.NewClient(media.WithBaseURL(srv.URL))
	_, err := NewExecutor(client).Execute(context.Background(), &Request{Model: "m", Input: []byte(`"x"`)}, testAccount())
	var ae *shared.AppError
	if !errors.As(err, &ae) {
		t.Fatalf("err = %T, want *shared.AppError", err)
	}
	if ae.Code != shared.ErrUnauthorized {
		t.Errorf("Code = %q", ae.Code)
	}
	details := ae.Details.(map[string]any)
	body := details["body"].(string)
	if strings.Contains(body, "sk-proj-TOP-SECRET") {
		t.Errorf("credential leaked in error details: %q", body)
	}
	if !strings.Contains(body, "sk-proj-***") {
		t.Errorf("expected redacted marker in body: %q", body)
	}
}

func TestExecuteRateLimitAndUpstream(t *testing.T) {
	for _, tc := range []struct {
		status int
		want   shared.ErrorCode
	}{
		{http.StatusTooManyRequests, shared.ErrRateLimited},
		{http.StatusInternalServerError, shared.ErrInternal},
		{http.StatusBadRequest, shared.ErrInternal},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(tc.status)
			_, _ = w.Write([]byte(`{"error":"boom"}`))
		}))
		client := media.NewClient(media.WithBaseURL(srv.URL))
		_, err := NewExecutor(client).Execute(context.Background(), &Request{Model: "m", Input: []byte(`"x"`)}, testAccount())
		var ae *shared.AppError
		if !errors.As(err, &ae) || ae.Code != tc.want {
			t.Errorf("status %d: err = %v, want code %q", tc.status, err, tc.want)
		}
		srv.Close()
	}
}

func TestExecuteResponseCeiling(t *testing.T) {
	big := strings.Repeat("x", 2048)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(big))
	}))
	defer srv.Close()
	client := media.NewClient(media.WithBaseURL(srv.URL))
	ex := NewExecutor(client, WithResponseCeiling(1024, "test-authority"))
	_, err := ex.Execute(context.Background(), &Request{Model: "m", Input: []byte(`"x"`)}, testAccount())
	if !media.IsPayloadTooLarge(err) {
		t.Fatalf("err = %v, want typed 413-class error", err)
	}
}

func TestExecuteCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()
	client := media.NewClient(media.WithBaseURL(srv.URL))
	cancel()
	_, err := NewExecutor(client).Execute(ctx, &Request{Model: "m", Input: []byte(`"x"`)}, testAccount())
	if err == nil {
		t.Fatal("expected cancellation error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

func TestExecuteBaseURLRequired(t *testing.T) {
	client := media.NewClient()
	_, err := NewExecutor(client).Execute(context.Background(), &Request{Model: "m", Input: []byte(`"x"`)}, testAccount())
	if !errors.Is(err, media.ErrBaseURLUnconfigured) {
		t.Fatalf("err = %v, want ErrBaseURLUnconfigured", err)
	}
}

func TestExecuteNilRequest(t *testing.T) {
	client := media.NewClient(media.WithBaseURL("https://example.test"))
	_, err := NewExecutor(client).Execute(context.Background(), nil, testAccount())
	if !errors.Is(err, media.ErrMalformedRequest) {
		t.Fatalf("err = %v, want ErrMalformedRequest", err)
	}
}

// FuzzDecodeRequest fuzzes the embeddings request parser (P3-T12 parser fuzz
// target; must never panic).
func FuzzDecodeRequest(f *testing.F) {
	f.Add([]byte(`{"model":"m","input":"hello"}`))
	f.Add([]byte(`{}`))
	f.Add([]byte(`{"model":"m","input":["a","b"],"dimensions":1536}`))
	f.Add([]byte(`garbage`))
	f.Fuzz(func(t *testing.T, body []byte) {
		_, _ = DecodeRequest(body)
		_, _ = DecodeResponse(body)
	})
}
