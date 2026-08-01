package video

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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

func TestDecodeCreateRequest(t *testing.T) {
	body := []byte(`{"model":"xai/grok-imagine-video","prompt":"a cat running","duration":5,"aspect_ratio":"16:9"}`)
	req, err := DecodeCreateRequest(body, KindGeneration)
	if err != nil {
		t.Fatal(err)
	}
	if req.Kind != KindGeneration || req.Model != "xai/grok-imagine-video" {
		t.Errorf("req = %+v", req)
	}
	if !json.Valid(req.Raw) || !strings.Contains(string(req.Raw), "duration") {
		t.Errorf("Raw must preserve the body verbatim: %s", req.Raw)
	}
}

func TestDecodeCreateRequestErrors(t *testing.T) {
	for _, tc := range []struct {
		body []byte
		kind Kind
	}{
		{nil, KindGeneration},
		{[]byte(`bad`), KindGeneration},
		{[]byte(`{}`), KindGeneration},
		{[]byte(`{"model":"m"}`), Kind("unknown")},
		{[]byte(`{"model":""}`), KindEdit},
	} {
		if _, err := DecodeCreateRequest(tc.body, tc.kind); !errors.Is(err, media.ErrMalformedRequest) {
			t.Errorf("DecodeCreateRequest(%q, %q) err = %v", tc.body, tc.kind, err)
		}
	}
}

func TestDecodeJob(t *testing.T) {
	job, err := DecodeJob([]byte(`{"request_id":"job-1","status":"COMPLETED","video":{"url":"https://x.test/v.mp4"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if job.RequestID != "job-1" || job.Status != "COMPLETED" {
		t.Errorf("job = %+v", job)
	}
	if job.Video == nil || job.Video.URL != "https://x.test/v.mp4" {
		t.Errorf("job.Video = %+v", job.Video)
	}
	if !json.Valid(job.Raw) {
		t.Errorf("Raw must preserve the payload verbatim")
	}
	if _, err := DecodeJob(nil); !errors.Is(err, media.ErrMalformedRequest) {
		t.Errorf("nil job err = %v", err)
	}
}

func TestIsTerminal(t *testing.T) {
	terminal := []string{"COMPLETED", "FAILED", "Ready", "Error", "Failed", "SUCCEEDED", "CANCELLED", "error"}
	for _, s := range terminal {
		if !IsTerminal(s) {
			t.Errorf("IsTerminal(%q) = false, want true", s)
		}
	}
	for _, s := range []string{"", "PENDING", "processing", "queued"} {
		if IsTerminal(s) {
			t.Errorf("IsTerminal(%q) = true, want false (in-progress statuses keep polling)", s)
		}
	}
}

func TestCreateJob(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/videos/generations" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("method = %q", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer sk-test-credential" {
			t.Errorf("auth = %q", got)
		}
		_, _ = w.Write([]byte(`{"request_id":"job-1","status":"PENDING"}`))
	}))
	defer srv.Close()
	client := media.NewClient(media.WithBaseURL(srv.URL))
	ex := NewExecutor(client)
	job, err := ex.Create(context.Background(), &CreateRequest{Kind: KindGeneration, Model: "xai/grok-imagine-video", Raw: []byte(`{"model":"xai/grok-imagine-video"}`)}, testAccount())
	if err != nil {
		t.Fatal(err)
	}
	if job.RequestID != "job-1" || job.Status != "PENDING" {
		t.Errorf("job = %+v", job)
	}
}

func TestCreateKindPaths(t *testing.T) {
	for _, kind := range []Kind{KindGeneration, KindEdit, KindExtension} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"request_id":"j","status":"PENDING"}`))
		}))
		client := media.NewClient(media.WithBaseURL(srv.URL))
		ex := NewExecutor(client)
		job, err := ex.Create(context.Background(), &CreateRequest{Kind: kind, Model: "m", Raw: []byte(`{"model":"m"}`)}, testAccount())
		if err != nil {
			t.Fatalf("kind %s: %v", kind, err)
		}
		if job.RequestID != "j" {
			t.Errorf("kind %s: job = %+v", kind, job)
		}
		srv.Close()
	}
}

func TestWaitPollingUntilTerminal(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/videos/job-1" {
			t.Errorf("path = %q", r.URL.Path)
		}
		attempts++
		if attempts < 3 {
			_, _ = w.Write([]byte(`{"request_id":"job-1","status":"PENDING"}`))
			return
		}
		_, _ = w.Write([]byte(`{"request_id":"job-1","status":"COMPLETED","video":{"url":"https://x.test/v.mp4"}}`))
	}))
	defer srv.Close()
	client := media.NewClient(media.WithBaseURL(srv.URL))
	ex := NewExecutor(client, WithPolling(10*time.Millisecond, time.Second))
	job, err := ex.Wait(context.Background(), "job-1", testAccount())
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != "COMPLETED" || job.Video == nil {
		t.Errorf("job = %+v", job)
	}
	if attempts < 3 {
		t.Errorf("expected >= 3 polls, got %d", attempts)
	}
}

func TestWaitCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"request_id":"job-1","status":"PENDING"}`))
	}))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	client := media.NewClient(media.WithBaseURL(srv.URL))
	ex := NewExecutor(client, WithPolling(time.Hour, time.Hour))
	_, err := ex.Wait(ctx, "job-1", testAccount())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestWaitTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"request_id":"job-1","status":"PENDING"}`))
	}))
	defer srv.Close()
	client := media.NewClient(media.WithBaseURL(srv.URL))
	ex := NewExecutor(client, WithPolling(5*time.Millisecond, 50*time.Millisecond))
	_, err := ex.Wait(context.Background(), "job-1", testAccount())
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded (cli wait timeout authority audit/08:142)", err)
	}
}

func TestVideoErrorClassificationAndRedaction(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"expired Bearer sk-proj-LEAK123"}`))
	}))
	defer srv.Close()
	client := media.NewClient(media.WithBaseURL(srv.URL))
	ex := NewExecutor(client)
	_, err := ex.Status(context.Background(), "job-1", testAccount())
	var ae *shared.AppError
	if !errors.As(err, &ae) || ae.Code != shared.ErrUnauthorized {
		t.Fatalf("err = %v, want ErrUnauthorized", err)
	}
	if strings.Contains(err.Error(), "sk-proj-LEAK123") {
		t.Errorf("credential leaked: %v", err)
	}
}

func TestVideoResponseCeiling(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", 4096)))
	}))
	defer srv.Close()
	client := media.NewClient(media.WithBaseURL(srv.URL))
	ex := NewExecutor(client, WithResponseCeiling(1024, "test-authority"))
	_, err := ex.Status(context.Background(), "job-1", testAccount())
	if !media.IsPayloadTooLarge(err) {
		t.Fatalf("err = %v, want typed 413-class error", err)
	}
}

// TestWithRoundTripTimeoutCancels verifies the round-trip timeout context
// fires and is cancelled (no leaked timer; go vet context-leak guard).
func TestWithRoundTripTimeoutCancels(t *testing.T) {
	ex := NewExecutor(media.NewClient(), WithRoundTripTimeout(100*time.Millisecond))
	ctx, cancel := ex.withRoundTripTimeout(context.Background())
	defer cancel()
	if ctx.Err() != nil {
		t.Fatalf("ctx already done: %v", ctx.Err())
	}
	select {
	case <-ctx.Done():
		if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
			t.Errorf("err = %v, want DeadlineExceeded", ctx.Err())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("round-trip timeout context never fired")
	}
	// Cancel after expiry must be a no-op (idempotent, no panic).
	cancel()
}

// FuzzDecodeRequest fuzzes the video codec parsers (P3-T12 parser fuzz
// target; must never panic).
func FuzzDecodeRequest(f *testing.F) {
	f.Add([]byte(`{"model":"xai/grok-imagine-video","duration":5}`))
	f.Add([]byte(`{"request_id":"j","status":"PENDING"}`))
	f.Add([]byte(`{"request_id":"j","status":"COMPLETED","video":{"url":"https://x.test/v.mp4"}}`))
	f.Add([]byte(`bad`))
	f.Fuzz(func(t *testing.T, body []byte) {
		_, _ = DecodeCreateRequest(body, KindGeneration)
		_, _ = DecodeJob(body)
	})
}
