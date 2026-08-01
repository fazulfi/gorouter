package audio

import (
	"bytes"
	"context"
	"errors"
	"io"
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

func TestDecodeSpeechRequest(t *testing.T) {
	req, err := DecodeSpeechRequest([]byte(`{"model":"tts-1","input":"hello","voice":"alloy"}`))
	if err != nil {
		t.Fatal(err)
	}
	if req.Model != "tts-1" || req.Input != "hello" || req.Voice != "alloy" {
		t.Errorf("req = %+v", req)
	}
}

func TestDecodeSpeechRequestErrors(t *testing.T) {
	for _, body := range [][]byte{nil, []byte(`bad`), []byte(`{}`), []byte(`{"model":"m"}`), []byte(`{"model":"m","input":"x"}`), []byte(`{"model":"m","voice":"v"}`)} {
		if _, err := DecodeSpeechRequest(body); !errors.Is(err, media.ErrMalformedRequest) {
			t.Errorf("DecodeSpeechRequest(%q) err = %v", body, err)
		}
	}
}

func TestDecodeTranscribeResponse(t *testing.T) {
	resp, err := DecodeTranscribeResponse([]byte(`{"text":"hello world"}`))
	if err != nil {
		t.Fatal(err)
	}
	if resp.Text != "hello world" {
		t.Errorf("text = %q", resp.Text)
	}
	if _, err := DecodeTranscribeResponse([]byte(`bad`)); !errors.Is(err, media.ErrMalformedRequest) {
		t.Errorf("bad body err = %v", err)
	}
}

func TestDecodeVoicesResponse(t *testing.T) {
	resp, err := DecodeVoicesResponse([]byte(`{"object":"list","data":[{"voice_id":"21m00Tcm4TlvDq8ikWAM"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if resp.Object != "list" || len(resp.Data) != 1 || resp.Data[0].VoiceID == "" {
		t.Errorf("resp = %+v", resp)
	}
	if _, err := DecodeVoicesResponse(nil); !errors.Is(err, media.ErrMalformedRequest) {
		t.Errorf("nil body err = %v", err)
	}
}

func TestSpeechBinaryStream(t *testing.T) {
	audio := bytes.Repeat([]byte{0x1F, 0x8B}, 512)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/audio/speech" {
			t.Errorf("path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "audio/mpeg")
		_, _ = w.Write(audio)
	}))
	defer srv.Close()
	client := media.NewClient(media.WithBaseURL(srv.URL))
	resp, err := NewExecutor(client).Speech(context.Background(), &SpeechRequest{Model: "tts-1", Input: "hi", Voice: "alloy"}, testAccount())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(resp.Audio, audio) {
		t.Errorf("audio mismatch: got %d bytes, want %d", len(resp.Audio), len(audio))
	}
	if resp.MediaType != "audio/mpeg" {
		t.Errorf("MediaType = %q", resp.MediaType)
	}
}

func TestSpeechResponseCeiling(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(bytes.Repeat([]byte("x"), 4096))
	}))
	defer srv.Close()
	client := media.NewClient(media.WithBaseURL(srv.URL))
	ex := NewExecutor(client, WithResponseCeiling(1024, "test-authority"))
	_, err := ex.Speech(context.Background(), &SpeechRequest{Model: "m", Input: "hi", Voice: "v"}, testAccount())
	if !media.IsPayloadTooLarge(err) {
		t.Fatalf("err = %v, want typed 413-class error", err)
	}
}

func TestTranscribeMultipart(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/audio/transcriptions" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "multipart/form-data; boundary=") {
			t.Errorf("Content-Type = %q", ct)
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("ParseMultipartForm err = %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if got := r.FormValue("model"); got != "whisper-1" {
			t.Errorf("model = %q", got)
		}
		file, _, err := r.FormFile("file")
		if err != nil {
			t.Errorf("FormFile err = %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		defer file.Close()
		data, _ := io.ReadAll(file)
		if string(data) != "AUDIODATA" {
			t.Errorf("uploaded audio = %q", data)
		}
		_, _ = w.Write([]byte(`{"text":"transcribed"}`))
	}))
	defer srv.Close()
	client := media.NewClient(media.WithBaseURL(srv.URL))
	req := &TranscribeRequest{
		Model:    "whisper-1",
		FileName: "clip.mp3",
		Audio:    strings.NewReader("AUDIODATA"),
	}
	resp, err := NewExecutor(client).Transcribe(context.Background(), req, testAccount())
	if err != nil {
		t.Fatal(err)
	}
	if resp.Text != "transcribed" {
		t.Errorf("text = %q", resp.Text)
	}
}

func TestTranscribeUploadCeiling(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		_, _ = w.Write([]byte(`{"text":""}`))
	}))
	defer srv.Close()
	client := media.NewClient(media.WithBaseURL(srv.URL))
	req := &TranscribeRequest{
		Model:    "m",
		FileName: "big.mp3",
		Audio:    strings.NewReader(strings.Repeat("a", 8192)),
	}
	ex := NewExecutor(client, WithUploadCeiling(1024, "test-authority"))
	_, err := ex.Transcribe(context.Background(), req, testAccount())
	if !media.IsPayloadTooLarge(err) {
		t.Fatalf("err = %v, want typed 413-class error (upload ceiling)", err)
	}
}

func TestTranscribeValidation(t *testing.T) {
	client := media.NewClient(media.WithBaseURL("https://example.test"))
	ex := NewExecutor(client)
	_, err := ex.Transcribe(context.Background(), &TranscribeRequest{}, testAccount())
	if !errors.Is(err, media.ErrMalformedRequest) {
		t.Fatalf("err = %v, want ErrMalformedRequest", err)
	}
}

func TestListVoicesQuery(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/audio/voices" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if got := r.URL.Query().Get("provider"); got != "elevenlabs" {
			t.Errorf("provider query = %q", got)
		}
		if got := r.URL.Query().Get("lang"); got != "en" {
			t.Errorf("lang query = %q", got)
		}
		_, _ = w.Write([]byte(`{"object":"list","data":[{"voice_id":"v1","name":"Voice One"}]}`))
	}))
	defer srv.Close()
	client := media.NewClient(media.WithBaseURL(srv.URL))
	resp, err := NewExecutor(client).ListVoices(context.Background(), "elevenlabs", "en", testAccount())
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Data) != 1 || resp.Data[0].Name != "Voice One" {
		t.Errorf("resp = %+v", resp)
	}
}

func TestAudioErrorClassificationAndRedaction(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":"rate limited api-key=SECRET123"}`))
	}))
	defer srv.Close()
	client := media.NewClient(media.WithBaseURL(srv.URL))
	_, err := NewExecutor(client).Speech(context.Background(), &SpeechRequest{Model: "m", Input: "hi", Voice: "v"}, testAccount())
	var ae *shared.AppError
	if !errors.As(err, &ae) || ae.Code != shared.ErrRateLimited {
		t.Fatalf("err = %v, want ErrRateLimited", err)
	}
	if strings.Contains(err.Error(), "SECRET123") {
		t.Errorf("credential leaked in message: %v", err)
	}
	details := ae.Details.(map[string]any)
	body := details["body"].(string)
	if strings.Contains(body, "SECRET123") {
		t.Errorf("credential leaked in details: %q", body)
	}
	if !strings.Contains(body, "api-key=[REDACTED]") {
		t.Errorf("expected redaction marker in details: %q", body)
	}
}

func TestAudioCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()
	client := media.NewClient(media.WithBaseURL(srv.URL))
	_, err := NewExecutor(client).Speech(ctx, &SpeechRequest{Model: "m", Input: "hi", Voice: "v"}, testAccount())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestMediaTypeFromResponseFormat(t *testing.T) {
	cases := map[string]string{"": "audio/mpeg", "mp3": "audio/mpeg", "json": "application/json", "wav": "audio/wav", "opus": "audio/opus"}
	for in, want := range cases {
		if got := MediaTypeFromResponseFormat(in); got != want {
			t.Errorf("MediaTypeFromResponseFormat(%q) = %q, want %q", in, got, want)
		}
	}
}

// FuzzDecodeRequest fuzzes the audio codec parsers (P3-T12 parser fuzz
// target; must never panic).
func FuzzDecodeRequest(f *testing.F) {
	f.Add([]byte(`{"model":"tts-1","input":"hi","voice":"alloy"}`))
	f.Add([]byte(`{"text":"x"}`))
	f.Add([]byte(`{"object":"list","data":[]}`))
	f.Add([]byte(`bad`))
	f.Fuzz(func(t *testing.T, body []byte) {
		_, _ = DecodeSpeechRequest(body)
		_, _ = DecodeTranscribeResponse(body)
		_, _ = DecodeVoicesResponse(body)
	})
}
