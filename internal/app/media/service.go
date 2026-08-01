// Package media is the app-layer media use case: it performs the fail-closed
// capability check for every modality, resolves provider base URLs and
// accounts, and delegates to engine media executors constructed per call
// with the resolved base URL (mirrors the per-call executor pattern in
// internal/app/providers/compatible.go clientWithBaseURL). All errors are
// credential-redacted before surfacing (decisions #127, #145).
package media

import (
	"context"
	"fmt"

	"gorouter/internal/domain/provider"
	"gorouter/internal/engine/media"
	"gorouter/internal/engine/media/audio"
	"gorouter/internal/engine/media/embeddings"
	"gorouter/internal/engine/media/images"
	"gorouter/internal/engine/media/video"
	"gorouter/internal/engine/media/web"
)

// BaseURLResolver resolves a provider's base URL from its record
// (provider.BaseURL). A provider without a configured base URL fails closed
// (media.ErrBaseURLUnconfigured) — media never invents a default host.
type BaseURLResolver interface {
	ResolveBaseURL(ctx context.Context, providerID string) (string, error)
}

// AccountResolver resolves an account for a provider.
type AccountResolver interface {
	ResolveAccount(ctx context.Context, providerID string) (*provider.Account, error)
}

// BaseURLResolverFunc adapts a function to BaseURLResolver.
type BaseURLResolverFunc func(ctx context.Context, providerID string) (string, error)

// ResolveBaseURL implements BaseURLResolver.
func (f BaseURLResolverFunc) ResolveBaseURL(ctx context.Context, providerID string) (string, error) {
	return f(ctx, providerID)
}

// AccountResolverFunc adapts a function to AccountResolver.
type AccountResolverFunc func(ctx context.Context, providerID string) (*provider.Account, error)

// ResolveAccount implements AccountResolver.
func (f AccountResolverFunc) ResolveAccount(ctx context.Context, providerID string) (*provider.Account, error) {
	return f(ctx, providerID)
}

// Service executes media modality requests with deterministic fail-closed
// capability checks.
type Service struct {
	client   *media.Client
	baseURLs BaseURLResolver
	accounts AccountResolver
}

// NewService creates the media use case. baseURLs and accounts must be
// non-nil (wired from provider/account repositories by bootstrap).
func NewService(client *media.Client, baseURLs BaseURLResolver, accounts AccountResolver) *Service {
	return &Service{client: client, baseURLs: baseURLs, accounts: accounts}
}

// Supports reports whether a provider is proven for a modality (fail closed).
func (s *Service) Supports(m media.Modality, pt provider.ProviderType) bool {
	ok, _ := media.Supports(m, pt)
	return ok
}

// Embeddings runs an embeddings request.
func (s *Service) Embeddings(ctx context.Context, pt provider.ProviderType, providerID string, req *embeddings.Request) (*embeddings.Response, error) {
	client, account, err := s.resolve(ctx, media.ModalityEmbeddings, pt, providerID)
	if err != nil {
		return nil, err
	}
	resp, err := embeddings.NewExecutor(client).Execute(ctx, req, account)
	return resp, media.RedactError(err)
}

// ImageGeneration runs an image generation request.
func (s *Service) ImageGeneration(ctx context.Context, pt provider.ProviderType, providerID string, req *images.GenerationRequest) (*images.GenerationResponse, error) {
	client, account, err := s.resolve(ctx, media.ModalityImageGeneration, pt, providerID)
	if err != nil {
		return nil, err
	}
	resp, err := images.NewExecutor(client).Generate(ctx, req, account)
	return resp, media.RedactError(err)
}

// ImageEdit is defined but fails closed for every provider: no upstream
// image-edit route or service kind exists at the pinned commit
// (audit/01-http-contracts.md:545 lists only image generations; the registry
// serviceKinds carry no image-edit kind), so every request is rejected
// deterministically until an authority proves edit support.
func (s *Service) ImageEdit(ctx context.Context, pt provider.ProviderType, providerID string, req *images.EditRequest) (*images.GenerationResponse, error) {
	return nil, media.NewCapabilityUnprovenError(media.ModalityImageGeneration, pt,
		"no upstream image-edit authority (audit/01-http-contracts.md:545; registry serviceKinds have no edit kind)")
}

// ImageToText runs an image-to-text request.
func (s *Service) ImageToText(ctx context.Context, pt provider.ProviderType, providerID string, req *images.ImageToTextRequest) ([]byte, error) {
	client, account, err := s.resolve(ctx, media.ModalityImageToText, pt, providerID)
	if err != nil {
		return nil, err
	}
	data, err := images.NewExecutor(client).ImageToText(ctx, req, account)
	return data, media.RedactError(err)
}

// TTS synthesizes speech.
func (s *Service) TTS(ctx context.Context, pt provider.ProviderType, providerID string, req *audio.SpeechRequest) (*audio.SpeechResponse, error) {
	client, account, err := s.resolve(ctx, media.ModalityTTS, pt, providerID)
	if err != nil {
		return nil, err
	}
	resp, err := audio.NewExecutor(client).Speech(ctx, req, account)
	return resp, media.RedactError(err)
}

// STT transcribes audio.
func (s *Service) STT(ctx context.Context, pt provider.ProviderType, providerID string, req *audio.TranscribeRequest) (*audio.TranscribeResponse, error) {
	client, account, err := s.resolve(ctx, media.ModalitySTT, pt, providerID)
	if err != nil {
		return nil, err
	}
	resp, err := audio.NewExecutor(client).Transcribe(ctx, req, account)
	return resp, media.RedactError(err)
}

// Voices lists voices for a provider.
func (s *Service) Voices(ctx context.Context, pt provider.ProviderType, providerID, lang string) (*audio.VoicesResponse, error) {
	client, account, err := s.resolve(ctx, media.ModalityVoices, pt, providerID)
	if err != nil {
		return nil, err
	}
	resp, err := audio.NewExecutor(client).ListVoices(ctx, string(pt), lang, account)
	return resp, media.RedactError(err)
}

// WebSearch runs a web search.
func (s *Service) WebSearch(ctx context.Context, pt provider.ProviderType, providerID string, req *web.SearchRequest) (*web.SearchResponse, error) {
	client, account, err := s.resolve(ctx, media.ModalityWebSearch, pt, providerID)
	if err != nil {
		return nil, err
	}
	resp, err := web.NewExecutor(client).Search(ctx, req, pt, account)
	return resp, media.RedactError(err)
}

// WebFetch extracts content from a URL.
func (s *Service) WebFetch(ctx context.Context, pt provider.ProviderType, providerID string, req *web.FetchRequest) (*web.FetchResponse, error) {
	client, account, err := s.resolve(ctx, media.ModalityWebFetch, pt, providerID)
	if err != nil {
		return nil, err
	}
	resp, err := web.NewExecutor(client).Fetch(ctx, req, account)
	return resp, media.RedactError(err)
}

// VideoCreate submits an async video job.
func (s *Service) VideoCreate(ctx context.Context, pt provider.ProviderType, providerID string, kind video.Kind, body []byte) (*video.Job, error) {
	var m media.Modality
	switch kind {
	case video.KindGeneration:
		m = media.ModalityVideoGeneration
	case video.KindEdit:
		m = media.ModalityVideoEdit
	case video.KindExtension:
		m = media.ModalityVideoExtension
	default:
		return nil, fmt.Errorf("media: %w: unknown video kind %q", media.ErrMalformedRequest, kind)
	}
	client, account, err := s.resolve(ctx, m, pt, providerID)
	if err != nil {
		return nil, err
	}
	req, err := video.DecodeCreateRequest(body, kind)
	if err != nil {
		return nil, err
	}
	job, err := video.NewExecutor(client).Create(ctx, req, account)
	return job, media.RedactError(err)
}

// VideoStatus polls a video job once.
func (s *Service) VideoStatus(ctx context.Context, pt provider.ProviderType, providerID, requestID string) (*video.Job, error) {
	client, account, err := s.resolve(ctx, media.ModalityVideoStatus, pt, providerID)
	if err != nil {
		return nil, err
	}
	job, err := video.NewExecutor(client).Status(ctx, requestID, account)
	return job, media.RedactError(err)
}

// VideoWait polls a video job until terminal.
func (s *Service) VideoWait(ctx context.Context, pt provider.ProviderType, providerID, requestID string) (*video.Job, error) {
	client, account, err := s.resolve(ctx, media.ModalityVideoStatus, pt, providerID)
	if err != nil {
		return nil, err
	}
	job, err := video.NewExecutor(client).Wait(ctx, requestID, account)
	return job, media.RedactError(err)
}

// resolve performs the fail-closed capability gate, resolves the account,
// and returns a per-call client with the provider base URL. No shared state
// is mutated, so the service is safe for concurrent use.
func (s *Service) resolve(ctx context.Context, m media.Modality, pt provider.ProviderType, providerID string) (*media.Client, *provider.Account, error) {
	if s.baseURLs == nil || s.accounts == nil {
		return nil, nil, fmt.Errorf("media: %w: base URL and account resolvers required", media.ErrMalformedRequest)
	}
	ok, cap := media.Supports(m, pt)
	if !ok {
		return nil, nil, media.NewCapabilityUnprovenError(m, pt, cap.Authority)
	}
	account, err := s.accounts.ResolveAccount(ctx, providerID)
	if err != nil {
		return nil, nil, err
	}
	base, err := s.baseURLs.ResolveBaseURL(ctx, providerID)
	if err != nil {
		return nil, nil, err
	}
	if base == "" {
		return nil, nil, fmt.Errorf("media: %w: empty base URL for provider %q", media.ErrBaseURLUnconfigured, pt)
	}
	return s.client.WithBaseURL(base), account, nil
}
