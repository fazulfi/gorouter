package media

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
	"gorouter/internal/engine/media/audio"
	"gorouter/internal/engine/media/embeddings"
	"gorouter/internal/engine/media/images"
	"gorouter/internal/engine/media/video"
	"gorouter/internal/engine/media/web"
	"gorouter/internal/shared"
)

const testProviderID = "11111111-1111-1111-1111-111111111111"

func testAccount() *provider.Account {
	return &provider.Account{
		ID:            uuid.MustParse(testProviderID),
		AuthType:      "api_key",
		CredentialRef: "sk-test-credential",
	}
}

func newTestService(t *testing.T) (*Service, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/embeddings":
			_, _ = w.Write([]byte(`{"object":"list","data":[{"object":"embedding","index":0,"embedding":[0.5]}],"model":"m"}`))
		case "/v1/images/generations":
			_, _ = w.Write([]byte(`{"created":1,"data":[{"url":"https://x.test/i.png"}]}`))
		case "/v1/chat/completions":
			_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"a cat"}}]}`))
		case "/v1/audio/speech":
			w.Header().Set("Content-Type", "audio/mpeg")
			_, _ = w.Write([]byte("MP3DATA"))
		case "/v1/audio/transcriptions":
			_, _ = w.Write([]byte(`{"text":"transcribed"}`))
		case "/v1/audio/voices":
			_, _ = w.Write([]byte(`{"object":"list","data":[{"voice_id":"v1"}]}`))
		case "/v1/search":
			_, _ = w.Write([]byte(`{"query":"q","results":[{"title":"T","url":"https://x.test","snippet":"S"}]}`))
		case "/v1/web/fetch":
			_, _ = w.Write([]byte(`{"url":"https://x.test","content":"extracted"}`))
		case "/v1/videos/generations", "/v1/videos/edits", "/v1/videos/extensions":
			_, _ = w.Write([]byte(`{"request_id":"job-1","status":"PENDING"}`))
		case "/v1/videos/job-1":
			_, _ = w.Write([]byte(`{"request_id":"job-1","status":"COMPLETED","video":{"url":"https://x.test/v.mp4"}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"not found"}`))
		}
	}))
	s := NewService(
		media.NewClient(media.WithTransport(srv.Client().Transport)),
		BaseURLResolverFunc(func(ctx context.Context, providerID string) (string, error) {
			return srv.URL, nil
		}),
		AccountResolverFunc(func(ctx context.Context, providerID string) (*provider.Account, error) {
			return testAccount(), nil
		}),
	)
	return s, srv
}

func TestServiceCapabilityFailClosed(t *testing.T) {
	s, srv := newTestService(t)
	defer srv.Close()
	ctx := context.Background()

	if s.Supports(media.ModalityVideoGeneration, "openai") {
		t.Errorf("Supports(video, openai) = true, want false")
	}
	if !s.Supports(media.ModalityVideoGeneration, "xai") {
		t.Errorf("Supports(video, xai) = false, want true (xai.js:37)")
	}
	if !s.Supports(media.ModalityTTS, "elevenlabs") {
		t.Errorf("Supports(tts, elevenlabs) = false, want true (elevenlabs.js:16)")
	}

	// Unproven provider fails closed with the typed capability error.
	_, err := s.Embeddings(ctx, "elevenlabs", testProviderID, &embeddings.Request{Model: "m", Input: []byte(`"x"`)})
	if !media.IsCapabilityUnproven(err) {
		t.Fatalf("err = %v, want typed capability-unproven error", err)
	}
	var ae *shared.AppError
	if errors.As(err, &ae) && ae.HTTPStatus != http.StatusBadRequest {
		t.Errorf("HTTPStatus = %d, want 400", ae.HTTPStatus)
	}
}

func TestServiceModalitiesEndToEnd(t *testing.T) {
	s, srv := newTestService(t)
	defer srv.Close()
	ctx := context.Background()

	emb, err := s.Embeddings(ctx, "openai", testProviderID, &embeddings.Request{Model: "m", Input: []byte(`"x"`)})
	if err != nil || len(emb.Data) != 1 {
		t.Errorf("Embeddings: resp=%+v err=%v", emb, err)
	}

	img, err := s.ImageGeneration(ctx, "openai", testProviderID, &images.GenerationRequest{Prompt: "cat"})
	if err != nil || len(img.Data) != 1 {
		t.Errorf("ImageGeneration: resp=%+v err=%v", img, err)
	}

	itt, err := s.ImageToText(ctx, "openai", testProviderID, &images.ImageToTextRequest{
		Model: "gpt-4o",
		Messages: []images.Message{{
			Role:    "user",
			Content: json.RawMessage(`[{"type":"image_url","image_url":{"url":"data:image/png;base64,AA"}}]`),
		}},
	})
	if err != nil || !strings.Contains(string(itt), "a cat") {
		t.Errorf("ImageToText: resp=%s err=%v", itt, err)
	}

	sp, err := s.TTS(ctx, "elevenlabs", testProviderID, &audio.SpeechRequest{Model: "m", Input: "hi", Voice: "v"})
	if err != nil || string(sp.Audio) != "MP3DATA" {
		t.Errorf("TTS: resp=%+v err=%v", sp, err)
	}

	st, err := s.STT(ctx, "assemblyai", testProviderID, &audio.TranscribeRequest{Model: "m", FileName: "c.mp3", Audio: strings.NewReader("AUDIO")})
	if err != nil || st.Text != "transcribed" {
		t.Errorf("STT: resp=%+v err=%v", st, err)
	}

	vc, err := s.Voices(ctx, "elevenlabs", testProviderID, "en")
	if err != nil || len(vc.Data) != 1 {
		t.Errorf("Voices: resp=%+v err=%v", vc, err)
	}

	ws, err := s.WebSearch(ctx, "serper", testProviderID, &web.SearchRequest{Query: "q"})
	if err != nil || len(ws.Results) != 1 {
		t.Errorf("WebSearch: resp=%+v err=%v", ws, err)
	}

	wf, err := s.WebFetch(ctx, "firecrawl", testProviderID, &web.FetchRequest{URL: "https://x.test"})
	if err != nil || wf.Content != "extracted" {
		t.Errorf("WebFetch: resp=%+v err=%v", wf, err)
	}

	job, err := s.VideoCreate(ctx, "xai", testProviderID, video.KindGeneration, []byte(`{"model":"xai/grok-imagine-video","duration":5}`))
	if err != nil || job.RequestID != "job-1" {
		t.Errorf("VideoCreate: job=%+v err=%v", job, err)
	}

	job, err = s.VideoStatus(ctx, "xai", testProviderID, "job-1")
	if err != nil || job.Status != "COMPLETED" {
		t.Errorf("VideoStatus: job=%+v err=%v", job, err)
	}

	// Video edit and extension are proven only for xai (videoCore.js:14).
	job, err = s.VideoCreate(ctx, "xai", testProviderID, video.KindEdit, []byte(`{"model":"xai/grok-imagine-video","image":"data:image/png;base64,AA"}`))
	if err != nil || job.RequestID != "job-1" {
		t.Errorf("VideoEdit: job=%+v err=%v", job, err)
	}
	job, err = s.VideoCreate(ctx, "xai", testProviderID, video.KindExtension, []byte(`{"model":"xai/grok-imagine-video","video":"https://x.test/v.mp4"}`))
	if err != nil || job.RequestID != "job-1" {
		t.Errorf("VideoExtension: job=%+v err=%v", job, err)
	}
}

func TestServiceImageEditFailsClosedForAll(t *testing.T) {
	s, srv := newTestService(t)
	defer srv.Close()
	ctx := context.Background()
	req := &images.EditRequest{Model: "m", Image: []byte("PNG"), ImageFileName: "i.png", Prompt: "p"}
	// No upstream image-edit authority exists (audit/01-http-contracts.md:545
	// lists only generations; serviceKinds carry no edit kind), so every
	// provider — proven or not — is rejected deterministically.
	for _, pt := range []provider.ProviderType{"openai", "fal-ai", "stability-ai", "xai", "unknown"} {
		_, err := s.ImageEdit(ctx, pt, testProviderID, req)
		if !media.IsCapabilityUnproven(err) {
			t.Errorf("ImageEdit(%s) err = %v, want typed capability-unproven error", pt, err)
		}
	}
}

func TestServiceEmptyBaseURLFailsClosed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	s := NewService(
		media.NewClient(),
		BaseURLResolverFunc(func(ctx context.Context, providerID string) (string, error) { return "", nil }),
		AccountResolverFunc(func(ctx context.Context, providerID string) (*provider.Account, error) { return testAccount(), nil }),
	)
	_, err := s.Embeddings(context.Background(), "openai", testProviderID, &embeddings.Request{Model: "m", Input: []byte(`"x"`)})
	if !errors.Is(err, media.ErrBaseURLUnconfigured) {
		t.Fatalf("err = %v, want ErrBaseURLUnconfigured (fail closed on empty base URL)", err)
	}
}

func TestServiceNilResolversFailsClosed(t *testing.T) {
	s := NewService(media.NewClient(), nil, nil)
	_, err := s.Embeddings(context.Background(), "openai", testProviderID, &embeddings.Request{Model: "m", Input: []byte(`"x"`)})
	if !errors.Is(err, media.ErrMalformedRequest) {
		t.Fatalf("err = %v, want ErrMalformedRequest", err)
	}
}

func TestServiceCredentialRedaction(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"denied Bearer sk-proj-SECRET42"}`))
	}))
	defer srv.Close()
	s := NewService(
		media.NewClient(media.WithTransport(srv.Client().Transport)),
		BaseURLResolverFunc(func(ctx context.Context, providerID string) (string, error) { return srv.URL, nil }),
		AccountResolverFunc(func(ctx context.Context, providerID string) (*provider.Account, error) { return testAccount(), nil }),
	)
	_, err := s.TTS(context.Background(), "elevenlabs", testProviderID, &audio.SpeechRequest{Model: "m", Input: "hi", Voice: "v"})
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "sk-proj-SECRET42") {
		t.Errorf("credential leaked through service: %v", err)
	}
	var ae *shared.AppError
	if errors.As(err, &ae) {
		if details, ok := ae.Details.(map[string]any); ok {
			if body, ok := details["body"].(string); ok {
				if strings.Contains(body, "sk-proj-SECRET42") {
					t.Errorf("credential leaked in details: %q", body)
				}
				if !strings.Contains(body, "Bearer ***") {
					t.Errorf("expected redaction marker in details: %q", body)
				}
			}
		}
	}
}
