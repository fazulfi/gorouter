package images

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"

	"gorouter/internal/domain/provider"
	"gorouter/internal/engine/media"
)

// GenerationEndpoint is the OpenAI-compatible image generation path, proven
// by the endpoint detector (internal/engine/formats/detect.go:33 maps
// /v1/images → FormatOpenAICompat).
const GenerationEndpoint = "/v1/images/generations"

// EditEndpoint is the OpenAI-compatible image edit path. No upstream route
// authority exists for image edits (audit/01-http-contracts.md lists only
// generation at :545); the executor exists but callers must gate it through
// media.Supports, which fails closed for every provider until a citation
// proves edit support.
const EditEndpoint = "/v1/images/edits"

// ChatEndpoint is the OpenAI-compatible chat path used for image-to-text
// requests (internal/engine/formats/detect.go:30 maps /v1/chat/completions →
// FormatOpenAIChat).
const ChatEndpoint = "/v1/chat/completions"

// Executor executes image generation, edit, and image-to-text requests.
type Executor struct {
	client       *media.Client
	genEndpoint  string
	editEndpoint string
	chatEndpoint string
	maxResponse  int64
	ceilingAuth  string
	maxUpload    int64
	uploadAuth   string
}

// Option configures an Executor.
type Option func(*Executor)

// WithResponseCeiling bounds upstream response bodies.
func WithResponseCeiling(max int64, authority string) Option {
	return func(e *Executor) { e.maxResponse, e.ceilingAuth = max, authority }
}

// WithUploadCeiling bounds multipart edit uploads.
func WithUploadCeiling(max int64, authority string) Option {
	return func(e *Executor) { e.maxUpload, e.uploadAuth = max, authority }
}

// NewExecutor creates an image executor with default endpoints and the
// platform body ceiling (media.BodyCeiling).
func NewExecutor(client *media.Client, opts ...Option) *Executor {
	e := &Executor{
		client:       client,
		genEndpoint:  GenerationEndpoint,
		editEndpoint: EditEndpoint,
		chatEndpoint: ChatEndpoint,
		maxResponse:  media.BodyCeiling,
		ceilingAuth:  media.BodyCeilingAuthority,
		maxUpload:    media.BodyCeiling,
		uploadAuth:   media.BodyCeilingAuthority,
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// Generate runs an image generation request.
func (e *Executor) Generate(ctx context.Context, req *GenerationRequest, account *provider.Account) (*GenerationResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("images: %w: nil request", media.ErrMalformedRequest)
	}
	body, err := EncodeGenerationRequest(req)
	if err != nil {
		return nil, err
	}
	data, err := e.roundTrip(ctx, http.MethodPost, e.genEndpoint, bytes.NewReader(body), "application/json", account)
	if err != nil {
		return nil, err
	}
	return DecodeGenerationResponse(data)
}

// Edit runs an image edit request as multipart/form-data. The audio/image
// binary upload streams through a bounded multipart encoder; exceeding the
// upload ceiling aborts with a typed 413-class error.
func (e *Executor) Edit(ctx context.Context, req *EditRequest, account *provider.Account) (*GenerationResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("images: %w: nil request", media.ErrMalformedRequest)
	}
	reader, contentType, err := encodeEditMultipart(req, e.maxUpload, e.uploadAuth)
	if err != nil {
		return nil, err
	}
	data, err := e.roundTrip(ctx, http.MethodPost, e.editEndpoint, reader, contentType, account)
	if err != nil {
		return nil, err
	}
	return DecodeGenerationResponse(data)
}

// ImageToText runs an image-to-text request against the chat endpoint. The
// request is validated through the codec first (image parts required), so
// invalid bodies fail closed before any upstream traffic.
func (e *Executor) ImageToText(ctx context.Context, req *ImageToTextRequest, account *provider.Account) ([]byte, error) {
	if req == nil {
		return nil, fmt.Errorf("images: %w: nil request", media.ErrMalformedRequest)
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("images: %w", media.ErrMalformedRequest)
	}
	if _, err := DecodeImageToTextRequest(body); err != nil {
		return nil, err
	}
	return e.roundTrip(ctx, http.MethodPost, e.chatEndpoint, bytes.NewReader(body), "application/json", account)
}

func (e *Executor) roundTrip(ctx context.Context, method, path string, body io.Reader, contentType string, account *provider.Account) ([]byte, error) {
	httpReq, err := e.client.BuildRequest(ctx, method, path, body, contentType, account)
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
	return media.ReadLimited(resp.Body, e.maxResponse, e.ceilingAuth)
}

// encodeEditMultipart builds a bounded multipart body with the JSON fields
// and the image file part. The returned reader fails with a typed 413-class
// error when the ceiling is exceeded mid-stream.
func encodeEditMultipart(req *EditRequest, max int64, authority string) (io.Reader, string, error) {
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
		writeField("prompt", req.Prompt)
		if req.Size != "" {
			writeField("size", req.Size)
		}
		if req.ResponseFormat != "" {
			writeField("response_format", req.ResponseFormat)
		}
		if req.N != nil {
			writeField("n", fmt.Sprintf("%d", *req.N))
		}
		if werr == nil {
			fw, ferr := mw.CreateFormFile("image", req.ImageFileName)
			if ferr != nil {
				werr = ferr
			} else {
				_, werr = io.Copy(fw, bytes.NewReader(req.Image))
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
