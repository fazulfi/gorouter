package providers

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"gorouter/internal/app/tx"
	"gorouter/internal/domain/provider"
	enginerouting "gorouter/internal/engine/routing"

	"github.com/google/uuid"
)

type fakePools struct {
	mu      sync.Mutex
	pools   map[uuid.UUID]provider.ProxyPool
	members map[uuid.UUID][]provider.PoolMember
	err     error
}

func newFakePools() *fakePools {
	return &fakePools{
		pools:   make(map[uuid.UUID]provider.ProxyPool),
		members: make(map[uuid.UUID][]provider.PoolMember),
	}
}

func (f *fakePools) List(_ context.Context) ([]provider.ProxyPool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	out := make([]provider.ProxyPool, 0, len(f.pools))
	for _, p := range f.pools {
		out = append(out, p)
	}
	return out, nil
}

func (f *fakePools) Create(_ context.Context, p *provider.ProxyPool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.pools[p.ID] = *p
	return nil
}

func (f *fakePools) Update(_ context.Context, p *provider.ProxyPool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	if _, ok := f.pools[p.ID]; !ok {
		return errors.New("pool not found")
	}
	f.pools[p.ID] = *p
	return nil
}

func (f *fakePools) Delete(_ context.Context, id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	delete(f.pools, id)
	return nil
}

func (f *fakePools) Members(_ context.Context, poolID uuid.UUID) ([]provider.PoolMember, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	return append([]provider.PoolMember(nil), f.members[poolID]...), nil
}

func (f *fakePools) SetMembers(_ context.Context, poolID uuid.UUID, members []provider.PoolMember) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.members[poolID] = append([]provider.PoolMember(nil), members...)
	return nil
}

type fakeNodes struct {
	mu    sync.Mutex
	nodes map[string]enginerouting.ProviderNode
	err   error
}

func newFakeNodes() *fakeNodes {
	return &fakeNodes{nodes: make(map[string]enginerouting.ProviderNode)}
}

func (f *fakeNodes) List(_ context.Context) ([]enginerouting.ProviderNode, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	out := make([]enginerouting.ProviderNode, 0, len(f.nodes))
	for _, n := range f.nodes {
		out = append(out, n)
	}
	return out, nil
}

func (f *fakeNodes) Save(_ context.Context, node enginerouting.ProviderNode) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.nodes[node.ID] = node
	return nil
}

func (f *fakeNodes) Delete(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	delete(f.nodes, id)
	return nil
}

type fakeProviders struct {
	mu      sync.Mutex
	byID    map[uuid.UUID]provider.Provider
	missing error
}

func newFakeProviders() *fakeProviders {
	return &fakeProviders{byID: make(map[uuid.UUID]provider.Provider)}
}

func (f *fakeProviders) FindByID(_ context.Context, id uuid.UUID) (*provider.Provider, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.missing != nil {
		return nil, f.missing
	}
	p, ok := f.byID[id]
	if !ok {
		return nil, errors.New("provider not found")
	}
	cp := p
	return &cp, nil
}

func (f *fakeProviders) FindByType(_ context.Context, _ provider.ProviderType) ([]provider.Provider, error) {
	return nil, errors.New("unused")
}

func (f *fakeProviders) List(_ context.Context) ([]provider.Provider, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]provider.Provider, 0, len(f.byID))
	for _, p := range f.byID {
		out = append(out, p)
	}
	return out, nil
}

func (f *fakeProviders) Create(_ context.Context, _ *provider.Provider) error {
	return errors.New("unused")
}
func (f *fakeProviders) Update(_ context.Context, _ *provider.Provider) error {
	return errors.New("unused")
}
func (f *fakeProviders) Delete(_ context.Context, _ uuid.UUID) error { return errors.New("unused") }

type fakeAccounts struct {
	mu      sync.Mutex
	byProv  map[uuid.UUID][]provider.Account
	missing error
}

func newFakeAccounts() *fakeAccounts {
	return &fakeAccounts{byProv: make(map[uuid.UUID][]provider.Account)}
}

func (f *fakeAccounts) FindByProviderID(_ context.Context, providerID uuid.UUID) ([]provider.Account, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.missing != nil {
		return nil, f.missing
	}
	return append([]provider.Account(nil), f.byProv[providerID]...), nil
}

func (f *fakeAccounts) FindByID(_ context.Context, _ uuid.UUID) (*provider.Account, error) {
	return nil, errors.New("unused")
}

func (f *fakeAccounts) Create(_ context.Context, _ *provider.Account) error {
	return errors.New("unused")
}
func (f *fakeAccounts) Update(_ context.Context, _ *provider.Account) error {
	return errors.New("unused")
}
func (f *fakeAccounts) Delete(_ context.Context, _ uuid.UUID) error { return errors.New("unused") }

type fakeAudit struct {
	mu      sync.Mutex
	entries []*tx.AuditLogEntry
	err     error
}

func newFakeAudit() *fakeAudit { return &fakeAudit{} }

func (f *fakeAudit) Create(_ context.Context, entry *tx.AuditLogEntry) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	cp := *entry
	f.entries = append(f.entries, &cp)
	return nil
}

func (f *fakeAudit) all() []*tx.AuditLogEntry {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*tx.AuditLogEntry(nil), f.entries...)
}

type fakeScope struct {
	beginner *fakeBeginner
}

func (s *fakeScope) Pools() provider.PoolRepository         { return s.beginner.pools }
func (s *fakeScope) Nodes() enginerouting.NodeStore         { return s.beginner.nodes }
func (s *fakeScope) Providers() provider.ProviderRepository { return s.beginner.providers }
func (s *fakeScope) Accounts() provider.AccountRepository   { return s.beginner.accounts }
func (s *fakeScope) AuditLog() tx.AuditLogRepository        { return s.beginner.audit }
func (s *fakeScope) Commit(_ context.Context) error         { s.beginner.commits.Add(1); return nil }
func (s *fakeScope) Rollback(_ context.Context) error       { s.beginner.rollbacks.Add(1); return nil }

type fakeBeginner struct {
	mu        sync.Mutex
	scopes    []*fakeScope
	errOn     error
	commits   atomic.Int64
	rollbacks atomic.Int64
	pools     *fakePools
	nodes     *fakeNodes
	providers *fakeProviders
	accounts  *fakeAccounts
	audit     *fakeAudit
}

func newFakeBeginner() *fakeBeginner {
	return &fakeBeginner{
		pools:     newFakePools(),
		nodes:     newFakeNodes(),
		providers: newFakeProviders(),
		accounts:  newFakeAccounts(),
		audit:     newFakeAudit(),
	}
}

func (b *fakeBeginner) Begin(_ context.Context) (Scope, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.errOn != nil {
		return nil, b.errOn
	}
	s := &fakeScope{beginner: b}
	b.scopes = append(b.scopes, s)
	return s, nil
}

func (b *fakeBeginner) lastScope() *fakeScope {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.scopes) == 0 {
		return nil
	}
	return b.scopes[len(b.scopes)-1]
}

type countingProber struct {
	current atomic.Int64
	max     atomic.Int64
	calls   atomic.Int64
	sleep   time.Duration
	err     error
}

func (p *countingProber) Probe(context.Context, *provider.Provider, *provider.Account) error {
	cur := p.current.Add(1)
	defer p.current.Add(-1)
	p.calls.Add(1)
	for {
		m := p.max.Load()
		if cur <= m || p.max.CompareAndSwap(m, cur) {
			break
		}
	}
	if p.sleep > 0 {
		time.Sleep(p.sleep)
	}
	return p.err
}
