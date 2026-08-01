package images

import (
	"bytes"
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

func TestDecodeGenerationRequest(t *testing.T) {
	req, err := DecodeGenerationRequest([]byte(`{"model":"dall-e-3","prompt":"a cat","size":"1024x1024"}`))
	if err != nil {
		t.Fatal(err)
	}
	if req.Prompt != "a cat" || req.Size != "1024x1024" {
		t.Errorf("req = %+v", req)
	}
}

func TestDecodeGenerationRequestErrors(t *testing.T) {
	for _, body := range [][]byte{nil, []byte(`bad`), []byte(`{}`), []byte(`{"prompt":""}`)} {
		if _, err := DecodeGenerationRequest(body); !errors.Is(err, media.ErrMalformedRequest) {
			t.Errorf("DecodeGenerationRequest(%q) err = %v", body, err)
		}
	}
}

func TestDecodeGenerationResponse(t *testing.T) {
	resp, err := DecodeGenerationResponse([]byte(`{"created":1,"data":[{"url":"https://x.test/i.png"},{"b64_json":"AAAA"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Data) != 2 || resp.Data[0].URL != "https://x.test/i.png" || resp.Data[1].B64JSON != "AAAA" {
		t.Errorf("resp = %+v", resp)
	}
}

func TestDecodeImageToTextRequest(t *testing.T) {
	body := []byte(`{
		"model":"gpt-4o",
		"messages":[
			{"role":"user","content":[
				{"type":"text","text":"what is this?"},
				{"type":"image_url","image_url":{"url":"data:image/png;base64,iVBORw0KGgo="}}
			]}
		]}`)
	req, err := DecodeImageToTextRequest(body)
	if err != nil {
		t.Fatal(err)
	}
	if req.Model != "gpt-4o" || len(req.Messages) != 1 {
		t.Errorf("req = %+v", req)
	}
}

func TestDecodeImageToTextHTTPSURL(t *testing.T) {
	body := []byte(`{"model":"m","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://example.test/i.png"}}]}]}`)
	if _, err := DecodeImageToTextRequest(body); err != nil {
		t.Fatalf("https image_url rejected: %v", err)
	}
}

func TestDecodeImageToTextRequestErrors(t *testing.T) {
	cases := [][]byte{
		nil,
		[]byte(`bad`),
		[]byte(`{}`),
		[]byte(`{"model":"m","messages":[]}`),
		[]byte(`{"model":"m","messages":[{"role":"user"}]}`),
		[]byte(`{"model":"m","messages":[{"role":"user","content":[{"type":"text","text":"no image"}]}]}`),
		[]byte(`{"model":"m","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"ftp://x.test/i.png"}}]}]}`),
		[]byte(`{"model":"m","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/png;base64,"}}]}]}`),
	}
	for _, body := range cases {
		if _, err := DecodeImageToTextRequest(body); !errors.Is(err, media.ErrMalformedRequest) {
			t.Errorf("DecodeImageToTextRequest(%q) err = %v, want ErrMalformedRequest", body, err)
		}
	}
}

func TestGenerateSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/images/generations" {
			t.Errorf("path = %q", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"created":1,"data":[{"url":"https://x.test/i.png"}]}`))
	}))
	defer srv.Close()
	client := media.NewClient(media.WithBaseURL(srv.URL))
	resp, err := NewExecutor(client).Generate(context.Background(), &GenerationRequest{Prompt: "cat"}, testAccount())
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Data) != 1 {
		t.Errorf("resp = %+v", resp)
	}
}

func TestGenerateAuthRedaction(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"forbidden sk-ant-LEAK"}`))
	}))
	defer srv.Close()
	client := media.NewClient(media.WithBaseURL(srv.URL))
	_, err := NewExecutor(client).Generate(context.Background(), &GenerationRequest{Prompt: "cat"}, testAccount())
	var ae *shared.AppError
	if !errors.As(err, &ae) || ae.Code != shared.ErrUnauthorized {
		t.Fatalf("err = %v, want ErrUnauthorized (401/403 definitive auth class, retry errors.go)", err)
	}
	if strings.Contains(err.Error(), "sk-ant-LEAK") {
		t.Errorf("credential leaked: %v", err)
	}
}

func TestGenerateResponseCeiling(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", 4096)))
	}))
	defer srv.Close()
	client := media.NewClient(media.WithBaseURL(srv.URL))
	ex := NewExecutor(client, WithResponseCeiling(1024, "test-authority"))
	_, err := ex.Generate(context.Background(), &GenerationRequest{Prompt: "cat"}, testAccount())
	if !media.IsPayloadTooLarge(err) {
		t.Fatalf("err = %v, want typed 413-class error", err)
	}
}

func TestEditMultipartUpload(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/images/edits" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "multipart/form-data; boundary=") {
			t.Errorf("Content-Type = %q, want multipart", ct)
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("ParseMultipartForm err = %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if got := r.FormValue("prompt"); got != "make it blue" {
			t.Errorf("prompt field = %q", got)
		}
		if got := r.FormValue("model"); got != "gpt-image-1" {
			t.Errorf("model field = %q", got)
		}
		file, _, err := r.FormFile("image")
		if err != nil {
			t.Errorf("FormFile err = %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		defer file.Close()
		_, _ = w.Write([]byte(`{"created":1,"data":[{"url":"https://x.test/e.png"}]}`))
	}))
	defer srv.Close()
	client := media.NewClient(media.WithBaseURL(srv.URL))
	req := &EditRequest{
		Model:         "gpt-image-1",
		Image:         []byte("PNGDATA"),
		ImageFileName: "in.png",
		Prompt:        "make it blue",
	}
	resp, err := NewExecutor(client).Edit(context.Background(), req, testAccount())
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Data) != 1 {
		t.Errorf("resp = %+v", resp)
	}
}

func TestEditUploadCeiling(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"created":1,"data":[]}`))
	}))
	defer srv.Close()
	client := media.NewClient(media.WithBaseURL(srv.URL))
	req := &EditRequest{
		Model:         "m",
		Image:         bytes.Repeat([]byte("x"), 4096),
		ImageFileName: "in.png",
		Prompt:        "p",
	}
	ex := NewExecutor(client, WithUploadCeiling(1024, "test-authority"))
	_, err := ex.Edit(context.Background(), req, testAccount())
	if !media.IsPayloadTooLarge(err) {
		t.Fatalf("err = %v, want typed 413-class error (upload ceiling)", err)
	}
}

func TestImageToTextRoundTrip(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("path = %q", r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("body decode err = %v", err)
		}
		if body["model"] != "gpt-4o" {
			t.Errorf("model = %v", body["model"])
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"a cat"}}]}`))
	}))
	defer srv.Close()
	client := media.NewClient(media.WithBaseURL(srv.URL))
	req := &ImageToTextRequest{
		Model: "gpt-4o",
		Messages: []Message{{
			Role:    "user",
			Content: json.RawMessage(`[{"type":"image_url","image_url":{"url":"data:image/png;base64,AAAA"}}]`),
		}},
	}
	data, err := NewExecutor(client).ImageToText(context.Background(), req, testAccount())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "a cat") {
		t.Errorf("data = %s", data)
	}
}

func TestImageToTextRejectsWithoutImage(t *testing.T) {
	client := media.NewClient(media.WithBaseURL("https://example.test"))
	req := &ImageToTextRequest{Model: "m", Messages: []Message{{Role: "user", Content: json.RawMessage(`"text only"`)}}}
	_, err := NewExecutor(client).ImageToText(context.Background(), req, testAccount())
	if !errors.Is(err, media.ErrMalformedRequest) {
		t.Fatalf("err = %v, want ErrMalformedRequest", err)
	}
}

func TestExecuteCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()
	client := media.NewClient(media.WithBaseURL(srv.URL))
	_, err := NewExecutor(client).Generate(ctx, &GenerationRequest{Prompt: "cat"}, testAccount())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

// FuzzDecodeRequest fuzzes the image codec parsers (P3-T12 parser fuzz
// target; must never panic).
func FuzzDecodeRequest(f *testing.F) {
	f.Add([]byte(`{"model":"dall-e-3","prompt":"cat"}`))
	f.Add([]byte(`{"model":"m","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/png;base64,AA"}}]}]}`))
	f.Add([]byte(`{}`))
	f.Add([]byte(`bad`))
	f.Fuzz(func(t *testing.T, body []byte) {
		_, _ = DecodeGenerationRequest(body)
		_, _ = DecodeEditRequest(body)
		_, _ = DecodeImageToTextRequest(body)
		_, _ = DecodeGenerationResponse(body)
	})
}
