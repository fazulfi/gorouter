package refresh

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/google/uuid"

	domainrefresh "gorouter/internal/domain/refresh"
)

type call struct {
	done   chan struct{}
	res    Result
	cancel context.CancelFunc
	refs   int32
}

type Result struct {
	Credential *domainrefresh.TokenCredential
	Err        error
	Definitive bool
}

type SingleFlight struct {
	mu    sync.Mutex
	calls map[uuid.UUID]*call
}

func NewSingleFlight() *SingleFlight {
	return &SingleFlight{
		calls: make(map[uuid.UUID]*call),
	}
}

func (sf *SingleFlight) Do(ctx context.Context, accountID uuid.UUID, fn func(context.Context) (Result, error)) (Result, error) {
	sf.mu.Lock()
	if c, ok := sf.calls[accountID]; ok {
		atomic.AddInt32(&c.refs, 1)
		sf.mu.Unlock()
		defer sf.decRef(c)
		return sf.wait(ctx, c)
	}

	ctxOp, cancel := context.WithCancel(context.Background())
	c := &call{
		done:   make(chan struct{}),
		cancel: cancel,
		refs:   1,
	}
	sf.calls[accountID] = c
	sf.mu.Unlock()

	defer sf.decRef(c)

	go func() {
		res, err := fn(ctxOp)
		if err != nil {
			c.res = Result{Err: err}
		} else {
			c.res = res
		}
		close(c.done)
	}()

	return sf.wait(ctx, c)
}

func (sf *SingleFlight) decRef(c *call) {
	if atomic.AddInt32(&c.refs, -1) == 0 {
		c.cancel()
	}
}

func (sf *SingleFlight) Forget(accountID uuid.UUID) {
	sf.mu.Lock()
	delete(sf.calls, accountID)
	sf.mu.Unlock()
}

func (sf *SingleFlight) ForgetAll() {
	sf.mu.Lock()
	sf.calls = make(map[uuid.UUID]*call)
	sf.mu.Unlock()
}

func (sf *SingleFlight) wait(ctx context.Context, c *call) (Result, error) {
	select {
	case <-c.done:
		return c.res, c.res.Err
	case <-ctx.Done():
		return Result{}, ctx.Err()
	}
}
