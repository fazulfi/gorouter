package usage

import (
	"context"
	"sync"
	"time"

	"gorouter/internal/domain/usage"

	"github.com/google/uuid"
)

// fakeUsageRepo is the in-memory backing store for the fake usage scope.
// DetailsBetween mirrors the repository contract: only details with
// occurred_at >= since are returned, ordered ascending.
type fakeUsageRepo struct {
	mu               sync.Mutex
	details          []usage.RequestDetail
	history          []usage.RequestHistoryEntry
	aggWrites        []usage.DailyAggregate
	detailWriteErr   error
	historyAppendErr error
	aggErr           error
	betweenErr       error
	recentErr        error
	recentLimit      int
	recentOut        []usage.RequestHistoryEntry
	purgeErr         error
	purgeCutoff      time.Time
	purgeOut         usage.PurgeStats
}

func (f *fakeUsageRepo) AggregateDaily(_ context.Context, agg *usage.DailyAggregate) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.aggErr != nil {
		return f.aggErr
	}
	cp := *agg
	f.aggWrites = append(f.aggWrites, cp)
	return nil
}

func (f *fakeUsageRepo) Daily(context.Context, time.Time) ([]usage.DailyAggregate, error) {
	return nil, nil
}

func (f *fakeUsageRepo) WriteRequestDetail(_ context.Context, detail *usage.RequestDetail) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.detailWriteErr != nil {
		return f.detailWriteErr
	}
	cp := *detail
	f.details = append(f.details, cp)
	return nil
}

func (f *fakeUsageRepo) RequestDetail(context.Context, uuid.UUID) (*usage.RequestDetail, error) {
	return nil, nil
}

func (f *fakeUsageRepo) AppendHistory(_ context.Context, entry *usage.RequestHistoryEntry) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.historyAppendErr != nil {
		return f.historyAppendErr
	}
	cp := *entry
	f.history = append(f.history, cp)
	return nil
}

func (f *fakeUsageRepo) RecentHistory(_ context.Context, limit int) ([]usage.RequestHistoryEntry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recentLimit = limit
	if f.recentErr != nil {
		return nil, f.recentErr
	}
	return append([]usage.RequestHistoryEntry(nil), f.recentOut...), nil
}

func (f *fakeUsageRepo) DetailsBetween(_ context.Context, since time.Time) ([]usage.RequestDetail, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.betweenErr != nil {
		return nil, f.betweenErr
	}
	var out []usage.RequestDetail
	for _, d := range f.details {
		if d.OccurredAt != nil && !d.OccurredAt.Before(since) {
			out = append(out, d)
		}
	}
	return out, nil
}

func (f *fakeUsageRepo) PurgeBefore(_ context.Context, cutoff time.Time) (usage.PurgeStats, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.purgeCutoff = cutoff
	if f.purgeErr != nil {
		return usage.PurgeStats{}, f.purgeErr
	}
	return f.purgeOut, nil
}

// fakeUsageScope models the transaction contract: repository writes are
// staged in the backing store on success and become visible only on Commit;
// Rollback discards the transaction and is a no-op after Commit, mirroring
// pgx. Commit failure propagates.
type fakeUsageScope struct {
	repo      *fakeUsageRepo
	commits   int
	rollbacks int
	commitErr error
	beginErr  error
	committed bool
}

func (s *fakeUsageScope) Usage() usage.UsageRepository { return s.repo }

func (s *fakeUsageScope) Commit(context.Context) error {
	if s.commitErr != nil {
		return s.commitErr
	}
	s.commits++
	s.committed = true
	return nil
}

func (s *fakeUsageScope) Rollback(context.Context) error {
	if s.committed {
		return nil
	}
	s.rollbacks++
	return nil
}

func newFakeBeginner(scope *fakeUsageScope) UsageScopeBeginner {
	return UsageScopeBeginnerFunc(func(ctx context.Context) (UsageScope, error) {
		if scope.beginErr != nil {
			return nil, scope.beginErr
		}
		return scope, nil
	})
}

func newFakeScope() (*fakeUsageScope, *fakeUsageRepo) {
	repo := &fakeUsageRepo{}
	return &fakeUsageScope{repo: repo}, repo
}
