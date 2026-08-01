package audio

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"gorouter/internal/domain/provider"
	"gorouter/internal/engine/media"
)

// SpeechEndpoint is the OpenAI-compatible TTS path, proven by the endpoint
// detector (internal/engine/formats/detect.go:34 maps /v1/audio →
// FormatOpenAICompat).
const SpeechEndpoint = "/v1/audio/speech"

// TranscriptionsEndpoint is the STT path (upstream
// src/app/api/v1/audio/transcriptions/route.js:16-18).
const TranscriptionsEndpoint = "/v1/audio/transcriptions"

// VoicesEndpoint is the OpenAI voice list path
// (audit/01-http-contracts.md:548; upstream
// src/app/api/v1/audio/voices/route.js:18-20).
const VoicesEndpoint = "/v1/audio/voices"

// Executor executes TTS, STT, and voices requests.
type Executor struct {
	client         *media.Client
	speechEndpoint string
	sttEndpoint    string
	voicesEndpoint string
	maxResponse    int64
	ceilingAuth    string
	maxUpload      int64
	uploadAuth     string
}

// Option configures an Executor.
type Option func(*Executor)

// WithResponseCeiling bounds upstream response bodies (including streamed
// TTS audio).
func WithResponseCeiling(max int64, authority string) Option {
	return func(e *Executor) { e.maxResponse, e.ceilingAuth = max, authority }
}

// WithUploadCeiling bounds STT multipart uploads.
func WithUploadCeiling(max int64, authority string) Option {
	return func(e *Executor) { e.maxUpload, e.uploadAuth = max, authority }
}

// NewExecutor creates an audio executor with default endpoints and the
// platform body ceiling (media.BodyCeiling).
func NewExecutor(client *media.Client, opts ...Option) *Executor {
	e := &Executor{
		client:         client,
		speechEndpoint: SpeechEndpoint,
		sttEndpoint:    TranscriptionsEndpoint,
		voicesEndpoint: VoicesEndpoint,
		maxResponse:    media.BodyCeiling,
		ceilingAuth:    media.BodyCeilingAuthority,
		maxUpload:      media.BodyCeiling,
		uploadAuth:     media.BodyCeilingAuthority,
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// Speech streams TTS audio from the upstream binary response
// (audit/01-http-contracts.md:546 "Binary audio stream"). The audio is
// buffered up to the response ceiling with a typed 413-class error when
// exceeded.
func (e *Executor) Speech(ctx context.Context, req *SpeechRequest, account *provider.Account) (*SpeechResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("audio: %w: nil request", media.ErrMalformedRequest)
	}
	body, err := EncodeSpeechRequest(req)
	if err != nil {
		return nil, err
	}
	httpReq, err := e.client.BuildRequest(ctx, http.MethodPost, e.speechEndpoint, bytes.NewReader(body), "application/json", account)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Accept", "audio/*")
	resp, err := e.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if err := classifyStatus(resp); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if _, err := media.CopyLimited(ctx, &buf, resp.Body, e.maxResponse, e.ceilingAuth); err != nil {
		return nil, err
	}
	mediaType := resp.Header.Get("Content-Type")
	if mediaType == "" {
		mediaType = "audio/mpeg"
	}
	return &SpeechResponse{Audio: buf.Bytes(), MediaType: mediaType}, nil
}

// Transcribe uploads audio via bounded multipart/form-data and returns the
// JSON text result (audit/01-http-contracts.md:547). The upload streams
// through an io.Pipe; exceeding the upload ceiling aborts with a typed
// 413-class error without buffering the whole file.
func (e *Executor) Transcribe(ctx context.Context, req *TranscribeRequest, account *provider.Account) (*TranscribeResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("audio: %w: nil request", media.ErrMalformedRequest)
	}
	if req.Model == "" || req.FileName == "" || req.Audio == nil {
		return nil, fmt.Errorf("audio: %w: model, file name and audio required", media.ErrMalformedRequest)
	}
	reader, contentType, err := encodeTranscribeMultipart(req, e.maxUpload, e.uploadAuth)
	if err != nil {
		return nil, err
	}
	httpReq, err := e.client.BuildRequest(ctx, http.MethodPost, e.sttEndpoint, reader, contentType, account)
	if err != nil {
		return nil, err
	}
	resp, err := e.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if err := classifyStatus(resp); err != nil {
		return nil, err
	}
	data, err := media.ReadLimited(resp.Body, e.maxResponse, e.ceilingAuth)
	if err != nil {
		return nil, err
	}
	return DecodeTranscribeResponse(data)
}

// ListVoices returns the OpenAI voice list for a provider
// (audit/01-http-contracts.md:548, query ?provider=&lang=).
func (e *Executor) ListVoices(ctx context.Context, p, lang string, account *provider.Account) (*VoicesResponse, error) {
	path := e.voicesEndpoint
	q := url.Values{}
	if p != "" {
		q.Set("provider", p)
	}
	if lang != "" {
		q.Set("lang", lang)
	}
	if len(q) > 0 {
		path += "?" + q.Encode()
	}
	httpReq, err := e.client.BuildRequest(ctx, http.MethodGet, path, nil, "", account)
	if err != nil {
		return nil, err
	}
	resp, err := e.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if err := classifyStatus(resp); err != nil {
		return nil, err
	}
	data, err := media.ReadLimited(resp.Body, e.maxResponse, e.ceilingAuth)
	if err != nil {
		return nil, err
	}
	return DecodeVoicesResponse(data)
}

// encodeTranscribeMultipart streams a bounded multipart body with the file
// part. The returned reader fails with a typed 413-class error when the
// ceiling is exceeded mid-stream.
func encodeTranscribeMultipart(req *TranscribeRequest, max int64, authority string) (io.Reader, string, error) {
	pr, pw := io.Pipe()
	mw := multipart.NewWriter(&cappedWriter{w: pw, max: max, authority: authority})
	go func() {
		var werr error
		writeField := func(field, value string) {
			if werr == nil {
				werr = mw.WriteField(field, value)
			}
		}
		writeField("model", req.Model)
		if req.Language != "" {
			writeField("language", req.Language)
		}
		if req.Prompt != "" {
			writeField("prompt", req.Prompt)
		}
		if req.ResponseFormat != "" {
			writeField("response_format", req.ResponseFormat)
		}
		if req.Temperature != nil {
			writeField("temperature", strconv.FormatFloat(*req.Temperature, 'f', -1, 64))
		}
		if werr == nil {
			fw, ferr := mw.CreateFormFile("file", req.FileName)
			if ferr != nil {
				werr = ferr
			} else {
				_, werr = io.Copy(fw, io.LimitReader(req.Audio, max+1))
			}
		}
		if werr == nil {
			werr = mw.Close()
		} else {
			_ = mw.Close()
		}
		_ = pw.CloseWithError(werr)
	}()
	return pr, mw.FormDataContentType(), nil
}

type cappedWriter struct {
	w         io.Writer
	n         int64
	max       int64
	authority string
}

func (cw *cappedWriter) Write(p []byte) (int, error) {
	if cw.n+int64(len(p)) > cw.max {
		return 0, media.NewPayloadTooLargeError(cw.max, cw.authority)
	}
	nn, err := cw.w.Write(p)
	cw.n += int64(nn)
	return nn, err
}

func classifyStatus(resp *http.Response) error {
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	msg := media.RedactErrorString(string(body))
	switch resp.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return media.NewAuthError(resp.StatusCode, msg)
	case http.StatusTooManyRequests:
		return media.NewRateLimitError(resp.StatusCode, msg)
	default:
		return media.NewUpstreamError(resp.StatusCode, msg)
	}
}

// MediaTypeFromResponseFormat maps the TTS response_format query value to a
// media type (upstream src/sse/handlers/tts.js:31 defaults to mp3).
func MediaTypeFromResponseFormat(format string) string {
	switch strings.ToLower(format) {
	case "json":
		return "application/json"
	case "wav":
		return "audio/wav"
	case "opus":
		return "audio/opus"
	default:
		return "audio/mpeg"
	}
}
