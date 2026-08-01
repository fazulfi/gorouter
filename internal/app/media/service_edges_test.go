package media

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gorouter/internal/domain/provider"
	"gorouter/internal/engine/media"
	"gorouter/internal/engine/media/audio"
	"gorouter/internal/engine/media/embeddings"
	"gorouter/internal/engine/media/images"
	"gorouter/internal/engine/media/video"
	"gorouter/internal/engine/media/web"
	"gorouter/internal/shared"
)

// Distinctive sentinels so propagation is asserted by identity
// (errors.Is), never by message text.
var (
	errAccountResolution   = errors.New("account resolution failed (test)")
	errBaseResolution      = errors.New("base URL resolution failed (test)")
	errContextNotForwarded = errors.New("context was not forwarded to resolver (test)")
)

// TestServiceResolveErrorPropagatesAcrossModalities pins the uniform
// fail-closed contract: when account resolution fails, every modality
// method surfaces the resolver error by identity and returns no result.
func TestServiceResolveErrorPropagatesAcrossModalities(t *testing.T) {
	s := NewService(
		media.NewClient(),
		BaseURLResolverFunc(func(ctx context.Context, providerID string) (string, error) {
			return "https://upstream.test", nil
		}),
		AccountResolverFunc(func(ctx context.Context, providerID string) (*provider.Account, error) {
			return nil, errAccountResolution
		}),
	)
	ctx := context.Background()

	cases := []struct {
		name string
		call func() error
	}{
		{"embeddings", func() error {
			_, err := s.Embeddings(ctx, "openai", testProviderID, &embeddings.Request{Model: "m", Input: []byte(`"x"`)})
			return err
		}},
		{"image-generation", func() error {
			_, err := s.ImageGeneration(ctx, "openai", testProviderID, &images.GenerationRequest{Prompt: "cat"})
			return err
		}},
		{"image-to-text", func() error {
			_, err := s.ImageToText(ctx, "openai", testProviderID, &images.ImageToTextRequest{
				Model:    "gpt-4o",
				Messages: []images.Message{{Role: "user", Content: json.RawMessage(`[{"type":"text","text":"x"}]`)}},
			})
			return err
		}},
		{"tts", func() error {
			_, err := s.TTS(ctx, "elevenlabs", testProviderID, &audio.SpeechRequest{Model: "m", Input: "hi", Voice: "v"})
			return err
		}},
		{"stt", func() error {
			_, err := s.STT(ctx, "assemblyai", testProviderID, &audio.TranscribeRequest{Model: "m", FileName: "c.mp3", Audio: strings.NewReader("AUDIO")})
			return err
		}},
		{"voices", func() error {
			_, err := s.Voices(ctx, "elevenlabs", testProviderID, "en")
			return err
		}},
		{"web-search", func() error {
			_, err := s.WebSearch(ctx, "serper", testProviderID, &web.SearchRequest{Query: "q"})
			return err
		}},
		{"web-fetch", func() error {
			_, err := s.WebFetch(ctx, "firecrawl", testProviderID, &web.FetchRequest{URL: "https://x.test"})
			return err
		}},
		{"video-create", func() error {
			_, err := s.VideoCreate(ctx, "xai", testProviderID, video.KindGeneration, []byte(`{"model":"xai/grok-imagine-video"}`))
			return err
		}},
		{"video-status", func() error {
			_, err := s.VideoStatus(ctx, "xai", testProviderID, "job-1")
			return err
		}},
		{"video-wait", func() error {
			_, err := s.VideoWait(ctx, "xai", testProviderID, "job-1")
			return err
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(); !errors.Is(err, errAccountResolution) {
				t.Fatalf("err = %v, want sentinel %v (resolve failure must propagate by identity)", err, errAccountResolution)
			}
		})
	}
}

// TestServiceBaseURLResolverErrorPropagates pins that a base URL
// resolution failure surfaces by identity and is not swallowed.
func TestServiceBaseURLResolverErrorPropagates(t *testing.T) {
	s := NewService(
		media.NewClient(),
		BaseURLResolverFunc(func(ctx context.Context, providerID string) (string, error) {
			return "", errBaseResolution
		}),
		AccountResolverFunc(func(ctx context.Context, providerID string) (*provider.Account, error) {
			return testAccount(), nil
		}),
	)

	_, err := s.Embeddings(context.Background(), "openai", testProviderID, &embeddings.Request{Model: "m", Input: []byte(`"x"`)})
	if !errors.Is(err, errBaseResolution) {
		t.Fatalf("err = %v, want sentinel %v", err, errBaseResolution)
	}
}

// TestServiceVideoCreateUnknownKindFailsClosed pins the deterministic
// rejection of unknown video kinds before any resolution or HTTP call.
func TestServiceVideoCreateUnknownKindFailsClosed(t *testing.T) {
	s, srv := newTestService(t)
	defer srv.Close()

	_, err := s.VideoCreate(context.Background(), "xai", testProviderID, video.Kind("bogus"), []byte(`{"model":"m"}`))
	if !errors.Is(err, media.ErrMalformedRequest) {
		t.Fatalf("err = %v, want ErrMalformedRequest", err)
	}
	if !strings.Contains(err.Error(), "unknown video kind") {
		t.Errorf("err = %v, want message mentioning unknown kind", err)
	}
}

// TestServiceVideoCreateMalformedBodyFailsClosed pins that a body failing
// codec validation is rejected as malformed after resolution succeeds.
func TestServiceVideoCreateMalformedBodyFailsClosed(t *testing.T) {
	s, srv := newTestService(t)
	defer srv.Close()

	_, err := s.VideoCreate(context.Background(), "xai", testProviderID, video.KindGeneration, []byte(`{}`))
	if !errors.Is(err, media.ErrMalformedRequest) {
		t.Fatalf("err = %v, want ErrMalformedRequest (missing model)", err)
	}
}

// TestServiceVideoWaitTerminalJobReturnsImmediately pins the async-wait
// contract: an already-terminal job is returned without polling.
func TestServiceVideoWaitTerminalJobReturnsImmediately(t *testing.T) {
	s, srv := newTestService(t)
	defer srv.Close()

	job, err := s.VideoWait(context.Background(), "xai", testProviderID, "job-1")
	if err != nil {
		t.Fatalf("VideoWait: %v", err)
	}
	if job == nil || job.RequestID != "job-1" || job.Status != "COMPLETED" {
		t.Fatalf("VideoWait = %+v, want COMPLETED job-1", job)
	}
}

// TestServiceVideoStatusUpstreamErrorSurfaces pins that a non-2xx upstream
// status becomes a typed media error with the status code preserved.
func TestServiceVideoStatusUpstreamErrorSurfaces(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"boom"}`))
	}))
	defer srv.Close()
	s := NewService(
		media.NewClient(media.WithTransport(srv.Client().Transport)),
		BaseURLResolverFunc(func(ctx context.Context, providerID string) (string, error) { return srv.URL, nil }),
		AccountResolverFunc(func(ctx context.Context, providerID string) (*provider.Account, error) { return testAccount(), nil }),
	)

	_, err := s.VideoStatus(context.Background(), "xai", testProviderID, "job-1")
	if err == nil {
		t.Fatal("VideoStatus: expected upstream error")
	}
	var ae *shared.AppError
	if !errors.As(err, &ae) {
		t.Fatalf("err = %v, want *shared.AppError", err)
	}
	if ae.HTTPStatus != http.StatusInternalServerError {
		t.Errorf("HTTPStatus = %d, want 500", ae.HTTPStatus)
	}
}

// TestServiceCancellationShortCircuitsBeforeUpstream pins that a cancelled
// context aborts the request path before any upstream HTTP call: the
// resolver observes the forwarded context, and the handler is never hit.
func TestServiceCancellationShortCircuitsBeforeUpstream(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	s := NewService(
		media.NewClient(media.WithTransport(srv.Client().Transport)),
		BaseURLResolverFunc(func(ctx context.Context, providerID string) (string, error) { return srv.URL, nil }),
		AccountResolverFunc(func(ctx context.Context, providerID string) (*provider.Account, error) {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(2 * time.Second):
				return nil, errContextNotForwarded
			}
		}),
	)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := s.Embeddings(ctx, "openai", testProviderID, &embeddings.Request{Model: "m", Input: []byte(`"x"`)})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if hits.Load() != 0 {
		t.Errorf("upstream handler hit %d times, want 0 (cancellation must short-circuit)", hits.Load())
	}
}
