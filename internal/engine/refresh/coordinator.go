package refresh

import (
	"context"
	"math/rand"
	"time"

	"github.com/google/uuid"

	domainrefresh "gorouter/internal/domain/refresh"
)

type Clock interface {
	Now() time.Time
	After(d time.Duration) <-chan time.Time
}

type realClock struct{}

func (realClock) Now() time.Time                         { return time.Now() }
func (realClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

type Sleeper interface {
	Sleep(d time.Duration)
}

type realSleeper struct{}

func (realSleeper) Sleep(d time.Duration) { time.Sleep(d) }

type JitterFunc func(base, jitter time.Duration) time.Duration

func DefaultJitter(base, jitter time.Duration) time.Duration {
	if jitter <= 0 {
		return base
	}
	offset := time.Duration(rand.Int63n(2*int64(jitter)+1)) - jitter
	return base + offset
}

type TokenRefresher interface {
	Refresh(ctx context.Context, accountID uuid.UUID, currentToken string) (*domainrefresh.TokenCredential, error)
}

type RefreshHandler interface {
	OnRefreshSuccess(ctx context.Context, accountID uuid.UUID, cred *domainrefresh.TokenCredential) error
	OnRefreshDefinitive(ctx context.Context, accountID uuid.UUID, err error) error
}

type AccountResolver interface {
	AlternateAccount(ctx context.Context, accountID uuid.UUID) (uuid.UUID, bool, error)
}

type AlternateAccountRefresher interface {
	RefreshAlternate(ctx context.Context, accountID uuid.UUID) (*domainrefresh.TokenCredential, error)
}

type Coordinator struct {
	policy       Policy
	sf           *SingleFlight
	refresher    TokenRefresher
	handler      RefreshHandler
	resolver     AccountResolver
	altRefresher AlternateAccountRefresher
	clock        Clock
	sleeper      Sleeper
	jitter       JitterFunc
	sem          chan struct{}
}

type Option func(*Coordinator)

func WithClock(c Clock) Option                     { return func(co *Coordinator) { co.clock = c } }
func WithSleeper(s Sleeper) Option                 { return func(co *Coordinator) { co.sleeper = s } }
func WithJitter(j JitterFunc) Option               { return func(co *Coordinator) { co.jitter = j } }
func WithSingleFlight(sf *SingleFlight) Option     { return func(co *Coordinator) { co.sf = sf } }
func WithAccountResolver(r AccountResolver) Option { return func(co *Coordinator) { co.resolver = r } }
func WithAlternateRefresher(r AlternateAccountRefresher) Option {
	return func(co *Coordinator) { co.altRefresher = r }
}

func NewCoordinator(policy Policy, refresher TokenRefresher, handler RefreshHandler, opts ...Option) *Coordinator {
	c := &Coordinator{
		policy:    policy,
		sf:        NewSingleFlight(),
		refresher: refresher,
		handler:   handler,
		clock:     realClock{},
		sleeper:   realSleeper{},
		jitter:    DefaultJitter,
		sem:       make(chan struct{}, policy.MaxParallel()),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

func (c *Coordinator) RefreshNow(ctx context.Context, accountID uuid.UUID, currentToken string) (*domainrefresh.TokenCredential, error) {
	type sfResult struct {
		res Result
		err error
	}

	sfCh := make(chan sfResult, 1)
	go func() {
		res, err := c.sf.Do(ctx, accountID, func(innerCtx context.Context) (Result, error) {
			select {
			case c.sem <- struct{}{}:
			case <-innerCtx.Done():
				return Result{}, innerCtx.Err()
			}
			defer func() { <-c.sem }()

			return c.attemptSequence(innerCtx, accountID, currentToken)
		})
		sfCh <- sfResult{res, err}
	}()

	rw := c.policy.RequestWait()
	if rw <= 0 {
		r := <-sfCh
		return c.processResult(r.res, r.err)
	}

	select {
	case r := <-sfCh:
		return c.processResult(r.res, r.err)

	case <-time.After(rw):
		if c.resolver != nil && c.altRefresher != nil {
			altID, ok, err := c.resolver.AlternateAccount(ctx, accountID)
			if err == nil && ok {
				cred, altErr := c.altRefresher.RefreshAlternate(ctx, altID)
				if altErr == nil && cred != nil {
					return cred, nil
				}
			}
		}
		r := <-sfCh
		return c.processResult(r.res, r.err)
	}
}

func (c *Coordinator) processResult(res Result, err error) (*domainrefresh.TokenCredential, error) {
	if err != nil {
		return nil, err
	}
	if res.Definitive {
		return nil, domainrefresh.ErrDefinitive("refresh token rejected")
	}
	if res.Credential == nil {
		return nil, domainrefresh.ErrTransient("no credential returned")
	}
	return res.Credential, nil
}

func (c *Coordinator) NextProactiveRefresh(expiresAt *time.Time) *time.Duration {
	if expiresAt == nil {
		d := c.policy.NoExpiryInterval()
		return &d
	}
	now := c.clock.Now()
	refreshAt := expiresAt.Add(-c.policy.DefaultLeadTime())
	if !now.Before(refreshAt) {
		d := time.Duration(0)
		return &d
	}
	d := refreshAt.Sub(now)
	return &d
}

func (c *Coordinator) attemptSequence(ctx context.Context, accountID uuid.UUID, currentToken string) (Result, error) {
	var lastErr error

	for attempt := 0; attempt < c.policy.MaxAttempts(); attempt++ {
		if attempt > 0 {
			backoff := c.jitter(c.policy.BackoffBase(), c.policy.JitterMax())
			select {
			case <-c.clock.After(backoff):
			case <-ctx.Done():
				return Result{}, ctx.Err()
			}
		}

		attemptCtx, cancel := context.WithTimeout(ctx, c.policy.AttemptTimeout())
		cred, err := c.refresher.Refresh(attemptCtx, accountID, currentToken)
		cancel()

		if err == nil {
			if persistErr := c.handler.OnRefreshSuccess(ctx, accountID, cred); persistErr != nil {
				return Result{}, persistErr
			}
			return Result{Credential: cred}, nil
		}

		lastErr = err

		if domainrefresh.IsDefinitive(err) {
			if handlerErr := c.handler.OnRefreshDefinitive(ctx, accountID, err); handlerErr != nil {
				return Result{}, handlerErr
			}
			return Result{Definitive: true, Err: err}, nil
		}

		if !domainrefresh.IsTransient(err) {
			return Result{Err: err}, nil
		}
	}

	return Result{Err: lastErr}, nil
}

func (c *Coordinator) ProactiveRefreshIfNeeded(ctx context.Context, accountID uuid.UUID, currentToken string, expiresAt *time.Time) bool {
	next := c.NextProactiveRefresh(expiresAt)
	if next == nil || *next > 0 {
		return false
	}
	go func() {
		_, _ = c.RefreshNow(ctx, accountID, currentToken)
	}()
	return true
}

func (c *Coordinator) SingleFlight() *SingleFlight { return c.sf }
func (c *Coordinator) SemaphoreSize() int          { return cap(c.sem) }
