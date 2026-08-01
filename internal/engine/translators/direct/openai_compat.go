package direct

import (
	"context"

	"gorouter/internal/domain/engine"
	"gorouter/internal/engine/formats"
)

// OpenAIToCompat handles FormatOpenAIChat ↔ FormatOpenAICompat conversion.
// Since both formats share the same wire shape, this is a semantic passthrough
// that preserves the original body without transformation.
type OpenAIToCompat struct{}
type CompatToOpenAI struct{}

func NewOpenAIToCompat() *OpenAIToCompat { return &OpenAIToCompat{} }
func NewCompatToOpenAI() *CompatToOpenAI { return &CompatToOpenAI{} }

func (t *OpenAIToCompat) Source() engine.RequestFormat { return engine.FormatOpenAIChat }
func (t *OpenAIToCompat) Target() engine.RequestFormat { return engine.FormatOpenAICompat }
func (t *CompatToOpenAI) Source() engine.RequestFormat { return engine.FormatOpenAICompat }
func (t *CompatToOpenAI) Target() engine.RequestFormat { return engine.FormatOpenAIChat }

func (t *OpenAIToCompat) TranslateRequest(_ context.Context, req *engine.Request) (*engine.Request, error) {
	if req == nil {
		return nil, errNilRequest
	}
	out := *req
	out.Format = engine.FormatOpenAICompat
	out.RawBody = req.RawBody
	out.MappedBody = req.RawBody
	return &out, nil
}
func (t *OpenAIToCompat) TranslateResponse(_ context.Context, resp *engine.Response) (*engine.Response, error) {
	if resp == nil {
		return nil, errNilResponse
	}
	return resp, nil
}
func (t *OpenAIToCompat) TranslateStreamChunk(_ context.Context, chunk *formats.Chunk) (*formats.Chunk, error) {
	if chunk == nil {
		return nil, errNilChunk
	}
	return chunk, nil
}

func (t *CompatToOpenAI) TranslateRequest(_ context.Context, req *engine.Request) (*engine.Request, error) {
	if req == nil {
		return nil, errNilRequest
	}
	out := *req
	out.Format = engine.FormatOpenAIChat
	out.RawBody = req.RawBody
	out.MappedBody = req.RawBody
	return &out, nil
}
func (t *CompatToOpenAI) TranslateResponse(_ context.Context, resp *engine.Response) (*engine.Response, error) {
	if resp == nil {
		return nil, errNilResponse
	}
	return resp, nil
}
func (t *CompatToOpenAI) TranslateStreamChunk(_ context.Context, chunk *formats.Chunk) (*formats.Chunk, error) {
	if chunk == nil {
		return nil, errNilChunk
	}
	return chunk, nil
}
