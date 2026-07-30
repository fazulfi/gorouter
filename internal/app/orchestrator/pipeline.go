package orchestrator

import (
	"context"
	"fmt"

	"gorouter/internal/domain/engine"
	"gorouter/internal/domain/modelref"
	"gorouter/internal/domain/provider"
)

// PipelineError is an intermediate error that carries both the stage name
// and the underlying error. It is not exported; callers see the wrapped
// AppError sentinel from errors.go.
type pipelineError struct {
	Stage string
	Err   error
}

func (e *pipelineError) Error() string {
	return fmt.Sprintf("orchestrator: pipeline stage %q failed: %v", e.Stage, e.Err)
}

func (e *pipelineError) Unwrap() error {
	return e.Err
}

// resolveModelStage parses the model string from the request and resolves it
// against the configured provider registry.
func (o *Orchestrator) resolveModelStage(ctx context.Context, req *engine.Request) (*modelref.ResolvedRef, error) {
	if o.resolver == nil {
		return nil, &pipelineError{
			Stage: "model_resolve",
			Err:   fmt.Errorf("%w: resolver not configured", ErrModelResolutionFailed),
		}
	}

	parsed, err := modelref.Parse(req.Model)
	if err != nil {
		return nil, &pipelineError{
			Stage: "model_parse",
			Err:   fmt.Errorf("%w: %v", ErrModelResolutionFailed, err),
		}
	}

	resolved, err := o.resolver.Resolve(ctx, parsed)
	if err != nil {
		return nil, &pipelineError{
			Stage: "model_resolve",
			Err:   fmt.Errorf("%w: %v", ErrModelResolutionFailed, err),
		}
	}
	return resolved, nil
}

// detectFormatStage examines the raw request body and determines the wire
// protocol format.
func (o *Orchestrator) detectFormatStage(ctx context.Context, req *engine.Request) (engine.RequestFormat, error) {
	detected, err := o.translateSvc.DetectFormat(ctx, req.RawBody)
	if err != nil {
		return "", &pipelineError{
			Stage: "detect_format",
			Err:   fmt.Errorf("%w: %v", ErrFormatDetectionFailed, err),
		}
	}
	return detected, nil
}

// translateStage converts the raw request body into the engine's canonical
// representation based on the detected format.
func (o *Orchestrator) translateStage(ctx context.Context, req *engine.Request, detected engine.RequestFormat) error {
	mappedFmt := mapTranslateFormat(detected)
	req.Format = mappedFmt

	var translated *engine.Request
	var err error

	switch detected {
	case engine.FormatOpenAIChat, engine.FormatOpenAICompat:
		translated, err = o.translateSvc.TranslateChatToRequest(ctx, req.RawBody)
	case engine.FormatCodexResponses:
		translated, err = o.translateSvc.TranslateCodexToRequest(ctx, req.RawBody)
	default:
		return &pipelineError{
			Stage: "translate",
			Err:   ErrFormatDetectionFailed,
		}
	}

	if err != nil {
		return &pipelineError{
			Stage: "translate",
			Err:   fmt.Errorf("%w: %v", ErrTranslationFailed, err),
		}
	}

	// Merge translated fields into the original request, preserving
	// externally-set fields such as ID, Headers, and UserID.
	req.MappedBody = translated.MappedBody
	if translated.Model != "" {
		req.Model = translated.Model
	}
	if translated.MaxTokens > 0 {
		req.MaxTokens = translated.MaxTokens
	} else if o.config.DefaultMaxTokens > 0 {
		req.MaxTokens = o.config.DefaultMaxTokens
	}
	if translated.Temperature != nil {
		req.Temperature = translated.Temperature
	}
	return nil
}

// selectAccountStage picks the provider account to use, either from the
// resolved reference's explicit account ID or via the AccountSelector.
//
// It returns the primary account and the full list of candidate accounts
// (including the primary) sorted by priority. The candidate list enables
// the retry layer to perform same-model account fallback on failure.
func (o *Orchestrator) selectAccountStage(ctx context.Context, resolved *modelref.ResolvedRef) (*provider.Account, []provider.Account, error) {
	if o.accountSel == nil {
		return nil, nil, &pipelineError{
			Stage: "select_account",
			Err:   fmt.Errorf("%w: account selector not configured", ErrAccountSelection),
		}
	}

	if resolved.AccountID != nil {
		account, err := o.accountRepo.FindByID(ctx, *resolved.AccountID)
		if err != nil {
			return nil, nil, &pipelineError{
				Stage: "select_account",
				Err:   fmt.Errorf("%w: lookup %s: %v", ErrAccountSelection, resolved.AccountID.String(), err),
			}
		}
		if account == nil {
			return nil, nil, &pipelineError{
				Stage: "select_account",
				Err:   fmt.Errorf("%w: account %s not found", ErrAccountSelection, resolved.AccountID.String()),
			}
		}
		if !account.IsEnabled {
			return nil, nil, &pipelineError{
				Stage: "select_account",
				Err:   fmt.Errorf("%w: account %s is disabled", ErrAccountSelection, resolved.AccountID.String()),
			}
		}
		// Explicit account: no candidates for fallback.
		return account, nil, nil
	}

	account, candidates, err := o.accountSel.SelectAccount(ctx, resolved.ProviderID, resolved.ModelName)
	if err != nil {
		return nil, nil, &pipelineError{
			Stage: "select_account",
			Err:   fmt.Errorf("%w: %v", ErrAccountSelection, err),
		}
	}
	if account == nil {
		return nil, nil, &pipelineError{
			Stage: "select_account",
			Err:   ErrAccountSelection,
		}
	}
	return account, candidates, nil
}

// getExecutorStage returns the executor for the given provider type and
// request format.
func (o *Orchestrator) getExecutorStage(ptype provider.ProviderType, format engine.RequestFormat) (engine.Executor, error) {
	exec, ok := o.execFactory.GetExecutor(ptype, format)
	if !ok {
		return nil, &pipelineError{
			Stage: "get_executor",
			Err:   ErrExecutorNotFound,
		}
	}
	return exec, nil
}
