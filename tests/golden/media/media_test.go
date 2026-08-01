package golden_media

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"gorouter/internal/engine/media"
	"gorouter/internal/engine/media/audio"
	"gorouter/internal/engine/media/embeddings"
	"gorouter/internal/engine/media/images"
	"gorouter/internal/engine/media/video"
	"gorouter/internal/engine/media/web"
)

func fixtureDir() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file))
}

func readGolden(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(fixtureDir(), name))
	if err != nil {
		t.Fatalf("read golden %s: %v", name, err)
	}
	return data
}

func TestAllMediaGoldens_ValidJSON(t *testing.T) {
	for _, name := range []string{
		"embeddings_request.json", "embeddings_response.json", "embeddings_error.json",
		"image_generation_request.json", "image_generation_response.json", "image_error.json",
		"image_to_text_request.json",
		"tts_request.json",
		"stt_response.json", "stt_error.json",
		"voices_response.json",
		"search_request.json", "search_response.json",
		"fetch_request.json", "fetch_response.json",
		"video_generation_request.json", "video_job_response.json", "video_status_response.json", "video_error.json",
	} {
		if !json.Valid(readGolden(t, name)) {
			t.Errorf("%s is not valid JSON", name)
		}
	}
}

func TestEmbeddingsGoldenRoundTrip(t *testing.T) {
	req, err := embeddings.DecodeRequest(readGolden(t, "embeddings_request.json"))
	if err != nil {
		t.Fatal(err)
	}
	if req.Model != "text-embedding-3-small" || string(req.Input) != `"hello world"` {
		t.Errorf("req = %+v", req)
	}
	resp, err := embeddings.DecodeResponse(readGolden(t, "embeddings_response.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Data) != 1 || resp.Data[0].Embedding[2] != 0.0789 {
		t.Errorf("resp = %+v", resp)
	}
	re, err := embeddings.EncodeResponse(resp)
	if err != nil || !json.Valid(re) {
		t.Errorf("encode = %s, err = %v", re, err)
	}
}

func TestImageGenerationGoldenRoundTrip(t *testing.T) {
	req, err := images.DecodeGenerationRequest(readGolden(t, "image_generation_request.json"))
	if err != nil {
		t.Fatal(err)
	}
	if req.Prompt == "" || req.Size != "1024x1024" || req.ResponseFormat != "b64_json" {
		t.Errorf("req = %+v", req)
	}
	resp, err := images.DecodeGenerationResponse(readGolden(t, "image_generation_response.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Data) != 1 || resp.Data[0].B64JSON == "" {
		t.Errorf("resp = %+v", resp)
	}
}

func TestImageToTextGolden(t *testing.T) {
	req, err := images.DecodeImageToTextRequest(readGolden(t, "image_to_text_request.json"))
	if err != nil {
		t.Fatal(err)
	}
	if req.Model != "gpt-4o" || len(req.Messages) != 1 {
		t.Errorf("req = %+v", req)
	}
}

func TestTTSSpeechGolden(t *testing.T) {
	req, err := audio.DecodeSpeechRequest(readGolden(t, "tts_request.json"))
	if err != nil {
		t.Fatal(err)
	}
	if req.Model != "tts-1" || req.Voice != "alloy" || req.ResponseFormat != "mp3" {
		t.Errorf("req = %+v", req)
	}
}

func TestSTTVoicesGoldens(t *testing.T) {
	resp, err := audio.DecodeTranscribeResponse(readGolden(t, "stt_response.json"))
	if err != nil {
		t.Fatal(err)
	}
	if resp.Text == "" {
		t.Errorf("resp = %+v", resp)
	}
	voices, err := audio.DecodeVoicesResponse(readGolden(t, "voices_response.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(voices.Data) != 1 || voices.Data[0].VoiceID == "" {
		t.Errorf("voices = %+v", voices)
	}
}

func TestWebGoldens(t *testing.T) {
	sreq, err := web.DecodeSearchRequest(readGolden(t, "search_request.json"))
	if err != nil {
		t.Fatal(err)
	}
	if sreq.Query == "" {
		t.Errorf("sreq = %+v", sreq)
	}
	sresp, err := web.DecodeSearchResponse(readGolden(t, "search_response.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(sresp.Results) != 1 || sresp.Results[0].Title == "" {
		t.Errorf("sresp = %+v", sresp)
	}
	freq, err := web.DecodeFetchRequest(readGolden(t, "fetch_request.json"))
	if err != nil {
		t.Fatal(err)
	}
	if freq.URL == "" {
		t.Errorf("freq = %+v", freq)
	}
	fresp, err := web.DecodeFetchResponse(readGolden(t, "fetch_response.json"))
	if err != nil {
		t.Fatal(err)
	}
	if fresp.Content == "" {
		t.Errorf("fresp = %+v", fresp)
	}
}

func TestVideoGoldens(t *testing.T) {
	req, err := video.DecodeCreateRequest(readGolden(t, "video_generation_request.json"), video.KindGeneration)
	if err != nil {
		t.Fatal(err)
	}
	if req.Model != "xai/grok-imagine-video" {
		t.Errorf("req = %+v", req)
	}
	job, err := video.DecodeJob(readGolden(t, "video_job_response.json"))
	if err != nil {
		t.Fatal(err)
	}
	if job.RequestID == "" || job.Status != "PENDING" {
		t.Errorf("job = %+v", job)
	}
	done, err := video.DecodeJob(readGolden(t, "video_status_response.json"))
	if err != nil {
		t.Fatal(err)
	}
	if done.Status != "COMPLETED" || done.Video == nil || done.Video.URL == "" {
		t.Errorf("done = %+v", done)
	}
	if !video.IsTerminal(done.Status) {
		t.Errorf("IsTerminal(COMPLETED) = false")
	}
	if video.IsTerminal(job.Status) {
		t.Errorf("IsTerminal(PENDING) = true")
	}
}

func TestMediaErrorsAreTyped(t *testing.T) {
	// Error fixtures must decode as JSON (upstream error surfaces) and the
	// codec path must not misclassify them as payload-limit violations.
	for _, name := range []string{"embeddings_error.json", "image_error.json", "stt_error.json", "video_error.json"} {
		body := readGolden(t, name)
		if !json.Valid(body) {
			t.Errorf("%s invalid", name)
		}
		if err := media.NewUpstreamError(502, string(body)); err == nil {
			t.Errorf("%s: expected non-nil error", name)
		}
	}
	if err := media.NewPayloadTooLargeError(1024, "x"); !media.IsPayloadTooLarge(err) {
		t.Errorf("typed 413 error not classified")
	}
	if media.IsPayloadTooLarge(errors.New("plain")) {
		t.Errorf("plain error misclassified as payload too large")
	}
}
