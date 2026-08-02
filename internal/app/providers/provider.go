package providers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gorouter/internal/domain/provider"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// DefaultTestBatchConcurrency is the concurrency limit applied when a
// non-positive limit is passed to TestBatch.
const DefaultTestBatchConcurrency = 4

// auditWriteTimeout bounds each detached audit write in TestBatch so a slow
// audit can never stall batch completion indefinitely.
const auditWriteTimeout = 10 * time.Second

// Prober performs a single provider connectivity probe.
type Prober interface {
	Probe(ctx context.Context, provider *provider.Provider, account *provider.Account) error
}

// ProberFunc adapts a function to Prober.
type ProberFunc func(ctx context.Context, provider *provider.Provider, account *provider.Account) error

// Probe implements Prober.
func (f ProberFunc) Probe(ctx context.Context, provider *provider.Provider, account *provider.Account) error {
	return f(ctx, provider, account)
}

// ProviderTestResult is the outcome of one provider probe in a batch.
type ProviderTestResult struct {
	ProviderID uuid.UUID
	Name       string
	OK         bool
	Error      string
	DurationMS int64
}

// TestBatchResult aggregates per-provider probe outcomes and audit writes.
// Audit failures never fail probes; they are counted and reported here.
type TestBatchResult struct {
	Results       []ProviderTestResult
	AuditFailures int
}

// ProviderService validates provider records and runs bounded-concurrency
// connectivity test batches (decision #193).
type ProviderService struct {
	beginner ScopeBeginner
	prober   Prober
	logger   zerolog.Logger
}

// NewProviderService creates a ProviderService over the given scope beginner
// and prober.
func NewProviderService(beginner ScopeBeginner, prober Prober, logger zerolog.Logger) *ProviderService {
	return &ProviderService{beginner: beginner, prober: prober, logger: logger}
}

// Validate verifies a provider record without any network call: the record
// must exist, carry a non-empty name, a known type, valid config JSON when
// present, and a parseable http(s) base URL when present.
func (s *ProviderService) Validate(ctx context.Context, providerID uuid.UUID) error {
	scope, err := s.beginner.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = scope.Rollback(ctx) }()
	p, err := scope.Providers().FindByID(ctx, providerID)
	if err != nil {
		return fmt.Errorf("find provider: %w", err)
	}
	return validateProvider(p)
}

// TestBatch probes every provider ID with bounded concurrency. A non-positive
// concurrencyLimit falls back to DefaultTestBatchConcurrency. The caller's ids
// slice is never mutated. Each probe result is written to the audit log
// asynchronously; audit failures are counted and logged, never propagated to
// probe results.
func (s *ProviderService) TestBatch(ctx context.Context, ids []uuid.UUID, concurrencyLimit int) (TestBatchResult, error) {
	if concurrencyLimit <= 0 {
		concurrencyLimit = DefaultTestBatchConcurrency
	}
	ids = append([]uuid.UUID(nil), ids...)

	sem := make(chan struct{}, concurrencyLimit)
	results := make([]ProviderTestResult, len(ids))
	var probeWG sync.WaitGroup
	for i, id := range ids {
		probeWG.Add(1)
		go func(i int, id uuid.UUID) {
			defer probeWG.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				results[i] = ProviderTestResult{ProviderID: id, Error: ctx.Err().Error()}
				return
			}
			defer func() { <-sem }()
			results[i] = s.probe(ctx, id)
		}(i, id)
	}
	probeWG.Wait()

	auditSem := make(chan struct{}, concurrencyLimit)
	var auditWG sync.WaitGroup
	var auditFailures atomic.Int64
	for _, res := range results {
		auditWG.Add(1)
		go func(res ProviderTestResult) {
			defer auditWG.Done()
			select {
			case auditSem <- struct{}{}:
			case <-ctx.Done():
				auditFailures.Add(1)
				return
			}
			defer func() { <-auditSem }()
			if err := s.auditTest(res); err != nil {
				auditFailures.Add(1)
				s.logger.Error().
					Err(err).
					Str("provider_id", res.ProviderID.String()).
					Msg("providers: test batch audit write failed")
			}
		}(res)
	}
	auditWG.Wait()

	return TestBatchResult{Results: results, AuditFailures: int(auditFailures.Load())}, nil
}

func (s *ProviderService) probe(ctx context.Context, providerID uuid.UUID) ProviderTestResult {
	start := time.Now()
	res := ProviderTestResult{ProviderID: providerID}
	finish := func() ProviderTestResult {
		res.DurationMS = time.Since(start).Milliseconds()
		return res
	}

	scope, err := s.beginner.Begin(ctx)
	if err != nil {
		res.Error = fmt.Sprintf("begin tx: %v", err)
		return finish()
	}
	defer func() { _ = scope.Rollback(ctx) }()

	p, err := scope.Providers().FindByID(ctx, providerID)
	if err != nil {
		res.Error = fmt.Sprintf("provider lookup: %v", err)
		return finish()
	}
	res.Name = p.Name

	accounts, err := scope.Accounts().FindByProviderID(ctx, providerID)
	if err != nil {
		res.Error = fmt.Sprintf("account lookup: %v", err)
		return finish()
	}
	account, ok := firstEnabledAccount(accounts)
	if !ok {
		res.Error = "no enabled account"
		return finish()
	}
	if s.prober == nil {
		res.Error = "prober not configured"
		return finish()
	}
	if err := s.prober.Probe(ctx, p, account); err != nil {
		res.Error = err.Error()
		return finish()
	}
	res.OK = true
	return finish()
}

func (s *ProviderService) auditTest(res ProviderTestResult) error {
	auditCtx, cancel := context.WithTimeout(context.Background(), auditWriteTimeout)
	defer cancel()
	scope, err := s.beginner.Begin(auditCtx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = scope.Rollback(auditCtx) }()
	id := res.ProviderID
	details := map[string]any{
		"ok":          res.OK,
		"duration_ms": res.DurationMS,
	}
	if err := scope.AuditLog().Create(auditCtx, auditEntry(nil, "provider.test_batch", "provider", &id, details)); err != nil {
		return fmt.Errorf("audit test batch: %w", err)
	}
	return scope.Commit(auditCtx)
}

func firstEnabledAccount(accounts []provider.Account) (*provider.Account, bool) {
	for i := range accounts {
		if accounts[i].IsEnabled {
			return &accounts[i], true
		}
	}
	return nil, false
}

func validateProvider(p *provider.Provider) error {
	if p == nil {
		return errors.New("providers: provider not found")
	}
	if strings.TrimSpace(p.Name) == "" {
		return errors.New("providers: provider name is required")
	}
	switch p.Type {
	case provider.ProviderOpenAI, provider.ProviderAnthropic, provider.ProviderAzure, provider.ProviderCustom:
	default:
		return fmt.Errorf("providers: unsupported provider type %q", p.Type)
	}
	if len(p.Config) > 0 {
		var v any
		if err := json.Unmarshal(p.Config, &v); err != nil {
			return fmt.Errorf("providers: invalid config json: %w", err)
		}
	}
	if p.BaseURL != "" {
		u, err := url.Parse(p.BaseURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
			return fmt.Errorf("providers: invalid base url %q", p.BaseURL)
		}
	}
	return nil
}
