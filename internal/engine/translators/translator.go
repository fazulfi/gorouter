package translators

import (
	"context"
	"gorouter/internal/domain/engine"
	"gorouter/internal/engine/formats"
)

type Translator interface {
	Source() engine.RequestFormat
	Target() engine.RequestFormat
	TranslateRequest(ctx context.Context, req *engine.Request) (*engine.Request, error)
	TranslateResponse(ctx context.Context, resp *engine.Response) (*engine.Response, error)
	TranslateStreamChunk(ctx context.Context, chunk *formats.Chunk) (*formats.Chunk, error)
}
