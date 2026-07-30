package orchestrator

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"gorouter/internal/app/retry"
	"gorouter/internal/app/translate"
	"gorouter/internal/app/tx"
	"gorouter/internal/domain/engine"
	"gorouter/internal/domain/engine/stream"
	"gorouter/internal/domain/modelref"
	"gorouter/internal/domain/provider"
)

// Config controls top-level orchestrator behaviour.
type Config struct {
	DefaultMaxTokens int
	RequestTimeout   time.Duration
}

// DefaultConfig returns a Config populated with sensible defaults.
func DefaultConfig() Config {
	return Config{
		DefaultMaxTokens: 4096,
		RequestTimeout:   120 * time.Second,
	}
}

// CooldownRegistry is the subset of cooldown operations the orchestrator
// needs, defined here to avoid coupling to a specific cooldown implementation.
type CooldownRegistry interface {
	IsOnCooldown(ctx context.Context, accountID uuid.UUID) bool
	RecordFailure(ctx context.Context, accountID uuid.UUID, err error)
	RecordSuccess(ctx context.Context, accountID uuid.UUID)
}

// Orchestrator implements engine.Orchestrator by coordinating the full
// request lifecycle: model resolution, format detection, translation,
// executor dispatch, retry/fallback, and stream lifecycle.
type Orchestrator struct {
	config       Config
	resolver     modelref.Resolver
	translateSvc *translate.Service
	execFactory  engine.ExecutorFactory
	accountSel   provider.AccountSelector
	accountRepo  provider.AccountRepository
	cooldown     CooldownRegistry
	retryCfg     retry.Config
	txManager    *tx.TransactionManager
	logger       zerolog.Logger

	mu      sync.RWMutex
	streams map[uuid.UUID]*stream.Stream
}

// New creates an Orchestrator with the given dependencies. All parameters
// are injected; the orchestrator uses no global state or service locator.
func New(
	config Config,
	resolver modelref.Resolver,
	translateSvc *translate.Service,
	execFactory engine.ExecutorFactory,
	accountSel provider.AccountSelector,
	accountRepo provider.AccountRepository,
	cooldown CooldownRegistry,
	retryCfg retry.Config,
	txManager *tx.TransactionManager,
	logger zerolog.Logger,
) *Orchestrator {
	if config == (Config{}) {
		config = DefaultConfig()
	}
	return &Orchestrator{
		config:       config,
		resolver:     resolver,
		translateSvc: translateSvc,
		execFactory:  execFactory,
		accountSel:   accountSel,
		accountRepo:  accountRepo,
		cooldown:     cooldown,
		retryCfg:     retryCfg,
		txManager:    txManager,
		logger:       logger.With().Str("component", "orchestrator").Logger(),
		streams:      make(map[uuid.UUID]*stream.Stream),
	}
}

// ExecuteRequest processes a single request end-to-end: parse and resolve the
// model reference, detect the wire format, translate the body, select a
// provider account, obtain an executor, and dispatch the request with
// retry/fallback support. For streaming requests a Stream is registered for
// later cancellation.
func (o *Orchestrator) ExecuteRequest(ctx context.Context, req *engine.Request) (_ *engine.Response, err error) {
	log := o.logger.With().Str("request_id", req.ID.String()).Logger()
	log.Debug().Msg("orchestrator: beginning request execution")

	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, o.config.RequestTimeout)
		defer cancel()
	}

	resolved, err := o.resolveModelStage(ctx, req)
	if err != nil {
		return nil, err
	}
	log = log.With().
		Str("provider", resolved.Provider.Name).
		Str("model", resolved.ModelName).
		Logger()
	log.Debug().Msg("orchestrator: model resolved")

	detected, err := o.detectFormatStage(ctx, req)
	if err != nil {
		return nil, err
	}

	if err := o.translateStage(ctx, req, detected); err != nil {
		return nil, err
	}
	log.Debug().Str("format", string(req.Format)).Msg("orchestrator: request translated")

	account, err := o.selectAccountStage(ctx, resolved)
	if err != nil {
		return nil, err
	}
	log.Debug().Str("account_id", account.ID.String()).Msg("orchestrator: account selected")

	exec, err := o.getExecutorStage(resolved.Provider.Type, req.Format)
	if err != nil {
		return nil, err
	}
	log.Debug().Str("provider_type", string(resolved.Provider.Type)).Msg("orchestrator: executor obtained")

	if req.Stream {
		return o.dispatchStream(ctx, req, exec, account, log)
	}
	return o.dispatchWithRetry(ctx, req, exec, account, log)
}

// CancelStream terminates an active streaming request identified by its
// request UUID. It is safe to call multiple times.
func (o *Orchestrator) CancelStream(_ context.Context, requestID uuid.UUID) error {
	o.mu.Lock()
	s, ok := o.streams[requestID]
	if ok {
		delete(o.streams, requestID)
	}
	o.mu.Unlock()

	if !ok {
		return ErrStreamNotFound
	}
	o.logger.Info().Str("stream_id", s.ID().String()).Msg("orchestrator: cancelling stream")
	s.Cancel(fmt.Errorf("stream cancelled by orchestrator"))
	return nil
}

// dispatchWithRetry dispatches a non-streaming request through the retry
// wrapper, which handles backoff, cooldown recording, and account fallback.
func (o *Orchestrator) dispatchWithRetry(
	ctx context.Context,
	req *engine.Request,
	exec engine.Executor,
	account *provider.Account,
	log zerolog.Logger,
) (*engine.Response, error) {
	policy := retry.NewPolicy(o.retryCfg)
	fallbackSel := retry.NewFallbackSelector(o.accountSel, o.cooldown)
	retryExec := retry.NewExecutor(exec, policy, fallbackSel)

	resp, err := retryExec.Execute(ctx, req, account)
	if err != nil {
		log.Error().Err(err).Msg("orchestrator: request failed after retries")
		return nil, fmt.Errorf("orchestrator: execute failed: %w", err)
	}
	log.Debug().
		Int("status_code", resp.StatusCode).
		Msg("orchestrator: request completed")
	return resp, nil
}

// dispatchStream dispatches a streaming request. The stream is registered in
// the orchestrator's stream map so that CancelStream can terminate it. A
// goroutine cleans up the registration once the stream completes.
func (o *Orchestrator) dispatchStream(
	ctx context.Context,
	req *engine.Request,
	exec engine.Executor,
	account *provider.Account,
	log zerolog.Logger,
) (*engine.Response, error) {
	policy := retry.NewPolicy(o.retryCfg)
	fallbackSel := retry.NewFallbackSelector(o.accountSel, o.cooldown)
	retryExec := retry.NewExecutor(exec, policy, fallbackSel)

	resp, err := retryExec.ExecuteStream(ctx, req, account)
	if err != nil {
		log.Error().Err(err).Msg("orchestrator: stream execution failed")
		return nil, fmt.Errorf("orchestrator: stream execute failed: %w", err)
	}

	if resp.Stream != nil {
		if s, ok := resp.Stream.(*stream.Stream); ok {
			o.mu.Lock()
			o.streams[req.ID] = s
			o.mu.Unlock()

			go func() {
				// Poll until the stream reaches a terminal state.
				ticker := time.NewTicker(2 * time.Second)
				defer ticker.Stop()
				for range ticker.C {
					state := s.State()
					if state == stream.StateDone ||
						state == stream.StateErrored ||
						state == stream.StateCancelled {
						o.mu.Lock()
						delete(o.streams, req.ID)
						o.mu.Unlock()
						log.Debug().Msg("orchestrator: stream auto-cleaned")
						return
					}
				}
			}()
		}
	}

	log.Debug().Msg("orchestrator: stream started")
	return resp, nil
}

// mapTranslateFormat maps the translate package's format constants to the
// engine-level RequestFormat constants.
func mapTranslateFormat(detected engine.RequestFormat) engine.RequestFormat {
	return detected
}
