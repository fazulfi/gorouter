package translators

import (
	"context"

	"gorouter/internal/domain/engine"
	"gorouter/internal/engine/formats"
)

type noopTranslator struct {
	fmt engine.RequestFormat
}

func (n *noopTranslator) Source() engine.RequestFormat { return n.fmt }
func (n *noopTranslator) Target() engine.RequestFormat { return n.fmt }

func (n *noopTranslator) TranslateRequest(_ context.Context, req *engine.Request) (*engine.Request, error) {
	if req == nil {
		return nil, ErrNilRequest
	}
	return req, nil
}

func (n *noopTranslator) TranslateResponse(_ context.Context, resp *engine.Response) (*engine.Response, error) {
	if resp == nil {
		return nil, ErrNilResponse
	}
	return resp, nil
}

func (n *noopTranslator) TranslateStreamChunk(_ context.Context, chunk *formats.Chunk) (*formats.Chunk, error) {
	if chunk == nil {
		return nil, ErrNilChunk
	}
	return chunk, nil
}
