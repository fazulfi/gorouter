package console

import (
	"context"
	"sort"
	"sync"
	"time"

	"gorouter/internal/domain/console"
)

// fakeConsoleRepo is the in-memory backing store for the fake console
// scope. It mirrors the repository contract: ListAfter returns only
// entries with seq > the given seq, ordered by seq ascending and capped at
// 50; MaxSeq returns the highest stored seq; PurgeBefore removes entries
// with retention_until strictly before the cutoff.
type fakeConsoleRepo struct {
	mu          sync.Mutex
	entries     []console.ConsoleLog
	appendErr   error
	listErr     error
	maxSeqErr   error
	purgeErr    error
	purgeCutoff time.Time
	purgeOut    int64
	listLimits  []int
}

func (f *fakeConsoleRepo) Append(_ context.Context, entry *console.ConsoleLog) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.appendErr != nil {
		return f.appendErr
	}
	cp := *entry
	cp.ID = int64(len(f.entries) + 1)
	f.entries = append(f.entries, cp)
	return nil
}

func (f *fakeConsoleRepo) ListAfter(_ context.Context, seq int64, limit int) ([]console.ConsoleLog, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.listErr != nil {
		return nil, f.listErr
	}
	f.listLimits = append(f.listLimits, limit)
	var out []console.ConsoleLog
	for _, e := range f.entries {
		if e.Seq > seq {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Seq < out[j].Seq })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (f *fakeConsoleRepo) MaxSeq(context.Context) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.maxSeqErr != nil {
		return 0, f.maxSeqErr
	}
	var max int64
	for _, e := range f.entries {
		if e.Seq > max {
			max = e.Seq
		}
	}
	return max, nil
}

func (f *fakeConsoleRepo) DeleteBefore(_ context.Context, seq int64) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	kept := f.entries[:0]
	var deleted int64
	for _, e := range f.entries {
		if e.Seq < seq {
			deleted++
		} else {
			kept = append(kept, e)
		}
	}
	f.entries = kept
	return deleted, nil
}

func (f *fakeConsoleRepo) PurgeBefore(_ context.Context, cutoff time.Time) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.purgeCutoff = cutoff
	if f.purgeErr != nil {
		return 0, f.purgeErr
	}
	if f.purgeOut >= 0 {
		return f.purgeOut, nil
	}
	kept := f.entries[:0]
	var deleted int64
	for _, e := range f.entries {
		if e.RetentionUntil != nil && e.RetentionUntil.Before(cutoff) {
			deleted++
		} else {
			kept = append(kept, e)
		}
	}
	f.entries = kept
	return deleted, nil
}

// fakeConsoleScope models the transaction contract: repository writes are
// staged in the backing store and become visible on Commit; Rollback
// discards the transaction and is a no-op after Commit, mirroring pgx.
// Commit failure propagates.
type fakeConsoleScope struct {
	repo      *fakeConsoleRepo
	commits   int
	rollbacks int
	commitErr error
	beginErr  error
	committed bool
}

func (s *fakeConsoleScope) ConsoleLogs() console.ConsoleLogRepository { return s.repo }

func (s *fakeConsoleScope) Commit(context.Context) error {
	if s.commitErr != nil {
		return s.commitErr
	}
	s.commits++
	s.committed = true
	return nil
}

func (s *fakeConsoleScope) Rollback(context.Context) error {
	if s.committed {
		return nil
	}
	s.rollbacks++
	return nil
}

func newFakeBeginner(scope *fakeConsoleScope) ConsoleScopeBeginner {
	return ConsoleScopeBeginnerFunc(func(ctx context.Context) (ConsoleScope, error) {
		if scope.beginErr != nil {
			return nil, scope.beginErr
		}
		return scope, nil
	})
}

func newFakeScope() (*fakeConsoleScope, *fakeConsoleRepo) {
	repo := &fakeConsoleRepo{purgeOut: -1}
	return &fakeConsoleScope{repo: repo}, repo
}
